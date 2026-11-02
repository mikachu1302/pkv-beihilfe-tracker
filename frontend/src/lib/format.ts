// Formatting helpers (German). Amounts arrive as "1234.56", dates as "YYYY-MM-DD".

const eurFmt = new Intl.NumberFormat("de-DE", {minimumFractionDigits: 2, maximumFractionDigits: 2});
const eurCompact = new Intl.NumberFormat("de-DE", {maximumFractionDigits: 0});

/** "1.234,56 €" – or "–" for empty values. */
export function eur(v: string | number | undefined | null): string {
    if (v === undefined || v === null || v === "") return "–";
    const n = typeof v === "number" ? v : Number(v);
    if (Number.isNaN(n)) return String(v);
    return `${eurFmt.format(n)} €`;
}

/** "1.235 €" (rounded) for axes and tight spaces. */
export function eurRound(v: string | number | undefined | null): string {
    if (v === undefined || v === null || v === "") return "–";
    return `${eurCompact.format(Number(v))} €`;
}

export function num(v: string | number | undefined | null): number {
    const n = Number(v ?? 0);
    return Number.isNaN(n) ? 0 : n;
}

/**
 * Reads an amount as people write it or OCR returns it – "286,40", "1.234,56 €",
 * "286.40", "-12,50", "12,50-", "EUR 7" – with the same rules as the backend
 * (money.Parse): a single "." followed by exactly three digits is a thousands
 * separator ("1.234" = 1234). NaN if the text is not an amount.
 */
export function parseAmount(v: string | number | undefined | null): number {
    if (typeof v === "number") return v;
    let s = (v ?? "").trim().replace(/€|EUR|Eur|eur|[\s\u00a0\u202f']/g, "");
    let neg = false;
    if (s.startsWith("-")) [neg, s] = [true, s.slice(1)];
    else if (s.endsWith("-")) [neg, s] = [true, s.slice(0, -1)];
    else if (s.startsWith("+")) s = s.slice(1);
    const dot = s.lastIndexOf("."), comma = s.lastIndexOf(",");
    let int = s, frac = "";
    if (dot >= 0 && comma >= 0) {
        const [sep, thousands] = dot > comma ? [dot, ","] : [comma, "."];
        [int, frac] = [s.slice(0, sep).replaceAll(thousands, ""), s.slice(sep + 1)];
    } else if (comma >= 0) {
        if (s.indexOf(",") !== comma) int = s.replaceAll(",", ""); // 1,234,567
        else [int, frac] = [s.slice(0, comma), s.slice(comma + 1)];
    } else if (dot >= 0) {
        if (s.indexOf(".") !== dot || s.length - dot - 1 === 3) int = s.replaceAll(".", "");
        else [int, frac] = [s.slice(0, dot), s.slice(dot + 1)];
    }
    if ((int === "" && frac === "") || !/^\d*$/.test(int) || !/^\d*$/.test(frac)) return NaN;
    const n = Number(`${int || "0"}.${frac || "0"}`);
    return neg ? -n : n;
}

/** CSS class for positive / negative numbers. */
export function signClass(v: string | number | undefined | null): string {
    const n = num(v);
    if (n > 0) return "pos";
    if (n < 0) return "neg";
    return "";
}

/** "2026-03-14" → "14.03.2026". */
export function date(iso: string | undefined | null): string {
    if (!iso) return "–";
    const [y, m, d] = iso.split("-");
    return d && m && y ? `${d}.${m}.${y}` : iso;
}

/** "2026-03-14" → "14.03.26" for checkbox labels. */
export function shortDate(iso: string | undefined | null): string {
    if (!iso) return "";
    const [y, m, d] = iso.split("-");
    return d && m && y ? `${d}.${m}.${y.slice(2)}` : iso;
}

/** Today as "YYYY-MM-DD" in local time. */
export function today(): string {
    const t = new Date();
    const pad = (n: number) => String(n).padStart(2, "0");
    return `${t.getFullYear()}-${pad(t.getMonth() + 1)}-${pad(t.getDate())}`;
}

/** "286.40" → "286,40" for input fields ("" stays ""). */
export function toInput(amount: string | undefined | null): string {
    if (!amount) return "";
    return amount.replace(".", ",");
}

/** "70" → "70 %". */
export function pct(rate: string | number | undefined | null): string {
    if (rate === undefined || rate === null || rate === "") return "–";
    return `${String(rate).replace(".", ",")} %`;
}

export function errMsg(e: unknown): string {
    if (e instanceof Error) return e.message;
    if (typeof e === "string") return e;
    if (e && typeof e === "object" && "message" in e) return String((e as {message: unknown}).message);
    return String(e);
}

export const CATEGORY_LABELS: Record<string, string> = {
    arzt: "Arzt",
    zahnarzt: "Zahnarzt",
    apotheke: "Apotheke",
    heilmittel: "Heilmittel",
    hilfsmittel: "Hilfsmittel",
    krankenhaus: "Krankenhaus",
    heilpraktiker: "Heilpraktiker",
    sonstiges: "Sonstiges",
};

export const STATUS_LABELS: Record<string, string> = {
    open: "nicht eingereicht",
    held: "zurückgehalten",
    submitted: "eingereicht",
    refunded: "erstattet",
    skipped: "verzichtet",
};

export const KIND_LABELS: Record<string, string> = {beihilfe: "Beihilfe", pkv: "PKV"};

/** "1 Rechnung" / "3 Rechnungen". */
export function plural(n: number, one: string, many: string): string {
    return `${n.toLocaleString("de-DE")} ${n === 1 ? one : many}`;
}
