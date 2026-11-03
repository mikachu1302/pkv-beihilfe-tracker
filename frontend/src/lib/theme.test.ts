import {afterEach, describe, expect, it, vi} from "vitest";
import {applyTheme, initTheme, nextTheme} from "./theme";

function fakeSystem(dark: boolean) {
    const listeners: (() => void)[] = [];
    const mql = {matches: dark, addEventListener: (_: string, l: () => void) => listeners.push(l)};
    vi.stubGlobal("matchMedia", vi.fn(() => mql));
    return {
        set(d: boolean) {
            mql.matches = d;
            listeners.forEach(l => l());
        },
    };
}

afterEach(() => {
    vi.unstubAllGlobals();
    applyTheme("");
    localStorage.clear();
});

describe("theme", () => {
    it("follows the system unless a scheme is chosen", () => {
        const system = fakeSystem(true);
        initTheme();
        applyTheme("");
        expect(document.documentElement.dataset.theme).toBe("dark");
        system.set(false);
        expect(document.documentElement.dataset.theme).toBe("light");

        applyTheme("dark");
        expect(document.documentElement.dataset.theme).toBe("dark");
        expect(localStorage.getItem("pkv.theme")).toBe("dark");
        system.set(false); // a chosen scheme stays
        expect(document.documentElement.dataset.theme).toBe("dark");

        applyTheme("");
        expect(localStorage.getItem("pkv.theme")).toBeNull();
    });

    it("cycles System → Hell → Dunkel", () => {
        expect([nextTheme(""), nextTheme("light"), nextTheme("dark")]).toEqual(["light", "dark", ""]);
    });
});
