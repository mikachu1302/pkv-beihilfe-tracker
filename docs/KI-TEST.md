# KI-Belegtest: lokales Modell auf dem Galaxy Book

Ziel: herausfinden, ob ein KI-Modell im Heimnetz Belege zuverlässiger ausliest als die
Texterkennung der App – und wie lange es pro Seite braucht. Danach entscheiden wir, ob und wie
die App eine KI als Ergänzung bekommt (nur im lokalen Netz, ohne Cloud).

Getestet werden zwei Wege auf dem Samsung Galaxy Book4 Edge (Snapdragon X Elite, 16 GB,
Windows):

| | Weg A: LM Studio (oder Ollama) | Weg B: Nexa SDK |
|---|---|---|
| Rechnet auf | CPU (ggf. GPU) | NPU |
| Modell | Qwen3-VL **8B** (und zum Vergleich 4B) | Qwen3-VL **4B** (NPU-Fassung) |
| Erwartung | genauer, langsamer | schneller und sparsamer, etwas ungenauer |

Beide bieten eine OpenAI-kompatible Schnittstelle – genau die würde die App später nutzen.
Menüs und Befehle können sich mit neuen Versionen ändern; im Zweifel die Hilfe des jeweiligen
Programms ansehen.

## 1. Galaxy Book vorbereiten

1. **Netzteil anschließen und Ruhezustand aus** (PowerShell als Administrator):

   ```powershell
   powercfg /change standby-timeout-ac 0
   powercfg /change hibernate-timeout-ac 0
   ```

   Zuklappen soll den Rechner nicht schlafen legen: Systemsteuerung → Energieoptionen →
   „Auswählen, was beim Zuklappen des Computers geschehen soll“ → im Netzbetrieb „Nichts
   unternehmen“.
2. **Feste Adresse im Netz**: im Router dem Galaxy Book immer dieselbe IPv4-Adresse geben (bei
   der FRITZ!Box: Heimnetz → Netzwerk → Gerät bearbeiten → „Diesem Netzwerkgerät immer die gleiche
   IPv4-Adresse zuweisen“). Die Adresse zeigt `ipconfig` (IPv4-Adresse). **Das Galaxy Book hat die
   feste Adresse `192.168.178.38`** – sie steht in allen Befehlen unten.
3. **Netzwerkprofil „Privat“** (Einstellungen → Netzwerk und Internet → WLAN/Ethernet →
   Netzwerkprofiltyp), damit die Freigabe nur im Heimnetz gilt.

## 2a. Weg A: LM Studio (CPU/GPU)

1. LM Studio für **Windows ARM64** von lmstudio.ai installieren.
2. Unter „Discover“ nach **Qwen3-VL** suchen und die **8B**-Fassung laden (GGUF, Quantisierung
   Q4_K_M – etwa 5–6 GB). Zum Vergleich später auch die **4B**-Fassung.
3. Reiter „Developer“: Modell laden, Server starten, **„Serve on Local Network“** einschalten.
   Port ist meist **1234**. Optional unter den Server-Einstellungen einen API-Schlüssel festlegen.
4. Firewall öffnen (PowerShell als Administrator):

   ```powershell
   New-NetFirewallRule -DisplayName "KI-Server LM Studio" -Direction Inbound -Protocol TCP -LocalPort 1234 -Action Allow -Profile Private
   ```

Adresse für den Test: `http://192.168.178.38:1234/v1`

**Alternativ Ollama:** installieren, `ollama pull qwen3-vl:8b`, die Umgebungsvariable
`OLLAMA_HOST` auf `0.0.0.0:11434` setzen (Systemeigenschaften → Umgebungsvariablen), Ollama neu
starten, Firewall für Port 11434 öffnen. Adresse: `http://192.168.178.38:11434/v1`

## 2b. Weg B: Nexa SDK (NPU)

1. Aktuelle **NPU-Treiber** für den Snapdragon installieren (Windows Update bzw. Samsung Update).
2. Nexa SDK von github.com/NexaAI/nexa-sdk installieren – die **arm64-Fassung mit Qualcomm-NPU**.
3. Modell laden und einmal ausprobieren (in einer Eingabeaufforderung):

   ```
   nexa infer NexaAI/Qwen3-VL-4B-Instruct-NPU
   ```

4. Server fürs Heimnetz starten:

   ```
   nexa serve --host 0.0.0.0:8080
   ```

   (`nexa serve --help` zeigt die Optionen, falls sich etwas geändert hat.)
5. Firewall für Port 8080 öffnen (wie oben, mit `-LocalPort 8080`).

Adresse für den Test: `http://192.168.178.38:8080/v1`

## 3. Testbelege zusammenstellen

1. Einen Ordner **außerhalb** des Repositorys anlegen, z. B. `~/Belegtest` (das Skript verweigert
   Ordner im Repository – echte Belege gehören nicht dorthin).
2. 5–10 Belege hineinlegen, vor allem solche, bei denen die Texterkennung der App scheitert oder
   Felder falsch füllt; dazu ein, zwei einfache zum Vergleich.
   - ein Beleg = eine Bilddatei (jpg, png, webp) oder eine PDF;
   - ein mehrseitiger Beleg aus Fotos = ein Unterordner mit den Seiten (Namen in Seitenfolge,
     z. B. `01.jpg`, `02.jpg`).
3. Die Vorlage für die erwarteten Werte anlegen:

   ```sh
   python3 scripts/ki-belegtest.py vorlage ~/Belegtest
   ```

   und in `~/Belegtest/erwartet.json` je Beleg eintragen, was richtig wäre (Behandler,
   Rechnungsdatum, Behandlungszeitraum, Betrag, Rechnungsnummer, IBAN, Zahlungsempfänger).
   Datum und Betrag dürfen wie auf dem Beleg geschrieben werden („21.09.2026“, „1.234,56“).
   Leere Felder werden nicht verglichen.

## 4. Test ausführen

Vom Rechner mit dem Tracker aus (Python 3 genügt; für PDFs `pdftoppm`, zum Verkleinern großer
Fotos ImageMagick – beides ist unter Fedora meist schon da):

```sh
# Verbindung prüfen und den Modellnamen ablesen
python3 scripts/ki-belegtest.py modelle --server http://192.168.178.38:1234/v1

# Test (Modellname wie von „modelle“ angezeigt)
python3 scripts/ki-belegtest.py test ~/Belegtest --server http://192.168.178.38:1234/v1 --modell qwen3-vl-8b
```

Optionen: `--schluessel …` (API-Schlüssel), `--max-seite 1600` (Bilder vor dem Senden auf diese
Kantenlänge verkleinern; größer = genauer, aber langsamer; `0` = nie), `--timeout 600`
(Sekunden je Beleg), `--ohne-schema` (falls der Server kein JSON-Schema versteht – das Skript
merkt das meist selbst).

Für jeden Weg und jedes Modell einmal laufen lassen. Ausgabe:

```
rechnung-zahnarzt.jpg               1 S.    41.3 s    6/7
    betrag: „1.284,50“ statt „1.248,50“
…
Zeit pro Seite: Median 38.2 s, Mittel 40.1 s (ohne den ersten Beleg, der das Modell lädt: 95.0 s)
Felder richtig: 41 von 46 (89 %)
```

Die vollständigen Antworten liegen danach in `~/Belegtest/ki-ergebnisse/` (eine Datei je Lauf).

## 5. Auswerten

- **Genauigkeit**: Wie viele Felder stimmen – und vor allem: Erfindet das Modell Werte (z. B.
  einen Betrag, der nicht auf dem Beleg steht)? Ein leeres Feld ist harmlos, ein falscher Betrag
  nicht.
- **Zeit pro Seite**: Für „schwierige Belege auf Knopfdruck“ sind auch 30–60 s in Ordnung, für
  jeden Beleg nebenbei eher nicht.
- **Vergleich A/B**: Reicht das 4B-Modell auf der NPU, oder braucht es das 8B-Modell?

Mit den Zahlen planen wir die Umsetzung in der App (Einstellungen: Adresse und Modell; „Mit KI
auslesen“ im Posteingang und Formular; Gegenprüfungen wie Summe der Positionen und IBAN-Prüfziffer;
alles bleibt zur Prüfung, nichts wird ungefragt gespeichert).

## Datenschutz

Die Belege gehen nur vom eigenen Rechner zum Galaxy Book im Heimnetz, nicht ins Internet. Die
Verbindung ist im Heimnetz unverschlüsselt (HTTP); deshalb die Freigabe nur für das private
Netzwerkprofil und optional mit API-Schlüssel. Testordner und Ergebnisse bleiben außerhalb des
Repositorys.
