// The design of this device: the new one (default) or the classic look. Kept
// in the device configuration (backend) and, to paint the right one at start,
// in localStorage; the CSS reads data-design on the root element.
import {useSyncExternalStore} from "react";

export type Design = "new" | "classic";

/** In the order shown in the settings. */
export const DESIGN_LABELS: Record<Design, string> = {new: "Neu", classic: "Klassisch"};
export const DESIGN_ORDER: Design[] = ["new", "classic"];

const KEY = "pkv.design";
const listeners = new Set<() => void>();
let current: Design = stored();

function stored(): Design {
    try {
        return localStorage.getItem(KEY) === "classic" ? "classic" : "new";
    } catch {
        return "new";
    }
}

function paint() {
    document.documentElement.dataset.design = current;
    listeners.forEach(l => l());
}

/** Applies a design on this device (without saving it in the backend). */
export function applyDesign(d: Design) {
    current = d === "classic" ? "classic" : "new";
    try {
        localStorage.setItem(KEY, current);
    } catch {
        // the backend keeps it
    }
    paint();
}

// Applied as soon as this module is evaluated (main.tsx imports it first).
paint();

const subscribe = (l: () => void) => {
    listeners.add(l);
    return () => {
        listeners.delete(l);
    };
};

/** The design in effect on this device. */
export function useDesign(): Design {
    return useSyncExternalStore(subscribe, () => current);
}
