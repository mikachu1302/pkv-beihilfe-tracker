import {describe, expect, it} from "vitest";
import {date, eur, num, pct, plural, shortDate, signClass, toInput} from "./format";

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
});
