# v0.4.0 – Redesign, views and themes (plan)

Status: planned · 2026-11-03 · mockups: the design canvas "PKV Beihilfe – Designrichtungen"
(private claude.ai artifact of the owner; section "v0.3.0 · Drei Ansichten, hell und dunkel" –
the mockups were made when the redesign was still planned for v0.3.0).

v0.3.0 comes first (documents in the invoice form, multi-page documents – see
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
| Design switch | "Design: Klassisch / Neu" per device. Both designs share the **same structure** (menu, pages, forms, views) and differ only in look (colours, fonts, spacing, radii, density). No second set of screens: every feature is built once and shows up in both designs. "Klassisch" is today's look, not a frozen copy of v0.2.0. |
| Views | Detail, Kompakt, Tabelle – the same data in all three – plus today's list as "Klassische Liste". Views and design are independent: every view works in both designs. |
| Default view | Configurable per device; out of the box: **Detail**. Switching in the list applies until the next start (or until changed in the settings). |
| Table | **Read only**: sorting, choosing columns, sum row. Editing stays in the invoice form (double click / Enter opens it). |
| Themes | System / Hell / Dunkel, per device, in the settings and as a quick toggle – for both designs (unless already shipped with v0.3.0). |
| Version | Redesign, views and the design switch are v0.4.0 (decided 2026-11-03; originally planned for v0.3.0). |

## Design switch

Today all colours are CSS custom properties in `frontend/src/styles.css` (about 360 lines, light
and dark via `prefers-color-scheme`); components contain no hard-coded colours. That makes the
switch cheap:

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

- Design: Klassisch / Neu.
- Standardansicht: Detail (default) / Kompakt / Tabelle / Klassische Liste.
- Farbschema: wie das System (default) / hell / dunkel.
- Tabelle: sum row, dense rows, visible columns (also from the column chooser).
- Stored in the device config (`internal/config`), not in the data folder – two devices can
  look different.

## Implementation steps (each one shippable)

1. **Design switch without a visible change**: split `styles.css` into shared rules and the
   classic token set; `data-design` / `data-theme` on the root element; setting in the device
   config and in Settings · Darstellung (only "Klassisch" selectable until step 2 is done).
2. **New design tokens and look**: token set for the new design (surfaces, text, muted, border,
   accent fill/text, payer colours, status colours) in light and dark, validated for contrast
   ≥ 4.5:1; bundle the two fonts; restyle the shell (menu, header, buttons, chips, cards) for
   `[data-design="new"]`. Theme switch System/Hell/Dunkel with quick toggle, if not already
   shipped with v0.3.0.
3. **View switch + Detail view** (default); today's table stays as "Klassische Liste"; keep all
   functions of the current list (ticks, popovers for refund amount and Klärung, bulk bar).
4. **Tabelle**: read-only grid with sorting, column chooser, sum row, sticky header/column.
5. **Kompakt** with expandable rows.
6. **Settings · Darstellung** complete: design, default view, table options; remember the last
   quick choice for the session only.
7. Tests: unit tests per view (same data, sorting, column chooser, keyboard), contrast check
   for all four design/theme combinations, headless screenshots of the main pages in each;
   docs (README, PLAN).

## Open points

- Default design after the update: "Neu" for everyone with a one-time hint where to switch back,
  or keep "Klassisch" for existing installations?
- Should the classic design default to the "Klassische Liste" view?
- Should the invoice detail page also get the "Reise einer Rechnung" layout (direction D)?
- Status dots need a text alternative in every place (screen readers, colour blindness).
- Performance with several hundred invoices in the table (virtualise only if needed).
