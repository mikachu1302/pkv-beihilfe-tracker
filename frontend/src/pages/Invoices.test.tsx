import {screen, waitFor, within} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {beforeEach, describe, expect, it, vi} from "vitest";
import {claimView, invoiceRow, settings} from "../test/fixtures";
import {renderAt} from "../test/render";
import Invoices, {matchesFilter} from "./Invoices";

const svc = vi.hoisted(() => ({
    Years: vi.fn(), GetSettings: vi.fn(), ListInvoices: vi.fn(), SetMark: vi.fn(), BulkSubmit: vi.fn(),
}));
vi.mock("../api", () => ({Svc: svc}));

const rows = [
    invoiceRow("a", "Dr. Muster"),
    invoiceRow("b", "Apotheke am Markt", {
        amount: "45.10", claims: [
            claimView("bh", "Beihilfe", "beihilfe", {status: "submitted", submitted: {done: true, date: "2026-04-02", note: ""}}),
            claimView("pkv", "Muster PKV", "pkv", {status: "held", held: true}),
        ],
    }),
];

beforeEach(() => {
    vi.clearAllMocks();
    svc.Years.mockResolvedValue([2026, 2025]);
    svc.GetSettings.mockResolvedValue(settings);
    svc.ListInvoices.mockResolvedValue(rows);
});

describe("Invoices", () => {
    it("lists invoices with one column group per payer", async () => {
        renderAt("/", "/", <Invoices/>);
        expect(await screen.findByText("Dr. Muster")).toBeInTheDocument();
        expect(screen.getByRole("columnheader", {name: "Beihilfe"})).toBeInTheDocument();
        expect(screen.getByRole("columnheader", {name: "Muster PKV"})).toBeInTheDocument();
        expect(svc.ListInvoices).toHaveBeenCalledWith(2026);
        expect(screen.getByText("2 Rechnungen")).toBeInTheDocument();
    });

    it("ticks a checkbox and shows the returned row", async () => {
        const user = userEvent.setup();
        svc.SetMark.mockResolvedValue(invoiceRow("a", "Dr. Muster", {
            claims: [claimView("bh", "Beihilfe", "beihilfe", {status: "submitted", submitted: {done: true, date: "2026-11-02", note: ""}}),
                claimView("pkv", "Muster PKV", "pkv")],
        }));
        renderAt("/", "/", <Invoices/>);
        const row = (await screen.findByText("Dr. Muster")).closest("tr")!;
        await user.click(within(row).getAllByLabelText("eingereicht")[0]);
        expect(svc.SetMark).toHaveBeenCalledWith({invoiceId: "a", payerId: "bh", mark: "submitted", on: true, date: "", note: "", amount: ""});
        expect(await within(row).findByText("02.11.26")).toBeInTheDocument();
    });

    it("asks for the refund amount, prefilled with the SOLL", async () => {
        const user = userEvent.setup();
        svc.SetMark.mockResolvedValue(rows[0]);
        renderAt("/", "/", <Invoices/>);
        const row = (await screen.findByText("Dr. Muster")).closest("tr")!;
        await user.click(within(row).getAllByLabelText("erstattet")[0]);
        const amount = screen.getByLabelText("Erstattet (€)");
        expect(amount).toHaveValue("200,48");
        await user.clear(amount);
        await user.type(amount, "180,20");
        await user.click(screen.getByRole("button", {name: "Übernehmen"}));
        await waitFor(() => expect(svc.SetMark).toHaveBeenCalled());
        expect(svc.SetMark.mock.calls[0][0]).toMatchObject({invoiceId: "a", payerId: "bh", mark: "refunded", on: true, amount: "180,20"});
    });

    it("marks several invoices as submitted at once", async () => {
        const user = userEvent.setup();
        svc.BulkSubmit.mockResolvedValue({marked: 2, skipped: []});
        renderAt("/", "/", <Invoices/>);
        await screen.findByText("Dr. Muster");
        await user.click(screen.getByLabelText("Alle auswählen"));
        expect(screen.getByText("2 Rechnungen ausgewählt")).toBeInTheDocument();
        await user.selectOptions(screen.getByLabelText("Kostenträger"), "pkv");
        await user.click(screen.getByRole("button", {name: "Als eingereicht markieren"}));
        await waitFor(() => expect(svc.BulkSubmit).toHaveBeenCalled());
        expect(svc.BulkSubmit.mock.calls[0][0]).toMatchObject({invoiceIds: ["a", "b"], payerId: "pkv"});
    });

    it("filters by status", async () => {
        const user = userEvent.setup();
        renderAt("/", "/", <Invoices/>);
        await screen.findByText("Dr. Muster");
        await user.click(screen.getByRole("button", {name: "Erstattung offen"}));
        expect(screen.queryByText("Dr. Muster")).not.toBeInTheDocument();
        expect(screen.getByText("Apotheke am Markt")).toBeInTheDocument();
        expect(matchesFilter(rows[1], "open")).toBe(true); // PKV held back counts as not submitted
        expect(matchesFilter(rows[0], "unpaid")).toBe(true);
    });
});
