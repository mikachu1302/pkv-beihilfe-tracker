import {describe, expect, it} from "vitest";
import {isTyping, key, mod} from "./shortcuts";

describe("shortcuts", () => {
    it("knows when keys go into a field", () => {
        const text = document.createElement("input");
        const box = document.createElement("input");
        box.type = "checkbox";
        expect(isTyping(text)).toBe(true);
        expect(isTyping(document.createElement("textarea"))).toBe(true);
        expect(isTyping(box)).toBe(false);
        expect(isTyping(document.createElement("button"))).toBe(false);
        expect(isTyping(null)).toBe(false);
    });

    it("uses Ctrl outside macOS", () => {
        expect(key("S")).toBe("Strg+S");
        expect(mod(new KeyboardEvent("keydown", {key: "s", ctrlKey: true}))).toBe(true);
        expect(mod(new KeyboardEvent("keydown", {key: "s", ctrlKey: true, altKey: true}))).toBe(false);
        expect(mod(new KeyboardEvent("keydown", {key: "s", metaKey: true}))).toBe(false);
    });
});
