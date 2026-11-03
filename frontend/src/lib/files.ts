// Files dropped onto the window and stored documents.
import {useRef, useState} from "react";
import type {DragEvent} from "react";

/** Largest file accepted by drag & drop – the same limit as the phone upload. */
export const MAX_FILE_MB = 40;

export function readAsDataURL(file: File): Promise<string> {
    return new Promise((resolve, reject) => {
        const r = new FileReader();
        r.onload = () => resolve(String(r.result));
        r.onerror = () => reject(r.error);
        r.readAsDataURL(file);
    });
}

/** Webview URL of a stored document (belege/<sha256>.<ext>). */
export function docUrl(name: string): string {
    return `/datafile/doc/${encodeURIComponent(name)}`;
}

/** pdf, image or other – as the backend's fileType. */
export function docType(name: string): string {
    const ext = name.slice(name.lastIndexOf(".") + 1).toLowerCase();
    if (ext === "pdf") return "pdf";
    return ["jpg", "jpeg", "png", "webp", "gif", "bmp", "tif", "tiff", "heic", "heif"].includes(ext) ? "image" : "other";
}

/** Splits dropped files into those within the size limit and the others. */
export function bySize(files: File[]): {fitting: File[]; tooBig: File[]} {
    const limit = MAX_FILE_MB * 1024 * 1024;
    return {fitting: files.filter(f => f.size <= limit), tooBig: files.filter(f => f.size > limit)};
}

/** "„a.pdf“ ist größer als 40 MB und wurde nicht hinzugefügt." */
export function tooBigText(tooBig: File[]): string {
    const names = tooBig.map(f => `„${f.name}“`).join(", ");
    const one = tooBig.length === 1;
    return `${names} ${one ? "ist" : "sind"} größer als ${MAX_FILE_MB} MB und ${one ? "wurde" : "wurden"} nicht hinzugefügt.`;
}

/**
 * Drag & drop of files onto an element: spread the handlers onto it and show
 * a hint while `dragging`. Other drags (text, elements) are ignored.
 */
export function useFileDrop(onFiles: (files: File[]) => void) {
    const [dragging, setDragging] = useState(false);
    // dragenter/dragleave also fire when moving between child elements: count them.
    const depth = useRef(0);
    const isFiles = (e: DragEvent) => e.dataTransfer.types.includes("Files");
    const handlers = {
        onDragEnter(e: DragEvent) {
            if (!isFiles(e)) return;
            depth.current++;
            setDragging(true);
        },
        onDragOver(e: DragEvent) {
            if (isFiles(e)) e.preventDefault();
        },
        onDragLeave(e: DragEvent) {
            if (!isFiles(e)) return;
            depth.current = Math.max(0, depth.current - 1);
            if (depth.current === 0) setDragging(false);
        },
        onDrop(e: DragEvent) {
            if (!isFiles(e)) return;
            e.preventDefault();
            depth.current = 0;
            setDragging(false);
            const files = [...e.dataTransfer.files];
            if (files.length) onFiles(files);
        },
    };
    return {dragging, handlers};
}
