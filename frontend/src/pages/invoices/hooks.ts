// Keyboard navigation between invoices and rendering only the visible ones.
import {useCallback, useEffect, useLayoutEffect, useRef, useState} from "react";
import type {FocusEvent, KeyboardEvent} from "react";
import {useVirtualizer} from "@tanstack/react-virtual";
import type {ViewProps} from "./model";

/**
 * A value read from the layout after every render – positions that change
 * when content above changes (notices, the bulk bar, cards that finish
 * loading) without this component rendering for it; every scroll of a
 * virtualised list renders again, and so does a window resize. State is only
 * set when the value changed, so this settles after one pass.
 */
export function useLayoutValue<T>(read: () => T, initial: T): T {
    const [value, setValue] = useState(initial);
    const [, setTick] = useState(0);
    useEffect(() => {
        const resize = () => setTick(t => t + 1);
        window.addEventListener("resize", resize);
        return () => window.removeEventListener("resize", resize);
    }, []);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- runs after every render on purpose (see above)
    useLayoutEffect(() => {
        const v = read();
        setValue(old => (Object.is(old, v) ? old : v));
    });
    return value;
}

/**
 * Keyboard navigation between invoices (roving tabindex): ↑/↓, PageUp/PageDown
 * and Home/End move the focus, Enter opens, Space ticks the invoice for bulk
 * actions. Only when the invoice itself has the focus – in a checkbox or
 * field the keys keep their usual meaning. scrollTo brings a row into view
 * first (virtualised lists render only the visible rows).
 *
 * Virtualised lists pass the rendered ids and the first visible one: the tab
 * stop moves to a visible row when the current one is scrolled away, and when
 * the focused row is unmounted by scrolling, the focus goes to the list
 * itself (containerProps), where the keys go on from the current row.
 */
export function useRowNav(ids: string[], {open, toggle, scrollTo, keys, rendered, firstVisible, pageStep = 10}: {
    open: (id: string) => void;
    toggle: (id: string) => void;
    scrollTo?: (index: number) => void;
    /** Further keys of a view (e.g. → to expand); return true when used. */
    keys?: (e: KeyboardEvent, id: string) => boolean;
    /** The ids rendered now (virtualised lists). */
    rendered?: string[];
    /** The first id in view (virtualised lists). */
    firstVisible?: string;
    /** Rows moved by PageUp/PageDown. */
    pageStep?: number;
}) {
    const [cursor, setCursor] = useState<string | null>(null);
    const container = useRef<HTMLElement | null>(null);
    const pending = useRef<string | null>(null);
    const focusWithin = useRef(false);
    const current = cursor !== null && ids.includes(cursor) ? cursor : ids[0] ?? null;
    const tabStop = rendered && current !== null && !rendered.includes(current) ? firstVisible ?? rendered[0] ?? current : current;

    useLayoutEffect(() => {
        const list = container.current;
        if (!list) return;
        // Focus the row moved to as soon as it is rendered.
        const id = pending.current;
        const el = id === null ? undefined : [...list.querySelectorAll<HTMLElement>("[data-row-id]")].find(e => e.dataset.rowId === id);
        if (el) {
            pending.current = null;
            el.focus({preventScroll: !!scrollTo});
        } else if (focusWithin.current && (document.activeElement === document.body || document.activeElement === null)) {
            // The focused row was unmounted (scrolled out of the rendered range).
            list.focus({preventScroll: true});
        }
    });

    // A click elsewhere ends the keyboard session in the list.
    useEffect(() => {
        const down = (e: PointerEvent) => {
            if (!container.current?.contains(e.target as Node)) focusWithin.current = false;
        };
        document.addEventListener("pointerdown", down, true);
        return () => document.removeEventListener("pointerdown", down, true);
    }, []);

    function go(index: number) {
        if (ids.length === 0) return;
        const i = Math.max(0, Math.min(ids.length - 1, index));
        setCursor(ids[i]);
        pending.current = ids[i];
        scrollTo?.(i);
    }

    function handle(e: KeyboardEvent, id: string) {
        if (e.altKey || e.ctrlKey || e.metaKey) return;
        const i = ids.indexOf(id);
        const moves: Record<string, number> = {
            ArrowDown: i + 1, ArrowUp: i - 1, PageDown: i + pageStep, PageUp: i - pageStep, Home: 0, End: ids.length - 1,
        };
        if (e.key in moves) go(moves[e.key]);
        else if (e.key === "Enter") open(id);
        else if (e.key === " ") toggle(id);
        else if (!keys?.(e, id)) return;
        e.preventDefault();
    }

    return {
        container,
        /** For the element that holds the rows: takes the focus when a focused row is unmounted. */
        containerProps: {
            tabIndex: -1,
            onKeyDown: (e: KeyboardEvent) => {
                if (e.target === e.currentTarget && current !== null) handle(e, current);
            },
            onFocus: () => {
                focusWithin.current = true;
            },
            onBlur: (e: FocusEvent) => {
                const to = e.relatedTarget as Node | null;
                if (to && !e.currentTarget.contains(to)) focusWithin.current = false;
            },
        },
        item: (id: string) => ({
            "data-row-id": id,
            tabIndex: id === tabStop ? 0 : -1,
            onKeyDown: (e: KeyboardEvent) => {
                if (e.target === e.currentTarget) handle(e, id);
            },
            onFocus: (e: FocusEvent) => {
                if (e.target === e.currentTarget) setCursor(id);
            },
        }),
    };
}

/** The props useRowNav gives each row. */
export type RowNavItem = ReturnType<ReturnType<typeof useRowNav>["item"]>;

/** The first of the rendered items that reaches below offset (the top edge of the visible area). */
export function firstInView<T extends {index: number; end: number}>(items: T[], offset: number): number | undefined {
    return (items.find(i => i.end > offset) ?? items[0])?.index;
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
    // The page is there already when the list opens (no first render of all items).
    const page = useLayoutValue(() => list.current?.closest<HTMLElement>(".content") ?? null,
        typeof document === "undefined" ? null : document.querySelector<HTMLElement>("main.content"));
    // Where the list starts below the top of the page …
    const margin = useLayoutValue(() => {
        const el = list.current;
        return el && page ? Math.round(el.getBoundingClientRect().top - page.getBoundingClientRect().top + page.scrollTop) : 0;
    }, 0);
    // … and the sticky bulk bar: rows scrolled to by keyboard must not end up below it.
    const barHeight = useLayoutValue(() => {
        const bar = page?.querySelector<HTMLElement>(".bulkbar");
        return bar ? bar.offsetHeight + 12 : 0;
    }, 0);

    const virtual = useVirtualizer({count, getScrollElement: () => page, estimateSize, overscan: 8, scrollMargin: margin, getItemKey, gap,
        scrollPaddingStart: barHeight});
    const items = page ? virtual.getVirtualItems() : Array.from({length: count}, (_, index) => ({index, start: margin, end: margin}));
    return {
        list,
        items,
        /** Index of the first item in view. */
        firstVisible: firstInView(items, (virtual.scrollOffset ?? 0) + barHeight),
        /** Heights of the spacers that stand in for the items above and below. */
        top: page && items.length ? items[0].start - margin : 0,
        bottom: page && items.length ? virtual.getTotalSize() - (items[items.length - 1].end - margin) : 0,
        measure: page ? virtual.measureElement : undefined,
        scrollTo: (index: number) => virtual.scrollToIndex(index, {align: "auto"}),
    };
}

/**
 * Keyboard navigation for a list rendered with usePageVirtualizer (Detail,
 * Kompakt): the tab stop follows what is rendered, an open popover closes
 * when its invoice is no longer rendered (scrolled away, filtered out – it
 * would otherwise come back open later and take the focus). listProps go on
 * the element that holds the items.
 */
export function useListNav(v: ViewProps, pv: ReturnType<typeof usePageVirtualizer>, opts: {
    keys?: (e: KeyboardEvent, id: string) => boolean;
    pageStep?: number;
} = {}) {
    const rendered = pv.items.map(i => v.rows[i.index].id);
    const popGone = v.pop !== null && !rendered.includes(v.pop.rowId);
    const {setPop} = v;
    useEffect(() => {
        if (popGone) setPop(null);
    }, [popGone, setPop]);

    const nav = useRowNav(v.rows.map(r => r.id), {
        ...opts, open: v.open, toggle: v.toggleOne, scrollTo: pv.scrollTo, rendered,
        firstVisible: pv.firstVisible === undefined ? undefined : v.rows[pv.firstVisible]?.id,
    });
    const {list} = pv;
    const {container} = nav;
    const ref = useCallback((el: HTMLElement | null) => {
        list.current = el;
        container.current = el;
    }, [list, container]);
    return {...nav, listProps: {...nav.containerProps, ref}};
}
