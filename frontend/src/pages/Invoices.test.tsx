import {fireEvent, screen, waitFor, within} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {beforeEach, describe, expect, it, vi} from "vitest";
import {DEFAULT_APPEARANCE, setAppearance, setSessionView} from "../lib/appearance";
import {applyDesign} from "../lib/design";
import {stubHeights} from "../test/layout";
import {claimView, invoiceRow, manyRows, settings} from "../test/fixtures";
import {renderAt} from "../test/render";
import Invoices, {matchesFilter} from "./Invoices";

const svc = vi.hoisted(() => ({
    Years: vi.fn(), GetSettings: vi.fn(), ListInvoices: vi.fn(), SetMark: vi.fn(), BulkSubmit: vi.fn(),
    Dashboard: vi.fn(), Overview: vi.fn(), SetTableOptions: vi.fn(),
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
    svc.Dashboard.mockResolvedValue(figures("2026"));
    svc.Overview.mockResolvedValue({years: [], total: figures("alle")});
    svc.SetTableOptions.mockResolvedValue(undefined);
});

/** Year figures with outstanding amounts (only what the cards read). */
function figures(tag: string) {
    const o = (total: string) => ({expected: "500.00", actual: "300.00", total, submitted: "150.00", open: "50.00", refunded: "0.00", held: "0.00"});
    return {
        year: 0, payers: [
            {payerId: "bh", payerName: "Beihilfe", claims: 3, outstanding: o(tag === "alle" ? "900.00" : "200.00")},
            {payerId: "pkv", payerName: "Muster PKV", claims: 3, outstanding: {...o("0.00"), held: "120.00"}},
        ],
    };
}

describe("Invoices, classic list", () => {
    beforeEach(() => applyDesign("classic"));

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

    it("finds invoices with plausibility hints", () => {
        const flagged = {...rows[0], warnings: [{code: "date_implausible", text: "Beihilfe: eingereicht am 17.09.2013 – Tippfehler?"}]};
        expect(matchesFilter(flagged, "warnings")).toBe(true);
        expect(matchesFilter({...rows[0], warnings: []}, "warnings")).toBe(false);
    });

    it("shows what each payer still owes, for one year or all years", async () => {
        renderAt("/", "/", <Invoices/>);
        const card = (await screen.findByText("Beihilfe · Ausstand 2026")).closest(".card") as HTMLElement;
        expect(within(card).getByText("200,00 €")).toBeInTheDocument();
        expect(within(card).getByText("SOLL 500,00 € − IST 300,00 €")).toBeInTheDocument();
        expect(within(card).getByText("davon eingereicht, offen 150,00 € · noch nicht eingereicht 50,00 €")).toBeInTheDocument();
        const pkv = screen.getByText("Muster PKV · Ausstand 2026").closest(".card") as HTMLElement;
        expect(within(pkv).getByText("zurückgehalten 120,00 € – nicht eingerechnet")).toBeInTheDocument();
    });

    it("uses the total over all years for \"alle Jahre\"", async () => {
        renderAt("/", "/", <Invoices/>, {year: 0});
        expect(await screen.findByText("Beihilfe · Ausstand alle Jahre")).toBeInTheDocument();
        expect(screen.getByText("900,00 €")).toBeInTheDocument();
        expect(svc.Overview).toHaveBeenCalled();
    });

    it("shows the note as a hint with the text on hover", async () => {
        svc.ListInvoices.mockResolvedValue([invoiceRow("a", "Dr. Muster", {note: "Rückfrage zur Ziffer 5"})]);
        renderAt("/", "/", <Invoices/>);
        const badge = await screen.findByLabelText("Notiz: Rückfrage zur Ziffer 5");
        expect(badge).toHaveAttribute("title", "Rückfrage zur Ziffer 5");
    });

    it("opens the invoice by clicking the row, but not when ticking a box", async () => {
        const user = userEvent.setup();
        svc.SetMark.mockResolvedValue(rows[0]);
        renderAt("/", "/", <Invoices/>);
        const row = (await screen.findByText("Dr. Muster")).closest("tr")!;
        await user.click(within(row).getByLabelText("Dr. Muster auswählen"));
        expect(screen.queryByText("andere Seite")).not.toBeInTheDocument();
        await user.click(within(row).getAllByRole("cell")[3]); // the amount
        expect(await screen.findByText("andere Seite")).toBeInTheDocument();
    });

    it("has an edit link per row", async () => {
        renderAt("/", "/", <Invoices/>);
        expect(await screen.findByRole("link", {name: "Dr. Muster bearbeiten"})).toHaveAttribute("href", "/rechnung/a");
    });
});

describe("Invoices, new design", () => {
    it("starts with the detail view: one card per invoice with a column per payer", async () => {
        renderAt("/", "/", <Invoices/>);
        const card = await screen.findByRole("article", {name: "Dr. Muster, 03.03.2026, 286,40 €"});
        expect(within(card).getByText("Beihilfe")).toBeInTheDocument();
        expect(within(card).getAllByText("nicht eingereicht")).toHaveLength(2);
        expect(screen.getByRole("button", {name: "Detail"})).toHaveAttribute("aria-pressed", "true");
        const other = screen.getByRole("article", {name: /Apotheke am Markt/});
        expect(within(other).getByText("eingereicht", {selector: ".badge"})).toBeInTheDocument();
        expect(within(other).getByText("zurückgehalten", {selector: ".badge"})).toBeInTheDocument();
        expect(screen.getByText("2 Rechnungen")).toBeInTheDocument();
    });

    it("ticks and asks for refunds in the card as in the list", async () => {
        const user = userEvent.setup();
        svc.SetMark.mockResolvedValue(rows[0]);
        renderAt("/", "/", <Invoices/>);
        const card = await screen.findByRole("article", {name: /Dr. Muster/});
        await user.click(within(card).getAllByLabelText("eingereicht")[0]);
        expect(svc.SetMark).toHaveBeenCalledWith({invoiceId: "a", payerId: "bh", mark: "submitted", on: true, date: "", note: "", amount: ""});
        await user.click(within(card).getAllByLabelText("erstattet")[0]);
        expect(screen.getByLabelText("Erstattet (€)")).toHaveValue("200,48");
    });

    it("closes a popover when its invoice is filtered out", async () => {
        const user = userEvent.setup();
        renderAt("/", "/", <Invoices/>);
        const card = await screen.findByRole("article", {name: /Dr. Muster/});
        await user.click(within(card).getAllByLabelText("erstattet")[0]);
        expect(screen.getByRole("dialog", {name: "Erstattung Beihilfe"})).toBeInTheDocument();
        await user.click(screen.getByRole("button", {name: "Erstattung offen"})); // Dr. Muster is not submitted
        await user.click(screen.getByRole("button", {name: "Alle"}));
        expect(await screen.findByRole("article", {name: /Dr. Muster/})).toBeInTheDocument();
        expect(screen.queryByRole("dialog", {name: "Erstattung Beihilfe"})).not.toBeInTheDocument();
    });

    it("lists note, waiting time and documents below the card", async () => {
        svc.ListInvoices.mockResolvedValue([invoiceRow("a", "Dr. Muster", {
            note: "Rückfrage zur Ziffer 5", attachments: 2,
            claims: [claimView("bh", "Beihilfe", "beihilfe", {status: "submitted", submitted: {done: true, date: "2026-08-01", note: ""}, waitingDays: 50, longWait: true})],
        })]);
        renderAt("/", "/", <Invoices/>);
        expect(await screen.findByText("Notiz: Rückfrage zur Ziffer 5")).toBeInTheDocument();
        expect(screen.getByText("Beihilfe: Erstattung seit 7 Wochen offen")).toBeInTheDocument();
        expect(screen.getByText("2 Belege")).toBeInTheDocument();
    });

    it("moves between invoices with the arrow keys, opens with Enter and selects with Space", async () => {
        const user = userEvent.setup();
        renderAt("/", "/", <Invoices/>);
        const first = await screen.findByRole("article", {name: /Dr. Muster/});
        const second = screen.getByRole("article", {name: /Apotheke am Markt/});
        expect(first).toHaveAttribute("tabindex", "0");
        expect(second).toHaveAttribute("tabindex", "-1");
        first.focus();
        await user.keyboard("{ArrowDown}");
        expect(second).toHaveFocus();
        await user.keyboard(" ");
        expect(screen.getByText("1 Rechnung ausgewählt")).toBeInTheDocument();
        await user.keyboard("{ArrowUp}");
        expect(first).toHaveFocus();
        await user.keyboard("{Enter}");
        expect(await screen.findByText("andere Seite")).toBeInTheDocument();
    });

    it("selects all invoices for bulk actions", async () => {
        const user = userEvent.setup();
        renderAt("/", "/", <Invoices/>);
        await screen.findByRole("article", {name: /Dr. Muster/});
        await user.click(screen.getByLabelText("Alle auswählen"));
        expect(screen.getByText("2 Rechnungen ausgewählt")).toBeInTheDocument();
    });

    it("switches the view for this session; the settings decide the default", async () => {
        const user = userEvent.setup();
        const view = renderAt("/", "/", <Invoices/>);
        await screen.findByRole("article", {name: /Dr. Muster/});
        await user.click(screen.getByRole("button", {name: "Klassische Liste"}));
        expect(screen.getByRole("columnheader", {name: "Muster PKV"})).toBeInTheDocument();
        expect(screen.queryByRole("article")).not.toBeInTheDocument();
        view.unmount();

        renderAt("/", "/", <Invoices/>); // still the classic list (same session)
        expect(await screen.findByRole("columnheader", {name: "Muster PKV"})).toBeInTheDocument();
        setSessionView(null);
        expect(await screen.findByRole("article", {name: /Dr. Muster/})).toBeInTheDocument();
        setAppearance({...DEFAULT_APPEARANCE, view: "classic"});
        expect(await screen.findByRole("columnheader", {name: "Muster PKV"})).toBeInTheDocument();
    });

    it("renders only the visible cards when the page scrolls", async () => {
        stubHeights(el => (el.classList.contains("content") ? 800 : el.tagName === "ARTICLE" ? 190 : 0));
        svc.ListInvoices.mockResolvedValue(manyRows(300));
        renderAt("/", "/", <main className="content"><Invoices/></main>);
        const feed = await screen.findByRole("feed", {name: "Rechnungen, Detailansicht"});
        await waitFor(() => expect(within(feed).getAllByRole("article").length).toBeLessThan(30));
        const cards = within(feed).getAllByRole("article");
        expect(cards.length).toBeGreaterThan(3);
        expect(cards[0]).toHaveAttribute("aria-setsize", "300");
        expect(cards[0]).toHaveAttribute("aria-posinset", "1");
        expect(screen.getByText("300 Rechnungen")).toBeInTheDocument();
    });

    it("opens the invoice with a click on the card, but not on its controls", async () => {
        const user = userEvent.setup();
        renderAt("/", "/", <Invoices/>);
        const card = await screen.findByRole("article", {name: /Dr. Muster/});
        await user.click(within(card).getByLabelText("Dr. Muster auswählen"));
        expect(screen.queryByText("andere Seite")).not.toBeInTheDocument();
        await user.click(within(card).getByText("286,40 €"));
        expect(await screen.findByText("andere Seite")).toBeInTheDocument();
    });

    it("closes a popover when the view changes", async () => {
        const user = userEvent.setup();
        renderAt("/", "/", <Invoices/>);
        const card = await screen.findByRole("article", {name: /Dr. Muster/});
        await user.click(within(card).getAllByLabelText("erstattet")[0]);
        expect(screen.getByRole("dialog", {name: "Erstattung Beihilfe"})).toBeInTheDocument();
        await user.click(screen.getByRole("button", {name: "Klassische Liste"}));
        expect(screen.queryByRole("dialog", {name: "Erstattung Beihilfe"})).not.toBeInTheDocument();
    });

    it("has no view switch in the classic design", async () => {
        applyDesign("classic");
        renderAt("/", "/", <Invoices/>);
        await screen.findByText("Dr. Muster");
        expect(screen.queryByRole("group", {name: "Ansicht"})).not.toBeInTheDocument();
        expect(screen.queryByRole("article")).not.toBeInTheDocument();
    });
});

describe("Invoices, table view", () => {
    beforeEach(() => {
        setSessionView("table");
        stubHeights(el => (el.classList.contains("grid-wrap") ? 400 : 0)); // the table's own scroll area
    });

    const bodyRows = () => within(screen.getByRole("table", {name: "Rechnungen, Tabellenansicht"})).getAllByRole("row")
        .filter(r => r.closest("tbody") && r.getAttribute("aria-hidden") !== "true");

    it("shows every field as a column, grouped per payer, with a sum row", async () => {
        renderAt("/", "/", <Invoices/>);
        const table = await screen.findByRole("table", {name: "Rechnungen, Tabellenansicht"});
        for (const name of ["Behandlung", "Rechnung", "Behandler", "Kategorie", "Re.-Nr.", "Betrag", "Bezahlt", "Notiz"]) {
            expect(within(table).getByRole("columnheader", {name})).toBeInTheDocument();
        }
        expect(within(table).getAllByRole("columnheader", {name: "SOLL"})).toHaveLength(2);
        expect(within(table).getByRole("columnheader", {name: "Muster PKV"})).toBeInTheDocument();
        expect(within(table).getByText("Summe")).toBeInTheDocument();
        expect(within(table).getByText("331,50")).toBeInTheDocument(); // 286,40 + 45,10
        expect(table).toHaveAttribute("aria-rowcount", "5"); // 2 header rows, 2 invoices, sum row
    });

    it("sorts by a column, ascending, descending and back to the list order", async () => {
        const user = userEvent.setup();
        renderAt("/", "/", <Invoices/>);
        await screen.findByRole("table", {name: "Rechnungen, Tabellenansicht"});
        const providers = () => bodyRows().map(r => within(r).getAllByRole("cell")[3].textContent);
        expect(providers()).toEqual(["Dr. Muster", "Apotheke am Markt"]);
        await user.click(screen.getByRole("button", {name: "Behandler"}));
        expect(screen.getByRole("columnheader", {name: "Behandler"})).toHaveAttribute("aria-sort", "ascending");
        expect(providers()).toEqual(["Apotheke am Markt", "Dr. Muster"]);
        await user.click(screen.getByRole("button", {name: "Behandler"}));
        expect(screen.getByRole("columnheader", {name: "Behandler"})).toHaveAttribute("aria-sort", "descending");
        await user.click(screen.getByRole("button", {name: "Beihilfe: SOLL"})); // sorting by another column
        expect(screen.getByRole("columnheader", {name: "Behandler"})).toHaveAttribute("aria-sort", "none");
        await user.click(screen.getByRole("button", {name: "Behandler"}));
        await user.click(screen.getByRole("button", {name: "Behandler"}));
        await user.click(screen.getByRole("button", {name: "Behandler"}));
        expect(screen.getByRole("columnheader", {name: "Behandler"})).toHaveAttribute("aria-sort", "none");
        expect(providers()).toEqual(["Dr. Muster", "Apotheke am Markt"]); // the list order again
    });

    it("hides columns with the column chooser and keeps the choice", async () => {
        const user = userEvent.setup();
        renderAt("/", "/", <Invoices/>);
        await screen.findByRole("table", {name: "Rechnungen, Tabellenansicht"});
        await user.click(screen.getByRole("button", {name: "Spalten 18 von 18 ▾"}));
        const menu = screen.getByRole("dialog", {name: "Spalten"});
        expect(within(menu).getByRole("checkbox", {name: "Behandlung"})).toBeDisabled();
        await user.click(within(menu).getByRole("checkbox", {name: "Notiz"}));
        expect(svc.SetTableOptions).toHaveBeenCalledWith({dense: false, sum: true, hidden: ["note"]});
        expect(screen.queryByRole("columnheader", {name: "Notiz"})).not.toBeInTheDocument();
        await user.click(within(menu).getByRole("checkbox", {name: "Summenzeile"}));
        expect(screen.queryByText("Summe")).not.toBeInTheDocument();
    });

    it("opens an invoice with a double click or Enter, moves with the arrow keys", async () => {
        const user = userEvent.setup();
        renderAt("/", "/", <Invoices/>);
        await screen.findByRole("table", {name: "Rechnungen, Tabellenansicht"});
        const [first, second] = bodyRows();
        first.focus();
        await user.keyboard("{ArrowDown}");
        expect(second).toHaveFocus();
        await user.dblClick(within(first).getAllByRole("cell")[5]);
        expect(await screen.findByText("andere Seite")).toBeInTheDocument();
    });

    it("sorts by Bezahlt: paid by date, then unpaid by due date", async () => {
        const user = userEvent.setup();
        svc.ListInvoices.mockResolvedValue([
            invoiceRow("a", "Offen ohne Frist"),
            invoiceRow("b", "Offen fällig", {due: "2026-12-01"}),
            invoiceRow("c", "Bezahlt spät", {paid: {done: true, date: "2026-05-01", note: ""}}),
            invoiceRow("d", "Bezahlt früh", {paid: {done: true, date: "2025-02-01", note: ""}}),
        ]);
        renderAt("/", "/", <Invoices/>);
        await screen.findByRole("table", {name: "Rechnungen, Tabellenansicht"});
        await user.click(screen.getByRole("button", {name: "Bezahlt"}));
        expect(bodyRows().map(r => within(r).getAllByRole("cell")[3].textContent))
            .toEqual(["Bezahlt früh", "Bezahlt spät", "Offen fällig", "Offen ohne Frist"]);
    });

    it("does not open the invoice on a double click on its checkbox", async () => {
        const user = userEvent.setup();
        renderAt("/", "/", <Invoices/>);
        await screen.findByRole("table", {name: "Rechnungen, Tabellenansicht"});
        await user.dblClick(within(bodyRows()[0]).getByRole("checkbox"));
        expect(screen.queryByText("andere Seite")).not.toBeInTheDocument();
    });

    it("keeps a rendered row reachable with Tab after scrolling", async () => {
        svc.ListInvoices.mockResolvedValue(manyRows(300));
        renderAt("/", "/", <Invoices/>);
        const table = await screen.findByRole("table", {name: "Rechnungen, Tabellenansicht"});
        expect(bodyRows()[0]).toHaveAttribute("tabindex", "0");
        const scroller = table.closest(".grid-wrap") as HTMLElement;
        Object.defineProperty(scroller, "scrollTop", {configurable: true, value: 36 * 150});
        fireEvent.scroll(scroller);
        await waitFor(() => expect(Number(bodyRows()[0].getAttribute("aria-rowindex"))).toBeGreaterThan(100));
        const stops = bodyRows().filter(r => r.getAttribute("tabindex") === "0");
        expect(stops).toHaveLength(1);
        expect(Number(stops[0].getAttribute("aria-rowindex"))).toBeGreaterThan(140); // in view, not the overscan above
    });

    it("renders only the visible rows of a long list", async () => {
        svc.ListInvoices.mockResolvedValue(manyRows(300));
        renderAt("/", "/", <Invoices/>);
        const table = await screen.findByRole("table", {name: "Rechnungen, Tabellenansicht"});
        expect(table).toHaveAttribute("aria-rowcount", "303");
        const rendered = bodyRows();
        expect(rendered.length).toBeGreaterThan(5);
        expect(rendered.length).toBeLessThan(40);
        expect(rendered[0]).toHaveAttribute("aria-rowindex", "3");
        expect(screen.queryByText("Praxis 300")).not.toBeInTheDocument();
        expect(within(table).getByText("300 Rechnungen")).toBeInTheDocument(); // the sum row counts all
    });
});

describe("Invoices, compact view", () => {
    beforeEach(() => setSessionView("compact"));

    it("shows one line per invoice with a short status per payer, open amount and flags", async () => {
        svc.ListInvoices.mockResolvedValue([rows[0], {...rows[1], note: "Rezept liegt bei", paid: {done: true, date: "2026-04-01", note: ""}}]);
        renderAt("/", "/", <Invoices/>);
        const list = await screen.findByRole("list");
        const items = within(list).getAllByRole("listitem");
        expect(items).toHaveLength(2);
        expect(items[1]).toHaveAttribute("aria-setsize", "2");
        expect(items[1]).toHaveAttribute("aria-posinset", "2");
        const first = within(list).getByRole("group", {name: "Dr. Muster, 03.03.2026, 286,40 €"});
        expect(within(first).getAllByText("nicht eingereicht")).toHaveLength(2);
        expect(within(first).getAllByText("286,40 €")).toHaveLength(2); // amount, and open as nothing is refunded
        expect(within(first).getByText("nicht bezahlt")).toBeInTheDocument(); // flag text for screen readers
        const second = within(list).getByRole("group", {name: /Apotheke am Markt/});
        expect(within(second).getByText("eingereicht 02.04.")).toBeInTheDocument();
        expect(within(second).getByText("zurückgehalten")).toBeInTheDocument();
        expect(within(second).getByText("Notiz: Rezept liegt bei")).toBeInTheDocument();
        expect(screen.getByText("2 Rechnungen")).toBeInTheDocument();
    });

    it("expands a line with the chevron or → and ticks inside", async () => {
        const user = userEvent.setup();
        svc.SetMark.mockResolvedValue(rows[0]);
        renderAt("/", "/", <Invoices/>);
        const line = await screen.findByRole("group", {name: /Dr. Muster/});
        await user.click(within(line).getByRole("button", {name: "Details anzeigen"}));
        const item = line.closest("[role=listitem]") as HTMLElement;
        expect(within(item).getByRole("link", {name: "Rechnung öffnen →"})).toBeInTheDocument();
        await user.click(within(item).getAllByLabelText("eingereicht")[0]);
        expect(svc.SetMark).toHaveBeenCalledWith({invoiceId: "a", payerId: "bh", mark: "submitted", on: true, date: "", note: "", amount: ""});
        line.focus();
        await user.keyboard("{ArrowLeft}");
        expect(within(item).queryByRole("link", {name: "Rechnung öffnen →"})).not.toBeInTheDocument();
        await user.keyboard("{ArrowRight}");
        expect(within(item).getByRole("link", {name: "Rechnung öffnen →"})).toBeInTheDocument();
        await user.keyboard("{Enter}");
        expect(await screen.findByText("andere Seite")).toBeInTheDocument();
    });

    it("opens the invoice with a double click on the line", async () => {
        const user = userEvent.setup();
        renderAt("/", "/", <Invoices/>);
        const line = await screen.findByRole("group", {name: /Dr. Muster/});
        await user.dblClick(within(line).getAllByText("286,40 €")[0]); // the amount
        expect(await screen.findByText("andere Seite")).toBeInTheDocument();
    });

    it("renders only the visible lines when the page scrolls", async () => {
        // the page is 500 px high, every line 46 px (the virtualiser measures them)
        stubHeights(el => (el.classList.contains("content") ? 500 : el.getAttribute("role") === "listitem" ? 46 : 0));
        svc.ListInvoices.mockResolvedValue(manyRows(300));
        renderAt("/", "/", <main className="content"><Invoices/></main>);
        const list = await screen.findByRole("list");
        await waitFor(() => expect(within(list).getAllByRole("listitem").length).toBeLessThan(40));
        const items = within(list).getAllByRole("listitem");
        expect(items.length).toBeGreaterThan(5);
        expect(items[0]).toHaveAttribute("aria-setsize", "300");
        expect(items[0]).toHaveAttribute("aria-posinset", "1");
        expect(screen.getByText("300 Rechnungen")).toBeInTheDocument();
    });

    it("opens with the filter and search another page hands over (\"In der Rechnungsliste zeigen\")", async () => {
        renderAt({pathname: "/", state: {filter: "submitted", search: "Apotheke"}}, "/", <Invoices/>);
        expect(await screen.findByText("Apotheke am Markt")).toBeInTheDocument();
        expect(screen.queryByText("Dr. Muster")).not.toBeInTheDocument();
        expect(screen.getByRole("button", {name: "Erstattung offen"})).toHaveAttribute("aria-pressed", "true");
        expect(screen.getByLabelText("Suchen")).toHaveValue("Apotheke");
    });
});
