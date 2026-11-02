import {screen, waitFor} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {beforeEach, describe, expect, it, vi} from "vitest";
import type {InvoiceDTO} from "../api";
import {invoiceRow, noMark} from "../test/fixtures";
import {renderAt} from "../test/render";
import InvoiceForm from "./InvoiceForm";

const svc = vi.hoisted(() => ({
    NewInvoice: vi.fn(), GetInvoice: vi.fn(), PreviewInvoice: vi.fn(), SaveInvoice: vi.fn(), DeleteInvoice: vi.fn(),
}));
vi.mock("../api", () => ({Svc: svc}));

function claim(payerId: string) {
    return {payerId, eligible: "", rate: "", expected: "", submitted: noMark, refunded: noMark, clarify: noMark,
        held: false, skip: false, keepsPremiumRefund: false, submissionId: "", refunds: []};
}

const blank: InvoiceDTO = {
    id: "", provider: "", payee: "", iban: "", reference: "", category: "", invoiceDate: "2026-11-02", serviceFrom: "",
    serviceTo: "", serviceYear: 0, amount: "0.00", due: "", paid: noMark, claims: [claim("bh"), claim("pkv")],
    attachments: [], note: "",
};

beforeEach(() => {
    vi.clearAllMocks();
    svc.NewInvoice.mockResolvedValue({invoice: blank, view: invoiceRow("", "", {amount: "0.00", expected: "0.00"})});
    svc.PreviewInvoice.mockResolvedValue({invoice: blank, view: invoiceRow("", "Dr. Muster")});
});

describe("InvoiceForm", () => {
    it("saves a new invoice with the German amount and the ticked mark", async () => {
        const user = userEvent.setup();
        const setYear = vi.fn();
        svc.SaveInvoice.mockResolvedValue({invoice: {...blank, id: "x"}, view: invoiceRow("x", "Dr. Muster", {year: 2026})});
        renderAt("/rechnung/neu", "/rechnung/neu", <InvoiceForm/>, {setYear});

        await user.type(await screen.findByLabelText("Behandler *"), "Dr. Muster");
        await user.type(screen.getByLabelText("Betrag (€) *"), "286,40");
        // Live calculation shows the payer cards with SOLL.
        expect(await screen.findByText("200,48 €")).toBeInTheDocument();
        const bh = screen.getByRole("region", {name: "Beihilfe"});
        await user.click(bh.querySelector("input[type=checkbox]")!); // eingereicht
        await user.click(screen.getByRole("button", {name: "Speichern"}));

        await waitFor(() => expect(svc.SaveInvoice).toHaveBeenCalled());
        const sent: InvoiceDTO = svc.SaveInvoice.mock.calls[0][0];
        expect(sent.provider).toBe("Dr. Muster");
        expect(sent.amount).toBe("286,40");
        expect(sent.claims?.[0].submitted.done).toBe(true);
        expect(sent.claims?.[0].submitted.date).toMatch(/^\d{4}-\d{2}-\d{2}$/);
        expect(setYear).toHaveBeenCalledWith(2026);
        expect(await screen.findByText("andere Seite")).toBeInTheDocument(); // navigated to the list
    });

    it("saves with Ctrl+S, also while typing in a field", async () => {
        const user = userEvent.setup();
        svc.SaveInvoice.mockResolvedValue({invoice: {...blank, id: "x"}, view: invoiceRow("x", "Dr. Muster", {year: 2026})});
        renderAt("/rechnung/neu", "/rechnung/neu", <InvoiceForm/>);
        await user.type(await screen.findByLabelText("Behandler *"), "Dr. Muster");
        await user.keyboard("{Control>}s{/Control}");
        await waitFor(() => expect(svc.SaveInvoice).toHaveBeenCalledTimes(1));
        expect(svc.SaveInvoice.mock.calls[0][0].provider).toBe("Dr. Muster");
    });

    it("asks before leaving with unsaved changes", async () => {
        const user = userEvent.setup();
        renderAt("/rechnung/neu", "/rechnung/neu", <InvoiceForm/>);
        await user.type(await screen.findByLabelText("Behandler *"), "Dr. Muster");
        await user.click(screen.getByRole("button", {name: "Abbrechen"}));
        expect(await screen.findByRole("alertdialog")).toHaveTextContent("noch nicht gespeichert");
    });

    it("shows backend validation errors", async () => {
        const user = userEvent.setup();
        svc.SaveInvoice.mockRejectedValue(new Error("Behandler fehlt\nBetrag fehlt"));
        renderAt("/rechnung/neu", "/rechnung/neu", <InvoiceForm/>);
        await user.click(await screen.findByRole("button", {name: "Speichern"}));
        expect(await screen.findByText(/Behandler fehlt/)).toBeInTheDocument();
    });
});
