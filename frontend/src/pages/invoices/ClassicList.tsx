// "Klassische Liste": the invoice table up to v0.3 – the only list of the
// classic design, and one of the views of the new design.
import {Link} from "react-router";
import type {ClaimViewDTO, InvoiceRowDTO, MarkChangeDTO} from "../../api";
import {MarkBox} from "../../components/MarkBox";
import {Badge} from "../../components/ui";
import {CATEGORY_LABELS, date, eur, num, plural} from "../../lib/format";
import {ClaimPopover, ClarifyFlag, claimOf, deadlineText, markBase, opensInvoice} from "./shared";
import type {Pop, ViewProps} from "./shared";

export function ClassicList(v: ViewProps) {
    const {rows, payers, selected} = v;
    const sum = (pick: (r: InvoiceRowDTO) => string) => rows.reduce((s, r) => s + num(pick(r)), 0);
    const claimSum = (payerId: string, field: "expected" | "actual") =>
        rows.reduce((s, r) => s + num(claimOf(r, payerId)?.[field]), 0);

    /** Opens the invoice when a row is clicked anywhere but on its checkboxes, buttons or links. */
    function openFromRow(e: React.MouseEvent, id: string) {
        const target = e.target as HTMLElement;
        if (target.closest("td:first-child") || !opensInvoice(target)) return;
        v.open(id);
    }

    return (
        <div className="table-wrap">
            <table className="invoices">
                <thead>
                <tr>
                    <th className="check-col"><input type="checkbox" aria-label="Alle auswählen" checked={v.allSelected} onChange={v.toggleAll}/></th>
                    <th>Behandlung</th>
                    <th>Behandler</th>
                    <th className="num">Betrag</th>
                    <th>Bezahlt</th>
                    {payers.map(p => <th key={p.id} className="payer" colSpan={2}>{p.name}</th>)}
                    <th className="num" title="Rechnungsbetrag minus Erstattungen">Nicht erstattet</th>
                </tr>
                </thead>
                <tbody>
                {rows.map(r => (
                    <tr key={r.id} className={`clickable ${selected.has(r.id) ? "sel" : ""}`}
                        onClick={e => openFromRow(e, r.id)}>
                        <td><input type="checkbox" aria-label={`${r.provider} auswählen`} checked={selected.has(r.id)} onChange={() => v.toggleOne(r.id)}/></td>
                        <td>
                            {date(r.serviceDate)}
                            {r.invoiceDate !== r.serviceDate && <div className="sub">Rechnung {date(r.invoiceDate)}</div>}
                        </td>
                        <td>
                            <Link className="provider-link" to={`/rechnung/${encodeURIComponent(r.id)}`}>{r.provider || "(ohne Namen)"}</Link>
                            {" "}<Link className="edit-link" to={`/rechnung/${encodeURIComponent(r.id)}`}
                                       aria-label={`${r.provider || "Rechnung"} bearbeiten`} title="Rechnung öffnen und bearbeiten">✎</Link>
                            {(r.warnings ?? []).length > 0 && (
                                <> <Badge kind="warn" title={(r.warnings ?? []).map(w => w.text).join("\n")}>⚠ {(r.warnings ?? []).length}</Badge></>
                            )}
                            {r.note.trim() !== "" && (
                                <> <span className="badge note-badge" title={r.note} aria-label={`Notiz: ${r.note}`}>Notiz</span></>
                            )}
                            <div className="sub">{[CATEGORY_LABELS[r.category], r.reference].filter(Boolean).join(" · ")}</div>
                        </td>
                        <td className="num">{eur(r.amount)}</td>
                        <td>
                            <MarkBox label="bezahlt" mark={r.paid}
                                     onToggle={on => v.change({invoiceId: r.id, payerId: "", mark: "paid", on, date: "", note: "", amount: ""})}
                                     onDate={d => v.change({invoiceId: r.id, payerId: "", mark: "paid", on: true, date: d, note: "", amount: ""})}/>
                        </td>
                        {payers.map(p => {
                            const c = claimOf(r, p.id);
                            if (!c) return <td key={p.id} colSpan={2} className="payer-first muted small">–</td>;
                            return (
                                <ClaimCells key={p.id} row={r} claim={c} pop={v.pop} setPop={v.setPop} change={v.change}
                                            toggleRefunded={on => v.toggleRefunded(r, c, on)}
                                            toggleClarify={() => v.toggleClarify(r, c)}/>
                            );
                        })}
                        <td className="num">{eur(r.ownShare)}</td>
                    </tr>
                ))}
                </tbody>
                <tfoot>
                <tr>
                    <td/>
                    <td colSpan={2}>{plural(rows.length, "Rechnung", "Rechnungen")}</td>
                    <td className="num">{eur(sum(r => r.amount))}</td>
                    <td/>
                    {payers.map(p => (
                        <td key={p.id} className="amounts payer-first" colSpan={2}>
                            SOLL {eur(claimSum(p.id, "expected"))} · IST {eur(claimSum(p.id, "actual"))}
                        </td>
                    ))}
                    <td className="num">{eur(sum(r => r.ownShare))}</td>
                </tr>
                </tfoot>
            </table>
        </div>
    );
}

function ClaimCells({row, claim: c, pop, setPop, change, toggleRefunded, toggleClarify}: {
    row: InvoiceRowDTO;
    claim: ClaimViewDTO;
    pop: Pop | null;
    setPop: (p: Pop | null) => void;
    change: (ch: MarkChangeDTO) => Promise<void>;
    toggleRefunded: (on: boolean) => void;
    toggleClarify: () => void;
}) {
    const base = markBase(row, c);
    const diff = num(c.diff);
    const deadline = deadlineText(c);
    return (
        <>
            <td className="amounts payer-first" title={`Satz ${c.rate} %`}>
                <span className="muted small">SOLL</span> {eur(c.expected)}
                <span className="ist">
                    <span className="muted small">IST</span> {c.status === "refunded" ? eur(c.actual) : "–"}
                    {c.status === "refunded" && diff !== 0 && <span className={`small ${diff < 0 ? "neg" : "pos"}`}> ({eur(c.diff)})</span>}
                </span>
            </td>
            <td className="rel">
                <div className="markstack">
                    <span className="markline">
                        <MarkBox label="eingereicht" mark={c.submitted}
                                 onToggle={on => change({...base, mark: "submitted", on, date: ""})}
                                 onDate={d => change({...base, mark: "submitted", on: true, date: d})}/>
                        <ClarifyFlag claim={c} onClick={toggleClarify}/>
                    </span>
                    <MarkBox label="erstattet" mark={c.refunded.done || num(c.actual) !== 0 ? {...c.refunded, done: true} : c.refunded}
                             onToggle={toggleRefunded}/>
                    <span className="sub">
                        {c.held && "zurückgehalten "}
                        {c.skip && "verzichtet "}
                        {deadline && <span className={c.deadlineDays < 0 ? "neg" : ""}>{deadline}</span>}
                    </span>
                </div>
                <ClaimPopover row={row} claim={c} pop={pop} setPop={setPop} change={change}/>
            </td>
        </>
    );
}
