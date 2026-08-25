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

exit 0
