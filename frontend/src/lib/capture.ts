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

pdfjs.GlobalWorkerOptions.workerSrc = pdfWorkerUrl;
prepareZXingModule({
    overrides: {locateFile: (path: string, prefix: string) => (path.endsWith(".wasm") ? zxingWasmUrl : prefix + path)},
});

export interface Recognized {
    lines: TextLine[];
    qr: string[];
    ocr: boolean; // text came from OCR (less reliable than a PDF text layer)
}

type Progress = (message: string) => void;

const MAX_PAGES = 4;
const MAX_SIDE = 2600;

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

async function ocr(canvas: HTMLCanvasElement, page: number, progress?: Progress, signal?: AbortSignal): Promise<TextLine[]> {
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
    return lines.filter(l => l.text !== "");
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
        const out: Recognized = {lines: [], qr: [], ocr: false};
        const pages = Math.min(doc.numPages, MAX_PAGES);
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
                    lines = await ocr(canvas, n, progress, signal);
                    out.ocr = true;
                }
            }
            out.lines.push(...lines);
        }
        return out;
    });
}

async function recognizeImage(url: string, progress?: Progress, signal?: AbortSignal): Promise<Recognized> {
    progress?.("Bild wird geladen …");
    const blob = await (await fetch(url, {signal})).blob();
    let bitmap: ImageBitmap;
    try {
        bitmap = await createImageBitmap(blob);
    } catch {
        throw new Error("Das Bild kann nicht gelesen werden (HEIC-Fotos bitte als JPEG senden).");
    }
    signal?.throwIfAborted();
    const scale = Math.min(1, MAX_SIDE / Math.max(bitmap.width, bitmap.height));
    const canvas = canvasFor(bitmap.width * scale, bitmap.height * scale);
    canvas.getContext("2d")!.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
    bitmap.close();
    const qr = await qrCodes(canvas);
    const lines = await ocr(canvas, 1, progress, signal);
    return {lines, qr, ocr: true};
}

/**
 * Recognises the text and QR codes of an inbox file. Aborting the signal stops
 * the work at the next step and rejects (a running OCR pass finishes in the
 * background; its result is dropped).
 */
export async function recognize(url: string, type: string, progress?: Progress, signal?: AbortSignal): Promise<Recognized> {
    if (type === "pdf") return recognizePdf(url, progress, signal);
    if (type === "image") return recognizeImage(url, progress, signal);
    throw new Error("Dieser Dateityp kann nicht gelesen werden – bitte PDF oder Foto verwenden.");
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
