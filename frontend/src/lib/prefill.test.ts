import {describe, expect, it} from "vitest";
import type {InvoiceDTO} from "../api";
import {aiReading} from "../test/fixtures";
import {alternativeText, applyReading, fillEmpty} from "./prefill";

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

    it("replaces values filled from a document before, unless edited", () => {
        const first = {...blank, amount: "100,00", provider: "Praxis"};
        const {next} = fillEmpty(first, {...found, amount: "286.40"}, undefined, ["amount"]);
        expect(next.amount).toBe("286,40");
        expect(next.provider).toBe("Praxis"); // typed by the user: stays
    });

    it("ignores a missing amount", () => {
        expect(fillEmpty(blank, {...found, amount: "0.00"}).filled).not.toContain("amount");
    });
});

describe("applyReading", () => {
    const read: InvoiceDTO = {...found, provider: "Donau-Apotheke Regensburg", amount: "27.21", iban: "DE89370400440532013000"};

    it("fills free fields and keeps typed ones, showing the AI's reading next to them", () => {
        const cur = {...blank, provider: "Apotheke am Markt", amount: "27,21", reference: "R-OCR"};
        const r = aiReading(read, {sources: {provider: "ai", iban: "ai-confirmed"}, others: {invoiceDate: {source: "label", value: "2026-01-03"}}});
        const {next, filled, others} = applyReading(cur, r, undefined, ["reference"]);
        expect(next).toMatchObject({provider: "Apotheke am Markt", amount: "27,21", reference: "R-1", iban: "DE89370400440532013000"});
        expect(filled).toContain("reference");
        expect(filled).not.toContain("provider");
        // typed and different: the reading as a hint; typed and the same (27,21): none
        expect(others.provider).toEqual({source: "ai", value: "Donau-Apotheke Regensburg"});
        expect(others.amount).toBeUndefined();
        // the value that lost in the backend's merge
        expect(others.invoiceDate).toEqual({source: "label", value: "2026-01-03"});
    });

    it("words the hint by field", () => {
        expect(alternativeText("amount", {source: "ai", value: "1234.5"})).toBe("KI: 1.234,50 €");
        expect(alternativeText("serviceTo", {source: "label", value: "2026-10-20"})).toBe("Texterkennung: 20.10.2026");
        expect(alternativeText("category", {source: "qr", value: "heilmittel"})).toBe("GiroCode: Heilmittel");
    });
});
