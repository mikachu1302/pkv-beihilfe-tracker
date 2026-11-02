import {useNavigate} from "react-router";
import {Svc} from "../api";
import type {YearDTO} from "../api";
import {useApp} from "../App";
import {StackedBars} from "../components/charts";
import type {StackRow} from "../components/charts";
import {Empty, ErrorNotice, Loading} from "../components/ui";
import {eur, num} from "../lib/format";
import {useBackend} from "../lib/useBackend";

/** Overview of all Leistungsjahre: refunds per payer and own share. */
export default function Overview() {
    const {setYear, dataVersion} = useApp();
    const navigate = useNavigate();
    const ov = useBackend(() => Svc.Overview(), [dataVersion]);
    if (ov.error) return <ErrorNotice error={ov.error}/>;
    if (!ov.data) return <Loading/>;
    const years = (ov.data.years ?? []).filter(y => y.invoices > 0 || num(y.allowances) > 0);
    const total = ov.data.total;
    const payers = (total.payers ?? []).map(p => ({id: p.payerId, name: p.payerName}));
    // Colour follows the payer (settings order), the own share takes the next slot.
    const slot = (i: number) => Math.min(i + 1, 5);
    const ownSlot = slot(payers.length);
    const payerOf = (y: YearDTO, id: string) => (y.payers ?? []).find(p => p.payerId === id);

    const rows: StackRow[] = years.map(y => ({
        key: String(y.year),
        label: y.year === 0 ? "ohne Datum" : String(y.year),
        total: num(y.billed),
        parts: [
            ...payers.map((p, i) => ({key: p.id, label: `${p.name} erstattet`, value: num(payerOf(y, p.id)?.actual), slot: slot(i)})),
            {key: "own", label: "Nicht erstattet", value: num(y.ownShare), slot: ownSlot},
        ],
    }));
    const counted = years.filter(y => y.year !== 0).length || 1;
    const avg = (v: string) => eur(num(v) / counted);

    function open(year: number) {
        setYear(year);
        navigate("/dashboard");
    }

    return (
        <>
            <div className="page-head"><h1>Alle Jahre</h1></div>
            {years.length === 0 ? <Empty>Noch keine Rechnungen erfasst.</Empty> : (
                <>
                    <div className="panel">
                        <StackedBars rows={rows} label="Rechnungsbeträge je Leistungsjahr: erstattet und Eigenanteil"
                                     legend={[...payers.map((p, i) => ({label: `${p.name} erstattet`, slot: slot(i)})),
                                         {label: "Nicht erstattet (Eigenanteil oder offen)", slot: ownSlot}]}/>
                    </div>
                    <div className="table-wrap">
                        <table>
                            <thead>
                            <tr>
                                <th>Jahr</th>
                                <th className="num">Rechnungen</th>
                                <th className="num">Rechnungsbeträge</th>
                                {payers.map(p => <th key={p.id} className="num">{p.name}<br/>SOLL / IST</th>)}
                                <th className="num" title="Rechnungsbeträge minus Erstattungen">Nicht erstattet</th>
                                <th className="num">Krankenhaustagegeld u. Ä.</th>
                            </tr>
                            </thead>
                            <tbody>
                            {years.map(y => (
                                <tr key={y.year} className="click" onClick={() => open(y.year)} title="Dashboard dieses Jahres öffnen">
                                    <td>{y.year === 0 ? "ohne Datum" : y.year}</td>
                                    <td className="num">{y.invoices}</td>
                                    <td className="num">{eur(y.billed)}</td>
                                    {payers.map(p => {
                                        const py = payerOf(y, p.id);
                                        return <td key={p.id} className="num">{py ? `${eur(py.expected)} / ${eur(py.actual)}` : "–"}</td>;
                                    })}
                                    <td className="num">{eur(y.ownShare)}</td>
                                    <td className="num">{num(y.allowances) ? eur(y.allowances) : "–"}</td>
                                </tr>
                            ))}
                            </tbody>
                            <tfoot>
                            <tr>
                                <td><strong>Gesamt</strong></td>
                                <td className="num">{total.invoices}</td>
                                <td className="num">{eur(total.billed)}</td>
                                {payers.map(p => {
                                    const py = payerOf(total, p.id);
                                    return <td key={p.id} className="num">{py ? `${eur(py.expected)} / ${eur(py.actual)}` : "–"}</td>;
                                })}
                                <td className="num">{eur(total.ownShare)}</td>
                                <td className="num">{eur(total.allowances)}</td>
                            </tr>
                            <tr>
                                <td className="muted">Ø pro Jahr</td>
                                <td className="num muted">{(total.invoices / counted).toLocaleString("de-DE", {maximumFractionDigits: 1})}</td>
                                <td className="num muted">{avg(total.billed)}</td>
                                {payers.map(p => <td key={p.id} className="num muted">{avg(payerOf(total, p.id)?.expected ?? "0")} / {avg(payerOf(total, p.id)?.actual ?? "0")}</td>)}
                                <td className="num muted">{avg(total.ownShare)}</td>
                                <td className="num muted">{avg(total.allowances)}</td>
                            </tr>
                            </tfoot>
                        </table>
                    </div>
                </>
            )}
        </>
    );
}
