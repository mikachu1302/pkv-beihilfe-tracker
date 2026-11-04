# v0.6.0 – Checking invoices against other documents with AI (plan, dropped)

Status: **dropped 2026-11-04** (the user's decision): too much for what it adds. Most checks would
confirm what the app already shows – the refund against SOLL, data read from the same invoice –
after about ten minutes of reading per invoice. Step 2 (`internal/check`, 9b224c2) was removed
again the same day; nothing of this plan is in the app. What stays is the test of step 1 below
and its consequence for v0.5's reading: dates and amounts as printed, parsed in Go (fixed
2026-11-04, see [PLAN-v0.5.md](PLAN-v0.5.md), "Implementation").
Builds on v0.5.0 ([PLAN-v0.5.md](PLAN-v0.5.md)).

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

**The model transcribes, the app interprets** (decided after the test below, 2026-11-03): the
model copies what is printed – dates, amounts and numbers unchanged, a table as its column
headers plus one text per row with the cells separated by " | ", the sum lines as "label |
amount", the notes of a Bescheid as number and text. Go parses dates and amounts, maps the
columns by the words in their headers, puts missing empty cells where the other cells fit their
columns, carries the date of a day forward to the rows below it and derives the treatment period
from the positions when none is printed. Asked to fill named fields instead, the model left the
amounts of every Bescheid line empty and converted "20.11.2026" to "2022-11-20".

| Type | Transcribed | The app derives |
|---|---|---|
| Rechnung | provider, payee, number, date, period, IBAN; the **positions table** (excluding carry-overs and an attached lab invoice); the sum lines | positions (date, tooth, number, text, count, factor, amount), total, material and lab costs |
| Bescheid / Leistungsabrechnung | payer, date, application date; the **table of invoices**; the sum lines; the **notes** (number, text) | lines (invoice date, provider, billed, eligible, rate, refund, note numbers), deductions, payout |
| Heil- und Kostenplan / Kostenvoranschlag | provider, date; the table; the sum lines | positions, fee, material and lab costs, total, own share |
| Verordnung (therapy) | prescriber, date, frequency, diagnosis, ICD, valid until; the prescribed lines ("6 × Krankengymnastik") | remedies with counts |

All pages of a document go into one request (as in v0.5); the model follows a table across pages
and leaves out carry-overs. Bescheide reuse this in the inbox too: "Mit KI auslesen" for a
Bescheid feeds its lines into the existing review and matcher (`internal/match`) – the matching
and the checksum against the payout stay deterministic. The column words for Bescheide are those
the text recognition already uses for its layout profiles.

### Test of step 1 (2026-11-03)

Made-up documents from `scripts/ki-testbelege.py` (no real data): a GOÄ invoice through a billing
service (2 pages, carry-over, factor 3.5 with a reason), a GOZ invoice with tooth numbers and an
attached lab invoice (3 pages), the cost plan for its crown, a prescription and the physiotherapy
invoice (one session more than prescribed), a Beihilfebescheid (note numbers in the lines, the
texts on page 2) and a PKV statement with a deductible – 7 documents, 11 pages; five of them also
as poor phone photos (skewed with a shadow, dark, folded, on a table): 12 documents, 18 pages,
563 compared fields. Measured with `scripts/ki-belegtest.py`, qwen3-vl-8b on the home server,
pages at 1,400 px, all pages of a document in one request.

| Approach | Fields right | What went wrong |
|---|---|---|
| A. Fields and lists in a JSON schema (as v0.5): dates as YYYY-MM-DD, amounts as 1234.56 | 391 (69 %) | Bescheide: every line found, but all amounts left empty, no payout; the PKV statement's dates all 2022 instead of 2026 (a conversion error – transcribed, the same dates are right). Invoices: 53 of 54 positions, 52 amounts right, but the total empty in 3 of 4 (asked before the positions). Prescription: counts empty, the second remedy missing. |
| B. A with the totals after the lists, amount fields named by their column headers | – | invoice totals right after the positions; Bescheid amounts still empty |
| **C. Transcribed, interpreted by the app** | **557 (99 %)** | all Bescheid lines, notes, deductions and payouts right, also on the photos; prescription right. Two errors left: two consecutive rows without a tooth number were merged (the Ä1 position missing) and 16,78 read as 18,78 on the dark photo – **both caught by the checksum** (positions 10,72 € below the fee; 2,00 € above the total). "Ä925a" read as "A925a" is corrected by a rule (dental invoices only: `A` before a digit is `Ä`). |

Time (unloaded server): reading ~30–40 tokens/s, writing ~7 tokens/s – writing the rows takes
longest. One page 1–2½ minutes, the 2-page GOÄ invoice ~5 minutes, the 3-page GOZ invoice ~7, the
2-page Bescheid ~5. Two requests at once halve the speed. A check of an invoice with its Bescheid
therefore takes about **ten minutes**: the background job with progress, "Abbrechen" and a toast
(Flow, step 3) is required, and documents are read one after another.

What follows for the plan:
- A sum that does not add up is **"prüfen"** (⚠), not "Abweichung" (✗): it can be a reading error
  as well as a wrong invoice. The finding names the difference ("Positionen 10,72 € unter dem
  Honorar – eine Position fehlt oder ist falsch gelesen") and the page.
- The recorded answers of approach C were to become test data for the Go interpretation (they
  were in `internal/airead/testdata/abgleich-*.json` until the plan was dropped).
- **v0.5's reading** asks for dates as YYYY-MM-DD too, and its date check only drops dates older than
  ten years – a wrong year would be offered as "KI – bitte prüfen" when the text recognition finds
  no date. Fixed 2026-11-04: it asks for dates and amounts as printed and parses them in Go.

## Checks (code, `internal/check` – new)

**The invoice in itself**
- positions add up to the total (within half a cent per position for rounding);
- position dates inside the treatment period; the same number twice on one day (hint);
- read header vs. the invoice in the app (amount, date, provider, number) – a mismatch means the
  stored data is wrong or it is another invoice.

No checks of the fee rules (GOÄ/GOZ factors, thresholds, points): the payers check those, and a
cut shows in the app as a deviation with its reason from the Bescheid (decided 2026-11-03).

**Invoice vs. Bescheid**
- the Bescheid line belonging to the invoice (via the existing matcher), billed amount = invoice
  total;
- eligible < billed: which positions were cut and by how much (when the Bescheid lists them);
- refund vs. the expected share (SOLL) – the same deviation the app shows today, now with the
  reason: the cut texts are **summarised in plain German by the AI** (marked "KI-Zusammenfassung,
  bitte im Bescheid nachlesen").

**Invoice vs. Heil- und Kostenplan / Kostenvoranschlag**
- positions in the invoice that the plan does not have, and the other way round;
- amounts per position and the total vs. the plan – shown as information with the difference in
  euros and per cent, no tolerance and no verdict (decided 2026-11-03);
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

1. ~~Test with made-up but realistic documents~~ – done 2026-11-03, see "Test of step 1":
   positions, Bescheid lines and note texts are read reliably when transcribed (99 %).
   `scripts/ki-testbelege.py` creates the documents, `scripts/ki-belegtest.py` reads and scores
   them (`auswerten` re-scores saved answers without the server).
2. ~~`internal/check` with unit tests~~ – written 2026-11-04 (9b224c2), removed with the plan
   the same day.
3. – 6. Not done (plan dropped). Only the date fix of step 3 was made: v0.5's reading asks for
   dates and amounts as printed and parses them in Go (`airead.readDate`, `money.Parse`).

## Open points

1. ~~Keep the last check?~~ Yes, in the invoice record (decided 2026-11-03).
2. ~~GOÄ/GOZ rules?~~ None – the payers check them (decided 2026-11-03).
3. ~~Tolerances?~~ Not needed: differences to a cost plan are shown as information (decided
   2026-11-03).
4. ~~Draft letters?~~ Not now (decided 2026-11-03).
5. ~~Test documents?~~ Made-up but realistic ones (decided 2026-11-03; public filled-in examples
   are scarce – sample pages move or are blank forms, no sample Bescheid found).
