// A backend call the user can cancel (a Wails CancellablePromise): one at a
// time, cancelled when a new one starts or the component goes away, and a
// late result of a cancelled call is dropped.
import {useCallback, useEffect, useRef, useState} from "react";

interface Cancellable<T> extends Promise<T> {
    cancel: () => void;
}

/** Thrown by run() for a call that was cancelled. */
export class Cancelled extends Error {
    constructor() {
        super("Abgebrochen");
    }
}

export function useCancellable() {
    const current = useRef<{cancel: () => void} | null>(null);
    const [running, setRunning] = useState(false);
    useEffect(() => () => current.current?.cancel(), []);

    const cancel = useCallback(() => {
        const c = current.current;
        current.current = null;
        c?.cancel();
        setRunning(false);
    }, []);

    /** Runs the call; rejects with Cancelled when it was cancelled (or replaced) meanwhile. */
    const run = useCallback(async <T, >(start: () => Cancellable<T>): Promise<T> => {
        current.current?.cancel();
        const p = start();
        const me = {cancel: () => p.cancel()};
        current.current = me;
        setRunning(true);
        try {
            const r = await p;
            if (current.current !== me) throw new Cancelled();
            return r;
        } catch (e) {
            if (current.current !== me) throw new Cancelled();
            throw e;
        } finally {
            if (current.current === me) {
                current.current = null;
                setRunning(false);
            }
        }
    }, []);

    return {running, run, cancel};
}
