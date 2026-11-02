import {screen} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {beforeEach, describe, expect, it, vi} from "vitest";
import type {ImportPreviewDTO, ImportRowDTO} from "../api";
import {settings} from "../test/fixtures";
import {renderAt} from "../test/render";
import Import from "./Import";

const svc = vi.hoisted(() => ({GetSettings: vi.fn(), PreviewSpreadsheet: vi.fn(), CommitSpreadsheet: vi.fn()}));
const desktop = vi.hoisted(() => ({ChooseFile: vi.fn()}));
vi.mock("../api", () => ({Svc: svc, Desktop: desktop}));

function row(key: string, sheet: string, n: number): ImportRowDTO {
    return {key, sheet, row: n, provider: "Dr. Muster", invoiceDate: "2024-03-14", serviceDate: "2024-03-03", year: 2024,
        amount: "120.00", status: [], note: "", duplicate: false, warnings: []};
}

function preview(rows: ImportRowDTO[]): ImportPreviewDTO {
    return {token: "t1", file: "abrechnung.xlsx", sheet: [...new Set(rows.map(r => r.sheet))].join(", "), rows,
        years: [{year: 2024, count: rows.length}], allowances: 0, duplicates: 0, warnings: 0};
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
        expect(screen.getByRole("checkbox", {name: "2023, Zeile 5 importieren"})).toBeChecked();
    });

    it("shows only the row number for a single sheet", async () => {
        const user = userEvent.setup();
        svc.PreviewSpreadsheet.mockResolvedValue(preview([row("0", "Tabelle1", 5), row("1", "Tabelle1", 6)]));
        renderAt("/", "/", <Import/>);
        await user.click(await screen.findByRole("button", {name: "Excel-Datei wählen …"}));
        expect(await screen.findByText(/Blatt „Tabelle1“/)).toBeInTheDocument();
        expect(screen.getByRole("columnheader", {name: "Zeile"})).toBeInTheDocument();
        expect(screen.getByRole("checkbox", {name: "Zeile 6 importieren"})).toBeInTheDocument();
    });
});
