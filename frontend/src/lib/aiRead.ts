// Reading a document with the AI (Svc.ReadWithAI): the pages are rendered
// here, the backend asks the server and merges the answer with the text
// recognition. It takes about a minute per page; leaving the page cancels.
import {useEffect, useRef, useState} from "react";
import {Svc} from "../api";
import type {AIReadDTO, LineDTO} from "../api";
import {AI_MAX_PAGES, pageImages} from "./capture";
import type {PageFile} from "./capture";
import {plural} from "./format";

interface Run {
    pages: number;
    started: number;
    cancel: () => void;
}

/** "1:05" */
const clock = (s: number) => `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;

/**
 * State of an AI reading: read() starts one (a running one is cancelled),
 * cancel() stops it; status says what is going on. read() resolves to null
 * when cancelled.
 */
export function useAIRead() {
    const [run, setRun] = useState<Run | null>(null);
    const runRef = useRef<Run | null>(null);
    const [, tick] = useState(0);
    useEffect(() => {
        if (!run) return;
        const t = window.setInterval(() => tick(n => n + 1), 1000);
        return () => window.clearInterval(t);
    }, [run]);
    useEffect(() => () => runRef.current?.cancel(), []);

    const finish = (r: Run) => {
        if (runRef.current !== r) return;
        runRef.current = null;
        setRun(null);
    };

    async function read(files: PageFile[], text: {lines: LineDTO[]; qr: string[]}): Promise<AIReadDTO | null> {
        runRef.current?.cancel();
        const stop = new AbortController();
        let call: {cancel: () => void} | null = null;
        const r: Run = {pages: Math.min(files.length, AI_MAX_PAGES), started: Date.now(), cancel: () => {
            stop.abort();
            call?.cancel();
            finish(r);
        }};
        runRef.current = r;
        setRun(r);
        try {
            const pages = await pageImages(files, stop.signal);
            if (stop.signal.aborted) return null;
            r.pages = pages.length;
            const p = Svc.ReadWithAI({pages, lines: text.lines, qr: text.qr});
            call = p;
            const res = await p;
            return stop.signal.aborted ? null : res;
        } catch (e) {
            if (stop.signal.aborted) return null;
            throw e;
        } finally {
            finish(r);
        }
    }

    const status = run
        ? `KI liest den Beleg … ${clock(Math.floor((Date.now() - run.started) / 1000))} (etwa ${plural(run.pages, "Minute", "Minuten")})`
        : "";
    return {running: run !== null, status, read, cancel: () => runRef.current?.cancel()};
}

/** "7 Felder gefüllt, 2 bestätigt, 1 Abweichung (74 s)" */
export function readingSummary(r: AIReadDTO, filled: number, others: number): string {
    const parts = [filled === 0 ? "Keine neuen Werte" : `${plural(filled, "Feld", "Felder")} gefüllt`];
    if (r.confirmed > 0) parts.push(`${r.confirmed} bestätigt`);
    if (others > 0) parts.push(plural(others, "Abweichung", "Abweichungen"));
    return `KI: ${parts.join(", ")} (${r.seconds} s).`;
}
