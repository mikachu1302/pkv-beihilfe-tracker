// "Klassische Liste": the invoice table up to v0.3 – the only list of the
// classic design, and one of the views of the new design.
import {Link} from "react-router";
import type {ClaimViewDTO, InvoiceRowDTO} from "../../api";
import {Badge} from "../../components/ui";
import {CATEGORY_LABELS, date, eur, num, plural, sumBy} from "../../lib/format";
import {invoicePath} from "../../lib/paths";
import {claimOf, deadlineText, opensInvoice, providerName} from "./model";
import type {ViewProps} from "./model";
import {ClaimPopover, ClarifyFlag, PaidMark, RefundedMark, SelectBox, SubmittedMark} from "./parts";

export function ClassicList(v: ViewProps) {
    const {rows, payers} = v;
    const claimSum = (payerId: string, field: "expected" | "actual") => sumBy(rows, r => claimOf(r, payerId)?.[field]);

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
                    <tr key={r.id} className={`clickable ${v.selected.has(r.id) ? "sel" : ""}`}
                        onClick={e => openFromRow(e, r.id)}>
                        <td><SelectBox row={r} v={v}/></td>
                        <td>
                            {date(r.serviceDate)}
                            {r.invoiceDate !== r.serviceDate && <div className="sub">Rechnung {date(r.invoiceDate)}</div>}
                        </td>
                        <td>
                            <Link className="provider-link" to={invoicePath(r.id)}>{providerName(r)}</Link>
                            {" "}<Link className="edit-link" to={invoicePath(r.id)}
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
                        <td><PaidMark row={r} change={v.change}/></td>
                        {payers.map(p => {
                            const c = claimOf(r, p.id);
                            if (!c) return <td key={p.id} colSpan={2} className="payer-first muted small">–</td>;
                            return <ClaimCells key={p.id} row={r} claim={c} v={v}/>;
                        })}
                        <td className="num">{eur(r.ownShare)}</td>
                    </tr>
                ))}
                </tbody>
                <tfoot>
                <tr>
                    <td/>
                    <td colSpan={2}>{plural(rows.length, "Rechnung", "Rechnungen")}</td>
                    <td className="num">{eur(sumBy(rows, r => r.amount))}</td>
                    <td/>
                    {payers.map(p => (
                        <td key={p.id} className="amounts payer-first" colSpan={2}>
                            SOLL {eur(claimSum(p.id, "expected"))} · IST {eur(claimSum(p.id, "actual"))}
                        </td>
                    ))}
                    <td className="num">{eur(sumBy(rows, r => r.ownShare))}</td>
                </tr>
                </tfoot>
            </table>
        </div>
    );
}

function ClaimCells({row, claim: c, v}: {row: InvoiceRowDTO; claim: ClaimViewDTO; v: ViewProps}) {
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
                        <SubmittedMark row={row} claim={c} change={v.change}/>
                        <ClarifyFlag claim={c} onClick={() => v.toggleClarify(row, c)}/>
                    </span>
                    <RefundedMark claim={c} onToggle={on => v.toggleRefunded(row, c, on)}/>
                    <span className="sub">
                        {c.held && "zurückgehalten "}
                        {c.skip && "verzichtet "}
                        {deadline && <span className={c.deadlineDays < 0 ? "neg" : ""}>{deadline}</span>}
                    </span>
                </div>
                <ClaimPopover row={row} claim={c} pop={v.pop} setPop={v.setPop} change={v.change}/>
            </td>
        </>
    );
}
