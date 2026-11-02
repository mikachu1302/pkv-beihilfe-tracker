import {useEffect, useRef, useState} from "react";
import {useNavigate, useParams} from "react-router";
import {Desktop, Svc} from "../api";
import type {ClaimDTO, ClaimViewDTO, InvoiceDTO, InvoiceRowDTO, MarkDTO, RefundDTO} from "../api";
import {useApp} from "../App";
import {useConfirm, useToast} from "../components/feedback";
import {Badge, ErrorNotice, Field, Loading, Notice} from "../components/ui";
import {CATEGORY_LABELS, STATUS_LABELS, date, errMsg, eur, num, pct, toInput, today} from "../lib/format";

/** Amounts arrive as "286.40"; the form shows and sends "286,40". */
function forForm(inv: InvoiceDTO): InvoiceDTO {
    return {
        ...inv,
        amount: toInput(inv.amount === "0.00" ? "" : inv.amount),
        claims: (inv.claims ?? []).map(c => ({
            ...c,
            eligible: toInput(c.eligible),
            expected: toInput(c.expected),
            rate: c.rate.replace(".", ","),
            refunds: (c.refunds ?? []).map(r => ({...r, amount: toInput(r.amount)})),
        })),
    };
}

function emptyClaim(payerId: string): ClaimDTO {
    const m = {done: false, date: "", note: ""};
    return {payerId, eligible: "", rate: "", expected: "", submitted: m, refunded: m, clarify: m, held: false, skip: false,
        keepsPremiumRefund: false, submissionId: "", refunds: []};
}

const STATUS_KIND: Record<string, "good" | "warn" | undefined> = {refunded: "good", submitted: "warn"};

export default function InvoiceForm() {
    const {id} = useParams();
    const navigate = useNavigate();
    const toast = useToast();
    const confirm = useConfirm();
    const {setYear, dataVersion} = useApp();
    const [inv, setInv] = useState<InvoiceDTO | null>(null);
    // The invoice as loaded (or last saved): saving sends it along, so changes
    // another device made meanwhile are kept (only fields edited here win).
    const [original, setOriginal] = useState<InvoiceDTO | null>(null);
    const [changedElsewhere, setChangedElsewhere] = useState(false);
    const [view, setView] = useState<InvoiceRowDTO | null>(null);
    const [loadError, setLoadError] = useState("");
    const [inputError, setInputError] = useState("");
    const [saveError, setSaveError] = useState("");
    const [saving, setSaving] = useState(false);
    const timer = useRef<number>(undefined);
    const seq = useRef(0);

    useEffect(() => {
        setInv(null);
        setView(null);
        setLoadError("");
        setChangedElsewhere(false);
        (id ? Svc.GetInvoice(id) : Svc.NewInvoice()).then(
            d => {
                setInv(forForm(d.invoice));
                setOriginal(id ? d.invoice : null);
                setView(d.view);
            },
            e => setLoadError(errMsg(e)),
        );
    }, [id]);

    // Another device changed the data folder: tell the user if it was this invoice.
    useEffect(() => {
        if (!dataVersion || !id || !original) return;
        Svc.GetInvoice(id).then(
            d => setChangedElsewhere(JSON.stringify(d.invoice) !== JSON.stringify(original)),
            () => setChangedElsewhere(true),
        );
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [dataVersion]);

    // Recompute SOLL/IST shortly after every change, without touching the input.
    function update(next: InvoiceDTO) {
        setInv(next);
        window.clearTimeout(timer.current);
        const n = ++seq.current;
        timer.current = window.setTimeout(() => {
            Svc.PreviewInvoice(next).then(
                d => {
                    if (n !== seq.current) return;
                    setView(d.view);
                    setInputError("");
                    // Payers may change with the treatment date: add rows for new ones.
                    setInv(cur => {
                        if (!cur) return cur;
                        const missing = (d.view.claims ?? []).filter(c => !(cur.claims ?? []).some(x => x.payerId === c.payerId));
                        return missing.length ? {...cur, claims: [...(cur.claims ?? []), ...missing.map(c => emptyClaim(c.payerId))]} : cur;
                    });
                },
                e => n === seq.current && setInputError(errMsg(e)),
            );
        }, 250);
    }

    if (loadError) return <ErrorNotice error={loadError}/>;
    if (!inv || !view) return <Loading/>;

    const set = (patch: Partial<InvoiceDTO>) => update({...inv, ...patch});
    const setClaim = (payerId: string, patch: Partial<ClaimDTO>) =>
        update({...inv, claims: (inv.claims ?? []).map(c => (c.payerId === payerId ? {...c, ...patch} : c))});

    async function save(next: "list" | "new") {
        if (!inv) return;
        setSaving(true);
        setSaveError("");
        try {
            const d = await Svc.SaveInvoice(inv, original);
            toast.show("Rechnung gespeichert");
            setYear(d.view.year);
            if (next === "new") {
                if (id) navigate("/rechnung/neu");
                else {
                    const blank = await Svc.NewInvoice();
                    setInv(forForm(blank.invoice));
                    setOriginal(null);
                    setView(blank.view);
                }
            } else {
                navigate("/rechnungen");
            }
        } catch (e) {
            setSaveError(errMsg(e));
        } finally {
            setSaving(false);
        }
    }

    async function remove() {
        if (!id || !await confirm("Diese Rechnung löschen?", "Löschen")) return;
        try {
            await Svc.DeleteInvoice(id);
            toast.show("Rechnung gelöscht");
            navigate("/rechnungen");
        } catch (e) {
            toast.error(e);
        }
    }

    const viewClaims = view.claims ?? [];
    return (
        <>
            <div className="page-head">
                <h1>{id ? inv.provider || "Rechnung" : "Neue Rechnung"}</h1>
                {view.year > 0 && <Badge>Leistungsjahr {view.year}</Badge>}
            </div>
            {saveError && <ErrorNotice error={saveError}/>}
            {changedElsewhere && (
                <Notice kind="warn">
                    Diese Rechnung wurde inzwischen auf einem anderen Gerät geändert. Beim Speichern werden nur die hier
                    geänderten Felder übernommen, die übrigen Änderungen bleiben erhalten.
                </Notice>
            )}
            {inputError && <Notice kind="warn"><span style={{whiteSpace: "pre-line"}}>{inputError}</span></Notice>}

            <fieldset className="panel">
                <legend>Rechnung</legend>
                <div className="grid">
                    <Field label="Behandler *" htmlFor="provider">
                        <input id="provider" autoFocus={!id} value={inv.provider} onChange={e => set({provider: e.target.value})}/>
                    </Field>
                    <Field label="Kategorie" htmlFor="category">
                        <select id="category" value={inv.category} onChange={e => set({category: e.target.value})}>
                            <option value="">–</option>
                            {Object.entries(CATEGORY_LABELS).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
                        </select>
                    </Field>
                    <Field label="Betrag (€) *" htmlFor="amount">
                        <input id="amount" inputMode="decimal" value={inv.amount} placeholder="0,00" onChange={e => set({amount: e.target.value})}/>
                    </Field>
                    <Field label="Rechnungsdatum *" htmlFor="invoiceDate">
                        <input id="invoiceDate" type="date" value={inv.invoiceDate} onChange={e => set({invoiceDate: e.target.value})}/>
                    </Field>
                    <Field label="Behandlung von" htmlFor="serviceFrom" hint="Bestimmt Leistungsjahr und Beihilfe-Satz">
                        <input id="serviceFrom" type="date" value={inv.serviceFrom} onChange={e => set({serviceFrom: e.target.value})}/>
                    </Field>
                    <Field label="Behandlung bis" htmlFor="serviceTo">
                        <input id="serviceTo" type="date" value={inv.serviceTo} onChange={e => set({serviceTo: e.target.value})}/>
                    </Field>
                    <Field label="Rechnungsnummer" htmlFor="reference">
                        <input id="reference" value={inv.reference} onChange={e => set({reference: e.target.value})}/>
                    </Field>
                    <Field label="Zahlungsempfänger" htmlFor="payee" hint="z. B. Abrechnungsstelle">
                        <input id="payee" value={inv.payee} onChange={e => set({payee: e.target.value})}/>
                    </Field>
                    <Field label="IBAN" htmlFor="iban">
                        <input id="iban" value={inv.iban} onChange={e => set({iban: e.target.value})}/>
                    </Field>
                    <Field label="Fällig am" htmlFor="due">
                        <input id="due" type="date" value={inv.due} onChange={e => set({due: e.target.value})}/>
                    </Field>
                    <Field label="Leistungsjahr" htmlFor="serviceYear" hint="Leer = aus dem Behandlungsdatum">
                        <input id="serviceYear" type="number" min={1990} max={2100} value={inv.serviceYear || ""}
                               placeholder={view.year ? String(view.year) : ""}
                               onChange={e => set({serviceYear: Number(e.target.value) || 0})}/>
                    </Field>
                    <Field label="Bezahlt (an Behandler)">
                        <MarkField label="bezahlt" mark={inv.paid} onChange={paid => set({paid})}/>
                    </Field>
                    <Field label="Notiz" htmlFor="note" wide>
                        <textarea id="note" value={inv.note} onChange={e => set({note: e.target.value})}/>
                    </Field>
                    {(inv.attachments ?? []).length > 0 && (
                        <Field label="Belege" wide>
                            <div className="row">
                                {(inv.attachments ?? []).map((a, i) => (
                                    <button key={a} onClick={() => Svc.DocumentPath(a).then(p => Desktop.OpenPath(p)).catch(toast.error)}>
                                        Beleg {i + 1} öffnen
                                    </button>
                                ))}
                            </div>
                        </Field>
                    )}
                </div>
            </fieldset>

            <div className="claim-cards">
                {viewClaims.map(cv => {
                    const c = (inv.claims ?? []).find(x => x.payerId === cv.payerId) ?? emptyClaim(cv.payerId);
                    return <ClaimCard key={cv.payerId} claim={c} view={cv} onChange={patch => setClaim(cv.payerId, patch)}/>;
                })}
            </div>

            {(view.warnings ?? []).length > 0 && (
                <Notice kind="warn">
                    Bitte prüfen:
                    <ul>{(view.warnings ?? []).map(w => <li key={w.code + w.text}>{w.text}</li>)}</ul>
                </Notice>
            )}

            <div className="panel row">
                <span>Gesamt SOLL <strong>{eur(view.expected)}</strong></span>
                <span>IST <strong>{eur(view.actual)}</strong></span>
                <span>Eigenanteil <strong>{eur(view.ownShare)}</strong></span>
            </div>

            <div className="actions">
                <button className="primary" disabled={saving} onClick={() => save("list")}>Speichern</button>
                <button disabled={saving} onClick={() => save("new")}>Speichern &amp; nächste</button>
                <button onClick={() => navigate(-1)}>Abbrechen</button>
                <span className="spacer"/>
                {id && <button className="danger" onClick={remove}>Löschen</button>}
            </div>
        </>
    );
}

function MarkField({label, mark, onChange, withNote}: {
    label: string;
    mark: MarkDTO;
    onChange: (m: MarkDTO) => void;
    withNote?: boolean;
}) {
    return (
        <div className="row" style={{gap: 8}}>
            <label className="inline">
                <input type="checkbox" checked={mark.done}
                       onChange={e => onChange({...mark, done: e.target.checked, date: e.target.checked ? (mark.date || today()) : mark.date})}/>
                {label}
            </label>
            {mark.done && (
                <input type="date" aria-label={`${label}: Datum`} style={{width: 150}} value={mark.date}
                       onChange={e => onChange({...mark, date: e.target.value})}/>
            )}
            {withNote && mark.done && (
                <input aria-label={`${label}: Grund`} placeholder="Grund" style={{flex: 1, minWidth: 140}} value={mark.note}
                       onChange={e => onChange({...mark, note: e.target.value})}/>
            )}
        </div>
    );
}

function ClaimCard({claim: c, view: v, onChange}: {claim: ClaimDTO; view: ClaimViewDTO; onChange: (p: Partial<ClaimDTO>) => void}) {
    const toast = useToast();
    const refunds = c.refunds ?? [];
    const setRefund = (i: number, patch: Partial<RefundDTO>) =>
        onChange({refunds: refunds.map((r, j) => (j === i ? {...r, ...patch} : r))});
    const addRefund = () => {
        const open = num(v.expected) - num(v.actual);
        onChange({
            refunds: [...refunds, {id: "", amount: open > 0 ? toInput(open.toFixed(2)) : "", date: today(), docId: "", lineId: "", note: ""}],
            refunded: c.refunded.done ? c.refunded : {...c.refunded, done: true, date: today()},
        });
    };
    const removeRefund = (i: number) => {
        const rest = refunds.filter((_, j) => j !== i);
        onChange({refunds: rest, ...(rest.length === 0 ? {refunded: {...c.refunded, done: false}} : {})});
    };
    const diff = num(v.diff);
    return (
        <section className="panel claim-card" aria-label={v.payerName}>
            <h3>{v.payerName} <Badge kind={STATUS_KIND[v.status]}>{STATUS_LABELS[v.status] ?? v.status}</Badge>
                {v.clarify.done && <Badge kind="warn">Klärungsbedarf</Badge>}</h3>
            <dl className="kv">
                <dt>Satz</dt><dd>{pct(v.rate)}</dd>
                <dt>Erstattungsfähig</dt><dd>{eur(v.eligible)}</dd>
                <dt>SOLL</dt><dd><strong>{eur(v.expected)}</strong></dd>
                <dt>IST</dt><dd><strong>{v.status === "refunded" ? eur(v.actual) : "–"}</strong></dd>
                {v.status === "refunded" && <><dt>Differenz</dt><dd className={diff < 0 ? "neg" : diff > 0 ? "pos" : ""}>{eur(v.diff)}</dd></>}
            </dl>
            {v.suggestClarify && <Notice kind="warn">IST weicht vom SOLL ab – Klärungsbedarf markieren?</Notice>}
            {v.deadline && <div className={`small ${v.deadlineDays < 0 ? "neg" : "muted"}`} style={{marginTop: 6}}>
                Antragsfrist {v.deadlineDays < 0 ? "abgelaufen am" : "bis"} {date(v.deadline)}
            </div>}

            <div className="checks">
                <MarkField label="eingereicht" mark={c.submitted} onChange={submitted => onChange({submitted})}/>
                <MarkField label="erstattet" mark={c.refunded} onChange={refunded => onChange({refunded})}/>
                <MarkField label="Klärungsbedarf" mark={c.clarify} withNote onChange={clarify => onChange({clarify})}/>
                <label className="inline"><input type="checkbox" checked={c.held} onChange={e => onChange({held: e.target.checked})}/>zurückgehalten</label>
                <label className="inline"><input type="checkbox" checked={c.skip} onChange={e => onChange({skip: e.target.checked})}/>wird nicht eingereicht</label>
                {v.kind === "pkv" && (
                    <label className="inline" title="z. B. Vorsorge – je nach Tarif">
                        <input type="checkbox" checked={c.keepsPremiumRefund} onChange={e => onChange({keepsPremiumRefund: e.target.checked})}/>
                        kostet keine Beitragsrückerstattung
                    </label>
                )}
            </div>

            <h3>Erstattungen</h3>
            {refunds.length > 0 && (
                <table className="refunds">
                    <thead><tr><th>Betrag (€)</th><th>Datum</th><th>Notiz</th><th/></tr></thead>
                    <tbody>
                    {refunds.map((r, i) => (
                        <tr key={r.id || `new-${i}`}>
                            <td><input aria-label="Erstattungsbetrag" inputMode="decimal" value={r.amount} onChange={e => setRefund(i, {amount: e.target.value})}/></td>
                            <td><input aria-label="Erstattungsdatum" type="date" value={r.date} onChange={e => setRefund(i, {date: e.target.value})}/></td>
                            <td>
                                {r.docId
                                    ? <button className="link" onClick={() => Svc.RefundDocPath(r.docId).then(p => Desktop.OpenPath(p)).catch(toast.error)}>aus Bescheid – öffnen</button>
                                    : <input aria-label="Erstattungsnotiz" value={r.note} onChange={e => setRefund(i, {note: e.target.value})}/>}
                            </td>
                            <td><button className="link" aria-label="Erstattung entfernen" onClick={() => removeRefund(i)}>✕</button></td>
                        </tr>
                    ))}
                    </tbody>
                </table>
            )}
            <button className="link" onClick={addRefund}>+ Erstattung erfassen</button>

            <details style={{marginTop: 10}}>
                <summary>Abweichende Werte</summary>
                <div className="grid" style={{marginTop: 8}}>
                    <Field label="Satz (%)" hint="Leer = aus dem Versicherungsschutz">
                        <input aria-label="Satz" value={c.rate} placeholder={v.rate} onChange={e => onChange({rate: e.target.value})}/>
                    </Field>
                    <Field label="Erstattungsfähig (€)" hint="Leer = Rechnungsbetrag">
                        <input aria-label="Erstattungsfähig" value={c.eligible} onChange={e => onChange({eligible: e.target.value})}/>
                    </Field>
                    <Field label="SOLL (€)" hint="Leer = berechnet">
                        <input aria-label="SOLL" value={c.expected} onChange={e => onChange({expected: e.target.value})}/>
                    </Field>
                </div>
            </details>
        </section>
    );
}
