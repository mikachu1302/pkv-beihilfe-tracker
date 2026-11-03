// Text contrast >= 4.5:1 (WCAG AA) for the text/background pairs of both
// designs in light and dark, read from the token files themselves.
import {describe, expect, it} from "vitest";
import classic from "./classic.css?raw";
import fresh from "./new.css?raw";

/** The custom properties declared in the block that starts with selector. */
function block(source: string, selector: string): Record<string, string> {
    const start = source.indexOf(`${selector} {`);
    if (start < 0) throw new Error(`no block ${selector}`);
    const body = source.slice(start + selector.length + 2, source.indexOf("}", start));
    const out: Record<string, string> = {};
    for (const m of body.matchAll(/--([\w-]+):\s*([^;]+);/g)) out[m[1]] = m[2].trim();
    return out;
}

/** Resolves var(--x) references; only hex colours are compared. */
function resolve(tokens: Record<string, string>, name: string): string {
    let v = tokens[name];
    for (let i = 0; v?.startsWith("var(") && i < 5; i++) v = tokens[v.slice(6, -1).split(",")[0].trim()];
    return v ?? "";
}

function luminance(hex: string): number {
    const c = hex.replace("#", "");
    const full = c.length === 3 ? [...c].map(x => x + x).join("") : c;
    const [r, g, b] = [0, 2, 4].map(i => parseInt(full.slice(i, i + 2), 16) / 255)
        .map(x => (x <= 0.04045 ? x / 12.92 : ((x + 0.055) / 1.055) ** 2.4));
    return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

function contrast(a: string, b: string): number {
    const [x, y] = [luminance(a), luminance(b)].sort((p, q) => q - p);
    return (x + 0.05) / (y + 0.05);
}

const combos: [string, Record<string, string>][] = [
    ["Klassisch hell", block(classic, ":root")],
    ["Klassisch dunkel", {...block(classic, ":root"), ...block(classic, ':root[data-theme="dark"]')}],
    ["Neu hell", {...block(classic, ":root"), ...block(fresh, ':root[data-design="new"]')}],
    ["Neu dunkel", {...block(classic, ":root"), ...block(fresh, ':root[data-design="new"]'), ...block(fresh, ':root[data-design="new"][data-theme="dark"]')}],
];

/** [text, background] pairs used by both designs. */
const shared: [string, string][] = [
    ["text", "bg"], ["text", "panel"], ["muted", "bg"], ["muted", "panel"],
    ["accent-fg", "bg"], ["accent-fg", "panel"], ["accent-text", "accent"],
    ["good", "good-bg"], ["warn", "warn-bg"], ["bad", "bad-bg"], ["good", "panel"], ["warn", "panel"], ["bad", "panel"],
    ["text", "good-bg"], ["text", "warn-bg"], ["text", "bad-bg"],
    ["#ffffff", "bad-strong"], ["toast-text", "toast-bg"], ["nav-text", "nav"], ["nav-active", "nav"],
];
/** Surfaces only the new design uses (views, rail) and the text on them. */
const onlyNew: [string, string][] = ["text", "muted", "accent-fg", "good", "warn", "bad", "info", "payer-1", "payer-2"]
    .flatMap(fg => ["panel-2", "zebra", "row-hover", "row-selected"].map(bg => [fg, bg] as [string, string]))
    .concat([["nav-active", "nav-active-bg"], ["nav-text", "nav-hover"], ["info", "panel"], ["payer-1", "panel"], ["payer-2", "panel"]]);

describe("contrast", () => {
    for (const [name, tokens] of combos) {
        it(`${name}: every text pair >= 4.5:1`, () => {
            const pairs = name.startsWith("Neu") ? [...shared, ...onlyNew] : shared;
            const low: string[] = [];
            for (const [fg, bg] of pairs) {
                const a = fg.startsWith("#") ? fg : resolve(tokens, fg);
                const b = resolve(tokens, bg);
                expect(a, `${fg} in ${name}`).toMatch(/^#[0-9a-f]{3,6}$/i);
                expect(b, `${bg} in ${name}`).toMatch(/^#[0-9a-f]{3,6}$/i);
                const r = contrast(a, b);
                if (r < 4.5) low.push(`${fg} ${a} on ${bg} ${b}: ${r.toFixed(2)}`);
            }
            expect(low).toEqual([]);
        });
    }
});
