import {useEffect, useRef} from "react";

const FOCUSABLE = "input:not([disabled]), select:not([disabled]), textarea:not([disabled]), button:not([disabled]), [href], [tabindex]:not([tabindex='-1'])";

/**
 * Keyboard handling for dialogs and popovers: on open the first form field
 * (or the first button, or the dialog itself) gets the focus, Escape closes,
 * and on close the focus returns to the element that opened the dialog.
 * Attach the returned ref to the dialog element (give it tabIndex={-1}).
 */
export function useDialog<T extends HTMLElement>(onClose: () => void) {
    const ref = useRef<T>(null);
    const close = useRef(onClose);
    useEffect(() => {
        close.current = onClose;
    });

    useEffect(() => {
        const el = ref.current;
        if (!el) return;
        const opener = document.activeElement;
        const field = el.querySelector<HTMLElement>("input:not([disabled]), select:not([disabled]), textarea:not([disabled])");
        (field ?? el.querySelector<HTMLElement>(FOCUSABLE) ?? el).focus();
        const onKey = (e: KeyboardEvent) => {
            if (e.key !== "Escape") return;
            e.stopPropagation();
            close.current();
        };
        el.addEventListener("keydown", onKey);
        return () => {
            el.removeEventListener("keydown", onKey);
            if (opener instanceof HTMLElement && opener.isConnected) opener.focus();
        };
    }, []);

    return ref;
}
