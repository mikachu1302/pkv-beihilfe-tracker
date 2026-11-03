// The appearance settings of this device as the backend keeps them (design,
// default invoice view, table options, the one-time design hint). Loaded at
// start and again when the data folder changes (App: DeviceSync); pages
// read them with useAppearance. The lists and defaults here match
// internal/service/appearance.go.
import type {AppearanceDTO} from "../api";
import {useDesign} from "./design";
import {createStore, useStore} from "./store";

/** The views of the invoice list (the new views only in the new design); the first is the default. */
export type InvoiceView = "detail" | "compact" | "table" | "classic";

export const VIEW_LABELS: Record<InvoiceView, string> = {detail: "Detail", compact: "Kompakt", table: "Tabelle", classic: "Klassische Liste"};
export const VIEW_ORDER: InvoiceView[] = ["detail", "compact", "table", "classic"];

/** Column kinds of the table view, as kept in the table options (hidden columns). */
export const COLUMN_KINDS = ["service", "invoice", "provider", "category", "reference", "amount", "paid",
    "expected", "actual", "diff", "submitted", "refunded", "note"] as const;
export type ColumnKind = typeof COLUMN_KINDS[number];
/** The kinds that exist once per payer. */
export const PAYER_KINDS: readonly ColumnKind[] = ["expected", "actual", "diff", "submitted", "refunded"];

/** Options of the table view (the DTO's hidden list may be null; here it never is). */
export interface TableOptions {
    dense: boolean;
    sum: boolean;
    hidden: string[];
}

export const DEFAULT_APPEARANCE: AppearanceDTO = {
    theme: "", design: "new", designHint: false, view: VIEW_ORDER[0],
    table: {dense: false, sum: true, hidden: []},
};

const appearance = createStore<AppearanceDTO | null>(null);

/** Replaces the settings (after loading them from the backend). */
export function setAppearance(a: AppearanceDTO | null) {
    appearance.set(a);
}

/** Changes some of the settings here (on the defaults if they did not load); saving them is up to the caller. */
export function patchAppearance(p: Partial<AppearanceDTO>) {
    appearance.set({...(appearance.get() ?? DEFAULT_APPEARANCE), ...p});
}

/** The settings, or null while they are loading. */
export function useAppearance(): AppearanceDTO | null {
    return useStore(appearance);
}

const isView = (v: unknown): v is InvoiceView => VIEW_ORDER.includes(v as InvoiceView);

/** The default view of the invoice list in these settings. */
export function defaultView(a: AppearanceDTO | null): InvoiceView {
    return isView(a?.view) ? a.view : VIEW_ORDER[0];
}

/** The table options in these settings (the defaults while they are loading). */
export function tableOptions(a: AppearanceDTO | null): TableOptions {
    const t = a?.table ?? DEFAULT_APPEARANCE.table;
    return {...t, hidden: t.hidden ?? []};
}

// The view chosen in the list: until the app is closed, or until the default
// view is changed in the settings.
const sessionView = createStore<InvoiceView | null>(null);

/** Chooses the view in the list for this session; null goes back to the default view. */
export function setSessionView(v: InvoiceView | null) {
    sessionView.set(v);
}

/** The view the invoice list shows: always the classic list in the classic design. */
export function useInvoiceView(): InvoiceView {
    const design = useDesign();
    const a = useAppearance();
    const chosen = useStore(sessionView);
    if (design === "classic") return "classic";
    return chosen ?? defaultView(a);
}
