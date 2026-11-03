// Pieces the invoice views share: ticks, popovers for refund amount and
// Klärungsbedarf, the selection checkbox, a payer's column and the hints
// under an invoice.
import {useState} from "react";
import type {ClaimViewDTO, InvoiceRowDTO, MarkChangeDTO} from "../../api";
import {MarkBox} from "../../components/MarkBox";
import {Badge} from "../../components/ui";
import {date, eur, num, pct, plural, signClass, toInput, today} from "../../lib/format";
import {useDialog} from "../../lib/useDialog";
import {claimState, deadlineText, markChange, payerClass, refundedMark, selectLabel} from "./model";
import type {Pop, ViewProps} from "./model";

type Change = (ch: MarkChangeDTO) => Promise<void>;

/** Checkbox that selects an invoice for bulk actions. */
export function SelectBox({row, v}: {row: InvoiceRowDTO; v: Pick<ViewProps, "selected" | "toggleOne">}) {
    return <input type="checkbox" aria-label={selectLabel(row)} checked={v.selected.has(row.id)} onChange={() => v.toggleOne(row.id)}/>;
}

/** "bezahlt" with its date. */
export function PaidMark({row, change}: {row: InvoiceRowDTO; change: Change}) {
    return (
        <MarkBox label="bezahlt" mark={row.paid}
                 onToggle={on => change(markChange(row.id, "", "paid", on))}
                 onDate={d => change(markChange(row.id, "", "paid", true, {date: d}))}/>
    );
}

/** "eingereicht" of a claim with its date. */
export function SubmittedMark({row, claim, change}: {row: InvoiceRowDTO; claim: ClaimViewDTO; change: Change}) {
    return (
        <MarkBox label="eingereicht" mark={claim.submitted}
                 onToggle={on => change(markChange(row.id, claim.payerId, "submitted", on))}
                 onDate={d => change(markChange(row.id, claim.payerId, "submitted", true, {date: d}))}/>
    );
}

/** "erstattet" of a claim; ticking asks for the amount (toggleRefunded). */
export function RefundedMark({claim, onToggle}: {claim: ClaimViewDTO; onToggle: (on: boolean) => void}) {
    return <MarkBox label="erstattet" mark={refundedMark(claim)} onToggle={onToggle}/>;
}

/** Clarify flag button of a claim (⚑): on, suggested (IST differs from SOLL) or off. */
export function ClarifyFlag({claim: c, onClick}: {claim: ClaimViewDTO; onClick: () => void}) {
    return (
        <button type="button" className={`flag ${c.clarify.done ? "on" : c.suggestClarify ? "suggest" : ""}`}
                aria-pressed={c.clarify.done}
                aria-label={`Klärungsbedarf ${c.payerName}`}
                title={c.clarify.done ? `Klärungsbedarf seit ${date(c.clarify.date)}${c.clarify.note ? `: ${c.clarify.note}` : ""}`
                    : c.suggestClarify ? "IST weicht vom SOLL ab – Klärungsbedarf markieren?" : "Klärungsbedarf markieren"}
                onClick={onClick}>⚑</button>
    );
}

/** The popover of one claim if it is open (refund amount or Klärungsbedarf). */
export function ClaimPopover({row, claim, pop, setPop, change}: {
    row: InvoiceRowDTO;
    claim: ClaimViewDTO;
    pop: Pop | null;
    setPop: (p: Pop | null) => void;
    change: Change;
}) {
    if (pop?.rowId !== row.id || pop.payerId !== claim.payerId) return null;
    if (pop.kind === "refund") {
        return <RefundPopover claim={claim} onCancel={() => setPop(null)}
                              onSave={(amount, d) => change(markChange(row.id, claim.payerId, "refunded", true, {date: d, amount}))}/>;
    }
    return <ClarifyPopover onCancel={() => setPop(null)}
                           onSave={note => change(markChange(row.id, claim.payerId, "clarify", true, {note}))}/>;
}

function RefundPopover({claim, onSave, onCancel}: {claim: ClaimViewDTO; onSave: (amount: string, date: string) => void; onCancel: () => void}) {
    const open = num(claim.expected) - num(claim.actual);
    const [amount, setAmount] = useState(toInput((open > 0 ? open : num(claim.expected)).toFixed(2)));
    const [d, setD] = useState(today());
    const ref = useDialog<HTMLDivElement>(onCancel);
    return (
        <div ref={ref} className="popover" role="dialog" aria-label={`Erstattung ${claim.payerName}`} tabIndex={-1}>
            <div className="grid two">
                <div className="field">
                    <label htmlFor="refund-amount">Erstattet (€)</label>
                    <input id="refund-amount" value={amount} onChange={e => setAmount(e.target.value)}
                           onKeyDown={e => e.key === "Enter" && onSave(amount, d)}/>
                </div>
                <div className="field">
                    <label htmlFor="refund-date">am</label>
                    <input id="refund-date" type="date" value={d} onChange={e => setD(e.target.value)}/>
                </div>
            </div>
            <div className="hint small muted mt-1">SOLL {eur(claim.expected)}</div>
            <div className="actions mt-3">
                <span className="spacer"/>
                <button onClick={onCancel}>Abbrechen</button>
                <button className="primary" onClick={() => onSave(amount, d)}>Übernehmen</button>
            </div>
        </div>
    );
}

function ClarifyPopover({onSave, onCancel}: {onSave: (note: string) => void; onCancel: () => void}) {
    const [note, setNote] = useState("");
    const ref = useDialog<HTMLDivElement>(onCancel);
    return (
        <div ref={ref} className="popover" role="dialog" aria-label="Klärungsbedarf" tabIndex={-1}>
            <div className="field">
                <label htmlFor="clarify-note">Klärungsbedarf – Grund</label>
                <input id="clarify-note" value={note} placeholder="z. B. Steigerungssatz gekürzt"
                       onChange={e => setNote(e.target.value)} onKeyDown={e => e.key === "Enter" && onSave(note)}/>
            </div>
            <div className="actions mt-3">
                <span className="spacer"/>
                <button onClick={onCancel}>Abbrechen</button>
                <button className="primary" onClick={() => onSave(note)}>Markieren</button>
            </div>
        </div>
    );
}

/** A payer's part of an invoice (Detail card, expanded Kompakt line): status, SOLL, IST, difference, ticks. */
export function PayerColumn({row: r, claim: c, index, v}: {row: InvoiceRowDTO; claim: ClaimViewDTO; index: number; v: ViewProps}) {
    const state = claimState(c);
    const refunded = c.status === "refunded";
    const deadline = deadlineText(c);
    return (
        <div className="inv-payer rel">
            <div className="inv-payer-head">
                <span className={`payer-name ${payerClass(index)}`}>{c.payerName} <span className="muted">{pct(c.rate)}</span></span>
                <Badge kind={state.tone}>{state.label}</Badge>
            </div>
            <div className="inv-figures">
                <span><span className="fig-label">SOLL</span><span className="num">{eur(c.expected)}</span></span>
                <span><span className="fig-label">IST</span><span className="num">{refunded ? eur(c.actual) : "–"}</span></span>
                <span>
                    <span className="fig-label">Differenz</span>
                    <span className={`num ${refunded ? signClass(c.diff) : "muted"}`}>{refunded ? eur(c.diff) : "–"}</span>
                </span>
            </div>
            <div className="inv-marks">
                <SubmittedMark row={r} claim={c} change={v.change}/>
                <RefundedMark claim={c} onToggle={on => v.toggleRefunded(r, c, on)}/>
                <ClarifyFlag claim={c} onClick={() => v.toggleClarify(r, c)}/>
            </div>
            {(c.skip || deadline) && (
                <div className="sub">
                    {c.skip && "verzichtet "}
                    {deadline && <span className={c.deadlineDays < 0 ? "neg" : ""}>{deadline}</span>}
                </div>
            )}
            <ClaimPopover row={r} claim={c} pop={v.pop} setPop={v.setPop} change={v.change}/>
        </div>
    );
}

/** What is listed under an invoice: note, Klärung, hints, waiting long, documents, Bescheid. */
export function footerItems(r: InvoiceRowDTO): {text: string; tone?: string}[] {
    const out: {text: string; tone?: string}[] = [];
    if (r.note.trim()) out.push({text: `Notiz: ${r.note.trim()}`});
    for (const c of r.claims ?? []) {
        if (c.clarify.done) out.push({text: `Klärungsbedarf ${c.payerName}${c.clarify.note ? `: ${c.clarify.note}` : ""}`, tone: "neg"});
        else if (c.suggestClarify) out.push({text: `${c.payerName}: IST weicht vom SOLL ab`, tone: "warn-text"});
        if (c.longWait) out.push({text: `${c.payerName}: Erstattung seit ${Math.floor(c.waitingDays / 7)} Wochen offen`, tone: "warn-text"});
    }
    for (const w of r.warnings ?? []) out.push({text: `⚠ ${w.text}`, tone: "warn-text"});
    if (r.attachments > 0) out.push({text: plural(r.attachments, "Beleg", "Belege"), tone: "muted"});
    for (const c of r.claims ?? []) {
        if (c.fromDoc) out.push({text: `Bescheid ${c.payerName} verknüpft`, tone: "muted"});
    }
    return out;
}
