// What the invoice views share: the props they get from the list page, the
// popovers for refund amount and Klärungsbedarf, status texts and the
// keyboard navigation between invoices.
import {useLayoutEffect, useRef, useState} from "react";
import type {KeyboardEvent as ReactKeyboardEvent} from "react";
import {useVirtualizer} from "@tanstack/react-virtual";
import type {ClaimViewDTO, InvoiceRowDTO, MarkChangeDTO, PayerDTO} from "../../api";
import {date, eur, num, toInput, today} from "../../lib/format";
import {useDialog} from "../../lib/useDialog";

/** An open popover: which claim, and refund amount or Klärungsbedarf. */
export interface Pop {
    rowId: string;
    payerId: string;
    kind: "refund" | "clarify";
}

/** What every view gets from the list page: the visible invoices and the actions on them. */
export interface ViewProps {
    /** The invoices passing filter and search, in list order. */
    rows: InvoiceRowDTO[];
    /** One column (group) per payer that occurs in the year, in settings order. */
    payers: PayerDTO[];
    selected: Set<string>;
    toggleOne: (id: string) => void;
    allSelected: boolean;
    toggleAll: () => void;
    pop: Pop | null;
    setPop: (p: Pop | null) => void;
    change: (ch: MarkChangeDTO) => Promise<void>;
    toggleRefunded: (r: InvoiceRowDTO, c: ClaimViewDTO, on: boolean) => void;
    toggleClarify: (r: InvoiceRowDTO, c: ClaimViewDTO) => void;
    /** Opens the invoice form. */
    open: (id: string) => void;
}

export function claimOf(r: InvoiceRowDTO, payerId: string): ClaimViewDTO | undefined {
    return (r.claims ?? []).find(c => c.payerId === payerId);
}

export type Tone = "warn" | "info" | "good" | "bad" | "muted";

/** Short status of a claim for pills and dots: Klärungsbedarf first, then the status. */
export function claimState(c: ClaimViewDTO): {label: string; tone: Tone} {
    if (c.clarify.done) return {label: "Klärung", tone: "bad"};
    switch (c.status) {
        case "submitted":
            return {label: "eingereicht", tone: "info"};
        case "refunded":
            return {label: "erstattet", tone: "good"};
        case "held":
            return {label: "zurückgehalten", tone: "muted"};
        case "skipped":
            return {label: "verzichtet", tone: "muted"};
        default:
            return {label: "nicht eingereicht", tone: "warn"};
    }
}

/** Colour class of the n-th payer column (payer-1, payer-2; further ones in the text colour). */
export function payerClass(index: number): string {
    return index < 2 ? `payer-${index + 1}` : "";
}

/** Text of the deadline when it is near or missed, else "". */
export function deadlineText(c: ClaimViewDTO): string {
    if (!c.deadline || c.deadlineDays > 90) return "";
    return c.deadlineDays < 0 ? `Frist abgelaufen (${date(c.deadline)})` : `Frist bis ${date(c.deadline)}`;
}

/** The marks of a claim as MarkChangeDTO without mark and on. */
export function markBase(r: InvoiceRowDTO, c: ClaimViewDTO) {
    return {invoiceId: r.id, payerId: c.payerId, note: "", amount: ""};
}

/** The popover of one claim if it is open (refund amount or Klärungsbedarf). */
export function ClaimPopover({row, claim, pop, setPop, change}: {
    row: InvoiceRowDTO;
    claim: ClaimViewDTO;
    pop: Pop | null;
    setPop: (p: Pop | null) => void;
    change: (ch: MarkChangeDTO) => Promise<void>;
}) {
    if (pop?.rowId !== row.id || pop.payerId !== claim.payerId) return null;
    const base = markBase(row, claim);
    if (pop.kind === "refund") {
        return <RefundPopover claim={claim} onCancel={() => setPop(null)}
                              onSave={(amount, d) => change({...base, mark: "refunded", on: true, date: d, amount})}/>;
    }
    return <ClarifyPopover onCancel={() => setPop(null)}
                           onSave={note => change({...base, mark: "clarify", on: true, date: "", note})}/>;
}

function RefundPopover({claim, onSave, onCancel}: {claim: ClaimViewDTO; onSave: (amount: string, date: string) => void; onCancel: () => void}) {
    const open = num(claim.expected) - num(claim.actual);
    const [amount, setAmount] = useState(toInput((open > 0 ? open : num(claim.expected)).toFixed(2)));
    const [d, setD] = useState(today());
    const ref = useDialog<HTMLDivElement>(onCancel);
    return (
        <div ref={ref} className="popover" role="dialog" aria-label={`Erstattung ${claim.payerName}`} tabIndex={-1}>
            <div className="grid two">
                <div className="field">
                    <label htmlFor="refund-amount">Erstattet (€)</label>
                    <input id="refund-amount" value={amount} onChange={e => setAmount(e.target.value)}
                           onKeyDown={e => e.key === "Enter" && onSave(amount, d)}/>
                </div>
                <div className="field">
                    <label htmlFor="refund-date">am</label>
                    <input id="refund-date" type="date" value={d} onChange={e => setD(e.target.value)}/>
                </div>
            </div>
            <div className="hint small muted mt-1">SOLL {eur(claim.expected)}</div>
            <div className="actions mt-3">
                <span className="spacer"/>
                <button onClick={onCancel}>Abbrechen</button>
                <button className="primary" onClick={() => onSave(amount, d)}>Übernehmen</button>
            </div>
        </div>
    );
}

function ClarifyPopover({onSave, onCancel}: {onSave: (note: string) => void; onCancel: () => void}) {
    const [note, setNote] = useState("");
    const ref = useDialog<HTMLDivElement>(onCancel);
    return (
        <div ref={ref} className="popover" role="dialog" aria-label="Klärungsbedarf" tabIndex={-1}>
            <div className="field">
                <label htmlFor="clarify-note">Klärungsbedarf – Grund</label>
                <input id="clarify-note" value={note} placeholder="z. B. Steigerungssatz gekürzt"
                       onChange={e => setNote(e.target.value)} onKeyDown={e => e.key === "Enter" && onSave(note)}/>
            </div>
            <div className="actions mt-3">
                <span className="spacer"/>
                <button onClick={onCancel}>Abbrechen</button>
                <button className="primary" onClick={() => onSave(note)}>Markieren</button>
            </div>
        </div>
    );
}

/** Clarify flag button of a claim (⚑): on, suggested (IST differs from SOLL) or off. */
export function ClarifyFlag({claim: c, onClick}: {claim: ClaimViewDTO; onClick: () => void}) {
    return (
        <button type="button" className={`flag ${c.clarify.done ? "on" : c.suggestClarify ? "suggest" : ""}`}
                aria-pressed={c.clarify.done}
                aria-label={`Klärungsbedarf ${c.payerName}`}
                title={c.clarify.done ? `Klärungsbedarf seit ${date(c.clarify.date)}${c.clarify.note ? `: ${c.clarify.note}` : ""}`
                    : c.suggestClarify ? "IST weicht vom SOLL ab – Klärungsbedarf markieren?" : "Klärungsbedarf markieren"}
                onClick={onClick}>⚑</button>
    );
}

/** Whether a click inside an invoice should open it: not on controls, links, popovers or while selecting text. */
export function opensInvoice(target: HTMLElement): boolean {
    if (target.closest("input, button, a, label, select, textarea, .popover, [role=dialog], .markbox, .no-open")) return false;
    return !window.getSelection()?.toString();
}

/**
 * Keyboard navigation between invoices (roving tabindex): ↑/↓ (and Home/End)
 * move the focus, Enter opens, Space ticks the invoice for bulk actions. Only
 * when the invoice itself has the focus – in a checkbox or field the keys
 * keep their usual meaning. scrollTo brings a row into view first
 * (virtualised lists render only the visible rows).
 */
export function useRowNav(ids: string[], {open, toggle, scrollTo, keys, rendered}: {
    open: (id: string) => void;
    toggle: (id: string) => void;
    scrollTo?: (index: number) => void;
    /** The ids rendered now (virtualised lists): the tab stop moves to the first of them when the current row is not. */
    rendered?: string[];
    /** Further keys of a view (e.g. → to expand); return true when used. */
    keys?: (e: ReactKeyboardEvent, id: string) => boolean;
}) {
    const [cursor, setCursor] = useState<string | null>(null);
    const container = useRef<HTMLElement | null>(null);
    const pending = useRef<string | null>(null);
    const current = cursor !== null && ids.includes(cursor) ? cursor : ids[0] ?? null;
    const tabStop = rendered && current !== null && !rendered.includes(current) ? rendered[0] ?? current : current;

    // Focus the row moved to as soon as it is rendered.
    useLayoutEffect(() => {
        const id = pending.current;
        if (id === null || !container.current) return;
        const el = [...container.current.querySelectorAll<HTMLElement>("[data-row-id]")].find(e => e.dataset.rowId === id);
        if (el) {
            pending.current = null;
            el.focus({preventScroll: !!scrollTo});
        }
    });

    function go(index: number) {
        if (ids.length === 0) return;
        const i = Math.max(0, Math.min(ids.length - 1, index));
        setCursor(ids[i]);
        pending.current = ids[i];
        scrollTo?.(i);
    }

    function onKeyDown(e: ReactKeyboardEvent, id: string) {
        if (e.target !== e.currentTarget || e.altKey || e.ctrlKey || e.metaKey) return;
        const i = ids.indexOf(id);
        let used = true;
        switch (e.key) {
            case "ArrowDown":
                go(i + 1);
                break;
            case "ArrowUp":
                go(i - 1);
                break;
            case "Home":
                go(0);
                break;
            case "End":
                go(ids.length - 1);
                break;
            case "Enter":
                open(id);
                break;
            case " ":
                toggle(id);
                break;
            default:
                used = keys?.(e, id) ?? false;
        }
        if (used) e.preventDefault();
    }

    return {
        container,
        current,
        item: (id: string) => ({
            "data-row-id": id,
            tabIndex: id === tabStop ? 0 : -1,
            onKeyDown: (e: ReactKeyboardEvent) => onKeyDown(e, id),
            onFocus: (e: React.FocusEvent) => {
                if (e.target === e.currentTarget) setCursor(id);
            },
        }),
    };
}

/**
 * Renders only the visible items of a list that scrolls with the page (the
 * `.content` element) – Detail cards and Kompakt lines. Items may differ in
 * height: they are measured (put `measure` as ref and `data-index` on each).
 * Without a scrolling page (tests, unusual layouts) all items are rendered.
 */
export function usePageVirtualizer(count: number, {estimateSize, getItemKey, gap = 0}: {
    estimateSize: (index: number) => number;
    getItemKey: (index: number) => string;
    /** Space between two rendered items (CSS margin between them, not on the spacers). */
    gap?: number;
}) {
    const list = useRef<HTMLElement | null>(null);
    const [page, setPage] = useState<HTMLElement | null>(null);
    const [margin, setMargin] = useState(0);

    // Where the list starts below the top of the page. Measured after every
    // render (the bulk bar or notices above may have changed it, and every
    // scroll renders again); state is only set when the value changed, so
    // this settles after one pass.
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

    const virtual = useVirtualizer({count, getScrollElement: () => page, estimateSize, overscan: 8, scrollMargin: margin, getItemKey, gap});
    const items = page ? virtual.getVirtualItems() : Array.from({length: count}, (_, index) => ({index, start: margin, end: margin}));
    return {
        list,
        items,
        /** Heights of the spacers that stand in for the items above and below. */
        top: page && items.length ? items[0].start - margin : 0,
        bottom: page && items.length ? virtual.getTotalSize() - (items[items.length - 1].end - margin) : 0,
        measure: page ? virtual.measureElement : undefined,
        scrollTo: (index: number) => virtual.scrollToIndex(index, {align: "auto"}),
    };
}

