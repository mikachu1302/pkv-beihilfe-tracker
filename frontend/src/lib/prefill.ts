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
 * empty. Returns the new form and the names of the fields it filled.
 * `found` is the recognised draft as the backend sends it (amount "286.40").
 */
export function fillEmpty(cur: InvoiceDTO, found: InvoiceDTO, defaults?: InvoiceDTO): {next: InvoiceDTO; filled: string[]} {
    const next: InvoiceDTO = {...cur};
    const filled: string[] = [];
    for (const f of TEXT_FIELDS) {
        if (found[f] && found[f] !== cur[f] && (!cur[f].trim() || (defaults !== undefined && cur[f] === defaults[f]))) {
            next[f] = found[f];
            filled.push(f);
        }
    }
    if (found.amount && found.amount !== "0.00" && !cur.amount.trim()) {
        next.amount = toInput(found.amount);
        filled.push("amount");
    }
    if (found.paid.done && !cur.paid.done) {
        next.paid = {...found.paid};
        filled.push("paid");
    }
    return {next, filled};
}
