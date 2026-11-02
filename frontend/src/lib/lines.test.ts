import {describe, expect, it} from "vitest";
import {groupLines} from "./lines";

describe("groupLines", () => {
    it("joins pieces on the same height and keeps columns apart", () => {
        const lines = groupLines([
            {text: "286,40", x: 0.8, y: 0.501, w: 0.08, h: 0.012},
            {text: "Rechnungsbetrag", x: 0.1, y: 0.5, w: 0.2, h: 0.012},
            {text: "Dr. Muster", x: 0.1, y: 0.1, w: 0.15, h: 0.02},
            {text: "Praxis", x: 0.26, y: 0.1, w: 0.08, h: 0.02},
            {text: "  ", x: 0.5, y: 0.3, w: 0.01, h: 0.01},
        ], 1);
        expect(lines.map(l => l.text)).toEqual(["Dr. Muster Praxis", "Rechnungsbetrag   286,40"]);
        expect(lines[1]).toMatchObject({page: 1, x: 0.1});
        expect(lines[1].w).toBeCloseTo(0.78);
    });
});
