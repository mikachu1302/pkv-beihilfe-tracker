import {describe, expect, it} from "vitest";
import type {InvoiceDTO} from "../api";
import {fillEmpty} from "./prefill";

const m = {done: false, date: "", note: ""};
const blank: InvoiceDTO = {id: "", provider: "", payee: "", iban: "", reference: "", category: "", invoiceDate: "", serviceFrom: "",
    serviceTo: "", serviceYear: 0, amount: "", due: "", paid: m, claims: [], attachments: [], note: ""};
const found: InvoiceDTO = {...blank, provider: "Donau-Apotheke", category: "apotheke", invoiceDate: "2026-01-02", amount: "27.21",
    reference: "R-1", paid: {done: true, date: "2026-01-02", note: ""}};

describe("fillEmpty", () => {
    it("fills empty fields and names them", () => {
        const {next, filled} = fillEmpty(blank, found);
        expect(next).toMatchObject({provider: "Donau-Apotheke", amount: "27,21", invoiceDate: "2026-01-02", paid: {done: true}});
        expect(filled.sort()).toEqual(["amount", "category", "invoiceDate", "paid", "provider", "reference"]);
    });

    it("keeps what the user typed", () => {
        const typed = {...blank, provider: "Apotheke am Markt", amount: "30,00", paid: {done: true, date: "2026-01-05", note: ""}};
        const {next, filled} = fillEmpty(typed, found);
        expect(next).toMatchObject({provider: "Apotheke am Markt", amount: "30,00", paid: {date: "2026-01-05"}, invoiceDate: "2026-01-02"});
        expect(filled).not.toContain("provider");
        expect(filled).not.toContain("amount");
        expect(filled).not.toContain("paid");
    });

    it("replaces values a new form starts with, unless changed", () => {
        const fresh = {...blank, invoiceDate: "2026-11-03"}; // today, set by the new form
        expect(fillEmpty(fresh, found, fresh).next.invoiceDate).toBe("2026-01-02");
        const typed = {...fresh, invoiceDate: "2026-10-30"};
        expect(fillEmpty(typed, found, fresh).next.invoiceDate).toBe("2026-10-30");
        expect(fillEmpty(fresh, found).next.invoiceDate).toBe("2026-11-03"); // without defaults: kept
    });

    it("ignores a missing amount", () => {
        expect(fillEmpty(blank, {...found, amount: "0.00"}).filled).not.toContain("amount");
    });
});
