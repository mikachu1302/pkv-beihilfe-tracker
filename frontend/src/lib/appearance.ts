// The appearance settings of this device as the backend keeps them (design,
// default invoice view, table options, the one-time design hint). Loaded once
// at start (App: AppearanceSync); pages read them with useAppearance.
import {useSyncExternalStore} from "react";
import type {AppearanceDTO} from "../api";
import {useDesign} from "./design";

let state: AppearanceDTO | null = null;
const listeners = new Set<() => void>();

/** Replaces the settings (after loading them from the backend). */
export function setAppearance(a: AppearanceDTO | null) {
    state = a;
    listeners.forEach(l => l());
}

/** Changes some of the settings here; saving them is up to the caller. */
export function patchAppearance(p: Partial<AppearanceDTO>) {
    if (state) setAppearance({...state, ...p});
}

const subscribe = (l: () => void) => {
    listeners.add(l);
    return () => {
        listeners.delete(l);
    };
};

/** The settings, or null while they are loading. */
export function useAppearance(): AppearanceDTO | null {
    return useSyncExternalStore(subscribe, () => state);
}

/** The views of the invoice list (the new views only in the new design). */
export type InvoiceView = "detail" | "compact" | "table" | "classic";

export const VIEW_LABELS: Record<InvoiceView, string> = {detail: "Detail", compact: "Kompakt", table: "Tabelle", classic: "Klassische Liste"};
export const VIEW_ORDER: InvoiceView[] = ["detail", "compact", "table", "classic"];

const isView = (v: unknown): v is InvoiceView => VIEW_ORDER.includes(v as InvoiceView);

// The view chosen in the list: until the app is closed, or until the default
// view is changed in the settings.
let sessionView: InvoiceView | null = null;

/** Chooses the view in the list for this session; null goes back to the default view. */
export function setSessionView(v: InvoiceView | null) {
    sessionView = v;
    listeners.forEach(l => l());
}

/** The view the invoice list shows: always the classic list in the classic design. */
export function useInvoiceView(): InvoiceView {
    const design = useDesign();
    const a = useAppearance();
    const chosen = useSyncExternalStore(subscribe, () => sessionView);
    if (design === "classic") return "classic";
    return chosen ?? (isView(a?.view) ? a.view : "detail");
}
