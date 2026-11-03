# KI-Belegtest: lokales Modell im Heimnetz

Ziel: herausfinden, ob das KI-Modell im Heimnetz Belege zuverlässiger ausliest als die
Texterkennung der App – und wie lange es pro Seite braucht. Die Ergebnisse entscheiden die
offenen Punkte in [PLAN-v0.5.md](PLAN-v0.5.md).

## Der Server

Eingerichtet am 2026-11-03 auf dem Galaxy Book4 Edge (Snapdragon X Elite, 16 GB, Windows 11):

| | |
|---|---|
| Adresse | `http://192.168.178.38:8080/v1` (OpenAI-kompatibel) |
| Erreichbar? | `http://192.168.178.38:8080/health` → `{"status":"ok"}` (ohne Schlüssel) |
| Schlüssel | API-Schlüssel (`Authorization: Bearer …`) – liegt auf dem Server, wird separat weitergegeben und **nie** ins Repository geschrieben |
| Modell | `qwen3-vl-8b` (Qwen3-VL-8B-Instruct, Q4_0, mit Bildverständnis) |
| Software | llama.cpp `llama-server`; Text auf der CPU, Bilder auf der Adreno-GPU (OpenCL) |
| Betrieb | startet beim Booten, auch ohne Anmeldung; am Netzteil, Deckel zu |
| Last | bis zu 4 Anfragen gleichzeitig (gemeinsam 16k Token Kontext); zwei Handys nutzen ihn mit |

Gemessen beim Einrichten: eine Rechnung als Bild (1000×1400 px) → JSON in etwa **47 s**, alle
Felder richtig.

Den Schlüssel als `API_KEY=…` in die Datei `.env` im Wurzelordner des Repositorys schreiben –
sie ist von Git ausgeschlossen (`.gitignore`) und wird vom Testskript gelesen. Alternativ die
Umgebungsvariable `LLM_API_KEY` setzen. Schnelltest vom eigenen Rechner:

```sh
export LLM_API_KEY=…   # der Schlüssel vom Server
curl http://192.168.178.38:8080/health
curl -H "Authorization: Bearer $LLM_API_KEY" http://192.168.178.38:8080/v1/models
```

## 1. Testbelege zusammenstellen

1. Einen Ordner **außerhalb** des Repositorys anlegen, z. B. `~/Belegtest` (das Skript verweigert
   Ordner im Repository – echte Belege gehören nicht dorthin).
2. 5–10 Rechnungen und 2–3 Bescheide hineinlegen, vor allem solche, bei denen die Texterkennung der
   App scheitert oder Felder falsch füllt; dazu ein, zwei einfache zum Vergleich.
   - ein Beleg = eine Bilddatei (jpg, png, webp) oder eine PDF;
   - ein mehrseitiger Beleg aus Fotos = ein Unterordner mit den Seiten (Namen in Seitenfolge,
     z. B. `01.jpg`, `02.jpg`). Mindestens ein mehrseitiger Beleg sollte dabei sein – ob drei
     Seiten in eine Anfrage passen, ist eine der offenen Fragen.
3. Die Vorlage für die erwarteten Werte anlegen:

   ```sh
   python3 scripts/ki-belegtest.py vorlage ~/Belegtest
   ```

   und in `~/Belegtest/erwartet.json` je Beleg eintragen, was richtig wäre (Behandler,
   Rechnungsdatum, Behandlungszeitraum, Betrag, Rechnungsnummer, IBAN, Zahlungsempfänger).
   Datum und Betrag dürfen wie auf dem Beleg geschrieben werden („21.09.2026“, „1.234,56“).
   Leere Felder werden nicht verglichen. (Bei Bescheiden genügen vorerst Absender und Datum – die
   einzelnen Zeilen prüft der Test noch nicht.)

## 2. Test ausführen

Vom eigenen Rechner aus (Python 3 genügt; für PDFs `pdftoppm`, zum Verkleinern großer Fotos
ImageMagick – beides ist unter Fedora meist schon da). Server, Modell und Schlüssel (aus
`LLM_API_KEY` oder `API_KEY` in `.env`) sind voreingestellt:

```sh
python3 scripts/ki-belegtest.py modelle            # Verbindung und Modell prüfen
python3 scripts/ki-belegtest.py test ~/Belegtest
```

Optionen: `--server …`, `--modell …`, `--schluessel …` (statt `.env`/`LLM_API_KEY`), `--max-seite 1400`
(Bilder vor dem Senden auf diese Kantenlänge verkleinern; größer = genauer, aber langsamer; `0` =
nie), `--timeout 600` (Sekunden je Beleg), `--ohne-schema` (falls ein Server kein JSON-Schema
versteht – das Skript merkt das meist selbst).

Am besten zweimal laufen lassen: einmal, wenn die Handys den Server nicht nutzen, einmal
nebenbei – dann zeigt sich, wie langsam es im Alltag wird. Ausgabe:

```
rechnung-zahnarzt.jpg               1 S.    41.3 s    6/7
    betrag: „1.284,50“ statt „1.248,50“
…
Zeit pro Seite: Median 38.2 s, Mittel 40.1 s (ohne den ersten Beleg, der das Modell lädt: 95.0 s)
Lesen 41 Token/s, Schreiben 14 Token/s
Felder richtig: 41 von 46 (89 %)
```

Die vollständigen Antworten liegen danach in `~/Belegtest/ki-ergebnisse/` (eine Datei je Lauf).

## 3. Auswerten

- **Genauigkeit**: Wie viele Felder stimmen – und vor allem: Erfindet das Modell Werte (z. B.
  einen Betrag, der nicht auf dem Beleg steht)? Ein leeres Feld ist harmlos, ein falscher Betrag
  nicht.
- **Zeit pro Seite**: Für „schwierige Belege auf Knopfdruck“ sind auch 30–60 s in Ordnung, für
  jeden Beleg nebenbei eher nicht.
- **Mehrseitige Belege**: Kommt eine Antwort, oder lehnt der Server ab (Kontext zu klein)?
- **Bescheide**: Liest das Modell die Zeilen brauchbar? (Dann lohnt sich Phase 2 im Plan.)

Die Zusammenfassung des Skripts (Zeit pro Seite, Felder richtig, auffällige Fehler) reicht als
Rückmeldung; die Belege selbst müssen dafür nicht weitergegeben werden.

## Fragen-Test (Chatbot)

`scripts/ki-fragentest.py` schickt 40 erfundene Fragen („Wie viele Rechnungen aus 2026 sind noch
offen?“, „Und 2025?“ …) mit dem geplanten Abfrage-Schema an den Server und vergleicht die
Antwort mit der erwarteten Abfrage. Es gehen keine Rechnungsdaten an den Server. Ergebnisse vom
2026-11-03: siehe [PLAN-v0.5.md](PLAN-v0.5.md) (Test results).

```sh
python3 scripts/ki-fragentest.py               # alle Fragen
python3 scripts/ki-fragentest.py --nur 1,33    # einzelne Fragen
```

## Später vergleichen: die NPU

Der Snapdragon hat eine NPU. Über das Nexa SDK gibt es Qwen3-VL in der **4B**-Fassung für die NPU
(`nexa serve`, ebenfalls OpenAI-kompatibel) – vermutlich schneller und sparsamer, aber etwas
ungenauer als das 8B-Modell. Falls das 8B-Modell zu langsam ist, lässt sich das mit demselben
Skript vergleichen (`--server`, `--modell`).

## Datenschutz

Die Belege gehen nur vom eigenen Rechner zum Galaxy Book im Heimnetz, nicht ins Internet. Die
Verbindung ist im Heimnetz unverschlüsselt (HTTP); der Server ist nur im Heimnetz erreichbar und
verlangt einen API-Schlüssel. Testordner und Ergebnisse bleiben außerhalb des Repositorys.
