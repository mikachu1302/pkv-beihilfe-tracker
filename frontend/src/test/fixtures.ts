// Made-up test data (no real invoices).
import type {ClaimViewDTO, InvoiceRowDTO, MarkDTO, PayerDTO, SettingsDTO} from "../api";

export const noMark: MarkDTO = {done: false, date: "", note: ""};

export function claimView(payerId: string, payerName: string, kind: string, patch: Partial<ClaimViewDTO> = {}): ClaimViewDTO {
    return {
        payerId, payerName, kind, rate: kind === "pkv" ? "30" : "70", eligible: "286.40",
        expected: kind === "pkv" ? "85.92" : "200.48", actual: "0.00", diff: "0.00", status: "open",
        submitted: noMark, refunded: noMark, clarify: noMark, held: false, skip: false, keepsPremiumRefund: false,
        suggestClarify: false, deadline: "", deadlineDays: 0, ...patch,
    };
}

export function invoiceRow(id: string, provider: string, patch: Partial<InvoiceRowDTO> = {}): InvoiceRowDTO {
    return {
        id, provider, category: "arzt", reference: "", invoiceDate: "2026-03-14", serviceDate: "2026-03-03", year: 2026,
        amount: "286.40", paid: noMark, expected: "286.40", actual: "0.00", ownShare: "286.40", settled: false,
        warnings: [], note: "", attachments: 0,
        claims: [claimView("bh", "Beihilfe", "beihilfe"), claimView("pkv", "Muster PKV", "pkv")],
        ...patch,
    };
}

export const payers: PayerDTO[] = [
    {id: "bh", kind: "beihilfe", name: "Beihilfe", claimDeadlineYears: 3, minClaimAmount: "200.00", note: ""},
    {id: "pkv", kind: "pkv", name: "Muster PKV", claimDeadlineYears: 0, minClaimAmount: "", note: ""},
];

export const settings: SettingsDTO = {
    payers,
    coverage: [{from: "2023-01-01", shares: [{payerId: "bh", rate: "70"}, {payerId: "pkv", rate: "30"}]}],
    years: [], aliases: [], refundLayouts: [], clarifyTolerance: "1.00",
};
