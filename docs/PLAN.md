# pkv-beihilfe-tracker – Technical Plan

Status: M1–M4 implemented (core, desktop app, live data folder, capture) · 2026-11-02 – section 16 lists what changed during implementation

## 1. Goal

A desktop app (Windows, Linux, macOS) for a privately insured person that replaces the
spreadsheet "Abrechnung Krankenkasse". It records medical invoices, tracks what was
submitted to which payer and what came back, and shows a yearly dashboard.

- Coverage: Beihilfe (50 % or 70 %) plus PKV (50 % or 30 %), **or PKV alone (100 %)**. It can
  change over time (other Beihilfestelle, retirement, other insurer).
- One person, no family members (for now).
- No running costs, no server. The data is a folder of files that the user keeps wherever
  they like: synced by any tool (Nextcloud, iCloud Drive, Google Drive, OneDrive, Dropbox,
  Syncthing …) **or not synced at all**.
- The phone is only a camera; there is no phone app.

Non-goals (for now): mobile apps, family members, filing claims electronically, tax
calculations, cloud OCR or AI.

Expected volume: 20–150 invoices per year.

## 2. Requirements

| # | Requirement |
|---|---|
| R1 | Capture invoices from scans, photos and PDFs; prefill from the GiroCode (EPC QR code). |
| R2 | Per invoice and payer: SOLL computed (overridable), IST entered, everything compared. |
| R3 | Status per payer as checkboxes that record the date: eingereicht, erstattet, Klärungsbedarf (with note); invoice paid to the provider. |
| R4 | Strict separation by year. |
| R5 | Year dashboard: what the PKV would have to pay vs. the expected Beitragsrückerstattung (BRE) if nothing is claimed; the same figures for Beihilfe without that comparison; overview of all years. |
| R6 | Refund documents (Beihilfebescheid, PKV-Leistungsabrechnung) on paper or as PDF: capture them and match their lines to invoices by amount and date, with review. |
| R7 | Several Beihilfestellen and PKVs over time; rules differ per Land → configurable, nothing hard-coded. |
| R8 | Import of the existing spreadsheet. |

## 3. Domain rules

| Rule | In the app |
|---|---|
| Beihilfe rate at the date of treatment: "Maßgeblich ist der Bemessungssatz im Zeitpunkt der Leistungserbringung" (§ 46 Abs. 1 BBhV) | Coverage periods with a start date; SOLL uses the period of the treatment date |
| Bund: claim within 3 years after the invoice date (§ 54 BBhV, since 2026; before: 1 year). Länder differ | Per payer: deadline in years (0 = none); warning 90 days ahead |
| Bund: a claim must exceed 200 € (§ 51 Abs. 8 BBhV), the office may waive it | Per payer: minimum amount (0 = none); dashboard shows the unclaimed sum |
| Kostendämpfungspauschale in some Länder (e.g. Saarland), abolished in NRW since 2022 | Per payer and year: annual deduction, shown on the dashboard, not per invoice |
| PKV premium refund requires a claim-free treatment year; the treatment date counts | Leistungsjahr = year of the (first) treatment date, overridable. Per PKV and year: expected BRE (entered by the user) and deductible |
| Some PKV claims do not cost the BRE (e.g. check-ups, tariff-dependent) | Flag per claim |
| Payers round their share; refunds may exceed the invoice by a cent per payer | No warning for that |
| Copies and electronic documents are accepted (§ 51 Abs. 3 BBhV) | PDF bundle export (M6) |

The tax effect of the BRE and BRE tiers are out of scope (user decision).

## 4. Decisions

| Topic | Decision | Reason |
|---|---|---|
| Stack | Go + Wails v3 + React 18/TypeScript (Vite, Vitest) | Same as crypto-tax: proven toolchain; one binary per OS on the system webview (WebView2, WKWebView, WebKitGTK) |
| Logic | All calculations in Go (`calc`, `match`); the UI only displays | One source of truth, testable without UI |
| Money | `money.Cents` (int64) and `money.Rate` (basis points); JSON `"286.40"` and `70` | Exact and readable; no float rounding |
| Dates | `civil.Date`, JSON `"2026-03-14"` | No time zones |
| Storage | One JSON file per record in year folders of a user-chosen data folder | Works with every sync tool or none; a conflict only affects one record; readable |
| Sync | None built in. Per-leaf clocks; conflict copies are found by record ID (independent of the sync tool's naming) and merged leaf by leaf, newer value wins | No accounts, keys or server |
| No SQLite in the data folder | | Sync tools can corrupt it |
| OCR | Tesseract.js (WebAssembly, German model bundled) in the webview; PDF text with pdf.js | Identical on all three OS, offline, nothing to install (crypto-tax's Tesseract CLI is practical on Linux only) |
| GiroCode | Decoded from the image or PDF on the desktop | No live scan on the phone needed |
| Phone | (a) inbox folder `Eingang/` in the data folder; (b) QR code → upload page in the home network | (a) needs no code on the phone; (b) works without any cloud |
| UI language | German (confirmed); code and docs English | Domain is German only |
| Private data | Never in the repo; tests use synthetic data | As in crypto-tax |
| Distribution | GitHub Actions builds Linux (RPM, AppImage), Windows (installer), macOS (DMG, ad-hoc signed, not notarised) | No cost; one-time OS warnings accepted |

## 5. Data folder

```
<data folder>/                     chosen on first start; anywhere
  einstellungen.json               payers, coverage periods, year settings, aliases
  Eingang/                         new scans/PDFs from phone, mail or scanner – picked up by the app
  belege/<sha256>.<ext>            documents, named by content (never change → never conflict)
  2026/
    rechnungen/<id>.json           one invoice
    erstattungen/<id>.json         one refund document with its lines
    einreichungen/<id>.json        one submission (which invoices, when, to whom)
    leistungen/<id>.json           benefits without an invoice (Krankenhaustagegeld)
```

Device-local settings (path of the data folder, device ID, window state) live in the OS
config folder, not in the data folder.

Record file:

```json
{
  "schema": 1,
  "type": "invoice",
  "data": { "id": "20261102-3fa9c1d2", "provider": "Dr. Muster", "amount": "286.40", "…": "…" },
  "clock": { "/amount": "2026-11-02T10:15:00.120Z@a1b2c3", "/claims/bh/submitted/date": "…" }
}
```

- Folder year: Leistungsjahr (invoices), document date (refund documents, submissions), date
  (benefits). Changing the year moves the file.
- Writes are atomic (hidden temp file + rename).
- On save, every changed leaf gets a new stamp (monotonic per record, device ID breaks ties).
  If the file changed on disk since it was read, the two versions are merged instead of
  overwritten.
- On load, files with the same record ID (conflict copies of any sync tool, or a record in
  two year folders) are merged leaf by leaf, written to the canonical path, and the copies are
  removed; the user sees a note.
- Arrays are leaves as a whole. Deleting = `deleted: true`.
- Users should set the data folder to "always keep on this device" (no placeholder files).

## 6. Data model (`internal/model`)

Glossary: Payer = Kostenträger · CoveragePeriod = Versicherungsschutz · Invoice = Rechnung ·
Claim = Anspruch je Kostenträger · Mark = Häkchen mit Datum · Refund = Erstattung (IST) ·
RefundDoc = Erstattungsbeleg · Submission = Einreichung · Allowance = Leistung ohne Rechnung ·
ServiceYear = Leistungsjahr · PremiumRefund = Beitragsrückerstattung.

- **Payer** {id, kind beihilfe|pkv, name, claimDeadlineYears, minClaimAmount}
- **CoveragePeriod** {from, shares [{payerId, rate}]} – valid until the next period
- **YearSetting** {year, payerId, deductible, annualDeduction, expectedPremiumRefund}
- **Invoice** {id, provider, payee, iban, reference, category, invoiceDate, serviceFrom,
  serviceTo, serviceYear?, amount, due, paid: Mark, claims: {payerId: Claim}, attachments, note}
- **Claim** {eligible?, rate?, expected?, submitted, refunded, clarify: Mark, held, skip,
  keepsPremiumRefund, submissionId, refunds [Refund]}
- **Mark** {date, note}: ticking sets today; the date stays editable
- **Refund** {id, amount, date, docId?, lineId?}
- **RefundDoc** {id, payerId, date, applicationDate, payoutDate, paidOut, deductions,
  lines [RefundLine], attachment, state inbox|confirmed}
- **RefundLine** {id, date, provider, billed, eligible, refund, invoiceIds, match, page, box}
- **Submission** {id, payerId, date, invoiceIds, channel}
- **Allowance** {id, payerId, kind, date, amount, received}

## 7. Calculations (`internal/calc`, pure)

- Treatment date = serviceFrom, else invoiceDate. Leistungsjahr = override or its year.
- Claims exist for every payer of the coverage at the treatment date, plus any payer that has
  claim data.
- SOLL = expected override, else (eligible override or amount) × (rate override or share).
- IST = Σ refunds. Diff = IST − SOLL once refunded.
- Status precedence: skipped > refunded > submitted > held > open. Klärungsbedarf is a separate
  flag; it is suggested when refunded and |Diff| > tolerance (default 1 €).
- Deadline: invoiceDate + n years for open/held claims.
- Per Leistungsjahr and payer: SOLL, IST, buckets (refunded, submitted, open, held, skipped),
  deviation, Klärungsbedarf, deadlines, unclaimed invoice amounts vs. minimum, annual deduction,
  Krankenhaustagegeld.
- PKV vs. BRE per year: claim everything = max(0, SOLL − deductible); hold = max(0, SOLL of
  BRE-neutral claims − deductible) + expected BRE; advantage = difference; "forfeited" once a
  relevant claim is submitted or refunded.
- Totals over all years.
- Plausibility warnings: refunds above the amount, dates before the invoice, implausible years
  (typos), treatment after the invoice date, refund before submission.

## 8. Capture (M4)

- The app watches `Eingang/` and also accepts drag & drop, the file picker and the phone
  upload. Each file → text (pdf.js, or Tesseract.js for images and scanned PDFs) → type
  (invoice or refund document) → draft in the in-app inbox → user confirms → the file moves to
  `belege/`.
- Phone (a): iPhone Files app "Dokumente scannen", the Nextcloud app's scanner, or any scanner
  app → save or share into `Eingang/` of the synced folder.
- Phone (b): "Mit Handy fotografieren" starts a local HTTP server only while the dialog is
  open and shows a QR code with a one-time token. The phone's browser opens a page, takes
  photos (`<input type=file capture>`) and uploads them; optionally encrypted with a key that
  only exists in the URL fragment. Works without any cloud. Cropping/deskewing on the desktop.
- Invoice parser (rules, German): invoice date, number, treatment period, total
  ("Rechnungsbetrag", "Gesamtbetrag", "Endbetrag", "zu zahlen"), IBAN, provider, category (GOÄ,
  GOZ, PZN, Heilmittel). The GiroCode fills payee, IBAN, amount and reference. Always a
  confirmation form; every field stays editable.

## 9. Refund documents and matching (`internal/match`, M5)

- Extraction: header (payer, document date, "Antrag vom", payout), lines (date, billed,
  eligible, refund), deductions; rows rebuilt from text positions.
- Layout profile per payer: the user marks the columns once on the first document.
- Checksum: Σ refunds − Σ deductions = payout, otherwise the gap is highlighted.
- Score per line and invoice: exact amount (50), date = invoice/treatment date or within the
  period (30), ±3 days (10), provider similarity or alias (≤ 15), claim submitted and open
  (10), refund ≈ share × billed (5); already refunded −30, skipped −20; refund above the
  invoice excludes the pair.
- Groups first (several lines = one invoice, one line = several invoices; exact sums of ≤ 4
  items), only for items without an exact amount partner; then 1:1 assignment maximising the
  total score (Hungarian algorithm).
- "auto" only if amount and date match exactly and the runner-up is ≥ 20 points behind;
  groups are always suggestions. Option: review everything.
- On confirm: refund per invoice, tick erstattet, backfill eingereicht from "Antrag vom", link
  the document, suggest Klärungsbedarf on deviations, learn aliases.

## 10. UI (M2+)

- **Rechnungen**: table per year with checkboxes per payer; filters (open, submitted,
  Klärungsbedarf, deadline); bulk "eingereicht heute" creates a submission.
- **Rechnung**: detail form with SOLL/IST per payer, documents, refunds.
- **Dashboard** per year; **Übersicht** of all years.
- **Posteingang**: drafts from scans and refund documents with review.
- **Einstellungen**: payers, coverage periods, year settings, data folder, aliases.

## 11. Spreadsheet import (`internal/xlsximport`)

Columns are recognised by header text (typos tolerated): Rechnungsdatum, Behandlungsdatum,
Anweisung am (paid), Behandler, Bemerkung (several), Gesamtbetrag, Eingereicht/Erstattet
Beihilfe, Eingereicht/Erstattet Krankenkasse, Krankenhaus Tagegeld. Computed columns and the
yearly summary blocks are ignored; dates come from raw cell values. Each row may produce
warnings (dates before the invoice, implausible years, refunds above the amount). The UI shows
a preview before saving (M2).

## 12. Build and packaging

- `make dev|build|install|check|demo`; `wails3 task build` builds for the current OS.
- CI: Go tests and a full app build on Linux, Windows and macOS; gofmt, vet, race tests,
  golangci-lint, govulncheck and frontend checks on Linux; release workflow for all three OS in M6.
- macOS: ad-hoc signed; first start via System Settings → Privacy & Security → "Open Anyway".
  Windows: SmartScreen "More info → Run anyway".

## 13. Privacy

Health data: no telemetry, no network access except the local upload page while it is open,
OCR on the device, nothing leaves the data folder. Real data never goes into the repo.

## 14. Milestones

| | Content | Status |
|---|---|---|
| M1 | Core: money/date types, model, calc (SOLL/IST, years, PKV vs. BRE, deadlines, warnings), matching, store (file per record, atomic writes, per-leaf merge, conflict copies), spreadsheet import; tests with synthetic data; CI on three OS | done |
| M2 | Desktop app: Wails + React, first start (data folder, payers, coverage), invoice list with checkboxes, detail form, dashboard, overview, spreadsheet import with preview → replaces the spreadsheet | done |
| M3 | Live data folder: file watcher, reload and merge while running, backups (zip snapshots to a second location, important without cloud) | done |
| M4 | Capture: inbox (`Eingang/`, drag & drop), Tesseract.js, pdf.js, GiroCode, invoice parser, phone upload via QR code | done |
| M5 | Refund documents: extraction, layout profiles, checksum, matching review, aliases | |
| M6 | Submissions and PDF bundle, exports (Excel/CSV/PDF), reminders (deadlines, due dates), release builds for three OS | |

## 15. Open points

- App name (working title: pkv-beihilfe-tracker; window title "PKV Beihilfe Tracker").

## 16. Changes during implementation

- **M1:** Groups (several lines = one invoice) are matched *before* the 1:1 assignment, otherwise
  a weak date-only pair would take the invoice first. Refunds may exceed the invoice by one cent
  per payer (each payer rounds its share up) without a warning.
- **M2:** The service layer (`internal/service`) speaks DTOs with amounts as strings; input
  accepts the German format. Every change is written immediately (no "save all").
- Ticking "erstattet" in the list asks for the amount, prefilled with the open SOLL;
  unticking removes refunds entered by hand (refunds from documents stay).
- "Eigenanteil" on the dashboard is shown as "noch nicht erstattet" (it includes claims held
  back or still open); the final own share counts settled invoices only.
- Charts follow the dataviz method: thin segmented bar per payer (SOLL by status, figures in
  the table below), horizontal stacked bars in the overview; colours validated for light and
  dark mode.
- UI checks run headless: Wails server mode (`-tags server`) plus Playwright/Firefox for
  screenshots; `cmd/demodata` creates a demo data folder.
- Own app icon (`build/appicon.svg`); the template's macOS vector icon was removed so the
  `.icns` is used.
- The linter's ST1005 rule is off for `internal/service` and `internal/backup`: their errors are
  German UI texts.
- **M3:** `internal/watch` (fsnotify) watches the data folder, its year and record folders, adds
  new folders, debounces bursts (0.8 s) and ignores the app's own writes (the store remembers size
  and mtime of what it wrote). After a reload the frontend gets a `data-changed` event: lists and
  dashboard refresh, a toast tells the user.
- An open invoice form keeps its original; saving sends both and the service applies only the
  fields edited in the form to the current version (`leaf.Rebase`, three-way). The form shows a
  notice when its invoice was changed elsewhere.
- `internal/backup`: zip snapshots (without hidden files) to a device-local backup folder, once a
  day on start and on demand, the newest 20 are kept; a folder inside the data folder is refused.
  Restore = unzip into an empty folder and choose it as data folder.
- **M4:** Recognition runs in the webview: pdf.js for the text layer (pages are rendered for
  GiroCodes; OCR when a page has no text), Tesseract.js 7 with the German "best_int" model,
  zxing-wasm for QR codes. All assets are copied into the app (`scripts/copy-assets.mjs`,
  ~13 MB) – nothing is loaded from a CDN. The text goes to Go (`internal/extract`), which decides
  invoice vs. refund document and fills the fields; every field carries its source
  (GiroCode / label / guess) for the UI.
- For billing services the letterhead names the service; the practitioner is taken from
  "Behandlung durch …", the service becomes the payee.
- The webview gets inbox files and documents through an asset middleware
  (`/datafile/inbox/…`, `/datafile/doc/…`).
- Phone upload: `internal/upload` serves an embedded page on the LAN with a random token; files
  arrive in `Eingang/` and reach the UI through the folder watcher (inbox files count as
  changes; the toast says "Neuer Beleg im Posteingang").
- Verified end to end (server mode + headless Firefox): a made-up invoice photo sent through
  the phone page was recognised in about 6 s with all fields correct and saved with the photo
  in `belege/`.
