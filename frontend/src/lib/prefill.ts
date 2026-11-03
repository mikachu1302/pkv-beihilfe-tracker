// Prefilling an invoice from a recognised document.
import type {AIReadDTO, InvoiceDTO} from "../api";
import {CATEGORY_LABELS, date, eur, toInput} from "./format";

/** How a value was found, as shown next to the field (ordered by trust, see internal/airead). */
export const SOURCE: Record<string, [string, "good" | "warn" | undefined]> = {
    qr: ["aus GiroCode", "good"],
    "ai-confirmed": ["KI, bestätigt", "good"],
    label: ["erkannt", undefined],
    ai: ["KI – bitte prüfen", "warn"],
    guess: ["bitte prüfen", "warn"],
};

/** Who found a value, for the hint with another reading ("KI: 98,50 €"). */
const FINDER: Record<string, string> = {qr: "GiroCode", "ai-confirmed": "KI", ai: "KI", label: "Texterkennung", guess: "Texterkennung"};

const TEXT_FIELDS = ["provider", "payee", "iban", "reference", "category", "invoiceDate", "serviceFrom", "serviceTo", "due"] as const;

/**
 * Copies what the document says into the fields that are still empty –
 * nothing the user typed is overwritten. A field still holding the value a
 * new form starts with (`defaults`, e.g. today as invoice date) counts as
 * empty, and so does one filled from a document before and not edited since
 * (`replaceable`: more pages may know better, e.g. the total on the last
 * page). Returns the new form and the names of the fields it filled.
 * `found` is the recognised draft as the backend sends it (amount "286.40").
 */
export function fillEmpty(cur: InvoiceDTO, found: InvoiceDTO, defaults?: InvoiceDTO, replaceable: string[] = []):
    {next: InvoiceDTO; filled: string[]; same: string[]} {
    const next: InvoiceDTO = {...cur};
    const filled: string[] = [];
    // Fields the document says the same about (their badge may change, e.g. to "aus GiroCode").
    const same: string[] = [];
    const free = (f: (typeof TEXT_FIELDS)[number]) =>
        !cur[f].trim() || replaceable.includes(f) || (defaults !== undefined && cur[f] === defaults[f]);
    for (const f of TEXT_FIELDS) {
        if (found[f] && found[f] === cur[f]) same.push(f);
        else if (found[f] && free(f)) {
            next[f] = found[f];
            filled.push(f);
        }
    }
    if (found.amount && found.amount !== "0.00") {
        if (toInput(found.amount) === cur.amount) same.push("amount");
        else if (!cur.amount.trim() || replaceable.includes("amount")) {
            next.amount = toInput(found.amount);
            filled.push("amount");
        }
    }
    if (found.paid.done) {
        const samePaid = cur.paid.done && cur.paid.date === found.paid.date && cur.paid.note === found.paid.note;
        if (samePaid) same.push("paid");
        else if (!cur.paid.done || replaceable.includes("paid")) {
            next.paid = {...found.paid};
            filled.push("paid");
        }
    }
    return {next, filled, same};
}

/** Another reading of a field: a value that lost, or the AI's where the user typed something else. */
export interface Alternative {
    source: string;
    value: string; // as the backend sends it: "286.40", "2026-03-14"
}

/** "KI: 98,50 €" */
export function alternativeText(field: string, a: Alternative): string {
    const v = field === "amount" ? eur(a.value)
        : ["invoiceDate", "serviceFrom", "serviceTo", "due"].includes(field) ? date(a.value)
            : field === "category" ? CATEGORY_LABELS[a.value] ?? a.value : a.value;
    return `${FINDER[a.source] ?? a.source}: ${v}`;
}

/** The value of an alternative as the form holds it (amounts "286,40"). */
export const alternativeInput = (field: string, a: Alternative) => (field === "amount" ? toInput(a.value) : a.value);

/**
 * Takes an AI reading (merged with the text recognition by the backend)
 * into the form like fillEmpty – typed values stay. Returns as well the
 * other readings to show next to the fields: values that lost, and the
 * reading where the user typed something else.
 */
export function applyReading(cur: InvoiceDTO, r: AIReadDTO, defaults?: InvoiceDTO, replaceable: string[] = []):
    {next: InvoiceDTO; filled: string[]; others: Record<string, Alternative>} {
    const found = r.draft.invoice;
    const {next, filled} = fillEmpty(cur, found, defaults, replaceable);
    const others: Record<string, Alternative> = {};
    for (const [f, o] of Object.entries(r.others ?? {})) if (o) others[f] = o;
    for (const f of [...TEXT_FIELDS, "amount"] as const) {
        if (!found[f] || found[f] === "0.00" || filled.includes(f)) continue;
        if (alternativeInput(f, {source: "", value: found[f]}) !== next[f]) others[f] = {source: r.sources?.[f] ?? "ai", value: found[f]};
    }
    return {next, filled, others};
}

/** The record without these keys (the badges and other readings of fields the user changed). */
export function omit<T>(o: Record<string, T>, keys: string[]): Record<string, T> {
    return Object.fromEntries(Object.entries(o).filter(([k]) => !keys.includes(k)));
}
