// "Kompakt": one line per invoice – date, provider, amount, per payer three
// status dots with a short text, the open amount and flags. A line expands
// (chevron, click or →) to everything the Detail card shows, ticks included.
// Only the visible lines are rendered; the page scrolls (no own scroll
// area), expanded lines are measured.
import {useLayoutEffect, useRef, useState} from "react";
import type {CSSProperties, KeyboardEvent as ReactKeyboardEvent} from "react";
import {Link} from "react-router";
import {useVirtualizer} from "@tanstack/react-virtual";
import type {ClaimViewDTO, InvoiceRowDTO, PayerDTO} from "../../api";
import {MarkBox} from "../../components/MarkBox";
import {CATEGORY_LABELS, date, eur, num, plural, shortDate} from "../../lib/format";
import {footerItems, PayerColumn} from "./DetailView";
import {claimOf, claimState, opensInvoice, payerClass, useRowNav} from "./shared";
import type {ViewProps} from "./shared";

const LINE_HEIGHT = 46;

/** What the payer still owes on this invoice: SOLL − IST of claims not held back or skipped. */
export function openAmount(r: InvoiceRowDTO): number {
    return (r.claims ?? []).reduce((s, c) => (["open", "submitted", "refunded"].includes(c.status) ? s + num(c.expected) - num(c.actual) : s), 0);
}

/** Progress in three dots: held 1, submitted 2, refunded 3 (the last one coloured by the outcome). */
function dots(c: ClaimViewDTO): {filled: number; last?: string} {
    switch (c.status) {
        case "held":
            return {filled: 1};
        case "submitted":
            return {filled: 2};
        case "refunded":
            return {filled: 3, last: c.clarify.done ? "bad" : c.suggestClarify ? "warn" : undefined};
        default:
            return {filled: 0};
    }
}

/** Short text next to the dots: "eingereicht 20.09.", "erstattet −39,21 €", "nicht eingereicht". */
function shortText(c: ClaimViewDTO): {text: string; tone: string} {
    const day = (iso: string) => shortDate(iso).slice(0, 6);
    switch (c.status) {
        case "submitted":
            return {text: `eingereicht ${day(c.submitted.date)}`, tone: ""};
        case "refunded": {
            const d = num(c.diff);
            if (d !== 0) return {text: `erstattet ${eur(d)}`, tone: c.clarify.done ? "neg" : c.suggestClarify ? "warn-text" : ""};
            return {text: `erstattet ${day(c.refunded.date)}`.trim(), tone: "pos"};
        }
        default:
            return {text: claimState(c).label, tone: c.status === "open" ? "warn-text" : "muted"};
    }
}

/** Small flags at the end of the line, each with its meaning as text. */
function flags(r: InvoiceRowDTO): {mark: string; text: string; tone: string}[] {
    const out: {mark: string; text: string; tone: string}[] = [];
    if (!r.paid.done) out.push({mark: "€", text: r.due ? `nicht bezahlt, fällig ${date(r.due)}` : "nicht bezahlt", tone: "warn"});
    const cl = r.claims ?? [];
    const wait = Math.max(0, ...cl.filter(c => c.longWait).map(c => c.waitingDays));
    if (wait > 0) out.push({mark: `${Math.floor(wait / 7)} W`, text: `Erstattung seit ${Math.floor(wait / 7)} Wochen offen`, tone: "warn"});
    if (cl.some(c => c.clarify.done)) out.push({mark: "!", text: "Klärungsbedarf", tone: "bad"});
    else if (cl.some(c => c.suggestClarify)) out.push({mark: "?", text: "IST weicht vom SOLL ab – prüfen", tone: "warn"});
    if ((r.warnings ?? []).length > 0) out.push({mark: "⚠", text: (r.warnings ?? []).map(w => w.text).join("; "), tone: "warn"});
    if (r.note.trim()) out.push({mark: "N", text: `Notiz: ${r.note.trim()}`, tone: "muted"});
    return out;
}

export function CompactView(v: ViewProps) {
    const {rows, payers} = v;
    const [expanded, setExpanded] = useState<Set<string>>(new Set());
    const list = useRef<HTMLDivElement | null>(null);
    const [page, setPage] = useState<HTMLElement | null>(null);
    const [margin, setMargin] = useState(0);

    // The page scrolls; the list starts this far below the top of the page.
    // Measured after every render (the bulk bar or notices above may have
    // changed it, and every scroll renders again); state is only set when the
    // value changed, so this settles after one pass.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    useLayoutEffect(() => {
        const el = list.current;
        const content = el?.closest<HTMLElement>(".content") ?? null;
        if (content !== page) setPage(content);
        if (el && content) {
            const m = Math.round(el.getBoundingClientRect().top - content.getBoundingClientRect().top + content.scrollTop);
            if (m !== margin) setMargin(m);
        }
    });

    const virtual = useVirtualizer({
        count: rows.length,
        getScrollElement: () => page,
        estimateSize: i => (expanded.has(rows[i].id) ? LINE_HEIGHT + 170 : LINE_HEIGHT),
        overscan: 10,
        scrollMargin: margin,
        getItemKey: i => rows[i].id,
    });

    const setOpen = (id: string, on: boolean) => setExpanded(s => {
        const n = new Set(s);
        if (on) n.add(id); else n.delete(id);
        return n;
    });
    const nav = useRowNav(rows.map(r => r.id), {
        open: v.open, toggle: v.toggleOne,
        scrollTo: i => virtual.scrollToIndex(i, {align: "auto"}),
        keys: (e: ReactKeyboardEvent, id: string) => {
            if (e.key === "ArrowRight") setOpen(id, true);
            else if (e.key === "ArrowLeft") setOpen(id, false);
            else return false;
            return true;
        },
    });

    // Without a scrolling page (tests, unusual layouts) all lines are rendered.
    const items = page ? virtual.getVirtualItems() : rows.map((_, index) => ({index, start: margin, end: margin}));
    const top = page && items.length ? items[0].start - margin : 0;
    const bottom = page && items.length ? virtual.getTotalSize() - (items[items.length - 1].end - margin) : 0;
    const style = {"--payers": payers.length} as CSSProperties;
    const total = rows.reduce((s, r) => s + num(r.amount), 0);
    const open = rows.reduce((s, r) => s + openAmount(r), 0);

    return (
        <section className="compact" style={style} aria-label="Rechnungen, Kompaktansicht">
            <div className="compact-line compact-head" aria-hidden="true">
                <span/>
                <span>Behandlung</span>
                <span>Behandler</span>
                <span className="right">Betrag</span>
                {payers.map((p, i) => <span key={p.id} className={payerClass(i)}>{p.name}</span>)}
                <span className="right">Offen</span>
                <span/>
                <span/>
            </div>
            <div ref={el => {
                list.current = el;
                nav.container.current = el;
            }} role="list">
                {top > 0 && <div aria-hidden="true" style={{height: top}}/>}
                {items.map(item => {
                    const r = rows[item.index];
                    return (
                        <div key={r.id} data-index={item.index} ref={page ? virtual.measureElement : undefined} role="listitem"
                             aria-setsize={rows.length} aria-posinset={item.index + 1}>
                            <CompactLine row={r} payers={payers} v={v} nav={nav.item(r.id)}
                                         expanded={expanded.has(r.id)} setExpanded={on => setOpen(r.id, on)}/>
                        </div>
                    );
                })}
                {bottom > 0 && <div aria-hidden="true" style={{height: bottom}}/>}
            </div>
            <div className="compact-line compact-sum">
                <span/>
                <span/>
                <span>{plural(rows.length, "Rechnung", "Rechnungen")}</span>
                <span className="right num">{eur(total)}</span>
                {payers.map(p => <span key={p.id}/>)}
                <span className="right num due-sum" title="SOLL − IST, ohne zurückgehaltene">{eur(open)}</span>
                <span/>
                <span/>
            </div>
        </section>
    );
}

function CompactLine({row: r, payers, v, nav, expanded, setExpanded}: {
    row: InvoiceRowDTO;
    payers: PayerDTO[];
    v: ViewProps;
    nav: ReturnType<ReturnType<typeof useRowNav>["item"]>;
    expanded: boolean;
    setExpanded: (on: boolean) => void;
}) {
    const selected = v.selected.has(r.id);
    const open = openAmount(r);
    const detailsId = `compact-${r.id}`;
    const label = `${r.provider || "Rechnung ohne Namen"}, ${date(r.serviceDate)}, ${eur(r.amount)}`;
    return (
        <div className={`compact-row ${selected ? "sel" : ""} ${expanded ? "open" : ""}`}>
            <div className="compact-line" role="group" aria-label={label} {...nav}
                 onClick={e => opensInvoice(e.target as HTMLElement) && setExpanded(!expanded)}
                 onDoubleClick={e => opensInvoice(e.target as HTMLElement) && v.open(r.id)}>
                <input type="checkbox" aria-label={`${r.provider} auswählen`} checked={selected} onChange={() => v.toggleOne(r.id)}/>
                <span className="num muted">{shortDate(r.serviceDate) || "–"}</span>
                <span className="compact-provider">
                    <Link to={`/rechnung/${encodeURIComponent(r.id)}`}>{r.provider || "(ohne Namen)"}</Link>
                    <span className="muted small">{CATEGORY_LABELS[r.category] ?? r.category}</span>
                </span>
                <span className="right num">{eur(r.amount)}</span>
                {payers.map((p, i) => {
                    const c = claimOf(r, p.id);
                    if (!c) return <span key={p.id} className="muted">–</span>;
                    const d = dots(c);
                    const t = shortText(c);
                    return (
                        <span key={p.id} className="compact-claim">
                            <span className={`dots ${payerClass(i)}`} aria-hidden="true">
                                {[0, 1, 2].map(k => <span key={k} className={k < d.filled ? `dot ${k === 2 && d.last ? d.last : ""}` : "ring"}/>)}
                            </span>
                            <span className={t.tone}>{t.text}</span>
                        </span>
                    );
                })}
                <span className={`right num ${open === 0 ? "muted" : ""}`}>{open === 0 ? "–" : eur(open)}</span>
                <span className="flags">
                    {flags(r).map(f => (
                        <span key={f.mark} className={`flag-box ${f.tone}`} title={f.text}>
                            <span aria-hidden="true">{f.mark}</span><span className="sr-only">{f.text}</span>
                        </span>
                    ))}
                </span>
                <button type="button" className="chevron" aria-expanded={expanded} aria-controls={detailsId}
                        aria-label={expanded ? "Details ausblenden" : "Details anzeigen"} tabIndex={-1}
                        onClick={() => setExpanded(!expanded)}>›</button>
            </div>
            {expanded && <CompactDetails id={detailsId} row={r} payers={payers} v={v}/>}
        </div>
    );
}

function CompactDetails({id, row: r, payers, v}: {id: string; row: InvoiceRowDTO; payers: PayerDTO[]; v: ViewProps}) {
    const footer = footerItems(r);
    return (
        <div id={id} className="compact-details" style={{"--cols": payers.length + 1} as CSSProperties}>
            {payers.map((p, i) => {
                const c = claimOf(r, p.id);
                return c ? <PayerColumn key={p.id} row={r} claim={c} index={i} v={v}/> : <div key={p.id} className="inv-payer muted">{p.name}: –</div>;
            })}
            <div className="inv-payer">
                <strong>
                    {r.invoiceDate ? `Rechnung ${date(r.invoiceDate)}` : "Rechnung"}{r.reference && ` · ${r.reference}`}
                </strong>
                <MarkBox label="bezahlt" mark={r.paid}
                         onToggle={on => v.change({invoiceId: r.id, payerId: "", mark: "paid", on, date: "", note: "", amount: ""})}
                         onDate={d => v.change({invoiceId: r.id, payerId: "", mark: "paid", on: true, date: d, note: "", amount: ""})}/>
                {!r.paid.done && r.due && <span className="due">fällig {date(r.due)}</span>}
                {footer.map(f => <span key={f.text} className={`small ${f.tone ?? ""}`}>{f.text}</span>)}
                <Link to={`/rechnung/${encodeURIComponent(r.id)}`}>Rechnung öffnen →</Link>
            </div>
        </div>
    );
}
