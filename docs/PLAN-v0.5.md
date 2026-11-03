# v0.5.0 – Reading documents with a local AI model (plan)

Status: draft · 2026-11-03 · server set up and reachable; decisions marked "open" below.

The text recognition (Tesseract.js) and the parsers in `internal/extract` read clean scans
well, but fail on skewed phone photos, long tables, unusual layouts and poor prints. A
vision-language model reads the page as an image and returns the fields directly. v0.5.0 adds
it as an **optional second way** to read a document – never a replacement: without the server
everything works as today.

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

Measured by the server's owner: a 1000×1400 px invoice image → JSON in about **47 s**, all fields
right (number, date, provider, amount, IBAN). An image costs ~1,300 tokens; input ~40 tokens/s
with an image, output 8–20 tokens/s. `response_format` with a JSON schema is enforced by a
grammar, so the answer is always valid JSON of that shape.

Checked from the development machine on 2026-11-03: `/health` answers `ok`, `/v1/models` without
a key answers 401 – as described.

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
- Slower than the owner's measurement: reading ~22 tokens/s with images (not ~40), writing ~6
  tokens/s – the image prefill dominates: **~1 minute per page, ~2.5 minutes for two pages**.
  The UI must show progress and offer "Abbrechen"; smaller pages (e.g. 1,100 px) are worth a try.
- llama.cpp reuses the prompt cache: a document read again (or a page sent before) is much faster.

## What it means for the app

- **Seconds, not milliseconds**: ~45 s per page (longer when the phones use the server). It must
  run in the background, show that it runs, and be cancellable; the form stays usable.
- **Optional with fallback**: check `/health` (short timeout) before a request; when the server
  is away, say so once and keep the OCR result.
- **Suggestions, not facts**: an 8B model makes mistakes and occasionally invents values. Every
  value it fills is marked ("KI – bitte prüfen") and checked against what the app can verify.
- **Privacy**: a second network path – health data travels as **plain HTTP over the home Wi-Fi**
  to the server. Off by default, LAN only, API key, no cloud; documented in the README.
- **Images, not PDFs**: the server takes images only (`data:` URLs). The frontend already renders
  PDF pages with pdf.js for the OCR; it sends the same pages, scaled to ~1,400 px on the long side
  (larger only costs time).

## Design

### Flow

1. The user opens a document (inbox or form); the OCR runs as today and fills the fields.
2. "**Mit KI auslesen**" – only when the user clicks it (decided, see below): the frontend renders the pages
   (≤ 1,400 px, JPEG) and calls `Svc.ReadWithAI(ctx, {pages, lines, qr})` – the OCR lines and
   GiroCode go along for the cross-checks. The call is a Wails `CancellablePromise`; cancelling
   (button, leaving the document) cancels the Go `context` and the HTTP request.
3. The backend checks `/health`, sends one chat request with the pages and a JSON schema, and turns
   the answer into the same `extract.Draft` the OCR produces, with sources per field.
4. The frontend fills the fields with the existing prefill logic (`lib/prefill.ts`): only empty
   fields and values marked "bitte prüfen" are replaced; typed values never. Where OCR and AI
   disagree, the order of trust decides (open point 3): **GiroCode > AI confirmed by the OCR text
   > OCR (label) > AI alone**; the value that loses is shown as a hint next to the field
   ("Texterkennung: …" / "KI: …") instead of being dropped silently.

### Backend: `internal/airead` (new)

- A small OpenAI-compatible client on `net/http` – no new dependency: `Health`, `Models`,
  `Extract(ctx, pages, schema)`. Timeouts: 3 s for health, **300 s per page** for a request
  (cancelled by the context earlier), one request at a time from the app (a mutex/queue – the
  server is shared).
- The JSON schema mirrors `extract.Draft`: kind (`invoice`, `refund`, `prescription`, `other`),
  provider, payee, IBAN, reference, category (enum of the app's categories), invoice date,
  treatment from/to, amount (string, "1234.56"), due date. German prompt: dates as YYYY-MM-DD, empty
  string when a field is not on the document, nothing guessed.
- **Checks** before a value is offered:
  - IBAN: valid checksum (mod 97), else dropped;
  - amount: also present in the OCR text → source `ai+ocr` (confirmed), else `ai` (check it);
    a GiroCode amount always wins;
  - dates: valid and plausible (not decades away, treatment ≤ invoice date + a margin);
  - a provider the AI names that matches a known alias/provider of earlier invoices → confirmed.
- New sources in `extract`: `SrcAI` ("KI – bitte prüfen", warn) and `SrcAIConfirmed` ("KI,
  bestätigt"); the frontend's `SOURCE` table gets both.
- **Multi-page documents**: each page costs ~1,300 tokens, and four parallel slots share 16k
  tokens. Open point 5 decides between all pages in one request (if they fit) and one request
  per page merged in Go (header fields from the first page, the total from the page that has it).

### Settings (device-local, `internal/config`)

"KI-Auslesen" in Einstellungen: on/off (default off), server address (default
`http://192.168.178.38:8080/v1`), model (default `qwen3-vl-8b`), API key, "Verbindung testen"
(health + `/v1/models` with the key + "model available?"). Never in the synced data folder: two
devices may differ, and the key must not travel with the data.

The API key is **entered by hand once per device** (decided 2026-11-03) in a password field and
kept in the device configuration (`config.json` in the OS config folder, readable only by the
user, 0600). It only goes one way: the frontend hands it to the backend once
(`Svc.SetAIKey`) and afterwards only learns "a key is stored" and its last four characters
("gespeichert (…7f3a)", with "Ändern" and "Entfernen"); only the backend sends it to the server.
The `.env` file is for the test script on the development machine; the app does not read it.

### Frontend

- Inbox review and invoice form: button "Mit KI auslesen" next to the recognised fields, shown
  when AI reading is on. While it runs: "KI liest den Beleg … (etwa 45 s pro Seite)" with
  "Abbrechen"; then a short summary ("7 Felder gefüllt, 2 bestätigt").
- Source badges "KI – bitte prüfen" / "KI, bestätigt" on the fields, like "aus GiroCode" today.
- Server away: notice "KI-Server nicht erreichbar – die Texterkennung bleibt." No retry storm.
- Both designs: only tokens and shared classes (the v0.4 rule).

### Refund documents (phase 2, after the invoice part works)

Bescheide are where OCR struggles most (many lines, columns). The AI would return the lines
(invoice date, provider, billed, eligible, refund) as JSON; the existing matcher
(`internal/match`) and the checksum against the payout stay as they are – they are deterministic
and already handle the matching. A "which invoice belongs to this line?" prompt is only worth
trying if the matcher misses cases the AI gets right (measure in the test).

## Steps

1. **Real-world test** (before building): 5–10 real invoices and 2–3 Bescheide with
   `scripts/ki-belegtest.py` against the server – accuracy per field, time per page, multi-page
   behaviour (does a 3-page document fit one request?), with the phones idle and in use.
   Results decide open points 4 and 5. Guide: [KI-TEST.md](KI-TEST.md).
2. `internal/airead`: client, schema, checks; unit tests against an `httptest` server (answers,
   401, timeouts, cancel, unreachable, invalid JSON, wrong IBAN, amount not in the OCR text).
3. Settings and device config (address, model, key, test connection).
4. Frontend: page rendering for the AI (≤ 1,400 px), the button with status and cancel, source
   badges, fill rules; tests.
5. README privacy section and docs; headless checks with a fake server.
6. Phase 2: refund documents.

## Open points

1. **Where the key lives – decided 2026-11-03: the device config file**, entered by hand in the
   settings (see Settings). Not the OS keyring: one more dependency and a keyring service on
   Linux, for little gain – the key only guards a LAN service, and whoever uses the account can
   read the data folder anyway.
2. **When it runs – decided 2026-11-03: only on a manual trigger.** Every AI feature starts
   with a click ("Mit KI auslesen", later the same for Bescheide); nothing runs automatically –
   not when the OCR is unsure, not in the background for the inbox. (The server is shared and
   slow, and health data leaves the device only when the user asks for it.)
3. **Conflicts – decided 2026-11-03: GiroCode > AI confirmed by the OCR text > OCR (label) > AI
   alone.** The losing value is shown as a hint next to the field, never replaced silently; a
   value the user typed always stays.
4. Is the 8B model good enough, or is the NPU path (Qwen3-VL-4B, faster, less exact) worth
   comparing? Decided by the test.
5. Multi-page: one request or one per page (see Design) – decided by the test.
6. Scope of v0.5.0: invoices only, Bescheide in v0.5.x – unless the test shows the Bescheide gain
   much more.
