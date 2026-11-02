import {createContext, useCallback, useContext, useRef, useState} from "react";
import type {ReactNode} from "react";
import {errMsg} from "../lib/format";

// --- toasts ---

interface ToastApi {
    show: (message: string, error?: boolean) => void;
    error: (e: unknown) => void;
}

const ToastContext = createContext<ToastApi>({show: () => {}, error: () => {}});

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
    const [toast, setToast] = useState<{message: string; error: boolean} | null>(null);
    const timer = useRef<number>(undefined);
    const [pending, setPending] = useState<Pending | null>(null);

    const show = useCallback((message: string, error = false) => {
        setToast({message, error});
        window.clearTimeout(timer.current);
        timer.current = window.setTimeout(() => setToast(null), error ? 6000 : 3000);
    }, []);
    const error = useCallback((e: unknown) => {
        console.error(e);
        show(errMsg(e), true);
    }, [show]);

    const confirm = useCallback<ConfirmFn>((message, okLabel = "OK") =>
        new Promise(resolve => setPending({message, okLabel, resolve})), []);

    const close = (ok: boolean) => {
        pending?.resolve(ok);
        setPending(null);
    };

    return (
        <ToastContext.Provider value={{show, error}}>
            <ConfirmContext.Provider value={confirm}>
                {children}
                <div className={`toast ${toast ? "show" : ""} ${toast?.error ? "error" : ""}`} role="status" aria-live="polite">
                    {toast?.message}
                </div>
                {pending && (
                    <div className="modal-back" onClick={e => e.target === e.currentTarget && close(false)}>
                        <div className="modal" role="dialog" aria-modal="true">
                            <p>{pending.message}</p>
                            <div className="actions">
                                <span className="spacer"/>
                                <button onClick={() => close(false)}>Abbrechen</button>
                                <button className="primary" onClick={() => close(true)}>{pending.okLabel}</button>
                            </div>
                        </div>
                    </div>
                )}
            </ConfirmContext.Provider>
        </ToastContext.Provider>
    );
}
