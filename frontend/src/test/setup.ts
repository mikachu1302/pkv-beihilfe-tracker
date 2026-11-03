import "@testing-library/jest-dom/vitest";
import {cleanup} from "@testing-library/react";
import {afterEach} from "vitest";
import {setAI} from "../lib/ai";
import {setAppearance, setSessionView} from "../lib/appearance";
import {applyDesign} from "../lib/design";
import {applyTheme} from "../lib/theme";
import {restoreLayout} from "./layout";

// Every test starts like a fresh device: default design and theme, no
// settings loaded (AI off), no quick view choice, nothing stored, no layout stubs.
afterEach(() => {
    cleanup();
    restoreLayout();
    applyDesign("new");
    applyTheme("");
    setSessionView(null);
    setAppearance(null);
    setAI(null);
    localStorage.clear();
});
