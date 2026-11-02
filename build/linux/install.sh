#!/bin/sh
# Installiert PKV Beihilfe Tracker für den aktuellen Benutzer – ohne Root:
# Programm nach ~/.local/bin, Eintrag im Anwendungsmenü, Symbol.
#
#   ./install.sh               installieren (oder aktualisieren)
#   ./install.sh --uninstall   entfernen (der Datenordner bleibt unberührt)
#
# Anderes Ziel: PREFIX=/opt/pkv ./install.sh
set -eu

APP=pkv-beihilfe-tracker
here=$(cd "$(dirname "$0")" && pwd)
prefix=${PREFIX:-$HOME/.local}
bin=$prefix/bin/$APP
desktop=$prefix/share/applications/$APP.desktop
icon=$prefix/share/icons/hicolor/512x512/apps/$APP.png

refresh() {
	update-desktop-database "$prefix/share/applications" >/dev/null 2>&1 || true
	gtk-update-icon-cache -q -t "$prefix/share/icons/hicolor" >/dev/null 2>&1 || true
}

if [ "${1:-}" = "--uninstall" ]; then
	rm -f "$bin" "$desktop" "$icon"
	refresh
	echo "PKV Beihilfe Tracker wurde entfernt. Der Datenordner und die Einstellungen in ~/.config/$APP bleiben erhalten."
	exit 0
fi

for f in "$APP" "$APP.png" "$APP.desktop"; do
	if [ ! -f "$here/$f" ]; then
		echo "Datei fehlt: $here/$f – bitte das Archiv vollständig entpacken." >&2
		exit 1
	fi
done

mkdir -p "$(dirname "$bin")" "$(dirname "$desktop")" "$(dirname "$icon")"
cp "$here/$APP" "$bin"
chmod 755 "$bin"
cp "$here/$APP.png" "$icon"
chmod 644 "$icon"
sed -e "s|^Exec=.*|Exec=$bin|" -e "s|^Icon=.*|Icon=$APP|" -e "s|^Name=.*|Name=PKV Beihilfe Tracker|" \
	"$here/$APP.desktop" >"$desktop"
chmod 644 "$desktop"
refresh

echo "Installiert: $bin"
echo "„PKV Beihilfe Tracker“ ist jetzt im Anwendungsmenü."

# The app needs GTK 3 and WebKitGTK 4.1 at runtime.
if command -v ldconfig >/dev/null 2>&1 && ! ldconfig -p 2>/dev/null | grep -q 'libwebkit2gtk-4\.1'; then
	echo
	echo "Hinweis: WebKitGTK 4.1 scheint zu fehlen. Bitte nachinstallieren:"
	echo "  Fedora:          sudo dnf install webkit2gtk4.1"
	echo "  Debian/Ubuntu:   sudo apt install libwebkit2gtk-4.1-0"
	echo "  Arch:            sudo pacman -S webkit2gtk-4.1"
fi
