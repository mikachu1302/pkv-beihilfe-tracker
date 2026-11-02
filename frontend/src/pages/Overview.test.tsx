import {screen, within} from "@testing-library/react";
import {beforeEach, describe, expect, it, vi} from "vitest";
import type {BucketDTO, OutstandingDTO, PayerYearDTO, YearDTO} from "../api";
import {renderAt} from "../test/render";
import Overview from "./Overview";

const svc = vi.hoisted(() => ({Overview: vi.fn(), ExportExcel: vi.fn()}));
vi.mock("../api", () => ({Svc: svc, Desktop: {}}));

const b = (expected = "0.00", actual = "0.00"): BucketDTO => ({count: 1, expected, actual});
const out = (total: string, patch: Partial<OutstandingDTO> = {}): OutstandingDTO =>
    ({expected: "0.00", actual: "0.00", total, submitted: total, open: "0.00", refunded: "0.00", held: "0.00", ...patch});

function payer(id: string, name: string, expected: string, actual: string, outstanding: OutstandingDTO): PayerYearDTO {
    return {
        payerId: id, payerName: name, kind: id, claims: 2, expected, actual, refunded: b(), submitted: b(), open: b(), held: b(),
        skipped: b(), deviation: "0.00", clarify: 0, suggestClarify: 0, deadlineSoon: 0, deadlineMissed: 0, openBilled: "0.00",
        minClaimAmount: "0.00", annualDeduction: "0.00", deductible: "0.00", premiumRefund: null,
        setting: {year: 0, payerId: id, deductible: "", annualDeduction: "", expectedPremiumRefund: ""}, outstanding,
    };
}

function year(y: number, bh: string, pkv: string): YearDTO {
    return {
        year: y, invoices: 2, billed: "1000.00", expected: "1000.00", actual: "600.00", ownShare: "400.00", ownShareSettled: "0.00",
        allowances: "0.00",
        payers: [payer("bh", "Beihilfe", "700.00", "500.00", out(bh)), payer("pkv", "Muster PKV", "300.00", "100.00", out(pkv))],
    };
}

beforeEach(() => {
    vi.clearAllMocks();
    svc.Overview.mockResolvedValue({
        years: [year(2026, "150.00", "0.00"), year(2025, "50.00", "0.00")],
        total: {...year(0, "200.00", "0.00"), payers: [
            payer("bh", "Beihilfe", "1400.00", "1000.00", out("200.00", {expected: "1200.00", actual: "1000.00"})),
            payer("pkv", "Muster PKV", "600.00", "200.00", out("0.00", {held: "400.00"})),
        ]},
    });
});

describe("Overview", () => {
    it("shows the outstanding amount per payer over all years and per year", async () => {
        renderAt("/", "/", <Overview/>);
        const card = (await screen.findByText("Beihilfe · Ausstand alle Jahre")).closest(".card") as HTMLElement;
        expect(within(card).getByText("200,00 €")).toBeInTheDocument();
        expect(screen.getByText("Muster PKV · Ausstand alle Jahre")).toBeInTheDocument();
        expect(screen.getByText("zurückgehalten 400,00 € – nicht eingerechnet")).toBeInTheDocument();
        const row2026 = screen.getByRole("cell", {name: "2026"}).closest("tr") as HTMLElement;
        expect(within(row2026).getByText("150,00 €")).toBeInTheDocument();
        expect(screen.getAllByRole("columnheader", {name: /Ausstand/})).toHaveLength(2);
        const avgRow = screen.getByRole("cell", {name: "Ø pro Jahr"}).closest("tr") as HTMLElement;
        expect(within(avgRow).getByText("100,00 €")).toBeInTheDocument(); // 200 € over two years
    });
});
