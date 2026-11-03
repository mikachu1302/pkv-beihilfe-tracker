// "Tabelle": every field as a column, like a spreadsheet – read only.
// Sorting by any column, a column chooser, dense or normal rows, a sum row.
// Only the visible rows are rendered (@tanstack/react-virtual): the header,
// the first columns and the sum row are sticky and stay outside the
// virtualised body, every row has the fixed height of its density.
import {useMemo, useRef, useState} from "react";
import type {CSSProperties, ReactNode} from "react";
import {useVirtualizer} from "@tanstack/react-virtual";
import type {ClaimViewDTO, InvoiceRowDTO, PayerDTO} from "../../api";
import {COLUMN_KINDS, PAYER_KINDS} from "../../lib/appearance";
import type {ColumnKind, TableOptions} from "../../lib/appearance";
import {CATEGORY_LABELS, date, dayMonth, decimal, eur, num, plural, shortDate, signClass, sumBy} from "../../lib/format";
import {useDialog} from "../../lib/useDialog";
import {firstInView, useLayoutValue, useRowNav} from "./hooks";
import {claimOf, opensInvoice, payerClass, providerName, selectLabel} from "./model";
import type {ViewProps} from "./model";

const COLUMN_LABELS: Record<ColumnKind, string> = {
    service: "Behandlung", invoice: "Rechnung", provider: "Behandler", category: "Kategorie", reference: "Re.-Nr.",
    amount: "Betrag", paid: "Bezahlt", expected: "SOLL", actual: "IST", diff: "Diff", submitted: "eingereicht",
    refunded: "erstattet", note: "Notiz",
};

/** Heights in px of the header rows, a body row per density and the sum row (views.css takes them as variables). */
const GROUP_ROW = 30;
const COLUMN_ROW = 34;
const HEAD_HEIGHT = GROUP_ROW + COLUMN_ROW;
const ROW_HEIGHT = {normal: 36, dense: 28};
const SUM_ROW = 36;

type Cell = {text: ReactNode; title?: string; className?: string};

interface Col {
    id: string;
    kind: ColumnKind;
    label: string;
    width: number;
    right?: boolean;
    payer?: number; // index of the payer column group
    sortValue: (r: InvoiceRowDTO) => string | number;
    cell: (r: InvoiceRowDTO) => Cell;
    sum?: (rows: InvoiceRowDTO[]) => ReactNode;
}

type Sort = {id: string; dir: "asc" | "desc"} | null;

const dash: Cell = {text: "–", className: "muted"};

function columns(payers: PayerDTO[]): Col[] {
    const col = (kind: ColumnKind, width: number, c: Omit<Col, "id" | "kind" | "label" | "width">): Col =>
        ({id: kind, kind, label: COLUMN_LABELS[kind], width, ...c});
    const base: Col[] = [
        col("service", 96, {sortValue: r => r.serviceDate, cell: r => ({text: shortDate(r.serviceDate) || "–", title: date(r.serviceDate)})}),
        col("invoice", 92, {sortValue: r => r.invoiceDate, cell: r => ({text: shortDate(r.invoiceDate) || "–", title: date(r.invoiceDate)})}),
        col("provider", 220, {sortValue: r => r.provider.toLocaleLowerCase("de"),
            cell: r => ({text: providerName(r), title: r.provider, className: "strong"}),
            sum: rows => plural(rows.length, "Rechnung", "Rechnungen")}),
        col("category", 110, {sortValue: r => CATEGORY_LABELS[r.category] ?? r.category, cell: r => ({text: CATEGORY_LABELS[r.category] ?? r.category})}),
        col("reference", 110, {sortValue: r => r.reference, cell: r => (r.reference ? {text: r.reference, title: r.reference} : dash)}),
        col("amount", 100, {right: true, sortValue: r => num(r.amount), cell: r => ({text: decimal(r.amount), title: eur(r.amount)}),
            sum: rows => decimal(sumBy(rows, r => r.amount))}),
        // paid ones by date, then unpaid ones by due date, then those without
        col("paid", 100, {sortValue: r => (r.paid.done ? `0${r.paid.date}` : r.due ? `1${r.due}` : "2"),
            cell: r => r.paid.done ? {text: shortDate(r.paid.date), title: `bezahlt am ${date(r.paid.date)}`}
                : r.due ? {text: `fällig ${dayMonth(r.due)}`, title: `fällig am ${date(r.due)}`, className: "warn-text"} : dash}),
    ];
    const perPayer = payers.flatMap((p, payer): Col[] => {
        const claim = (r: InvoiceRowDTO) => claimOf(r, p.id);
        const refunded = (r: InvoiceRowDTO) => {
            const c = claim(r);
            return c?.status === "refunded" ? c : undefined;
        };
        const sumOf = (pick: (r: InvoiceRowDTO) => ClaimViewDTO | undefined, field: "expected" | "actual" | "diff") =>
            (rows: InvoiceRowDTO[]) => decimal(sumBy(rows, r => pick(r)?.[field]));
        const pcol = (kind: ColumnKind, width: number, c: Omit<Col, "id" | "kind" | "label" | "width" | "payer">): Col =>
            ({id: `${p.id}:${kind}`, kind, label: COLUMN_LABELS[kind], width, payer, ...c});
        return [
            pcol("expected", 92, {right: true, sortValue: r => num(claim(r)?.expected),
                cell: r => {
                    const c = claim(r);
                    return c ? {text: decimal(c.expected)} : dash;
                }, sum: sumOf(claim, "expected")}),
            pcol("actual", 92, {right: true, sortValue: r => (refunded(r) ? num(refunded(r)?.actual) : -Infinity),
                cell: r => {
                    const c = refunded(r);
                    return c ? {text: decimal(c.actual)} : dash;
                }, sum: sumOf(claim, "actual")}),
            pcol("diff", 86, {right: true, sortValue: r => (refunded(r) ? num(refunded(r)?.diff) : -Infinity),
                cell: r => {
                    const c = refunded(r);
                    return c ? {text: decimal(c.diff), className: signClass(c.diff)} : dash;
                }, sum: sumOf(refunded, "diff")}),
            pcol("submitted", 96, {sortValue: r => claim(r)?.submitted.date ?? "",
                cell: r => {
                    const c = claim(r);
                    if (!c) return dash;
                    if (c.submitted.done) return {text: shortDate(c.submitted.date), title: `eingereicht am ${date(c.submitted.date)}`};
                    if (c.held) return {text: "zurückg.", title: "zurückgehalten", className: "muted"};
                    if (c.skip) return {text: "verzichtet", className: "muted"};
                    return dash;
                }}),
            pcol("refunded", 96, {sortValue: r => claim(r)?.refunded.date ?? "",
                cell: r => {
                    const c = claim(r);
                    if (!c) return dash;
                    const flag = c.clarify.done ? " ⚑" : "";
                    const clarify = c.clarify.done ? `Klärungsbedarf${c.clarify.note ? `: ${c.clarify.note}` : ""}` : undefined;
                    const weeks = Math.floor(c.waitingDays / 7);
                    if (c.refunded.done) return {text: shortDate(c.refunded.date) + flag, title: clarify ?? `erstattet am ${date(c.refunded.date)}`};
                    if (c.longWait) return {text: `${weeks} Wo. offen${flag}`, title: clarify ?? `Erstattung seit ${weeks} Wochen offen`, className: "warn-text"};
                    return flag ? {text: flag.trim(), title: clarify, className: "neg"} : dash;
                }}),
        ];
    });
    const note = col("note", 220, {sortValue: r => r.note.toLocaleLowerCase("de"), cell: r => ({text: r.note, title: r.note})});
    return [...base, ...perPayer, note];
}

export function TableView(v: ViewProps & {options: TableOptions}) {
    const {payers, options} = v;
    const [sort, setSort] = useState<Sort>(null);
    const all = useMemo(() => columns(payers), [payers]);
    const cols = useMemo(() => all.filter(c => c.kind === "service" || !options.hidden.includes(c.kind)), [all, options.hidden]);

    const rows = useMemo(() => {
        const col = sort && cols.find(c => c.id === sort.id); // a hidden column does not sort
        if (!sort || !col) return v.rows;
        const sign = sort.dir === "asc" ? 1 : -1;
        return [...v.rows].sort((a, b) => {
            const x = col.sortValue(a), y = col.sortValue(b);
            const d = typeof x === "number" && typeof y === "number" ? x - y : String(x).localeCompare(String(y), "de");
            return (d || 0) * sign; // -∞ − -∞ is NaN: equal
        });
    }, [v.rows, sort, cols]);

    const rowHeight = options.dense ? ROW_HEIGHT.dense : ROW_HEIGHT.normal;
    const scroller = useRef<HTMLDivElement | null>(null);
    // The table's own scroll area fills the rest of the window below its top
    // (with the page scrolled to the top), at least 360 px – so the sum row
    // is in view without scrolling the page when there is room.
    const height = useLayoutValue<number | undefined>(() => {
        const el = scroller.current;
        const page = el?.closest<HTMLElement>(".content");
        if (!el || !page) return undefined;
        const top = el.getBoundingClientRect().top - page.getBoundingClientRect().top + page.scrollTop;
        return Math.max(360, Math.round(page.clientHeight - top - (parseFloat(getComputedStyle(page).paddingBottom) || 0)));
    }, undefined);

    const virtual = useVirtualizer({
        count: rows.length,
        getScrollElement: () => scroller.current,
        estimateSize: () => rowHeight,
        overscan: 12,
        scrollMargin: HEAD_HEIGHT,
        scrollPaddingStart: HEAD_HEIGHT,
        scrollPaddingEnd: options.sum ? SUM_ROW : 0,
        getItemKey: i => rows[i].id,
    });
    const items = virtual.getVirtualItems();
    const nav = useRowNav(rows.map(r => r.id), {
        open: v.open, toggle: v.toggleOne,
        scrollTo: i => virtual.scrollToIndex(i, {align: "auto"}),
        rendered: items.map(i => rows[i.index].id),
        firstVisible: rows[firstInView(items, (virtual.scrollOffset ?? 0) + HEAD_HEIGHT) ?? -1]?.id,
    });

    const top = items.length ? items[0].start - HEAD_HEIGHT : 0;
    const bottom = items.length ? virtual.getTotalSize() - (items[items.length - 1].end - HEAD_HEIGHT) : 0;
    const width = 36 + cols.reduce((s, c) => s + c.width, 0);
    const span = cols.length + 1;
    const headRows = 2;

    // Group header: the invoice columns, then one group per payer, then the note.
    const groups: {label: ReactNode; span: number; payer?: number}[] = [];
    for (const c of cols) {
        const last = groups[groups.length - 1];
        if (last && last.payer === c.payer) last.span++;
        else groups.push({label: c.payer === undefined ? "" : payers[c.payer].name, span: 1, payer: c.payer});
    }

    function toggleSort(id: string) {
        setSort(s => (s?.id !== id ? {id, dir: "asc"} : s.dir === "asc" ? {id, dir: "desc"} : null));
    }

    const sizes = {"--row-h": `${rowHeight}px`, "--group-h": `${GROUP_ROW}px`, "--cols-h": `${COLUMN_ROW}px`, "--sum-h": `${SUM_ROW}px`};
    return (
        <div ref={scroller} className={`grid-wrap ${options.dense ? "dense" : ""}`} style={{...sizes, maxHeight: height} as CSSProperties}>
            <table className="data-grid" style={{width}} aria-rowcount={rows.length + headRows + (options.sum ? 1 : 0)}
                   aria-label="Rechnungen, Tabellenansicht">
                <colgroup>
                    <col style={{width: 36}}/>
                    {cols.map(c => <col key={c.id} style={{width: c.width}}/>)}
                </colgroup>
                <thead>
                <tr className="groups" aria-rowindex={1}>
                    <th className="sticky-a" aria-hidden="true"/>
                    {/* the first column is sticky on its own; a wide sticky cell would cover the payer headers */}
                    <th className="sticky-b" aria-hidden="true"/>
                    {groups.map((g, i) => {
                        const n = i === 0 ? g.span - 1 : g.span;
                        return n > 0 && (
                            <th key={i} colSpan={n} scope={g.payer === undefined ? undefined : "colgroup"}
                                className={g.payer !== undefined ? `group ${payerClass(g.payer)}` : ""}>{g.label}</th>
                        );
                    })}
                </tr>
                <tr className="cols" aria-rowindex={2}>
                    <th className="sticky-a"><span className="sr-only">Auswahl</span></th>
                    {cols.map((c, i) => {
                        const dir = sort?.id === c.id ? sort.dir : null;
                        const name = c.payer !== undefined ? `${payers[c.payer].name}: ${c.label}` : c.label;
                        return (
                            <th key={c.id} scope="col" className={`${i === 0 ? "sticky-b" : ""} ${c.right ? "right" : ""}`}
                                aria-sort={dir === "asc" ? "ascending" : dir === "desc" ? "descending" : "none"}>
                                <button type="button" className="sort" aria-label={name} title={`Nach ${name} sortieren`} onClick={() => toggleSort(c.id)}>
                                    {c.label}<span className="sort-mark" aria-hidden="true">{dir === "asc" ? " ↑" : dir === "desc" ? " ↓" : ""}</span>
                                </button>
                            </th>
                        );
                    })}
                </tr>
                </thead>
                <tbody {...nav.containerProps} ref={el => {
                    nav.container.current = el;
                }}>
                {top > 0 && <tr className="spacer" aria-hidden="true"><td colSpan={span} style={{height: top}}/></tr>}
                {items.map(item => {
                    const r = rows[item.index];
                    return (
                        <tr key={r.id} className={`${item.index % 2 ? "zebra" : ""} ${v.selected.has(r.id) ? "sel" : ""}`} aria-rowindex={item.index + 1 + headRows}
                            {...nav.item(r.id)} onDoubleClick={e => opensInvoice(e.target as HTMLElement, true) && v.open(r.id)}>
                            <td className="sticky-a">
                                <input type="checkbox" aria-label={selectLabel(r)} checked={v.selected.has(r.id)} onChange={() => v.toggleOne(r.id)}/>
                            </td>
                            {cols.map((c, i) => {
                                const cell = c.cell(r);
                                return (
                                    <td key={c.id} title={cell.title} className={`${i === 0 ? "sticky-b" : ""} ${c.right ? "right num" : ""} ${cell.className ?? ""}`}>
                                        {cell.text}
                                    </td>
                                );
                            })}
                        </tr>
                    );
                })}
                {bottom > 0 && <tr className="spacer" aria-hidden="true"><td colSpan={span} style={{height: bottom}}/></tr>}
                </tbody>
                {options.sum && (
                    <tfoot>
                    <tr aria-rowindex={rows.length + headRows + 1}>
                        <td className="sticky-a"/>
                        {cols.map((c, i) => (
                            <td key={c.id} className={`${i === 0 ? "sticky-b" : ""} ${c.right ? "right num" : ""}`}>
                                {i === 0 ? "Summe" : c.sum?.(rows) ?? ""}
                            </td>
                        ))}
                    </tr>
                    </tfoot>
                )}
            </table>
        </div>
    );
}

/** "Spalten 12 von 18": shows and hides columns (per payer: all payers at once), and the sum row. */
export function ColumnChooser({payers, options, onChange}: {payers: number; options: TableOptions; onChange: (o: TableOptions) => void}) {
    const [open, setOpen] = useState(false);
    const count = (k: ColumnKind) => (PAYER_KINDS.includes(k) ? payers : 1);
    const total = COLUMN_KINDS.reduce((s, k) => s + count(k), 0);
    const shown = COLUMN_KINDS.filter(k => k === "service" || !options.hidden.includes(k)).reduce((s, k) => s + count(k), 0);
    const toggle = (k: ColumnKind) => onChange({...options, hidden: options.hidden.includes(k) ? options.hidden.filter(h => h !== k) : [...options.hidden, k]});
    return (
        <span className="rel">
            <button type="button" aria-expanded={open} onClick={() => setOpen(!open)}>Spalten {shown} von {total} ▾</button>
            {open && <ColumnMenu options={options} toggle={toggle} onSum={sum => onChange({...options, sum})} onClose={() => setOpen(false)}/>}
        </span>
    );
}

function ColumnMenu({options, toggle, onSum, onClose}: {
    options: TableOptions;
    toggle: (k: ColumnKind) => void;
    onSum: (on: boolean) => void;
    onClose: () => void;
}) {
    const ref = useDialog<HTMLDivElement>(onClose);
    const box = (k: ColumnKind) => (
        <label key={k} className="inline">
            <input type="checkbox" checked={k === "service" || !options.hidden.includes(k)} disabled={k === "service"} onChange={() => toggle(k)}/>
            {COLUMN_LABELS[k]}
        </label>
    );
    return (
        <div ref={ref} className="popover column-menu" role="dialog" aria-label="Spalten" tabIndex={-1}>
            <div className="column-menu-grid">
                <div>
                    <strong className="small">Rechnung</strong>
                    {COLUMN_KINDS.filter(k => !PAYER_KINDS.includes(k)).map(box)}
                </div>
                <div>
                    <strong className="small">Je Kostenträger</strong>
                    {PAYER_KINDS.map(box)}
                    <hr/>
                    <label className="inline"><input type="checkbox" checked={options.sum} onChange={e => onSum(e.target.checked)}/>Summenzeile</label>
                </div>
            </div>
            <div className="actions mt-3"><span className="spacer"/><button className="primary" onClick={onClose}>Fertig</button></div>
        </div>
    );
}
