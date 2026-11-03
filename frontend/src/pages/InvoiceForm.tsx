import {useEffect, useId, useRef, useState} from "react";
import type {ReactNode} from "react";
import {useNavigate, useParams} from "react-router";
import {Desktop, Svc} from "../api";
import type {ClaimDTO, ClaimViewDTO, InvoiceDTO, InvoiceRowDTO, LineDTO, MarkDTO, RefundDTO, StagedDocDTO} from "../api";
import {useApp} from "../App";
import {AIReadBar, AlternativeHint} from "../components/AIRead";
import {DocThumb} from "../components/DocThumb";
import {useConfirm, useToast} from "../components/feedback";
import {Badge, ErrorNotice, Field, Loading, Notice, SourceBadge} from "../components/ui";
import {useAIOn} from "../lib/ai";
import {readingSummary, useAIRead} from "../lib/aiRead";
import {recognizePages} from "../lib/capture";
import {bySize, docType, docUrl, readAsDataURL, tooBigText, useFileDrop} from "../lib/files";
import {CATEGORY_LABELS, STATUS_LABELS, date, errMsg, eur, num, pct, plural, toInput, today} from "../lib/format";
import {alternativeInput, applyReading, fillEmpty} from "../lib/prefill";
import type {Alternative} from "../lib/prefill";
import {dialogOpen, key, mod, useKeydown, useSaveShortcut, useUnsavedChanges} from "../lib/shortcuts";

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

/**
 * The form as compared for unsaved changes: unticked marks forget their date
 * and note, empty new refund rows do not count (ticking and unticking again
 * is no change).
 */
function snapshot(inv: InvoiceDTO): string {
    const mark = (m: MarkDTO): MarkDTO => (m.done ? m : {done: false, date: "", note: ""});
    return JSON.stringify({
        ...inv,
        paid: mark(inv.paid),
        claims: (inv.claims ?? []).map(c => ({
            ...c, submitted: mark(c.submitted), refunded: mark(c.refunded), clarify: mark(c.clarify),
            refunds: (c.refunds ?? []).filter(r => r.id !== "" || r.amount.trim() !== ""),
        })),
    });
}

function emptyClaim(payerId: string): ClaimDTO {
    const m = {done: false, date: "", note: ""};
    return {payerId, eligible: "", rate: "", expected: "", submitted: m, refunded: m, clarify: m, held: false, skip: false,
        keepsPremiumRefund: false, submissionId: "", refunds: []};
}

const STATUS_KIND: Record<string, "good" | "warn" | undefined> = {refunded: "good", submitted: "warn"};

/** Natural order of file names: "Seite 2" before "Seite 10". */
const byName = new Intl.Collator("de", {numeric: true, sensitivity: "base"});
const baseName = (path: string) => path.slice(Math.max(path.lastIndexOf("/"), path.lastIndexOf("\\")) + 1);

/** State of reading the documents added to a new invoice. */
interface Recognition {
    running: boolean;
    status: string;
    error: string;
    notes: string[];
    refund: boolean; // a Bescheid: booked in the inbox, not here
}

const NO_RECOGNITION: Recognition = {running: false, status: "", error: "", notes: [], refund: false};

/** The added documents as the recognition takes them (pages in order). */
const stagedPages = (docs: StagedDocDTO[]) => docs.map(d => ({url: d.url, type: d.type, modified: d.id, size: d.size}));

const fromAI = (source: string) => source === "ai" || source === "ai-confirmed";

/** Reading with the AI in the form: other readings of fields, what it did, what went wrong. */
interface AIState {
    others: Record<string, Alternative>;
    done: string;
    error: string;
}

const NO_AI: AIState = {others: {}, done: "", error: ""};

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
    // The form as loaded: differences are unsaved changes (asked before leaving).
    const [initial, setInitial] = useState("");
    const timer = useRef<number>(undefined);
    const seq = useRef(0);
    // Documents added in the form: they move to belege/ when it is saved.
    const [staged, setStaged] = useState<StagedDocDTO[]>([]);
    const stagedRef = useRef(staged);
    stagedRef.current = staged;
    // Fields filled from a document → where the value came from (badge until
    // edited). Such fields may be filled again when more pages are added.
    const [filled, setFilled] = useState<Record<string, string>>({});
    const filledRef = useRef(filled);
    filledRef.current = filled;
    // Counts the forms shown by this component (another invoice, "Speichern &
    // nächste", leaving): late results of an earlier form are dropped.
    const formGen = useRef(0);
    // Documents still being copied into the session folder: saving waits for them.
    const [staging, setStaging] = useState(0);
    const savingRef = useRef(false);
    const [recog, setRecog] = useState<Recognition>(NO_RECOGNITION);
    const recogRun = useRef<AbortController | null>(null);
    // The text the recognition found in the added documents (the AI reading checks against it).
    const recognized = useRef<{lines: LineDTO[]; qr: string[]} | null>(null);
    const aiOn = useAIOn();
    const reading = useAIRead();
    const readingRef = useRef(reading); // for the clean-up when the form is left
    readingRef.current = reading;
    const [aiState, setAIState] = useState<AIState>(NO_AI);
    const invRef = useRef(inv);
    invRef.current = inv;
    // A new invoice as it started (today as invoice date …): such values count as empty for prefilling.
    const blankRef = useRef<InvoiceDTO | null>(null);
    const {dragging, handlers: dropHandlers} = useFileDrop(files => void addDropped(files));

    const dirty = (inv !== null && initial !== "" && snapshot(inv) !== initial) || staged.length > 0;
    useUnsavedChanges(dirty);
    useSaveShortcut(() => void save("list"), inv !== null);
    useKeydown(e => {
        if (!mod(e) || e.key !== "Enter" || dialogOpen()) return false;
        if (!e.repeat) void save("new");
        return true;
    }, inv !== null);
    // A preview still pending must not land in another invoice's form.
    const cancelPreview = () => {
        window.clearTimeout(timer.current);
        seq.current++;
    };
    useEffect(() => () => window.clearTimeout(timer.current), []);

    // Documents of a form that is left without saving are discarded.
    const dropStaged = () => {
        formGen.current++;
        recogRun.current?.abort();
        readingRef.current.cancel();
        recognized.current = null;
        const ids = stagedRef.current.map(d => d.id);
        if (ids.length) Svc.DiscardStaged(ids).catch(() => {});
        setStaged([]);
        setFilled({});
        setRecog(NO_RECOGNITION);
        setAIState(NO_AI);
    };
    useEffect(() => () => dropStaged(), []);

    useEffect(() => {
        cancelPreview();
        dropStaged();
        setInv(null);
        setView(null);
        setLoadError("");
        setChangedElsewhere(false);
        (id ? Svc.GetInvoice(id) : Svc.NewInvoice()).then(
            d => {
                blankRef.current = id ? null : forForm(d.invoice);
                setInv(forForm(d.invoice));
                setInitial(snapshot(forForm(d.invoice)));
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

    /** Adds staged documents to the form they were added in (gen); a form left meanwhile discards them. */
    async function addStaged(docs: StagedDocDTO[], gen: number) {
        if (!docs.length) return;
        if (gen !== formGen.current) {
            Svc.DiscardStaged(docs.map(d => d.id)).catch(() => {});
            return;
        }
        const all = [...stagedRef.current, ...docs];
        stagedRef.current = all;
        setStaged(all);
        reading.cancel(); // it reads the pages as they were
        if (!id) await recognizeStaged(all);
    }

    /** Copies documents into the session folder; saving waits until it is done. */
    async function stage<T>(work: () => Promise<T>): Promise<T> {
        setStaging(n => n + 1);
        try {
            return await work();
        } finally {
            setStaging(n => n - 1);
        }
    }

    async function chooseFiles() {
        if (savingRef.current) return;
        const gen = formGen.current;
        try {
            const paths = await Desktop.ChooseFiles("Beleg hinzufügen", "PDF und Bilder", "*.pdf;*.jpg;*.jpeg;*.png;*.webp;*.heic");
            if (!paths?.length) return;
            // Pages in name order ("Seite 2" before "Seite 10"), whatever order the dialog returns.
            const sorted = [...paths].sort((a, b) => byName.compare(baseName(a), baseName(b)));
            await addStaged((await stage(() => Svc.StageFiles(sorted))) ?? [], gen);
        } catch (e) {
            toast.error(e);
        }
    }

    async function addDropped(files: File[]) {
        if (savingRef.current) return;
        const gen = formGen.current;
        const {fitting, tooBig} = bySize(files);
        if (tooBig.length) toast.show(tooBigText(tooBig), true);
        const docs: StagedDocDTO[] = [];
        await stage(async () => {
            try {
                for (const f of [...fitting].sort((a, b) => byName.compare(a.name, b.name))) {
                    docs.push(await Svc.StageFile(f.name, await readAsDataURL(f)));
                }
            } catch (e) {
                toast.error(e);
            }
        });
        await addStaged(docs, gen);
    }

    /** Reads the added documents (as pages of one) and fills the empty fields of a new invoice. */
    async function recognizeStaged(docs: StagedDocDTO[]) {
        const run = new AbortController();
        const previous = recogRun.current;
        recogRun.current = run;
        previous?.abort(); // after the new run joined: shared pages are not read again
        const {signal} = run;
        recognized.current = null;
        setRecog({...NO_RECOGNITION, running: true, status: "Beleg wird gelesen …"});
        try {
            const rec = await recognizePages(stagedPages(docs), m => !signal.aborted && setRecog(r => ({...r, status: m})), signal);
            if (signal.aborted) return;
            recognized.current = {lines: rec.lines, qr: rec.qr};
            const a = await Svc.AnalyzeDocument({lines: rec.lines, qr: rec.qr});
            if (signal.aborted || !invRef.current) return;
            if (a.kind === "refund") {
                setRecog({...NO_RECOGNITION, refund: true});
                return;
            }
            // Values from the AI stay: the text recognition of more pages does not know better.
            const replaceable = Object.entries(filledRef.current).filter(([, src]) => !fromAI(src)).map(([f]) => f);
            const {next, filled: got} = fillEmpty(invRef.current, a.draft.invoice, blankRef.current ?? undefined, replaceable);
            if (got.length) {
                setFilled(f => Object.fromEntries([...Object.entries(f), ...got.map(k => [k, a.sources?.[k] ?? "label"])]));
                update(next);
            }
            const notes = [...(a.notes ?? [])];
            if (rec.truncated) notes.push(`Nur die ersten ${rec.pages} Seiten wurden gelesen.`);
            const status = got.length
                ? `${plural(got.length, "Feld", "Felder")} aus dem Beleg übernommen${rec.ocr ? " (per Texterkennung – bitte prüfen)" : ""}.`
                : "Im Beleg wurde nichts für die leeren Felder gefunden.";
            setRecog({...NO_RECOGNITION, status, notes});
        } catch (e) {
            if (!signal.aborted) setRecog({...NO_RECOGNITION, error: errMsg(e)});
        }
    }

    async function stagedToInbox() {
        const ids = stagedRef.current.map(d => d.id);
        if (ids.length === 0) {
            setRecog(NO_RECOGNITION);
            return;
        }
        try {
            const name = await Svc.StagedToInbox(ids);
            stagedRef.current = [];
            setStaged([]);
            setRecog(NO_RECOGNITION);
            toast.show("Der Bescheid liegt jetzt im Posteingang");
            navigate("/posteingang", {state: {select: name}});
        } catch (e) {
            toast.error(e);
        }
    }

    /** Reads the added documents with the AI and takes what it found into the fields the user did not type. */
    async function readWithAI() {
        const gen = formGen.current;
        setAIState(NO_AI);
        try {
            const r = await reading.read(stagedPages(stagedRef.current), recognized.current ?? {lines: [], qr: []});
            if (!r || gen !== formGen.current || !invRef.current) return;
            if (r.kind === "refund") {
                setRecog({...NO_RECOGNITION, refund: true});
                return;
            }
            const before = filledRef.current;
            const {next, filled: got, others} = applyReading(invRef.current, r, blankRef.current ?? undefined, Object.keys(before));
            // Badges: fields filled now, and fields filled before whose value the AI read the same (e.g. "KI, bestätigt").
            const sources = {...before};
            for (const [f, src] of Object.entries(r.sources ?? {})) {
                if (src && (got.includes(f) || (f in before && r.draft.invoice[f as keyof InvoiceDTO]))) sources[f] = src;
            }
            setFilled(sources);
            if (got.length) update(next);
            setAIState({...NO_AI, others, done: readingSummary(r, got.length, Object.keys(others).length)});
        } catch (e) {
            if (gen === formGen.current) setAIState({...NO_AI, error: `${errMsg(e)} Die Werte der Texterkennung bleiben.`});
        }
    }

    function removeStaged(doc: StagedDocDTO) {
        Svc.DiscardStaged([doc.id]).catch(() => {});
        recogRun.current?.abort();
        reading.cancel();
        setAIState(NO_AI);
        const rest = stagedRef.current.filter(d => d.id !== doc.id);
        stagedRef.current = rest;
        setStaged(rest);
        setRecog(NO_RECOGNITION);
        // The remaining pages are read again (the removed one must not fill fields).
        if (!id && rest.length > 0) void recognizeStaged(rest);
    }

    const open = (p: Promise<string>) => p.then(path => Desktop.OpenPath(path)).catch(toast.error);

    if (loadError) return <ErrorNotice error={loadError}/>;
    if (!inv || !view) return <Loading/>;

    const set = (patch: Partial<InvoiceDTO>) => {
        if (Object.keys(patch).some(k => k in filled)) {
            setFilled(f => Object.fromEntries(Object.entries(f).filter(([k]) => !(k in patch))));
        }
        if (Object.keys(patch).some(k => k in aiState.others)) {
            setAIState(a => ({...a, others: Object.fromEntries(Object.entries(a.others).filter(([k]) => !(k in patch)))}));
        }
        update({...inv, ...patch});
    };
    /** The source badge of a prefilled field (else the usual hint) and another reading of it. */
    const hint = (field: keyof InvoiceDTO, normal?: ReactNode) => {
        const alt = aiState.others[field];
        const badge = filled[field] ? <SourceBadge source={filled[field]}/> : normal;
        if (!alt) return badge;
        return <>{badge}<AlternativeHint field={field} alt={alt} onTake={() => set({[field]: alternativeInput(field, alt)})}/></>;
    };
    const attachments = inv.attachments ?? [];
    const removed = (original?.attachments ?? []).filter(a => !attachments.includes(a));
    const setClaim = (payerId: string, patch: Partial<ClaimDTO>) =>
        update({...inv, claims: (inv.claims ?? []).map(c => (c.payerId === payerId ? {...c, ...patch} : c))});

    async function save(next: "list" | "new") {
        if (!inv || saving) return;
        if (staging > 0) {
            toast.show("Einen Moment – ein Beleg wird noch übernommen.");
            return;
        }
        cancelPreview();
        setSaving(true);
        savingRef.current = true;
        setSaveError("");
        try {
            const sent = stagedRef.current.map(s => s.id);
            const d = await Svc.SaveInvoice(inv, original, sent);
            // The saved form is done: a recognition still running must not fill the next one.
            recogRun.current?.abort();
            formGen.current++;
            stagedRef.current = stagedRef.current.filter(s => !sent.includes(s.id));
            setStaged(stagedRef.current);
            setFilled({});
            setRecog(NO_RECOGNITION);
            setAIState(NO_AI);
            if (d.warning) toast.show(d.warning, true);
            toast.show(d.movedToInbox > 0
                ? `Rechnung gespeichert – ${d.movedToInbox === 1 ? "der entfernte Beleg liegt" : `${d.movedToInbox} entfernte Belege liegen`} wieder im Posteingang`
                : "Rechnung gespeichert");
            setYear(d.view.year);
            if (next === "new") {
                if (id) navigate("/rechnung/neu");
                else {
                    const blank = await Svc.NewInvoice();
                    blankRef.current = forForm(blank.invoice);
                    setInv(forForm(blank.invoice));
                    setInitial(snapshot(forForm(blank.invoice)));
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
            savingRef.current = false;
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
        <div {...dropHandlers}>
            {dragging && <div className="dropzone">Dateien hier ablegen – sie werden als Belege dieser Rechnung übernommen</div>}
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
            {inputError && <Notice kind="warn"><span className="pre-line">{inputError}</span></Notice>}

            <fieldset className="panel">
                <legend>Rechnung</legend>
                <div className="grid">
                    <Field label="Behandler *" htmlFor="provider" hint={hint("provider")}>
                        <input id="provider" autoFocus={!id} value={inv.provider} onChange={e => set({provider: e.target.value})}/>
                    </Field>
                    <Field label="Kategorie" htmlFor="category" hint={hint("category")}>
                        <select id="category" value={inv.category} onChange={e => set({category: e.target.value})}>
                            <option value="">–</option>
                            {Object.entries(CATEGORY_LABELS).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
                        </select>
                    </Field>
                    <Field label="Betrag (€) *" htmlFor="amount" hint={hint("amount")}>
                        <input id="amount" inputMode="decimal" value={inv.amount} placeholder="0,00" onChange={e => set({amount: e.target.value})}/>
                    </Field>
                    <Field label="Rechnungsdatum *" htmlFor="invoiceDate" hint={hint("invoiceDate")}>
                        <input id="invoiceDate" type="date" value={inv.invoiceDate} onChange={e => set({invoiceDate: e.target.value})}/>
                    </Field>
                    <Field label="Behandlung von" htmlFor="serviceFrom" hint={hint("serviceFrom", "Bestimmt Leistungsjahr und Beihilfe-Satz")}>
                        <input id="serviceFrom" type="date" value={inv.serviceFrom} onChange={e => set({serviceFrom: e.target.value})}/>
                    </Field>
                    <Field label="Behandlung bis" htmlFor="serviceTo" hint={hint("serviceTo")}>
                        <input id="serviceTo" type="date" value={inv.serviceTo} onChange={e => set({serviceTo: e.target.value})}/>
                    </Field>
                    <Field label="Rechnungsnummer" htmlFor="reference" hint={hint("reference")}>
                        <input id="reference" value={inv.reference} onChange={e => set({reference: e.target.value})}/>
                    </Field>
                    <Field label="Zahlungsempfänger" htmlFor="payee" hint={hint("payee", "z. B. Abrechnungsstelle")}>
                        <input id="payee" value={inv.payee} onChange={e => set({payee: e.target.value})}/>
                    </Field>
                    <Field label="IBAN" htmlFor="iban" hint={hint("iban")}>
                        <input id="iban" value={inv.iban} onChange={e => set({iban: e.target.value})}/>
                    </Field>
                    <Field label="Fällig am" htmlFor="due" hint={hint("due")}>
                        <input id="due" type="date" value={inv.due} onChange={e => set({due: e.target.value})}/>
                    </Field>
                    <Field label="Leistungsjahr" htmlFor="serviceYear" hint="Leer = aus dem Behandlungsdatum">
                        <input id="serviceYear" type="number" min={1990} max={2100} value={inv.serviceYear || ""}
                               placeholder={view.year ? String(view.year) : ""}
                               onChange={e => set({serviceYear: Number(e.target.value) || 0})}/>
                    </Field>
                    <Field label="Bezahlt (an Behandler)" hint={hint("paid")}>
                        <MarkField label="bezahlt" mark={inv.paid} onChange={paid => set({paid})}/>
                    </Field>
                    <Field label="Notiz" htmlFor="note" wide>
                        <textarea id="note" value={inv.note} onChange={e => set({note: e.target.value})}/>
                    </Field>
                    <Field label="Belege" wide>
                        <div className="doc-tiles">
                            {attachments.map((a, i) => (
                                <DocTile key={a} url={docUrl(a)} type={docType(a)} label={`Seite ${i + 1}`}
                                         onOpen={() => open(Svc.DocumentPath(a))}
                                         onRemove={() => set({attachments: attachments.filter(x => x !== a)})}/>
                            ))}
                            {staged.map((d, i) => (
                                <DocTile key={d.id} url={d.url} type={d.type} label={`Seite ${attachments.length + i + 1}`} isNew title={d.name}
                                         onOpen={() => open(Promise.resolve(d.path))} onRemove={() => removeStaged(d)}/>
                            ))}
                            <button type="button" className="doc-add" onClick={chooseFiles} disabled={saving}>
                                {staging > 0 ? "wird übernommen …" : "+ Beleg hinzufügen …"}
                            </button>
                        </div>
                        {attachments.length > 1 && (
                            <button type="button" className="link" onClick={() => open(Svc.DocumentsPDF(attachments))}>Alle Seiten zusammen öffnen</button>
                        )}
                        <p className="muted small">
                            PDF oder Fotos hierher ziehen oder auswählen – mehrere Dateien werden Seiten desselben Belegs.
                            {staged.length > 0 && " Neue Seiten werden beim Speichern im Ordner „belege“ abgelegt."}
                            {removed.length > 0 && ` ${removed.length === 1 ? "Die entfernte Seite kommt" : "Die entfernten Seiten kommen"} beim Speichern zurück in den Posteingang.`}
                        </p>
                        {recog.status && <p className="muted small" role="status">{recog.status}</p>}
                        {recog.error && <Notice kind="warn">{recog.error} Die Felder lassen sich von Hand ausfüllen.</Notice>}
                        {aiOn && !id && staged.length > 0 && !recog.refund && (
                            <AIReadBar reading={reading} onRead={() => void readWithAI()} disabled={recog.running} done={aiState.done}/>
                        )}
                        {aiState.error && <Notice kind="warn">{aiState.error}</Notice>}
                        {recog.notes.map(n => <Notice key={n} kind={n.startsWith("Rezept erkannt –") ? undefined : "warn"}>{n}</Notice>)}
                        {recog.refund && (
                            <Notice kind="warn">
                                Das ist wohl ein Erstattungsbescheid – Bescheide werden im Posteingang den Rechnungen zugeordnet.{" "}
                                <button type="button" className="link" onClick={stagedToInbox}>In den Posteingang verschieben</button>
                            </Notice>
                        )}
                    </Field>
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
                <button className="primary" disabled={saving} onClick={() => save("list")} title={`Speichern (${key("S")})`}>Speichern</button>
                <button disabled={saving} onClick={() => save("new")} title={`Speichern und nächste Rechnung erfassen (${key("Enter")})`}>Speichern &amp; nächste</button>
                <button onClick={async () => {
                    if (dirty && !await confirm("Die Änderungen sind noch nicht gespeichert. Trotzdem verlassen?", "Verlassen")) return;
                    navigate(-1);
                }}>Abbrechen</button>
                <span className="spacer"/>
                {id && <button className="danger" onClick={remove}>Löschen</button>}
            </div>
        </div>
    );
}

/** A page of the invoice's documents: preview, open, remove. */
function DocTile({url, type, label, title, isNew, onOpen, onRemove}: {
    url: string;
    type: string;
    label: string;
    title?: string;
    isNew?: boolean;
    onOpen: () => void;
    onRemove: () => void;
}) {
    return (
        <div className="doc-tile" title={title}>
            <button type="button" className="doc-open" onClick={onOpen} aria-label={`${label} öffnen`}>
                <DocThumb url={url} type={type} label={label}/>
            </button>
            <div className="doc-meta">
                <span>{label}</span>
                {isNew && <Badge kind="good">neu</Badge>}
                <span className="spacer"/>
                <button type="button" className="link" onClick={onRemove} aria-label={`${label} entfernen`} title="Entfernen">✕</button>
            </div>
        </div>
    );
}

function MarkField({label, mark, onChange, withNote}: {
    label: string;
    mark: MarkDTO;
    onChange: (m: MarkDTO) => void;
    withNote?: boolean;
}) {
    return (
        <div className="row gap-2">
            <label className="inline">
                <input type="checkbox" checked={mark.done}
                       onChange={e => onChange({...mark, done: e.target.checked, date: e.target.checked ? (mark.date || today()) : mark.date})}/>
                {label}
            </label>
            {mark.done && (
                <input type="date" aria-label={`${label}: Datum`} className="w-date" value={mark.date}
                       onChange={e => onChange({...mark, date: e.target.value})}/>
            )}
            {withNote && mark.done && (
                <input aria-label={`${label}: Grund`} placeholder="Grund" className="grow-input" value={mark.note}
                       onChange={e => onChange({...mark, note: e.target.value})}/>
            )}
        </div>
    );
}

function ClaimCard({claim: c, view: v, onChange}: {claim: ClaimDTO; view: ClaimViewDTO; onChange: (p: Partial<ClaimDTO>) => void}) {
    const toast = useToast();
    const confirm = useConfirm();
    const id = useId();
    const refunds = c.refunds ?? [];
    const setRefund = (i: number, patch: Partial<RefundDTO>) =>
        onChange({refunds: refunds.map((r, j) => (j === i ? {...r, ...patch} : r))});
    const addRefund = (date = today()) => {
        const open = num(v.expected) - num(v.actual);
        onChange({
            refunds: [...refunds, {id: "", amount: open > 0 ? toInput(open.toFixed(2)) : "", date, docId: "", lineId: "", note: ""}],
            refunded: c.refunded.done ? c.refunded : {...c.refunded, done: true, date},
        });
    };
    // "erstattet" and the refunds belong together (as in the invoice list): a
    // claim with refunds counts as refunded, and ticking it without an amount
    // adds a refund prefilled with the open SOLL.
    const withAmount = refunds.filter(r => r.amount.trim() !== "" && num(r.amount) !== 0);
    const refundedMark: MarkDTO = refunds.length > 0 && !c.refunded.done
        ? {...c.refunded, done: true, date: c.refunded.date || latestDate(refunds)}
        : c.refunded;
    const setRefunded = async (m: MarkDTO) => {
        if (m.done && !refundedMark.done) {
            // Without a known SOLL only the tick is set; the hint below asks for the amount.
            if (refunds.length === 0 && num(v.expected) - num(v.actual) > 0) addRefund(m.date);
            else onChange({refunded: m});
            return;
        }
        if (!m.done) {
            const manual = withAmount.filter(r => r.docId === "");
            if (manual.length > 0 && !await confirm(`Von Hand erfasste Erstattungen (${manual.map(r => `${r.amount} €`).join(", ")}) entfernen?`, "Entfernen")) return;
            const fromDocs = refunds.filter(r => r.docId !== "");
            onChange({refunds: fromDocs, refunded: {...c.refunded, done: false}});
            if (fromDocs.length > 0) toast.show("Erstattungen aus einem Bescheid bleiben – zum Entfernen in der Tabelle auf ✕ klicken");
            return;
        }
        onChange({refunded: m}); // date changed
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
            {v.suggestClarify && refunds.length > 0 && (
                <Notice kind="warn">
                    IST weicht vom SOLL ab – Klärungsbedarf markieren oder{" "}
                    <button className="link" onClick={() => onChange({expected: toInput(v.actual), clarify: {...c.clarify, done: false}})}>
                        Abweichung akzeptieren (SOLL = IST)
                    </button>?
                </Notice>
            )}
            {v.deadline && <div className={`small mt-2 ${v.deadlineDays < 0 ? "neg" : "muted"}`}>
                Antragsfrist {v.deadlineDays < 0 ? "abgelaufen am" : "bis"} {date(v.deadline)}
            </div>}

            <div className="checks">
                <MarkField label="eingereicht" mark={c.submitted} onChange={submitted => onChange({submitted})}/>
                <MarkField label="erstattet" mark={refundedMark} onChange={m => void setRefunded(m)}/>
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

            {refundedMark.done && withAmount.length === 0 && (
                <Notice kind="warn">
                    Als erstattet markiert, aber kein Betrag erfasst.{" "}
                    <button className="link" onClick={() => addRefund(c.refunded.date || today())}>Offenes SOLL als Erstattung eintragen</button>
                    {" "}oder das Häkchen entfernen.
                </Notice>
            )}

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
            <button className="link" onClick={() => addRefund()}>+ Erstattung erfassen</button>

            <details className="mt-3">
                <summary>Abweichende Werte</summary>
                <div className="grid mt-2">
                    <Field label="Satz (%)" htmlFor={`${id}-rate`} hint="Leer = aus dem Versicherungsschutz">
                        <input id={`${id}-rate`} value={c.rate} placeholder={v.rate} onChange={e => onChange({rate: e.target.value})}/>
                    </Field>
                    <Field label="Erstattungsfähig (€)" htmlFor={`${id}-eligible`} hint="Leer = Rechnungsbetrag">
                        <input id={`${id}-eligible`} value={c.eligible} onChange={e => onChange({eligible: e.target.value})}/>
                    </Field>
                    <Field label="SOLL (€)" htmlFor={`${id}-expected`} hint="Leer = berechnet">
                        <input id={`${id}-expected`} value={c.expected} onChange={e => onChange({expected: e.target.value})}/>
                    </Field>
                </div>
            </details>
        </section>
    );
}

/** The latest date of the refunds (for the "erstattet" date shown when only refunds exist). */
function latestDate(refunds: RefundDTO[]): string {
    return refunds.map(r => r.date).filter(Boolean).sort().at(-1) ?? "";
}
