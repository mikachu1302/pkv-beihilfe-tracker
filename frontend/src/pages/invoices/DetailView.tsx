// "Detail" (default view of the new design): one card per invoice – the
// invoice on the left, one column per payer with status, SOLL, IST,
// difference and the ticks, a footer with note, documents and hints. Only
// the visible cards are rendered (usePageVirtualizer, a feed of articles).
import type {CSSProperties} from "react";
import {Link} from "react-router";
import type {ClaimViewDTO, InvoiceRowDTO, PayerDTO} from "../../api";
import {MarkBox} from "../../components/MarkBox";
import {CATEGORY_LABELS, date, eur, num, pct, plural, shortDate} from "../../lib/format";
import {ClaimPopover, ClarifyFlag, claimOf, claimState, deadlineText, markBase, opensInvoice, payerClass, usePageVirtualizer, useRowNav} from "./shared";
import type {ViewProps} from "./shared";

/** Space between two cards in px (views.css: article + article). */
const CARD_GAP = 12;

export function DetailView(v: ViewProps) {
    const {rows, payers} = v;
    const pv = usePageVirtualizer(rows.length, {estimateSize: () => 190, getItemKey: i => rows[i].id, gap: CARD_GAP});
    const nav = useRowNav(rows.map(r => r.id), {open: v.open, toggle: v.toggleOne, scrollTo: pv.scrollTo,
        rendered: pv.items.map(i => rows[i.index].id)});
    const total = rows.reduce((s, r) => s + num(r.amount), 0);
    const own = rows.reduce((s, r) => s + num(r.ownShare), 0);
    return (
        <section className="inv-cards">
            <div role="feed" aria-label="Rechnungen, Detailansicht" aria-busy="false" ref={el => {
                pv.list.current = el;
                nav.container.current = el;
            }}>
                {pv.top > 0 && <div aria-hidden="true" style={{height: pv.top}}/>}
                {pv.items.map(item => {
                    const r = rows[item.index];
                    return (
                        <InvoiceCard key={r.id} row={r} payers={payers} v={v} nav={nav.item(r.id)}
                                     slot={{index: item.index, count: rows.length, measure: pv.measure}}/>
                    );
                })}
                {pv.bottom > 0 && <div aria-hidden="true" style={{height: pv.bottom}}/>}
            </div>
            <p className="inv-total">
                <span>{plural(rows.length, "Rechnung", "Rechnungen")}</span>
                <span>Betrag <strong className="num">{eur(total)}</strong></span>
                <span title="Rechnungsbetrag minus Erstattungen">Nicht erstattet <strong className="num">{eur(own)}</strong></span>
            </p>
        </section>
    );
}

function InvoiceCard({row: r, payers, v, nav, slot}: {
    row: InvoiceRowDTO;
    payers: PayerDTO[];
    v: ViewProps;
    nav: ReturnType<ReturnType<typeof useRowNav>["item"]>;
    /** Position in the list, and the virtualiser's measuring ref. */
    slot: {index: number; count: number; measure?: (el: HTMLElement | null) => void};
}) {
    const href = `/rechnung/${encodeURIComponent(r.id)}`;
    const selected = v.selected.has(r.id);
    const unpaidDue = !r.paid.done && r.due !== "";
    const footer = footerItems(r);
    const label = `${r.provider || "Rechnung ohne Namen"}, ${date(r.serviceDate)}, ${eur(r.amount)}`;
    return (
        <article className={`inv-card ${selected ? "sel" : ""}`} aria-label={label} {...nav}
                 ref={slot.measure} data-index={slot.index} aria-posinset={slot.index + 1} aria-setsize={slot.count}
                 onClick={e => opensInvoice(e.target as HTMLElement) && v.open(r.id)}>
            <div className="inv-grid" style={{"--payers": payers.length} as CSSProperties}>
                <div className="inv-main">
                    <div className="inv-dates">
                        <input type="checkbox" aria-label={`${r.provider} auswählen`} checked={selected} onChange={() => v.toggleOne(r.id)}/>
                        <span className="num">{date(r.serviceDate)}</span>
                        {r.invoiceDate && r.invoiceDate !== r.serviceDate && <span>· Rechnung <span className="num">{shortDate(r.invoiceDate)}</span></span>}
                    </div>
                    <Link className="inv-provider" to={href}>{r.provider || "(ohne Namen)"}</Link>
                    <span className="muted small">{[CATEGORY_LABELS[r.category], r.reference].filter(Boolean).join(" · ") || " "}</span>
                    <div className="inv-amount-row">
                        <span className="inv-amount num">{eur(r.amount)}</span>
                        <span className="inv-paid">
                            <MarkBox label="bezahlt" mark={r.paid}
                                     onToggle={on => v.change({invoiceId: r.id, payerId: "", mark: "paid", on, date: "", note: "", amount: ""})}
                                     onDate={d => v.change({invoiceId: r.id, payerId: "", mark: "paid", on: true, date: d, note: "", amount: ""})}/>
                            {unpaidDue && <span className="due">fällig {shortDate(r.due)}</span>}
                        </span>
                    </div>
                </div>
                {payers.map((p, i) => {
                    const c = claimOf(r, p.id);
                    if (!c) return <div key={p.id} className="inv-payer"><span className={`payer-name ${payerClass(i)}`}>{p.name}</span><span className="muted">–</span></div>;
                    return <PayerColumn key={p.id} row={r} claim={c} index={i} v={v}/>;
                })}
            </div>
            {footer.length > 0 && (
                <div className="inv-foot">
                    {footer.map(f => <span key={f.text} className={f.tone ?? ""}>{f.text}</span>)}
                </div>
            )}
        </article>
    );
}

export function PayerColumn({row: r, claim: c, index, v}: {row: InvoiceRowDTO; claim: ClaimViewDTO; index: number; v: ViewProps}) {
    const state = claimState(c);
    const refunded = c.status === "refunded";
    const diff = num(c.diff);
    const base = markBase(r, c);
    const deadline = deadlineText(c);
    return (
        <div className="inv-payer rel">
            <div className="inv-payer-head">
                <span className={`payer-name ${payerClass(index)}`}>{c.payerName} <span className="muted">{pct(c.rate)}</span></span>
                <span className={`pill ${state.tone}`}>{state.label}</span>
            </div>
            <div className="inv-figures">
                <span><span className="fig-label">SOLL</span><span className="num">{eur(c.expected)}</span></span>
                <span><span className="fig-label">IST</span><span className="num">{refunded ? eur(c.actual) : "–"}</span></span>
                <span>
                    <span className="fig-label">Differenz</span>
                    <span className={`num ${refunded && diff < 0 ? "neg" : refunded && diff > 0 ? "pos" : refunded ? "" : "muted"}`}>{refunded ? eur(c.diff) : "–"}</span>
                </span>
            </div>
            <div className="inv-marks">
                <span className="markline">
                    <MarkBox label="eingereicht" mark={c.submitted}
                             onToggle={on => v.change({...base, mark: "submitted", on, date: ""})}
                             onDate={d => v.change({...base, mark: "submitted", on: true, date: d})}/>
                </span>
                <MarkBox label="erstattet" mark={c.refunded.done || num(c.actual) !== 0 ? {...c.refunded, done: true} : c.refunded}
                         onToggle={on => v.toggleRefunded(r, c, on)}/>
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

/** What the footer of a card lists: note, Klärung, hints, waiting long, documents, Bescheid. */
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
