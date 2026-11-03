import {screen, waitFor, within} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {beforeEach, describe, expect, it, vi} from "vitest";
import type {AnalysisDTO, InvoiceDTO, StagedDocDTO} from "../api";
import {setAI} from "../lib/ai";
import {aiReading, cancellable, claimView, invoiceRow, noMark} from "../test/fixtures";
import {renderAt} from "../test/render";
import InvoiceForm from "./InvoiceForm";

const svc = vi.hoisted(() => ({
    NewInvoice: vi.fn(), GetInvoice: vi.fn(), PreviewInvoice: vi.fn(), SaveInvoice: vi.fn(), DeleteInvoice: vi.fn(),
    StageFiles: vi.fn(), StageFile: vi.fn(), DiscardStaged: vi.fn(), StagedToInbox: vi.fn(), AnalyzeDocument: vi.fn(),
    DocumentPath: vi.fn(), DocumentsPDF: vi.fn(), ReadWithAI: vi.fn(),
}));
const desktop = vi.hoisted(() => ({ChooseFiles: vi.fn(), OpenPath: vi.fn()}));
const capture = vi.hoisted(() => ({recognizePages: vi.fn(), renderPdfPreview: vi.fn(), pageImages: vi.fn(), AI_MAX_PAGES: 4}));
vi.mock("../api", () => ({Svc: svc, Desktop: desktop}));
vi.mock("../lib/capture", () => capture);

function claim(payerId: string) {
    return {payerId, eligible: "", rate: "", expected: "", submitted: noMark, refunded: noMark, clarify: noMark,
        held: false, skip: false, keepsPremiumRefund: false, submissionId: "", refunds: []};
}

const blank: InvoiceDTO = {
    id: "", provider: "", payee: "", iban: "", reference: "", category: "", invoiceDate: "2026-11-02", serviceFrom: "",
    serviceTo: "", serviceYear: 0, amount: "0.00", due: "", paid: noMark, claims: [claim("bh"), claim("pkv")],
    attachments: [], note: "",
};

const page: StagedDocDTO = {id: "0123456789abcdef.png", name: "rechnung.png", type: "image", url: "/datafile/staged/0123456789abcdef.png",
    path: "/tmp/sitzung/staged/0123456789abcdef.png", size: 1000};

function analysis(kind: string, found: Partial<InvoiceDTO>): AnalysisDTO {
    return {kind, draft: {invoice: {...blank, invoiceDate: "", ...found}, view: invoiceRow("", ""), movedToInbox: 0, warning: ""},
        sources: {amount: "qr", reference: "label"}, notes: []};
}

beforeEach(() => {
    vi.clearAllMocks();
    svc.NewInvoice.mockResolvedValue({invoice: blank, view: invoiceRow("", "", {amount: "0.00", expected: "0.00"})});
    svc.PreviewInvoice.mockResolvedValue({invoice: blank, view: invoiceRow("", "Dr. Muster")});
    svc.DiscardStaged.mockResolvedValue(undefined);
    svc.StageFiles.mockResolvedValue([page]);
    desktop.ChooseFiles.mockResolvedValue(["/home/x/rechnung.png"]);
    capture.recognizePages.mockResolvedValue({lines: [], qr: [], ocr: false, pages: 1, truncated: false});
    capture.renderPdfPreview.mockResolvedValue(undefined);
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

    it("does not ask before leaving after ticking and unticking a box", async () => {
        const user = userEvent.setup();
        renderAt("/rechnung/neu", "/rechnung/neu", <InvoiceForm/>);
        const bh = await screen.findByRole("region", {name: "Beihilfe"});
        await user.click(within(bh).getByLabelText("eingereicht"));
        await user.click(within(bh).getByLabelText("eingereicht"));
        await user.click(screen.getByRole("button", {name: "Abbrechen"}));
        expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
    });

    it("shows backend validation errors", async () => {
        const user = userEvent.setup();
        svc.SaveInvoice.mockRejectedValue(new Error("Behandler fehlt\nBetrag fehlt"));
        renderAt("/rechnung/neu", "/rechnung/neu", <InvoiceForm/>);
        await user.click(await screen.findByRole("button", {name: "Speichern"}));
        expect(await screen.findByText(/Behandler fehlt/)).toBeInTheDocument();
    });

    it("fills the empty fields from an added document and keeps typed ones", async () => {
        const user = userEvent.setup();
        svc.AnalyzeDocument.mockResolvedValue(analysis("invoice", {provider: "Dr. Muster", amount: "286.40", reference: "R-1"}));
        svc.SaveInvoice.mockResolvedValue({invoice: {...blank, id: "x"}, view: invoiceRow("x", "Praxis", {year: 2026}), movedToInbox: 0, warning: ""});
        renderAt("/rechnung/neu", "/rechnung/neu", <InvoiceForm/>);
        await user.type(await screen.findByLabelText("Behandler *"), "Praxis Beispiel");
        await user.click(screen.getByRole("button", {name: "+ Beleg hinzufügen …"}));

        expect(await screen.findByText("2 Felder aus dem Beleg übernommen.")).toBeInTheDocument();
        expect(capture.recognizePages).toHaveBeenCalledWith([expect.objectContaining({url: page.url, type: "image"})],
            expect.any(Function), expect.any(AbortSignal));
        expect(screen.getByLabelText("Behandler *")).toHaveValue("Praxis Beispiel");
        expect(screen.getByLabelText("Betrag (€) *")).toHaveValue("286,40");
        expect(screen.getByLabelText("Rechnungsnummer")).toHaveValue("R-1");
        expect(screen.getByText("aus GiroCode")).toBeInTheDocument();
        expect(screen.getByRole("img", {name: "Seite 1"})).toBeInTheDocument();

        // Editing a prefilled field removes its badge.
        await user.type(screen.getByLabelText("Betrag (€) *"), "0");
        expect(screen.queryByText("aus GiroCode")).not.toBeInTheDocument();

        await user.click(screen.getByRole("button", {name: "Speichern"}));
        await waitFor(() => expect(svc.SaveInvoice).toHaveBeenCalled());
        expect(svc.SaveInvoice.mock.calls[0][2]).toEqual([page.id]);
    });

    it("reads added documents with the AI and upgrades the badges", async () => {
        const user = userEvent.setup();
        setAI({on: true, url: "http://192.168.178.38:8080/v1", model: "qwen3-vl-8b", keySet: true, keyEnd: ""});
        svc.AnalyzeDocument.mockResolvedValue(analysis("invoice", {amount: "286.40", reference: "R-1"}));
        capture.recognizePages.mockResolvedValue({lines: [{text: "R-1", page: 1, x: 0, y: 0, w: 0, h: 0}], qr: [], ocr: true, pages: 1, truncated: false});
        capture.pageImages.mockResolvedValue({images: ["data:image/jpeg;base64,AA"], total: 1});
        const read = {...blank, provider: "Dr. Muster", amount: "286.40", reference: "R-1", invoiceDate: "2026-10-20"};
        svc.ReadWithAI.mockReturnValue(cancellable(Promise.resolve(aiReading(read, {
            sources: {provider: "ai", amount: "qr", reference: "ai-confirmed", invoiceDate: "ai"}, confirmed: 1}))));
        renderAt("/rechnung/neu", "/rechnung/neu", <InvoiceForm/>);
        expect(await screen.findByLabelText("Behandler *")).toBeInTheDocument();
        expect(screen.queryByRole("button", {name: "Mit KI auslesen"})).not.toBeInTheDocument(); // nothing to read yet
        await user.click(screen.getByRole("button", {name: "+ Beleg hinzufügen …"}));
        await screen.findByText(/2 Felder aus dem Beleg übernommen/);

        await user.click(screen.getByRole("button", {name: "Mit KI auslesen"}));
        expect(await screen.findByText("KI: 2 Felder gefüllt, 1 bestätigt (64 s).")).toBeInTheDocument();
        expect(svc.ReadWithAI).toHaveBeenCalledWith({pages: ["data:image/jpeg;base64,AA"], totalPages: 1, lines: [{text: "R-1", page: 1, x: 0, y: 0, w: 0, h: 0}], qr: []});
        expect(screen.getByLabelText("Behandler *")).toHaveValue("Dr. Muster");
        expect(screen.getByLabelText("Rechnungsdatum *")).toHaveValue("2026-10-20"); // the new form's "today" counts as empty
        expect(screen.getByText("KI, bestätigt")).toBeInTheDocument(); // the number, found before and read the same
        expect(screen.getByText("aus GiroCode")).toBeInTheDocument();
        expect(screen.getAllByText("KI – bitte prüfen")).toHaveLength(2);
    });

    it("moves a recognised Bescheid into the inbox", async () => {
        const user = userEvent.setup();
        svc.AnalyzeDocument.mockResolvedValue(analysis("refund", {}));
        svc.StagedToInbox.mockResolvedValue(["rechnung.png"]);
        renderAt("/rechnung/neu", "/rechnung/neu", <InvoiceForm/>);
        await user.click(await screen.findByRole("button", {name: "+ Beleg hinzufügen …"}));
        await user.click(await screen.findByRole("button", {name: "In den Posteingang verschieben"}));
        await waitFor(() => expect(svc.StagedToInbox).toHaveBeenCalledWith([page.id]));
        expect(await screen.findByText("andere Seite")).toBeInTheDocument();
    });

    it("only attaches documents to an existing invoice; removed pages go back to the inbox", async () => {
        const user = userEvent.setup();
        const inv = {...blank, id: "a", provider: "Dr. Muster", amount: "286.40", attachments: ["aaa.pdf", "bbb.png"]};
        svc.GetInvoice.mockResolvedValue({invoice: inv, view: invoiceRow("a", "Dr. Muster")});
        svc.SaveInvoice.mockResolvedValue({invoice: inv, view: invoiceRow("a", "Dr. Muster", {year: 2026}), movedToInbox: 1});
        svc.DocumentsPDF.mockResolvedValue("/tmp/Belege-1.pdf");
        renderAt("/rechnung/a", "/rechnung/:id", <InvoiceForm/>);

        await user.click(await screen.findByRole("button", {name: "Alle Seiten zusammen öffnen"}));
        await waitFor(() => expect(desktop.OpenPath).toHaveBeenCalledWith("/tmp/Belege-1.pdf"));
        expect(svc.DocumentsPDF).toHaveBeenCalledWith(["aaa.pdf", "bbb.png"]);

        await user.click(screen.getByRole("button", {name: "Seite 1 entfernen"}));
        expect(screen.getByText(/Die entfernte Seite kommt beim Speichern zurück in den Posteingang/)).toBeInTheDocument();
        await user.click(screen.getByRole("button", {name: "+ Beleg hinzufügen …"}));
        expect(await screen.findByRole("button", {name: "Seite 2 öffnen"})).toBeInTheDocument(); // bbb.png, then the new page
        expect(screen.getByText("neu")).toBeInTheDocument();
        expect(capture.recognizePages).not.toHaveBeenCalled();

        await user.click(screen.getByRole("button", {name: "Speichern"}));
        await waitFor(() => expect(svc.SaveInvoice).toHaveBeenCalled());
        const [sent, original, staged] = svc.SaveInvoice.mock.calls[0];
        expect(sent.attachments).toEqual(["bbb.png"]);
        expect(original.attachments).toEqual(["aaa.pdf", "bbb.png"]);
        expect(staged).toEqual([page.id]);
        expect(await screen.findByText(/der entfernte Beleg liegt wieder im Posteingang/)).toBeInTheDocument();
    });

    it("asks before leaving with an added document and discards it", async () => {
        const user = userEvent.setup();
        const inv = {...blank, id: "a", provider: "Dr. Muster", amount: "286.40"};
        svc.GetInvoice.mockResolvedValue({invoice: inv, view: invoiceRow("a", "Dr. Muster")});
        const {unmount} = renderAt("/rechnung/a", "/rechnung/:id", <InvoiceForm/>);
        await user.click(await screen.findByRole("button", {name: "+ Beleg hinzufügen …"}));
        await screen.findByText("neu");
        await user.click(screen.getByRole("button", {name: "Abbrechen"}));
        expect(await screen.findByRole("alertdialog")).toHaveTextContent("noch nicht gespeichert");
        unmount();
        expect(svc.DiscardStaged).toHaveBeenCalledWith([page.id]);
    });

    it("does not let a pending recognition fill the next invoice after \"Speichern & nächste\"", async () => {
        const user = userEvent.setup();
        let finish!: (v: unknown) => void;
        capture.recognizePages.mockImplementation(() => new Promise(res => (finish = res)));
        svc.AnalyzeDocument.mockResolvedValue(analysis("invoice", {provider: "Dr. Alt", amount: "99.00"}));
        svc.SaveInvoice.mockResolvedValue({invoice: {...blank, id: "x"}, view: invoiceRow("x", "Praxis", {year: 2026}), movedToInbox: 0, warning: ""});
        renderAt("/rechnung/neu", "/rechnung/neu", <InvoiceForm/>);
        await user.type(await screen.findByLabelText("Behandler *"), "Praxis");
        await user.click(screen.getByRole("button", {name: "+ Beleg hinzufügen …"}));
        await screen.findByText("neu");
        await user.click(screen.getByRole("button", {name: /Speichern & nächste/}));
        await waitFor(() => expect(svc.NewInvoice).toHaveBeenCalledTimes(2));
        finish({lines: [], qr: [], ocr: false, pages: 1, truncated: false}); // the old run ends now
        await new Promise(r => setTimeout(r, 50));
        expect(screen.getByLabelText("Behandler *")).toHaveValue("");
        expect(screen.queryByText("neu")).not.toBeInTheDocument();
    });

    it("reads the remaining pages again when a page is removed", async () => {
        const user = userEvent.setup();
        const second = {...page, id: "fedcba9876543210.png", name: "seite2.png", url: "/datafile/staged/fedcba9876543210.png"};
        svc.StageFiles.mockResolvedValue([page, second]);
        svc.AnalyzeDocument.mockResolvedValue(analysis("invoice", {provider: "Dr. Muster"}));
        renderAt("/rechnung/neu", "/rechnung/neu", <InvoiceForm/>);
        await user.click(await screen.findByRole("button", {name: "+ Beleg hinzufügen …"}));
        await waitFor(() => expect(capture.recognizePages).toHaveBeenCalledTimes(1));
        await user.click(screen.getByRole("button", {name: "Seite 1 entfernen"}));
        await waitFor(() => expect(capture.recognizePages).toHaveBeenCalledTimes(2));
        expect(capture.recognizePages.mock.calls[1][0].map((p: {url: string}) => p.url)).toEqual([second.url]);
        expect(svc.DiscardStaged).toHaveBeenCalledWith([page.id]);
    });

    it("lets the text recognition decide again when a page goes after an AI reading", async () => {
        const user = userEvent.setup();
        setAI({on: true, url: "http://192.168.178.38:8080/v1", model: "qwen3-vl-8b", keySet: true, keyEnd: ""});
        const second = {...page, id: "fedcba9876543210.png", name: "seite2.png", url: "/datafile/staged/fedcba9876543210.png"};
        svc.StageFiles.mockResolvedValue([page, second]);
        svc.AnalyzeDocument.mockResolvedValue(analysis("invoice", {provider: "Dr. A", amount: "10.00"}));
        capture.pageImages.mockResolvedValue({images: ["data:image/jpeg;base64,AA", "data:image/jpeg;base64,BB"], total: 2});
        svc.ReadWithAI.mockReturnValue(cancellable(Promise.resolve(aiReading({...blank, provider: "Dr. Falsch", amount: "77.00"},
            {sources: {provider: "ai", amount: "ai"}}))));
        renderAt("/rechnung/neu", "/rechnung/neu", <InvoiceForm/>);
        await user.click(await screen.findByRole("button", {name: "+ Beleg hinzufügen …"}));
        await screen.findByDisplayValue("Dr. A");
        await user.click(screen.getByRole("button", {name: "Mit KI auslesen"}));
        expect(await screen.findByDisplayValue("Dr. Falsch")).toBeInTheDocument();

        // Page 2 goes: the values of the AI were about both pages – the remaining page decides.
        await user.click(screen.getByRole("button", {name: "Seite 2 entfernen"}));
        await waitFor(() => expect(screen.getByLabelText("Behandler *")).toHaveValue("Dr. A"));
        expect(screen.getByLabelText("Betrag (€) *")).toHaveValue("10,00");
        expect(screen.queryByText(/^KI: /)).not.toBeInTheDocument();
    });

    it("cancels an AI reading with \"Speichern & nächste\"", async () => {
        const user = userEvent.setup();
        setAI({on: true, url: "http://192.168.178.38:8080/v1", model: "qwen3-vl-8b", keySet: true, keyEnd: ""});
        svc.AnalyzeDocument.mockResolvedValue(analysis("invoice", {provider: "Dr. A", amount: "10.00"}));
        capture.pageImages.mockResolvedValue({images: ["data:image/jpeg;base64,AA"], total: 1});
        const running = cancellable(new Promise(() => {}));
        svc.ReadWithAI.mockReturnValue(running);
        svc.SaveInvoice.mockResolvedValue({invoice: {...blank, id: "x"}, view: invoiceRow("x", "Dr. A", {year: 2026}), movedToInbox: 0, warning: ""});
        renderAt("/rechnung/neu", "/rechnung/neu", <InvoiceForm/>);
        await user.click(await screen.findByRole("button", {name: "+ Beleg hinzufügen …"}));
        await screen.findByDisplayValue("Dr. A");
        await user.click(screen.getByRole("button", {name: "Mit KI auslesen"}));
        await waitFor(() => expect(svc.ReadWithAI).toHaveBeenCalled());
        await user.click(screen.getByRole("button", {name: /Speichern & nächste/}));
        await waitFor(() => expect(running.cancel).toHaveBeenCalled());
    });
});
