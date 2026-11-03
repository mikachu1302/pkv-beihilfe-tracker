// public/boot.js paints colour scheme and design before the app loads; it
// must come to the same result as lib/theme.ts and lib/design.ts.
import {afterEach, describe, expect, it, vi} from "vitest";
import boot from "../../public/boot.js?raw";
import {applyDesign} from "./design";
import {applyTheme} from "./theme";

function systemDark(dark: boolean) {
    vi.stubGlobal("matchMedia", vi.fn(() => ({matches: dark, addEventListener: () => {}})));
}

/** What boot.js sets for these stored values. */
function runBoot(theme: string | null, design: string | null) {
    localStorage.clear();
    if (theme !== null) localStorage.setItem("pkv.theme", theme);
    if (design !== null) localStorage.setItem("pkv.design", design);
    delete document.documentElement.dataset.theme;
    delete document.documentElement.dataset.design;
    new Function(boot)();
    return {...document.documentElement.dataset};
}

afterEach(() => vi.unstubAllGlobals());

describe("boot.js", () => {
    for (const dark of [false, true]) {
        it(`matches theme.ts and design.ts (system ${dark ? "dark" : "light"})`, () => {
            systemDark(dark);
            for (const theme of [null, "", "light", "dark", "sepia"]) {
                for (const design of [null, "new", "classic", "retro"]) {
                    const booted = runBoot(theme, design);
                    applyTheme(theme ?? "");
                    applyDesign(design ?? "");
                    expect(booted, `theme ${theme}, design ${design}`).toEqual({
                        theme: document.documentElement.dataset.theme, design: document.documentElement.dataset.design,
                    });
                }
            }
        });
    }
});
