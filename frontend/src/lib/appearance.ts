// The appearance settings of this device as the backend keeps them (design,
// default invoice view, table options, the one-time design hint). Loaded once
// at start (App: AppearanceSync); pages read them with useAppearance.
import {useSyncExternalStore} from "react";
import type {AppearanceDTO} from "../api";

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
