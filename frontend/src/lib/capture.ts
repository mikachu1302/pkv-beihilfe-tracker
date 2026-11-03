// Text and QR recognition in the webview – everything runs locally:
// pdf.js for PDFs with a text layer, Tesseract.js (German) for photos and
// scanned PDFs, zxing-wasm for GiroCodes. The assets are bundled (see
// scripts/copy-assets.mjs); nothing is loaded from the internet.
import * as pdfjs from "pdfjs-dist";
import pdfWorkerUrl from "pdfjs-dist/build/pdf.worker.min.mjs?url";
import {createWorker, OEM} from "tesseract.js";
import type {Worker as OcrWorker} from "tesseract.js";
import {prepareZXingModule, readBarcodes} from "zxing-wasm/reader";
import zxingWasmUrl from "zxing-wasm/reader/zxing_reader.wasm?url";
import {errMsg} from "./format";
import {groupLines} from "./lines";
import type {Piece, TextLine} from "./lines";
import {MAX_PAGES, createRecognizer} from "./pages";
import type {PageFile, Progress, Recognized} from "./pages";

export type {PageFile, Recognized};

pdfjs.GlobalWorkerOptions.workerSrc = pdfWorkerUrl;
prepareZXingModule({
    overrides: {locateFile: (path: string, prefix: string) => (path.endsWith(".wasm") ? zxingWasmUrl : prefix + path)},
});

const MAX_SIDE = 2600;
const UNREADABLE = "Dieser Dateityp kann nicht gelesen werden – bitte PDF oder Foto verwenden.";

/**
 * Opens a PDF with the bundled decoders (JBIG2/JPEG 2000 scans), standard
 * fonts and CMaps. The URLs are absolute because pdf.js may run its worker
 * from a blob: URL (under the wails:// scheme), where relative URLs do not resolve.
 */
function openPdf(url: string): pdfjs.PDFDocumentLoadingTask {
    const asset = (dir: string) => new URL(`/pdfjs/${dir}/`, window.location.href).href;
    return pdfjs.getDocument({
        url,
        wasmUrl: asset("wasm"),
        standardFontDataUrl: asset("standard_fonts"),
        cMapUrl: asset("cmaps"),
        iccUrl: asset("iccs"),
    });
}

// --- OCR ---

let ocrWorker: Promise<OcrWorker> | null = null;
let ocrJobs = 0;
// Progress callback per recognition job: several items may be recognised one after another.
const ocrProgress = new Map<string, Progress>();

function worker(): Promise<OcrWorker> {
    if (ocrWorker) return ocrWorker;
    const started = new Promise<OcrWorker>((resolve, reject) => {
        createWorker("deu", OEM.LSTM_ONLY, {
            workerPath: "/ocr/worker.min.js",
            corePath: "/ocr/core",
            langPath: "/ocr/lang",
            gzip: true,
            workerBlobURL: false,
            logger: m => {
                if (m.status === "recognizing text") ocrProgress.get(m.userJobId)?.(`Text wird erkannt … ${Math.round(m.progress * 100)} %`);
            },
            // Without a handler Tesseract.js throws inside its message handler, and a
            // failed start would leave the worker promise pending. Failed jobs still reject.
            errorHandler: (e: unknown) => reject(new Error(`Die Texterkennung konnte nicht gestartet werden: ${errMsg(e)}`)),
        }).then(resolve, reject);
    });
    ocrWorker = started;
    // A failed start must not stay cached: the next recognition tries again.
    started.catch(() => {
        if (ocrWorker === started) ocrWorker = null;
    });
    return started;
}

interface OcrResult {
    lines: TextLine[];
    confidence: number; // Tesseract's mean word confidence, 0–100
}

async function ocr(canvas: HTMLCanvasElement, page: number, progress?: Progress, signal?: AbortSignal): Promise<OcrResult> {
    progress?.("Texterkennung wird geladen …");
    const w = await worker();
    signal?.throwIfAborted();
    const job = `ocr-${++ocrJobs}`;
    if (progress) ocrProgress.set(job, progress);
    const {data} = await w.recognize(canvas, {}, {blocks: true}, job).finally(() => ocrProgress.delete(job));
    const lines: TextLine[] = [];
    for (const block of data.blocks ?? []) {
        for (const para of block.paragraphs) {
            for (const line of para.lines) {
                const b = line.bbox;
                lines.push({
                    text: line.text.replace(/\s+/g, " ").trim(), page,
                    x: b.x0 / canvas.width, y: b.y0 / canvas.height,
                    w: (b.x1 - b.x0) / canvas.width, h: (b.y1 - b.y0) / canvas.height,
                });
            }
        }
    }
    return {lines: lines.filter(l => l.text !== ""), confidence: data.confidence ?? 0};
}

// Below this confidence the page may lie sideways or upside down (a photo
// without orientation data). The orientation is then looked for on small
// copies – a full-size OCR pass takes seconds – and only the best one is
// read again in full size.
const OCR_RETRY_BELOW = 65;
const PROBE_SIDE = 1100;
const PROBE_MARGIN = 10; // a turned copy must read clearly better

function rotated(src: HTMLCanvasElement, degrees: 90 | 180 | 270): HTMLCanvasElement {
    const swap = degrees !== 180;
    const c = canvasFor(swap ? src.height : src.width, swap ? src.width : src.height);
    const ctx = c.getContext("2d")!;
    ctx.translate(c.width / 2, c.height / 2);
    ctx.rotate((degrees * Math.PI) / 180);
    ctx.drawImage(src, -src.width / 2, -src.height / 2);
    return c;
}

function scaled(src: HTMLCanvasElement, maxSide: number): HTMLCanvasElement {
    const f = Math.min(1, maxSide / Math.max(src.width, src.height));
    const c = canvasFor(src.width * f, src.height * f);
    c.getContext("2d")!.drawImage(src, 0, 0, c.width, c.height);
    return c;
}

/** OCR in the orientation that reads best. */
async function ocrUpright(canvas: HTMLCanvasElement, page: number, progress?: Progress, signal?: AbortSignal): Promise<TextLine[]> {
    const full = await ocr(canvas, page, progress, signal);
    const chars = full.lines.reduce((n, l) => n + l.text.length, 0);
    if (full.confidence >= OCR_RETRY_BELOW || chars < 20) return full.lines; // fine, or an empty page
    const small = scaled(canvas, PROBE_SIDE);
    progress?.("Bild liegt vermutlich quer – Ausrichtung wird gesucht …");
    let best = {degrees: 0, confidence: (await ocr(small, page, undefined, signal)).confidence};
    for (const degrees of [90, 270, 180] as const) {
        signal?.throwIfAborted();
        const turned = rotated(small, degrees);
        const r = await ocr(turned, page, undefined, signal);
        turned.width = 0; // free the pixels now, not at the next GC
        if (r.confidence > best.confidence + PROBE_MARGIN) best = {degrees, confidence: r.confidence};
    }
    small.width = 0;
    if (best.degrees === 0) return full.lines;
    const turned = rotated(canvas, best.degrees as 90 | 180 | 270);
    const r = await ocr(turned, page, progress, signal);
    turned.width = 0;
    return r.confidence > full.confidence ? r.lines : full.lines;
}

async function qrCodes(canvas: HTMLCanvasElement): Promise<string[]> {
    const ctx = canvas.getContext("2d");
    if (!ctx) return [];
    try {
        const img = ctx.getImageData(0, 0, canvas.width, canvas.height);
        const found = await readBarcodes(img, {formats: ["QRCode"], tryHarder: true, maxNumberOfSymbols: 4});
        return found.filter(r => r.isValid && r.text).map(r => r.text);
    } catch {
        return [];
    }
}

function canvasFor(width: number, height: number): HTMLCanvasElement {
    const c = document.createElement("canvas");
    c.width = Math.round(width);
    c.height = Math.round(height);
    return c;
}

async function renderPdfPage(page: pdfjs.PDFPageProxy, scale: number): Promise<HTMLCanvasElement> {
    const vp = page.getViewport({scale});
    const canvas = canvasFor(vp.width, vp.height);
    await page.render({canvas, viewport: vp}).promise;
    return canvas;
}

/** Runs fn with a loading task that is destroyed afterwards or as soon as signal aborts. */
async function withPdf<T>(url: string, signal: AbortSignal | undefined, fn: (task: pdfjs.PDFDocumentLoadingTask) => Promise<T>): Promise<T> {
    signal?.throwIfAborted();
    const task = openPdf(url);
    const stop = () => void task.destroy();
    signal?.addEventListener("abort", stop);
    try {
        return await fn(task);
    } finally {
        signal?.removeEventListener("abort", stop);
        await task.destroy();
    }
}

function recognizePdf(url: string, progress?: Progress, signal?: AbortSignal): Promise<Recognized> {
    progress?.("PDF wird gelesen …");
    return withPdf(url, signal, async task => {
        const doc = await task.promise;
        const pages = Math.min(doc.numPages, MAX_PAGES);
        const out: Recognized = {lines: [], qr: [], ocr: false, pages, truncated: doc.numPages > pages};
        for (let n = 1; n <= pages; n++) {
            signal?.throwIfAborted();
            const page = await doc.getPage(n);
            const vp = page.getViewport({scale: 1});
            const content = await page.getTextContent();
            const pieces: Piece[] = [];
            for (const item of content.items) {
                if (!("str" in item) || !item.str.trim()) continue;
                const [, , , d, e, f] = item.transform;
                const h = Math.abs(d) || item.height;
                pieces.push({text: item.str, x: e / vp.width, y: 1 - (f + h) / vp.height, w: item.width / vp.width, h: h / vp.height});
            }
            let lines = groupLines(pieces, n);
            // GiroCodes are drawn as graphics: render the page to find them; OCR if there is no text layer.
            const scale = Math.min(2.5, MAX_SIDE / Math.max(vp.width, vp.height));
            const needOcr = lines.reduce((s, l) => s + l.text.length, 0) < 40;
            if (needOcr || n === 1 || n === pages) {
                const canvas = await renderPdfPage(page, scale);
                out.qr.push(...await qrCodes(canvas));
                if (needOcr) {
                    lines = await ocrUpright(canvas, n, progress, signal);
                    out.ocr = true;
                }
            }
            out.lines.push(...lines);
        }
        return out;
    });
}

/** Loads a photo (turned upright by its orientation data). */
async function loadBitmap(url: string, signal?: AbortSignal): Promise<ImageBitmap> {
    const blob = await (await fetch(url, {signal})).blob();
    try {
        return await createImageBitmap(blob);
    } catch {
        throw new Error("Das Bild kann nicht gelesen werden (HEIC-Fotos bitte als JPEG senden).");
    }
}

async function recognizeImage(url: string, progress?: Progress, signal?: AbortSignal): Promise<Recognized> {
    progress?.("Bild wird geladen …");
    const bitmap = await loadBitmap(url, signal);
    signal?.throwIfAborted();
    const scale = Math.min(1, MAX_SIDE / Math.max(bitmap.width, bitmap.height));
    const canvas = canvasFor(bitmap.width * scale, bitmap.height * scale);
    canvas.getContext("2d")!.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
    bitmap.close();
    const qr = await qrCodes(canvas);
    const lines = await ocrUpright(canvas, 1, progress, signal);
    return {lines, qr, ocr: true, pages: 1, truncated: false};
}

/**
 * Recognises the text and QR codes of an inbox file. Aborting the signal stops
 * the work at the next step and rejects (a running OCR pass finishes in the
 * background; its result is dropped).
 */
export async function recognize(url: string, type: string, progress?: Progress, signal?: AbortSignal): Promise<Recognized> {
    if (type === "pdf") return recognizePdf(url, progress, signal);
    if (type === "image") return recognizeImage(url, progress, signal);
    throw new Error(UNREADABLE);
}

const shared = createRecognizer((f, progress, signal) => recognize(f.url, f.type, progress, signal));

/**
 * Recognises a document of one or more files (pages in order), see
 * pages.ts. Results are kept per file: an entry selected again, or
 * recognised in the background before, is there at once.
 */
export const recognizePages = shared.recognizePages;

/** The recognition result of a file if it is known already. */
export const recognizedFile = shared.peek;

// --- pages as images for the AI ---

/** Pages sent to the AI at most (internal/airead.MaxPages). */
export const AI_MAX_PAGES = 4;
/** Long side of a page sent to the AI: smaller pages lose amounts in small print (tested). */
const AI_SIDE = 1400;

/** A JPEG of the picture, at most AI_SIDE on the long side, on white (PDF pages may be transparent). */
function jpeg(src: CanvasImageSource & {width: number; height: number}): string {
    const f = Math.min(1, AI_SIDE / Math.max(src.width, src.height));
    const c = canvasFor(src.width * f, src.height * f);
    const ctx = c.getContext("2d")!;
    ctx.fillStyle = "#fff";
    ctx.fillRect(0, 0, c.width, c.height);
    ctx.drawImage(src, 0, 0, c.width, c.height);
    const url = c.toDataURL("image/jpeg", 0.88);
    c.width = 0; // free the pixels now
    return url;
}

/**
 * The pages of a document as JPEG data URLs for the AI (in order, at most
 * AI_MAX_PAGES). PDFs are rendered page by page, photos scaled down.
 */
export async function pageImages(files: PageFile[], signal?: AbortSignal): Promise<string[]> {
    const out: string[] = [];
    for (const f of files) {
        if (out.length >= AI_MAX_PAGES) break;
        signal?.throwIfAborted();
        if (f.type === "pdf") {
            await withPdf(f.url, signal, async task => {
                const doc = await task.promise;
                for (let n = 1; n <= doc.numPages && out.length < AI_MAX_PAGES; n++) {
                    signal?.throwIfAborted();
                    const page = await doc.getPage(n);
                    const vp = page.getViewport({scale: 1});
                    const canvas = await renderPdfPage(page, AI_SIDE / Math.max(vp.width, vp.height));
                    out.push(jpeg(canvas));
                    canvas.width = 0;
                }
            });
        } else if (f.type === "image") {
            const bitmap = await loadBitmap(f.url, signal);
            out.push(jpeg(bitmap));
            bitmap.close();
        } else {
            throw new Error(UNREADABLE);
        }
    }
    return out;
}

// pdf.js refuses two renders into one canvas at the same time: a new preview
// cancels the one still drawing into the same canvas.
const previewRenders = new WeakMap<HTMLCanvasElement, pdfjs.RenderTask>();

/** Renders the first page of a PDF into a canvas (preview). Resolves quietly when cancelled. */
export async function renderPdfPreview(url: string, canvas: HTMLCanvasElement, width: number, signal?: AbortSignal): Promise<void> {
    try {
        await withPdf(url, signal, async task => {
            const doc = await task.promise;
            const page = await doc.getPage(1);
            signal?.throwIfAborted();
            const vp = page.getViewport({scale: 1});
            const view = page.getViewport({scale: width / vp.width});
            previewRenders.get(canvas)?.cancel();
            canvas.width = Math.round(view.width);
            canvas.height = Math.round(view.height);
            const render = page.render({canvas, viewport: view});
            previewRenders.set(canvas, render);
            const cancel = () => render.cancel();
            signal?.addEventListener("abort", cancel);
            try {
                await render.promise;
            } finally {
                signal?.removeEventListener("abort", cancel);
                if (previewRenders.get(canvas) === render) previewRenders.delete(canvas);
            }
        });
    } catch (e) {
        if (signal?.aborted || e instanceof pdfjs.RenderingCancelledException) return;
        throw e;
    }
}
