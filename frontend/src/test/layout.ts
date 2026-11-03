// jsdom has no layout: every element is 0 px high, so a virtualised list
// would render nothing. Tests give the elements they need a height; the stub
// is removed after each test (setup.ts).
import {vi} from "vitest";
import type {MockInstance} from "vitest";

const stubs: MockInstance[] = [];

/** Gives elements an offsetHeight (what the virtualiser measures), e.g. the scroll area and its rows. */
export function stubHeights(height: (el: HTMLElement) => number) {
    stubs.push(vi.spyOn(HTMLElement.prototype, "offsetHeight", "get").mockImplementation(function (this: HTMLElement) {
        return height(this);
    }));
}

export function restoreLayout() {
    stubs.splice(0).forEach(s => s.mockRestore());
}
