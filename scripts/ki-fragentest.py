#!/usr/bin/env python3
"""Testet, wie gut das lokale KI-Modell Fragen zu den eigenen Rechnungen in
eine Abfrage übersetzt (Plan v0.5.0, „Fragen“: das Modell übersetzt, die App
rechnet – docs/PLAN-v0.5.md).

Schickt jede Frage mit dem Abfrage-Schema an den Server und vergleicht die
Antwort mit der erwarteten Abfrage. Die Fragen sind erfunden; es gehen keine
Rechnungsdaten an den Server, nur Frage, Datum und Vokabular (Kostenträger,
Kategorien).

    python3 scripts/ki-fragentest.py              # alle Fragen
    python3 scripts/ki-fragentest.py --nur 3,7    # einzelne Fragen (Nummern)

Server, Modell und Schlüssel wie bei ki-belegtest.py (Standard: der Server im
Heimnetz; Schlüssel aus LLM_API_KEY oder API_KEY in .env).
"""

import argparse
import importlib.util
import json
import statistics
import time
import urllib.error
from datetime import date
from pathlib import Path

# Server access and key handling are shared with the document test.
_spec = importlib.util.spec_from_file_location("belegtest", Path(__file__).with_name("ki-belegtest.py"))
belegtest = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(belegtest)

TODAY = date(2026, 11, 3)  # fixed, so the expected years and dates hold
PAYERS = ["Beihilfe Bund", "Muster PKV"]
CATEGORIES = ["arzt", "zahnarzt", "apotheke", "heilmittel", "hilfsmittel", "krankenhaus", "heilpraktiker", "sonstiges"]

FILTERS = {  # filter field → allowed values (None: free text)
    "kostentraeger": PAYERS,
    "status": ["nicht_eingereicht", "eingereicht", "erstattet", "zurueckgehalten", "nicht_erstattet"],
    "abweichung": ["zu_wenig", "zu_viel"],
    "klaerungsbedarf": ["ja", "nein"],
    "bezahlt": ["ja", "nein"],
    "frist_bald": ["ja"],
    "kategorie": CATEGORIES,
    "behandler": None,
    "von": None,
    "bis": None,
}

# Only the filters a question needs are listed – with every filter field required, the model
# filled in filters nobody asked for (first run: 10 of 40 right).
SCHEMA = {
    "type": "object",
    "properties": {
        "typ": {"type": "string", "enum": ["anzahl", "summe", "liste", "beitragsrueckerstattung", "ausstand", "nicht_unterstuetzt"]},
        "messgroesse": {"type": "string", "enum": ["", "betrag", "soll", "ist", "differenz", "offen", "eigenanteil"]},
        "jahre": {"type": "array", "items": {"type": "integer"}},
        "filter": {"type": "array", "items": {"anyOf": [
            {"type": "object", "properties": {"feld": {"const": f}, "wert": {"type": "string"} if v is None else {"enum": v}},
             "required": ["feld", "wert"], "additionalProperties": False}
            for f, v in FILTERS.items()]}},
        "gruppierung": {"type": "string", "enum": ["", "jahr", "monat", "kostentraeger", "behandler", "kategorie"]},
        "grund": {"type": "string"},
    },
    "required": ["typ", "messgroesse", "jahre", "filter", "gruppierung", "grund"],
    "additionalProperties": False,
}

EXAMPLES = [
    ("Wie viele Rechnungen aus 2025 sind noch nicht eingereicht?",
     {"typ": "anzahl", "messgroesse": "", "jahre": [2025], "filter": [{"feld": "status", "wert": "nicht_eingereicht"}], "gruppierung": "", "grund": ""}),
    ("Was hat die Beihilfe insgesamt erstattet?",
     {"typ": "summe", "messgroesse": "ist", "jahre": [], "filter": [{"feld": "kostentraeger", "wert": "Beihilfe Bund"}], "gruppierung": "", "grund": ""}),
    ("Ausgaben beim Hautarzt pro Jahr",
     {"typ": "summe", "messgroesse": "betrag", "jahre": [], "filter": [{"feld": "behandler", "wert": "Hautarzt"}], "gruppierung": "jahr", "grund": ""}),
    ("Welche Rechnungen hat die PKV 2026 gekürzt?",
     {"typ": "liste", "messgroesse": "differenz", "jahre": [2026], "filter": [{"feld": "kostentraeger", "wert": "Muster PKV"}, {"feld": "abweichung", "wert": "zu_wenig"}], "gruppierung": "", "grund": ""}),
    ("Wie funktioniert die Beihilfe?",
     {"typ": "nicht_unterstuetzt", "messgroesse": "", "jahre": [], "filter": [], "gruppierung": "", "grund": "keine Frage zu den eigenen Rechnungen"}),
]

SYSTEM = f"""Du übersetzt Fragen zu Arztrechnungen und Erstattungen (private Krankenversicherung und Beihilfe) in eine Abfrage.
Du siehst keine Daten und rechnest nichts – die App führt die Abfrage aus. Antworte nur mit JSON nach dem Schema.

Heute ist der {TODAY:%d.%m.%Y}. Kostenträger: {", ".join(PAYERS)}. Kategorien: {", ".join(CATEGORIES)}.

- typ: anzahl (wie viele), summe (wie viel Geld), liste (welche, wo, zeige), beitragsrueckerstattung (lohnt sich Einreichen bei der PKV oder die Beitragsrückerstattung), ausstand (was schulden die Kostenträger noch), nicht_unterstuetzt (keine Frage zu den eigenen Rechnungen, oder eine Bitte, etwas zu tun – Grund in „grund“).
- messgroesse: betrag (Rechnungsbetrag, „ausgegeben“), soll (erwartete Erstattung), ist (tatsächlich erstattet), differenz (IST minus SOLL), offen (noch ausstehend), eigenanteil (selbst getragen); leer bei anzahl.
- jahre: Jahre der Behandlung; leer = alle. „dieses Jahr“ = {TODAY.year}.
- filter: NUR die Bedingungen, die die Frage nennt – sonst eine leere Liste. kostentraeger („Beihilfe“ = Beihilfe Bund, „PKV“/„Versicherung“ = Muster PKV); status je Kostenträger (nicht_erstattet = noch offen, also nicht eingereicht oder eingereicht; eingereicht = Erstattung steht aus); abweichung (zu_wenig: IST kleiner als SOLL); klaerungsbedarf; bezahlt (an den Arzt bezahlt); frist_bald (Antragsfrist läuft ab); kategorie; behandler (Name wie genannt); von/bis (JJJJ-MM-TT, nur bei Monaten oder Tagen).
- gruppierung: nur bei „pro“, „nach“, „je“ oder „am meisten/teuersten“.
- Nachfragen („und 2025?“, „nur die vom Zahnarzt“): die vorige Abfrage übernehmen und nur das Genannte ändern.

Beispiele:
""" + "\n".join(f"Frage: {q}\nAbfrage: {json.dumps(a, ensure_ascii=False)}" for q, a in EXAMPLES)


def flatten(a: dict) -> dict:
    """The answer as flat fields (filters as keys), for the comparison."""
    out = {k: a.get(k) for k in ("typ", "messgroesse", "jahre", "gruppierung", "grund")}
    for f in a.get("filter") or []:
        if isinstance(f, dict) and f.get("feld"):
            out[f["feld"]] = f.get("wert")
    return out


def unflatten(want: dict) -> dict:
    """An expected query in the answer format (for follow-ups whose first answer is missing)."""
    top = {"typ": "", "messgroesse": "", "jahre": [], "filter": [], "gruppierung": "", "grund": ""}
    for k, v in want.items():
        if k in top:
            top[k] = v
        elif k in FILTERS:
            top["filter"].append({"feld": k, "wert": v})
    return top


# (question, expected fields; "folge": index of the question this one follows up)
QUESTIONS = [
    ("Wie viele Rechnungen aus 2026 sind noch offen?", {"typ": "anzahl", "jahre": [2026], "status": "nicht_erstattet"}),
    ("Wo hat mir die Beihilfe zu wenig erstattet?", {"typ": "liste", "kostentraeger": "Beihilfe Bund", "abweichung": "zu_wenig"}),
    ("Was hat die PKV 2025 insgesamt erstattet?", {"typ": "summe", "messgroesse": "ist", "kostentraeger": "Muster PKV", "jahre": [2025]}),
    ("Welche Rechnungen sind noch nicht bezahlt?", {"typ": "liste", "bezahlt": "nein"}),
    ("Wie viel habe ich 2026 beim Zahnarzt ausgegeben?", {"typ": "summe", "messgroesse": "betrag", "jahre": [2026], "kategorie": "zahnarzt"}),
    ("Lohnt sich die Beitragsrückerstattung dieses Jahr?", {"typ": "beitragsrueckerstattung", "jahre": [2026]}),
    ("Was schuldet mir die Beihilfe noch?", {"typ": "ausstand", "kostentraeger": "Beihilfe Bund"}),
    ("Welche Rechnungen habe ich noch nicht eingereicht?", {"typ": "liste", "status": "nicht_eingereicht"}),
    ("Bei welchen Rechnungen läuft die Antragsfrist bald ab?", {"typ": "liste", "frist_bald": "ja"}),
    ("Wo ist noch Klärungsbedarf?", {"typ": "liste", "klaerungsbedarf": "ja"}),
    ("Wie viele Rechnungen hatte ich 2024?", {"typ": "anzahl", "jahre": [2024]}),
    ("Wie hoch war mein Eigenanteil 2025?", {"typ": "summe", "messgroesse": "eigenanteil", "jahre": [2025]}),
    ("Zeig mir alle Rechnungen von Dr. Muster.", {"typ": "liste", "behandler": "Dr. Muster"}),
    ("Wie viel hat die Beihilfe 2026 bisher erstattet, nach Monaten?", {"typ": "summe", "messgroesse": "ist", "kostentraeger": "Beihilfe Bund", "jahre": [2026], "gruppierung": "monat"}),
    ("Welche Erstattungen der PKV stehen noch aus?", {"typ": "liste", "kostentraeger": "Muster PKV", "status": "eingereicht"}),
    ("Gesamtbetrag aller Rechnungen nach Jahren", {"typ": "summe", "messgroesse": "betrag", "gruppierung": "jahr"}),
    ("Wie viel Geld ist bei der Beihilfe eingereicht, aber noch nicht erstattet?", {"typ": "summe", "messgroesse": "offen", "kostentraeger": "Beihilfe Bund", "status": "eingereicht"}),
    ("Wo hat die PKV mehr erstattet als erwartet?", {"typ": "liste", "kostentraeger": "Muster PKV", "abweichung": "zu_viel"}),
    ("Welche Physiotherapie-Rechnungen gab es 2025?", {"typ": "liste", "jahre": [2025], "kategorie": "heilmittel"}),
    ("Wie viel habe ich für Medikamente aus der Apotheke bezahlt?", {"typ": "summe", "messgroesse": "betrag", "kategorie": "apotheke"}),
    ("Was ist die Kostendämpfungspauschale?", {"typ": "nicht_unterstuetzt"}),
    ("Wie wird das Wetter morgen?", {"typ": "nicht_unterstuetzt"}),
    ("Welche Rechnungen halte ich bei der PKV zurück?", {"typ": "liste", "kostentraeger": "Muster PKV", "status": "zurueckgehalten"}),
    ("Wie viele Rechnungen aus 2025 und 2026 hat die Beihilfe vollständig erstattet?", {"typ": "anzahl", "jahre": [2025, 2026], "kostentraeger": "Beihilfe Bund", "status": "erstattet"}),
    ("Summe der Differenzen bei der Beihilfe 2025", {"typ": "summe", "messgroesse": "differenz", "kostentraeger": "Beihilfe Bund", "jahre": [2025]}),
    ("Welche Rechnungen aus dem letzten Monat?", {"typ": "liste", "von": "2026-10-01", "bis": "2026-10-31"}),
    ("Ausgaben pro Kategorie 2026", {"typ": "summe", "messgroesse": "betrag", "jahre": [2026], "gruppierung": "kategorie"}),
    ("Bei welchem Arzt war ich 2026 am teuersten?", {"typ": "summe", "messgroesse": "betrag", "jahre": [2026], "gruppierung": "behandler"}),
    ("Wie viele Rechnungen sind bezahlt, aber noch nicht eingereicht?", {"typ": "anzahl", "bezahlt": "ja", "status": "nicht_eingereicht"}),
    ("Welche Rechnungen der Radiologie Mitte sind noch offen?", {"typ": "liste", "behandler": "Radiologie Mitte", "status": "nicht_erstattet"}),
    ("Wie viel steht insgesamt noch aus?", {"typ": "ausstand"}),
    ("Wie viel hat die Beihilfe 2026 erstattet?", {"typ": "summe", "messgroesse": "ist", "kostentraeger": "Beihilfe Bund", "jahre": [2026]}),
    ("Und 2025?", {"typ": "summe", "messgroesse": "ist", "kostentraeger": "Beihilfe Bund", "jahre": [2025], "folge": 31}),
    ("Und die PKV?", {"typ": "summe", "messgroesse": "ist", "kostentraeger": "Muster PKV", "jahre": [2025], "folge": 32}),
    ("Welche Rechnungen aus 2026 sind noch nicht eingereicht?", {"typ": "liste", "jahre": [2026], "status": "nicht_eingereicht"}),
    ("Nur die vom Zahnarzt.", {"typ": "liste", "jahre": [2026], "status": "nicht_eingereicht", "kategorie": "zahnarzt", "folge": 34}),
    ("Wie viele davon gibt es?", {"typ": "anzahl", "jahre": [2026], "status": "nicht_eingereicht", "kategorie": "zahnarzt", "folge": 35}),
    ("Schreib mir eine E-Mail an die Beihilfe.", {"typ": "nicht_unterstuetzt"}),
    ("Wie viele Rechnungen mit Klärungsbedarf hat die PKV?", {"typ": "anzahl", "kostentraeger": "Muster PKV", "klaerungsbedarf": "ja"}),
    ("SOLL und IST der Beihilfe 2026", {"typ": "summe", "kostentraeger": "Beihilfe Bund", "jahre": [2026]}),
]


def ask(args, messages):
    body = {"model": args.modell, "temperature": 0, "max_tokens": 400, "messages": messages,
            "response_format": {"type": "json_schema", "json_schema": {"name": "abfrage", "strict": True, "schema": SCHEMA}}}
    resp = belegtest.call(args.server, "/chat/completions", args.schluessel, body, args.timeout)
    text = resp["choices"][0]["message"].get("content") or ""
    start, end = text.find("{"), text.rfind("}")
    try:
        got = json.loads(text[start:end + 1])  # keeps the types (jahre is a list)
    except (ValueError, json.JSONDecodeError):
        got = {"_fehler": f"keine Abfrage: {text[:80]}"}
    return got, resp.get("timings", {})


def compare(answer: dict, want: dict) -> list[str]:
    got = flatten(answer)
    wrong = []
    for k, v in want.items():
        if k == "folge":
            continue
        g = got.get(k)
        if k == "jahre":
            if sorted(v) != sorted(g if isinstance(g, list) else []):
                wrong.append(f"jahre: {g} statt {v}")
        elif str(g or "") != str(v):
            wrong.append(f"{k}: „{g or ''}“ statt „{v}“")
    # fields the question does not ask for must stay empty (a wrong extra filter changes the answer)
    for k in ("kostentraeger", "status", "abweichung", "klaerungsbedarf", "bezahlt", "frist_bald", "kategorie", "behandler", "gruppierung"):
        if k not in want and got.get(k) not in ("", None) and want.get("typ") != "nicht_unterstuetzt":
            wrong.append(f"{k}: „{got.get(k)}“ zu viel")
    return wrong


def main():
    ap = argparse.ArgumentParser(description="Testet das Übersetzen von Fragen in Abfragen (docs/PLAN-v0.5.md).")
    ap.add_argument("--server", default=belegtest.SERVER)
    ap.add_argument("--schluessel", default=belegtest.env_key())
    ap.add_argument("--modell", default=belegtest.MODEL)
    ap.add_argument("--timeout", type=int, default=120)
    ap.add_argument("--nur", default="", help="nur diese Fragen (Nummern ab 1, mit Komma)")
    ap.add_argument("--speichern", default="", help="Fragen, Antworten und Fehler als JSON in diese Datei schreiben")
    args = ap.parse_args()
    belegtest.check_health(args.server)
    only = {int(x) for x in args.nur.split(",") if x.strip()}

    answers: dict[int, dict] = {}
    ok, times, results = 0, [], []
    for i, (q, want) in enumerate(QUESTIONS):
        if only and i + 1 not in only:
            continue
        messages = [{"role": "system", "content": SYSTEM}]
        if "folge" in want:  # the conversation so far: the previous questions with their queries
            chain, j = [], want["folge"]
            while j is not None:
                chain.insert(0, j)
                j = QUESTIONS[j][1].get("folge")
            for j in chain:
                messages += [{"role": "user", "content": QUESTIONS[j][0]},
                             {"role": "assistant", "content": json.dumps(answers.get(j) or unflatten(QUESTIONS[j][1]), ensure_ascii=False)}]
        messages.append({"role": "user", "content": q})
        t0 = time.monotonic()
        try:
            got, timings = ask(args, messages)
        except urllib.error.HTTPError as e:
            if e.code in (401, 403):
                belegtest.key_refused(e)
            got, timings = {"_fehler": f"HTTP {e.code}"}, {}
        except (urllib.error.URLError, TimeoutError) as e:
            got, timings = {"_fehler": str(e)}, {}
        secs = time.monotonic() - t0
        times.append(secs)
        answers[i] = got
        wrong = ["Fehler: " + got["_fehler"]] if "_fehler" in got else compare(got, want)
        ok += not wrong
        results.append({"frage": q, "erwartet": want, "antwort": got, "sekunden": round(secs, 1), "fehler": wrong})
        print(f"{i + 1:2d}. {'✓' if not wrong else '✗'} {secs:5.1f} s  {q}")
        for w in wrong:
            print(f"        {w}")
    n = len(results)
    print(f"\nRichtig verstanden: {ok} von {n} ({100 * ok / n:.0f} %) · Zeit pro Frage: Median {statistics.median(times):.1f} s")
    if args.speichern:
        Path(args.speichern).write_text(json.dumps(results, ensure_ascii=False, indent=2) + "\n")
        print(f"Gespeichert: {args.speichern}")


if __name__ == "__main__":
    main()
