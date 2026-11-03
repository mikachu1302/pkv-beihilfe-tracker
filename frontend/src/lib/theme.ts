// The colour scheme of this device: like the system, light or dark. The
// choice is kept in the device configuration (backend) and, to paint the
// right one at start, in localStorage (public/boot.js reads it before the
// first paint); the CSS reads data-theme on the root element.
import {useSyncExternalStore} from "react";
import {createStore, loadLocal, saveLocal, useStore} from "./store";

export type Theme = "" | "light" | "dark";

/** In the order shown in the settings. */
export const THEME_LABELS: Record<Theme, string> = {light: "Hell", dark: "Dunkel", "": "Automatisch"};
export const THEME_ORDER: Theme[] = ["light", "dark", ""];

const KEY = "pkv.theme";

const parse = (v: unknown): Theme => (v === "light" || v === "dark" ? v : "");
const theme = createStore<Theme>(parse(loadLocal(KEY)));

function systemDark(): boolean {
    return typeof window.matchMedia === "function" && window.matchMedia("(prefers-color-scheme: dark)").matches;
}

/** Sets data-theme to the scheme in effect ("dark" or "light"). */
function paint() {
    const t = theme.get();
    document.documentElement.dataset.theme = t === "dark" || (t === "" && systemDark()) ? "dark" : "light";
    theme.notify();
}

/** Applies a theme on this device (without saving it in the backend); unknown values mean automatic. */
export function applyTheme(t: string) {
    const value = parse(t);
    theme.set(value);
    saveLocal(KEY, value || null);
    paint();
}

// Applied as soon as this module is evaluated (main.tsx imports it first).
paint();

/** Called once at start: following the system when it changes. */
export function initTheme() {
    if (typeof window.matchMedia === "function") {
        window.matchMedia("(prefers-color-scheme: dark)").addEventListener?.("change", () => theme.get() === "" && paint());
    }
}

/** The theme chosen on this device ("" = automatic). */
export function useTheme(): Theme {
    return useStore(theme);
}

/** Whether the dark scheme is in effect right now (chosen, or automatic and the system is dark). */
export function useDarkActive(): boolean {
    // Read from the page: with "Automatisch" the system may switch while the chosen theme stays "".
    return useSyncExternalStore(theme.subscribe, () => document.documentElement.dataset.theme === "dark");
}
