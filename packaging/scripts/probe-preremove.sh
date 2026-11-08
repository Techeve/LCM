#!/bin/sh
# probe-preremove - stops the service before any files are removed.
set -e

if [ -d /run/systemd/system ]; then
	systemctl stop lcm-probe.service >/dev/null 2>&1 || true
	if [ "$1" = "remove" ] || [ "$1" = "0" ]; then
		systemctl disable lcm-probe.service >/dev/null 2>&1 || true
	fi
fi

exit 0
