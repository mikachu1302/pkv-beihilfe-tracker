import {fireEvent, screen, waitFor} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {beforeEach, describe, expect, it, vi} from "vitest";
import type {AnalysisDTO, InvoiceDTO} from "../api";
import {invoiceRow, noMark, settings} from "../test/fixtures";
import {renderAt} from "../test/render";
import Inbox from "./Inbox";

const svc = vi.hoisted(() => ({
    ListInbox: vi.fn(), AnalyzeDocument: vi.fn(), CreateInvoiceFromInbox: vi.fn(), DiscardInbox: vi.fn(),
    AddFilesToInbox: vi.fn(), AddToInbox: vi.fn(), StartPhoneUpload: vi.fn(), StopPhoneUpload: vi.fn(),
    GetSettings: vi.fn(), AnalyzeRefund: vi.fn(), ReviewRefund: vi.fn(), ConfirmRefund: vi.fn(),
}));
const desktop = vi.hoisted(() => ({ChooseFiles: vi.fn()}));
const capture = vi.hoisted(() => ({recognize: vi.fn(), renderPdfPreview: vi.fn()}));
vi.mock("../api", () => ({Svc: svc, Desktop: desktop}));
vi.mock("../lib/capture", () => capture);

const draft: InvoiceDTO = {
    id: "", provider: "Dr. med. Anna Muster", payee: "PVS Beispiel eG", iban: "DE89370400440532013000", reference: "2026-004711",
    category: "arzt", invoiceDate: "2026-03-14", serviceFrom: "2026-03-03", serviceTo: "2026-03-05", serviceYear: 0,
    amount: "286.40", due: "", paid: noMark, claims: [], attachments: [], note: "",
};

function analysis(kind = "invoice"): AnalysisDTO {
    return {kind, draft: {invoice: draft, view: invoiceRow("", draft.provider)}, sources: {amount: "qr", provider: "label", category: "guess"}, notes: []};
}

beforeEach(() => {
    vi.clearAllMocks();
    svc.ListInbox.mockResolvedValue([{name: "scan.pdf", size: 1000, modified: "2026-11-02 10:15", type: "pdf", url: "/datafile/inbox/scan.pdf"}]);
    capture.recognize.mockResolvedValue({lines: [{text: "Rechnung", page: 1, x: 0, y: 0, w: 0, h: 0}], qr: ["BCD"], ocr: false});
    capture.renderPdfPreview.mockResolvedValue(undefined);
    svc.AnalyzeDocument.mockResolvedValue(analysis());
    svc.StopPhoneUpload.mockResolvedValue(undefined);
    svc.GetSettings.mockResolvedValue(settings);
});

describe("Inbox", () => {
    it("recognises a document and saves it as an invoice", async () => {
        const user = userEvent.setup();
        svc.CreateInvoiceFromInbox.mockResolvedValue({invoice: {...draft, id: "x"}, view: invoiceRow("x", draft.provider, {year: 2026})});
        renderAt("/", "/", <Inbox/>);
        expect(await screen.findByDisplayValue("Dr. med. Anna Muster")).toBeInTheDocument();
        expect(screen.getByLabelText("Betrag (€) *")).toHaveValue("286,40");
        expect(screen.getByText("aus GiroCode")).toBeInTheDocument();
        expect(screen.getByText("bitte prüfen")).toBeInTheDocument();
        expect(capture.recognize).toHaveBeenCalledWith("/datafile/inbox/scan.pdf", "pdf", expect.any(Function), expect.any(AbortSignal));
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
        capture.recognize.mockRejectedValue(new Error("Das Bild kann nicht gelesen werden."));
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
        expect(capture.recognize).toHaveBeenCalledTimes(1);
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
        await waitFor(() => expect(capture.recognize).toHaveBeenCalledTimes(2));
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
});
