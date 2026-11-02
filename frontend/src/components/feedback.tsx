import {createContext, useCallback, useContext, useId, useMemo, useRef, useState} from "react";
import type {ReactNode} from "react";
import {errMsg} from "../lib/format";
import {useDialog} from "../lib/useDialog";

// --- toasts ---

interface ToastApi {
    show: (message: string, error?: boolean) => void;
    /** Something went wrong although the action itself succeeded. */
    warn: (message: string) => void;
    error: (e: unknown) => void;
}

const ToastContext = createContext<ToastApi>({show: () => {}, warn: () => {}, error: () => {}});

interface Toast {
    id: number;
    message: string;
    kind: "" | "warn" | "error";
}

const MAX_TOASTS = 4;

export function useToast(): ToastApi {
    return useContext(ToastContext);
}

// --- confirm dialog ---

type ConfirmFn = (message: string, okLabel?: string) => Promise<boolean>;

const ConfirmContext = createContext<ConfirmFn>(async () => false);

export function useConfirm(): ConfirmFn {
    return useContext(ConfirmContext);
}

interface Pending {
    message: string;
    okLabel: string;
    resolve: (ok: boolean) => void;
}

/** Provides toasts and the confirm dialog to the whole app. */
export function FeedbackProvider({children}: {children: ReactNode}) {
    // Toasts stack (newest at the bottom), so a warning does not hide the message before it.
    const [toasts, setToasts] = useState<Toast[]>([]);
    const seq = useRef(0);
    const [pending, setPending] = useState<Pending | null>(null);

    const push = useCallback((message: string, kind: Toast["kind"]) => {
        const id = ++seq.current;
        setToasts(ts => [...ts.slice(1 - MAX_TOASTS), {id, message, kind}]);
        window.setTimeout(() => setToasts(ts => ts.filter(t => t.id !== id)), kind ? 6000 : 3000);
    }, []);
    const api = useMemo<ToastApi>(() => ({
        show: (message, error = false) => push(message, error ? "error" : ""),
        warn: message => push(message, "warn"),
        error: e => {
            console.error(e);
            push(errMsg(e), "error");
        },
    }), [push]);

    const confirm = useCallback<ConfirmFn>((message, okLabel = "OK") =>
        new Promise(resolve => setPending({message, okLabel, resolve})), []);

    const close = (ok: boolean) => {
        pending?.resolve(ok);
        setPending(null);
    };

    return (
        <ToastContext.Provider value={api}>
            <ConfirmContext.Provider value={confirm}>
                {children}
                <div className="toasts" role="status" aria-live="polite">
                    {toasts.map(t => <div key={t.id} className={`toast ${t.kind}`}>{t.message}</div>)}
                </div>
                {pending && <ConfirmDialog message={pending.message} okLabel={pending.okLabel} onClose={close}/>}
            </ConfirmContext.Provider>
        </ToastContext.Provider>
    );
}

function ConfirmDialog({message, okLabel, onClose}: {message: string; okLabel: string; onClose: (ok: boolean) => void}) {
    const ref = useDialog<HTMLDivElement>(() => onClose(false));
    const id = useId();
    return (
        <div className="modal-back" onClick={e => e.target === e.currentTarget && onClose(false)}>
            <div ref={ref} className="modal" role="alertdialog" aria-modal="true" aria-labelledby={id} tabIndex={-1}>
                <p id={id}>{message}</p>
                <div className="actions">
                    <span className="spacer"/>
                    <button onClick={() => onClose(false)}>Abbrechen</button>
                    <button className="primary" onClick={() => onClose(true)}>{okLabel}</button>
                </div>
            </div>
        </div>
    );
}
