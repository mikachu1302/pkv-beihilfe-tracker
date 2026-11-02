import {screen, waitFor} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {beforeEach, describe, expect, it, vi} from "vitest";
import type {ImportPreviewDTO, ImportRowDTO} from "../api";
import {settings} from "../test/fixtures";
import {renderAt} from "../test/render";
import Import from "./Import";

const svc = vi.hoisted(() => ({GetSettings: vi.fn(), PreviewSpreadsheet: vi.fn(), CommitSpreadsheet: vi.fn()}));
const desktop = vi.hoisted(() => ({ChooseFile: vi.fn()}));
vi.mock("../api", () => ({Svc: svc, Desktop: desktop}));

function row(key: string, sheet: string, n: number, patch: Partial<ImportRowDTO> = {}): ImportRowDTO {
    return {key, sheet, row: n, provider: "Dr. Muster", invoiceDate: "2024-03-14", serviceDate: "2024-03-03", year: 2024,
        amount: "120.00", status: [], note: "", warnings: [], state: "new", invoiceId: "", uncertain: false, changes: [], ...patch};
}

function preview(rows: ImportRowDTO[], patch: Partial<ImportPreviewDTO> = {}): ImportPreviewDTO {
    const count = (state: string) => rows.filter(r => r.state === state).length;
    return {token: "t1", file: "abrechnung.xlsx", sheet: [...new Set(rows.map(r => r.sheet))].join(", "), rows,
        years: [{year: 2024, count: rows.length}], allowances: 0, warnings: 0,
        new: count("new"), changed: count("changed"), unchanged: count("unchanged"), deletedInApp: count("deleted"), removed: [], ...patch};
}

beforeEach(() => {
    vi.clearAllMocks();
    svc.GetSettings.mockResolvedValue(settings);
    desktop.ChooseFile.mockResolvedValue("/tmp/abrechnung.xlsx");
});

describe("Import", () => {
    it("names the sheet of each row when the file has several", async () => {
        const user = userEvent.setup();
        svc.PreviewSpreadsheet.mockResolvedValue(preview([row("0", "2023", 5), row("1", "2024", 5)]));
        renderAt("/", "/", <Import/>);
        await user.click(await screen.findByRole("button", {name: "Excel-Datei wählen …"}));
        expect(await screen.findByText(/Blätter „2023, 2024“/)).toBeInTheDocument();
        expect(screen.getByRole("columnheader", {name: "Blatt / Zeile"})).toBeInTheDocument();
        expect(screen.getByText("2024 · 5")).toBeInTheDocument();
        expect(screen.getByRole("checkbox", {name: "2023, Zeile 5 übernehmen"})).toBeChecked();
    });

    it("shows only the row number for a single sheet", async () => {
        const user = userEvent.setup();
        svc.PreviewSpreadsheet.mockResolvedValue(preview([row("0", "Tabelle1", 5), row("1", "Tabelle1", 6)]));
        renderAt("/", "/", <Import/>);
        await user.click(await screen.findByRole("button", {name: "Excel-Datei wählen …"}));
        expect(await screen.findByText(/Blatt „Tabelle1“/)).toBeInTheDocument();
        expect(screen.getByRole("columnheader", {name: "Zeile"})).toBeInTheDocument();
        expect(screen.getByRole("checkbox", {name: "Zeile 6 übernehmen"})).toBeInTheDocument();
    });

    it("compares with the invoices already there and applies the chosen rows", async () => {
        const user = userEvent.setup();
        svc.PreviewSpreadsheet.mockResolvedValue(preview([
            row("0", "Tabelle1", 2, {provider: "Physio Mitte"}),
            row("1", "Tabelle1", 3, {provider: "Labor Nord", state: "changed", invoiceId: "a",
                changes: [{label: "Betrag", from: "13.725,22 €", to: "137,25 €", conflict: false}]}),
            row("2", "Tabelle1", 4, {provider: "Optik Ost", state: "changed", invoiceId: "b", uncertain: true,
                changes: [{label: "Betrag", from: "310,00 €", to: "320,00 €", conflict: true}]}),
            row("3", "Tabelle1", 5, {provider: "Praxis Süd", state: "unchanged", invoiceId: "c"}),
            row("4", "Tabelle1", 6, {provider: "Zahnarzt Nord", state: "changed", invoiceId: "e",
                changes: [{label: "Bemerkung", from: "aus der App", to: "aus der Excel", conflict: true}]}),
        ], {removed: [{key: "del:d", invoiceId: "d", label: "Apotheke West · 12.02.2026 · 20,00 €", sheet: "Tabelle1", row: 4}]}));
        svc.CommitSpreadsheet.mockResolvedValue({invoices: 1, updated: 1, deleted: 1, allowances: 0, skipped: 1});
        renderAt("/", "/", <Import/>);
        await user.click(await screen.findByRole("button", {name: "Excel-Datei wählen …"}));

        expect(await screen.findByText("1 neu")).toBeInTheDocument();
        expect(screen.getByText("3 geändert")).toBeInTheDocument();
        expect(screen.getByText("1 nicht mehr in der Excel")).toBeInTheDocument();
        expect(screen.getByText("137,25 €")).toBeInTheDocument();
        expect(screen.getAllByText("(in der App geändert)")).toHaveLength(2);
        expect(screen.getByText("Zuordnung bitte prüfen")).toBeInTheDocument();
        expect(screen.queryByText("Praxis Süd")).not.toBeInTheDocument(); // unchanged rows hidden
        // Preselected: new rows and certain changes; not uncertain ones or deletions.
        expect(screen.getByRole("checkbox", {name: "Zeile 3 übernehmen"})).toBeChecked();
        expect(screen.getByRole("checkbox", {name: "Zeile 4 übernehmen"})).not.toBeChecked();
        expect(screen.getByRole("checkbox", {name: "Zeile 6 übernehmen"})).not.toBeChecked(); // would overwrite the app's value
        expect(screen.getByRole("checkbox", {name: /löschen: Apotheke West/})).not.toBeChecked();

        await user.click(screen.getByRole("checkbox", {name: /löschen: Apotheke West/}));
        await user.click(screen.getByRole("button", {name: /Übernehmen \(1 neu, 1 geändert, 1 löschen\)/}));
        await waitFor(() => expect(svc.CommitSpreadsheet).toHaveBeenCalled());
        expect(new Set(svc.CommitSpreadsheet.mock.calls[0][1])).toEqual(new Set(["0", "1", "del:d"]));
        expect(await screen.findByText("Übernommen: 1 Rechnung neu, 1 aktualisiert, 1 gelöscht")).toBeInTheDocument();
    });
});
