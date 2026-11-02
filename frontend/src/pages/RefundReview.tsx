import {useEffect, useRef, useState} from "react";
import {Svc} from "../api";
import type {PayerDTO, RefundLineDTO, RefundReviewDTO} from "../api";
import {useToast} from "../components/feedback";
import {Badge, Field, Notice} from "../components/ui";
import {errMsg, eur, num, parseAmount, plural} from "../lib/format";

let lineSeq = 0;

/**
 * ID for a line added here, assigned once so it stays the React key: the
 * backend keeps non-empty unique IDs and only numbers lines without one.
 */
function newLineId(): string {
    return `u${Date.now().toString(36)}${(++lineSeq).toString(36)}`;
}

/** "180.20" (from the backend) → "180,20". */
function german(v: string): string {
    return /^-?\d+\.\d{2}$/.test(v) ? v.replace(".", ",") : v;
}

/**
 * Amounts of a backend result in the German format for the inputs. Converted
 * once here, not while rendering: text the user typed comes back unchanged and
 * stays as typed (otherwise "1.23" on the way to "1.234,56" would turn into "1,23").
 */
function forForm(r: RefundReviewDTO, sent?: RefundReviewDTO): RefundReviewDTO {
    const keep = (v: string, typed: string | undefined) => (v === typed ? v : german(v));
    const before = new Map((sent?.lines ?? []).map(l => [l.id, l]));
    return {
        ...r,
        paidOut: keep(r.paidOut, sent?.paidOut),
        lines: (r.lines ?? []).map(l => {
            const b = before.get(l.id);
            return {...l, id: l.id || newLineId(), billed: keep(l.billed, b?.billed), eligible: keep(l.eligible, b?.eligible),
                refund: keep(l.refund, b?.refund)};
        }),
    };
}

const MATCH: Record<string, [string, "good" | "warn" | "bad" | undefined]> = {
    auto: ["sicher", "good"],
    suggest: ["Vorschlag – prüfen", "warn"],
    confirmed: ["gewählt", "good"],
    none: ["keine Rechnung", "bad"],
};

const COLUMNS = [["billed", "Rechnungsbetrag"], ["eligible", "erstattungsfähig"], ["refund", "Erstattung"]] as const;

const POSITIONS = [
    [0, "– nicht vorhanden –"], [1, "1. Betrag"], [2, "2. Betrag"], [3, "3. Betrag"], [4, "4. Betrag"], [-1, "letzter Betrag"],
] as const;

/** Amount as a number, 0 if the text is not an amount. */
const amount = (v: string) => parseAmount(v) || 0;

/**
 * Review of a Beihilfebescheid or PKV-Leistungsabrechnung: the recognised
 * lines, their matching invoices and the checksum. Nothing is booked before
 * "Erstattungen übernehmen".
 */
export default function RefundReview({inboxName, initial, payers, onDone, onDiscard}: {
    inboxName: string;
    initial: RefundReviewDTO;
    payers: PayerDTO[];
    onDone: () => void;
    onDiscard: () => void;
}) {
    const toast = useToast();
    const [r, setR] = useState<RefundReviewDTO>(() => forForm(initial));
    const [error, setError] = useState("");
    const [saveLayout, setSaveLayout] = useState(true);
    const [busy, setBusy] = useState(false);
    const timer = useRef<number>(undefined);
    const seq = useRef(0);

    useEffect(() => () => window.clearTimeout(timer.current), []);

    // Re-check (amounts, matches, checksum) shortly after every change.
    function update(next: RefundReviewDTO, now = false) {
        setR(next);
        window.clearTimeout(timer.current);
        const n = ++seq.current;
        timer.current = window.setTimeout(() => {
            Svc.ReviewRefund(next).then(
                res => {
                    if (n !== seq.current) return;
                    setR(forForm(res, next));
                    setError("");
                },
                e => n === seq.current && setError(errMsg(e)),
            );
        }, now ? 0 : 400);
    }

    const lines = r.lines ?? [];
    const setLine = (i: number, patch: Partial<RefundLineDTO>, now = false) =>
        update({...r, lines: lines.map((l, j) => (j === i ? {...l, ...patch} : l))}, now);
    const addLine = () => update({...r, lines: [...lines, {id: newLineId(), date: r.date, provider: "", amounts: [], billed: "", eligible: "",
        refund: "", edited: true, invoiceIds: [], match: "none", score: 0, candidates: []}]});
    const removeLine = (i: number) => update({...r, lines: lines.filter((_, j) => j !== i)}, true);
    const deductions = r.deductions ?? [];

    async function confirm() {
        setBusy(true);
        try {
            const res = await Svc.ConfirmRefund(inboxName, r, saveLayout);
            toast.show(`Erstattungen für ${plural(res.invoices, "Rechnung", "Rechnungen")} übernommen` +
                (res.aliases ? ` – ${plural(res.aliases, "Namensvariante", "Namensvarianten")} gelernt` : "") +
                (res.rebooked ? ". Dieser Bescheid war bereits gebucht – die frühere Buchung wurde ersetzt." : ""));
            // Booked anyway; these are side effects that did not work.
            for (const w of res.warnings ?? []) toast.warn(w);
            onDone();
        } catch (e) {
            setError(errMsg(e));
        } finally {
            setBusy(false);
        }
    }

    const assigned = lines.filter(l => (l.invoiceIds ?? []).length > 0).length;
    const unsure = lines.filter(l => l.match === "suggest").length;
    const dedTotal = deductions.reduce((s, d) => s + Math.abs(amount(d.amount)), 0);
    const paidOut = amount(r.paidOut);
    return (
        <div className="refund-review">
            {error && <Notice kind="bad"><span style={{whiteSpace: "pre-line"}}>{error}</span></Notice>}
            {(r.notes ?? []).map(n => <Notice key={n} kind="warn">{n}</Notice>)}
            <div className="grid">
                <Field label="Kostenträger" htmlFor="rr-payer">
                    <select id="rr-payer" value={r.payerId} onChange={e => update({...r, payerId: e.target.value}, true)}>
                        {payers.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
                    </select>
                </Field>
                <Field label="Bescheid / Abrechnung vom" htmlFor="rr-date">
                    <input id="rr-date" type="date" value={r.date} onChange={e => update({...r, date: e.target.value})}/>
                </Field>
                <Field label="Antrag vom" htmlFor="rr-app" hint="Wird bei Rechnungen ohne Einreichungsdatum eingetragen">
                    <input id="rr-app" type="date" value={r.applicationDate} onChange={e => update({...r, applicationDate: e.target.value})}/>
                </Field>
                <Field label="Auszahlung (€)" htmlFor="rr-paid">
                    <input id="rr-paid" inputMode="decimal" value={r.paidOut} onChange={e => update({...r, paidOut: e.target.value})}/>
                </Field>
                <Field label="Ausgezahlt am" htmlFor="rr-paydate" hint="Leer = Datum des Bescheids">
                    <input id="rr-paydate" type="date" value={r.payoutDate} onChange={e => update({...r, payoutDate: e.target.value})}/>
                </Field>
            </div>

            <h3>Positionen</h3>
            <div className="row small" style={{marginBottom: 8}}>
                <span className="muted">Spalten:</span>
                {COLUMNS.map(([k, label]) => (
                    <label key={k} className="inline">
                        {label} =
                        <select aria-label={`Spalte ${label}`} style={{width: "auto"}} value={r.layout[k]}
                                onChange={e => update({...r, layout: {...r.layout, [k]: Number(e.target.value)},
                                    lines: lines.map(l => ({...l, edited: l.amounts?.length ? false : l.edited}))}, true)}>
                            {POSITIONS.filter(([v]) => k !== "refund" || v !== 0).map(([v, label]) => <option key={v} value={v}>{label}</option>)}
                        </select>
                    </label>
                ))}
            </div>
            <div className="table-wrap">
                <table className="refund-lines">
                    <thead>
                    <tr><th>Datum</th><th>Leistungserbringer</th><th className="num">Rechnungsbetrag</th><th className="num">Erstattung</th><th>Rechnung</th><th/></tr>
                    </thead>
                    <tbody>
                    {lines.map((l, i) => {
                        const [mlabel, mkind] = MATCH[l.match] ?? MATCH.none;
                        const chosen = (l.invoiceIds ?? [])[0] ?? "";
                        const candidates = l.candidates ?? [];
                        const others = (r.invoices ?? []).filter(o => !candidates.some(c => c.invoiceId === o.invoiceId));
                        return (
                            <tr key={l.id}>
                                <td><input type="date" aria-label={`Zeile ${i + 1}: Datum`} value={l.date} onChange={e => setLine(i, {date: e.target.value})}/></td>
                                <td><input aria-label={`Zeile ${i + 1}: Leistungserbringer`} value={l.provider} onChange={e => setLine(i, {provider: e.target.value})}/></td>
                                <td><input aria-label={`Zeile ${i + 1}: Rechnungsbetrag`} className="num" value={l.billed}
                                           onChange={e => setLine(i, {billed: e.target.value, edited: true})}/></td>
                                <td><input aria-label={`Zeile ${i + 1}: Erstattung`} className="num" value={l.refund}
                                           onChange={e => setLine(i, {refund: e.target.value, edited: true})}/></td>
                                <td>
                                    <div className="row" style={{flexWrap: "nowrap", gap: 6}}>
                                        <select aria-label={`Zeile ${i + 1}: Rechnung`} value={(l.invoiceIds ?? []).length > 1 ? "__group" : chosen}
                                                onChange={e => setLine(i, e.target.value
                                                    ? {invoiceIds: [e.target.value], match: "confirmed"}
                                                    : {invoiceIds: [], match: "confirmed"}, true)}>
                                            <option value="">– keine –</option>
                                            {(l.invoiceIds ?? []).length > 1 && <option value="__group">{l.invoiceIds?.length} Rechnungen zusammen</option>}
                                            {candidates.length > 0 && (
                                                <optgroup label="Passend">
                                                    {candidates.map(c => <option key={c.invoiceId} value={c.invoiceId}>{c.label}</option>)}
                                                </optgroup>
                                            )}
                                            {others.length > 0 && (
                                                <optgroup label="Weitere offene Rechnungen">
                                                    {others.map(c => <option key={c.invoiceId} value={c.invoiceId}>{c.label}</option>)}
                                                </optgroup>
                                            )}
                                        </select>
                                        <Badge kind={mkind} title={candidates.find(c => c.invoiceId === chosen)?.reasons?.join(", ")}>{mlabel}</Badge>
                                    </div>
                                </td>
                                <td><button className="link" aria-label={`Zeile ${i + 1} entfernen`} onClick={() => removeLine(i)}>✕</button></td>
                            </tr>
                        );
                    })}
                    </tbody>
                </table>
            </div>
            <div className="row" style={{marginTop: 8}}>
                <button className="link" onClick={addLine}>+ Zeile</button>
                {deductions.map((d, i) => (
                    <span key={i} className="small muted">{d.label}: {eur(Math.abs(amount(d.amount)))}
                        <button className="link" aria-label={`Abzug ${d.label} entfernen`} onClick={() => update({...r, deductions: deductions.filter((_, j) => j !== i)}, true)}> ✕</button>
                    </span>
                ))}
            </div>
            <Notice kind={r.checksumOk ? "good" : paidOut ? "warn" : undefined}>
                Positionen {eur(r.linesTotal)}{dedTotal ? ` − Abzüge ${eur(dedTotal)}` : ""} = {eur(num(r.linesTotal) - dedTotal)}
                {paidOut ? (r.checksumOk ? ` ✓ entspricht der Auszahlung` : ` – Auszahlung laut Beleg ${eur(paidOut)}, Differenz ${eur(r.checksumDiff)}: fehlt eine Zeile oder wurde ein Betrag falsch gelesen?`) : " (Auszahlungsbetrag nicht erkannt)"}
            </Notice>
            <label className="inline" style={{marginTop: 8}}>
                <input type="checkbox" checked={saveLayout} onChange={e => setSaveLayout(e.target.checked)}/>
                Spaltenaufteilung für {payers.find(p => p.id === r.payerId)?.name ?? "diesen Kostenträger"} merken
            </label>
            <div className="actions">
                <button className="primary" disabled={busy || assigned === 0} onClick={confirm}>
                    Erstattungen übernehmen ({assigned} von {lines.length})
                </button>
                <button className="danger" onClick={onDiscard}>Verwerfen</button>
                {unsure > 0 && <span className="muted small">{plural(unsure, "Vorschlag", "Vorschläge")} bitte prüfen</span>}
            </div>
        </div>
    );
}
