# v0.3.0 – Documents in the invoice form, multi-page documents (plan)

Status: planned · 2026-11-03

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
| Multi-page documents | Several photos can become one document (pages in order): merged by hand in the inbox, suggested by the app (never merged without confirmation), as a "mehrseitiger Beleg" from the phone page, and as several pages in "Beleg hinzufügen". Recognition reads all pages together; invoices and Bescheide alike. |
| Look | Today's look stays. New UI parts (drop zone, page list, merge dialog) use only the existing CSS tokens and shared classes, so they work unchanged in both designs of v0.4.0. |
| Version | Both topics are v0.3.0 (decided 2026-11-03); the redesign moves to v0.4.0. |

## Documents in the invoice form

- A drop zone / button "Beleg hinzufügen" at the top of the form (new and existing invoices);
  PDF and images, same size limit as the inbox (40 MB); several documents per invoice
  (e.g. a second page).
- New invoice: the document runs through the same recognition as the inbox (`capture.ts` →
  `AnalyzeDocument`) and fills only fields that are still empty; every filled field shows its
  source (GiroCode / erkannt / geschätzt) as in the inbox. Fields the user typed stay.
  Prescriptions also set "bezahlt". A refund document (Bescheid) is not booked from here: the
  form says so and offers to open it in the inbox review instead.
- Existing invoice: the document is only attached (no prefilling, nothing overwritten).
- Backend: a method to attach a document to an invoice (`AddDocument` + `Attachments`), with
  the duplicate check of the inbox (the same file as another invoice's document is refused) and
  only PDF/image types; saving a new invoice attaches the documents added in the form.
- Removing an attachment from an invoice (the file in `belege/` stays as long as another record
  uses it).
- The inbox keeps its role: files that arrive without the user (phone, cloud folder) and refund
  documents that are spread over several invoices.

## Multi-page documents

A multi-page PDF already works as one document (up to 4 pages are read). v0.3.0:

- **Model**: a document consists of one or more files (pages, in order). An invoice already holds
  several attachments; a grouped inbox entry keeps its pages until it is saved, then all pages are
  attached to the one invoice (and go into the submission PDF in that order). Raise the page limit
  for PDFs (e.g. 12 pages).
- **Merge by hand in the inbox**: select entries → "Zu einem Beleg zusammenfassen", reorder the
  pages by dragging, split again. Recognition runs over all pages as one text (page numbers kept):
  header from the first page, total and IBAN usually from the last.
- **Suggestions**: the app proposes "Diese 3 Fotos gehören wohl zusammen – zusammenfassen?" when
  pages say "Seite 1/3" / "Seite 2 von 3", share an invoice number or provider without an own total,
  or arrived from the phone within a short time; merging always needs a click.
- **Phone page**: a mode "Mehrseitiger Beleg" – take the pages one after another, then "Fertig";
  the upload is marked as one group and lands in the inbox as one entry.
- **Invoice form**: "Beleg hinzufügen" accepts several files at once as pages of one document.
- **Refund documents** (Bescheide are often several pages) use the same grouping; their review
  reads the lines of all pages.
- Storage: pages stay separate files in `belege/` (content-named as today); the grouping lives in
  the record (order of the attachments), so syncing stays per file and conflict-free.

## Implementation steps (each one shippable)

1. **Attachments backend**: `AddDocument` / attach to an invoice with duplicate and type checks,
   removing an attachment, keeping files that other records still use.
2. **"Beleg hinzufügen" in the form**: attach to existing invoices; for new invoices recognition
   that fills empty fields only, with source markers; Bescheide redirected to the inbox.
3. **Multi-page model**: inbox entries with ordered pages, recognition over all pages, page
   limit for PDFs raised; pages attached in order and in the submission PDF.
4. **Merge by hand in the inbox**: select → merge, reorder pages, split again.
5. **Suggestions** for pages that belong together (page numbers, invoice number, provider
   without total, arrival time from the phone).
6. **Phone page**: "Mehrseitiger Beleg" mode; several pages in "Beleg hinzufügen".
7. **Bescheide over several pages** in the refund review.
8. *Optional, small*: theme switch System/Hell/Dunkel (today the app follows the system only)
   for the current look; otherwise it comes with v0.4.0.
9. Tests (Go: attachments, grouping, recognition over pages with synthetic layouts; frontend:
   form prefill keeps typed fields, merge/split/reorder), docs (README, PLAN, INSTALLATION if
   the phone page changes).

## Open points

- How long may the gap between phone uploads be to suggest a group (e.g. 2 minutes)?
- Should a merged inbox entry be splittable again after saving (i.e. on the invoice)?
