#!/bin/sh
# preremove - stops the service before any files are removed.
set -e

if [ -d /run/systemd/system ]; then
	systemctl stop lcm.service >/dev/null 2>&1 || true
	# On final removal (not on upgrade) also disable it.
	if [ "$1" = "remove" ] || [ "$1" = "0" ]; then
		systemctl disable lcm.service >/dev/null 2>&1 || true
	fi
fi

# dpkg calls this script with "remove" for "apt purge" too, so the message has
# to fit both: "remove" keeps the data, "purge" (run after this) deletes it.
if [ "$1" = "remove" ] && ls /var/lib/lcm/backups/*.lcmbak >/dev/null 2>&1; then
	echo "LCM: remove keeps /var/lib/lcm. 'apt purge lcm' also deletes the database," >&2
	echo "LCM: the keys and the backups there - copy them first." >&2
	echo "LCM: remove behaelt /var/lib/lcm. 'apt purge lcm' loescht Datenbank, Schluessel" >&2
	echo "LCM: und Sicherungen - vorher sichern." >&2
fi

exit 0
