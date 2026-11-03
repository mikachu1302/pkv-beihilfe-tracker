# v0.6.0 – Checking invoices against other documents with AI (plan)

Status: draft · 2026-11-03 · builds on v0.5.0 ([PLAN-v0.5.md](PLAN-v0.5.md): AI client, settings,
manual trigger, order of trust).

For an invoice already in the app, the user starts a **check** ("Abgleich"): the invoice and other
documents – the Bescheid, a Heil- und Kostenplan or cost estimate, a prescription for therapy –
are read by the AI, and the app compares them with the invoice's data and with each other. The
result is a list "passt / prüfen / Abweichung" with reasons, and actions such as marking
Klärungsbedarf.

The rule from v0.5 holds: **the model reads and phrases, the app computes and checks.** The model
turns each document into structured data (positions, lines, counts); every comparison, sum and
rule is code in the app – traceable and without invented numbers. Only where text must be
understood (the reasons for a cut in a Bescheid) does the model summarise, and that summary is
marked as such.

## Flow

1. Invoice form → **"Abgleich mit KI"** (only when AI is on; a manual trigger as all AI features).
2. A panel lists the documents for the check: the invoice's attached documents (preselected) and
   "Dokument hinzufügen" (file or drag & drop; from the inbox too). For each document the type:
   Rechnung, Bescheid, Heil- und Kostenplan / Kostenvoranschlag, Verordnung – suggested by the AI
   (or the OCR), changeable before the start. Added documents can be kept as documents of the
   invoice ("Als Beleg zur Rechnung speichern", default on for Bescheide).
3. "Abgleich starten": the documents are read one after another (~1 minute per page; progress per
   document, "Abbrechen"). The check runs in the backend as a job with progress events, so the user
   can work on; a toast says when it is done, and the result waits in the invoice form.
4. The app runs the checks and shows the result; the user acts on it.

## What the AI reads (one JSON schema per document type)

| Type | Fields |
|---|---|
| Rechnung | header (provider, date, number, total, period, IBAN) and **positions**: date, number (GOÄ/GOZ Ziffer), text, factor, count, amount, reason for the factor |
| Bescheid | header (payer, date, payout, application date) and **lines**: invoice date, provider, billed, eligible, refund, rate, reason / note for a cut (text and code) |
| Heil- und Kostenplan / Kostenvoranschlag | provider, date, positions (number, text, count, amount), totals (fee, material and lab costs, expected own share) |
| Verordnung (therapy) | date, prescriber, remedy, count, frequency, diagnosis/ICD (only what the check needs), valid until |

Long documents: page by page with the positions merged in Go (decided in v0.5's test whether
pages can be combined). Bescheide reuse this in the inbox too: "Mit KI auslesen" for a Bescheid
feeds its lines into the existing review and matcher (`internal/match`) – the matching and the
checksum against the payout stay deterministic.

## Checks (code, `internal/check` – new)

**The invoice in itself**
- positions add up to the total (±1 cent per position for rounding);
- position dates inside the treatment period; the same number twice on one day (hint);
- read header vs. the invoice in the app (amount, date, provider, number) – a mismatch means the
  stored data is wrong or it is another invoice.

**GOÄ / GOZ rules** (a small rule table in the app, to be researched and tested before use)
- factor above the threshold (GOÄ: 2.3 personal services, 1.8 technical services, 1.15 laboratory;
  GOZ: 2.3) **without a reason** → "Begründung fehlt";
- factor above the maximum (GOÄ 3.5 / 2.5 / 1.3, GOZ 3.5) → needs an agreement (§ 2 GOÄ/GOZ);
- the amount of a position = points × point value × factor × count, where the number is known
  (point values: GOÄ 5.82873 ct, GOZ 5.62421 ct) – hint only, never an automatic correction.
- The values above are those of the current GOÄ and GOZ; a new GOÄ has been in preparation for
  years – check the state before building, and keep the table versioned by date of treatment.

**Invoice vs. Bescheid**
- the Bescheid line belonging to the invoice (via the existing matcher), billed amount = invoice
  total;
- eligible < billed: which positions were cut and by how much (when the Bescheid lists them);
- refund vs. the expected share (SOLL) – the same deviation the app shows today, now with the
  reason: the cut texts are **summarised in plain German by the AI** (marked "KI-Zusammenfassung,
  bitte im Bescheid nachlesen").

**Invoice vs. Heil- und Kostenplan / Kostenvoranschlag**
- positions in the invoice that the plan does not have, and the other way round;
- amounts per position and the total vs. the plan, beyond a **tolerance the user sets** (Einstellungen,
  explained there: "Abweichung vom Kostenvoranschlag melden ab … %"; default 15 % – the margin above
  which a dentist must inform the patient before going on, § 9 Abs. 2 GOZ);
- expected own share vs. what the payers refunded.

**Invoice vs. Verordnung (therapy)**
- sessions billed ≤ sessions prescribed; first treatment after the prescription date and within
  its validity (private rules vary – hints, no verdict).

## Result and actions

- A list grouped by document: ✓ passt, ⚠ prüfen, ✗ Abweichung – each with the numbers ("Summe der
  Positionen 312,40 € = Rechnungsbetrag"), the source (page, line) and, where relevant, the AI
  summary.
- Actions: **"Klärungsbedarf markieren"** with the finding as the note (prefilled, editable);
  "In die Notiz übernehmen". (Draft letters to the payer or provider are not part of v0.6 –
  decided 2026-11-03; maybe later.)
- The last check is kept with the invoice (date, documents, findings) so it does not have to be
  repeated; a new check replaces it. Stored in the invoice's record (schema extension, merged like
  other fields) – decided 2026-11-03.

## Smaller AI helpers (same release, all manual)

- **Recognising the type of a document** in the inbox when the OCR is unsure (invoice, Bescheid,
  prescription, reminder, cost estimate) – one click "Belegart erkennen".
- **Reminders (Mahnungen)**: a reminder is matched to the existing invoice (provider, number,
  amount) instead of creating a new one; the app offers to set the due date / mark it unpaid.
- Handwritten documents (Heilpraktiker, receipts) are covered by v0.5's "Mit KI auslesen".

## Steps

1. Test with real documents (as in v0.5): positions of 3–5 GOÄ/GOZ invoices, 2–3 Bescheide with
   cuts, one Heil- und Kostenplan – are positions and cut texts read reliably? (Extend
   `scripts/ki-belegtest.py` with the document types.)
2. Research and test the GOÄ/GOZ rule table (sections, thresholds, maxima, point values).
3. `internal/check` with unit tests (made-up invoices, Bescheide, plans, prescriptions).
4. Schemas per document type, the job with progress events, cancel.
5. UI: the panel in the invoice form, the result list, the actions; the tolerance setting.
6. Type recognition and reminders in the inbox.
7. Docs (README privacy section: what is sent for a check).

## Open points

1. ~~Keep the last check?~~ Yes, in the invoice record (decided 2026-11-03).
2. How far the GOÄ/GOZ rules go: thresholds and maxima are clear; the section of each number needs a
   table (maintained in the app, from the official lists). Start with GOÄ, GOZ afterwards?
   (Explained to the user 2026-11-03; decision open.)
3. ~~Tolerances?~~ A setting, explained in the settings, default 15 % (§ 9 Abs. 2 GOZ) – decided
   2026-11-03.
4. ~~Draft letters?~~ Not now (decided 2026-11-03).
5. Test documents: no real documents (the user's wish). Public filled-in examples are scarce
   (insurers' and consumer centres' sample pages move or are blank forms; no sample Bescheid
   found). Proposal: made-up but realistic documents with real GOÄ/GOZ numbers, points and
   factors, as in v0.5's test – decision open.
