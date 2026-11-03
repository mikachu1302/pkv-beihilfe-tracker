import {describe, expect, it, vi} from "vitest";
import {MAX_PAGES, combinePages, createRecognizer} from "./pages";
import type {PageFile, Recognized} from "./pages";

const line = (text: string, page: number) => ({text, page, x: 0, y: 0, w: 0, h: 0});
const rec = (pages: number, qr: string[] = [], ocr = false): Recognized => ({
    lines: Array.from({length: pages}, (_, i) => line(`Seite ${i + 1}`, i + 1)), qr, ocr, pages, truncated: false,
});
const file = (name: string): PageFile => ({url: `/datafile/inbox/${name}`, type: "image", modified: "2026-11-03 10:00", size: 1});

/** A promise to resolve or reject from outside. */
function deferred<T>() {
    let resolve!: (v: T) => void, reject!: (e: unknown) => void;
    const promise = new Promise<T>((res, rej) => {
        resolve = res;
        reject = rej;
    });
    return {promise, resolve, reject};
}

describe("combinePages", () => {
    it("numbers the pages through and joins QR codes", () => {
        const out = combinePages([rec(2, ["BCD"]), rec(1, ["BCD", "X"], true)]);
        expect(out.lines.map(l => [l.text, l.page])).toEqual([["Seite 1", 1], ["Seite 2", 2], ["Seite 1", 3]]);
        expect(out).toMatchObject({pages: 3, qr: ["BCD", "X"], ocr: true, truncated: false});
    });

    it("keeps at most the page limit", () => {
        const out = combinePages([rec(MAX_PAGES - 1), rec(3), rec(1)]);
        expect(out.pages).toBe(MAX_PAGES);
        expect(out.lines.at(-1)?.page).toBe(MAX_PAGES);
        expect(out.truncated).toBe(true);
        expect(combinePages([{...rec(1), truncated: true}]).truncated).toBe(true);
    });
});

describe("createRecognizer", () => {
    it("shares a run and keeps its result", async () => {
        const d = deferred<Recognized>();
        const run = vi.fn(() => d.promise);
        const r = createRecognizer(run);
        const a = r.recognizeFile(file("a.jpg"));
        const b = r.recognizeFile(file("a.jpg"));
        d.resolve(rec(1));
        await expect(a).resolves.toMatchObject({pages: 1});
        await expect(b).resolves.toMatchObject({pages: 1});
        expect(run).toHaveBeenCalledTimes(1);
        expect(r.peek(file("a.jpg"))).toMatchObject({pages: 1});
        await r.recognizeFile(file("a.jpg"));
        expect(run).toHaveBeenCalledTimes(1);
        // Another version of the file is recognised again.
        r.recognizeFile({...file("a.jpg"), modified: "2026-11-03 11:00"});
        expect(run).toHaveBeenCalledTimes(2);
    });

    it("cancels a run only when every consumer gave up", async () => {
        const d = deferred<Recognized>();
        let runSignal: AbortSignal | undefined;
        const run = vi.fn((_f: PageFile, _p: unknown, s: AbortSignal) => {
            runSignal = s;
            return d.promise;
        });
        const r = createRecognizer(run);
        const one = new AbortController(), two = new AbortController();
        const a = r.recognizeFile(file("a.jpg"), undefined, one.signal);
        const b = r.recognizeFile(file("a.jpg"), undefined, two.signal);
        one.abort();
        await expect(a).rejects.toBeDefined();
        expect(runSignal?.aborted).toBe(false);
        two.abort();
        await expect(b).rejects.toBeDefined();
        await new Promise(r => setTimeout(r, 5)); // cancelled a moment later
        expect(runSignal?.aborted).toBe(true);
        // The cancelled run is not kept: the next use starts again.
        r.recognizeFile(file("a.jpg"));
        expect(run).toHaveBeenCalledTimes(2);
    });

    it("does not keep failures", async () => {
        const run = vi.fn().mockRejectedValueOnce(new Error("kaputt")).mockResolvedValue(rec(1));
        const r = createRecognizer(run);
        await expect(r.recognizeFile(file("a.jpg"))).rejects.toThrow("kaputt");
        await expect(r.recognizeFile(file("a.jpg"))).resolves.toMatchObject({pages: 1});
        expect(run).toHaveBeenCalledTimes(2);
    });

    it("tells a late consumer the last progress", async () => {
        const d = deferred<Recognized>();
        const r = createRecognizer((_f, progress) => {
            progress("Text wird erkannt … 40 %");
            return d.promise;
        });
        r.recognizeFile(file("a.jpg"));
        const seen: string[] = [];
        const late = r.recognizeFile(file("a.jpg"), m => seen.push(m));
        expect(seen).toEqual(["Text wird erkannt … 40 %"]);
        d.resolve(rec(1));
        await late;
    });

    it("recognises the files of a document in order", async () => {
        const r = createRecognizer(async f => (f.url.endsWith("a.pdf") ? rec(2) : rec(1)));
        const seen: string[] = [];
        const out = await r.recognizePages([{...file("a.pdf"), type: "pdf"}, file("b.jpg")], m => seen.push(m));
        expect(out.pages).toBe(3);
        expect(out.lines.map(l => l.page)).toEqual([1, 2, 3]);
        expect(seen).toEqual([]);
        const progress = createRecognizer(async (_f, p) => {
            p("liest");
            return rec(1);
        });
        await progress.recognizePages([file("a.jpg"), file("b.jpg")], m => seen.push(m));
        expect(seen).toEqual(["Seite 1 von 2: liest", "Seite 2 von 2: liest"]);
    });

    it("stops after the page limit", async () => {
        const run = vi.fn(async () => rec(MAX_PAGES));
        const r = createRecognizer(run);
        const out = await r.recognizePages([file("a.jpg"), file("b.jpg")]);
        expect(run).toHaveBeenCalledTimes(1);
        expect(out).toMatchObject({pages: MAX_PAGES, truncated: true});
    });

    it("keeps a run when its consumer restarts at once", async () => {
        const d = deferred<Recognized>();
        let runSignal: AbortSignal | undefined;
        const run = vi.fn((_f: PageFile, _p: unknown, s: AbortSignal) => {
            runSignal = s;
            return d.promise;
        });
        const r = createRecognizer(run);
        const first = new AbortController();
        const a = r.recognizeFile(file("a.jpg"), undefined, first.signal);
        first.abort(); // an effect cleaned up …
        const b = r.recognizeFile(file("a.jpg")); // … and runs again
        await expect(a).rejects.toBeDefined();
        await new Promise(res => setTimeout(res, 5));
        expect(runSignal?.aborted).toBe(false);
        expect(run).toHaveBeenCalledTimes(1);
        d.resolve(rec(1));
        await expect(b).resolves.toMatchObject({pages: 1});
    });

    it("lets foreground runs go first", async () => {
        const order: string[] = [];
        const gates = new Map<string, () => void>();
        const r = createRecognizer(f => new Promise<Recognized>(res => {
            order.push(f.url);
            gates.set(f.url, () => res(rec(1)));
        }));
        // A foreground run is going on …
        const front = r.recognizePages([file("selected.jpg")]);
        await Promise.resolve();
        // … the background waits with its next entry until it is done.
        const back = r.recognizePages([file("other.jpg")], undefined, undefined, {background: true});
        await new Promise(res => setTimeout(res, 5));
        expect(order).toEqual(["/datafile/inbox/selected.jpg"]);
        gates.get("/datafile/inbox/selected.jpg")!();
        await front;
        await new Promise(res => setTimeout(res, 5));
        expect(order).toEqual(["/datafile/inbox/selected.jpg", "/datafile/inbox/other.jpg"]);
        gates.get("/datafile/inbox/other.jpg")!();
        await back;
    });
});
