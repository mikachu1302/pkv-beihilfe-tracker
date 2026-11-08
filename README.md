# pkv-beihilfe-tracker

Desktop app (Windows, Linux, macOS) for privately insured people – with or without
Beihilfe – to keep track of medical invoices: what was submitted to the Beihilfestelle and
the PKV, what came back, what is still open, and whether claiming from the PKV pays off
compared with the premium refund (Beitragsrückerstattung). The user interface is German.

- **Invoice list** with checkboxes per payer (eingereicht, erstattet, Klärungsbedarf, bezahlt)
  that record the date; bulk "eingereicht" for several invoices. Four views: **Detail** (one card
  per invoice with SOLL, IST and dates per payer), **Kompakt** (one line per invoice with status
  dots, expandable), **Tabelle** (every field as a column like a spreadsheet: sortable, column
  chooser, sum row – read only) and the classic list. Long lists stay fast: the three new views
  render only the visible invoices.
- **SOLL/IST per invoice and payer**; SOLL follows the share (50/50, 70/30 or 100 % PKV) at the
  date of treatment.
- **Dashboard per year** (Leistungsjahr = year of treatment), comparison "claim from the PKV
  vs. keep the premium refund", overview of all years.
- **Capture**: scans, photos and PDFs land in the inbox – from the phone (QR code, no app
  needed), by drag & drop or through the `Eingang/` folder; text recognition and the GiroCode
  pre-fill the invoice (everything runs on the device). Documents can also be added right in the
  invoice form, where they fill the fields that are still empty.
- **Multi-page documents**: several photos become one document – merged in the inbox (the app
  suggests pages that belong together, from their content), photographed as "Mehrseitiger Beleg"
  on the phone, or added together in the form; recognition reads all pages.
- **Refund documents** (Beihilfebescheid, PKV-Leistungsabrechnung) are read the same way; their
  lines are matched to invoices by amount, date and name, checked against the payout, and booked
  as refunds after a review.
- **Exports**: Excel workbook per year or for all years; one PDF per submission with a cover page
  and all documents (for the Beihilfe app or a portal).
- **Zu erledigen**: deadlines, payments due, refunds taking long, Klärungsbedarf, the premium
  refund decision.
- **Import** of the old spreadsheet with preview and duplicate check.
- **Data stays yours**: one JSON file per record in a folder of your choice, synced by any
  tool (Nextcloud, iCloud Drive, Google Drive, OneDrive, Dropbox, Syncthing …) or not at all.
  There is no server and no account, and nothing runs in the cloud. Changes from other devices
  show up while the app runs; conflict copies are merged field by field.
- **Backups**: daily zip snapshots to a second folder (USB stick, NAS, another cloud).
- **Optional local AI** (off by default, only on a click): a model on a computer in the home
  network (llama.cpp, OpenAI-compatible) reads difficult documents – dark or skewed photos, long
  tables – next to the text recognition ("Mit KI auslesen"), and answers questions about the own
  data ("Fragen": *Wie viele Rechnungen aus 2026 sind noch offen?*). The AI only translates the
  question; the app computes the answer with the same figures as the list and the dashboard.
- **Two designs**, chosen per device: the new one (default – calm surfaces, one accent colour,
  amounts in a monospace face, an icon rail) and the classic look of v0.3 with the classic list;
  each **light or dark**, or following the system.

Status: all planned milestones (M1–M6) are done. Releases: push a tag `v1.2.3` – GitHub Actions
builds the Windows installer, the macOS app and Linux packages. See [docs/PLAN.md](docs/PLAN.md).

## Installation

Installation guide (German): **[docs/INSTALLATION.md](docs/INSTALLATION.md)** – packages for Linux
(`.rpm`, `.deb`, `.tar.gz` with `install.sh`), macOS (`.dmg`, universal) and Windows (installer)
from the GitHub releases, an install script for Linux and macOS, and building from source.

```sh
sh scripts/install.sh   # Linux/macOS: latest release via the GitHub CLI, no root needed
make install            # from source: Linux ~/.local, macOS ~/Applications
```

## Build and run

Requirements: Go 1.26, Node 22, the Wails CLI
(`go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0`, on Linux with `-tags gtk3`). On Linux also the GTK3 and
WebKitGTK 4.1 development packages (Fedora: `gtk3-devel webkit2gtk4.1-devel`, Debian/Ubuntu:
`libgtk-3-dev libwebkit2gtk-4.1-dev`).

```sh
make dev                       # run with hot reload
make build                     # bin/pkv-beihilfe-tracker for this OS
make install                   # Linux: ~/.local with menu entry · macOS: ~/Applications
make dist-linux                # dist/…-linux-amd64.tar.gz (binary, icon, menu entry, install.sh)
make demo DIR=/tmp/pkv-demo    # a data folder with made-up invoices to try the app
```

On first start the app asks for the data folder and how you are insured.

## Development

### Architecture

```
frontend/ (React, in the system webview)          Go (one binary)
  pages, components                                main.go          Wails app, window, asset middleware
  lib/useBackend.ts ── generated bindings ──────▶  internal/service bound methods, DTOs, German messages
  lib/capture.ts: pdf.js, Tesseract.js, zxing      desktop.go       native dialogs, open files
        │ recognised text + QR payloads ────────▶  internal/extract → invoice / refund draft
        ◀── /datafile/inbox/…, /datafile/doc/… ──  service.ServeFile (inbox files, documents)
        ◀── event "data-changed" ────────────────  internal/watch → reload when others change the folder
  lib/aiRead.ts: pages as JPEG ─────────────────▶  internal/airead ─▶ internal/ai ─▶ AI server in the LAN
  pages/Ask.tsx: question ──────────────────────▶  internal/ask: question → query (AI), query → answer
                                                   internal/calc, match (pure logic)
                                                   internal/store ─▶ data folder (JSON per record)
```

- All calculations happen in Go; the UI only displays and edits. The service keeps the data
  folder in memory and writes every change through the store at once (no "save all").
- Text recognition runs in the webview (WebAssembly, offline). Only the recognised lines go to
  Go, which decides what the document is and fills the fields.
- The data folder layout, record format and merge rules are described in
  [docs/PLAN.md §5](docs/PLAN.md#5-data-folder); what changed during implementation in §16.

### Workflow

```sh
make check           # what CI runs: gofmt, vet, Go tests with -race, bindings, frontend
                     # typecheck + tests; golangci-lint, shellcheck, govulncheck if installed
make bindings        # regenerate frontend/bindings after changing exported service methods
cd frontend && npm ci && npx vitest run    # frontend tests only
```

Generated and copied files are not in the repository: `frontend/bindings/` (TypeScript
bindings of the Go service, `make bindings`), `frontend/public/ocr/` and `frontend/public/pdfjs/`
(Tesseract, zxing and pdf.js assets, copied from `node_modules` by
`frontend/scripts/copy-assets.mjs` before `npm run dev`/`npm run build`, or with
`npm run assets`) and `frontend/dist/`.
Go tests that need no UI run without them: `go test ./internal/...`.

The AI features are tested against a stand-in server (`internal/ai/aitest`). Against the real
server, the question set (`internal/ask/testdata/fragen.json`, made up) shows how many questions
the model and the app's corrections understand right; documents are measured with
`scripts/ki-belegtest.py`, made-up test documents (invoices with positions, Bescheide, a cost
plan, a prescription) come from `scripts/ki-testbelege.py` (see [docs/KI-TEST.md](docs/KI-TEST.md)):

```sh
KI_TEST=1 go test ./internal/ask -run Live -v    # key from LLM_API_KEY or API_KEY in .env
```

Try the spreadsheet import on a real file without saving anything:

```sh
PKV_XLSX=/path/to/Abrechnung.xlsx go test ./internal/xlsximport -run TestImportFile -v
```

To look at the UI in a browser (e.g. for headless screenshots), use the Wails server mode. It
embeds the built frontend, so build that first:

```sh
make build                                  # or: make bindings && (cd frontend && npm ci && npm run build)
go build -tags server,gtk3 -o /tmp/pkv-server .   # gtk3 only on Linux (still needed to compile)
WAILS_SERVER_PORT=18466 XDG_CONFIG_HOME=/tmp/pkv-config /tmp/pkv-server   # → http://localhost:18466
```

Native dialogs are not available there; enter paths by hand. `make demo DIR=/tmp/pkv-demo`
creates a data folder with made-up invoices to point it at.

### Code map

| Package | Purpose |
|---|---|
| `main.go` | Wails application: window, bound services, `/datafile/` middleware, events |
| `desktop.go` | Native file dialogs and opening files with the default app |
| `internal/money` | Euro amounts as cents, percentages as basis points |
| `internal/civil` | Calendar dates |
| `internal/model` | Records: payers, coverage, invoices, claims, refund documents |
| `internal/calc` | SOLL/IST, statuses, deadlines, yearly figures, PKV vs. premium refund |
| `internal/match` | Matching refund document lines to invoices |
| `internal/store` | Data folder: file per record, atomic writes, merging conflict copies |
| `internal/leaf` | JSON leaves: flatten, rebuild, three-way rebase |
| `internal/watch` | Notices changes in the data folder made by others |
| `internal/backup` | Zip snapshots of the data folder |
| `internal/config` | Device-local settings (data folder, device ID, backup folder, appearance, AI server) |
| `internal/extract` | Recognises invoices and refund documents from text and GiroCodes |
| `internal/ai` | Client for the local AI server (OpenAI-compatible, one request at a time); `aitest`: a stand-in server for tests |
| `internal/airead` | Reading a document with the AI: checks the answer, merges it with the text recognition |
| `internal/ask` | Questions: the query model, running a query on the data, prompt and corrections for the AI |
| `internal/upload` | Upload page for the phone (LAN only, secret link valid 15 minutes) |
| `internal/export` | Excel export and PDF bundle for submissions |
| `internal/xlsximport` | Import of the old spreadsheet |
| `internal/service` | Methods bound to the frontend (Wails), German messages |
| `frontend/` | React + TypeScript UI (Vite, Vitest) |
| `cmd/demodata` | Generator for a demo data folder |

## Privacy and security

- Health data never leaves the device through the app unless you ask for it: no telemetry, no
  cloud services, text recognition offline. Sync, if any, is done by the tool you choose for the
  data folder.
- **The optional AI** is a second network path, off by default and only to a server you name
  (a computer in the home network – no cloud). It sends something only on a click:
  - "Mit KI auslesen" sends the pages of that one document (as JPEG images – health data). Its
    recognised text stays on the device; the app uses it to check the AI's answer before a
    field is filled.
  - "Fragen" sends the question, today's date and the app's vocabulary (payer names, categories,
    statuses) – never an invoice. The app computes the answer from its own data.
  - Only a model that reads images is used: the app refuses one the server lists as text-only
    (`architecture.input_modalities`), for documents and questions alike. On the home server
    the text-only model searches the web, so health data must never reach it
    ([docs/KI-TEST.md](docs/KI-TEST.md)).
  - The connection is plain HTTP inside the home network, protected by an API key. The key is
    kept in the device configuration (`config.json` in the OS config folder, readable by the user
    only), never in the data folder, and never shown again after it was entered.
- The phone upload is the only network listener. It runs only while its dialog is open (at most
  15 minutes), listens on the LAN address shown in the QR code, requires the random token in
  the link, accepts only images and PDFs (checked by content) and at most 100 files / 500 MB.
  It uses plain HTTP inside the home network – anyone who can read your Wi-Fi traffic could
  read the uploads; use it on your own (WPA2/WPA3) network, or use the `Eingang/` folder.
- Only PDFs and images are kept as documents, so nothing executable can be opened from the app.
- Personal data never goes into this repository; tests and demo data use made-up values only.
