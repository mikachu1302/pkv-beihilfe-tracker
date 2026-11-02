// Minimal lint setup: TypeScript recommendations plus the classic React hooks
// rules (rules-of-hooks, exhaustive-deps) – stale or over-eager effect
// dependencies are an easy bug to miss in review.
import {defineConfig, globalIgnores} from "eslint/config";
import reactHooks from "eslint-plugin-react-hooks";
import tseslint from "typescript-eslint";

export default defineConfig([
    globalIgnores(["dist/", "bindings/", "public/", "node_modules/"]),
    {
        files: ["src/**/*.{ts,tsx}", "*.ts"],
        extends: [tseslint.configs.recommended],
        plugins: {"react-hooks": reactHooks},
        rules: {
            "react-hooks/rules-of-hooks": "error",
            "react-hooks/exhaustive-deps": "error",
        },
    },
]);
