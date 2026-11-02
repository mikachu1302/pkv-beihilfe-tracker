// Copies the OCR engine and the German language data into public/ocr so the
// app recognises text offline (Tesseract.js would otherwise load them from a CDN),
// and the pdf.js decoders (JBIG2/JPEG 2000 scans), standard fonts, CMaps and the
// CMYK colour profile into public/pdfjs (without them such PDFs render blank).
import {copyFileSync, cpSync, mkdirSync} from "node:fs";
import {dirname, join} from "node:path";
import {fileURLToPath} from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const nm = join(root, "node_modules");
const ocr = join(root, "public", "ocr");
const files = [
    ["tesseract.js/dist/worker.min.js", "worker.min.js"],
    ["tesseract.js-core/tesseract-core-lstm.wasm.js", "core/tesseract-core-lstm.wasm.js"],
    ["tesseract.js-core/tesseract-core-simd-lstm.wasm.js", "core/tesseract-core-simd-lstm.wasm.js"],
    ["tesseract.js-core/tesseract-core-relaxedsimd-lstm.wasm.js", "core/tesseract-core-relaxedsimd-lstm.wasm.js"],
    ["@tesseract.js-data/deu/4.0.0_best_int/deu.traineddata.gz", "lang/deu.traineddata.gz"],
];
for (const [src, dst] of files) {
    mkdirSync(dirname(join(ocr, dst)), {recursive: true});
    copyFileSync(join(nm, src), join(ocr, dst));
}
console.log(`OCR assets copied to ${ocr}`);

const pdfjs = join(root, "public", "pdfjs");
for (const dir of ["wasm", "standard_fonts", "cmaps", "iccs"]) {
    cpSync(join(nm, "pdfjs-dist", dir), join(pdfjs, dir), {
        recursive: true,
        // QuickJS only runs PDF scripts (forms), which the app never enables.
        filter: src => !/quickjs/i.test(src),
    });
}
console.log(`pdf.js assets copied to ${pdfjs}`);
