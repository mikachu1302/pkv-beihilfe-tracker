// Keyboard shortcuts. The modifier is Cmd on macOS and Ctrl elsewhere, as
// users expect from their system ("Speichern" = ⌘S / Strg+S).
import {useEffect, useRef} from "react";

export const isMac = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent);

/** The platform's modifier key is held (Cmd on macOS, Ctrl elsewhere) – and no other except Shift. */
export function mod(e: KeyboardEvent): boolean {
    return (isMac ? e.metaKey && !e.ctrlKey : e.ctrlKey && !e.metaKey) && !e.altKey;
}

/** Label of a shortcut with the modifier: key("S") → "Strg+S" or "⌘S". */
export function key(k: string, shift = false): string {
    if (isMac) return `${shift ? "⇧" : ""}⌘${k === "Enter" ? "↩" : k}`;
    return `Strg+${shift ? "Umschalt+" : ""}${k}`;
}

/** Whether typing goes into the element (then Backspace deletes, it does not navigate). */
export function isTyping(target: EventTarget | null): boolean {
    if (!(target instanceof HTMLElement)) return false;
    if (target.isContentEditable || target instanceof HTMLTextAreaElement || target instanceof HTMLSelectElement) return true;
    if (target instanceof HTMLInputElement) {
        return !["checkbox", "radio", "button", "submit", "reset", "file", "range", "color"].includes(target.type);
    }
    return false;
}

/** A modal dialog is open: page shortcuts stay quiet while it has the keyboard (popovers do not count). */
export function dialogOpen(): boolean {
    return document.querySelector("[aria-modal=true]") !== null;
}

/**
 * Calls handler for keydown events that match. The handler decides with its
 * return value whether the event was used (true: default action prevented).
 */
export function useKeydown(handler: (e: KeyboardEvent) => boolean | void, enabled = true) {
    const latest = useRef(handler);
    useEffect(() => {
        latest.current = handler;
    });
    useEffect(() => {
        if (!enabled) return;
        const onKey = (e: KeyboardEvent) => {
            if (e.defaultPrevented || e.isComposing) return;
            if (latest.current(e) === true) e.preventDefault();
        };
        window.addEventListener("keydown", onKey);
        return () => window.removeEventListener("keydown", onKey);
    }, [enabled]);
}

/** ⌘S / Strg+S runs save (also while typing in a field), unless a dialog is open. */
export function useSaveShortcut(save: () => void, enabled = true) {
    useKeydown(e => {
        if (!mod(e) || e.shiftKey || e.key.toLowerCase() !== "s" || dialogOpen()) return false;
        if (!e.repeat) save(); // holding the keys must not save again and again
        return true;
    }, enabled);
}

// --- unsaved changes ---

const dirty = new Set<symbol>();

/** Marks the page as having unsaved changes while dirty is true. */
export function useUnsavedChanges(isDirty: boolean) {
    const id = useRef(Symbol("form"));
    useEffect(() => {
        const me = id.current;
        if (isDirty) dirty.add(me);
        else dirty.delete(me);
        return () => void dirty.delete(me);
    }, [isDirty]);
}

/** Some page has unsaved changes. */
export function hasUnsavedChanges(): boolean {
    return dirty.size > 0;
}

/** Forgets all unsaved changes (the user chose to leave them). */
export function clearUnsavedChanges() {
    dirty.clear();
}
