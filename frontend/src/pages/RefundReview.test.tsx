import {screen, waitFor} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {beforeEach, describe, expect, it, vi} from "vitest";
import type {RefundLineDTO, RefundReviewDTO} from "../api";
import {payers} from "../test/fixtures";
import {renderAt} from "../test/render";
import RefundReview from "./RefundReview";

const svc = vi.hoisted(() => ({ReviewRefund: vi.fn(), ConfirmRefund: vi.fn()}));
vi.mock("../api", () => ({Svc: svc}));

function line(id: string, provider: string, billed: string, refund: string, match: string, invoice?: string): RefundLineDTO {
    return {id, date: "2026-03-03", provider, amounts: [billed, refund], billed, eligible: "", refund, edited: false,
        invoiceIds: invoice ? [invoice] : [], match, score: 90,
        candidates: invoice ? [{invoiceId: invoice, label: `${provider} · 03.03.2026 · ${billed} €`, score: 90, exact: true, reasons: ["Betrag stimmt"]}] : []};
}

const review: RefundReviewDTO = {
    payerId: "bh", date: "2026-04-28", applicationDate: "2026-04-02", payoutDate: "", paidOut: "120.52",
    deductions: [{label: "Kostendämpfungspauschale", amount: "100.00"}],
    lines: [line("p1", "Dr. Muster", "286.40", "180.20", "auto", "inv1"), line("p2", "Unbekannt", "420.00", "40.32", "none")],
    layout: {billed: 1, eligible: 0, refund: -1}, linesTotal: "220.52", checksumOk: true, checksumDiff: "0.00", notes: [],
    invoices: [{invoiceId: "inv2", label: "Labor Nord · 20.03.2026 · 57,60 €", score: 0, exact: false, reasons: []}],
};

beforeEach(() => {
    vi.clearAllMocks();
    svc.ReviewRefund.mockImplementation(async (r: RefundReviewDTO) => r);
});

describe("RefundReview", () => {
    it("shows lines, matches and the checksum, and books the refunds", async () => {
        const user = userEvent.setup();
        const onDone = vi.fn();
        svc.ConfirmRefund.mockResolvedValue({docId: "d1", invoices: 2, aliases: 1});
        renderAt("/", "/", <RefundReview inboxName="Bescheid.pdf" initial={review} payers={payers} onDone={onDone} onDiscard={() => {}}/>);

        expect(screen.getByLabelText("Zeile 1: Erstattung")).toHaveValue("180,20");
        expect(screen.getByText("sicher")).toBeInTheDocument();
        expect(screen.getByText("keine Rechnung")).toBeInTheDocument();
        expect(screen.getByText(/entspricht der Auszahlung/)).toBeInTheDocument();
        expect(screen.getByRole("button", {name: "Erstattungen übernehmen (1 von 2)"})).toBeEnabled();

        // Assign line 2 by hand to another open invoice.
        await user.selectOptions(screen.getByLabelText("Zeile 2: Rechnung"), "inv2");
        await waitFor(() => expect(svc.ReviewRefund).toHaveBeenCalled());
        const sent: RefundReviewDTO = svc.ReviewRefund.mock.calls.at(-1)![0];
        expect(sent.lines![1]).toMatchObject({invoiceIds: ["inv2"], match: "confirmed"});
        expect(await screen.findByText("gewählt")).toBeInTheDocument();

        await user.click(screen.getByRole("button", {name: "Erstattungen übernehmen (2 von 2)"}));
        await waitFor(() => expect(svc.ConfirmRefund).toHaveBeenCalled());
        const [name, booked, saveLayout] = svc.ConfirmRefund.mock.calls[0];
        expect(name).toBe("Bescheid.pdf");
        expect(booked.lines[1].invoiceIds).toEqual(["inv2"]);
        expect(saveLayout).toBe(true);
        expect(onDone).toHaveBeenCalled();
        expect(await screen.findByText(/Erstattungen für 2 Rechnungen übernommen – 1 Namensvariante gelernt/)).toBeInTheDocument();
    });

    it("marks an edited amount and reports a checksum gap", async () => {
        const user = userEvent.setup();
        svc.ReviewRefund.mockImplementation(async (r: RefundReviewDTO) => ({...r, checksumOk: false, checksumDiff: "-10.00"}));
        renderAt("/", "/", <RefundReview inboxName="" initial={review} payers={payers} onDone={() => {}} onDiscard={() => {}}/>);
        const refund = screen.getByLabelText("Zeile 1: Erstattung");
        await user.clear(refund);
        await user.type(refund, "170,20");
        await waitFor(() => expect(svc.ReviewRefund).toHaveBeenCalled(), {timeout: 2000});
        const sent: RefundReviewDTO = svc.ReviewRefund.mock.calls.at(-1)![0];
        expect(sent.lines![0]).toMatchObject({refund: "170,20", edited: true});
        expect(await screen.findByText(/Differenz -10,00 €/)).toBeInTheDocument();
    });
});
