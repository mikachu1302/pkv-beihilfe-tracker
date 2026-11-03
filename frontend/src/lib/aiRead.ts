// Reading a document with the AI (Svc.ReadWithAI): the pages are rendered
// here, the backend asks the server and merges the answer with the text
// recognition. It takes about a minute per page; leaving the page cancels.
import {useCallback, useEffect, useRef, useState} from "react";
import {Svc} from "../api";
import type {AIReadDTO, LineDTO} from "../api";
import {AI_MAX_PAGES, pageImages} from "./capture";
import type {PageFile} from "./capture";
import {errMsg, plural, sentence} from "./format";

/** A reading under way: how many pages (for the expected time) and since when. */
export interface AIRun {
    pages: number;
    started: number;
}

/**
 * State of an AI reading: read() starts one (a running one is cancelled),
 * cancel() stops it. read() resolves to null when cancelled.
 */
export function useAIRead() {
    const [run, setRun] = useState<AIRun | null>(null);
    const current = useRef<{cancel: () => void} | null>(null);
    useEffect(() => () => current.current?.cancel(), []);

    const cancel = useCallback(() => {
        const c = current.current;
        current.current = null;
        c?.cancel();
        setRun(null);
    }, []);

    const read = useCallback(async (files: PageFile[], text: {lines: LineDTO[]; qr: string[]}): Promise<AIReadDTO | null> => {
        current.current?.cancel();
        const stop = new AbortController();
        let call: {cancel: () => void} | null = null;
        const me = {cancel: () => {
            stop.abort();
            call?.cancel();
        }};
        current.current = me;
        setRun({pages: Math.min(files.length, AI_MAX_PAGES), started: Date.now()});
        try {
            const {images, total} = await pageImages(files, stop.signal);
            if (stop.signal.aborted) return null;
            setRun(r => r && {...r, pages: images.length});
            const p = Svc.ReadWithAI({pages: images, lines: text.lines, qr: text.qr, totalPages: total});
            call = p;
            const res = await p;
            return stop.signal.aborted ? null : res;
        } catch (e) {
            if (stop.signal.aborted) return null;
            throw e;
        } finally {
            if (current.current === me) {
                current.current = null;
                setRun(null);
            }
        }
    }, []);

    return {run, read, cancel};
}

/** "7 Felder gefüllt, 2 bestätigt, 1 Abweichung (74 s)" */
export function readingSummary(r: AIReadDTO, filled: number, others: number): string {
    const parts = [filled === 0 ? "Keine neuen Werte" : `${plural(filled, "Feld", "Felder")} gefüllt`];
    if (r.confirmed > 0) parts.push(`${r.confirmed} bestätigt`);
    if (others > 0) parts.push(plural(others, "Abweichung", "Abweichungen"));
    return `KI: ${parts.join(", ")} (${r.seconds} s).`;
}

/** The message when reading failed: the values of the text recognition stay. */
export const readingError = (e: unknown) => `${sentence(errMsg(e))} Die Werte der Texterkennung bleiben.`;
