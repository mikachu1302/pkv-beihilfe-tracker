# pkv-beihilfe-tracker

Desktop app (Windows, Linux, macOS) for privately insured people – with or without
Beihilfe – to keep track of medical invoices: what was submitted to the Beihilfestelle and
the PKV, what came back, what is still open, and whether claiming from the PKV pays off
compared with the premium refund (Beitragsrückerstattung).

- **SOLL/IST per invoice and payer.** SOLL follows the share (50/50, 70/30 or 100 % PKV) at the
  date of treatment.
- **Status checkboxes with dates:** eingereicht, erstattet, Klärungsbedarf, bezahlt.
- **Yearly dashboard and overview of all years.**
- **Refund documents** (Beihilfebescheid, PKV-Leistungsabrechnung) are matched to invoices by
  amount and date.
- **Data stays yours:** one JSON file per record in a folder of your choice, synced by any tool
  or not at all. There is no server and no account, and nothing runs in the cloud.

Status: the core (calculations, matching, storage, spreadsheet import) is done; the desktop
UI comes next. See [docs/PLAN.md](docs/PLAN.md).

## Development

Requirements: Go 1.26.

```sh
make test     # unit tests
make check    # gofmt, vet, tests with -race, golangci-lint (if installed)
```

Try the spreadsheet import on a real file without saving anything:

```sh
PKV_XLSX=/path/to/Abrechnung.xlsx go test ./internal/xlsximport -run TestImportFile -v
```

| Package | Purpose |
|---|---|
| `internal/money` | Euro amounts as cents, percentages as basis points |
| `internal/civil` | Calendar dates |
| `internal/model` | Records: payers, coverage, invoices, claims, refund documents |
| `internal/calc` | SOLL/IST, statuses, deadlines, yearly figures, PKV vs. premium refund |
| `internal/match` | Matching refund document lines to invoices |
| `internal/store` | Data folder: file per record, atomic writes, merging conflict copies |
| `internal/xlsximport` | Import of the old spreadsheet |

Personal data never goes into this repository; tests use made-up values only.
