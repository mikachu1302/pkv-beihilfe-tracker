import {fireEvent, screen, waitFor} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {beforeEach, describe, expect, it, vi} from "vitest";
import type {AnalysisDTO, InvoiceDTO} from "../api";
import {setAI} from "../lib/ai";
import {aiReading, cancellable, invoiceRow, noMark, settings} from "../test/fixtures";
import {renderAt} from "../test/render";
import Inbox from "./Inbox";

const svc = vi.hoisted(() => ({
    ListInbox: vi.fn(), AnalyzeDocument: vi.fn(), CreateInvoiceFromInbox: vi.fn(), DiscardInbox: vi.fn(),
    AddFilesToInbox: vi.fn(), AddToInbox: vi.fn(), StartPhoneUpload: vi.fn(), StopPhoneUpload: vi.fn(),
    GetSettings: vi.fn(), AnalyzeRefund: vi.fn(), ReviewRefund: vi.fn(), ConfirmRefund: vi.fn(),
    MergeInbox: vi.fn(), ReorderInbox: vi.fn(), SplitInbox: vi.fn(), SuggestInboxGroups: vi.fn(), ReadWithAI: vi.fn(),
}));
const desktop = vi.hoisted(() => ({ChooseFiles: vi.fn()}));
const capture = vi.hoisted(() => ({recognizePages: vi.fn(), renderPdfPreview: vi.fn(), pageImages: vi.fn(), AI_MAX_PAGES: 4}));
vi.mock("../api", () => ({Svc: svc, Desktop: desktop}));
vi.mock("../lib/capture", () => capture);

const draft: InvoiceDTO = {
    id: "", provider: "Dr. med. Anna Muster", payee: "PVS Beispiel eG", iban: "DE89370400440532013000", reference: "2026-004711",
    category: "arzt", invoiceDate: "2026-03-14", serviceFrom: "2026-03-03", serviceTo: "2026-03-05", serviceYear: 0,
    amount: "286.40", due: "", paid: noMark, claims: [], attachments: [], note: "",
};

function analysis(kind = "invoice"): AnalysisDTO {
    return {kind, draft: {invoice: draft, view: invoiceRow("", draft.provider), movedToInbox: 0, warning: ""}, sources: {amount: "qr", provider: "label", category: "guess"}, notes: []};
}

beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    svc.SuggestInboxGroups.mockResolvedValue([]);
    svc.ListInbox.mockResolvedValue([{name: "scan.pdf", size: 1000, modified: "2026-11-02 10:15", type: "pdf", url: "/datafile/inbox/scan.pdf"}]);
    capture.recognizePages.mockResolvedValue({lines: [{text: "Rechnung", page: 1, x: 0, y: 0, w: 0, h: 0}], qr: ["BCD"], ocr: false, pages: 1, truncated: false});
    capture.renderPdfPreview.mockResolvedValue(undefined);
    svc.AnalyzeDocument.mockResolvedValue(analysis());
    svc.StopPhoneUpload.mockResolvedValue(undefined);
    svc.GetSettings.mockResolvedValue(settings);
    capture.pageImages.mockResolvedValue(["data:image/jpeg;base64,AA"]);
});

const aiOn = () => setAI({on: true, url: "http://192.168.178.38:8080/v1", model: "qwen3-vl-8b", keySet: true, keyEnd: ""});

describe("Inbox", () => {
    it("recognises a document and saves it as an invoice", async () => {
        const user = userEvent.setup();
        svc.CreateInvoiceFromInbox.mockResolvedValue({invoice: {...draft, id: "x"}, view: invoiceRow("x", draft.provider, {year: 2026})});
        renderAt("/", "/", <Inbox/>);
        expect(await screen.findByDisplayValue("Dr. med. Anna Muster")).toBeInTheDocument();
        expect(screen.getByLabelText("Betrag (€) *")).toHaveValue("286,40");
        expect(screen.getByText("aus GiroCode")).toBeInTheDocument();
        expect(screen.getByText("bitte prüfen")).toBeInTheDocument();
        expect(capture.recognizePages).toHaveBeenCalledWith([expect.objectContaining({url: "/datafile/inbox/scan.pdf", type: "pdf"})],
            expect.any(Function), expect.any(AbortSignal));
        expect(svc.AnalyzeDocument).toHaveBeenCalledWith({lines: [{text: "Rechnung", page: 1, x: 0, y: 0, w: 0, h: 0}], qr: ["BCD"]});

        await user.clear(screen.getByLabelText("Betrag (€) *"));
        await user.type(screen.getByLabelText("Betrag (€) *"), "290,00");
        await user.click(screen.getByRole("button", {name: "Als Rechnung speichern"}));
        await waitFor(() => expect(svc.CreateInvoiceFromInbox).toHaveBeenCalled());
        const [name, sent] = svc.CreateInvoiceFromInbox.mock.calls[0];
        expect(name).toBe("scan.pdf");
        expect(sent).toMatchObject({provider: "Dr. med. Anna Muster", amount: "290,00", iban: "DE89370400440532013000"});
    });

    it("takes over \"bezahlt\" for a prescription and lets the user change it", async () => {
        const user = userEvent.setup();
        const rx = {...draft, provider: "Linden-Apotheke", category: "apotheke", amount: "31.40", invoiceDate: "2026-03-14",
            paid: {done: true, date: "2026-03-14", note: ""}};
        svc.AnalyzeDocument.mockResolvedValue({...analysis(), draft: {invoice: rx, view: invoiceRow("", rx.provider)},
            notes: ["Rezept erkannt – an der Apotheke bezahlt"]});
        svc.CreateInvoiceFromInbox.mockResolvedValue({invoice: {...rx, id: "x"}, view: invoiceRow("x", rx.provider, {year: 2026})});
        renderAt("/", "/", <Inbox/>);
        expect(await screen.findByText("Rezept erkannt – an der Apotheke bezahlt")).toBeInTheDocument();
        expect(screen.getByLabelText("Bezahlt")).toBeChecked();
        expect(screen.getByLabelText("Bezahlt am")).toHaveValue("2026-03-14");
        await user.click(screen.getByRole("button", {name: "Als Rechnung speichern"}));
        await waitFor(() => expect(svc.CreateInvoiceFromInbox).toHaveBeenCalled());
        expect(svc.CreateInvoiceFromInbox.mock.calls[0][1]).toMatchObject({paid: {done: true, date: "2026-03-14"}});
    });

    it("opens the review for a refund document", async () => {
        svc.AnalyzeDocument.mockResolvedValue(analysis("refund"));
        svc.AnalyzeRefund.mockResolvedValue({payerId: "bh", date: "2026-04-28", applicationDate: "", payoutDate: "", paidOut: "",
            deductions: [], lines: [], layout: {billed: 1, eligible: 0, refund: -1}, linesTotal: "0.00", checksumOk: false,
            checksumDiff: "0.00", notes: ["Keine Positionen erkannt – bitte die Zeilen von Hand eintragen"], invoices: []});
        renderAt("/", "/", <Inbox/>);
        expect(await screen.findByText("Erstattungsbeleg erkannt")).toBeInTheDocument();
        expect(await screen.findByText(/Keine Positionen erkannt/)).toBeInTheDocument();
        expect(screen.getByRole("button", {name: "Als Rechnung behandeln"})).toBeInTheDocument();
        expect(svc.AnalyzeRefund).toHaveBeenCalledWith({lines: [{text: "Rechnung", page: 1, x: 0, y: 0, w: 0, h: 0}], qr: ["BCD"]});
    });

    it("lets the user fill the fields when recognition fails", async () => {
        capture.recognizePages.mockRejectedValue(new Error("Das Bild kann nicht gelesen werden."));
        renderAt("/", "/", <Inbox/>);
        expect(await screen.findByText(/Das Bild kann nicht gelesen werden/)).toBeInTheDocument();
        expect(screen.getByLabelText("Behandler *")).toHaveValue("");
    });

    it("shows the QR code for the phone", async () => {
        const user = userEvent.setup();
        svc.StartPhoneUpload.mockResolvedValue({url: "http://192.168.1.20:41234/u/abc", qr: "data:image/png;base64,AAAA",
            expires: "11:30", received: 0, lan: true});
        renderAt("/", "/", <Inbox/>);
        await user.click(screen.getByRole("button", {name: "Mit Handy fotografieren"}));
        expect(await screen.findByAltText("QR-Code für das Handy")).toBeInTheDocument();
        expect(screen.getByText("http://192.168.1.20:41234/u/abc")).toBeInTheDocument();
        await user.click(screen.getByRole("button", {name: "Fertig"}));
        await waitFor(() => expect(svc.StopPhoneUpload).toHaveBeenCalled());
    });

    it("keeps the edited draft when the list reloads", async () => {
        const user = userEvent.setup();
        const view = renderAt("/", "/", <Inbox/>);
        const provider = await screen.findByLabelText("Behandler *");
        await user.clear(provider);
        await user.type(provider, "Praxis Beispiel");

        // A live update reloads the list: new objects for the same file, plus a new file.
        svc.ListInbox.mockResolvedValue([
            {name: "scan.pdf", size: 1000, modified: "2026-11-02 10:15", type: "pdf", url: "/datafile/inbox/scan.pdf"},
            {name: "foto.jpg", size: 2000, modified: "2026-11-02 10:30", type: "image", url: "/datafile/inbox/foto.jpg"},
        ]);
        view.setApp({dataVersion: 1});
        expect(await screen.findByText("foto.jpg")).toBeInTheDocument();
        expect(screen.getByLabelText("Behandler *")).toHaveValue("Praxis Beispiel");
        // The detail (with progress) read scan.pdf once; the background reads the rest.
        const detailRuns = capture.recognizePages.mock.calls.filter(c => typeof c[1] === "function");
        expect(detailRuns).toHaveLength(1);
        expect(capture.renderPdfPreview).toHaveBeenCalledTimes(1);
    });

    it("reads a changed file again without discarding edits", async () => {
        const user = userEvent.setup();
        const view = renderAt("/", "/", <Inbox/>);
        const provider = await screen.findByLabelText("Behandler *");
        await user.clear(provider);
        await user.type(provider, "Praxis Beispiel");

        svc.ListInbox.mockResolvedValue([{name: "scan.pdf", size: 1200, modified: "2026-11-02 10:20", type: "pdf", url: "/datafile/inbox/scan.pdf"}]);
        svc.AnalyzeDocument.mockResolvedValue({...analysis(), draft: {invoice: {...draft, provider: "Dr. Neu"}, view: invoiceRow("", "Dr. Neu")}});
        view.setApp({dataVersion: 1});
        await waitFor(() => expect(capture.recognizePages).toHaveBeenCalledTimes(2));
        await waitFor(() => expect(svc.AnalyzeDocument).toHaveBeenCalledTimes(2));
        expect(screen.getByLabelText("Behandler *")).toHaveValue("Praxis Beispiel");
        expect(capture.renderPdfPreview).toHaveBeenCalledTimes(2);
    });

    it("closes the confirm dialog with Escape and returns the focus", async () => {
        const user = userEvent.setup();
        renderAt("/", "/", <Inbox/>);
        await screen.findByLabelText("Behandler *");
        const discard = screen.getByRole("button", {name: "Verwerfen"});
        await user.click(discard);
        const dialog = await screen.findByRole("alertdialog", {name: "„scan.pdf“ aus dem Posteingang löschen?"});
        expect(dialog).toHaveAttribute("aria-modal", "true");
        expect(screen.getByRole("button", {name: "Abbrechen"})).toHaveFocus();
        await user.keyboard("{Escape}");
        expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
        expect(discard).toHaveFocus();
        expect(svc.DiscardInbox).not.toHaveBeenCalled();
    });

    it("refuses dropped files above the size limit", async () => {
        svc.AddToInbox.mockResolvedValue("klein.pdf");
        renderAt("/", "/", <Inbox/>);
        await screen.findByLabelText("Behandler *");
        const small = new File(["%PDF-1.4"], "klein.pdf", {type: "application/pdf"});
        const big = new File(["%PDF-1.4"], "gross.pdf", {type: "application/pdf"});
        Object.defineProperty(big, "size", {value: 41 * 1024 * 1024});
        const page = screen.getByRole("heading", {name: "Posteingang"}).closest(".page-head")!.parentElement!;
        fireEvent.drop(page, {dataTransfer: {files: [small, big], types: ["Files"]}});
        await waitFor(() => expect(svc.AddToInbox).toHaveBeenCalledTimes(1));
        expect(svc.AddToInbox.mock.calls[0][0]).toBe("klein.pdf");
        expect(await screen.findByText("„gross.pdf“ ist größer als 40 MB und wurde nicht hinzugefügt. 1 Datei hinzugefügt.")).toBeInTheDocument();
    });

    it("shows an empty inbox", async () => {
        svc.ListInbox.mockResolvedValue([]);
        renderAt("/", "/", <Inbox/>);
        expect(await screen.findByText(/Der Posteingang ist leer/)).toBeInTheDocument();
    });

    it("merges ticked documents in the order of the list", async () => {
        const user = userEvent.setup();
        svc.ListInbox.mockResolvedValue([
            {name: "Scan 1.jpg", size: 10, modified: "2026-11-03 10:01", type: "image", url: "/datafile/inbox/Scan%201.jpg"},
            {name: "Brief.pdf", size: 10, modified: "2026-11-03 10:02", type: "pdf", url: "/datafile/inbox/Brief.pdf"},
            {name: "Scan 2.jpg", size: 10, modified: "2026-11-03 10:03", type: "image", url: "/datafile/inbox/Scan%202.jpg"},
        ]);
        svc.MergeInbox.mockResolvedValue("Scan 1");
        renderAt("/", "/", <Inbox/>);
        await user.click(await screen.findByLabelText("„Scan 2.jpg“ zum Zusammenfassen auswählen"));
        expect(screen.getByText(/Weitere Belege anhaken/)).toBeInTheDocument();
        await user.click(screen.getByLabelText("„Scan 1.jpg“ zum Zusammenfassen auswählen"));
        await user.click(screen.getByRole("button", {name: "Zu einem Beleg zusammenfassen (2)"}));
        await waitFor(() => expect(svc.MergeInbox).toHaveBeenCalledWith(["Scan 1.jpg", "Scan 2.jpg"]));
        expect(await screen.findByText("2 Belege zu einem zusammengefasst")).toBeInTheDocument();
    });

    it("shows the pages of a multi-page document and changes them", async () => {
        const user = userEvent.setup();
        const page = (n: number) => ({name: `Scan/0${n} Scan ${n}.jpg`, size: 10 + n, modified: "2026-11-03 10:00:00", type: "image",
            url: `/datafile/inbox/Scan/0${n}%20Scan%20${n}.jpg`});
        // Every reload hands in new objects, as the backend does.
        svc.ListInbox.mockImplementation(async () => [{name: "Scan", size: 23, modified: "2026-11-03 10:00", type: "group", url: "", group: true,
            pages: [page(1), page(2)]}]);
        svc.ReorderInbox.mockResolvedValue(undefined);
        svc.SplitInbox.mockResolvedValue(undefined);
        renderAt("/", "/", <Inbox/>);
        expect(await screen.findByText("2 Seiten", {selector: ".badge"})).toBeInTheDocument();
        await waitFor(() => expect(capture.recognizePages).toHaveBeenCalled());
        expect(capture.recognizePages.mock.calls[0][0].map((p: {url: string}) => p.url)).toEqual([page(1).url, page(2).url]);

        await user.click(screen.getByRole("button", {name: "Seite 2 nach vorne"}));
        await waitFor(() => expect(svc.ReorderInbox).toHaveBeenCalledWith("Scan", [page(2).name, page(1).name]));
        await user.click(screen.getByRole("button", {name: "Seite 1 herauslösen"}));
        await waitFor(() => expect(svc.SplitInbox).toHaveBeenCalledWith(page(1).name));
        await user.click(screen.getByRole("button", {name: "Alle Seiten trennen"}));
        await waitFor(() => expect(svc.SplitInbox).toHaveBeenCalledWith("Scan"));
        expect(screen.getByRole("button", {name: "Seite 1 nach vorne"})).toHaveAttribute("aria-disabled", "true");
    });

    it("suggests documents that belong together and remembers a no", async () => {
        const user = userEvent.setup();
        const items = [
            {name: "b.jpg", size: 10, modified: "2026-11-03 10:01", type: "image", url: "/datafile/inbox/b.jpg"},
            {name: "a.jpg", size: 11, modified: "2026-11-03 10:02", type: "image", url: "/datafile/inbox/a.jpg"},
        ];
        svc.ListInbox.mockResolvedValue(items);
        svc.SuggestInboxGroups.mockResolvedValue([{names: ["a.jpg", "b.jpg"], reason: "Seite 1 und 2 von 2"}]);
        svc.MergeInbox.mockResolvedValue("a");
        const view = renderAt("/", "/", <Inbox/>);
        expect(await screen.findByText(/„a.jpg“ und „b.jpg“ \(Seite 1 und 2 von 2\)/, {}, {timeout: 2000})).toBeInTheDocument();
        // Both were read in the background, in the order of the list.
        expect(svc.SuggestInboxGroups.mock.calls.at(-1)?.[0].map((t: {name: string}) => t.name)).toEqual(["b.jpg", "a.jpg"]);
        await user.click(screen.getByRole("button", {name: "Zusammenfassen"}));
        await waitFor(() => expect(svc.MergeInbox).toHaveBeenCalledWith(["a.jpg", "b.jpg"]));

        await user.click(screen.getByRole("button", {name: "Gehören nicht zusammen"}));
        expect(screen.queryByText(/gehören wohl zusammen/i)).not.toBeInTheDocument();
        view.unmount();
        renderAt("/", "/", <Inbox/>);
        await waitFor(() => expect(svc.SuggestInboxGroups.mock.calls.length).toBeGreaterThan(1));
        await new Promise(r => setTimeout(r, 400));
        expect(screen.queryByText(/Gehören wohl zusammen/)).not.toBeInTheDocument();
    });

    it("selects the merged document", async () => {
        const user = userEvent.setup();
        const before = [
            {name: "Brief.pdf", size: 10, modified: "2026-11-03 10:01", type: "pdf", url: "/datafile/inbox/Brief.pdf"},
            {name: "Scan 1.jpg", size: 10, modified: "2026-11-03 10:02", type: "image", url: "/datafile/inbox/Scan%201.jpg"},
            {name: "Scan 2.jpg", size: 10, modified: "2026-11-03 10:03", type: "image", url: "/datafile/inbox/Scan%202.jpg"},
        ];
        const after = [before[0], {name: "Scan 1", size: 20, modified: "2026-11-03 10:03", type: "group", url: "", group: true,
            pages: [{name: "Scan 1/01 Scan 1.jpg", size: 10, modified: "x", type: "image", url: "/datafile/inbox/Scan%201/01%20Scan%201.jpg"},
                {name: "Scan 1/02 Scan 2.jpg", size: 10, modified: "x", type: "image", url: "/datafile/inbox/Scan%201/02%20Scan%202.jpg"}]}];
        svc.ListInbox.mockResolvedValue(before);
        svc.MergeInbox.mockImplementation(async () => {
            svc.ListInbox.mockResolvedValue(after);
            return "Scan 1";
        });
        renderAt("/", "/", <Inbox/>);
        await user.click(await screen.findByLabelText("„Scan 1.jpg“ zum Zusammenfassen auswählen"));
        await user.click(screen.getByLabelText("„Scan 2.jpg“ zum Zusammenfassen auswählen"));
        await user.click(screen.getByRole("button", {name: "Zu einem Beleg zusammenfassen (2)"}));
        expect(await screen.findByRole("region", {name: "Scan 1"})).toBeInTheDocument();
    });

    it("offers no AI reading while the AI is off", async () => {
        renderAt("/", "/", <Inbox/>);
        await screen.findByDisplayValue("Dr. med. Anna Muster");
        expect(screen.queryByRole("button", {name: "Mit KI auslesen"})).not.toBeInTheDocument();
    });

    it("reads with the AI on request and keeps typed values", async () => {
        const user = userEvent.setup();
        aiOn();
        const read = {...draft, provider: "Dr. med. Anna Muster", amount: "296.40", serviceTo: "2026-03-07", iban: "DE02120300000000202051"};
        svc.ReadWithAI.mockReturnValue(cancellable(Promise.resolve(aiReading(read, {
            sources: {amount: "ai-confirmed", serviceTo: "ai", iban: "ai", provider: "label"}, confirmed: 1,
            others: {amount: {source: "label", value: "286.40"}},
        }))));
        renderAt("/", "/", <Inbox/>);
        const iban = await screen.findByLabelText("IBAN");
        await user.clear(iban);
        await user.type(iban, "DE89370400440532013000");
        await user.click(screen.getByRole("button", {name: "Mit KI auslesen"}));

        expect(capture.pageImages).toHaveBeenCalledWith([expect.objectContaining({url: "/datafile/inbox/scan.pdf"})], expect.any(AbortSignal));
        expect(svc.ReadWithAI).toHaveBeenCalledWith({pages: ["data:image/jpeg;base64,AA"],
            lines: [{text: "Rechnung", page: 1, x: 0, y: 0, w: 0, h: 0}], qr: ["BCD"]});
        expect(await screen.findByText("KI: 2 Felder gefüllt, 1 bestätigt, 2 Abweichungen (64 s).")).toBeInTheDocument();
        expect(screen.getByLabelText("Betrag (€) *")).toHaveValue("296,40");
        expect(screen.getByText("KI, bestätigt")).toBeInTheDocument();
        expect(screen.getByLabelText("Behandlung bis")).toHaveValue("2026-03-07");
        expect(screen.getByText("KI – bitte prüfen")).toBeInTheDocument();
        // The IBAN the user typed stays; the AI's reading is offered.
        expect(iban).toHaveValue("DE89370400440532013000");
        await user.click(screen.getByRole("button", {name: "KI: DE02120300000000202051 übernehmen"}));
        expect(iban).toHaveValue("DE02120300000000202051");
        // The value that lost can be taken as well.
        await user.click(screen.getByRole("button", {name: "Texterkennung: 286,40 € übernehmen"}));
        expect(screen.getByLabelText("Betrag (€) *")).toHaveValue("286,40");
        expect(screen.queryByText(/übernehmen/)).not.toBeInTheDocument();
    });

    it("cancels an AI reading and says when the server is away", async () => {
        const user = userEvent.setup();
        aiOn();
        const running = cancellable(new Promise(() => {}));
        svc.ReadWithAI.mockReturnValue(running);
        renderAt("/", "/", <Inbox/>);
        await user.click(await screen.findByRole("button", {name: "Mit KI auslesen"}));
        expect(await screen.findByText(/KI liest den Beleg … 0:0\d \(etwa 1 Minute\)/)).toBeInTheDocument();
        await user.click(screen.getByRole("button", {name: "Abbrechen"}));
        expect(running.cancel).toHaveBeenCalled();
        expect(screen.getByRole("button", {name: "Mit KI auslesen"})).toBeEnabled();

        svc.ReadWithAI.mockImplementation(() => cancellable(Promise.reject(new Error("KI-Server nicht erreichbar: 192.168.178.38:8080 antwortet nicht"))));
        await user.click(screen.getByRole("button", {name: "Mit KI auslesen"}));
        expect(await screen.findByText(/nicht erreichbar.*Die Werte der Texterkennung bleiben\./)).toBeInTheDocument();
        expect(screen.getByLabelText("Betrag (€) *")).toHaveValue("286,40");
    });
});
