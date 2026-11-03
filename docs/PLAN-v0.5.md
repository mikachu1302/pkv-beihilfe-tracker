# v0.5.0 – Local AI: reading documents and asking questions (plan)

Status: draft · 2026-11-03 · server set up and tested (results below); decisions recorded; a final
check with real documents is still advised. Next: [PLAN-v0.6.md](PLAN-v0.6.md) (checking invoices
against other documents).

v0.5.0 brings two AI features, both **optional** (without the server everything works as today)
and both **started by the user** – nothing runs on its own:

1. **Reading documents with AI** – in the inbox and the invoice form, as an alternative to the
   text recognition (Tesseract.js) where it fails: skewed phone photos, long tables, unusual
   layouts, poor prints, handwriting.
2. **Asking questions about the own data** ("Fragen", a chat) – "Wie viele Rechnungen aus 2026
   sind noch offen?", "Wo hat die Beihilfe zu wenig erstattet?". The model turns the question into
   a query; **the app computes the answer** from its data.

The rule for both: **the model reads and phrases, the app computes and checks.** An 8B model
reads text and tables well but invents numbers and rules now and then.

## The server (as set up on 2026-11-03)

A spare Samsung Galaxy Book4 Edge in the home network runs the model; the app only calls it over
HTTP. No development happens on the server.

| | |
|---|---|
| API | OpenAI-compatible, `http://192.168.178.38:8080/v1` (`POST /chat/completions`, `GET /models`) |
| Health | `GET http://192.168.178.38:8080/health` → `{"status":"ok"}` (no key; llama.cpp-specific) |
| Auth | API key as `Authorization: Bearer …` – handed over separately, never in the repo (on the development machine it sits in the git-ignored `.env` as `API_KEY`, read by `scripts/ki-belegtest.py`) |
| Model | `qwen3-vl-8b` (Qwen3-VL-8B-Instruct, Q4_0, vision) |
| Software | llama.cpp `llama-server` on Windows 11 ARM64 (Snapdragon X Elite, 16 GB); text on the CPU, the image encoder on the Adreno GPU (OpenCL) |
| Availability | starts at boot without login, restarts after crashes; on AC, lid closed; may still be away (battery, Wi-Fi, Windows Update) |
| Reach | home network only (`192.168.178.0/24`), plain HTTP, no cloud |
| Load | up to 4 requests in parallel sharing a 16k-token context; two phones use it too |

`response_format` with a JSON schema is enforced by a grammar: the answer is always valid JSON of
that shape. Measured by the server's owner: a 1000×1400 px invoice → JSON in ~47 s; text-only
questions in ~5–9 s.

**First run of `scripts/ki-belegtest.py` (2026-11-03, made-up documents, pages scaled to 1,400 px):**

| Document | Pages | Prompt tokens | Time | Fields right |
|---|---|---|---|---|
| pharmacy receipt (photo-like PNG) | 1 | 1,675 | 56 s | 3/3 |
| invoice, 2 photos as one document | 2 | 3,041 | 149 s | 6/6 (total and IBAN from page 2) |
| invoice, 2-page PDF | 2 | 3,041 | 152 s | 6/6 |
| first page of that invoice alone | 1 | 1,675 | 16 s | 5/5 (prompt cache: same image as before) |

- All fields right – but these are clean, made-up pages the OCR reads too; the real test with
  difficult documents is still to come.
- **Two pages fit one request** (~3,000 tokens) and the model combines them (header from page 1,
  total from page 2). Three or more pages are untested (per-slot context may be ~4,000 tokens).
- Reading ~22 tokens/s with images (the owner measured ~40), writing ~6 tokens/s – the image
  prefill dominates: **~1 minute per page, ~2.5 minutes for two pages**. Smaller pages (e.g.
  1,100 px) are worth a try. llama.cpp reuses the prompt cache: a page sent before is much faster.

## Test results (2026-11-03)

Made-up documents only (real ones were not at hand); they are built to be hard: a 3-page GOÄ
invoice with carry-overs, a 4-page dentist invoice, a skewed phone photo with a shadow, a dark
low-contrast photo, a faded till receipt, 30 positions in a small font. Scripts:
`scripts/ki-belegtest.py`, `scripts/ki-fragentest.py`.

**Reading: AI vs. the app's text recognition** (same documents, same comparison; the OCR values
read in the headless app, as the inbox fills them):

| | App OCR | AI, pages 1,400 px | AI, pages 1,000 px |
|---|---|---|---|
| Fields right (36) | 28 (78 %) | **33 (92 %)** | 30 (83 %) |
| Time per page | seconds, on the own computer | ~70 s | ~32 s |
| 3 pages / 4 pages in one request | – | 250 s / 372 s, all fields but one | 92 s / 128 s, amount lost on 4 pages |
| Dark photo | 0 of 5 | 4 of 5 | 4 of 5 |
| IBAN | 1 of 4 | 4 of 4 | 4 of 4 |
| Amount on the till receipt, treatment end on the dentist invoice | right | wrong (empty / a position date) | wrong |

- **The two ways complement each other**: the AI reads dark photos and IBANs, the OCR got the
  receipt total and the period the AI missed. Combined with the order of trust, 35 of 36 fields
  would have been right. A sharper prompt did not fix the AI's three misses – a limit of the model.
- **Up to four pages fit one request** (the context is large enough); time grows with the pages.
- **1,400 px is worth the time**: at 1,000 px the AI loses amounts in small print (the most
  important field) and is only about twice as fast.
- Speed: reading 24–33 tokens/s with images, writing 6–8 tokens/s.

**Questions → queries** (40 made-up questions, `scripts/ki-fragentest.py`):

| Schema | Understood right | Time per question |
|---|---|---|
| every filter field required (first draft) | 10 of 40 (25 %) | ~15 s |
| **filters as a list (only the needed ones), five examples in the prompt** | **27 of 40 (68 %)** | ~8 s |

- With every field required, the model filled in filters nobody asked for; listing only the
  needed filters fixed most of it and halved the time (shorter answers).
- Remaining misses are mostly borderline: "stehen noch aus" as "not refunded" instead of
  "submitted", "nur die vom Zahnarzt" as provider instead of category, physiotherapy as hospital,
  two questions about Klärungsbedarf / held-back claims declared unsupported, a follow-up that lost
  its year.
- Consequences for the design (Feature 2): the interpretation chips are essential (they make
  every miss visible and fixable); the app adds a synonym table and deterministic corrections
  ("Physio…" → Heilmittel, "Klärung" → Klärungsbedarf, "zurückgehalten" → held); more examples for
  status words; target **≥ 85 % on the question set** before the feature ships.

## Decisions (2026-11-03)

| Topic | Decision |
|---|---|
| Trigger | **Only manual.** Every AI feature starts with a click (or a sent question); nothing runs automatically – not when the OCR is unsure, not in the background for the inbox. The server is shared and slow, and health data leaves the device only when the user asks for it. |
| API key | **Entered by hand once per device** in the settings (password field), kept in the device configuration (`config.json` in the OS config folder, 0600). Write-only towards the frontend: it hands the key to the backend once (`Svc.SetAIKey`) and afterwards only learns "stored" and the last four characters. Not the OS keyring (one more dependency, a keyring service on Linux, little gain for a LAN-only key). The app never reads `.env`. |
| Conflicts | **GiroCode > AI confirmed by the OCR text > OCR (label) > AI alone.** The losing value is shown as a hint next to the field, never replaced silently; a value the user typed always stays. |
| Chat | **Part of v0.5.0** – "Fragen" with the AI translating, the app computing. |
| Refund documents | Reading Bescheide with AI moves to v0.6.0, where the comparison needs it anyway. |

## Shared parts

### Backend: `internal/ai` (new) – the client

- OpenAI-compatible client on `net/http`, no new dependency: `Health(ctx)` (3 s), `Models(ctx)`,
  `Chat(ctx, messages, schema)` returning the JSON content and llama.cpp's timings.
- **One request at a time** from the app (a mutex; the server is shared). Timeouts: 300 s per page
  for documents, 60 s for questions – the `context` cancels earlier.
- Errors in German for the UI: server not reachable (with the address), key refused (401/403),
  model missing, timeout, answer not readable.

### Settings · KI (device-local, `internal/config`)

On/off (default off), server address (default `http://192.168.178.38:8080/v1`), model (default
`qwen3-vl-8b`), API key ("gespeichert (…7f3a)" · "Ändern" · "Entfernen"), "Verbindung testen"
(health, `/v1/models` with the key, "model available?"). Never in the synced data folder: two
devices may differ, and the key must not travel with the data.

### Privacy (README)

A second network path: on a click, document pages (health data) travel as **plain HTTP over the
home Wi-Fi** to the server; for questions only the question and the list of possible filters go
there – the invoices stay in the app. Off by default, LAN only, API key, no cloud.

## Feature 1: reading documents with AI

### Flow

1. The user opens a document (inbox or form); the OCR runs as today and fills the fields.
2. "**Mit KI auslesen**": the frontend renders the pages (≤ 1,400 px, JPEG – pdf.js already
   renders PDF pages for the OCR) and calls `Svc.ReadWithAI(ctx, {pages, lines, qr})`; the OCR
   lines and GiroCode go along for the checks. The call is a Wails `CancellablePromise`;
   "Abbrechen" or leaving the document cancels the Go `context` and the HTTP request.
3. The backend checks `/health`, sends one chat request with the pages and a JSON schema, and turns
   the answer into the same `extract.Draft` the OCR produces, with sources per field.
4. The frontend fills the fields with the existing prefill logic (`lib/prefill.ts`): empty fields
   and "bitte prüfen" values are replaced, typed values never; conflicts follow the order of trust
   above, the losing value is shown as a hint ("Texterkennung: …" / "KI: …").

### Backend: `internal/airead` (new)

- JSON schema mirroring `extract.Draft`: kind (`invoice`, `refund`, `prescription`, `other`),
  provider, payee, IBAN, reference, category (enum of the app's categories), invoice date,
  treatment from/to, amount (string, "1234.56"), due date. German prompt: dates as YYYY-MM-DD,
  empty string when a field is not on the document, nothing guessed.
- **Checks** before a value is offered:
  - IBAN: valid checksum (mod 97), else dropped;
  - amount: also present in the OCR text → `SrcAIConfirmed`, else `SrcAI`; a GiroCode amount wins;
  - dates: valid and plausible (not decades away, treatment ≤ invoice date + a margin);
  - provider matching a known provider or alias of earlier invoices → confirmed.
- New sources in `extract`: `SrcAI` ("KI – bitte prüfen", warn) and `SrcAIConfirmed` ("KI,
  bestätigt"); the frontend's `SOURCE` table gets both.
- Multi-page: all pages in one request (tested up to four); pages scaled to 1,400 px on the long
  side, JPEG. The progress shows the expected time (~1 minute per page).

### Frontend

- Inbox review and invoice form: "Mit KI auslesen" next to the recognised fields when AI is on.
  While it runs: "KI liest den Beleg … (etwa 1 Minute pro Seite)", elapsed time, "Abbrechen";
  afterwards a short summary ("7 Felder gefüllt, 2 bestätigt, 1 Abweichung").
- Source badges "KI – bitte prüfen" / "KI, bestätigt", conflict hints next to the fields.
- Server away: "KI-Server nicht erreichbar – die Texterkennung bleibt." No automatic retries.

## Feature 2: asking questions ("Fragen")

### Idea

The model never sees the invoices. It gets the question, today's date and the vocabulary of the
app (payers by name, categories, statuses) and answers with a **query** in a fixed JSON schema.
The app runs the query on its data – the same calculations as the list, the dashboard and the
year overview – and shows the result. Text only: a question takes seconds, not minutes.

Examples:

| Question | Query (simplified) | Answer |
|---|---|---|
| Wie viele Rechnungen aus 2026 sind noch offen? | count · year 2026 · not yet refunded | "12 Rechnungen" + the list |
| Wo hat die Beihilfe zu wenig erstattet? | list · payer Beihilfe · refunded with IST < SOLL | list with the differences, sum |
| Was hat die PKV 2025 insgesamt erstattet? | sum IST · payer PKV · year 2025 | "3.010,80 €" + by month |
| Welche Rechnungen sind noch nicht bezahlt? | list · not paid | list, due dates |
| Wie viel habe ich 2026 beim Zahnarzt ausgegeben? | sum amount · year 2026 · category Zahnarzt | sum + list |
| Lohnt sich die Beitragsrückerstattung 2026? | premium refund · year 2026 | the dashboard's comparison |
| … und 2025? | (follow-up: the last query with year 2025) | |

### Query model (`internal/ask`, new – deterministic, the important part)

- `Query`: `measure` (count, amount, SOLL, IST, difference, outstanding, own share), `filters`
  (years or a date range, payer, status per claim, deviation IST < / > SOLL, Klärungsbedarf,
  paid / not paid / due, deadline soon, category, provider text, amount range), `group by`
  (none, year, month, payer, provider, category), `sort` and `limit`; special queries for the
  existing figures (premium refund comparison, Ausstand per payer, reminders).
- `Run(data, query, today)` → result: a number, a small table or groups, and the matching
  invoices (`InvoiceRowDTO`) – computed with `internal/calc`, the same code as the list.
- `unsupported` with a reason when the question is not about the own data ("Was ist die
  Kostendämpfungspauschale?") – the app says so instead of letting the model answer from its
  general knowledge (an 8B model is not reliable on Beihilfe rules).
- Many unit tests: every filter, follow-ups, edge cases (no data, all years, unknown payer).

### Turning the question into a query

- JSON schema built at runtime: `typ`, `messgroesse`, `jahre`, a **list of filters** (each a field
  with its allowed values – payer names from the settings, categories, statuses), `gruppierung`;
  prompt with today's date ("dieses Jahr", "letzten Monat") and examples. Listing only the needed
  filters is what made the model reliable (see Test results).
- After the model: a synonym table and deterministic corrections in the app (words for
  categories and statuses), then the query runs; the chips show the final query.
- Follow-ups: the last two questions with their queries go along (no invoice data), so "und
  2025?" changes only the year.
- Measured later with a fixed set of ~40 German questions and the expected queries
  (`scripts/ki-fragentest.py`, made-up, in the repo): share of questions understood right.

### UI

- Menu entry **"Fragen"** (both designs; a chat page): input field, "Fragen" button (Enter),
  history of this session (not stored).
- Each answer shows **how the app understood the question** as chips ("2026 · Beihilfe · zu wenig
  erstattet") – removable and editable, so a misunderstanding is visible and fixable without the
  model; then the number or table in a sentence the app builds ("12 Rechnungen, zusammen 3.041,23
  €"), the invoices as a compact list, and "In der Rechnungsliste zeigen" (opens the list with the
  same filters where the list has them).
- While waiting: "Frage wird verstanden …" (a few seconds), "Abbrechen".
- Server away: the page says so; the filters of the invoice list remain the way without AI.

## Steps

1. **Real-world test** (before building): 5–10 real invoices with `scripts/ki-belegtest.py` –
   accuracy, time per page, a 3-page document, with the phones idle and in use; smaller pages.
   Guide: [KI-TEST.md](KI-TEST.md).
2. `internal/ai` (client) and Settings · KI (address, model, key, test connection).
3. `internal/airead` (schema, checks) and the frontend for reading (button, progress, cancel,
   badges, conflict hints); tests against an `httptest` server (answers, 401, timeouts, cancel,
   unreachable, invalid JSON, wrong IBAN, amount not in the OCR text).
4. `internal/ask` (query model and `Run`) with unit tests – independent of the AI.
5. Question → query (schema, prompt, follow-ups) and `scripts/ki-fragentest.py` with the question
   set; then the "Fragen" page.
6. README privacy section, docs, headless checks with a fake server.

## Open points

1. ~~8B model or NPU path?~~ The 8B model reads well (92 % on hard made-up documents, better than
   the OCR where it matters); ~1 minute per page is acceptable for a manual trigger. The NPU
   (Qwen3-VL-4B) only if the time becomes a problem.
2. ~~Three or more pages?~~ One request, tested up to four pages.
3. ~~Image size?~~ 1,400 px – smaller pages lose amounts in small print.
4. Real documents: one more run of `scripts/ki-belegtest.py` with 5–10 real difficult invoices
   before building, to confirm the numbers above.
5. Questions the query model cannot express: collect them during use and extend the model (no
   free-form answers from the model); the question set grows with them.
