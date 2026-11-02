import {describe, expect, it} from "vitest";
import {date, eur, num, parseAmount, pct, plural, shortDate, signClass, toInput} from "./format";

describe("format", () => {
    it("formats euro amounts the German way", () => {
        expect(eur("1234.5")).toBe("1.234,50 €");
        expect(eur(-20.28)).toBe("-20,28 €");
        expect(eur("")).toBe("–");
        expect(eur(null)).toBe("–");
    });

    it("formats dates", () => {
        expect(date("2026-03-14")).toBe("14.03.2026");
        expect(shortDate("2026-03-14")).toBe("14.03.26");
        expect(date("")).toBe("–");
    });

    it("prepares values for inputs", () => {
        expect(toInput("286.40")).toBe("286,40");
        expect(toInput("")).toBe("");
        expect(pct("30.5")).toBe("30,5 %");
        expect(num("abc")).toBe(0);
        expect(signClass("-1")).toBe("neg");
        expect(plural(1, "Rechnung", "Rechnungen")).toBe("1 Rechnung");
        expect(plural(1200, "Rechnung", "Rechnungen")).toBe("1.200 Rechnungen");
    });

    it("reads amounts typed the German way", () => {
        expect(parseAmount("120,52")).toBe(120.52);
        expect(parseAmount("1.234,56")).toBe(1234.56);
        expect(parseAmount("1.234.567,8")).toBe(1234567.8);
        expect(parseAmount("1.234")).toBe(1234);
        expect(parseAmount("-20,28 €")).toBe(-20.28);
        expect(parseAmount("12,50-")).toBe(-12.5);
        expect(parseAmount(" 7,5 ")).toBe(7.5);
        expect(parseAmount("EUR 7")).toBe(7);
        expect(parseAmount(",5")).toBe(0.5);
        expect(parseAmount(42)).toBe(42);
    });

    it("reads amounts from the backend and OCR", () => {
        expect(parseAmount("1234.56")).toBe(1234.56);
        expect(parseAmount("120.5")).toBe(120.5);
        expect(parseAmount("-0.01")).toBe(-0.01);
        expect(parseAmount("100")).toBe(100);
        expect(parseAmount("1,234.56")).toBe(1234.56);
        expect(parseAmount("1,234,567")).toBe(1234567);
    });

    it("rejects text that is no amount", () => {
        for (const v of ["", "   ", "€", "abc", "12a", "1,2x", "--1", ".", undefined, null]) {
            expect(parseAmount(v), String(v)).toBeNaN();
        }
    });
});
