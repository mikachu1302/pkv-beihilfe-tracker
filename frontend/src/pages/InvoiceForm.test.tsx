import {screen, waitFor, within} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {beforeEach, describe, expect, it, vi} from "vitest";
import type {InvoiceDTO} from "../api";
import {claimView, invoiceRow, noMark} from "../test/fixtures";
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

    it("adds a refund with the open SOLL when \"erstattet\" is ticked without an amount", async () => {
        const user = userEvent.setup();
        const inv = {...blank, id: "a", provider: "Dr. Muster", amount: "286.40"};
        svc.GetInvoice.mockResolvedValue({invoice: inv, view: invoiceRow("a", "Dr. Muster")});
        svc.PreviewInvoice.mockResolvedValue({invoice: inv, view: invoiceRow("a", "Dr. Muster")});
        renderAt("/rechnung/a", "/rechnung/:id", <InvoiceForm/>);
        const bh = await screen.findByRole("region", {name: "Beihilfe"});
        await user.click(within(bh).getByLabelText("erstattet"));
        expect(within(bh).getByLabelText("Erstattungsbetrag")).toHaveValue("200,48"); // 70 % of 286,40
        expect(within(bh).getByLabelText("erstattet")).toBeChecked();
        await user.click(within(bh).getByLabelText("erstattet")); // untick: the manual refund goes again
        expect(within(bh).queryByLabelText("Erstattungsbetrag")).not.toBeInTheDocument();
    });

    it("shows \"erstattet\" for refunds and warns about a tick without an amount", async () => {
        const user = userEvent.setup();
        const done = {done: true, date: "2026-11-02", note: ""};
        const inv = {...blank, id: "a", provider: "Dr. Muster", amount: "286.40", claims: [
            {...claim("bh"), refunds: [{id: "r1", amount: "22,21", date: "", docId: "", lineId: "", note: "aus Excel übernommen"}]},
            {...claim("pkv"), refunded: done},
        ]};
        const view = invoiceRow("a", "Dr. Muster", {claims: [
            claimView("bh", "Beihilfe", "beihilfe", {status: "refunded", actual: "22.21"}),
            claimView("pkv", "Muster PKV", "pkv", {status: "refunded", refunded: done, suggestClarify: true, diff: "-85.92"}),
        ]});
        svc.GetInvoice.mockResolvedValue({invoice: inv, view});
        renderAt("/rechnung/a", "/rechnung/:id", <InvoiceForm/>);
        const bh = await screen.findByRole("region", {name: "Beihilfe"});
        expect(within(bh).getByLabelText("erstattet")).toBeChecked();
        const pkv = screen.getByRole("region", {name: "Muster PKV"});
        expect(within(pkv).getByText(/kein Betrag erfasst/)).toBeInTheDocument();
        expect(within(pkv).queryByText(/Abweichung akzeptieren/)).not.toBeInTheDocument();
        await user.click(within(pkv).getByRole("button", {name: "Offenes SOLL als Erstattung eintragen"}));
        expect(within(pkv).getByLabelText("Erstattungsbetrag")).toBeInTheDocument();
    });

    it("shows backend validation errors", async () => {
        const user = userEvent.setup();
        svc.SaveInvoice.mockRejectedValue(new Error("Behandler fehlt\nBetrag fehlt"));
        renderAt("/rechnung/neu", "/rechnung/neu", <InvoiceForm/>);
        await user.click(await screen.findByRole("button", {name: "Speichern"}));
        expect(await screen.findByText(/Behandler fehlt/)).toBeInTheDocument();
    });
});
