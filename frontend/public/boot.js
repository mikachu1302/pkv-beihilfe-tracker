// Sets the colour scheme and the design before the first paint, so the window
// never flashes in the wrong colours. Keep in sync with src/lib/theme.ts and
// src/lib/design.ts, which take over once the app is loaded.
(function () {
    var root = document.documentElement, theme = null, design = null;
    try {
        theme = localStorage.getItem("pkv.theme");
        design = localStorage.getItem("pkv.design");
    } catch (e) {
        // no storage: the defaults below
    }
    var dark = theme === "dark" ||
        (theme !== "light" && typeof window.matchMedia === "function" && window.matchMedia("(prefers-color-scheme: dark)").matches);
    root.dataset.theme = dark ? "dark" : "light";
    root.dataset.design = design === "classic" ? "classic" : "new";
})();
