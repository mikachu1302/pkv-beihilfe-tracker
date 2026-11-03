import {useEffect, useId, useRef, useState} from "react";
import {useLocation} from "react-router";
import {Desktop, Svc} from "../api";
import type {AnalysisDTO, AnalyzeInputDTO, InboxItemDTO, InboxPageDTO, InboxSuggestionDTO, InvoiceDTO, LineDTO, PayerDTO, PhoneUploadDTO,
    RefundReviewDTO} from "../api";
import {useApp, useInboxChanged} from "../App";
import {DocThumb} from "../components/DocThumb";
import {useConfirm, useToast} from "../components/feedback";
import {Badge, Empty, ErrorNotice, Field, Notice, SourceBadge} from "../components/ui";
import {recognizePages, renderPdfPreview} from "../lib/capture";
import {bySize, readAsDataURL, tooBigText, useFileDrop} from "../lib/files";
import {CATEGORY_LABELS, errMsg, eur, plural, toInput, today} from "../lib/format";
import {hasUnsavedChanges, key, useSaveShortcut, useUnsavedChanges} from "../lib/shortcuts";
import {useBackend} from "../lib/useBackend";
import {useDialog} from "../lib/useDialog";
import RefundReview from "./RefundReview";

/** Posteingang: scans, photos and PDFs waiting to become invoices. */
export default function Inbox() {
    const {dataVersion} = useApp();
    const inboxChanged = useInboxChanged();
    const toast = useToast();
    const confirm = useConfirm();
    const list = useBackend(() => Svc.ListInbox(), [dataVersion]);
    const settings = useBackend(() => Svc.GetSettings(), [dataVersion]);
    const location = useLocation();
    // An entry to select as soon as the list has it (after merging, or a
    // Bescheid moved here from the invoice form).
    const wanted = useRef<string>((location.state as {select?: string} | null)?.select ?? "");
    const [selected, setSelected] = useState<string>(wanted.current);
    const [phone, setPhone] = useState(false);
    const {dragging, handlers} = useFileDrop(files => void addDropped(files));
    // Entries ticked to be merged into one document.
    const [ticked, setTicked] = useState<string[]>([]);
    const [merging, setMerging] = useState(false);
    // Recognised text per entry (from the background recognition) and the
    // suggestions which entries belong together.
    const [texts, setTexts] = useState<Record<string, EntryText>>({});
    const textsRef = useRef(texts);
    textsRef.current = texts;
    const [suggestions, setSuggestions] = useState<InboxSuggestionDTO[]>([]);
    const [notTogether, setNotTogether] = useState<string[]>(loadNotTogether);
    const items = list.data ?? [];
    const current = items.find(i => i.name === selected) ?? items[0];
    const listKey = items.map(i => `${i.name}\t${entryKey(i)}`).join("\n");

    // Recognise the entries one after another in the background (the selected
    // one first – the detail reads it anyway, the results are shared) so that
    // pages which belong together can be suggested.
    useEffect(() => {
        if (items.length < 2) return;
        const run = new AbortController();
        (async () => {
            const order = [...items.filter(i => i.name === current?.name), ...items.filter(i => i.name !== current?.name)];
            for (const it of order.slice(0, MAX_BACKGROUND)) {
                const k = entryKey(it);
                if (run.signal.aborted) return;
                if (textsRef.current[it.name]?.key === k) continue;
                try {
                    const rec = await recognizePages(pageFiles(it), undefined, run.signal);
                    if (run.signal.aborted) return;
                    setTexts(t => ({...t, [it.name]: {key: k, lines: rec.lines, qr: rec.qr}}));
                } catch {
                    if (run.signal.aborted) return; // unreadable: no suggestion for it
                }
            }
        })();
        return () => run.abort();
        // Selecting another entry does not restart the background run.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [listKey]);

    useEffect(() => {
        const known = items.filter(i => texts[i.name]?.key === entryKey(i));
        if (known.length < 2) {
            setSuggestions([]);
            return;
        }
        let alive = true;
        const t = window.setTimeout(() => {
            Svc.SuggestInboxGroups(known.map(i => ({name: i.name, lines: texts[i.name].lines, qr: texts[i.name].qr})))
                .then(s => alive && setSuggestions(s ?? []), () => {});
        }, 300);
        return () => {
            alive = false;
            window.clearTimeout(t);
        };
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [texts, listKey]);

    const visibleSuggestions = suggestions.filter(s => !notTogether.includes(togetherKey(s.names ?? [])) &&
        (s.names ?? []).every(n => items.some(i => i.name === n)));

    useEffect(() => {
        const all = list.data ?? [];
        if (wanted.current && all.some(i => i.name === wanted.current)) {
            setSelected(wanted.current);
            wanted.current = "";
        } else if (all.length && !all.some(i => i.name === selected) && !(wanted.current && list.loading)) {
            setSelected(all[0].name);
        }
        setTicked(t => t.filter(n => all.some(i => i.name === n)));
    }, [list.data, list.loading, selected]);

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

    async function addDropped(files: File[]) {
        const {fitting, tooBig} = bySize(files);
        try {
            for (const f of fitting) await Svc.AddToInbox(f.name, await readAsDataURL(f));
            const added = fitting.length ? `${plural(fitting.length, "Datei", "Dateien")} hinzugefügt` : "";
            if (tooBig.length === 0) toast.show(added);
            else toast.show(tooBigText(tooBig) + (added ? ` ${added}.` : ""), true);
        } catch (err) {
            toast.error(err);
        } finally {
            if (fitting.length) reload();
        }
    }

    async function merge(names: string[]) {
        if (current && names.includes(current.name) && hasUnsavedChanges() &&
            !await confirm("Die Eingaben zu diesem Beleg gehen beim Zusammenfassen verloren. Trotzdem zusammenfassen?", "Zusammenfassen")) return;
        setMerging(true);
        try {
            const name = await Svc.MergeInbox(names);
            setTicked([]);
            wanted.current = name;
            setSelected(name);
            toast.show(`${names.length} Belege zu einem zusammengefasst`);
            reload();
        } catch (e) {
            toast.error(e);
        } finally {
            setMerging(false);
        }
    }

    function dismiss(names: string[]) {
        const next = [...notTogether, togetherKey(names)].slice(-200);
        setNotTogether(next);
        try {
            localStorage.setItem(NOT_TOGETHER_KEY, JSON.stringify(next));
        } catch {
            // only remembered for this session
        }
    }

    const tick = (name: string, on: boolean) => setTicked(t => (on ? [...t, name] : t.filter(n => n !== name)));

    async function discard(item: InboxItemDTO) {
        const what = item.group ? `„${item.name}“ mit ${plural(pagesOf(item).length, "Seite", "Seiten")}` : `„${item.name}“`;
        if (!await confirm(`${what} aus dem Posteingang löschen?`, "Löschen")) return;
        try {
            // Only the pages shown: one arriving meanwhile stays.
            await Svc.DiscardInbox(item.name, item.group ? pagesOf(item).map(p => p.name) : []);
            reload();
        } catch (e) {
            toast.error(e);
        }
    }

    return (
        <div {...handlers}>
            <div className="page-head">
                <h1>Posteingang</h1>
                <span className="muted small">Neue Belege aus dem Ordner „Eingang“, vom Handy oder per Drag &amp; Drop</span>
                <span className="spacer"/>
                <button onClick={addChosen}>Dateien hinzufügen …</button>
                <button className="primary" onClick={() => setPhone(true)}>Mit Handy fotografieren</button>
            </div>
            {list.error && <ErrorNotice error={list.error}/>}
            {dragging && <div className="dropzone">Dateien hier ablegen</div>}
            {visibleSuggestions.map(sg => (
                <Notice key={(sg.names ?? []).join("\n")}>
                    <strong>Gehören wohl zusammen:</strong> {quoted(sg.names ?? [])} ({sg.reason}).{" "}
                    <button className="link" disabled={merging} onClick={() => merge(sg.names ?? [])}>Zusammenfassen</button>
                    {" · "}
                    <button className="link" onClick={() => dismiss(sg.names ?? [])}>Gehören nicht zusammen</button>
                </Notice>
            ))}
            {items.length === 0 ? (
                <Empty>
                    Der Posteingang ist leer. Belege mit dem Handy fotografieren, Dateien hierher ziehen oder in den Ordner
                    „Eingang“ im Datenordner legen (z. B. mit der Scan-Funktion der Dateien-App oder der Nextcloud-App).
                </Empty>
            ) : (
                <div className="inbox">
                    <div>
                        {items.length > 1 && (
                            <div className="merge-bar" role="group" aria-label="Zusammenfassen">
                                {ticked.length >= 2 ? (
                                    <>
                                        <button className="primary" disabled={merging}
                                                onClick={() => merge(items.filter(i => ticked.includes(i.name)).map(i => i.name))}>
                                            Zu einem Beleg zusammenfassen ({ticked.length})
                                        </button>
                                        <button className="link" onClick={() => setTicked([])}>Auswahl aufheben</button>
                                    </>
                                ) : (
                                    <span className="muted small">
                                        {ticked.length === 1 ? "Weitere Belege anhaken, um sie als Seiten zusammenzufassen." : "Seiten eines Belegs anhaken, um sie zusammenzufassen."}
                                    </span>
                                )}
                            </div>
                        )}
                        <ul className="inbox-list" aria-label="Belege im Posteingang">
                            {items.map(i => (
                                <li key={i.name} className="inbox-row">
                                    {items.length > 1 && (
                                        <input type="checkbox" aria-label={`„${i.name}“ zum Zusammenfassen auswählen`}
                                               checked={ticked.includes(i.name)} onChange={e => tick(i.name, e.target.checked)}/>
                                    )}
                                    <button className={`inbox-item ${current?.name === i.name ? "on" : ""}`} onClick={() => setSelected(i.name)}>
                                        <span className="name">{i.name}</span>
                                        <span className="sub">{kindLabel(i)} · {i.modified.slice(8, 10)}.{i.modified.slice(5, 7)}. {i.modified.slice(11)}</span>
                                    </button>
                                </li>
                            ))}
                        </ul>
                    </div>
                    {current && <InboxItem key={current.name} item={current} payers={settings.data?.payers ?? []}
                                           onDone={reload} onChanged={reload} onDiscard={() => discard(current)}/>}
                </div>
            )}
            {phone && <PhoneDialog onClose={() => {
                setPhone(false);
                reload();
            }}/>}
        </div>
    );
}

/** Entries recognised in the background, at most. */
const MAX_BACKGROUND = 30;

interface EntryText {
    key: string; // entryKey when it was read
    lines: LineDTO[];
    qr: string[];
}

const NOT_TOGETHER_KEY = "pkv.inboxNotTogether";

/** Suggestions answered with "Gehören nicht zusammen" (on this device). */
function loadNotTogether(): string[] {
    try {
        const v = JSON.parse(localStorage.getItem(NOT_TOGETHER_KEY) ?? "[]");
        return Array.isArray(v) ? v.filter(x => typeof x === "string") : [];
    } catch {
        return [];
    }
}

const togetherKey = (names: string[]) => [...names].sort().join("\n");

/** „a“, „b“ und „c“ */
function quoted(names: string[]): string {
    const q = names.map(n => `„${n}“`);
    return q.length > 1 ? `${q.slice(0, -1).join(", ")} und ${q.at(-1)}` : q.join("");
}

/** The pages of an inbox document; a single file is its only page. */
const pagesOf = (i: InboxItemDTO): InboxPageDTO[] =>
    i.pages?.length ? i.pages : [{name: i.name, size: i.size, modified: i.modified, type: i.type, url: i.url}];

function kindLabel(i: InboxItemDTO): string {
    if (i.group) return plural(pagesOf(i).length, "Seite", "Seiten");
    return i.type === "pdf" ? "PDF" : i.type === "image" ? "Bild" : "Datei";
}

/** The page names without the number the app puts before them ("01 "): stable across renumbering. */
const pageKey = (p: InboxPageDTO) => `${p.name.slice(p.name.lastIndexOf("/") + 1).replace(/^\d{2,3} /, "")}|${p.size}|${p.modified}`;

/** The pages as the recognition takes them. */
const pageFiles = (i: InboxItemDTO) => pagesOf(i).map(p => ({url: p.url, type: p.type, modified: p.modified, size: p.size, key: pageKey(p)}));

/** Identifies the version of an entry's pages. */
const entryKey = (i: InboxItemDTO) => pagesOf(i).map(pageKey).join("\n");

/** All pages of a document, one below the other. */
function Pages({item}: {item: InboxItemDTO}) {
    if (!item.group) return <Preview page={pagesOf(item)[0]}/>;
    return (
        <>
            {pagesOf(item).map((p, i) => (
                <figure key={p.name} className="page-preview">
                    <figcaption className="muted small">Seite {i + 1}{p.type === "pdf" ? " (PDF – erste Seite)" : ""}</figcaption>
                    <Preview page={p}/>
                </figure>
            ))}
        </>
    );
}

function Preview({page}: {page: {name: string; url: string; type: string; modified: string}}) {
    const canvas = useRef<HTMLCanvasElement>(null);
    const [error, setError] = useState("");
    const {url, type, modified} = page;
    // Rendered again only when the file changed – list reloads hand in new item objects.
    useEffect(() => {
        if (type !== "pdf" || !canvas.current) return;
        const run = new AbortController();
        setError("");
        renderPdfPreview(url, canvas.current, 520, run.signal).catch(e => !run.signal.aborted && setError(errMsg(e)));
        return () => run.abort();
    }, [url, type, modified]);
    if (type === "image") return <img className="preview" src={url} alt={page.name}/>;
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

function InboxItem({item, payers, onDone, onChanged, onDiscard}: {
    item: InboxItemDTO;
    payers: PayerDTO[];
    onDone: () => void;
    onChanged: () => void; // pages reordered or taken out
    onDiscard: () => void;
}) {
    const toast = useToast();
    const {setYear} = useApp();
    const [status, setStatus] = useState("");
    const [error, setError] = useState("");
    const [analysis, setAnalysis] = useState<AnalysisDTO | null>(null);
    const [draft, setDraft] = useState<InvoiceDTO | null>(null);
    const [saving, setSaving] = useState(false);
    const [input, setInput] = useState<AnalyzeInputDTO | null>(null);
    const [review, setReviewState] = useState<RefundReviewDTO | null>(null);
    // Every new reading gets a fresh review form (RefundReview keeps its own state).
    const [reviewKey, setReviewKey] = useState(0);
    const setReview = (r: RefundReviewDTO | null) => {
        setReviewState(r);
        if (r) setReviewKey(k => k + 1);
    };
    // Set once the user edits the draft or switches between invoice and refund
    // document: a later recognition run (the file changed) must not undo that.
    const touched = useRef(false);
    const [edited, setEdited] = useState(false); // the user typed into the draft: leaving asks first
    // The pages changed after the user edited the draft or review: offer to read them again.
    const [stale, setStale] = useState(false);
    const [reread, setReread] = useState(0);
    const reads = useRef(0);
    useUnsavedChanges(edited && !saving);
    // Recognition runs again when the pages change (not when the list is reloaded).
    const pages = pageFiles(item);
    const pagesKey = pages.map(p => p.key).join("\n");
    const pagesRef = useRef(pages);
    pagesRef.current = pages;
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
                const rec = await recognizePages(pagesRef.current, msg => !signal.aborted && setStatus(msg), signal);
                if (signal.aborted) return;
                setStatus("Felder werden zugeordnet …");
                const inp = {lines: rec.lines, qr: rec.qr};
                const a = await Svc.AnalyzeDocument(inp);
                if (signal.aborted) return;
                done = [rec.ocr ? "Text per Texterkennung gelesen – bitte die Felder prüfen." : "",
                    rec.truncated ? `Nur die ersten ${rec.pages} Seiten wurden gelesen.` : ""].filter(Boolean).join(" ");
                setInput(inp);
                setAnalysis(a);
                reads.current++;
                if (touched.current) {
                    if (reads.current > 1) setStale(true);
                    return;
                }
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
    }, [pagesKey, reread]);

    function readAgain() {
        touched.current = false;
        setEdited(false);
        setStale(false);
        setReview(null);
        setReread(n => n + 1);
    }

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
        return s ? <SourceBadge source={s}/> : undefined;
    };
    const set = (patch: Partial<InvoiceDTO>) => {
        touched.current = true;
        setEdited(true);
        setDraft(d => d && {...d, ...patch});
    };

    return (
        <section className={`inbox-detail ${review ? "review" : ""}`} aria-label={item.name}>
            <div className="preview-wrap"><Pages item={item}/></div>
            <div className="panel">
                <div className="payer-head">
                    <h2>{item.name}</h2>
                    {item.group && <Badge>{plural(pagesOf(item).length, "Seite", "Seiten")}</Badge>}
                    {analysis?.kind === "invoice" && <Badge kind="good">Rechnung erkannt</Badge>}
                    {analysis?.kind === "refund" && <Badge kind="good">Erstattungsbeleg erkannt</Badge>}
                    <span className="spacer"/>
                    {input && (review
                        ? <button className="link" onClick={asInvoice}>Als Rechnung behandeln</button>
                        : <button className="link" onClick={() => asRefund(input)}>Als Erstattungsbeleg behandeln</button>)}
                </div>
                {item.group && <PageStrip item={item} onChanged={onChanged} edited={edited}/>}
                {stale && (
                    <Notice kind="warn">
                        Die Seiten dieses Belegs haben sich geändert – die Angaben unten stammen noch von vorher.{" "}
                        <button className="link" onClick={readAgain}>Neu einlesen</button> (eigene Eingaben gehen dabei verloren)
                    </Notice>
                )}
                {status && <p className="muted small" role="status">{status}</p>}
                {error && <Notice kind="warn">{error} Die Felder lassen sich von Hand ausfüllen.</Notice>}
                {review && <RefundReview key={reviewKey} inboxName={item.name} initial={review} payers={payers} onDone={onDone}
                                         onDiscard={onDiscard} onEdit={() => {
                                             touched.current = true;
                                             setEdited(true);
                                         }}/>}
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

/** The pages of a multi-page document: change the order, take pages out. */
function PageStrip({item, onChanged, edited}: {item: InboxItemDTO; onChanged: () => void; edited: boolean}) {
    const toast = useToast();
    const confirm = useConfirm();
    const [busy, setBusy] = useState(false);
    const [dragFrom, setDragFrom] = useState<number | null>(null);
    // After moving a page with the keyboard the focus follows it.
    const focusAfter = useRef("");
    const pages = pagesOf(item);
    const names = pages.map(p => p.name);
    // Busy until the list was loaded again (a second move needs the new names);
    // every reload hands in a new item object.
    useEffect(() => {
        setBusy(false);
        if (focusAfter.current) {
            document.querySelector<HTMLElement>(`[data-focus="${CSS.escape(focusAfter.current)}"]`)?.focus();
            focusAfter.current = "";
        }
    }, [item]);
    async function run(p: Promise<unknown>) {
        setBusy(true);
        try {
            await p;
            onChanged();
        } catch (e) {
            toast.error(e);
            setBusy(false);
        }
    }
    const move = (from: number, to: number, focus = "") => {
        if (busy || to < 0 || to >= names.length) return;
        const next = [...names];
        const [page] = next.splice(from, 1);
        next.splice(to, 0, page);
        focusAfter.current = focus;
        void run(Svc.ReorderInbox(item.name, next));
    };
    // Taking pages out dissolves the document when at most one page is left:
    // the entries typed for it are lost then.
    async function split(name: string, all: boolean) {
        if (busy) return;
        if (edited && (all || names.length <= 2) &&
            !await confirm("Die Eingaben zu diesem Beleg gehen dabei verloren. Trotzdem trennen?", "Trennen")) return;
        void run(Svc.SplitInbox(name));
    }
    return (
        <div className="page-strip">
            <div className="doc-tiles" aria-label="Seiten">
                {pages.map((p, i) => {
                    const k = pageKey(p);
                    return (
                        <div key={k} className={`doc-tile ${dragFrom === i ? "dragging" : ""}`} draggable={!busy}
                             onDragStart={e => {
                                 e.dataTransfer.effectAllowed = "move";
                                 setDragFrom(i);
                             }}
                             onDragOver={e => dragFrom !== null && e.preventDefault()}
                             onDrop={e => {
                                 if (dragFrom === null) return; // files from outside: the page's drop zone takes them
                                 e.preventDefault();
                                 e.stopPropagation();
                                 if (dragFrom !== i) move(dragFrom, i);
                                 setDragFrom(null);
                             }}
                             onDragEnd={() => setDragFrom(null)}>
                            <div className="doc-open"><DocThumb url={p.url} type={p.type} label={`Seite ${i + 1}`}/></div>
                            <div className="doc-meta">
                                <span>Seite {i + 1}</span>
                                <span className="spacer"/>
                                <button className="link" aria-disabled={busy || i === 0} data-focus={`${k}:prev`}
                                        aria-label={`Seite ${i + 1} nach vorne`} title="Nach vorne"
                                        onClick={() => move(i, i - 1, `${k}:prev`)}>←</button>
                                <button className="link" aria-disabled={busy || i === names.length - 1} data-focus={`${k}:next`}
                                        aria-label={`Seite ${i + 1} nach hinten`} title="Nach hinten"
                                        onClick={() => move(i, i + 1, `${k}:next`)}>→</button>
                            </div>
                            <button className="link small" aria-disabled={busy} aria-label={`Seite ${i + 1} herauslösen`}
                                    onClick={() => void split(p.name, false)}>Herauslösen</button>
                        </div>
                    );
                })}
            </div>
            <p className="muted small">
                Reihenfolge per Pfeil oder durch Ziehen ändern. „Herauslösen“ macht eine Seite wieder zu einem eigenen Beleg.{" "}
                <button className="link" aria-disabled={busy} onClick={() => void split(item.name, true)}>Alle Seiten trennen</button>
            </p>
        </div>
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
                        <p>QR-Code mit der Kamera des Handys scannen. Auf der Seite, die sich öffnet, Belege fotografieren oder PDFs wählen – sie erscheinen hier im Posteingang.
                            Für einen Beleg mit mehreren Seiten dort „Mehrseitiger Beleg“ wählen.</p>
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
