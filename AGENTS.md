# Working on pkv-beihilfe-tracker

Guidelines for anyone – person or coding agent – changing this repository. They sum up the
decisions in [docs/PLAN.md](docs/PLAN.md) and what the review rounds of v0.2–v0.5 kept finding.
The plan documents say *what* the app does; this file says *how* to work on it.

## The non-negotiables

- **No private data in the repository.** No real invoices, Bescheide, amounts, names, IBANs,
  insurance numbers, file paths or screenshots of real data – not in tests, fixtures, docs,
  commit messages or issues. Tests and test documents are made up (`cmd/demodata`,
  `scripts/ki-testbelege.py`, `internal/ask/testdata`). `.gitignore` blocks images, PDFs and
  spreadsheets outside `testdata/`; do not force-add them.
- **The API key of the AI server** lives in the git-ignored `.env` (`API_KEY=…`) for test tooling
  only. Never print, log or commit it; the app itself never reads `.env` (users enter the key in
  Settings · KI, stored per device). Delete scratch configs that hold the key after a test.
- **Commits** as `Mika <223709831+mikachu1302@users.noreply.github.com>`, without AI
  co-author or "generated with" lines. `main` is protected – no force push.
- **Tags and releases only on an explicit request** ("Release vX.Y.Z bitte"), never as a side
  effect of "weiter" or "passt".
- **Never break the data folder for other devices.** Older app versions on other devices read and
  write the same folder. A new field whose loss would lose user data needs a new
  `store.SchemaVersion` (see the comment there); rebuildable data does not. Say in the plan which
  case a change is.

## Architecture rules

- **Go computes, the UI displays.** Sums, SOLL/IST, matching, checks, deadlines live in Go
  (`calc`, `match`, `ask`, `airead`, …) with unit tests; the frontend shows DTOs. If the
  UI needs a number, add it to the DTO (e.g. `InvoiceRowDTO.Owed`) instead of computing it in
  TypeScript – a float sum in the browser once showed "-0,00 €".
- **Money is `money.Cents` (int64), rates `money.Rate`, dates `civil.Date`.** No floats, no
  `time.Time` for calendar dates. DTOs carry amounts as strings; input accepts the German format.
- **AI: the model reads and phrases, the app computes and checks.** The model fills a fixed JSON
  schema (questions → a query, documents → fields); every sum, comparison and rule is Go code.
  Dates and amounts the model **copies as printed**, and Go parses them: asked to convert them, it
  turned 20.11.2026 into 2022-11-20 and left most totals empty (`docs/PLAN-v0.5.md`,
  `docs/PLAN-v0.6.md`). Tables, too, are better copied (cells separated by " | ").
  Order of trust: GiroCode > AI confirmed by OCR > OCR label > AI alone > guess; the losing value
  stays visible as an alternative. AI only runs on a manual trigger, and only with a model that
  reads images: `ai.Client.Chat` refuses one the server lists as text-only – on the home server
  that model searches the web, and health data must not go there.
- **Rules on text read whole words of a folded string** (`match.Fold`, `match.NameWords`), never
  substrings: "zurück" must not fire in "zurückbekommen", "ERGO" not in "Ergotherapie", "Datum"
  not in "Geburtsdatum". Handle negation ("ohne Klärungsbedarf"). Every such rule gets a negative
  test (`internal/ask/misread_test.go`).
- **One record = one JSON file**, merged per leaf by clocks. An open form saves only the fields it
  edited (`leaf.Rebase`); keep that when adding fields.
- **Language:** UI texts German; code, comments, commit messages and plan docs English; user
  guides (`docs/INSTALLATION.md`, `docs/KI-TEST.md`) German. Error strings that reach the UI are
  German (ST1005 is excluded for those packages in `.golangci.yml`) and, like all Go error
  strings, end without a period; wrap with `%w`.

## Reuse before writing

Duplicated code was the most common review finding. Look for these first:

- Go: `model.Categories` / `Category.Label()`, `money.Cents.EUR()` / `.DE()`, `match.Fold`,
  `match.NameWords`, `extract.NormIBAN`, `extract.SameText`, `ai.NormalizeURL`,
  `calc.ClaimView` (`Owed`, `LongWait`, `DeadlineSoon`), `Config.Clone`.
- Frontend: `lib/useCancellable.ts` (cancellable backend calls), `lib/aiRead.ts` /
  `components/AIRead.tsx`, `lib/prefill.ts` (`fillEmpty`), `lib/format.ts`, `lib/store.ts`,
  `lib/appearance*.ts`, `pages/invoices/{model.ts,hooks.ts,parts.tsx}`, `components/ui.tsx`.
- Tests: `frontend/src/test/` (`render.tsx`, `fixtures.ts`, `layout.ts` for virtualised lists),
  `internal/ai/aitest` (stand-in AI server).

## Frontend

- **Styles only through tokens** (`styles/classic.css`, `styles/new.css`): every screen must work
  in both designs (classic/new) and both themes (light/dark). No inline spacing or colours; new
  rules for plain elements in `:where()`. `styles/contrast.test.ts` checks text contrast ≥ 4.5:1 –
  add new token pairs there.
- **Long-running calls are cancellable and late results are dropped.** Use `useCancellable`;
  guard results with a key of the input (pages, question) so an answer for changed input is
  ignored; leaving, saving or "Speichern & nächste" cancels what is running.
- **Accessibility is part of done:** labelled controls, visible keyboard focus, focus moves
  somewhere sensible after removing an item, status never by colour alone, `aria-live` regions
  without duplicates, shortcuts listed under "?".
- Wails v3 bound methods take `ctx` first – the JS side gets a cancellable promise; cancel it.
- `frontend/bindings/` is generated (`make bindings`), not committed.

## Checking your work

Run what CI runs before saying something is done:

```sh
make check            # gofmt, vet, race tests, golangci-lint, frontend typecheck/lint/vitest
~/go/bin/golangci-lint run   # outside make: the linter is not on the default PATH
```

- Do not trust an empty output: a bare `golangci-lint run | tail` once printed nothing because the
  binary was not found, and CI went red with 18 findings. Call it by path or through `make`.
- After pushing, read the CI result job by job (`gh run view <id>`), not the exit code of a watch
  command. CI also runs Windows and macOS – file modes, paths and line endings differ there.
- Go tests that need no UI: `go test ./internal/...`. `make test`/`make check` create a
  placeholder `frontend/dist` (main.go embeds it).
- jsdom has no layout: virtualised views render nothing unless a test stubs `offsetHeight`
  (`test/layout.ts`).
- AI: unit tests use the stand-in server; live tests are opt-in
  (`KI_TEST=1 go test ./internal/ask -run Live -v`, documents: `scripts/ki-belegtest.py`, see
  `docs/KI-TEST.md`). Recorded model answers in `internal/ask/testdata` must stay 100 % right.
  Report measured accuracy honestly – fresh phrasings, not only the tuned set.
- **See it in the app.** For UI changes, run the app headless: build Wails server mode
  (`go build -tags server,gtk3` after `npm run build`), start it with `XDG_CONFIG_HOME` pointing to
  a scratch config with a demo data folder (`go run -tags gtk3 ./cmd/demodata <dir>`), drive it
  with Playwright/Firefox and look at the screenshots. Server mode cuts calls after
  `WriteTimeout` (set to 30 min in `main.go`) – a cut call is retried and looks like a delay.
- Stop background servers by port (`ss -ltnp`), never with `pkill -f <pattern>` – the pattern
  matches your own shell. Running `make bindings`/`make check` while `make dev` runs breaks the
  dev window (restart it and clear `frontend/node_modules/.vite`).

## How a release is made

1. **Plan** in `docs/PLAN-vX.Y.md`: goal, flow, data, steps, open points. The user decides open
   points; record each decision with its date. Test risky parts first with made-up data and write
   the measurements into the plan.
2. **Implement** step by step with tests; keep `docs/PLAN.md` section 16 and the release plan's
   implementation notes up to date (what changed and why).
3. **Review** with independent reviewers (e.g. functional regressions against the last release,
   duplicated code, guidelines/quality, and the domain logic), fix the findings, note them in the
   plan.
4. **CI green** on all jobs, README/docs updated (privacy section when data leaves the device).
5. **Release** only when the user asks: tag `vX.Y.Z` on `main`; `release.yml` builds Windows
   (NSIS), macOS (universal DMG/zip) and Linux (DEB/RPM/tar.gz). Release notes in German.
