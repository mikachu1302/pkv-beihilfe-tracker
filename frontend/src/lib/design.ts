// The design of this device: the new one (default) or the classic look. Kept
// in the device configuration (backend) and, to paint the right one at start,
// in localStorage (public/boot.js reads it before the first paint); the CSS
// reads data-design on the root element.
import {createStore, loadLocal, saveLocal, useStore} from "./store";

export type Design = "new" | "classic";

/** In the order shown in the settings; the first is the default. */
export const DESIGN_LABELS: Record<Design, string> = {new: "Neu", classic: "Klassisch"};
export const DESIGN_ORDER: Design[] = ["new", "classic"];

const KEY = "pkv.design";

const parse = (v: unknown): Design => (v === "classic" ? "classic" : "new");
const design = createStore<Design>(parse(loadLocal(KEY)));

function paint() {
    document.documentElement.dataset.design = design.get();
}

/** Applies a design on this device (without saving it in the backend); unknown values mean the default. */
export function applyDesign(d: string) {
    design.set(parse(d));
    saveLocal(KEY, design.get());
    paint();
}

// Applied as soon as this module is evaluated (main.tsx imports it first).
paint();

/** The design in effect on this device. */
export function useDesign(): Design {
    return useStore(design);
}
