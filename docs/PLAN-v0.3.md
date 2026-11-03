# v0.3.0 – Documents in the invoice form, multi-page documents (plan)

Status: implemented (steps 1–7), reviewed and fixed · plan 2026-11-03

The redesign (new look, views Detail/Kompakt/Tabelle, switch between classic and new design)
follows in v0.4.0 – see [PLAN-v0.4.md](PLAN-v0.4.md).

## Goal

Two gaps in the daily work with documents:

- The invoice form can only open documents. Adding one, or prefilling a new invoice from a
  photo, needs the detour through the inbox.
- Every image is its own inbox entry: a three-page invoice photographed as three pictures
  becomes three drafts (and three invoices if all are saved).

## Decisions

| Topic | Decision |
|---|---|
| Documents in the form | The invoice form gets "Beleg hinzufügen" (click or drop) for new and existing invoices; for a new invoice the document is recognised and fills **empty fields only**. The inbox stays for everything that arrives on its own (phone, `Eingang/` folder, Bescheide). |
| Multi-page documents | Several photos can become one document (pages in order): merged by hand in the inbox (always possible), suggested by the app from the content – not the arrival time – (never merged without confirmation), as a "mehrseitiger Beleg" from the phone page, and as several pages in "Beleg hinzufügen". Recognition reads all pages together; invoices and Bescheide alike. |
| Look | Today's look stays. New UI parts (drop zone, page list, merge bar) use only the existing CSS tokens and shared classes, so they work unchanged in both designs of v0.4.0. |
| Version | Both topics are v0.3.0 (decided 2026-11-03); the redesign moves to v0.4.0. |

## Current state (analysis 2026-11-03)

- **Inbox** (`store.ListInbox`, `service/inbox.go`, `pages/Inbox.tsx`): every visible *file* in
  `Eingang/` is one entry; folders are skipped. The watcher (`internal/watch`) watches `Eingang/`
  itself but not folders inside it. `CreateInvoiceFromInbox(name, d)` moves one file to
  `belege/<sha256>.<ext>` (refused if another invoice already has that file) and links it;
  `ConfirmRefund(inboxName, …)` does the same for a Bescheid (`RefundDoc.Attachment`, one file).
- **Recognition** runs in the webview (`lib/capture.ts`): `recognize(url, type)` reads a PDF
  (text layer, OCR for scanned pages, at most 4 pages) or one image (OCR, rotation probe) and
  returns lines with a `page` number; `AnalyzeDocument` / `AnalyzeRefund` interpret them.
  `extract.Analyze` already works on several pages: date from the first page, the strongest
  total label wins (ties: the later line), the provider from the first 25 lines. Results are not
  cached – selecting an entry again recognises it again.
- **Invoice form** (`pages/InvoiceForm.tsx`): attachments are only listed as "Beleg n öffnen"
  buttons (opened with the default app). `SaveInvoice(d, original)` takes `d.attachments` as
  they are and rebases on changes from other devices.
- **Phone page** (`internal/upload`): every photo/PDF is sent on its own (parallel `fetch`) and
  written as `Eingang/Handy-<timestamp>-<n>.<ext>`.
- **Submission PDF** (`export.WriteBundle`): takes all attachments of an invoice in order –
  multi-page invoices need no change there.
- **Device settings** (`internal/config`): data folder, device ID, backup folder. Dark mode
  follows the system only (`prefers-color-scheme` in `styles.css`).

## Design

### 1. Multi-page inbox entries = folders in `Eingang/`

A multi-page document waiting in the inbox is a **folder inside `Eingang/`**; its files are the
pages, ordered by a number prefix: `Eingang/<name>/01 <file>`, `02 <file>`, …

- Why a folder: each page stays a file of its own (sync tools handle them one by one), the
  grouping needs no extra metadata file that two devices could change at the same time, the
  phone page can write into it directly, and users can build a multi-page document by hand in
  the file manager or the Nextcloud app (folder with the pages in name order).
- Rejected: a JSON file with the groups (conflicts, hidden files that some sync tools skip);
  merging the photos into one PDF (originals lost, needs conversion).
- Folder name: the name of the first page without extension, made unique (`Scan 12`,
  `Scan 12 (2)`). Pages are renamed `NN <original name>` (an existing `NN ` prefix is
  replaced); renumbering renames through hidden temporary names, so two pages never collide.
- Only one level: deeper folders are ignored, hidden files and temp files are skipped, an
  empty folder is not shown and is removed when the app takes out its last page.
- Older app versions (v0.2.0) on other devices do not show folders in the inbox – harmless,
  the files are kept.

**Store** (`internal/store`): `ListInbox` returns entries
`InboxEntry{Name, Group bool, Pages []InboxFile, Modified}` (single file = one page; page
names relative to `Eingang/`, e.g. `Scan 12/01 Scan 12.jpg`). `InboxPath` accepts `file` and
`folder/file`. New: `MergeInbox(names) (string, error)` (entries in the given order; a group
absorbs the others), `ReorderInbox(group, pages)` (must be a permutation), `SplitInbox(group,
page)` (one page back to `Eingang/`, or all with `page == ""`; a group left with one page is
dissolved), `RemoveFromInbox` also removes a group. Own changes are noted for the watcher.

**Watcher**: also watches the folders inside `Eingang/`; a new folder there is added and its
files noted; files in such a folder are relevant. The data-changed event gets
`InboxPages` (new pages in an existing entry) next to `InboxAdded`, so the toast says
"Neue Seite im Posteingang" instead of the generic text.

### 2. Recognition over several pages

- `capture.ts`: `recognizePages(pages[])` recognises the files one after another and numbers
  the pages through (page 1 of the second file becomes page 3 after a two-page PDF); QR codes of
  all pages, `ocr` if any page needed OCR. Limit **12 pages** per document (all files
  together); more pages → note "nur die ersten 12 Seiten gelesen".
- A **cache** per file (URL + modified + size → result) shared by the inbox detail, the
  background recognition for suggestions (section 4) and the form: switching between entries
  no longer recognises them again. Shared work is not cancelled when one consumer goes away
  (it only stops waiting); entries of files that left the inbox are dropped.
- `extract`: multi-page tests with synthetic layouts (header on page 1, items on page 2, total
  on page 3; "Zwischensumme"/"Übertrag" on page 1 must not beat "Rechnungsbetrag" on page 3;
  "Seite 2 von 3" lines are never taken as provider). Refund analysis over two pages likewise.

### 3. Inbox UI

- **List**: a checkbox per entry; with two or more ticked a bar shows
  "Zu einem Beleg zusammenfassen (n)". Page order = order in the list; it can be changed
  afterwards. Groups show "n Seiten".
- **Detail of a group**: the preview shows all pages one below the other ("Seite 1", …; of a
  PDF inside a group its first page plus "PDF, n Seiten"). A page strip with thumbnails:
  move left/right (buttons, keyboard-operable; drag & drop in addition), "Seite herauslösen",
  "Alle Seiten trennen". Recognition runs again when the pages change; fields the user already
  edited stay (as today with `touched`).
- **Saving**: `CreateInvoiceFromInbox` with a group attaches all pages in order (duplicate
  check per page: a page that already belongs to another invoice is named in the error), then
  removes the folder. `ConfirmRefund` with a group: see section 6. Discarding asks
  "Beleg mit n Seiten löschen?".

### 4. Suggestions (content only)

- While the inbox page is open, the entries not yet recognised are recognised in the
  background, one after another (the selected entry first; the background pauses while the
  selected entry is being recognised; at most 30 entries).
- Backend `SuggestInboxGroups(texts)` (`texts`: name + lines + QR per entry) returns
  suggestions `{names in page order, reason}`. Rules, from strong to weak:
  1. **Page numbers**: "Seite 2 von 3", "Seite 2/3", "Blatt 2 von 3", "S. 2/3" (OCR-tolerant).
     Entries with the same page total and different page numbers form a group in page order –
     unless their invoice numbers, IBANs or providers contradict each other. If two entries
     claim the same page and nothing tells them apart, no suggestion (ambiguous).
  2. **Same invoice number** (at least 4 characters) on several entries; order by page numbers,
     else the page with the letterhead/date first and the page with the labelled total last,
     else the inbox order.
  3. **Continuation page**: an entry without letterhead, date and labelled total, and exactly
     one other entry of the same provider or IBAN – only when unambiguous.
  A group can absorb further entries. Entries of a different kind (invoice vs. Bescheid) are
  not combined.
- Display: a notice above the list per suggestion, e.g. "„Scan 12“ und „Scan 13“ gehören wohl
  zusammen (Seite 1 und 2 von 2). [Zusammenfassen] [Gehören nicht zusammen]". Merging needs the
  click; "Gehören nicht zusammen" is remembered on this device (per set of files).

### 5. "Beleg hinzufügen" in the invoice form

- **Staging outside the data folder**: added files are first copied into a temporary folder of
  the running app (served to the webview under `/datafile/staged/<id>`), not into `belege/`.
  Cancelling the form leaves nothing behind, and no orphaned files end up in the synced folder.
  The folder is removed at shutdown; leftovers of a crashed session at the next start.
  Backend: `StageFiles(paths)` (from the file dialog) and `StageFile(name, data)` (drag & drop,
  base64 as `AddToInbox`) → `StagedDocDTO{id, name, type, url, size}`; PDF and images only,
  40 MB each.
- **Saving**: `SaveInvoice(d, original, staged []string)`: after the rebase the staged files are
  stored in `belege/` and appended to the attachments (attachments another device added
  meanwhile stay). The same file as another invoice's document is refused (as in the inbox);
  the same file twice on this invoice is skipped.
- **Removing** an attachment (✕ at the page) takes effect on saving. A file no other record uses
  any more (invoices incl. deleted ones, Bescheide) goes **back into the inbox** instead of being
  deleted – nothing is lost, and a document attached to the wrong invoice can be assigned anew
  from there (decided 2026-11-03). A file still used elsewhere is only unlinked.
- **UI**: a drop zone / button "Beleg hinzufügen …" in the "Belege" area of the form (always
  visible). Pages as small tiles (thumbnail, "Seite n", open, ✕); new pages marked
  "wird beim Speichern abgelegt"; several files at once = pages of one document in the chosen
  order. "Alle Seiten öffnen" merges the pages into one temporary PDF (`export.MergeDocuments`,
  taken out of `WriteBundle`) and opens it. Staged and removed pages count as unsaved changes.
- **Recognition (new invoice only)**: after adding, all new pages are recognised together
  (`recognizePages` → `AnalyzeDocument`); only fields that are still empty are filled
  (provider, amount, invoice date, treatment from/to, category, invoice number, payee, IBAN,
  due date; "bezahlt" for prescriptions). Every filled field shows its source badge until the
  user edits it; recognition notes are shown. Adding more pages later fills what is still empty.
- **Bescheid recognised**: the form says "Das ist wohl ein Erstattungsbescheid – Bescheide werden
  im Posteingang gebucht" with "In den Posteingang verschieben" (`StagedToInbox(ids)`, several
  pages as one group) and switches to the inbox.
- **Existing invoice**: pages are only attached, nothing is recognised or overwritten.

### 6. Bescheide over several pages

- Model: `RefundDoc.Attachment` stays the first page (so older app versions still find the
  document); new `RefundDoc.MorePages []string` for pages 2…n. No schema bump: a Bescheid record
  is written once when it is booked and never saved again by the app; only merging sync
  conflict copies of the same Bescheid in an old version could drop the field (the files stay in
  `belege/`). Documented at the field.
- `ConfirmRefund` with a group: all pages into `belege/`, `Attachment` + `MorePages`; a
  repeated booking is recognised by the first page as today; the folder is removed afterwards.
- The review reads the lines of all pages (same `recognizePages`).
- "aus Bescheid – öffnen" opens one file directly, several pages as merged temporary PDF.

### 7. Phone page: "Mehrseitiger Beleg"

- A switch on the page: **Einzelne Belege** (as today) / **Mehrseitiger Beleg**. In the
  multi-page mode every photo ("Seite aufnehmen") or chosen file becomes the next page; a strip
  shows the pages taken; "Fertig – nächster Beleg" closes the document.
- Pages are sent at once (nothing is lost if the phone browser closes), but one after another,
  with a header `X-Beleg: <random id per document>`. The server creates
  `Eingang/Handy-<timestamp>/` at the first page and writes `NN Seite.<ext>` (temp file +
  rename, as today); limits per connection unchanged; the id is checked (`[a-z0-9]{8,32}`).
- On the computer the entry appears with the first page and grows; the recognition runs again
  when pages arrive.

### 8. Theme switch (decided 2026-11-03: in v0.3.0)

`Config.Theme` (`""` = system, `light`, `dark`), `data-theme` on the root element, the dark
token blocks also under `:root[data-theme="dark"]` and the media query only for
`:root:not([data-theme="light"])`; Settings → "Darstellung" with three options; a copy in
`localStorage` avoids a flash at start.

## Implementation steps

Each step ends with `make check` green and a commit; pushed when a step is complete.

1. **Shared base** – `capture.ts`: `recognizePages`, cache, 12-page limit;
   `export.MergeDocuments` + `OpenDocuments(names)` (temporary PDF); extract tests for
   multi-page invoices and Bescheide.
2. **Form: "Beleg hinzufügen"** – staging (`StageFiles`, `StageFile`, `StagedToInbox`,
   cleanup), `SaveInvoice(…, staged)`, removed attachments back to the inbox, form UI (tiles,
   drop zone, open all), recognition that fills empty fields only, Bescheid redirect. Tests:
   Go (staging limits/types, duplicate refused, same file skipped, foreign attachments kept,
   unused file back to the inbox, used file only unlinked); frontend (typed fields stay,
   sources shown, staged pages make the form dirty, Bescheid notice).
3. **Inbox groups, backend** – store (`ListInbox` entries, `MergeInbox`, `ReorderInbox`,
   `SplitInbox`, group removal, name handling), watcher for sub folders, service DTOs and
   methods, `CreateInvoiceFromInbox` and `ConfirmRefund` for groups (`MorePages`), event
   `InboxPages`. Tests: store (order, renumbering without collisions, split/dissolve, hidden and
   deep files ignored), watcher (page added in a group), service (pages attached in order,
   duplicate page named, folder removed, Bescheid with `MorePages`).
4. **Inbox groups, UI** – checkboxes + merge bar, group preview, page strip (move, take out,
   split all, drag & drop), discard group, toast for new pages. Tests: merge/reorder/split call
   the backend with the right order; group save.
5. **Suggestions** – `extract.PageMarker`, `SuggestInboxGroups` (rules 1–3), background
   recognition, notices with merge / dismiss. Tests: page numbers, same invoice number,
   ambiguous → none, contradicting numbers → none, continuation page, kinds not mixed.
6. **Phone page** – multi-page mode (`page.html`), `X-Beleg` handling in `upload.go`. Tests:
   pages of one id land in one folder in order, invalid id refused, single mode unchanged.
7. **Theme switch** (section 8).
8. **Finish** – review by sub-agents (Go and frontend, as for v0.2.0) and fixes; manual test in
   the dev app with synthetic documents (real documents of the owner only locally, never in the
   repo); docs: README, `docs/PLAN.md`, `docs/INSTALLATION.md` (phone page); release notes.
   The tag v0.3.0 only after the owner's OK.

## Risks

- **Background OCR** costs CPU while the inbox is open (a photo takes a few seconds): one at a
  time, only while the page is open, at most 30 entries, the selected entry has priority.
- **Renaming in the synced folder** while another device works on the same inbox: the duplicate
  check on saving prevents counting a document twice; at worst a page shows up as its own entry
  again.
- **`SaveInvoice` gets a third parameter**: regenerated bindings; a running `make dev` must be
  restarted afterwards (known gotcha: delete `frontend/node_modules/.vite`).

## Open points

- Should a saved multi-page invoice be splittable again (pages moved to another invoice)?
  For now: remove a page in the form → it goes back to the inbox → attach it elsewhere.
