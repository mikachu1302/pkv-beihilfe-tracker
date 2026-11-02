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

let ocrWorker: Promise<OcrWorker> | null = null;
let ocrProgress: Progress | undefined;

function worker(): Promise<OcrWorker> {
    ocrWorker ??= createWorker("deu", OEM.LSTM_ONLY, {
        workerPath: "/ocr/worker.min.js",
        corePath: "/ocr/core",
        langPath: "/ocr/lang",
        gzip: true,
        workerBlobURL: false,
        logger: (m: {status: string; progress: number}) => {
            if (m.status === "recognizing text") ocrProgress?.(`Text wird erkannt … ${Math.round(m.progress * 100)} %`);
        },
    });
    return ocrWorker;
}

async function ocr(canvas: HTMLCanvasElement, page: number, progress?: Progress): Promise<TextLine[]> {
    progress?.("Texterkennung wird geladen …");
    const w = await worker();
    ocrProgress = progress;
    const {data} = await w.recognize(canvas, {}, {blocks: true});
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

async function recognizePdf(url: string, progress?: Progress): Promise<Recognized> {
    progress?.("PDF wird gelesen …");
    const task = pdfjs.getDocument({url});
    const doc = await task.promise;
    const out: Recognized = {lines: [], qr: [], ocr: false};
    const pages = Math.min(doc.numPages, MAX_PAGES);
    for (let n = 1; n <= pages; n++) {
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
                lines = await ocr(canvas, n, progress);
                out.ocr = true;
            }
        }
        out.lines.push(...lines);
    }
    await task.destroy();
    return out;
}

async function recognizeImage(url: string, progress?: Progress): Promise<Recognized> {
    progress?.("Bild wird geladen …");
    const blob = await (await fetch(url)).blob();
    let bitmap: ImageBitmap;
    try {
        bitmap = await createImageBitmap(blob);
    } catch {
        throw new Error("Das Bild kann nicht gelesen werden (HEIC-Fotos bitte als JPEG senden).");
    }
    const scale = Math.min(1, MAX_SIDE / Math.max(bitmap.width, bitmap.height));
    const canvas = canvasFor(bitmap.width * scale, bitmap.height * scale);
    canvas.getContext("2d")!.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
    const qr = await qrCodes(canvas);
    const lines = await ocr(canvas, 1, progress);
    return {lines, qr, ocr: true};
}

/** Recognises the text and QR codes of an inbox file. */
export async function recognize(url: string, type: string, progress?: Progress): Promise<Recognized> {
    if (type === "pdf") return recognizePdf(url, progress);
    if (type === "image") return recognizeImage(url, progress);
    throw new Error("Dieser Dateityp kann nicht gelesen werden – bitte PDF oder Foto verwenden.");
}

/** Renders the first page of a PDF into a canvas (preview). */
export async function renderPdfPreview(url: string, canvas: HTMLCanvasElement, width: number): Promise<void> {
    const task = pdfjs.getDocument({url});
    const doc = await task.promise;
    const page = await doc.getPage(1);
    const vp = page.getViewport({scale: 1});
    const view = page.getViewport({scale: width / vp.width});
    canvas.width = Math.round(view.width);
    canvas.height = Math.round(view.height);
    await page.render({canvas, viewport: view}).promise;
    await task.destroy();
}
