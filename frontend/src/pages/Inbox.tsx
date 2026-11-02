import {useEffect, useId, useRef, useState} from "react";
import type {DragEvent} from "react";
import {Desktop, Svc} from "../api";
import type {AnalysisDTO, AnalyzeInputDTO, InboxItemDTO, InvoiceDTO, PayerDTO, PhoneUploadDTO, RefundReviewDTO} from "../api";
import {useApp, useInboxChanged} from "../App";
import {useConfirm, useToast} from "../components/feedback";
import {Badge, Empty, ErrorNotice, Field, Notice} from "../components/ui";
import {recognize, renderPdfPreview} from "../lib/capture";
import {CATEGORY_LABELS, errMsg, eur, plural, toInput, today} from "../lib/format";
import {key, useSaveShortcut, useUnsavedChanges} from "../lib/shortcuts";
import {useBackend} from "../lib/useBackend";
import {useDialog} from "../lib/useDialog";
import RefundReview from "./RefundReview";

/** Largest file accepted by drag & drop – the same limit as the phone upload. */
export const MAX_FILE_MB = 40;

const SOURCE: Record<string, [string, "good" | "warn" | undefined]> = {
    qr: ["aus GiroCode", "good"],
    label: ["erkannt", undefined],
    guess: ["bitte prüfen", "warn"],
};

function readAsDataURL(file: File): Promise<string> {
    return new Promise((resolve, reject) => {
        const r = new FileReader();
        r.onload = () => resolve(String(r.result));
        r.onerror = () => reject(r.error);
        r.readAsDataURL(file);
    });
}

/** Posteingang: scans, photos and PDFs waiting to become invoices. */
export default function Inbox() {
    const {dataVersion} = useApp();
    const inboxChanged = useInboxChanged();
    const toast = useToast();
    const confirm = useConfirm();
    const list = useBackend(() => Svc.ListInbox(), [dataVersion]);
    const settings = useBackend(() => Svc.GetSettings(), [dataVersion]);
    const [selected, setSelected] = useState<string>("");
    const [phone, setPhone] = useState(false);
    const [dragging, setDragging] = useState(false);
    // dragenter/dragleave also fire when moving between child elements: count them.
    const dragDepth = useRef(0);
    const items = list.data ?? [];
    const current = items.find(i => i.name === selected) ?? items[0];

    useEffect(() => {
        const all = list.data ?? [];
        if (all.length && !all.some(i => i.name === selected)) setSelected(all[0].name);
    }, [list.data, selected]);

    // After own changes: reload the list and the count in the navigation.
    const reload = () => {
        list.reload();
        inboxChanged();
    };

    async function addChosen() {
        try {
            const paths = await Desktop.ChooseFiles("Belege hinzufügen", "PDF und Bilder", "*.pdf;*.jpg;*.jpeg;*.png;*.webp");
            if (!paths?.length) return;
            const names = await Svc.AddFilesToInbox(paths);
            toast.show(`${plural(names?.length ?? 0, "Datei", "Dateien")} hinzugefügt`);
            reload();
        } catch (e) {
            toast.error(e);
        }
    }

    function onDragEnter(e: DragEvent) {
        if (!e.dataTransfer.types.includes("Files")) return;
        dragDepth.current++;
        setDragging(true);
    }

    function onDragLeave(e: DragEvent) {
        if (!e.dataTransfer.types.includes("Files")) return;
        dragDepth.current = Math.max(0, dragDepth.current - 1);
        if (dragDepth.current === 0) setDragging(false);
    }

    async function onDrop(e: DragEvent) {
        e.preventDefault();
        dragDepth.current = 0;
        setDragging(false);
        const files = [...e.dataTransfer.files];
        if (!files.length) return;
        const limit = MAX_FILE_MB * 1024 * 1024;
        const fitting = files.filter(f => f.size <= limit);
        const tooBig = files.filter(f => f.size > limit);
        try {
            for (const f of fitting) await Svc.AddToInbox(f.name, await readAsDataURL(f));
            const added = fitting.length ? `${plural(fitting.length, "Datei", "Dateien")} hinzugefügt` : "";
            if (tooBig.length === 0) toast.show(added);
            else {
                const names = tooBig.map(f => `„${f.name}“`).join(", ");
                const one = tooBig.length === 1;
                toast.show(`${names} ${one ? "ist" : "sind"} größer als ${MAX_FILE_MB} MB und ${one ? "wurde" : "wurden"} nicht hinzugefügt.` +
                    (added ? ` ${added}.` : ""), true);
            }
        } catch (err) {
            toast.error(err);
        } finally {
            if (fitting.length) reload();
        }
    }

    async function discard(item: InboxItemDTO) {
        if (!await confirm(`„${item.name}“ aus dem Posteingang löschen?`, "Löschen")) return;
        try {
            await Svc.DiscardInbox(item.name);
            reload();
        } catch (e) {
            toast.error(e);
        }
    }

    return (
        <div onDragEnter={onDragEnter} onDragOver={e => e.preventDefault()} onDragLeave={onDragLeave} onDrop={onDrop}>
            <div className="page-head">
                <h1>Posteingang</h1>
                <span className="muted small">Neue Belege aus dem Ordner „Eingang“, vom Handy oder per Drag &amp; Drop</span>
                <span className="spacer"/>
                <button onClick={addChosen}>Dateien hinzufügen …</button>
                <button className="primary" onClick={() => setPhone(true)}>Mit Handy fotografieren</button>
            </div>
            {list.error && <ErrorNotice error={list.error}/>}
            {dragging && <div className="dropzone">Dateien hier ablegen</div>}
            {items.length === 0 ? (
                <Empty>
                    Der Posteingang ist leer. Belege mit dem Handy fotografieren, Dateien hierher ziehen oder in den Ordner
                    „Eingang“ im Datenordner legen (z. B. mit der Scan-Funktion der Dateien-App oder der Nextcloud-App).
                </Empty>
            ) : (
                <div className="inbox">
                    <ul className="inbox-list" aria-label="Belege im Posteingang">
                        {items.map(i => (
                            <li key={i.name}>
                                <button className={`inbox-item ${current?.name === i.name ? "on" : ""}`} onClick={() => setSelected(i.name)}>
                                    <span className="name">{i.name}</span>
                                    <span className="sub">{i.type === "pdf" ? "PDF" : i.type === "image" ? "Bild" : "Datei"} · {i.modified.slice(8, 10)}.{i.modified.slice(5, 7)}. {i.modified.slice(11)}</span>
                                </button>
                            </li>
                        ))}
                    </ul>
                    {current && <InboxItem key={current.name} item={current} payers={settings.data?.payers ?? []}
                                           onDone={reload} onDiscard={() => discard(current)}/>}
                </div>
            )}
            {phone && <PhoneDialog onClose={() => {
                setPhone(false);
                reload();
            }}/>}
        </div>
    );
}

function Preview({item}: {item: InboxItemDTO}) {
    const canvas = useRef<HTMLCanvasElement>(null);
    const [error, setError] = useState("");
    const {url, type, modified} = item;
    // Rendered again only when the file changed – list reloads hand in new item objects.
    useEffect(() => {
        if (type !== "pdf" || !canvas.current) return;
        const run = new AbortController();
        setError("");
        renderPdfPreview(url, canvas.current, 520, run.signal).catch(e => !run.signal.aborted && setError(errMsg(e)));
        return () => run.abort();
    }, [url, type, modified]);
    if (type === "image") return <img className="preview" src={url} alt={item.name}/>;
    if (type === "pdf") {
        return (
            <>
                {error && <Notice kind="warn">{error}</Notice>}
                <canvas className="preview" ref={canvas} style={error ? {display: "none"} : undefined}/>
            </>
        );
    }
    return <Notice kind="warn">Vorschau nicht möglich.</Notice>;
}

function InboxItem({item, payers, onDone, onDiscard}: {item: InboxItemDTO; payers: PayerDTO[]; onDone: () => void; onDiscard: () => void}) {
    const toast = useToast();
    const {setYear} = useApp();
    const [status, setStatus] = useState("");
    const [error, setError] = useState("");
    const [analysis, setAnalysis] = useState<AnalysisDTO | null>(null);
    const [draft, setDraft] = useState<InvoiceDTO | null>(null);
    const [saving, setSaving] = useState(false);
    const [input, setInput] = useState<AnalyzeInputDTO | null>(null);
    const [review, setReview] = useState<RefundReviewDTO | null>(null);
    // Set once the user edits the draft or switches between invoice and refund
    // document: a later recognition run (the file changed) must not undo that.
    const touched = useRef(false);
    const [edited, setEdited] = useState(false); // the user typed into the draft: leaving asks first
    useUnsavedChanges(edited && !saving);
    const {url, type, modified} = item;
    useSaveShortcut(() => void save(), draft !== null && review === null);

    async function asRefund(inp: AnalyzeInputDTO) {
        touched.current = true;
        try {
            setReview(await Svc.AnalyzeRefund(inp));
        } catch (e) {
            setError(errMsg(e));
        }
    }

    function asInvoice() {
        touched.current = true;
        setReview(null);
    }

    // Recognition runs once per file version. List reloads (own actions, live
    // updates from other devices) hand in a new item object for the same file;
    // they must neither restart the recognition nor reset the form.
    useEffect(() => {
        const run = new AbortController();
        const {signal} = run;
        (async () => {
            let done = "";
            setError("");
            try {
                const rec = await recognize(url, type, msg => !signal.aborted && setStatus(msg), signal);
                if (signal.aborted) return;
                setStatus("Felder werden zugeordnet …");
                const inp = {lines: rec.lines, qr: rec.qr};
                const a = await Svc.AnalyzeDocument(inp);
                if (signal.aborted) return;
                done = rec.ocr ? "Text per Texterkennung gelesen – bitte die Felder prüfen." : "";
                setInput(inp);
                setAnalysis(a);
                if (touched.current) return;
                if (a.kind === "refund") {
                    try {
                        const r = await Svc.AnalyzeRefund(inp);
                        if (signal.aborted) return;
                        setReview(r);
                    } catch (e) {
                        if (signal.aborted) return;
                        setError(errMsg(e));
                    }
                }
                setDraft({...a.draft.invoice, amount: toInput(a.draft.invoice.amount === "0.00" ? "" : a.draft.invoice.amount)});
            } catch (e) {
                if (signal.aborted) return;
                setError(errMsg(e));
                if (!touched.current) setDraft(d => d ?? emptyDraft());
            } finally {
                if (!signal.aborted) setStatus(done);
            }
        })();
        return () => run.abort();
    }, [url, type, modified]);

    async function save() {
        if (!draft || saving) return;
        setSaving(true);
        try {
            const d = await Svc.CreateInvoiceFromInbox(item.name, draft);
            toast.show(`Rechnung „${d.invoice.provider}“ über ${eur(d.invoice.amount)} gespeichert`);
            setYear(d.view.year);
            onDone();
        } catch (e) {
            toast.error(e);
        } finally {
            setSaving(false);
        }
    }

    const src = (field: string) => {
        const s = analysis?.sources?.[field];
        if (!s) return undefined;
        const [label, kind] = SOURCE[s] ?? [s, undefined];
        return <Badge kind={kind}>{label}</Badge>;
    };
    const set = (patch: Partial<InvoiceDTO>) => {
        touched.current = true;
        setEdited(true);
        setDraft(d => d && {...d, ...patch});
    };

    return (
        <section className={`inbox-detail ${review ? "review" : ""}`} aria-label={item.name}>
            <div className="preview-wrap"><Preview item={item}/></div>
            <div className="panel">
                <div className="payer-head">
                    <h2>{item.name}</h2>
                    {analysis?.kind === "invoice" && <Badge kind="good">Rechnung erkannt</Badge>}
                    {analysis?.kind === "refund" && <Badge kind="good">Erstattungsbeleg erkannt</Badge>}
                    <span className="spacer"/>
                    {input && (review
                        ? <button className="link" onClick={asInvoice}>Als Rechnung behandeln</button>
                        : <button className="link" onClick={() => asRefund(input)}>Als Erstattungsbeleg behandeln</button>)}
                </div>
                {status && <p className="muted small" role="status">{status}</p>}
                {error && <Notice kind="warn">{error} Die Felder lassen sich von Hand ausfüllen.</Notice>}
                {review && <RefundReview inboxName={item.name} initial={review} payers={payers} onDone={onDone} onDiscard={onDiscard}/>}
                {!review && (analysis?.notes ?? []).map(n => <Notice key={n} kind={n.startsWith("Rezept erkannt –") ? undefined : "warn"}>{n}</Notice>)}
                {!review && draft && (
                    <>
                        <div className="grid" style={{marginTop: 8}}>
                            <Field label="Behandler *" htmlFor="ib-provider" hint={src("provider")}>
                                <input id="ib-provider" value={draft.provider} onChange={e => set({provider: e.target.value})}/>
                            </Field>
                            <Field label="Betrag (€) *" htmlFor="ib-amount" hint={src("amount")}>
                                <input id="ib-amount" inputMode="decimal" value={draft.amount} onChange={e => set({amount: e.target.value})}/>
                            </Field>
                            <Field label="Rechnungsdatum *" htmlFor="ib-date" hint={src("invoiceDate")}>
                                <input id="ib-date" type="date" value={draft.invoiceDate} onChange={e => set({invoiceDate: e.target.value})}/>
                            </Field>
                            <Field label="Behandlung von" htmlFor="ib-from" hint={src("serviceFrom")}>
                                <input id="ib-from" type="date" value={draft.serviceFrom} onChange={e => set({serviceFrom: e.target.value})}/>
                            </Field>
                            <Field label="Behandlung bis" htmlFor="ib-to">
                                <input id="ib-to" type="date" value={draft.serviceTo} onChange={e => set({serviceTo: e.target.value})}/>
                            </Field>
                            <Field label="Kategorie" htmlFor="ib-cat" hint={src("category")}>
                                <select id="ib-cat" value={draft.category} onChange={e => set({category: e.target.value})}>
                                    <option value="">–</option>
                                    {Object.entries(CATEGORY_LABELS).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
                                </select>
                            </Field>
                            <Field label="Rechnungsnummer" htmlFor="ib-ref" hint={src("reference")}>
                                <input id="ib-ref" value={draft.reference} onChange={e => set({reference: e.target.value})}/>
                            </Field>
                            <Field label="Zahlungsempfänger" htmlFor="ib-payee" hint={src("payee")}>
                                <input id="ib-payee" value={draft.payee} onChange={e => set({payee: e.target.value})}/>
                            </Field>
                            <Field label="IBAN" htmlFor="ib-iban" hint={src("iban")}>
                                <input id="ib-iban" value={draft.iban} onChange={e => set({iban: e.target.value})}/>
                            </Field>
                            <Field label="Bezahlt" htmlFor="ib-paid" hint={draft.paid.done ? "z. B. an der Apothekenkasse" : undefined}>
                                <span className="inline">
                                    <input id="ib-paid" type="checkbox" checked={draft.paid.done}
                                           onChange={e => set({paid: {...draft.paid, done: e.target.checked,
                                               date: e.target.checked ? draft.paid.date || draft.invoiceDate || today() : ""}})}/>
                                    {draft.paid.done && (
                                        <input type="date" aria-label="Bezahlt am" value={draft.paid.date}
                                               onChange={e => set({paid: {...draft.paid, date: e.target.value}})}/>
                                    )}
                                </span>
                            </Field>
                        </div>
                        <div className="actions">
                            <button className="primary" disabled={saving} onClick={save} title={`Als Rechnung speichern (${key("S")})`}>Als Rechnung speichern</button>
                            <button className="danger" onClick={onDiscard}>Verwerfen</button>
                        </div>
                        <p className="muted small" style={{marginTop: 8}}>Der Beleg wird mit der Rechnung verknüpft und im Ordner „belege“ abgelegt. Status und Erstattungen lassen sich danach in der Rechnungsliste setzen.</p>
                    </>
                )}
            </div>
        </section>
    );
}

function emptyDraft(): InvoiceDTO {
    const m = {done: false, date: "", note: ""};
    return {id: "", provider: "", payee: "", iban: "", reference: "", category: "", invoiceDate: "", serviceFrom: "", serviceTo: "",
        serviceYear: 0, amount: "", due: "", paid: m, claims: [], attachments: [], note: ""};
}

function PhoneDialog({onClose}: {onClose: () => void}) {
    const [info, setInfo] = useState<PhoneUploadDTO | null>(null);
    const [error, setError] = useState("");
    const ref = useDialog<HTMLDivElement>(onClose);
    const titleId = useId();
    useEffect(() => {
        let alive = true;
        const load = () => Svc.StartPhoneUpload().then(i => alive && setInfo(i), e => alive && setError(errMsg(e)));
        load();
        const t = window.setInterval(load, 3000);
        return () => {
            alive = false;
            window.clearInterval(t);
            Svc.StopPhoneUpload().catch(() => {});
        };
    }, []);
    return (
        <div className="modal-back" onClick={e => e.target === e.currentTarget && onClose()}>
            <div ref={ref} className="modal" role="dialog" aria-modal="true" aria-labelledby={titleId} tabIndex={-1} style={{maxWidth: 460}}>
                <h2 id={titleId} style={{marginTop: 0}}>Mit Handy fotografieren</h2>
                {error && <ErrorNotice error={error}/>}
                {info && (
                    <>
                        <p>QR-Code mit der Kamera des Handys scannen. Auf der Seite, die sich öffnet, Belege fotografieren oder PDFs wählen – sie erscheinen hier im Posteingang.</p>
                        {!info.lan && <Notice kind="warn">Kein Netzwerk gefunden – Handy und Computer müssen im selben WLAN sein.</Notice>}
                        <div style={{textAlign: "center"}}><img src={info.qr} alt="QR-Code für das Handy" width={240} height={240}/></div>
                        <p className="mono small" style={{wordBreak: "break-all", textAlign: "center"}}>{info.url}</p>
                        <p className="muted small">
                            Nur im eigenen WLAN verwenden. Der Link gilt bis {info.expires} Uhr und endet, sobald dieses Fenster
                            geschlossen wird. {info.received > 0 && <strong>{plural(info.received, "Datei", "Dateien")} empfangen.</strong>}
                            {" "}Beim ersten Mal fragt die Firewall unter Windows oder macOS ggf. nach – bitte für private Netzwerke erlauben.
                        </p>
                    </>
                )}
                <div className="actions"><span className="spacer"/><button className="primary" onClick={onClose}>Fertig</button></div>
            </div>
        </div>
    );
}
