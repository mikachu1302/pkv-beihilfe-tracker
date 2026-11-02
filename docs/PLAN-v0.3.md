# v0.3.0 – Views and themes (plan)

Status: planned · 2026-11-02 · mockups: the design canvas "PKV Beihilfe – Designrichtungen"
(private claude.ai artifact of the owner; section "v0.3.0 · Drei Ansichten, hell und dunkel").

## Goal

The app looks plain. v0.3.0 gives it a distinct look (direction "A · Nachtschicht", adapted to
light mode) and three ways to look at the invoices – without losing any data in any view.

## Decisions (2026-11-02)

| Topic | Decision |
|---|---|
| Look | Direction A: one accent colour (lime `#C8F542` as fill; dark olive `#3F6212` for text on light), numbers in a monospace face, calm surfaces. Fonts bundled (no CDN): Space Grotesk (UI) and JetBrains Mono (numbers), both OFL. |
| Views | Detail, Kompakt, Tabelle – the same data in all three. |
| Default view | Configurable per device; out of the box: **Detail**. Switching in the list applies until the next start (or until changed in the settings). |
| Table | **Read only**: sorting, choosing columns, sum row. Editing stays in the invoice form (double click / Enter opens it). |
| Themes | System / Hell / Dunkel, per device, in the settings and as a quick toggle. |
| Documents in the form | The invoice form gets "Beleg hinzufügen" (click or drop) for new and existing invoices; for a new invoice the document is recognised and fills **empty fields only**. The inbox stays for everything that arrives on its own (phone, `Eingang/` folder, Bescheide). |
| Version | All of this is v0.3.0; v0.2.0 (prescriptions, re-import, shortcuts …) ships first. |

## Views

**Detail (default)** – one card per invoice:
- left: treatment and invoice date, provider, category, invoice number, amount, paid / due;
- one column per payer: status pill (offen, eingereicht, erstattet, zurückgehalten,
  Klärung), SOLL, IST, difference, dates of eingereicht/erstattet;
- footer (only when there is something): note, documents, hints, linked Bescheid.
- The checkboxes of today's list stay available in the card (tick eingereicht/erstattet
  without opening the form).

**Kompakt** – one row per invoice: date, provider + category, amount, per payer three status
dots plus a short text ("eingereicht 20.09.", "erstattet −39,21 €"), open amount, flags (not
paid, waiting long, Klärung, note). A row expands (click on the chevron or →) to show all
details of the Detail card; Enter opens the invoice.

**Tabelle** – all fields as columns: Behandlung, Rechnung, Behandler, Kategorie, Re.-Nr.,
Betrag, Bezahlt, per payer SOLL / IST / Diff / eingereicht / erstattet, Notiz. Group headers per
payer, sticky header and first column, sortable by any column (`aria-sort`), column chooser,
dense/normal rows, sum row (amount, SOLL, IST, diff per payer), horizontal scrolling inside the
table only. Double click / Enter opens the invoice.

Common to all three: the toolbar (year, search, filters, Ausstand per payer), keyboard
navigation (↑/↓ moves the selection, Enter opens), bulk selection (eingereicht for several),
the same filters and search, the same sort order unless the table is sorted otherwise.

## Documents in the invoice form

Today the form can only open documents; adding one, or prefilling a new invoice from a photo,
needs the detour through the inbox. v0.3.0:

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

## Settings · Darstellung

- Standardansicht: Detail (default) / Kompakt / Tabelle.
- Farbschema: wie das System (default) / hell / dunkel.
- Tabelle: sum row, dense rows, visible columns (also from the column chooser).
- Stored in the device config (`internal/config`), not in the data folder – two devices can
  look different.

## Implementation steps (each one shippable)

1. **Tokens and themes**: CSS custom properties for both themes (surfaces, text, muted,
   border, accent fill/text, payer colours, status colours) validated for contrast ≥ 4.5:1;
   theme switch (system/light/dark) in config + settings + quick toggle; bundle the two fonts.
   Restyle the shell (menu, header, buttons, chips, cards) to the new look.
2. **View switch + Detail view** (default) replacing today's table; keep all functions of the
   current list (ticks, popovers for refund amount and Klärung, bulk bar).
3. **Tabelle**: read-only grid with sorting, column chooser, sum row, sticky header/column.
4. **Kompakt** with expandable rows.
5. **Settings · Darstellung** with the default view and table options; remember the last
   quick choice for the session only.
6. **Documents in the invoice form**: "Beleg hinzufügen" with recognition for new invoices
   (fills empty fields only), attaching to existing invoices, removing attachments.
7. Tests: unit tests per view (same data, sorting, column chooser, keyboard), theme contrast
   check, headless screenshots in both themes; docs (README, PLAN).

## Open points

- Should the invoice detail page also get the "Reise einer Rechnung" layout (direction D)?
- Status dots need a text alternative in every place (screen readers, colour blindness).
- Performance with several hundred invoices in the table (virtualise only if needed).
