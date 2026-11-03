import {screen, waitFor, within} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {afterEach, beforeEach, describe, expect, it, vi} from "vitest";
import type {AnswerDTO, QueryDTO} from "../api";
import {setAI} from "../lib/ai";
import {cancellable, invoiceRow} from "../test/fixtures";
import {renderAt} from "../test/render";
import Ask, {clearQuestions, listState} from "./Ask";

const svc = vi.hoisted(() => ({Ask: vi.fn(), RunQuery: vi.fn(), AskVocabulary: vi.fn()}));
vi.mock("../api", () => ({Svc: svc}));

const query: QueryDTO = {typ: "anzahl", messgroesse: "", jahre: [2026], filter: [{feld: "status", wert: "nicht_eingereicht"},
    {feld: "kostentraeger", wert: "Beihilfe Bund"}], gruppierung: "", grund: ""};

function answer(patch: Partial<AnswerDTO> = {}): AnswerDTO {
    return {query, answer: "2 Rechnungen", seconds: 7.6,
        chips: [{key: "typ", text: "Anzahl"}, {key: "jahre", text: "2026"}, {key: "status", text: "nicht eingereicht"},
            {key: "kostentraeger", text: "Beihilfe Bund"}],
        figures: [{label: "Rechnungen", value: "2"}], groups: [],
        invoices: [invoiceRow("a", "Dr. Muster", {serviceDate: "2026-03-03", amount: "286.40"}), invoiceRow("b", "Radiologie Mitte", {amount: "150.00"})], ...patch};
}

beforeEach(() => {
    vi.clearAllMocks();
    setAI({on: true, url: "http://192.168.178.38:8080/v1", model: "qwen3-vl-8b", keySet: true, keyEnd: ""});
    svc.AskVocabulary.mockResolvedValue({years: [2026, 2025], payers: [{value: "Beihilfe Bund", label: "Beihilfe Bund"},
        {value: "Muster PKV", label: "Muster PKV"}], statuses: [{value: "nicht_eingereicht", label: "nicht eingereicht"},
        {value: "eingereicht", label: "Erstattung offen"}], categories: [], groups: []});
});
afterEach(clearQuestions);

describe("Fragen", () => {
    it("asks, shows how the question was understood and the invoices", async () => {
        const user = userEvent.setup();
        svc.Ask.mockReturnValue(cancellable(Promise.resolve(answer())));
        renderAt("/", "/", <Ask/>);
        await user.type(screen.getByLabelText("Frage"), "Wie viele Rechnungen aus 2026 hat die Beihilfe noch nicht?{Enter}");
        expect(svc.Ask).toHaveBeenCalledWith({question: "Wie viele Rechnungen aus 2026 hat die Beihilfe noch nicht?", history: []});
        const entry = await screen.findByRole("article", {name: "Wie viele Rechnungen aus 2026 hat die Beihilfe noch nicht?"});
        expect(within(entry).getByText("2 Rechnungen")).toBeInTheDocument();
        const chips = within(entry).getByLabelText("So hat die App die Frage verstanden");
        expect(within(chips).getByText("Anzahl")).toBeInTheDocument();
        expect(within(chips).getByRole("combobox", {name: "Jahr ändern"})).toHaveValue("2026");
        expect(within(entry).getByRole("link", {name: "Dr. Muster"})).toHaveAttribute("href", "/rechnung/a");
        expect(within(entry).getByText("286,40 €")).toBeInTheDocument();

        // A follow-up takes the question before and its query along.
        svc.Ask.mockReturnValue(cancellable(Promise.resolve(answer({answer: "1 Rechnung"}))));
        await user.type(screen.getByLabelText("Frage"), "Und 2025?{Enter}");
        expect(svc.Ask).toHaveBeenLastCalledWith({question: "Und 2025?",
            history: [{frage: "Wie viele Rechnungen aus 2026 hat die Beihilfe noch nicht?", abfrage: query}]});
        const followUp = await screen.findByRole("article", {name: "Und 2025?"});
        expect(await within(followUp).findByText("1 Rechnung")).toBeInTheDocument();
        // Screen readers hear the answer sentence (and not the chips and tables).
        expect(document.querySelector("[aria-live=polite]")).toHaveTextContent("1 Rechnung");
    });

    it("changes and removes chips without the AI", async () => {
        const user = userEvent.setup();
        svc.Ask.mockReturnValue(cancellable(Promise.resolve(answer())));
        svc.RunQuery.mockImplementation(async (q: QueryDTO) => answer({query: q, answer: "5 Rechnungen"}));
        renderAt("/", "/", <Ask/>);
        await user.type(screen.getByLabelText("Frage"), "Wie viele?{Enter}");
        await user.click(await screen.findByRole("button", {name: "„nicht eingereicht“ entfernen"}));
        expect(svc.RunQuery).toHaveBeenCalledWith({...query, filter: [{feld: "kostentraeger", wert: "Beihilfe Bund"}]});
        const entry = screen.getByRole("article", {name: "Wie viele?"});
        expect(await within(entry).findByText("5 Rechnungen")).toBeInTheDocument();
        // The chip with the focused button went away: the focus stays in the chips.
        expect(within(entry).getByRole("group", {name: "So hat die App die Frage verstanden"})).toHaveFocus();

        // Changing a chip changes the query as it stands now (without the removed status).
        await user.selectOptions(screen.getByRole("combobox", {name: "Kostenträger ändern"}), "Muster PKV");
        expect(svc.RunQuery).toHaveBeenLastCalledWith(expect.objectContaining({filter: [{feld: "kostentraeger", wert: "Muster PKV"}]}));
        await user.click(screen.getByRole("button", {name: "„2026“ entfernen"}));
        expect(svc.RunQuery).toHaveBeenLastCalledWith(expect.objectContaining({jahre: []}));
        expect(svc.Ask).toHaveBeenCalledTimes(1);
    });

    it("opens the invoice list with what it can filter", async () => {
        const user = userEvent.setup();
        const setYear = vi.fn();
        svc.Ask.mockReturnValue(cancellable(Promise.resolve(answer())));
        renderAt("/", "/", <Ask/>, {setYear});
        await user.type(screen.getByLabelText("Frage"), "Welche?{Enter}");
        const open = await screen.findByRole("link", {name: "In der Rechnungsliste zeigen"});
        expect(screen.getByText("(ohne Filter nach Kostenträger)")).toBeInTheDocument();
        await user.click(open);
        expect(setYear).toHaveBeenCalledWith(2026);
        expect(screen.getByText("andere Seite")).toBeInTheDocument();
    });

    it("shows the Ausstand without a way into the list", async () => {
        const user = userEvent.setup();
        svc.Ask.mockReturnValue(cancellable(Promise.resolve(answer({query: {...query, typ: "ausstand", filter: []},
            chips: [{key: "typ", text: "Ausstand"}], answer: "Noch ausstehend: 1.165,40 €",
            figures: [{label: "Beihilfe Bund: eingereicht, Erstattung offen", value: "700,00 €"}]}))));
        renderAt("/", "/", <Ask/>);
        await user.type(screen.getByLabelText("Frage"), "Was steht noch aus?{Enter}");
        const entry = await screen.findByRole("article", {name: "Was steht noch aus?"});
        expect(await within(entry).findByText("Noch ausstehend: 1.165,40 €")).toBeInTheDocument();
        expect(within(entry).getByText("Beihilfe Bund: eingereicht, Erstattung offen")).toBeInTheDocument();
        expect(within(entry).queryByRole("link", {name: "In der Rechnungsliste zeigen"})).not.toBeInTheDocument();
    });

    it("cancels a question and says when the server is away", async () => {
        const user = userEvent.setup();
        // As the Wails promise: cancelling rejects it.
        let reject: (e: Error) => void = () => {};
        const running = Object.assign(new Promise<AnswerDTO>((_, r) => (reject = r)), {cancel: vi.fn(() => reject(new Error("cancelled")))});
        svc.Ask.mockReturnValue(running);
        renderAt("/", "/", <Ask/>);
        await user.type(screen.getByLabelText("Frage"), "Wie viele?{Enter}");
        expect(screen.getByRole("button", {name: "Fragen"})).toBeDisabled();
        await user.click(screen.getByRole("button", {name: "Abbrechen"}));
        expect(running.cancel).toHaveBeenCalled();
        expect(await screen.findByText("Abgebrochen.")).toBeInTheDocument();

        svc.Ask.mockImplementation(() => cancellable(Promise.reject(new Error("KI-Server nicht erreichbar: 192.168.178.38:8080 antwortet nicht"))));
        await user.type(screen.getByLabelText("Frage"), "Und jetzt?{Enter}");
        expect(await screen.findByText(/nicht erreichbar.*Ohne KI helfen die Filter der Rechnungsliste\./)).toBeInTheDocument();
    });

    it("stops a running question when the history is cleared", async () => {
        const user = userEvent.setup();
        let reject: (e: Error) => void = () => {};
        const running = Object.assign(new Promise<AnswerDTO>((_, r) => (reject = r)), {cancel: vi.fn(() => reject(new Error("cancelled")))});
        svc.Ask.mockReturnValue(running);
        renderAt("/", "/", <Ask/>);
        await user.type(screen.getByLabelText("Frage"), "Wie viele?{Enter}");
        await user.click(screen.getByRole("button", {name: "Verlauf leeren"}));
        expect(running.cancel).toHaveBeenCalled();
        expect(screen.queryByRole("article")).not.toBeInTheDocument();
        await user.type(screen.getByLabelText("Frage"), "Neu");
        expect(screen.getByRole("button", {name: "Fragen"})).toBeEnabled();
    });

    it("offers the year asked about even without invoices, and takes only the latest chip change", async () => {
        const user = userEvent.setup();
        svc.Ask.mockReturnValue(cancellable(Promise.resolve(answer({query: {...query, jahre: [2019]},
            chips: [{key: "typ", text: "Anzahl"}, {key: "jahre", text: "2019"}]}))));
        let first: (a: AnswerDTO) => void = () => {};
        svc.RunQuery.mockImplementationOnce(() => new Promise(r => (first = r)))
            .mockImplementationOnce(async (q: QueryDTO) => answer({query: q, answer: "später"}));
        renderAt("/", "/", <Ask/>);
        await user.type(screen.getByLabelText("Frage"), "Wie viele 2019?{Enter}");
        const year = await screen.findByRole("combobox", {name: "Jahr ändern"});
        expect(year).toHaveValue("2019");
        await user.selectOptions(year, "2026");
        await user.selectOptions(year, "2025");
        const entry = screen.getByRole("article", {name: "Wie viele 2019?"});
        expect(await within(entry).findByText("später")).toBeInTheDocument();
        first(answer({answer: "früher"})); // the answer to the first change comes last
        await new Promise(r => setTimeout(r, 0));
        expect(within(entry).queryByText("früher")).not.toBeInTheDocument();
    });

    it("points to the settings while the AI is off", () => {
        setAI({on: false, url: "", model: "", keySet: false, keyEnd: ""});
        renderAt("/", "/", <Ask/>);
        expect(screen.getByText(/Die KI ist ausgeschaltet/)).toBeInTheDocument();
        expect(screen.getByLabelText("Frage")).toBeDisabled();
    });

    it("offers example questions", async () => {
        const user = userEvent.setup();
        svc.Ask.mockReturnValue(cancellable(Promise.resolve(answer())));
        renderAt("/", "/", <Ask/>);
        await user.click(screen.getByRole("button", {name: "Was steht insgesamt noch aus?"}));
        await waitFor(() => expect(svc.Ask).toHaveBeenCalledWith({question: "Was steht insgesamt noch aus?", history: []}));
    });
});

describe("listState", () => {
    it("maps a query to the list's year, filter and search", () => {
        expect(listState({...query, filter: [{feld: "status", wert: "eingereicht"}, {feld: "behandler", wert: "Dr. Muster"}]}))
            .toEqual({year: 2026, state: {filter: "submitted", search: "Muster"}, missing: []}); // the list searches plain text
        expect(listState({...query, jahre: [2025, 2026], filter: [{feld: "kategorie", wert: "zahnarzt"}, {feld: "bezahlt", wert: "nein"}]}))
            .toEqual({year: 0, state: {filter: "unpaid", search: undefined}, missing: ["Kategorie", "mehreren Jahren"]});
    });
});
