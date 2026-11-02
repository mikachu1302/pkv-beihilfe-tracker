#!/bin/sh
# Lädt die neueste Version von PKV Beihilfe Tracker aus den GitHub-Releases
# und installiert sie für den aktuellen Benutzer:
#   Linux: ~/.local/bin + Anwendungsmenü (ohne Root)
#   macOS: ~/Applications/PKV Beihilfe Tracker.app
#
# Das Repository ist privat, deshalb wird die GitHub-CLI „gh“ mit Anmeldung
# benötigt (https://cli.github.com, danach einmal: gh auth login).
#
#   sh scripts/install.sh            neueste Version
#   VERSION=v0.1.0 sh scripts/install.sh
set -eu

REPO=${REPO:-mikachu1302/pkv-beihilfe-tracker}
VERSION=${VERSION:-}

if ! command -v gh >/dev/null 2>&1; then
	echo "Bitte zuerst die GitHub-CLI installieren (https://cli.github.com) und mit „gh auth login“ anmelden." >&2
	exit 1
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

download() {
	# shellcheck disable=SC2086 # VERSION is empty or one tag
	if ! gh release download $VERSION --repo "$REPO" --pattern "$1" --dir "$tmp"; then
		echo "Download fehlgeschlagen – gibt es schon ein Release? (gh release list --repo $REPO)" >&2
		exit 1
	fi
}

case "$(uname -s)" in
Linux)
	if [ "$(uname -m)" != "x86_64" ]; then
		echo "Für $(uname -m) gibt es kein fertiges Paket – bitte aus dem Quellcode bauen (make install)." >&2
		exit 1
	fi
	download '*-linux-amd64.tar.gz'
	tar -xzf "$tmp"/*-linux-amd64.tar.gz -C "$tmp"
	sh "$tmp"/pkv-beihilfe-tracker-linux-amd64/install.sh
	;;
Darwin)
	download '*-macos-universal.zip'
	ditto -x -k "$tmp"/*-macos-universal.zip "$tmp/app"
	target="$HOME/Applications/PKV Beihilfe Tracker.app"
	mkdir -p "$HOME/Applications"
	rm -rf "$target"
	mv "$tmp/app/PKV Beihilfe Tracker.app" "$target"
	# Downloaded with gh, the app carries no quarantine flag; remove it anyway
	# in case the archive came through a browser.
	xattr -dr com.apple.quarantine "$target" 2>/dev/null || true
	echo "Installiert: $target"
	echo "Start über Launchpad, Spotlight oder den Ordner „Programme“ im Benutzerordner."
	;;
*)
	echo "Dieses Skript unterstützt Linux und macOS. Für Windows den Installer aus dem Release verwenden." >&2
	exit 1
	;;
esac
