// Copies the OCR engine and the German language data into public/ocr so the
// app recognises text offline (Tesseract.js would otherwise load them from a CDN).
import {copyFileSync, mkdirSync} from "node:fs";
import {dirname, join} from "node:path";
import {fileURLToPath} from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const nm = join(root, "node_modules");
const out = join(root, "public", "ocr");
const files = [
    ["tesseract.js/dist/worker.min.js", "worker.min.js"],
    ["tesseract.js-core/tesseract-core-lstm.wasm.js", "core/tesseract-core-lstm.wasm.js"],
    ["tesseract.js-core/tesseract-core-simd-lstm.wasm.js", "core/tesseract-core-simd-lstm.wasm.js"],
    ["tesseract.js-core/tesseract-core-relaxedsimd-lstm.wasm.js", "core/tesseract-core-relaxedsimd-lstm.wasm.js"],
    ["@tesseract.js-data/deu/4.0.0_best_int/deu.traineddata.gz", "lang/deu.traineddata.gz"],
];
for (const [src, dst] of files) {
    mkdirSync(dirname(join(out, dst)), {recursive: true});
    copyFileSync(join(nm, src), join(out, dst));
}
console.log(`OCR assets copied to ${out}`);
