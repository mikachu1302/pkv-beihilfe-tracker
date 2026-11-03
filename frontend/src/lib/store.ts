// A small external store for state that lives outside React (device
// settings such as theme, design and appearance), and access to
// localStorage that never throws (private windows, blocked storage).
import {useSyncExternalStore} from "react";

export interface Store<T> {
    get: () => T;
    set: (value: T) => void;
    /** Tells the subscribers to read again (e.g. after a change outside the value). */
    notify: () => void;
    subscribe: (listener: () => void) => () => void;
}

export function createStore<T>(initial: T): Store<T> {
    let value = initial;
    const listeners = new Set<() => void>();
    const notify = () => listeners.forEach(l => l());
    return {
        get: () => value,
        set: v => {
            value = v;
            notify();
        },
        notify,
        subscribe: l => {
            listeners.add(l);
            return () => {
                listeners.delete(l);
            };
        },
    };
}

/** The current value of a store; re-renders when it changes. */
export function useStore<T>(store: Store<T>): T {
    return useSyncExternalStore(store.subscribe, store.get);
}

/** A value from localStorage, or null if missing or not readable. */
export function loadLocal(key: string): string | null {
    try {
        return localStorage.getItem(key);
    } catch {
        return null;
    }
}

/** Stores a value in localStorage (null removes it); not being able to is fine – it is a convenience. */
export function saveLocal(key: string, value: string | null) {
    try {
        if (value === null) localStorage.removeItem(key);
        else localStorage.setItem(key, value);
    } catch {
        // not remembered on this device
    }
}
