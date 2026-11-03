// Documents of several files (pages) and a cache of recognition results.
// Kept free of browser APIs so it can be unit-tested; capture.ts plugs in
// the actual recognition.
import type {TextLine} from "./lines";

export interface Recognized {
    lines: TextLine[];
    qr: string[];
    ocr: boolean; // text came from OCR (less reliable than a PDF text layer)
    pages: number; // pages read
    truncated: boolean; // the document has more pages than were read
}

/** A file of a document: one photo, or a PDF with one or more pages. */
export interface PageFile {
    url: string;
    type: string; // pdf, image
    // Together with the URL they identify the version of the file: a file
    // replaced under the same name is recognised again.
    modified?: string;
    size?: number;
    // Identifies the file for the cache instead of the URL – a page that is
    // only renamed (renumbered in a multi-page document) is not read again.
    key?: string;
}

export type Progress = (message: string) => void;

/** Pages read per document, all files together. */
export const MAX_PAGES = 12;

/**
 * Joins the results of the files of one document: pages are numbered through
 * (page 1 of the second file follows the last page of the first), at most max
 * pages are kept.
 */
export function combinePages(parts: Recognized[], max = MAX_PAGES): Recognized {
    const out: Recognized = {lines: [], qr: [], ocr: false, pages: 0, truncated: false};
    for (const p of parts) {
        if (out.pages >= max) {
            out.truncated = true;
            break;
        }
        const take = Math.min(p.pages, max - out.pages);
        const offset = out.pages;
        out.lines.push(...p.lines.filter(l => l.page <= take).map(l => ({...l, page: l.page + offset})));
        out.qr.push(...p.qr);
        out.ocr ||= p.ocr;
        out.truncated ||= p.truncated || take < p.pages;
        out.pages += take;
    }
    out.qr = [...new Set(out.qr)];
    return out;
}

type Run = (file: PageFile, progress: Progress, signal: AbortSignal) => Promise<Recognized>;

interface Job {
    promise: Promise<Recognized>;
    result?: Recognized;
    last: string; // last progress message, for consumers that join late
    listeners: Set<Progress>;
    users: number;
    ctrl: AbortController;
}

function abortError(signal: AbortSignal): unknown {
    return signal.reason ?? new DOMException("Abgebrochen", "AbortError");
}

/**
 * Shares recognition runs between consumers (the inbox detail, the background
 * recognition for suggestions, the invoice form) and keeps the results of the
 * last `size` files. A run is cancelled only when every consumer gave up on
 * it; failed and cancelled runs are not kept.
 */
export function createRecognizer(run: Run, size = 60) {
    const jobs = new Map<string, Job>();
    const key = (f: PageFile) => f.key ?? `${f.url}\n${f.modified ?? ""}\n${f.size ?? ""}`;

    function job(f: PageFile): Job {
        const k = key(f);
        const found = jobs.get(k);
        if (found) {
            jobs.delete(k); // most recently used last
            jobs.set(k, found);
            return found;
        }
        const ctrl = new AbortController();
        const j: Job = {promise: Promise.resolve(null as never), last: "", listeners: new Set(), users: 0, ctrl};
        j.promise = run(f, m => {
            j.last = m;
            j.listeners.forEach(l => l(m));
        }, ctrl.signal);
        j.promise.then(r => (j.result = r), () => jobs.get(k) === j && jobs.delete(k));
        jobs.set(k, j);
        for (const old of jobs.keys()) {
            if (jobs.size <= size) break;
            const o = jobs.get(old)!;
            if (o.result || o.users === 0) jobs.delete(old);
        }
        return j;
    }

    /** Recognises one file (or waits for the run already going on). */
    function recognizeFile(f: PageFile, progress?: Progress, signal?: AbortSignal): Promise<Recognized> {
        if (signal?.aborted) return Promise.reject(abortError(signal));
        const j = job(f);
        if (j.result) return Promise.resolve(j.result);
        j.users++;
        if (progress) {
            j.listeners.add(progress);
            if (j.last) progress(j.last);
        }
        return new Promise<Recognized>((resolve, reject) => {
            let left = false;
            const leave = () => {
                if (left) return false;
                left = true;
                signal?.removeEventListener("abort", onAbort);
                if (progress) j.listeners.delete(progress);
                j.users--;
                return true;
            };
            const onAbort = () => {
                if (!leave()) return;
                if (j.users === 0 && !j.result) {
                    j.ctrl.abort();
                    for (const [k, v] of jobs) if (v === j) jobs.delete(k);
                }
                reject(abortError(signal!));
            };
            signal?.addEventListener("abort", onAbort);
            j.promise.then(r => leave() && resolve(r), e => leave() && reject(e));
        });
    }

    /**
     * Recognises the files of one document one after another and joins them
     * (see combinePages). With several files the progress names the file.
     */
    async function recognizePages(files: PageFile[], progress?: Progress, signal?: AbortSignal): Promise<Recognized> {
        const parts: Recognized[] = [];
        let read = 0;
        for (const [i, f] of files.entries()) {
            if (read >= MAX_PAGES) break;
            const p = progress && files.length > 1 ? (m: string) => progress(`Seite ${i + 1} von ${files.length}: ${m}`) : progress;
            const r = await recognizeFile(f, p, signal);
            parts.push(r);
            read += r.pages;
        }
        const out = combinePages(parts);
        if (parts.length < files.length) out.truncated = true;
        return out;
    }

    /** The result if the file was recognised already. */
    const peek = (f: PageFile): Recognized | undefined => jobs.get(key(f))?.result;

    return {recognizeFile, recognizePages, peek};
}
