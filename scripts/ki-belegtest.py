#!/usr/bin/env python3
"""Testet, wie gut ein lokales KI-Modell Belege ausliest (Plan: KI als
Ergänzung zur Texterkennung, siehe docs/KI-TEST.md).

Schickt jeden Beleg eines Ordners an einen OpenAI-kompatiblen Server im
lokalen Netz (LM Studio, Nexa, Ollama, llama.cpp), misst die Zeit und
vergleicht die Felder mit den erwarteten Werten aus erwartet.json.

    export LLM_API_KEY=…                    # der Schlüssel des Servers
    python3 scripts/ki-belegtest.py modelle
    python3 scripts/ki-belegtest.py vorlage ~/Belegtest
    python3 scripts/ki-belegtest.py test ~/Belegtest

Belegarten (Feld „art“ je Beleg in erwartet.json): „beleg“ (Standard: die Kopfdaten einer
Rechnung wie beim Auslesen in v0.5), „rechnung“ (dazu die Positionen), „bescheid“ (Bescheid
oder Leistungsabrechnung mit Zeilen, Hinweisen und Abzügen), „kostenplan“ und „verordnung“
(Plan v0.6, Abgleich). Erfundene Testbelege dafür erzeugt scripts/ki-testbelege.py.

Voreingestellt ist der Server im Heimnetz (llama.cpp auf dem Galaxy Book,
http://192.168.178.38:8080/v1, Modell qwen3-vl-8b); --server, --modell und
--schluessel ändern das. Der Schlüssel kommt sonst aus LLM_API_KEY oder aus
API_KEY in der Datei .env im Wurzelordner des Repositorys (nicht versioniert).

Ein Beleg ist eine Bilddatei (jpg, png, webp), eine PDF-Datei (jede Seite
ein Bild) oder ein Unterordner (seine Dateien sind die Seiten, nach Namen
sortiert). Die Belege verlassen den Rechner nur in Richtung des angegebenen
Servers; die Ergebnisse landen im Testordner (ki-ergebnisse/), nie im
Repository. Nur Python-Standardbibliothek; pdftoppm (poppler) für PDFs,
ImageMagick (magick) zum Verkleinern großer Bilder, beides optional.
"""

import argparse
import base64
import itertools
import json
import os
import re
import shutil
import statistics
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request
from datetime import datetime
from pathlib import Path

FIELDS = {
    "behandler": "Name der Praxis, des Arztes, der Apotheke oder des Rechnungsstellers",
    "rechnungsdatum": "Datum der Rechnung (auf Quittungen und Kassenbons: das Kaufdatum)",
    "behandlung_von": "erster Behandlungstag – aus „Behandlungszeitraum“/„Leistungszeitraum“, sonst das früheste Datum der Positionen",
    "behandlung_bis": "letzter Behandlungstag – aus „Behandlungszeitraum“/„Leistungszeitraum“, sonst das späteste Datum der Positionen",
    "betrag": "Gesamtbetrag der Rechnung in Euro – der Endbetrag („Rechnungsbetrag“, „Gesamtbetrag“, auf Kassenbons „Summe“), "
              "auch wenn schon bezahlt; nicht ein Übertrag oder eine Zwischensumme",
    "rechnungsnummer": "Rechnungsnummer",
    "iban": "IBAN für die Überweisung",
    "zahlungsempfaenger": "Empfänger der Zahlung, falls nicht der Behandler (z. B. Abrechnungsstelle)",
}
# The v0.6 types are transcribed: the model copies what is printed (dates, amounts, table rows with
# their cells) and the script interprets it the way the app will – the model is bad at converting
# dates (it wrote 2022 for 2026) and at mapping similar amount columns to field names (it left
# them empty), but copies them reliably (test of 2026-11-03, docs/PLAN-v0.6.md).
SUMS = "die Summen- und Betragszeilen unter der Tabelle, je Zeile „Bezeichnung | Betrag“"
TYPES = {
    "beleg": {"was": "den Beleg", "fields": FIELDS, "lists": {}, "max_tokens": 800},
    "rechnung": {
        "was": "die Rechnung",
        "kopf": {
            "behandler": "Name der Praxis, des Arztes oder Therapeuten, der behandelt hat",
            "zahlungsempfaenger": "Empfänger der Zahlung, falls nicht der Behandler (z. B. Abrechnungsstelle)",
            "rechnungsnummer": "Rechnungsnummer",
            "rechnungsdatum": "Rechnungsdatum",
            "behandlungszeitraum": "Behandlungs- bzw. Leistungszeitraum, wie gedruckt (z. B. „08.09.2026 – 29.09.2026“)",
            "iban": "IBAN für die Überweisung",
        },
        "tabelle": "die Tabelle der Leistungen (Positionen) über alle Seiten – ohne Überträge und Zwischensummen und "
                   "ohne die Zeilen einer beigefügten Laborrechnung",
        "summen": SUMS + " (z. B. „Rechnungsbetrag“, „Honorar“, „Material- und Laborkosten“) – ohne Überträge",
        "fields": ["behandler", "zahlungsempfaenger", "rechnungsnummer", "rechnungsdatum", "behandlung_von",
                   "behandlung_bis", "iban", "betrag", "auslagen"],
        "lists": ["positionen"],
    },
    "kostenplan": {
        "was": "den Heil- und Kostenplan bzw. Kostenvoranschlag",
        "kopf": {"behandler": "Name der Praxis bzw. der Zahnärztin oder des Zahnarztes", "datum": "Datum des Kostenplans"},
        "tabelle": "die Tabelle der geplanten Leistungen",
        "summen": SUMS + " (z. B. „Honorar“, „Material- und Laborkosten“, „Gesamtkosten“, „Eigenanteil“)",
        "fields": ["behandler", "datum", "honorar", "material_labor", "gesamt", "eigenanteil"],
        "lists": ["positionen"],
    },
    "bescheid": {
        "was": "den Bescheid bzw. die Leistungsabrechnung",
        "kopf": {
            "kostentraeger": "Absender: Beihilfestelle oder Krankenversicherung",
            "bescheiddatum": "Datum des Bescheids bzw. der Abrechnung",
            "antrag_vom": "Datum des Antrags bzw. der Einreichung („Ihr Antrag vom“)",
        },
        "tabelle": "die Tabelle der abgerechneten Rechnungen",
        "summen": SUMS + " (Summe, Abzüge wie Kostendämpfungspauschale oder Selbstbehalt, Auszahlungsbetrag)",
        "hinweise": "die Hinweise bzw. Bemerkungen, auf die die Zeilen verweisen",
        "fields": ["kostentraeger", "bescheiddatum", "antrag_vom", "auszahlung"],
        "lists": ["zeilen", "hinweise", "abzuege"],
    },
    "verordnung": {
        "was": "die Verordnung (Rezept für Heilmittel)",
        "kopf": {
            "verordner": "verordnende Ärztin bzw. verordnender Arzt",
            "datum": "Ausstellungsdatum",
            "frequenz": "Häufigkeit (z. B. „2 × wöchentlich“)",
            "diagnose": "Diagnose als Text",
            "icd": "ICD-10-Code",
            "gueltig_bis": "spätester Behandlungsbeginn bzw. gültig bis",
        },
        "zeilen": "jede verordnete Zeile, wie gedruckt (z. B. „6 × Krankengymnastik“) – alle",
        "fields": ["verordner", "datum", "frequenz", "diagnose", "icd", "gueltig_bis"],
        "lists": ["heilmittel"],
    },
}
AMOUNT_FIELDS = {"betrag", "auslagen", "auszahlung", "rechnungsbetrag", "erstattungsfaehig", "erstattung", "honorar",
                 "material_labor", "gesamt", "eigenanteil"}
DATE_FIELDS = {"rechnungsdatum", "behandlung_von", "behandlung_bis", "datum", "bescheiddatum", "antrag_vom", "gueltig_bis"}
CODE_FIELDS = {"iban", "rechnungsnummer", "ziffer", "zahn", "icd", "nr"}
NUMBER_FIELDS = {"anzahl", "satz"}
IMAGE_TYPES = {".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png", ".webp": "image/webp"}
EXPECTED = "erwartet.json"
SERVER = "http://192.168.178.38:8080/v1"
MODEL = "qwen3-vl-8b"

SYSTEM = ("Du liest Belege der deutschen privaten Krankenversicherung und Beihilfe aus: Arztrechnungen, "
          "Zahnarztrechnungen, Rezepte, Rechnungen von Apotheken und Therapeuten, Bescheide, Kostenpläne und "
          "Verordnungen. Antworte nur mit JSON.")
RULES = ("Regeln: Datumsangaben als JJJJ-MM-TT. Beträge mit Punkt als Dezimaltrennzeichen, ohne Währung "
         "(z. B. 1234.56). IBAN ohne Leerzeichen. Steht ein Feld nicht auf dem Beleg, gib eine leere "
         "Zeichenkette zurück – nichts schätzen oder erfinden.")
COPY_RULES = ("Regeln: Schreibe alles so ab, wie es gedruckt ist – Datumsangaben, Beträge und Nummern unverändert "
              "(z. B. „12.10.2026“, „1.234,56 €“), nichts umrechnen. Tabellenzeilen: die Zellen von links nach rechts "
              "mit „ | “ getrennt, eine leere Zelle als leeres Feld („a |  | c“); Text, der in einer Zelle über zwei "
              "Druckzeilen läuft, gehört in dieselbe Zelle. Steht etwas nicht auf dem Beleg, gib eine leere "
              "Zeichenkette bzw. eine leere Liste zurück – nichts schätzen oder erfinden.")


def copied(art: str) -> bool:
    """Is the type transcribed (v0.6) rather than read into fields (v0.5)?"""
    return "kopf" in TYPES[art]


def prompt(art: str) -> str:
    t = TYPES[art]
    lines = [f"Lies {t['was']} (alle Seiten zusammen) und gib diese Felder als JSON-Objekt zurück:"]
    if not copied(art):
        lines += [f"- {k}: {v}" for k, v in t["fields"].items()]
        return "\n".join(lines) + "\n\n" + RULES
    lines += [f"- {k}: {v}" for k, v in t["kopf"].items()]
    if "tabelle" in t:
        lines += [f"- spalten: die Spaltenüberschriften der Tabelle – gemeint ist {t['tabelle']} –, mit „ | “ getrennt",
                  "- zeilen: Liste – jede Zeile dieser Tabelle, die Zellen mit „ | “ getrennt"]
    else:
        lines.append(f"- zeilen: Liste – {t['zeilen']}")
    if "summen" in t:
        lines.append(f"- summen: Liste – {t['summen']}")
    if "hinweise" in t:
        lines.append(f"- hinweise: Liste – {t['hinweise']}; je Eintrag nr (Nummer oder Kürzel wie in der Zeile) "
                     "und text (der Text, vollständig)")
    return "\n".join(lines) + "\n\n" + COPY_RULES


def schema(art: str) -> dict:
    def obj(fields):
        return {"type": "object", "properties": fields, "required": list(fields), "additionalProperties": False}
    t = TYPES[art]
    if not copied(art):
        return obj({k: {"type": "string"} for k in t["fields"]})
    texts = {"type": "array", "items": {"type": "string"}}
    props = {k: {"type": "string"} for k in t["kopf"]}
    if "tabelle" in t:
        props["spalten"] = {"type": "string"}
    props["zeilen"] = texts
    if "summen" in t:
        props["summen"] = texts
    if "hinweise" in t:
        props["hinweise"] = {"type": "array", "items": obj({"nr": {"type": "string"}, "text": {"type": "string"}})}
    return obj(props)


# --- Abschriften deuten (so, wie es später die App tut) ----------------------------------------

DATE = re.compile(r"(\d{1,2})\.\s?(\d{1,2})\.\s?(\d{4}|\d{2})(?!\d)|(\d{4})-(\d{2})-(\d{2})")
AMOUNT = re.compile(r"\d{1,3}(?:\.\d{3})+,\d{2}|\d+,\d{2}|\d+\.\d{2}(?!\d)")
COLUMNS = {  # column kind ← words in the header, checked in this order
    "bescheid": [("erstattungsfaehig", ["fähig", "anerkannt"]), ("rechnungsbetrag", ["rechnungs"]), ("satz", ["satz", "%"]),
                 ("erstattung", ["beihilfe", "erstattung"]), ("hinweis_nr", ["hinw", "bem"]),
                 ("rechnungsdatum", ["vom", "datum"]), ("behandler", ["leistungserbringer", "behandler", "art"])],
    "": [("datum", ["datum"]), ("zahn", ["zahn", "region"]), ("faktor", ["faktor"]), ("einzel", ["einzel"]),
         ("anzahl", ["anz", "menge"]), ("text", ["leistung", "bezeichnung", "text"]),
         ("betrag", ["betrag", "gesamt", "eur", "€"]), ("ziffer", ["nr", "ziffer", "goä", "goz", "pos", "geb"])],
}


def dates(s: str) -> list[str]:
    out = []
    for m in DATE.finditer(s or ""):
        if m.group(4):
            out.append(f"{m.group(4)}-{m.group(5)}-{m.group(6)}")
        else:
            y = m.group(3) if len(m.group(3)) == 4 else "20" + m.group(3)
            out.append(f"{y}-{int(m.group(2)):02d}-{int(m.group(1)):02d}")
    return out


def first_date(s: str) -> str:
    return (dates(s) or [""])[0]


def amount_text(s: str) -> str:
    """Der letzte Betrag im Text als „1234.56“ (auch aus „– 100,00“, „516,70 €“)."""
    found = AMOUNT.findall(s or "")
    if not found:
        return ""
    a = found[-1]
    return a.replace(".", "").replace(",", ".") if "," in a else a


def column(art: str, header: str) -> str:
    h = header.strip().casefold()
    for kind, words in COLUMNS["bescheid" if art == "bescheid" else ""]:
        if any(w in h for w in words):
            return kind
    return ""


def fits(kind: str, cell: str) -> bool:
    c = cell.strip()
    if kind in ("datum", "rechnungsdatum"):
        return bool(dates(c))
    if kind in ("betrag", "einzel", "rechnungsbetrag", "erstattungsfaehig", "erstattung"):
        return bool(AMOUNT.fullmatch(c.replace("€", "").replace("EUR", "").strip()))
    if kind == "anzahl":
        return bool(re.fullmatch(r"\d+", c))
    if kind in ("faktor", "satz"):
        return bool(re.fullmatch(r"\d+([.,]\d+)?\s*%?", c))
    if kind == "zahn":
        return c == "" or bool(re.fullmatch(r"\d{2}([ ,/-]+\d{2})*|OK|UK|OK/UK", c))
    if kind == "ziffer":
        return bool(re.fullmatch(r"[A-ZÄÁa-z]{0,2}\d[\w.]*", c))
    if kind == "hinweis_nr":
        return c == "" or bool(re.fullmatch(r"[\w ,]+", c))
    return bool(re.search(r"[A-Za-zÄÖÜäöü]", c))


def align(kinds: list[str], cells: list[str]) -> list[tuple[str, str]]:
    """Zellen den Spalten zuordnen; fehlen leere Zellen, kommen sie dorthin, wo die übrigen am besten passen."""
    missing = len(kinds) - len(cells)
    if missing <= 0 or missing > 2:
        return list(zip(kinds, cells))
    best, score = cells, -1
    for gaps in itertools.combinations(range(len(kinds)), missing):
        rest = iter(cells)
        filled = ["" if i in gaps else next(rest) for i in range(len(kinds))]
        s = sum(fits(k, c) for k, c in zip(kinds, filled) if k)
        if s > score:
            best, score = filled, s
    return list(zip(kinds, best))


def interpret(art: str, a: dict) -> dict:
    """Aus der Abschrift die Felder, die verglichen werden – Datumsangaben, Beträge, Spalten."""
    get = a.get
    out = {k: get(k, "") for k in TYPES[art]["kopf"]}
    for k in ("rechnungsdatum", "datum", "bescheiddatum", "antrag_vom", "gueltig_bis"):
        if k in out:
            out[k] = first_date(out[k])
    if art == "verordnung":
        items = []
        for line in get("zeilen") or []:
            m = re.match(r"\s*(\d+)\s*[x×]?\s*(.+)", line)
            items.append({"anzahl": m.group(1), "heilmittel": m.group(2)} if m else {"anzahl": "", "heilmittel": line})
        return {**out, "heilmittel": items}

    kinds = [column(art, h) for h in (get("spalten") or "").split("|")]
    dental = "zahn" in kinds
    rows, last_date = [], ""
    for line in get("zeilen") or []:
        row = {}
        for kind, cell in align(kinds, [c.strip() for c in line.split("|")]):
            if not kind:
                continue
            if kind in ("datum", "rechnungsdatum"):
                cell = first_date(cell)
            elif kind in ("betrag", "rechnungsbetrag", "erstattungsfaehig", "erstattung"):
                cell = amount_text(cell)
            elif kind == "ziffer" and dental:
                cell = re.sub(r"^[AÁÀÂ](?=\d)", "Ä", cell)  # GOÄ number in a dental invoice: Ä925a read as A925a
            row[kind] = cell
        if art != "bescheid":
            row["datum"] = row.get("datum") or last_date  # date printed once per day
            last_date = row["datum"]
        rows.append(row)

    sums = [(label.strip().casefold(), amount_text(value)) for label, _, value in (x.rpartition("|") for x in get("summen") or [])]
    sums = [(label, value) for label, value in sums if value and "übertrag" not in label]

    def pick(pattern: str, prefer: str = "") -> str:
        hits = [v for label, v in sums if re.search(prefer, label)] if prefer else []
        hits = hits or [v for label, v in sums if re.search(pattern, label)]
        return hits[-1] if hits else ""

    if art == "bescheid":
        return {**out, "zeilen": rows, "hinweise": get("hinweise") or [],
                "auszahlung": pick(r"auszahl|überweis|zahlbetrag"),
                "abzuege": [{"text": label, "betrag": v} for label, v in sums
                            if re.search(r"abzüglich|abzug|selbstbehalt|pauschale|eigenbehalt", label)]}
    if art == "kostenplan":
        return {**out, "positionen": rows, "honorar": pick(r"honorar"), "material_labor": pick(r"material|labor"),
                "gesamt": pick(r"gesamt"), "eigenanteil": pick(r"eigenanteil") or "0"}
    period = dates(out.pop("behandlungszeitraum", "")) or sorted(r["datum"] for r in rows if r.get("datum"))
    return {**out, "positionen": rows, "behandlung_von": period[0] if period else "", "behandlung_bis": period[-1] if period else "",
            "betrag": pick(r"rechnungsbetrag|gesamt|endbetrag|zu zahlen|summe", r"rechnungsbetrag|endbetrag|zu zahlen"),
            "auslagen": pick(r"material|labor|auslagen") or "0"}


# --- Belege finden und vorbereiten ---------------------------------------------------------

def env_key() -> str:
    """The API key: LLM_API_KEY, else API_KEY (or LLM_API_KEY) from .env in the repository root."""
    if os.environ.get("LLM_API_KEY"):
        return os.environ["LLM_API_KEY"]
    env = Path(__file__).resolve().parent.parent / ".env"
    if env.is_file():
        for line in env.read_text().splitlines():
            name, _, value = line.strip().partition("=")
            if name.strip() in ("API_KEY", "LLM_API_KEY") and value.strip():
                return value.strip().strip('"').strip("'")
    return ""


def natural_key(p: Path):
    return [int(t) if t.isdigit() else t.lower() for t in re.split(r"(\d+)", p.name)]


def documents(folder: Path) -> dict[str, list[Path]]:
    """Name → Seiten (Dateien) aller Belege im Ordner."""
    docs = {}
    for p in sorted(folder.iterdir(), key=natural_key):
        if p.name.startswith(".") or p.name in (EXPECTED, "ki-ergebnisse"):
            continue
        if p.is_dir():
            pages = [f for f in sorted(p.iterdir(), key=natural_key) if f.suffix.lower() in IMAGE_TYPES or f.suffix.lower() == ".pdf"]
            if pages:
                docs[p.name] = pages
        elif p.suffix.lower() in IMAGE_TYPES or p.suffix.lower() == ".pdf":
            docs[p.name] = [p]
    return docs


def page_images(pages: list[Path], tmp: Path, max_side: int) -> list[Path]:
    """Die Seiten als Bilddateien: PDFs als PNG je Seite, große Bilder verkleinert."""
    out = []
    for i, p in enumerate(pages):
        if p.suffix.lower() == ".pdf":
            if not shutil.which("pdftoppm"):
                sys.exit(f"{p.name}: für PDFs wird pdftoppm gebraucht (Paket poppler-utils)")
            prefix = tmp / f"pdf{i}"
            subprocess.run(["pdftoppm", "-r", "150", "-png", str(p), str(prefix)], check=True)
            out += sorted(tmp.glob(f"pdf{i}-*.png"), key=natural_key)
        else:
            out.append(p)
    if max_side and shutil.which("magick"):
        small = []
        for i, p in enumerate(out):
            target = tmp / f"seite{i}.jpg"
            # ">" verkleinert nur, was größer ist
            subprocess.run(["magick", str(p), "-auto-orient", "-resize", f"{max_side}x{max_side}>", "-quality", "90", str(target)], check=True)
            small.append(target)
        out = small
    return out


def data_url(p: Path) -> str:
    kind = IMAGE_TYPES.get(p.suffix.lower(), "image/png")
    return f"data:{kind};base64,{base64.b64encode(p.read_bytes()).decode()}"


# --- Server ------------------------------------------------------------------------------------

def call(server: str, path: str, key: str, body: dict | None, timeout: int) -> dict:
    req = urllib.request.Request(server.rstrip("/") + path, method="POST" if body is not None else "GET",
                                 data=json.dumps(body).encode() if body is not None else None,
                                 headers={"Content-Type": "application/json", **({"Authorization": f"Bearer {key}"} if key else {})})
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        return json.loads(resp.read())


def key_refused(e: urllib.error.HTTPError):
    sys.exit(f"Der Server lehnt ab (HTTP {e.code}): API-Schlüssel fehlt oder ist falsch – --schluessel, LLM_API_KEY oder API_KEY in .env.")


def check_health(server: str):
    """llama.cpp answers GET /health without a key; other servers may not have it (then nothing to check)."""
    url = re.sub(r"/v1/?$", "", server.rstrip("/")) + "/health"
    try:
        with urllib.request.urlopen(url, timeout=5) as resp:
            body = resp.read().decode(errors="replace")
        if '"ok"' not in body:
            print(f"Hinweis: {url} meldet {body.strip()[:80]}")
    except urllib.error.HTTPError as e:
        if e.code != 404:
            print(f"Hinweis: {url} antwortet mit HTTP {e.code}")
    except (urllib.error.URLError, TimeoutError) as e:
        sys.exit(f"Server nicht erreichbar ({url}): {e}. Läuft das Galaxy Book, am Netzteil, im WLAN?")


def ask(args, art: str, images: list[Path], use_schema: bool) -> tuple[dict, dict]:
    content = [{"type": "text", "text": prompt(art)}] + [{"type": "image_url", "image_url": {"url": data_url(p)}} for p in images]
    body = {"model": args.modell, "temperature": 0, "max_tokens": TYPES[art].get("max_tokens", 4096),
            "messages": [{"role": "system", "content": SYSTEM}, {"role": "user", "content": content}]}
    if use_schema:
        body["response_format"] = {"type": "json_schema", "json_schema": {"name": art, "strict": True, "schema": schema(art)}}
    resp = call(args.server, "/chat/completions", args.schluessel, body, args.timeout)
    text = resp["choices"][0]["message"].get("content") or ""
    # llama.cpp adds "timings" (tokens per second while reading and writing)
    return parse_json(text), {**(resp.get("usage") or {}), **({"timings": resp["timings"]} if "timings" in resp else {})}


def parse_json(text: str) -> dict:
    """Das JSON-Objekt aus der Antwort, auch mit ```json-Rahmen oder Text davor."""
    start, end = text.find("{"), text.rfind("}")
    if start < 0 or end < start:
        return {"_antwort": text}
    try:
        data = json.loads(text[start:end + 1])
    except json.JSONDecodeError:
        return {"_antwort": text}
    return clean(data) if isinstance(data, dict) else {"_antwort": text}


def clean(v):
    """Werte als Zeichenketten (Listen und Objekte bleiben)."""
    if isinstance(v, dict):
        return {k: clean(x) for k, x in v.items()}
    if isinstance(v, list):
        return [clean(x) for x in v]
    return "" if v is None else str(v).strip()


# --- Vergleich ---------------------------------------------------------------------------------

def norm_amount(v: str):
    s = re.sub(r"[€\s]|EUR", "", v, flags=re.I)
    if re.fullmatch(r"-?\d{1,3}(\.\d{3})*,\d{1,2}", s) or re.fullmatch(r"-?\d+,\d{1,2}", s):
        s = s.replace(".", "").replace(",", ".")
    s = s.replace(",", "")
    try:
        return round(float(s) * 100)
    except ValueError:
        return v


def norm_date(v: str):
    v = v.strip()
    if m := re.fullmatch(r"(\d{4})-(\d{1,2})-(\d{1,2})", v):
        y, mo, d = m.groups()
    elif m := re.fullmatch(r"(\d{1,2})\.(\d{1,2})\.(\d{2,4})", v):
        d, mo, y = m.groups()
        y = ("20" + y) if len(y) == 2 else y
    else:
        return v
    return f"{int(y):04d}-{int(mo):02d}-{int(d):02d}"


def norm_text(v: str) -> str:
    return re.sub(r"[^0-9a-zäöüß]+", " ", v.casefold()).strip()


def norm_number(v: str):
    m = re.search(r"\d+(?:[.,]\d+)?", v)
    return float(m.group().replace(",", ".")) if m else v


def codes(v: str) -> list[str]:
    return sorted(re.findall(r"[A-Za-z]*\d+", v))


def same(field: str, got, want: str) -> bool:
    if not isinstance(got, str):
        return False
    if field in AMOUNT_FIELDS:
        if norm_amount(want) == 0:  # „0“ erwartet: leer ist auch richtig (nichts erfunden)
            return got == "" or norm_amount(got) == 0
        return norm_amount(got) == norm_amount(want)
    if field in DATE_FIELDS:
        return norm_date(got) == norm_date(want)
    if field in CODE_FIELDS:
        return re.sub(r"[^0-9A-ZÄÖÜ]", "", got.upper()) == re.sub(r"[^0-9A-ZÄÖÜ]", "", want.upper())
    if field in NUMBER_FIELDS:
        return norm_number(got) == norm_number(want)
    if field == "hinweis_nr":
        return codes(got) == codes(want)
    if field == "frequenz":
        return re.findall(r"\d+", got) == re.findall(r"\d+", want) and all("wöch" in x.casefold() for x in (got, want))
    g, w = norm_text(got), norm_text(want)
    if field == "text":  # langer Text: die meisten Wörter müssen da sein
        words = w.split()
        return bool(words) and sum(x in g.split() for x in words) >= 0.7 * len(words)
    return bool(g) and (g == w or w in g or g in w)  # "Dr. med. Petra Beispiel" passt zu "Petra Beispiel"


def match_list(got: list, want: list) -> tuple[list, list, list]:
    """Ordnet erwartete und gelesene Einträge einander zu (die meisten gleichen Felder, mindestens die Hälfte).
    Gibt (Paare erwartet→gelesen oder None, Liste der überzähligen gelesenen Einträge, Feldnamen) zurück."""
    got = [g for g in got if isinstance(g, dict)]
    used, pairs = set(), []
    for w in want:
        keys = [k for k in w if w[k] != ""]
        best, score = None, 0
        for i, g in enumerate(got):
            if i in used:
                continue
            s = sum(same(k, g.get(k, ""), w[k]) for k in keys)
            if s > score:
                best, score = i, s
        if best is not None and score * 2 >= len(keys):
            used.add(best)
            pairs.append((w, got[best]))
        else:
            pairs.append((w, None))
    return pairs, [g for i, g in enumerate(got) if i not in used]


def amount(v) -> int:
    n = norm_amount(v) if isinstance(v, str) and v else 0
    return n if isinstance(n, int) else 0


def checksum(art: str, got: dict) -> str:
    """Die Summenprüfung, die später die App rechnet – mit den gelesenen Werten."""
    def euro(c):
        return f"{c / 100:.2f}".replace(".", ",")

    def total(items, key):
        return sum(amount(x.get(key, "")) for x in items if isinstance(x, dict))

    if art == "rechnung":
        pos, extra, want = total(got.get("positionen") or [], "betrag"), amount(got.get("auslagen", "")), amount(got.get("betrag", ""))
        return f"Positionen {euro(pos)} + Auslagen {euro(extra)} {'=' if pos + extra == want else '≠'} Betrag {euro(want)}"
    if art == "bescheid":
        lines, cut = total(got.get("zeilen") or [], "erstattung"), total(got.get("abzuege") or [], "betrag")
        want = amount(got.get("auszahlung", ""))
        return f"Erstattungen {euro(lines)} − Abzüge {euro(cut)} {'=' if lines - cut == want else '≠'} Auszahlung {euro(want)}"
    if art == "kostenplan":
        pos, fee, lab, want = (total(got.get("positionen") or [], "betrag"), amount(got.get("honorar", "")),
                               amount(got.get("material_labor", "")), amount(got.get("gesamt", "")))
        return (f"Positionen {euro(pos)} {'=' if pos == fee else '≠'} Honorar {euro(fee)}; "
                f"+ Material/Labor {euro(lab)} {'=' if fee + lab == want else '≠'} Gesamt {euro(want)}")
    return ""


# --- Befehle -----------------------------------------------------------------------------------

def check_folder(folder: Path, force: bool):
    if not folder.is_dir():
        sys.exit(f"{folder}: kein Ordner")
    repo = Path(__file__).resolve().parent.parent
    if (folder.resolve() == repo or repo in folder.resolve().parents) and not force:
        sys.exit("Der Testordner liegt im Repository – echte Belege gehören nicht ins Repo. Bitte einen Ordner außerhalb wählen.")


def cmd_models(args):
    check_health(args.server)
    try:
        resp = call(args.server, "/models", args.schluessel, None, 30)
    except urllib.error.HTTPError as e:
        if e.code in (401, 403):
            key_refused(e)
        sys.exit(f"{args.server}/models: HTTP {e.code}")
    for m in resp.get("data", []):
        print(m.get("id"))


def cmd_template(args):
    folder = Path(args.ordner).expanduser()
    check_folder(folder, args.trotzdem)
    path = folder / EXPECTED
    data = json.loads(path.read_text()) if path.exists() else {"dokumente": {}}
    t = TYPES[args.art]
    added = 0
    for name in documents(folder):
        if name not in data["dokumente"]:
            data["dokumente"][name] = {**({"art": args.art} if args.art != "beleg" else {}), **{k: "" for k in t["fields"]},
                                       **{k: [] for k in t["lists"]}}
            added += 1
    path.write_text(json.dumps(data, ensure_ascii=False, indent=2) + "\n")
    print(f"{path}: {added} Beleg(e) ergänzt, {len(data['dokumente'])} insgesamt. Bitte die erwarteten Werte eintragen "
          "(leere Felder werden nicht verglichen).")


def short(item: dict) -> str:
    return " · ".join(str(v)[:30] for v in item.values() if v)


def compare(art: str, got: dict, want: dict, stats: dict) -> tuple[int, int, list[str], list[str]]:
    """Vergleicht eine Antwort mit den erwarteten Werten. Zählt je Feld in stats; gibt (richtig, verglichen,
    Zusammenfassung der Listen, Abweichungen) zurück."""
    def count(key, ok):
        stats.setdefault(key, [0, 0])
        stats[key][0] += ok
        stats[key][1] += 1

    right = compared = 0
    lists, wrong = [], []
    for k in TYPES[art]["fields"]:
        if want.get(k, "") == "":
            continue
        ok = same(k, got.get(k, ""), want[k])
        count(f"{art}.{k}", ok)
        right, compared = right + ok, compared + 1
        if not ok:
            wrong.append(f"{k}: „{got.get(k, '')}“ statt „{want[k]}“")
    for name in TYPES[art]["lists"]:
        if name not in want:
            continue
        items = got.get(name) if isinstance(got.get(name), list) else []
        pairs, extra = match_list(items, want[name])
        found = sum(g is not None for _, g in pairs)
        count(f"{art}.{name} (Einträge gefunden)", 0)
        stats[f"{art}.{name} (Einträge gefunden)"] = [stats[f"{art}.{name} (Einträge gefunden)"][0] + found,
                                                      stats[f"{art}.{name} (Einträge gefunden)"][1] - 1 + len(pairs)]
        stats.setdefault(f"{art}.{name} (zusätzlich gelesen)", [0, 0])[0] += len(extra)
        for i, (w, g) in enumerate(pairs):
            for k, v in w.items():
                if v == "":
                    continue
                ok = g is not None and same(k, g.get(k, ""), v)
                count(f"{art}.{name}.{k}", ok)
                right, compared = right + ok, compared + 1
                if g is not None and not ok:
                    wrong.append(f"{name}[{i + 1}] {k}: „{g.get(k, '')}“ statt „{v}“")
            if g is None:
                wrong.append(f"{name}[{i + 1}] fehlt: {short(w)}")
        wrong += [f"{name}: zusätzlich gelesen: {short(g)}" for g in extra]
        lists.append(f"{name} {found}/{len(pairs)}" + (f" +{len(extra)}" if extra else ""))
    return right, compared, lists, wrong


def report(name: str, art: str, raw: dict, want: dict, stats: dict, head: str, max_lines: int) -> dict:
    """Deutet und vergleicht eine Antwort, gibt die Zeile des Belegs aus und liefert das Ergebnis."""
    got = interpret(art, raw) if copied(art) and raw else raw
    right, compared, lists, wrong = compare(art, got, want, stats)
    score = f"{right}/{compared}" if compared else "–"
    print(f"{name:<30} {art:<10} {head} {score:>7}  {'  '.join(lists)}")
    for w in wrong[:max_lines]:
        print(f"    {w}")
    if len(wrong) > max_lines:
        print(f"    … und {len(wrong) - max_lines} weitere")
    check = checksum(art, got) if raw else ""
    if check:
        print(f"    Prüfsumme: {check}")
    return {"richtig": right, "verglichen": compared, "pruefsumme": check, "abweichungen": wrong, "gedeutet": got}


def summary(results: list[dict], stats: dict):
    ok = [r for r in results if not r["fehler"]]
    total = sum(r["verglichen"] for r in ok)
    if total:
        print(f"Felder richtig: {sum(r['richtig'] for r in ok)} von {total} ({100 * sum(r['richtig'] for r in ok) / total:.0f} %)")
        for k, (r, c) in sorted(stats.items()):
            if c:
                print(f"  {k:<45} {r}/{c}")
            elif r:
                print(f"  {k:<45} {r}")
    checks = [r["pruefsumme"] for r in ok if r["pruefsumme"]]
    if checks:
        print(f"Prüfsummen mit den gelesenen Werten stimmig: {sum('≠' not in c for c in checks)} von {len(checks)}")
    if len(ok) < len(results):
        print(f"{len(results) - len(ok)} Beleg(e) mit Fehler (Server nicht erreichbar, Zeitüberschreitung …)")


def cmd_test(args):
    folder = Path(args.ordner).expanduser()
    check_folder(folder, args.trotzdem)
    docs = documents(folder)
    if args.nur:
        docs = {k: v for k, v in docs.items() if any(n in k for n in args.nur)}
    if not docs:
        sys.exit(f"{folder}: keine Belege gefunden")
    expected = json.loads((folder / EXPECTED).read_text())["dokumente"] if (folder / EXPECTED).exists() else {}
    use_schema = not args.ohne_schema
    if not args.schluessel:
        print("Hinweis: kein API-Schlüssel (--schluessel, LLM_API_KEY oder API_KEY in .env) – der Server im Heimnetz verlangt einen.")
    check_health(args.server)
    results, stats = [], {}
    print(f"{len(docs)} Belege → {args.server} ({args.modell})\n")
    for n, (name, pages) in enumerate(docs.items()):
        art = expected.get(name, {}).get("art", "beleg")
        with tempfile.TemporaryDirectory() as tmp:
            images = page_images(pages, Path(tmp), args.max_seite)
            t0 = time.monotonic()
            try:
                try:
                    raw, usage = ask(args, art, images, use_schema)
                except urllib.error.HTTPError as e:
                    if e.code in (401, 403):
                        key_refused(e)
                    if not use_schema or e.code not in (400, 422, 500):
                        raise
                    use_schema = False  # der Server kennt kein JSON-Schema: ohne weiter
                    print("  (Server lehnt response_format ab – weiter ohne JSON-Schema)")
                    raw, usage = ask(args, art, images, use_schema)
                error = ""
            except (urllib.error.URLError, TimeoutError, KeyError, json.JSONDecodeError) as e:
                raw, usage, error = {}, {}, str(e)
            secs = time.monotonic() - t0
        want = {} if error else expected.get(name, {})  # a failed request is reported, not compared
        head = f"{len(images)} S. {secs:6.1f} s {usage.get('completion_tokens', 0):5d} Tok." + (f"  FEHLER: {error}" if error else "")
        r = report(name, art, raw, want, stats, head, args.zeilen)
        results.append({"beleg": name, "art": art, "seiten": len(images), "sekunden": round(secs, 1), "erstes": n == 0,
                        **r, "antwort": raw, "usage": usage, "fehler": error})

    ok = [r for r in results if not r["fehler"]]
    warm = [r for r in ok if not r["erstes"]] or ok
    print()
    if warm:
        per_page = [r["sekunden"] / r["seiten"] for r in warm]
        print(f"Zeit pro Seite: Median {statistics.median(per_page):.1f} s, Mittel {statistics.mean(per_page):.1f} s"
              + (f" (ohne den ersten Beleg, der das Modell lädt: {results[0]['sekunden']:.1f} s)" if len(ok) > 1 else ""))
    timings = [r["usage"]["timings"] for r in warm if "timings" in r["usage"]]
    if timings:
        read = statistics.median(t.get("prompt_per_second", 0) for t in timings)
        write = statistics.median(t.get("predicted_per_second", 0) for t in timings)
        print(f"Lesen {read:.0f} Token/s, Schreiben {write:.0f} Token/s")
    summary(results, stats)

    out = folder / "ki-ergebnisse"
    out.mkdir(exist_ok=True)
    file = out / f"{datetime.now():%Y%m%d-%H%M%S}-{re.sub(r'[^A-Za-z0-9._-]+', '_', args.modell)}.json"
    file.write_text(json.dumps({"server": args.server, "modell": args.modell, "json_schema": use_schema, "max_seite": args.max_seite,
                                "ergebnisse": results}, ensure_ascii=False, indent=2) + "\n")
    print(f"\nAntworten gespeichert: {file}")


def cmd_evaluate(args):
    """Gespeicherte Antworten neu deuten und vergleichen – ohne Server (z. B. nach einer Änderung an interpret)."""
    folder = Path(args.ordner).expanduser()
    files = sorted((folder / "ki-ergebnisse").glob("[0-9]*.json"))  # the files cmd_test writes
    file = Path(args.datei).expanduser() if args.datei else (files[-1] if files else None)
    if not file or not file.exists():
        sys.exit(f"{folder}/ki-ergebnisse: keine gespeicherten Antworten")
    expected = json.loads((folder / EXPECTED).read_text())["dokumente"]
    data = json.loads(file.read_text())
    results, stats = [], {}
    print(f"{file.name}: {len(data['ergebnisse'])} Antworten\n")
    for old in data["ergebnisse"]:
        name, art = old["beleg"], old.get("art") or expected.get(old["beleg"], {}).get("art", "beleg")
        want = {} if old.get("fehler") else expected.get(name, {})
        r = report(name, art, old.get("antwort") or {}, want, stats, f"{old.get('sekunden', 0):6.1f} s", args.zeilen)
        results.append({**r, "fehler": old.get("fehler", "")})
    print()
    summary(results, stats)


def main():
    ap = argparse.ArgumentParser(description="Testet ein lokales KI-Modell beim Auslesen von Belegen (docs/KI-TEST.md).")
    sub = ap.add_subparsers(dest="befehl", required=True)

    def server_args(p):
        p.add_argument("--server", default=SERVER, help=f"OpenAI-kompatible Adresse (Standard {SERVER})")
        p.add_argument("--schluessel", default=env_key(),
                       help="API-Schlüssel (Standard: LLM_API_KEY oder API_KEY aus .env im Repository)")

    m = sub.add_parser("modelle", help="Modelle auflisten, die der Server anbietet")
    server_args(m)
    v = sub.add_parser("vorlage", help="erwartet.json im Testordner anlegen bzw. um neue Belege ergänzen")
    v.add_argument("ordner")
    v.add_argument("--art", choices=list(TYPES), default="beleg", help="Belegart neuer Einträge (Standard beleg)")
    v.add_argument("--trotzdem", action="store_true", help=argparse.SUPPRESS)
    t = sub.add_parser("test", help="alle Belege auslesen lassen, Zeit messen, mit erwartet.json vergleichen")
    t.add_argument("ordner")
    server_args(t)
    t.add_argument("--modell", default=MODEL, help=f"Name des Modells, wie „modelle“ ihn zeigt (Standard {MODEL})")
    t.add_argument("--max-seite", type=int, default=1400, help="Bilder auf höchstens so viele Pixel Kantenlänge verkleinern (0: nie; Standard 1400)")
    t.add_argument("--timeout", type=int, default=600, help="Sekunden je Beleg (Standard 600)")
    t.add_argument("--ohne-schema", action="store_true", help="kein JSON-Schema mitschicken (für Server, die es nicht kennen)")
    t.add_argument("--nur", nargs="+", metavar="NAME", help="nur Belege, deren Name einen dieser Teile enthält")
    t.add_argument("--zeilen", type=int, default=12, help="höchstens so viele Abweichungen je Beleg zeigen (Standard 12)")
    t.add_argument("--trotzdem", action="store_true", help=argparse.SUPPRESS)
    e = sub.add_parser("auswerten", help="gespeicherte Antworten ohne Server neu deuten und vergleichen")
    e.add_argument("ordner")
    e.add_argument("datei", nargs="?", help="Ergebnisdatei (Standard: die neueste in ki-ergebnisse/)")
    e.add_argument("--zeilen", type=int, default=12, help="höchstens so viele Abweichungen je Beleg zeigen (Standard 12)")
    args = ap.parse_args()
    {"modelle": cmd_models, "vorlage": cmd_template, "test": cmd_test, "auswerten": cmd_evaluate}[args.befehl](args)


if __name__ == "__main__":
    main()
