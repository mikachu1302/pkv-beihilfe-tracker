# pkv-beihilfe-tracker – Technical Plan

Status: M1–M6 implemented, reviewed · 2026-11-02 – section 16 lists what changed during
implementation; where it differs from sections 1–15, section 16 is what the code does.

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
| Distribution | GitHub Actions builds Linux (DEB, RPM, tar.gz with `install.sh`), Windows (NSIS installer), macOS (universal app as DMG and zip, ad-hoc signed, not notarised); see `docs/INSTALLATION.md` | No cost; one-time OS warnings accepted |

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

Device-local settings (path of the data folder, device ID, backup folder) live in the OS
config folder (`internal/config`), not in the data folder.

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
- Files that cannot be read, or that come from a newer app version (`schema` > current), are
  reported and never overwritten: saving such a record fails until the file is fixed, and
  conflict copies of it are left alone.
- An older app version drops fields it does not know when it saves a record. A new field
  therefore needs a schema bump (`store.SchemaVersion`), which makes older versions leave
  such files alone.
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
  open (at most 15 minutes) and shows a QR code with a secret link (random token; usable for
  several photos until it expires). The phone's browser opens a page, takes photos
  (`<input type=file capture>`) and uploads them. Works without any cloud. Plain HTTP inside
  the home network (a self-signed certificate would scare the user with a browser warning);
  the planned extra encryption with a key in the URL fragment and cropping/deskewing on the
  desktop were not built (section 15).
- Invoice parser (rules, German): invoice date, number, treatment period, total
  ("Rechnungsbetrag", "Gesamtbetrag", "Endbetrag", "zu zahlen"), IBAN, provider, category (GOÄ,
  GOZ, PZN, Heilmittel). The GiroCode fills payee, IBAN, amount and reference. Always a
  confirmation form; every field stays editable.

## 9. Refund documents and matching (`internal/match`, M5)

- Extraction: header (payer, document date, "Antrag vom", payout), lines (date, billed,
  eligible, refund), deductions; rows rebuilt from text positions.
- Layout profile per payer: which amount of a line means billed, eligible and refund
  (section 16, M5), confirmed once and remembered.
- Checksum: Σ refunds − Σ deductions = payout, otherwise the gap is highlighted.
- Score per line and invoice: exact amount (50) or amount off by one digit (20), date =
  invoice/treatment date or within the period (30), ±3 days (10), provider similarity or alias
  (≤ 15), claim submitted and open (10), refund ≈ share × billed (5); already refunded −30,
  skipped −20; refund above the invoice excludes the pair.
- Groups first (several lines = one invoice, one line = several invoices; exact sums of ≤ 4
  items), only for items without an exact amount partner, only for lines with a date and
  invoices whose claim is still open, and only in pools of at most 30 items (the number of
  subsets grows with the fourth power); then 1:1 assignment maximising the total score
  (Hungarian algorithm).
- "auto" only if amount and date match exactly and the runner-up is ≥ 20 points behind;
  groups are always suggestions. Nothing is booked before the user confirms the whole
  document in the review, so a separate "review everything" option was not needed.
- An alias name may stand for several providers (a billing service for several doctors).
- On confirm: refund per invoice, tick erstattet, backfill eingereicht from "Antrag vom", link
  the document, suggest Klärungsbedarf on deviations, learn aliases.

## 10. UI (M2+)

- **Rechnungen**: table per year or all years with checkboxes per payer; filters (open,
  submitted, Klärungsbedarf, deadline, unpaid, with hints); bulk "eingereicht heute" creates a
  submission. Above the table one card per payer shows the Ausstand (SOLL − IST of claims
  submitted or still to be submitted; held-back and skipped claims are not owed). A row opens
  the invoice when clicked anywhere outside its controls (also ✎ next to the name); notes show
  as a "Notiz" badge with the text on hover.
- **Rechnung**: detail form with SOLL/IST per payer, documents, refunds.
- **Dashboard** per year; **Übersicht** of all years.
- **Posteingang**: drafts from scans and refund documents with review.
- **Einstellungen**: payers, coverage periods, year settings, data folder, backups. Aliases
  and column layouts are learned from confirmed refund documents and kept, but there is no
  screen to edit them yet (section 15).

## 11. Spreadsheet import (`internal/xlsximport`)

Columns are recognised by header text (typos tolerated): Rechnungsdatum, Behandlungsdatum,
Anweisung am / Überwiesen (am) / Bezahlt (paid to the provider; not when the header names
Beihilfe or the PKV), Behandler, Bemerkung (several), Gesamtbetrag, Eingereicht/Erstattet
Beihilfe, Eingereicht/Erstattet Krankenkasse, Krankenhaus Tagegeld. Every sheet with such a
table is read (one sheet per year works). Computed columns and summary blocks next to the
table are ignored, and so are rows with neither a date nor a provider (totals below the
table); a refund of 0 counts as empty. Dates come from raw cell values. Each row may produce
warnings (dates before the invoice, implausible years, refunds above the amount). The UI
shows a preview before saving (M2); the records keep their IDs from the preview, so saving
again after an error does not duplicate them. Cells that cannot be read are kept in the
invoice note ("In Excel nicht lesbar: …"); the other findings are plausibility rules the app
checks all the time (section 7), shown as ⚠ in the list (filter "Mit Hinweisen") and in the
invoice form until they are fixed.

## 12. Build and packaging

- `make dev|build|install|check|demo`; `wails3 task build` builds for the current OS.
- CI: Go tests and a full app build on Linux, Windows and macOS; gofmt, vet, race tests,
  golangci-lint, govulncheck and frontend checks on Linux; release workflow for all three OS in M6.
- macOS: ad-hoc signed; first start via System Settings → Privacy & Security → "Open Anyway".
  Windows: SmartScreen "More info → Run anyway".

## 13. Privacy

Health data: no telemetry, no network access except the local upload page while it is open
(bound to the LAN address in the QR code, limited to 100 files / 500 MB, images and PDFs
checked by content), OCR on the device, nothing leaves the data folder. Only PDFs and images
are stored as documents (they are opened with the system's default app). Real data never goes
into the repo.

## 14. Milestones

| | Content | Status |
|---|---|---|
| M1 | Core: money/date types, model, calc (SOLL/IST, years, PKV vs. BRE, deadlines, warnings), matching, store (file per record, atomic writes, per-leaf merge, conflict copies), spreadsheet import; tests with synthetic data; CI on three OS | done |
| M2 | Desktop app: Wails + React, first start (data folder, payers, coverage), invoice list with checkboxes, detail form, dashboard, overview, spreadsheet import with preview → replaces the spreadsheet | done |
| M3 | Live data folder: file watcher, reload and merge while running, backups (zip snapshots to a second location, important without cloud) | done |
| M4 | Capture: inbox (`Eingang/`, drag & drop), Tesseract.js, pdf.js, GiroCode, invoice parser, phone upload via QR code | done |
| M5 | Refund documents: extraction, layout profiles, checksum, matching review, aliases | done |
| M6 | Submissions and PDF bundle, exports (Excel/CSV/PDF), reminders (deadlines, due dates), release builds for three OS | done (Excel instead of CSV) |

## 15. Open points

- App name (working title: pkv-beihilfe-tracker; window title "PKV Beihilfe Tracker").
- A screen to view and delete learned aliases and column layouts.
- Phone upload: optional end-to-end encryption (key in the URL fragment); cropping and
  deskewing photos on the desktop.
- HEIC photos (iPhone) are stored and recognised, but cannot go into the PDF bundle (pdfcpu
  cannot convert them); they are listed as missing there.

## 16. Changes during implementation

- **M1:** Groups (several lines = one invoice) are matched *before* the 1:1 assignment, otherwise
  a weak date-only pair would take the invoice first. Refunds may exceed the invoice by one cent
  per payer (each payer rounds its share up) without a warning.
- **M2:** The service layer (`internal/service`) speaks DTOs with amounts as strings; input
  accepts the German format. Every change is written immediately (no "save all").
- Ticking "erstattet" in the list asks for the amount, prefilled with the open SOLL;
  unticking removes refunds entered by hand (refunds from documents stay). Ticking it again
  when nothing is open only sets the tick and adds no second refund.
- "Eigenanteil" on the dashboard is shown as "noch nicht erstattet" (it includes claims held
  back or still open); the final own share counts settled invoices only.
- Charts follow the dataviz method: thin segmented bar per payer (SOLL by status, figures in
  the table below), horizontal stacked bars in the overview; colours validated for light and
  dark mode.
- UI checks run headless: Wails server mode (`-tags server`) plus Playwright/Firefox for
  screenshots; `cmd/demodata` creates a demo data folder.
- Own app icon (`build/appicon.svg`); the template's macOS vector icon was removed so the
  `.icns` is used.
- The linter's ST1005 rule is off for `internal/service`, `internal/backup` and
  `internal/upload`: their errors are German UI texts.
- **M3:** `internal/watch` (fsnotify) watches the data folder, `Eingang/`, the year and record
  folders – nothing else (on macOS every watched folder costs a file descriptor per file) –,
  adds new folders, treats a removed or renamed year folder as a change, debounces bursts
  (0.8 s) and ignores the app's own writes (the store remembers size and mtime of what it
  wrote). If merging conflict copies fails during a reload (a file locked by another program),
  the merged state is used in memory and the merge is tried again next time. After a reload the frontend gets a `data-changed` event: lists and
  dashboard refresh, a toast tells the user.
- An open invoice form keeps its original; saving sends both and the service applies only the
  fields edited in the form to the current version (`leaf.Rebase`, three-way). The form shows a
  notice when its invoice was changed elsewhere.
- `internal/backup`: zip snapshots (without hidden files) to a device-local backup folder, once a
  day on start and on demand, the newest 20 are kept; a folder inside the data folder is refused
  (symbolic links resolved). Snapshots run without holding the service lock, so the app stays
  usable; files that disappear meanwhile are skipped. Restore = unzip into an empty folder and
  choose it as data folder.
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
  changes; the toast says "Neuer Beleg im Posteingang"). It stops when the data folder changes.
- Creating an invoice from an inbox file that is already the document of another invoice is
  refused (same content = same SHA-256 name); the inbox file may then be discarded.
- Verified end to end (server mode + headless Firefox): a made-up invoice photo sent through
  the phone page was recognised in about 6 s with all fields correct and saved with the photo
  in `belege/`.
- **M5:** `extract.AnalyzeRefund` reads refund documents generically: a line with a date and
  amounts is a position (percentages ignored, summary and deduction lines recognised); header
  dates ("Bescheid vom", "Antrag vom") and the payout by labels. Instead of marking columns on
  the page, the layout profile is "which amount of a line means what" (first/second/last),
  guessed from the number of amounts and remembered per payer once confirmed.
- Document kind by points: refund indicators ("Bescheid", "Antrag vom", "Auszahlungsbetrag",
  "Beihilfe" …) against invoice indicators ("Rechnung", "GOÄ", "bitte überweisen" …); the user can
  switch between both views in the inbox.
- The review (`ReviewRefund`) keeps lines the user assigned by hand ("confirmed") and amounts
  edited by hand; the 1:1 matching runs only over the rest.
- Booking: one refund per line and invoice (a line covering several invoices is split by their
  amounts), linked to the stored document (doc/line ID), "erstattet" with the payout date or the
  notice date, "eingereicht" backfilled from "Antrag vom", aliases learned from names that
  differ from the invoice (e.g. billing services). Every line gets a unique ID; the amounts are
  recomputed from the layout when confirming.
- Confirming the same document file again (a retry, or the Bescheid scanned twice) re-uses its
  record and replaces the earlier booking instead of counting the refunds twice. Once the
  refunds are booked, failures to save learned aliases or to remove the inbox file are reported
  as warnings, not errors, so that nobody retries a booking that already happened.
- Recognition labels match at the start of a word ("Datum" but not "Geburtsdatum"); a position
  line that mentions "Kürzung" stays a position; "GebüH" (Heilpraktiker) is not confused with the
  "Gebührenordnung für Ärzte".
- Photos taken sideways: when Tesseract's confidence is below 65, the image is also read turned
  by 90°, 270° and 180°, and the best reading is used.
- Prescriptions (Privatrezept with the pharmacy imprint) are recognised by the IK number
  ("+1234567+"), PZN lines (PZN, quantity, price in cents) and the date as DDMMYY: the printed
  total (else the sum of the prices), the date, the pharmacy's short name ("Linden-Apo" →
  "Linden-Apotheke"), category Apotheke, and "bezahlt" on that date (paid at the counter).
- Provider names are cleaned of OCR junk ("SYNLAB\/" → "SYNLAB"); label lines ("Ärztliche
  Leitung:") are skipped; a name with a legal form (GmbH, eG, e. K. …) wins, also inside a long
  sender line. An invoice number label without a number ("Rechnungsnummer | Datum" table header)
  takes the number from the line below.
- The menu can be collapsed to icons (remembered per device), so wide tables fit.
- Keyboard shortcuts (Cmd on macOS, Ctrl elsewhere; list with "?"): save (S) in the invoice
  form, inbox and settings, save & next (Enter), new invoice (N), search (F or /), menu entries
  (1–7), back with Backspace outside fields or Alt+← (⌘[ on macOS), forward. Leaving a form with
  unsaved changes asks first.
- Verified end to end: a made-up Beihilfebescheid photo for three demo invoices was recognised,
  all three lines matched "sicher", checksum correct, refunds booked with the notice linked.
- **M6:** Excel export (`internal/export`): sheets Rechnungen (SOLL/IST, status and dates per
  payer), Übersicht (years and total), Leistungen ohne Rechnung; for one year or all. CSV was
  dropped – Excel/LibreOffice cover it.
- PDF bundle for a submission: cover page (gofpdf) listing the invoices and their sum, then the
  documents (PDFs as they are, photos as pages via pdfcpu); invoices without a document are named.
- "Zu erledigen" on the dashboard: claim deadlines, payments due, refunds open for more than six
  weeks, Klärungsbedarf and deviations, the premium refund decision (January–June for the
  previous year), documents in the inbox; urgent items first. No system notifications.
- Release workflow (`.github/workflows/release.yml`): a tag `v*` builds a Windows installer
  (NSIS), a macOS universal app (DMG and zip, ad-hoc signed) and Linux DEB/RPM/tar.gz and
  attaches them to a GitHub release (tags with a suffix such as `-beta.1` become pre-releases);
  started by hand it only uploads artifacts. The version comes from the tag (`APP_VERSION`);
  Linux is built on Ubuntu 22.04 so the binary runs on glibc ≥ 2.35. The Linux packages install
  to `/usr/bin`, depend on GTK3 and WebKitGTK 4.1 and use the SVG icon.
- Claim deadlines from 29 February end on 28 February in non-leap years (§ 188 Abs. 3 BGB).
