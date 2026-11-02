// Turns positioned text pieces (from pdf.js or OCR) into lines of text.
// Kept free of browser APIs so it can be unit-tested.

export interface Piece {
    text: string;
    x: number; // 0..1 of the page width, left edge
    y: number; // 0..1 of the page height, top edge
    w: number;
    h: number;
}

export interface TextLine {
    text: string;
    page: number;
    x: number;
    y: number;
    w: number;
    h: number;
}

/**
 * Groups pieces whose vertical centres are close into lines (sorted top to
 * bottom, pieces left to right). Wide gaps become three spaces so columns stay
 * apart ("Rechnungsbetrag   286,40").
 */
export function groupLines(pieces: Piece[], page: number): TextLine[] {
    const items = pieces.filter(p => p.text.trim() !== "").sort((a, b) => (a.y + a.h / 2) - (b.y + b.h / 2));
    const rows: Piece[][] = [];
    for (const p of items) {
        const c = p.y + p.h / 2;
        const row = rows.find(r => {
            const rc = r[0].y + r[0].h / 2;
            return Math.abs(rc - c) < Math.max(r[0].h, p.h, 0.004) * 0.5;
        });
        if (row) row.push(p);
        else rows.push([p]);
    }
    return rows
        .map(r => {
            r.sort((a, b) => a.x - b.x);
            let text = "";
            let end = -1;
            for (const p of r) {
                if (end >= 0) {
                    const gap = p.x - end;
                    text += gap > Math.max(p.h, 0.01) * 1.5 ? "   " : gap > 0.001 ? " " : "";
                }
                text += p.text.trim();
                end = p.x + p.w;
            }
            const x = r[0].x;
            const y = Math.min(...r.map(p => p.y));
            const right = Math.max(...r.map(p => p.x + p.w));
            const h = Math.max(...r.map(p => p.h));
            return {text, page, x, y, w: right - x, h};
        })
        .sort((a, b) => a.y - b.y);
}
