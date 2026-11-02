import {useEffect, useState} from "react";
import {Desktop, Svc} from "../api";
import type {ImportPreviewDTO, ImportRowDTO} from "../api";
import {useToast} from "../components/feedback";
import {ErrorNotice, Field, Loading, Notice} from "../components/ui";
import {date, errMsg, eur, plural} from "../lib/format";
import {useBackend} from "../lib/useBackend";

/** Import of the old spreadsheet with preview. */
export default function Import() {
    const toast = useToast();
    const settings = useBackend(() => Svc.GetSettings(), []);
    const payers = settings.data?.payers ?? [];
    const beihilfen = payers.filter(p => p.kind === "beihilfe");
    const pkvs = payers.filter(p => p.kind === "pkv");
    const [bh, setBh] = useState("");
    const [pkv, setPkv] = useState("");
    const [preview, setPreview] = useState<ImportPreviewDTO | null>(null);
    const [include, setInclude] = useState<Set<string>>(new Set());
    const [error, setError] = useState("");
    const [busy, setBusy] = useState(false);

    // Preselect the first Beihilfe and PKV once the settings are there.
    useEffect(() => {
        const all = settings.data?.payers ?? [];
        setBh(b => b || (all.find(x => x.kind === "beihilfe")?.id ?? ""));
        setPkv(p => p || (all.find(x => x.kind === "pkv")?.id ?? ""));
    }, [settings.data]);

    async function choose() {
        setError("");
        try {
            const path = await Desktop.ChooseFile("Excel-Datei wählen", "Excel", "*.xlsx;*.xlsm");
            if (!path) return;
            setBusy(true);
            const pv = await Svc.PreviewSpreadsheet(path, bh, pkv);
            setPreview(pv);
            setInclude(new Set((pv.rows ?? []).filter(r => !r.duplicate).map(r => r.key)));
        } catch (e) {
            setError(errMsg(e));
        } finally {
            setBusy(false);
        }
    }

    async function commit() {
        if (!preview) return;
        setBusy(true);
        try {
            const res = await Svc.CommitSpreadsheet(preview.token, [...include]);
            toast.show(`${plural(res.invoices, "Rechnung", "Rechnungen")} importiert` +
                (res.allowances ? `, ${plural(res.allowances, "Leistung", "Leistungen")} ohne Rechnung` : "") +
                (res.skipped ? `, ${res.skipped} übersprungen` : ""));
            setPreview(null);
        } catch (e) {
            setError(errMsg(e));
        } finally {
            setBusy(false);
        }
    }

    const toggle = (key: string) => setInclude(s => {
        const n = new Set(s);
        if (n.has(key)) n.delete(key); else n.add(key);
        return n;
    });

    if (settings.error) return <ErrorNotice error={settings.error}/>;
    if (!settings.data) return <Loading/>;
    const rows = preview?.rows ?? [];
    // The import reads every sheet with a table: name the sheet when there is more than one.
    const sheets = new Set(rows.map(r => r.sheet)).size > 1;
    const where = (r: ImportRowDTO) => (sheets ? `${r.sheet}, Zeile ${r.row}` : `Zeile ${r.row}`);
    return (
        <>
            <div className="page-head"><h1>Excel importieren</h1></div>
            <div className="panel">
                <p>
                    Liest eine Tabelle im Aufbau der bisherigen Excel-Datei („Abrechnung Krankenkasse“): eine Zeile pro
                    Rechnung mit Rechnungsdatum, Behandler, Gesamtbetrag sowie Einreichungsdatum und erstattetem Betrag
                    für Beihilfe und Krankenkasse. Die Spalten werden an ihren Überschriften erkannt. Vor dem Speichern
                    gibt es eine Vorschau.
                </p>
                <div className="grid">
                    <Field label="Spalten „Beihilfe“ gehören zu" htmlFor="imp-bh">
                        <select id="imp-bh" value={bh} onChange={e => setBh(e.target.value)}>
                            <option value="">– nicht übernehmen –</option>
                            {beihilfen.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
                        </select>
                    </Field>
                    <Field label="Spalten „Krankenkasse“ gehören zu" htmlFor="imp-pkv">
                        <select id="imp-pkv" value={pkv} onChange={e => setPkv(e.target.value)}>
                            {pkvs.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
                        </select>
                    </Field>
                </div>
                <div className="actions">
                    <button className="primary" disabled={busy || !pkv} onClick={choose}>Excel-Datei wählen …</button>
                </div>
            </div>
            {error && <ErrorNotice error={error}/>}
            {preview && (
                <>
                    <div className="panel">
                        <strong>{preview.file}</strong> · {sheets ? "Blätter" : "Blatt"} „{preview.sheet}“ · {plural(rows.length, "Rechnung", "Rechnungen")}
                        {" "}({(preview.years ?? []).map(y => `${y.year || "ohne Datum"}: ${y.count}`).join(", ")})
                        {preview.allowances > 0 && <> · {plural(preview.allowances, "Leistung", "Leistungen")} ohne Rechnung</>}
                        {preview.duplicates > 0 && <Notice kind="warn">{plural(preview.duplicates, "Rechnung ist", "Rechnungen sind")} schon vorhanden (gleiches Datum, Betrag und Behandler) und nicht ausgewählt.</Notice>}
                        {preview.warnings > 0 && <Notice kind="warn">{plural(preview.warnings, "Hinweis", "Hinweise")} zu einzelnen Zeilen – bitte prüfen (Tippfehler bei Daten, Erstattungen über dem Rechnungsbetrag). Korrigieren lässt sich alles auch nach dem Import.</Notice>}
                    </div>
                    <div className="table-wrap">
                        <table>
                            <thead>
                            <tr>
                                <th className="check-col">
                                    <input type="checkbox" aria-label="Alle auswählen" checked={include.size === rows.length}
                                           onChange={() => setInclude(include.size === rows.length ? new Set() : new Set(rows.map(r => r.key)))}/>
                                </th>
                                <th>{sheets ? "Blatt / Zeile" : "Zeile"}</th><th>Rechnungsdatum</th><th>Behandler</th><th className="num">Betrag</th><th>Status</th><th>Hinweise</th>
                            </tr>
                            </thead>
                            <tbody>
                            {rows.map(r => (
                                <tr key={r.key}>
                                    <td><input type="checkbox" aria-label={`${where(r)} importieren`} checked={include.has(r.key)} onChange={() => toggle(r.key)}/></td>
                                    <td className="muted">{sheets ? `${r.sheet} · ${r.row}` : r.row}</td>
                                    <td>{date(r.invoiceDate)}</td>
                                    <td>{r.provider}{r.note && <div className="sub">{r.note}</div>}</td>
                                    <td className="num">{eur(r.amount)}</td>
                                    <td className="small">{(r.status ?? []).map(s => <div key={s}>{s}</div>)}</td>
                                    <td className="small">
                                        {r.duplicate && <div className="neg">schon vorhanden</div>}
                                        {(r.warnings ?? []).map(w => <div key={w}>{w}</div>)}
                                    </td>
                                </tr>
                            ))}
                            </tbody>
                        </table>
                    </div>
                    <div className="actions">
                        <button className="primary" disabled={busy || include.size === 0} onClick={commit}>
                            {plural(include.size, "Rechnung", "Rechnungen")} importieren
                        </button>
                        <button onClick={() => setPreview(null)}>Verwerfen</button>
                    </div>
                </>
            )}
        </>
    );
}
