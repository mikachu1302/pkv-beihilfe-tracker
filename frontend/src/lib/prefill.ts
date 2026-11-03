// Prefilling an invoice from a recognised document.
import type {InvoiceDTO} from "../api";
import {toInput} from "./format";

/** How a value was found, as shown next to the field. */
export const SOURCE: Record<string, [string, "good" | "warn" | undefined]> = {
    qr: ["aus GiroCode", "good"],
    label: ["erkannt", undefined],
    guess: ["bitte prüfen", "warn"],
};

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
    {next: InvoiceDTO; filled: string[]} {
    const next: InvoiceDTO = {...cur};
    const filled: string[] = [];
    const free = (f: (typeof TEXT_FIELDS)[number]) =>
        !cur[f].trim() || replaceable.includes(f) || (defaults !== undefined && cur[f] === defaults[f]);
    for (const f of TEXT_FIELDS) {
        if (found[f] && found[f] !== cur[f] && free(f)) {
            next[f] = found[f];
            filled.push(f);
        }
    }
    if (found.amount && found.amount !== "0.00" && (!cur.amount.trim() || replaceable.includes("amount"))) {
        next.amount = toInput(found.amount);
        filled.push("amount");
    }
    if (found.paid.done && (!cur.paid.done || replaceable.includes("paid"))) {
        next.paid = {...found.paid};
        filled.push("paid");
    }
    return {next, filled};
}
