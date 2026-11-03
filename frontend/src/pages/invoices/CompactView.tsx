// "Kompakt": one line per invoice – date, provider, amount, per payer three
// status dots with a short text, the open amount and flags. A line expands
// (chevron, click or →) to everything the Detail card shows, ticks included.
// Only the visible lines are rendered (usePageVirtualizer); the page
// scrolls, expanded lines are measured.
import {useState} from "react";
import type {CSSProperties, KeyboardEvent} from "react";
import {Link} from "react-router";
import type {ClaimViewDTO, InvoiceRowDTO, PayerDTO} from "../../api";
import {CATEGORY_LABELS, date, dayMonth, eur, num, plural, shortDate, sumBy} from "../../lib/format";
import {invoicePath} from "../../lib/paths";
import {useListNav, usePageVirtualizer} from "./hooks";
import type {RowNavItem} from "./hooks";
import {claimOf, claimState, opensInvoice, payerClass, providerName, rowLabel} from "./model";
import type {Tone, ViewProps} from "./model";
import {footerItems, PaidMark, PayerColumn, SelectBox} from "./parts";

/** Estimated height of a line in px (expanded lines are measured). */
const LINE_HEIGHT = 46;

/** Progress in three dots: held 1, submitted 2, refunded 3 (the last one coloured by the outcome). */
function dots(c: ClaimViewDTO): {filled: number; last?: Tone} {
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
function shortText(c: ClaimViewDTO): {text: string; className: string} {
    switch (c.status) {
        case "submitted":
            return {text: `eingereicht ${dayMonth(c.submitted.date)}`, className: ""};
        case "refunded": {
            if (num(c.diff) !== 0) return {text: `erstattet ${eur(c.diff)}`, className: c.clarify.done ? "neg" : c.suggestClarify ? "warn-text" : ""};
            return {text: `erstattet ${dayMonth(c.refunded.date)}`.trim(), className: "pos"};
        }
        default:
            return {text: claimState(c).label, className: c.status === "open" ? "warn-text" : "muted"};
    }
}

/** Small flags at the end of the line, each with its meaning as text. */
function flags(r: InvoiceRowDTO): {mark: string; text: string; tone: Tone}[] {
    const out: {mark: string; text: string; tone: Tone}[] = [];
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
    const pv = usePageVirtualizer(rows.length, {
        estimateSize: i => (expanded.has(rows[i].id) ? LINE_HEIGHT + 170 : LINE_HEIGHT),
        getItemKey: i => rows[i].id,
    });

    const setOpen = (id: string, on: boolean) => {
        setExpanded(s => {
            const n = new Set(s);
            if (on) n.add(id); else n.delete(id);
            return n;
        });
        if (!on && v.pop?.rowId === id) v.setPop(null); // its popover goes with the details
    };
    const nav = useListNav(v, pv, {
        keys: (e: KeyboardEvent, id: string) => {
            if (e.key === "ArrowRight") setOpen(id, true);
            else if (e.key === "ArrowLeft") setOpen(id, false);
            else return false;
            return true;
        },
    });

    return (
        <section className="compact" style={{"--payers": payers.length} as CSSProperties} aria-label="Rechnungen, Kompaktansicht">
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
            <div role="list" {...nav.listProps}>
                {pv.top > 0 && <div aria-hidden="true" style={{height: pv.top}}/>}
                {pv.items.map(item => {
                    const r = rows[item.index];
                    return (
                        <div key={r.id} data-index={item.index} ref={pv.measure} role="listitem"
                             aria-setsize={rows.length} aria-posinset={item.index + 1}>
                            <CompactLine row={r} payers={payers} v={v} nav={nav.item(r.id)}
                                         expanded={expanded.has(r.id)} setExpanded={on => setOpen(r.id, on)}/>
                        </div>
                    );
                })}
                {pv.bottom > 0 && <div aria-hidden="true" style={{height: pv.bottom}}/>}
            </div>
            <div className="compact-line compact-sum">
                <span/>
                <span/>
                <span>{plural(rows.length, "Rechnung", "Rechnungen")}</span>
                <span className="right num">{eur(sumBy(rows, r => r.amount))}</span>
                {payers.map(p => <span key={p.id}/>)}
                <span className="right num due-sum" title="SOLL − IST, ohne zurückgehaltene">{eur(sumBy(rows, r => r.owed))}</span>
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
    nav: RowNavItem;
    expanded: boolean;
    setExpanded: (on: boolean) => void;
}) {
    const owed = num(r.owed);
    const detailsId = `compact-${r.id}`;
    return (
        <div className={`compact-row ${v.selected.has(r.id) ? "sel" : ""} ${expanded ? "open" : ""}`}>
            <div className="compact-line" role="group" aria-label={rowLabel(r)} aria-expanded={expanded} aria-controls={detailsId}
                 aria-keyshortcuts="ArrowRight ArrowLeft Enter" {...nav}
                 onClick={e => opensInvoice(e.target as HTMLElement) && setExpanded(!expanded)}
                 onDoubleClick={e => opensInvoice(e.target as HTMLElement, true) && v.open(r.id)}>
                <SelectBox row={r} v={v}/>
                <span className="num muted">{shortDate(r.serviceDate) || "–"}</span>
                <span className="compact-provider">
                    <Link className="provider-link" to={invoicePath(r.id)}>{providerName(r)}</Link>
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
                            <span className={t.className}>{t.text}</span>
                        </span>
                    );
                })}
                <span className={`right num ${owed === 0 ? "muted" : ""}`}>{owed === 0 ? "–" : eur(owed)}</span>
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
                <PaidMark row={r} change={v.change}/>
                {!r.paid.done && r.due && <span className="due">fällig {date(r.due)}</span>}
                {footerItems(r).map(f => <span key={f.text} className={`small ${f.tone ?? ""}`}>{f.text}</span>)}
                <Link to={invoicePath(r.id)}>Rechnung öffnen →</Link>
            </div>
        </div>
    );
}
