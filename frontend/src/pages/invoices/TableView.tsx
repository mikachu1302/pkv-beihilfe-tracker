// "Tabelle": every field as a column, like a spreadsheet – read only.
// Sorting by any column, a column chooser, dense or normal rows, a sum row.
// Only the visible rows are rendered (@tanstack/react-virtual): the header,
// the first columns and the sum row are sticky and stay outside the
// virtualised body, every row has the fixed height of its density.
import {useLayoutEffect, useMemo, useRef, useState} from "react";
import type {CSSProperties, ReactNode} from "react";
import {useVirtualizer} from "@tanstack/react-virtual";
import {Svc} from "../../api";
import type {ClaimViewDTO, InvoiceRowDTO, PayerDTO} from "../../api";
import {useToast} from "../../components/feedback";
import {patchAppearance, useAppearance} from "../../lib/appearance";
import {CATEGORY_LABELS, date, eur, num, plural, shortDate} from "../../lib/format";
import {useDialog} from "../../lib/useDialog";
import {claimOf, payerClass, useRowNav} from "./shared";
import type {ViewProps} from "./shared";

/** Column kinds, as kept in the table options (hidden columns). Per payer: expected … refunded. */
export const COLUMN_KINDS = ["service", "invoice", "provider", "category", "reference", "amount", "paid",
    "expected", "actual", "diff", "submitted", "refunded", "note"] as const;
export type ColumnKind = typeof COLUMN_KINDS[number];

export const COLUMN_LABELS: Record<ColumnKind, string> = {
    service: "Behandlung", invoice: "Rechnung", provider: "Behandler", category: "Kategorie", reference: "Re.-Nr.",
    amount: "Betrag", paid: "Bezahlt", expected: "SOLL", actual: "IST", diff: "Diff", submitted: "eingereicht",
    refunded: "erstattet", note: "Notiz",
};
const PAYER_KINDS: ColumnKind[] = ["expected", "actual", "diff", "submitted", "refunded"];

/** Row heights in px per density (the CSS sets the same). */
export const ROW_HEIGHT = {normal: 36, dense: 28};
const HEAD_HEIGHT = 64; // two header rows of 30 + 34 px
const SUM_HEIGHT = 36;

export interface TableOptions {
    dense: boolean;
    sum: boolean;
    hidden: string[];
}

const DEFAULT_OPTIONS: TableOptions = {dense: false, sum: true, hidden: []};

/** Options of the table view (device configuration); changes are saved right away. */
export function useTableOptions(): [TableOptions, (o: TableOptions) => void] {
    const toast = useToast();
    const a = useAppearance();
    const options = a ? {...a.table, hidden: a.table.hidden ?? []} : DEFAULT_OPTIONS;
    return [options, o => {
        patchAppearance({table: o});
        Svc.SetTableOptions(o).catch(toast.error);
    }];
}

interface Col {
    id: string;
    kind: ColumnKind;
    label: string;
    width: number;
    right?: boolean;
    payer?: number; // index of the payer column group
    sortValue: (r: InvoiceRowDTO) => string | number;
    cell: (r: InvoiceRowDTO) => {text: ReactNode; title?: string; tone?: string};
    sum?: (rows: InvoiceRowDTO[]) => ReactNode;
}

type Sort = {id: string; dir: "asc" | "desc"} | null;

const fmt = new Intl.NumberFormat("de-DE", {minimumFractionDigits: 2, maximumFractionDigits: 2});
const amount = (v: string | number) => fmt.format(num(v));
const dash = {text: "–", tone: "muted"};

function columns(payers: PayerDTO[]): Col[] {
    const base: Col[] = [
        {id: "service", kind: "service", label: "Behandlung", width: 96, sortValue: r => r.serviceDate,
            cell: r => ({text: shortDate(r.serviceDate) || "–", title: date(r.serviceDate)})},
        {id: "invoice", kind: "invoice", label: "Rechnung", width: 92, sortValue: r => r.invoiceDate,
            cell: r => ({text: shortDate(r.invoiceDate) || "–", title: date(r.invoiceDate)})},
        {id: "provider", kind: "provider", label: "Behandler", width: 220, sortValue: r => r.provider.toLocaleLowerCase("de"),
            cell: r => ({text: r.provider || "(ohne Namen)", title: r.provider, tone: "strong"}),
            sum: rows => plural(rows.length, "Rechnung", "Rechnungen")},
        {id: "category", kind: "category", label: "Kategorie", width: 110, sortValue: r => CATEGORY_LABELS[r.category] ?? r.category,
            cell: r => ({text: CATEGORY_LABELS[r.category] ?? r.category})},
        {id: "reference", kind: "reference", label: "Re.-Nr.", width: 110, sortValue: r => r.reference,
            cell: r => (r.reference ? {text: r.reference, title: r.reference} : dash)},
        {id: "amount", kind: "amount", label: "Betrag", width: 100, right: true, sortValue: r => num(r.amount),
            cell: r => ({text: amount(r.amount), title: eur(r.amount)}),
            sum: rows => amount(rows.reduce((s, r) => s + num(r.amount), 0))},
        {id: "paid", kind: "paid", label: "Bezahlt", width: 100, sortValue: r => r.paid.done ? r.paid.date : `~${r.due}`,
            cell: r => r.paid.done ? {text: shortDate(r.paid.date), title: `bezahlt am ${date(r.paid.date)}`}
                : r.due ? {text: `fällig ${shortDate(r.due).slice(0, 6)}`, title: `fällig am ${date(r.due)}`, tone: "warn-text"} : dash},
    ];
    const perPayer = payers.flatMap((p, pi): Col[] => {
        const c = (r: InvoiceRowDTO) => claimOf(r, p.id);
        const refunded = (cl?: ClaimViewDTO) => cl?.status === "refunded";
        const sumOf = (field: "expected" | "actual" | "diff", only?: (cl: ClaimViewDTO) => boolean) =>
            (rows: InvoiceRowDTO[]) => amount(rows.reduce((s, r) => {
                const cl = c(r);
                return cl && (!only || only(cl)) ? s + num(cl[field]) : s;
            }, 0));
        return [
            {id: `${p.id}:expected`, kind: "expected", label: "SOLL", width: 92, right: true, payer: pi,
                sortValue: r => num(c(r)?.expected), cell: r => (c(r) ? {text: amount(c(r)!.expected)} : dash), sum: sumOf("expected")},
            {id: `${p.id}:actual`, kind: "actual", label: "IST", width: 92, right: true, payer: pi,
                sortValue: r => (refunded(c(r)) ? num(c(r)!.actual) : -Infinity),
                cell: r => (refunded(c(r)) ? {text: amount(c(r)!.actual)} : dash), sum: sumOf("actual")},
            {id: `${p.id}:diff`, kind: "diff", label: "Diff", width: 86, right: true, payer: pi,
                sortValue: r => (refunded(c(r)) ? num(c(r)!.diff) : -Infinity),
                cell: r => {
                    const cl = c(r);
                    if (!refunded(cl)) return dash;
                    const d = num(cl!.diff);
                    return {text: amount(d), tone: d < 0 ? "neg" : d > 0 ? "pos" : ""};
                },
                sum: sumOf("diff", refunded)},
            {id: `${p.id}:submitted`, kind: "submitted", label: "eingereicht", width: 96, payer: pi,
                sortValue: r => c(r)?.submitted.date ?? "",
                cell: r => {
                    const cl = c(r);
                    if (!cl) return dash;
                    if (cl.submitted.done) return {text: shortDate(cl.submitted.date), title: `eingereicht am ${date(cl.submitted.date)}`};
                    if (cl.held) return {text: "zurückg.", title: "zurückgehalten", tone: "muted"};
                    if (cl.skip) return {text: "verzichtet", tone: "muted"};
                    return dash;
                }},
            {id: `${p.id}:refunded`, kind: "refunded", label: "erstattet", width: 96, payer: pi,
                sortValue: r => c(r)?.refunded.date ?? "",
                cell: r => {
                    const cl = c(r);
                    if (!cl) return dash;
                    const flag = cl.clarify.done ? " ⚑" : "";
                    const title = cl.clarify.done ? `Klärungsbedarf${cl.clarify.note ? `: ${cl.clarify.note}` : ""}` : undefined;
                    if (cl.refunded.done) return {text: shortDate(cl.refunded.date) + flag, title: title ?? `erstattet am ${date(cl.refunded.date)}`};
                    if (cl.longWait) return {text: `${Math.floor(cl.waitingDays / 7)} Wo. offen${flag}`, title: title ?? "Erstattung seit mehr als 6 Wochen offen", tone: "warn-text"};
                    return flag ? {text: flag.trim(), title, tone: "neg"} : dash;
                }},
        ];
    });
    const note: Col = {id: "note", kind: "note", label: "Notiz", width: 220, sortValue: r => r.note.toLocaleLowerCase("de"),
        cell: r => ({text: r.note, title: r.note})};
    return [...base, ...perPayer, note];
}

export function TableView(v: ViewProps & {options: TableOptions}) {
    const {payers, options} = v;
    const [sort, setSort] = useState<Sort>(null);
    const all = useMemo(() => columns(payers), [payers]);
    const cols = all.filter(c => c.kind === "service" || !options.hidden.includes(c.kind));

    const rows = useMemo(() => {
        if (!sort) return v.rows;
        const col = all.find(c => c.id === sort.id);
        if (!col) return v.rows;
        const sign = sort.dir === "asc" ? 1 : -1;
        return [...v.rows].sort((a, b) => {
            const x = col.sortValue(a), y = col.sortValue(b);
            const d = typeof x === "number" && typeof y === "number" ? x - y : String(x).localeCompare(String(y), "de");
            return d * sign;
        });
    }, [v.rows, sort, all]);

    const rowHeight = options.dense ? ROW_HEIGHT.dense : ROW_HEIGHT.normal;
    const scroller = useRef<HTMLDivElement | null>(null);
    const height = useFillHeight(scroller);
    const virtual = useVirtualizer({
        count: rows.length,
        getScrollElement: () => scroller.current,
        estimateSize: () => rowHeight,
        overscan: 12,
        scrollMargin: HEAD_HEIGHT,
        scrollPaddingStart: HEAD_HEIGHT,
        scrollPaddingEnd: options.sum ? SUM_HEIGHT : 0,
        getItemKey: i => rows[i].id,
    });
    const items = virtual.getVirtualItems();
    const nav = useRowNav(rows.map(r => r.id), {
        open: v.open, toggle: v.toggleOne,
        scrollTo: i => virtual.scrollToIndex(i, {align: "auto"}),
        rendered: items.map(i => rows[i.index].id),
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
        const payer = c.payer;
        if (last && last.payer === payer) last.span++;
        else groups.push({label: payer === undefined ? "" : payers[payer].name, span: 1, payer});
    }

    function toggleSort(id: string) {
        setSort(s => (s?.id !== id ? {id, dir: "asc"} : s.dir === "asc" ? {id, dir: "desc"} : null));
    }

    return (
        <div ref={scroller} className={`grid-wrap ${options.dense ? "dense" : ""}`}
             style={{"--row-h": `${rowHeight}px`, maxHeight: height} as CSSProperties}>
            <table className="data-grid" style={{width}} aria-rowcount={rows.length + headRows + (options.sum ? 1 : 0)}
                   aria-label="Rechnungen, Tabellenansicht">
                <colgroup>
                    <col style={{width: 36}}/>
                    {cols.map(c => <col key={c.id} style={{width: c.width}}/>)}
                </colgroup>
                <thead>
                <tr className="groups" aria-rowindex={1}>
                    <th className="sticky-a" aria-hidden="true"/>
                    {groups.map((g, i) => (
                        <th key={i} colSpan={g.span} scope={g.payer === undefined ? undefined : "colgroup"}
                            className={`${i === 0 ? "sticky-b" : ""} ${g.payer !== undefined ? `group ${payerClass(g.payer)}` : ""}`}>{g.label}</th>
                    ))}
                </tr>
                <tr className="cols" aria-rowindex={2}>
                    <th className="sticky-a"><span className="sr-only">Auswahl</span></th>
                    {cols.map((c, i) => {
                        const dir = sort?.id === c.id ? sort.dir : null;
                        return (
                            <th key={c.id} scope="col" className={`${i === 0 ? "sticky-b" : ""} ${c.right ? "right" : ""}`}
                                aria-sort={dir === "asc" ? "ascending" : dir === "desc" ? "descending" : "none"}>
                                <button type="button" className="sort" onClick={() => toggleSort(c.id)}
                                        title={`Nach ${c.payer !== undefined ? `${payers[c.payer].name}: ` : ""}${c.label} sortieren`}>
                                    {c.label}<span className="sort-mark" aria-hidden="true">{dir === "asc" ? " ↑" : dir === "desc" ? " ↓" : ""}</span>
                                </button>
                            </th>
                        );
                    })}
                </tr>
                </thead>
                <tbody ref={el => {
                    nav.container.current = el;
                }}>
                {top > 0 && <tr className="spacer" aria-hidden="true"><td colSpan={span} style={{height: top}}/></tr>}
                {items.map(item => {
                    const r = rows[item.index];
                    const selected = v.selected.has(r.id);
                    return (
                        <tr key={r.id} className={`${item.index % 2 ? "zebra" : ""} ${selected ? "sel" : ""}`} aria-rowindex={item.index + 1 + headRows}
                            {...nav.item(r.id)} onDoubleClick={() => v.open(r.id)}>
                            <td className="sticky-a"><input type="checkbox" aria-label={`${r.provider} auswählen`} checked={selected} onChange={() => v.toggleOne(r.id)}/></td>
                            {cols.map((c, i) => {
                                const cell = c.cell(r);
                                return (
                                    <td key={c.id} title={cell.title} className={`${i === 0 ? "sticky-b" : ""} ${c.right ? "right num" : ""} ${cell.tone ?? ""}`}>
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

/**
 * Height for the table's own scroll area: the rest of the window below its
 * top (with the page scrolled to the top), at least 360 px – so the sum row
 * is in view without scrolling the page when there is room.
 */
function useFillHeight(ref: React.RefObject<HTMLElement | null>): number | undefined {
    const [height, setHeight] = useState<number>();
    useLayoutEffect(() => {
        const update = () => {
            const el = ref.current;
            const page = el?.closest<HTMLElement>(".content");
            if (!el || !page) return;
            const top = el.getBoundingClientRect().top - page.getBoundingClientRect().top + page.scrollTop;
            const bottom = parseFloat(getComputedStyle(page).paddingBottom) || 0;
            setHeight(Math.max(360, page.clientHeight - top - bottom));
        };
        update();
        window.addEventListener("resize", update);
        return () => window.removeEventListener("resize", update);
    });
    return height;
}

/** "Spalten 12 von 18": shows and hides columns (per payer: all payers at once), and the sum row. */
export function ColumnChooser({payers, options, onChange}: {payers: number; options: TableOptions; onChange: (o: TableOptions) => void}) {
    const [open, setOpen] = useState(false);
    const count = (k: ColumnKind) => (PAYER_KINDS.includes(k) ? Math.max(payers, 0) : 1);
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
    const box = (k: ColumnKind, note = "") => (
        <label key={k} className="inline">
            <input type="checkbox" checked={k === "service" || !options.hidden.includes(k)} disabled={k === "service"} onChange={() => toggle(k)}/>
            {COLUMN_LABELS[k]}{note}
        </label>
    );
    return (
        <div ref={ref} className="popover column-menu" role="dialog" aria-label="Spalten" tabIndex={-1}>
            <div className="column-menu-grid">
                <div>
                    <strong className="small">Rechnung</strong>
                    {(["service", "invoice", "provider", "category", "reference", "amount", "paid", "note"] as ColumnKind[]).map(k => box(k))}
                </div>
                <div>
                    <strong className="small">Je Kostenträger</strong>
                    {PAYER_KINDS.map(k => box(k))}
                    <hr/>
                    <label className="inline"><input type="checkbox" checked={options.sum} onChange={e => onSum(e.target.checked)}/>Summenzeile</label>
                </div>
            </div>
            <div className="actions mt-3"><span className="spacer"/><button className="primary" onClick={onClose}>Fertig</button></div>
        </div>
    );
}
