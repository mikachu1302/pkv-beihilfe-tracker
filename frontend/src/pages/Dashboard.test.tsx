import {fireEvent, screen, waitFor} from "@testing-library/react";
import {beforeEach, describe, expect, it, vi} from "vitest";
import type {BucketDTO, PayerYearDTO, YearDTO} from "../api";
import {renderAt} from "../test/render";
import Dashboard from "./Dashboard";

const svc = vi.hoisted(() => ({Years: vi.fn(), Dashboard: vi.fn(), ListAllowances: vi.fn(), SaveYearSetting: vi.fn()}));
vi.mock("../api", () => ({Svc: svc}));

const b = (count: number, expected: string, actual = "0.00"): BucketDTO => ({count, expected, actual});

function payer(patch: Partial<PayerYearDTO>): PayerYearDTO {
    return {
        payerId: "", payerName: "", kind: "", claims: 5, expected: "0.00", actual: "0.00",
        refunded: b(0, "0.00"), submitted: b(0, "0.00"), open: b(0, "0.00"), held: b(0, "0.00"), skipped: b(0, "0.00"),
        deviation: "0.00", clarify: 0, suggestClarify: 0, deadlineSoon: 0, deadlineMissed: 0, openBilled: "0.00",
        minClaimAmount: "0.00", annualDeduction: "0.00", deductible: "0.00", premiumRefund: null,
        setting: {year: 2026, payerId: "", deductible: "", annualDeduction: "", expectedPremiumRefund: ""}, ...patch,
    };
}

const year: YearDTO = {
    year: 2026, invoices: 5, billed: "3800.00", expected: "3800.00", actual: "2100.00", ownShare: "1700.00",
    ownShareSettled: "0.00", allowances: "0.00",
    payers: [
        payer({payerId: "bh", payerName: "Beihilfe", kind: "beihilfe", expected: "2660.00", actual: "2100.00",
            refunded: b(3, "2100.00", "2100.00"), submitted: b(1, "350.00"), open: b(1, "210.00"), openBilled: "300.00",
            minClaimAmount: "200.00", clarify: 1}),
        payer({payerId: "pkv", payerName: "Muster PKV", kind: "pkv", expected: "1140.00", held: b(5, "1140.00"),
            premiumRefund: {expected: "1260.00", claimAll: "1140.00", claimNeutralOnly: "0.00", holdTotal: "1260.00",
                advantage: "-120.00", forfeited: false},
            setting: {year: 2026, payerId: "pkv", deductible: "", annualDeduction: "", expectedPremiumRefund: "1260.00"}}),
    ],
};

beforeEach(() => {
    vi.clearAllMocks();
    svc.Years.mockResolvedValue([2026]);
    svc.Dashboard.mockResolvedValue(year);
    svc.ListAllowances.mockResolvedValue([]);
    svc.SaveYearSetting.mockResolvedValue(undefined);
});

describe("Dashboard", () => {
    it("shows the year figures and the PKV comparison", async () => {
        renderAt("/", "/", <Dashboard/>);
        expect(await screen.findByText("3.800,00 €")).toBeInTheDocument();
        expect(screen.getByRole("region", {name: "Beihilfe"})).toHaveTextContent("1 Rechnung mit Klärungsbedarf");
        expect(screen.getByText(/über dem Mindestbetrag von 200,00 €/)).toBeInTheDocument();
        expect(screen.getByText(/Nicht einreichen lohnt sich/)).toHaveTextContent("120,00 €");
        expect(screen.getByRole("group", {name: "Beihilfe: SOLL nach Status"})).toBeInTheDocument();
        expect(screen.getByLabelText("Erstattet: 2.100,00 € (79 %)")).toBeInTheDocument();
    });

    it("saves the expected premium refund", async () => {
        renderAt("/", "/", <Dashboard/>);
        const input = await screen.findByLabelText("Erwartete Beitragsrückerstattung 2026");
        expect(input).toHaveValue("1260,00");
        fireEvent.change(input, {target: {value: "900"}});
        fireEvent.blur(input);
        await waitFor(() => expect(svc.SaveYearSetting).toHaveBeenCalled());
        expect(svc.SaveYearSetting.mock.calls[0][0]).toMatchObject({year: 2026, payerId: "pkv", expectedPremiumRefund: "900"});
    });
});
