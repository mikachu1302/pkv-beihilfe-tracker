import {useEffect, useRef, useState} from "react";
import {renderPdfPreview} from "../lib/capture";

/**
 * A small preview of a document page: the photo, or the first page of a PDF.
 * Files the webview cannot show (HEIC photos, broken PDFs) get a label.
 */
export function DocThumb({url, type, label, width = 96}: {url: string; type: string; label: string; width?: number}) {
    const canvas = useRef<HTMLCanvasElement>(null);
    const [failed, setFailed] = useState(false);
    useEffect(() => {
        setFailed(false);
        if (type !== "pdf" || !canvas.current) return;
        const run = new AbortController();
        renderPdfPreview(url, canvas.current, width * 2, run.signal).catch(() => !run.signal.aborted && setFailed(true));
        return () => run.abort();
    }, [url, type, width]);
    if (type === "image" && !failed) {
        return <img className="thumb" src={url} alt={label} draggable={false} onError={() => setFailed(true)}/>;
    }
    if (type === "pdf") {
        // The canvas stays in place (hidden on failure), so a new URL can render into it.
        return (
            <>
                <canvas className="thumb" ref={canvas} role="img" aria-label={label} hidden={failed}/>
                {failed && <span className="thumb thumb-text" role="img" aria-label={label}>PDF</span>}
            </>
        );
    }
    return <span className="thumb thumb-text" role="img" aria-label={label}>{type === "image" ? "Bild" : "Datei"}</span>;
}
