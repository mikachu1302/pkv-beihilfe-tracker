# v0.4.0 – Redesign, views and themes (plan)

Status: planned, analysed · 2026-11-03 · mockups: the design canvas "PKV Beihilfe –
Designrichtungen" (private claude.ai artifact of the owner; board "A · Nachtschicht" and section
"v0.3.0 · Drei Ansichten, hell und dunkel" – the mockups were made when the redesign was still
planned for v0.3.0).

v0.3.0 is released (documents in the invoice form, multi-page documents, theme switch – see
[PLAN-v0.3.md](PLAN-v0.3.md)).

## Goal

The app looks plain. v0.4.0 gives it a distinct look (direction "A · Nachtschicht", adapted to
light mode) and three ways to look at the invoices – without losing any data in any view. The
current look stays available: everyone can choose between the classic and the new design and
switch at any time.

## Decisions

| Topic | Decision |
|---|---|
| Look | Direction A: one accent colour (lime `#C8F542` as fill; dark olive `#3F6212` for text on light), numbers in a monospace face, calm surfaces. Fonts bundled (no CDN): Space Grotesk (UI) and JetBrains Mono (numbers), both OFL. |
| Design switch | "Design: Klassisch / Neu" per device; **Neu is the default**, also for existing installations after the update, with a one-time hint where to switch back (decided 2026-11-03). Both designs share the **same structure** (menu, pages, forms) and differ only in look (colours, fonts, spacing, radii, density). No second set of screens: every feature is built once and shows up in both designs. One exception: the new invoice views exist only in the new design (see Views). "Klassisch" is today's look, not a frozen copy of v0.2.0. |
| Views | **New design**: Detail, Kompakt, Tabelle – the same data in all three – plus today's list as "Klassische Liste", with a view switch. **Classic design**: only today's list, no view switch (decided 2026-11-03: the classic design does not need the new views). So the new views are styled for the new design only. |
| Default view | New design: configurable per device, out of the box **Detail**. Switching in the list applies until the next start (or until changed in the settings). Classic design: always today's list. |
| Invoice page | No "Reise einer Rechnung" layout (direction D) – decided 2026-11-03. |
| Table | **Read only**: sorting, choosing columns, sum row. Editing stays in the invoice form (double click / Enter opens it). |
| Themes | Shipped with v0.3.0: switch "Dunkel" in the menu, Hell / Dunkel / Automatisch in the settings, per device. Both designs get a light and a dark variant. |
| Dashboard | **Same structure as today, new look only** (decided 2026-11-03, option a). The dashboard of mockup A (big "noch offen" number, "Weg des Geldes" bar, invoices with status dots, "Zu erledigen" beside them) is a different layout; it may follow later as its own step, then for both designs. |
| Version | Redesign, views and the design switch are v0.4.0 (decided 2026-11-03; originally planned for v0.3.0). |

## Design switch

Today all colours are CSS custom properties in `frontend/src/styles.css` (about 420 lines; dark
tokens under `:root[data-theme="dark"]` with a `prefers-color-scheme` fallback for the first
paint); components contain no hard-coded colours. That makes the switch cheap:

- `styles.css` is split into a shared part (layout, structure, states) and one token set per
  design (colours, fonts, spacing, radii, density), each with a light and a dark variant.
- The chosen design is an attribute on the root element (`data-design="classic" | "new"`), next
  to the colour scheme (`data-theme`); switching needs no restart.
- Where the new design needs different styling beyond tokens (e.g. cards, status pills), the
  rules are scoped to `[data-design="new"]`; the classic rules stay as they are.
- Stored in the device config (`internal/config`) like the other appearance settings.
- Four combinations to check: classic/new × light/dark (contrast ≥ 4.5:1 for every text and
  background pair, headless screenshots of the main pages).
- Rule for all future work: new UI uses only tokens and shared classes, so it works in both
  designs without extra effort.

## Analysis: what changes, and is it CSS only? (2026-11-03)

Compared: the mockups (A, Detail, Kompakt, Tabelle, Darstellung; light and dark) with headless
screenshots of today's pages (demo data) and the code.

Result: the look can be done in CSS almost everywhere. New structure comes from the new invoice
views (planned as a feature anyway, in the new design only); everything else keeps its markup.
The top of the invoice list is already built like the Detail mockup (title, year, search, Excel,
"Neue Rechnung", Ausstand cards per payer, filter chips) – it only changes its look.

| Area | Change | How |
|---|---|---|
| Colours | All colours are tokens already; no component has a fixed colour. | CSS only |
| Fonts, radii, spacing, cards, buttons, chips, badges/pills, tables, dialogs, toasts | New style for the shared classes (`panel`, `badge`, `chips`, `num`, `page-head`, `grid`, `field` …); fonts bundled (~200 KB, `font-src 'self'`). | CSS only |
| Menu | Mockup: a 72 px icon rail with a logo tile "P·B". Today's collapsed menu is that rail; the new design starts collapsed (labels as tooltips). Data folder and version move out of sight in the rail (they are in the settings). | CSS + a small markup addition (logo tile) |
| Pages without a mockup (Dashboard, Posteingang, Rechnungsformular, Alle Jahre, Import, Einstellungen, Einrichtung) | New look through the same classes, layout unchanged. | CSS only |
| Spacing in the code | About 60 spacing values are inline styles (`marginTop: 8`, `gap: 24` …) that a design cannot override. They become classes / spacing tokens – mechanical, for both designs. | small code change |
| Amounts in the monospace face | Mostly marked with `num` already; some large figures (cards) and amounts inside text need a marker. | small code change |
| View switch, Detail cards, Kompakt rows (expandable, status dots), Tabelle (sorting, column chooser, sum row, sticky header/column) | New components; today's table stays as "Klassische Liste". | new structure, new design only |
| Settings · Darstellung | Design, default view, table options (the colour scheme is there since v0.3.0). | new structure, small |
| Colour scheme control in the list header (mockup) | Dropped – the menu switch "Dunkel" does this. | – |

Effort: the design switch, the new token sets and the CSS for the existing pages are roughly a
third of the work; the views are the larger part, because each must keep everything today's list
can do (ticks, popovers for refund amount and Klärung, bulk "eingereicht", keyboard).

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

**Klassische Liste** – today's invoice table with all its functions (ticks, popovers, bulk bar).

Common to all views: the toolbar (year, search, filters, Ausstand per payer), keyboard
navigation (↑/↓ moves the selection, Enter opens), bulk selection (eingereicht for several),
the same filters and search, the same sort order unless the table is sorted otherwise.

## Settings · Darstellung

- Design: Neu (default) / Klassisch.
- Standardansicht (shown with the new design only): Detail (default) / Kompakt / Tabelle /
  Klassische Liste.
- Farbschema: Hell / Dunkel / Automatisch (exists since v0.3.0).
- Tabelle: sum row, dense rows, visible columns (also from the column chooser).
- Stored in the device config (`internal/config`), not in the data folder – two devices can
  look different.

## Implementation steps (each one shippable)

1. **Design switch without a visible change**: split `styles.css` into shared rules and the
   classic token set; `data-design` on the root element (next to `data-theme`); setting in the
   device config and in Settings · Darstellung (only "Klassisch" selectable until step 2 is done);
   inline spacing values → classes / spacing tokens; amount markers where missing.
2. **New design tokens and look**: token set for the new design (surfaces, text, muted, border,
   accent fill/text, payer colours, status colours, spacing, radii, fonts) in light and dark,
   validated for contrast ≥ 4.5:1; bundle the two fonts; restyle every page through the shared
   classes for `[data-design="new"]` – menu as icon rail with logo tile, header, buttons, chips,
   cards, tables, forms, dialogs, dashboard (layout unchanged); one-time hint after the update
   where to switch back to "Klassisch".
3. **View switch + Detail view** (default) in the new design; today's table stays as "Klassische
   Liste" (and is the only list in the classic design, without a switch); keep all functions of
   the current list (ticks, popovers for refund amount and Klärung, bulk bar).
4. **Tabelle**: read-only grid with sorting, column chooser, sum row, sticky header/column.
5. **Kompakt** with expandable rows.
6. **Settings · Darstellung** complete: design, default view, table options; remember the last
   quick choice for the session only.
7. Tests: unit tests per view (same data, sorting, column chooser, keyboard), contrast check
   for all four design/theme combinations, headless screenshots of the main pages in each (the
   new views in the new design, light and dark);
   docs (README, PLAN).

## Open points

- Later, own step: the dashboard of mockup A (decided 2026-11-03: not in v0.4.0's first version).
- Status dots need a text alternative in every place (screen readers, colour blindness): the
  short text next to them ("eingereicht 20.09.") stays visible, the dots are decoration.
- Performance with several hundred invoices in the table ("alle Jahre"): render all rows first,
  measure, virtualise only if needed.
