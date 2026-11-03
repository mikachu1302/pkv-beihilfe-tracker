// The colour scheme of this device: like the system, light or dark. The
// choice is kept in the device configuration (backend) and, to avoid a flash
// at start, in localStorage; the CSS reads data-theme on the root element.
import {useSyncExternalStore} from "react";

export type Theme = "" | "light" | "dark";

/** In the order shown in the settings. */
export const THEME_LABELS: Record<Theme, string> = {light: "Hell", dark: "Dunkel", "": "Automatisch"};
export const THEME_ORDER: Theme[] = ["light", "dark", ""];

const KEY = "pkv.theme";
const listeners = new Set<() => void>();
let current: Theme = stored();

function stored(): Theme {
    try {
        const v = localStorage.getItem(KEY);
        return v === "light" || v === "dark" ? v : "";
    } catch {
        return "";
    }
}

function systemDark(): boolean {
    return typeof window.matchMedia === "function" && window.matchMedia("(prefers-color-scheme: dark)").matches;
}

/** Sets data-theme to the scheme in effect ("dark" or "light"). */
function paint() {
    const dark = current === "dark" || (current === "" && systemDark());
    document.documentElement.dataset.theme = dark ? "dark" : "light";
    listeners.forEach(l => l());
}

/** Applies a theme on this device (without saving it in the backend). */
export function applyTheme(t: Theme) {
    current = t;
    try {
        if (t) localStorage.setItem(KEY, t);
        else localStorage.removeItem(KEY);
    } catch {
        // the backend keeps it
    }
    paint();
}

/** Called once at start: the stored choice now, and following the system when it changes. */
export function initTheme() {
    paint();
    if (typeof window.matchMedia === "function") {
        window.matchMedia("(prefers-color-scheme: dark)").addEventListener?.("change", () => current === "" && paint());
    }
}

const subscribe = (l: () => void) => {
    listeners.add(l);
    return () => {
        listeners.delete(l);
    };
};

/** The theme chosen on this device ("" = automatic). */
export function useTheme(): Theme {
    return useSyncExternalStore(subscribe, () => current);
}

/** Whether the dark scheme is in effect right now (chosen, or automatic and the system is dark). */
export function useDarkActive(): boolean {
    return useSyncExternalStore(subscribe, () => document.documentElement.dataset.theme === "dark");
}
