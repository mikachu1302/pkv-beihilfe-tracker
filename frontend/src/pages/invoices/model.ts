// What the invoice views share: the props they get from the list page and
// small pure helpers about invoices and claims.
import type {ClaimViewDTO, InvoiceRowDTO, MarkChangeDTO, MarkDTO, PayerDTO} from "../../api";
import {date, eur, num, STATUS_LABELS} from "../../lib/format";

/** An open popover: which claim, and refund amount or Klärungsbedarf. */
export interface Pop {
    rowId: string;
    payerId: string;
    kind: "refund" | "clarify";
}

/** What every view gets from the list page: the visible invoices and the actions on them. */
export interface ViewProps {
    /** The invoices passing filter and search, in list order. */
    rows: InvoiceRowDTO[];
    /** One column (group) per payer that occurs in the year, in settings order. */
    payers: PayerDTO[];
    selected: Set<string>;
    toggleOne: (id: string) => void;
    allSelected: boolean;
    toggleAll: () => void;
    pop: Pop | null;
    setPop: (p: Pop | null) => void;
    change: (ch: MarkChangeDTO) => Promise<void>;
    toggleRefunded: (r: InvoiceRowDTO, c: ClaimViewDTO, on: boolean) => void;
    toggleClarify: (r: InvoiceRowDTO, c: ClaimViewDTO) => void;
    /** Opens the invoice form. */
    open: (id: string) => void;
}

/** Colour of a status: pill, text, dot. */
export type Tone = "good" | "warn" | "bad" | "info" | "muted";

export function claimOf(r: InvoiceRowDTO, payerId: string): ClaimViewDTO | undefined {
    return (r.claims ?? []).find(c => c.payerId === payerId);
}

const STATUS_TONES: Record<string, Tone> = {open: "warn", submitted: "info", refunded: "good", held: "muted", skipped: "muted"};

/** Short status of a claim for pills: Klärungsbedarf first, then the status. */
export function claimState(c: ClaimViewDTO): {label: string; tone: Tone} {
    if (c.clarify.done) return {label: "Klärung", tone: "bad"};
    return {label: STATUS_LABELS[c.status] ?? c.status, tone: STATUS_TONES[c.status] ?? "muted"};
}

/** Colour class of the n-th payer column (payer-1, payer-2; further ones in the text colour). */
export function payerClass(index: number): string {
    return index < 2 ? `payer-${index + 1}` : "";
}

/** Text of the deadline when it is near or missed, else "". */
export function deadlineText(c: ClaimViewDTO): string {
    if (!c.deadlineSoon) return "";
    return c.deadlineDays < 0 ? `Frist abgelaufen (${date(c.deadline)})` : `Frist bis ${date(c.deadline)}`;
}

/** The provider as shown, also for invoices without one. */
export function providerName(r: InvoiceRowDTO): string {
    return r.provider.trim() || "(ohne Namen)";
}

/** Name of an invoice for screen readers: provider, treatment date, amount. */
export function rowLabel(r: InvoiceRowDTO): string {
    return `${r.provider.trim() || "Rechnung ohne Namen"}, ${date(r.serviceDate)}, ${eur(r.amount)}`;
}

/** Label of the checkbox that selects an invoice for bulk actions. */
export function selectLabel(r: InvoiceRowDTO): string {
    return `${r.provider.trim() || "Rechnung ohne Namen"} auswählen`;
}

/** The "erstattet" tick: also ticked when refunds were entered without it. */
export function refundedMark(c: ClaimViewDTO): MarkDTO {
    return c.refunded.done || num(c.actual) !== 0 ? {...c.refunded, done: true} : c.refunded;
}

/** A change of one tick; payerId "" for "bezahlt". */
export function markChange(invoiceId: string, payerId: string, mark: "paid" | "submitted" | "refunded" | "clarify", on: boolean,
                           extra: Partial<Pick<MarkChangeDTO, "date" | "note" | "amount">> = {}): MarkChangeDTO {
    return {invoiceId, payerId, mark, on, date: "", note: "", amount: "", ...extra};
}

/**
 * Whether a click inside an invoice should open it: not on controls, links,
 * popovers or while selecting text. A double click always selects a word, so
 * there the selection does not count (and is cleared).
 */
export function opensInvoice(target: HTMLElement, doubleClick = false): boolean {
    if (target.closest("input, button, a, label, select, textarea, .popover, [role=dialog], .markbox, .no-open")) return false;
    if (doubleClick) {
        window.getSelection()?.removeAllRanges();
        return true;
    }
    return !window.getSelection()?.toString();
}
