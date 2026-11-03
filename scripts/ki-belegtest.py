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
DATE_FIELDS = {"rechnungsdatum", "behandlung_von", "behandlung_bis"}
IMAGE_TYPES = {".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png", ".webp": "image/webp"}
EXPECTED = "erwartet.json"
SERVER = "http://192.168.178.38:8080/v1"
MODEL = "qwen3-vl-8b"

SYSTEM = ("Du liest Belege der deutschen privaten Krankenversicherung und Beihilfe aus: Arztrechnungen, "
          "Zahnarztrechnungen, Rezepte, Rechnungen von Apotheken und Therapeuten. Antworte nur mit JSON.")
PROMPT = ("Lies den Beleg (alle Seiten zusammen) und gib diese Felder als JSON-Objekt zurück:\n"
          + "\n".join(f"- {k}: {v}" for k, v in FIELDS.items())
          + "\n\nRegeln: Datumsangaben als JJJJ-MM-TT. Betrag mit Punkt als Dezimaltrennzeichen, ohne Währung "
            "(z. B. 1234.56). IBAN ohne Leerzeichen. Steht ein Feld nicht auf dem Beleg, gib eine leere "
            "Zeichenkette zurück – nichts schätzen oder erfinden.")
SCHEMA = {
    "type": "object",
    "properties": {k: {"type": "string"} for k in FIELDS},
    "required": list(FIELDS),
    "additionalProperties": False,
}


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


def ask(args, images: list[Path], use_schema: bool) -> tuple[dict, dict]:
    content = [{"type": "text", "text": PROMPT}] + [{"type": "image_url", "image_url": {"url": data_url(p)}} for p in images]
    body = {"model": args.modell, "temperature": 0, "max_tokens": 800,
            "messages": [{"role": "system", "content": SYSTEM}, {"role": "user", "content": content}]}
    if use_schema:
        body["response_format"] = {"type": "json_schema", "json_schema": {"name": "beleg", "strict": True, "schema": SCHEMA}}
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
    return {k: "" if v is None else str(v).strip() for k, v in data.items()} if isinstance(data, dict) else {"_antwort": text}


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


def same(field: str, got: str, want: str) -> bool:
    if field == "betrag":
        return norm_amount(got) == norm_amount(want)
    if field in DATE_FIELDS:
        return norm_date(got) == norm_date(want)
    if field in ("iban", "rechnungsnummer"):
        return re.sub(r"[^0-9A-Z]", "", got.upper()) == re.sub(r"[^0-9A-Z]", "", want.upper())
    g, w = norm_text(got), norm_text(want)
    return bool(g) and (g == w or w in g or g in w)  # "Dr. med. Petra Beispiel" passt zu "Petra Beispiel"


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
    added = 0
    for name in documents(folder):
        if name not in data["dokumente"]:
            data["dokumente"][name] = {k: "" for k in FIELDS}
            added += 1
    path.write_text(json.dumps(data, ensure_ascii=False, indent=2) + "\n")
    print(f"{path}: {added} Beleg(e) ergänzt, {len(data['dokumente'])} insgesamt. Bitte die erwarteten Werte eintragen "
          "(leere Felder werden nicht verglichen).")


def cmd_test(args):
    folder = Path(args.ordner).expanduser()
    check_folder(folder, args.trotzdem)
    docs = documents(folder)
    if not docs:
        sys.exit(f"{folder}: keine Belege gefunden")
    expected = json.loads((folder / EXPECTED).read_text())["dokumente"] if (folder / EXPECTED).exists() else {}
    use_schema = not args.ohne_schema
    if not args.schluessel:
        print("Hinweis: kein API-Schlüssel (--schluessel, LLM_API_KEY oder API_KEY in .env) – der Server im Heimnetz verlangt einen.")
    check_health(args.server)
    results, per_field = [], {k: [0, 0] for k in FIELDS}
    print(f"{len(docs)} Belege → {args.server} ({args.modell})\n")
    for n, (name, pages) in enumerate(docs.items()):
        with tempfile.TemporaryDirectory() as tmp:
            images = page_images(pages, Path(tmp), args.max_seite)
            t0 = time.monotonic()
            try:
                try:
                    got, usage = ask(args, images, use_schema)
                except urllib.error.HTTPError as e:
                    if e.code in (401, 403):
                        key_refused(e)
                    if not use_schema or e.code not in (400, 422, 500):
                        raise
                    use_schema = False  # der Server kennt kein JSON-Schema: ohne weiter
                    print("  (Server lehnt response_format ab – weiter ohne JSON-Schema)")
                    got, usage = ask(args, images, use_schema)
                error = ""
            except (urllib.error.URLError, TimeoutError, KeyError, json.JSONDecodeError) as e:
                got, usage, error = {}, {}, str(e)
            secs = time.monotonic() - t0
        want = {} if error else expected.get(name, {})  # a failed request is reported, not compared
        wrong, right, compared = [], 0, 0
        for k in FIELDS:
            if not want.get(k):
                continue
            compared += 1
            per_field[k][1] += 1
            if same(k, got.get(k, ""), want[k]):
                right += 1
                per_field[k][0] += 1
            else:
                wrong.append(f"{k}: „{got.get(k, '')}“ statt „{want[k]}“")
        score = f"{right}/{compared}" if compared else "–"
        print(f"{name:<34} {len(images)} S. {secs:7.1f} s  {score:>5}" + (f"  FEHLER: {error}" if error else ""))
        for w in wrong:
            print(f"    {w}")
        results.append({"beleg": name, "seiten": len(images), "sekunden": round(secs, 1), "richtig": right, "verglichen": compared,
                        "erstes": n == 0, "antwort": got, "usage": usage, "fehler": error})

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
    total = sum(r["verglichen"] for r in ok)
    if total:
        print(f"Felder richtig: {sum(r['richtig'] for r in ok)} von {total} ({100 * sum(r['richtig'] for r in ok) / total:.0f} %)")
        for k, (r, c) in per_field.items():
            if c:
                print(f"  {k:<20} {r}/{c}")
    if len(ok) < len(results):
        print(f"{len(results) - len(ok)} Beleg(e) mit Fehler (Server nicht erreichbar, Zeitüberschreitung …)")

    out = folder / "ki-ergebnisse"
    out.mkdir(exist_ok=True)
    file = out / f"{datetime.now():%Y%m%d-%H%M%S}-{re.sub(r'[^A-Za-z0-9._-]+', '_', args.modell)}.json"
    file.write_text(json.dumps({"server": args.server, "modell": args.modell, "json_schema": use_schema, "max_seite": args.max_seite,
                                "ergebnisse": results}, ensure_ascii=False, indent=2) + "\n")
    print(f"\nAntworten gespeichert: {file}")


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
    v.add_argument("--trotzdem", action="store_true", help=argparse.SUPPRESS)
    t = sub.add_parser("test", help="alle Belege auslesen lassen, Zeit messen, mit erwartet.json vergleichen")
    t.add_argument("ordner")
    server_args(t)
    t.add_argument("--modell", default=MODEL, help=f"Name des Modells, wie „modelle“ ihn zeigt (Standard {MODEL})")
    t.add_argument("--max-seite", type=int, default=1400, help="Bilder auf höchstens so viele Pixel Kantenlänge verkleinern (0: nie; Standard 1400)")
    t.add_argument("--timeout", type=int, default=600, help="Sekunden je Beleg (Standard 600)")
    t.add_argument("--ohne-schema", action="store_true", help="kein JSON-Schema mitschicken (für Server, die es nicht kennen)")
    t.add_argument("--trotzdem", action="store_true", help=argparse.SUPPRESS)
    args = ap.parse_args()
    {"modelle": cmd_models, "vorlage": cmd_template, "test": cmd_test}[args.befehl](args)


if __name__ == "__main__":
    main()
