import {useEffect, useRef, useState} from "react";
import type {DragEvent} from "react";
import {Desktop, Svc} from "../api";
import type {AnalysisDTO, AnalyzeInputDTO, InboxItemDTO, InvoiceDTO, PayerDTO, PhoneUploadDTO, RefundReviewDTO} from "../api";
import {useApp} from "../App";
import {useConfirm, useToast} from "../components/feedback";
import {Badge, Empty, ErrorNotice, Field, Notice} from "../components/ui";
import {recognize, renderPdfPreview} from "../lib/capture";
import {CATEGORY_LABELS, errMsg, eur, plural, toInput} from "../lib/format";
import {useBackend} from "../lib/useBackend";
import RefundReview from "./RefundReview";

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
    const toast = useToast();
    const confirm = useConfirm();
    const list = useBackend(() => Svc.ListInbox(), [dataVersion]);
    const settings = useBackend(() => Svc.GetSettings(), [dataVersion]);
    const [selected, setSelected] = useState<string>("");
    const [phone, setPhone] = useState(false);
    const [dragging, setDragging] = useState(false);
    const items = list.data ?? [];
    const current = items.find(i => i.name === selected) ?? items[0];

    useEffect(() => {
        if (items.length && !items.some(i => i.name === selected)) setSelected(items[0].name);
    }, [items, selected]);

    async function addChosen() {
        try {
            const paths = await Desktop.ChooseFiles("Belege hinzufügen", "PDF und Bilder", "*.pdf;*.jpg;*.jpeg;*.png;*.webp");
            if (!paths?.length) return;
            const names = await Svc.AddFilesToInbox(paths);
            toast.show(`${plural(names?.length ?? 0, "Datei", "Dateien")} hinzugefügt`);
            list.reload();
        } catch (e) {
            toast.error(e);
        }
    }

    async function onDrop(e: DragEvent) {
        e.preventDefault();
        setDragging(false);
        const files = [...e.dataTransfer.files];
        if (!files.length) return;
        try {
            for (const f of files) await Svc.AddToInbox(f.name, await readAsDataURL(f));
            toast.show(`${plural(files.length, "Datei", "Dateien")} hinzugefügt`);
            list.reload();
        } catch (err) {
            toast.error(err);
        }
    }

    async function discard(item: InboxItemDTO) {
        if (!await confirm(`„${item.name}“ aus dem Posteingang löschen?`, "Löschen")) return;
        try {
            await Svc.DiscardInbox(item.name);
            list.reload();
        } catch (e) {
            toast.error(e);
        }
    }

    return (
        <div onDragOver={e => { e.preventDefault(); setDragging(true); }} onDragLeave={() => setDragging(false)} onDrop={onDrop}>
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
                                           onDone={list.reload} onDiscard={() => discard(current)}/>}
                </div>
            )}
            {phone && <PhoneDialog onClose={() => setPhone(false)}/>}
        </div>
    );
}

function Preview({item}: {item: InboxItemDTO}) {
    const canvas = useRef<HTMLCanvasElement>(null);
    const [error, setError] = useState("");
    useEffect(() => {
        if (item.type !== "pdf" || !canvas.current) return;
        renderPdfPreview(item.url, canvas.current, 520).catch(e => setError(errMsg(e)));
    }, [item]);
    if (item.type === "image") return <img className="preview" src={item.url} alt={item.name}/>;
    if (item.type === "pdf") return error ? <Notice kind="warn">{error}</Notice> : <canvas className="preview" ref={canvas}/>;
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

    async function asRefund(inp: AnalyzeInputDTO) {
        try {
            setReview(await Svc.AnalyzeRefund(inp));
        } catch (e) {
            setError(errMsg(e));
        }
    }

    useEffect(() => {
        let stale = false;
        (async () => {
            try {
                const rec = await recognize(item.url, item.type, msg => !stale && setStatus(msg));
                if (stale) return;
                setStatus("Felder werden zugeordnet …");
                const inp = {lines: rec.lines, qr: rec.qr};
                const a = await Svc.AnalyzeDocument(inp);
                if (stale) return;
                setInput(inp);
                setAnalysis(a);
                if (a.kind === "refund") await asRefund(inp);
                setDraft({...a.draft.invoice, amount: toInput(a.draft.invoice.amount === "0.00" ? "" : a.draft.invoice.amount)});
                setStatus(rec.ocr ? "Text per Texterkennung gelesen – bitte die Felder prüfen." : "");
            } catch (e) {
                if (!stale) {
                    setError(errMsg(e));
                    setStatus("");
                    setDraft(emptyDraft());
                }
            }
        })();
        return () => { stale = true; };
    }, [item]);

    async function save() {
        if (!draft) return;
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
    const set = (patch: Partial<InvoiceDTO>) => draft && setDraft({...draft, ...patch});

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
                        ? <button className="link" onClick={() => setReview(null)}>Als Rechnung behandeln</button>
                        : <button className="link" onClick={() => asRefund(input)}>Als Erstattungsbeleg behandeln</button>)}
                </div>
                {status && <p className="muted small" role="status">{status}</p>}
                {error && <Notice kind="warn">{error} Die Felder lassen sich von Hand ausfüllen.</Notice>}
                {review && <RefundReview inboxName={item.name} initial={review} payers={payers} onDone={onDone} onDiscard={onDiscard}/>}
                {!review && (analysis?.notes ?? []).map(n => <Notice key={n} kind="warn">{n}</Notice>)}
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
                        </div>
                        <div className="actions">
                            <button className="primary" disabled={saving} onClick={save}>Als Rechnung speichern</button>
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
            <div className="modal" role="dialog" aria-modal="true" aria-label="Mit Handy fotografieren" style={{maxWidth: 460}}>
                <h2 style={{marginTop: 0}}>Mit Handy fotografieren</h2>
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
