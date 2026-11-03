/// <reference types="vitest/config" />
import {defineConfig} from "vite";
import type {Plugin} from "vite";
import react from "@vitejs/plugin-react";
import wails from "@wailsio/runtime/plugins/vite";

/** The CSP in index.html applies to builds; the dev server needs inline scripts and its HMR websocket. */
function noCspInDev(): Plugin {
    return {
        name: "no-csp-in-dev",
        apply: "serve",
        transformIndexHtml: html => html.replace(/\s*<meta http-equiv="Content-Security-Policy"[^>]*>/, ""),
    };
}

export default defineConfig({
    server: {
        host: "127.0.0.1",
        port: Number(process.env.WAILS_VITE_PORT) || 9246,
        strictPort: true,
    },
    plugins: [react(), wails("./bindings"), noCspInDev()],
    test: {
        environment: "jsdom",
        globals: true,
        setupFiles: ["./src/test/setup.ts"],
        // The contrast test reads the token files (?raw); other CSS stays empty in tests.
        css: {include: [/src\/styles\/.*\.css/]},
        // Wails bindings are mocked in tests; the real runtime is not needed.
        server: {deps: {inline: ["@wailsio/runtime"]}},
    },
});
