#!/bin/sh
# Make the menu entry and the icon show up right away.
update-desktop-database -q /usr/share/applications >/dev/null 2>&1 || true
gtk-update-icon-cache -q -t /usr/share/icons/hicolor >/dev/null 2>&1 || true
exit 0
