// "Detail" (default view of the new design): one card per invoice – the
// invoice on the left, one column per payer with status, SOLL, IST,
// difference and the ticks, a footer with note, documents and hints. Only
// the visible cards are rendered (usePageVirtualizer, a feed of articles).
import type {CSSProperties} from "react";
import {Link} from "react-router";
import type {InvoiceRowDTO, PayerDTO} from "../../api";
import {CATEGORY_LABELS, date, dayMonth, eur, plural, shortDate, sumBy} from "../../lib/format";
import {invoicePath} from "../../lib/paths";
import {useListNav, usePageVirtualizer} from "./hooks";
import type {RowNavItem} from "./hooks";
import {claimOf, opensInvoice, payerClass, providerName, rowLabel} from "./model";
import type {ViewProps} from "./model";
import {footerItems, PaidMark, PayerColumn, SelectBox} from "./parts";

/** Space between two cards in px (views.css takes it as --card-gap). */
const CARD_GAP = 12;

export function DetailView(v: ViewProps) {
    const {rows, payers} = v;
    const pv = usePageVirtualizer(rows.length, {estimateSize: () => 190, getItemKey: i => rows[i].id, gap: CARD_GAP});
    // a feed moves by one article with PageUp/PageDown
    const nav = useListNav(v, pv, {pageStep: 1});
    return (
        <section className="inv-cards" style={{"--card-gap": `${CARD_GAP}px`} as CSSProperties}>
            <div role="feed" aria-label="Rechnungen, Detailansicht" aria-busy="false" {...nav.listProps}>
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
                <span>Betrag <strong className="num">{eur(sumBy(rows, r => r.amount))}</strong></span>
                <span title="Rechnungsbetrag minus Erstattungen">Nicht erstattet <strong className="num">{eur(sumBy(rows, r => r.ownShare))}</strong></span>
            </p>
        </section>
    );
}

function InvoiceCard({row: r, payers, v, nav, slot}: {
    row: InvoiceRowDTO;
    payers: PayerDTO[];
    v: ViewProps;
    nav: RowNavItem;
    /** Position in the list, and the virtualiser's measuring ref. */
    slot: {index: number; count: number; measure?: (el: HTMLElement | null) => void};
}) {
    const footer = footerItems(r);
    return (
        <article className={`inv-card ${v.selected.has(r.id) ? "sel" : ""}`} aria-label={rowLabel(r)} {...nav}
                 ref={slot.measure} data-index={slot.index} aria-posinset={slot.index + 1} aria-setsize={slot.count}
                 onClick={e => opensInvoice(e.target as HTMLElement) && v.open(r.id)}>
            <div className="inv-grid" style={{"--payers": payers.length} as CSSProperties}>
                <div className="inv-main">
                    <div className="inv-dates">
                        <SelectBox row={r} v={v}/>
                        <span className="num">{date(r.serviceDate)}</span>
                        {r.invoiceDate && r.invoiceDate !== r.serviceDate && <span>· Rechnung <span className="num">{shortDate(r.invoiceDate)}</span></span>}
                    </div>
                    <Link className="provider-link inv-provider" to={invoicePath(r.id)}>{providerName(r)}</Link>
                    <span className="muted small">{[CATEGORY_LABELS[r.category], r.reference].filter(Boolean).join(" · ") || " "}</span>
                    <div className="inv-amount-row">
                        <span className="inv-amount num">{eur(r.amount)}</span>
                        <span className="inv-paid">
                            <PaidMark row={r} change={v.change}/>
                            {!r.paid.done && r.due && <span className="due">fällig {dayMonth(r.due)}</span>}
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
