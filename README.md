# pkv-beihilfe-tracker

Desktop app (Windows, Linux, macOS) for privately insured people – with or without
Beihilfe – to keep track of medical invoices: what was submitted to the Beihilfestelle and
the PKV, what came back, what is still open, and whether claiming from the PKV pays off
compared with the premium refund (Beitragsrückerstattung). The user interface is German.

- **Invoice list** with checkboxes per payer (eingereicht, erstattet, Klärungsbedarf, bezahlt)
  that record the date; bulk "eingereicht" for several invoices.
- **SOLL/IST per invoice and payer**; SOLL follows the share (50/50, 70/30 or 100 % PKV) at the
  date of treatment.
- **Dashboard per year** (Leistungsjahr = year of treatment), comparison "claim from the PKV
  vs. keep the premium refund", overview of all years.
- **Capture**: scans, photos and PDFs land in the inbox – from the phone (QR code, no app
  needed), by drag & drop or through the `Eingang/` folder; text recognition and the GiroCode
  pre-fill the invoice (everything runs on the device).
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

```sh
make check    # gofmt, vet, Go tests with -race, frontend typecheck + tests, golangci-lint (if installed)
```

Try the spreadsheet import on a real file without saving anything:

```sh
PKV_XLSX=/path/to/Abrechnung.xlsx go test ./internal/xlsximport -run TestImportFile -v
```

To look at the UI in a browser (e.g. for headless screenshots), build the Wails server mode:
`go build -tags server,gtk3 -o /tmp/pkv-server .`, then run it with
`WAILS_SERVER_PORT=18466 XDG_CONFIG_HOME=/tmp/pkv-config /tmp/pkv-server` and open
`http://localhost:18466`. Native dialogs are not available there; enter paths by hand.

| Package | Purpose |
|---|---|
| `internal/money` | Euro amounts as cents, percentages as basis points |
| `internal/civil` | Calendar dates |
| `internal/model` | Records: payers, coverage, invoices, claims, refund documents |
| `internal/calc` | SOLL/IST, statuses, deadlines, yearly figures, PKV vs. premium refund |
| `internal/match` | Matching refund document lines to invoices |
| `internal/store` | Data folder: file per record, atomic writes, merging conflict copies |
| `internal/xlsximport` | Import of the old spreadsheet |
| `internal/extract` | Recognises invoices and refund documents from text and GiroCodes |
| `internal/upload` | Upload page for the phone (LAN, one-time link) |
| `internal/export` | Excel export and PDF bundle for submissions |
| `internal/leaf` | JSON leaves: flatten, rebuild, three-way rebase |
| `internal/watch` | Notices changes in the data folder made by others |
| `internal/backup` | Zip snapshots of the data folder |
| `internal/config` | Device-local settings (data folder, device ID, backup folder) |
| `internal/service` | Methods bound to the frontend (Wails), German messages |
| `frontend/` | React + TypeScript UI (Vite, Vitest) |
| `cmd/demodata` | Generator for a demo data folder |

Personal data never goes into this repository; tests and demo data use made-up values only.
