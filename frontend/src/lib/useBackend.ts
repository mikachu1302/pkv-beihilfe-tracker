import {useCallback, useEffect, useRef, useState} from "react";
import {errMsg} from "./format";

export interface BackendState<T> {
    data: T | undefined;
    error: string | undefined;
    loading: boolean;
    reload: () => void;
}

/**
 * Runs a backend call when the component mounts or a dependency changes, and
 * exposes loading / error state plus a reload function. Results of outdated
 * calls are ignored.
 */
export function useBackend<T>(load: () => Promise<T>, deps: unknown[]): BackendState<T> {
    const [data, setData] = useState<T>();
    const [error, setError] = useState<string>();
    const [loading, setLoading] = useState(true);
    const [tick, setTick] = useState(0);
    const call = useRef(0);

    useEffect(() => {
        const id = ++call.current;
        setLoading(true);
        load().then(
            result => {
                if (id !== call.current) return;
                setData(result);
                setError(undefined);
                setLoading(false);
            },
            e => {
                if (id !== call.current) return;
                setError(errMsg(e));
                setLoading(false);
            },
        );
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [...deps, tick]);

    const reload = useCallback(() => setTick(t => t + 1), []);
    return {data, error, loading, reload};
}
