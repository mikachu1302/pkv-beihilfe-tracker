# Installation

PKV Beihilfe Tracker läuft unter **Linux**, **macOS** (Intel und Apple Silicon) und **Windows**.
Fertige Pakete gibt es auf der Release-Seite des Repositorys
(`github.com/mikachu1302/pkv-beihilfe-tracker` → *Releases*). Das Repository ist privat: zum
Herunterladen bei GitHub anmelden oder die GitHub-CLI `gh` verwenden.

Die Rechnungen liegen nicht im Programm, sondern im **Datenordner**, den die App beim ersten Start
abfragt. Installieren, Aktualisieren oder Entfernen der App ändert daran nichts.

| | Am einfachsten | Ohne Root bzw. ohne Admin | Aus dem Quellcode |
|---|---|---|---|
| Linux | `.rpm` / `.deb` installieren | `.tar.gz` + `./install.sh` | `make install` |
| macOS | `.dmg` öffnen, App in „Programme“ ziehen | dto. (in den Benutzerordner) | `make install` |
| Linux + macOS | `sh scripts/install.sh` (lädt das neueste Release) | ja | – |

---

## Linux

Benötigt werden GTK 3 und **WebKitGTK 4.1** – auf den meisten Desktop-Systemen schon vorhanden;
die Pakete (`.rpm`, `.deb`) ziehen sie automatisch nach.

### Paket (empfohlen)

Release-Datei herunterladen und im Download-Ordner installieren:

```sh
# Fedora
sudo dnf install ./pkv-beihilfe-tracker.rpm

# Debian, Ubuntu, Linux Mint
sudo apt install ./pkv-beihilfe-tracker.deb
```

Danach steht **PKV Beihilfe Tracker** im Anwendungsmenü (Kategorie Büro). Andere Distributionen
(openSUSE, Arch …) nehmen das `.tar.gz` – die Paketnamen der Abhängigkeiten unterscheiden sich dort.
Aktualisieren: neueres Paket genauso installieren. Entfernen: `sudo dnf remove pkv-beihilfe-tracker`
bzw. `sudo apt remove pkv-beihilfe-tracker`.

### Ohne Root (tar.gz)

```sh
tar -xzf pkv-beihilfe-tracker-linux-amd64.tar.gz
cd pkv-beihilfe-tracker-linux-amd64
./install.sh            # → ~/.local/bin, Anwendungsmenü, Symbol
./install.sh --uninstall
```

Falls WebKitGTK 4.1 fehlt, sagt `install.sh` das und nennt den Befehl zum Nachinstallieren
(Fedora: `sudo dnf install webkit2gtk4.1`, Debian/Ubuntu: `sudo apt install libwebkit2gtk-4.1-0`).

### Per Skript

Lädt das neueste Release mit der GitHub-CLI und installiert es wie oben ohne Root:

```sh
gh auth login                 # einmalig
sh scripts/install.sh         # aus einem Klon des Repositorys
# bestimmte Version: VERSION=v0.1.0 sh scripts/install.sh
```

---

## macOS

Die App ist eine Universal-App (Intel und Apple Silicon) ab macOS 12. Sie ist nur „ad-hoc“
signiert, nicht von Apple beglaubigt (das kostet 99 $ im Jahr) – deshalb fragt macOS beim ersten
Start einmal nach.

### DMG (empfohlen)

1. `pkv-beihilfe-tracker-macos-universal.dmg` herunterladen und öffnen.
2. **PKV Beihilfe Tracker** auf **Programme** ziehen.
3. Beim ersten Start meldet macOS, dass die App nicht überprüft werden konnte: **Systemeinstellungen →
   Datenschutz & Sicherheit** öffnen, ganz unten bei „PKV Beihilfe Tracker wurde blockiert“ auf
   **Dennoch öffnen** klicken und bestätigen. Danach startet sie ohne Rückfrage.

Alternativ im Terminal (entfernt die Download-Markierung, danach startet die App direkt):

```sh
xattr -dr com.apple.quarantine "/Applications/PKV Beihilfe Tracker.app"
```

Aktualisieren: neue Version genauso in „Programme“ ziehen und ersetzen. Entfernen: App in den
Papierkorb legen.

### Per Skript

```sh
gh auth login                 # einmalig, GitHub-CLI: brew install gh
sh scripts/install.sh         # → ~/Applications/PKV Beihilfe Tracker.app
```

Mit dem Skript heruntergeladene Apps tragen keine Download-Markierung; die Rückfrage beim ersten
Start entfällt.

---

## Windows

`pkv-beihilfe-tracker-amd64-installer.exe` aus dem Release starten. Weil der Installer nicht
signiert ist, warnt SmartScreen einmal: **Weitere Informationen → Trotzdem ausführen**.
Entfernen über *Einstellungen → Apps*.

---

## Aus dem Quellcode

Voraussetzungen: Go 1.26, Node 22 und die Wails-CLI:

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0              # macOS, Windows
go install -tags gtk3 github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0   # Linux
```

- **Linux** zusätzlich die Entwicklerpakete: Fedora `sudo dnf install gtk3-devel webkit2gtk4.1-devel`,
  Debian/Ubuntu `sudo apt install libgtk-3-dev libwebkit2gtk-4.1-dev`.
- **macOS** zusätzlich die Xcode-Befehlszeilenwerkzeuge: `xcode-select --install`.

```sh
git clone https://github.com/mikachu1302/pkv-beihilfe-tracker.git
cd pkv-beihilfe-tracker
make install      # Linux: ~/.local (ohne Root) · macOS: ~/Applications
make uninstall
```

`make run` baut und startet die App ohne Installation, `make dev` mit automatischem Neuladen.

---

## Erste Schritte

1. **Datenordner wählen.** Ein leerer Ordner – gern in einem Cloud-Ordner (Nextcloud, iCloud Drive,
   Google Drive, OneDrive, Dropbox, Syncthing …), dann sind die Daten auf allen Geräten gleich.
   Den Ordner im Sync-Programm auf „immer auf diesem Gerät behalten“ stellen. Auf einem zweiten
   Gerät denselben Ordner wählen.
2. **Versicherung einrichten:** Beihilfe (50 % oder 70 %) + PKV oder nur PKV. Fristen,
   Mindestbetrag und spätere Änderungen (z. B. 70/30 ab der Pensionierung) unter *Einstellungen*.
3. **Alte Excel-Tabelle übernehmen** (optional): *Import*.
4. **Sicherung einrichten:** *Einstellungen → Sicherung* – ein zweiter Ordner (USB-Stick, NAS,
   anderer Cloud-Ordner) für tägliche Zip-Kopien. Besonders wichtig ohne Cloud.
5. **Belege erfassen:** im *Posteingang* per Handy (QR-Code scannen – Handy und Computer im selben
   WLAN), per Drag & Drop oder über den Ordner `Eingang` im Datenordner.

## Wo liegt was?

| | Linux | macOS | Windows |
|---|---|---|---|
| Programm | `~/.local/bin` bzw. `/usr/local/bin` | `/Applications` bzw. `~/Applications` | `C:\Program Files\mikachu1302\PKV Beihilfe Tracker` |
| Einstellungen des Geräts (Datenordner, Sicherungsordner) | `~/.config/pkv-beihilfe-tracker` | `~/Library/Application Support/pkv-beihilfe-tracker` | `%AppData%\pkv-beihilfe-tracker` |
| Rechnungen, Belege, Einstellungen der Versicherung | im gewählten Datenordner | ebenso | ebenso |

**Wiederherstellen aus einer Sicherung:** Zip in einen leeren Ordner entpacken und diesen unter
*Einstellungen → Datenordner → Wechseln* wählen.

## Probleme

- **Linux: Fenster bleibt weiß oder leer** (manche Grafiktreiber, v. a. NVIDIA unter Wayland):
  mit `WEBKIT_DISABLE_DMABUF_RENDERER=1 pkv-beihilfe-tracker` starten. Hilft das, die Zeile
  `Exec=` in `~/.local/share/applications/pkv-beihilfe-tracker.desktop` entsprechend ergänzen
  (`Exec=env WEBKIT_DISABLE_DMABUF_RENDERER=1 …`).
- **Linux: „libwebkit2gtk-4.1.so.0: cannot open shared object file“** – WebKitGTK 4.1 nachinstallieren
  (siehe oben).
- **macOS: „ist beschädigt und kann nicht geöffnet werden“** – das ist die Download-Markierung, nicht
  ein Defekt: den `xattr`-Befehl oben ausführen.
- **Handy erreicht die Upload-Seite nicht:** Handy und Computer müssen im selben WLAN sein (kein
  Gäste-WLAN). Fragt die Firewall unter Windows oder macOS nach, für private Netzwerke erlauben;
  unter Linux mit aktiver Firewall (z. B. firewalld) den angezeigten Port freigeben oder die Belege
  über den Cloud-Ordner `Eingang` schicken.
