#!/bin/sh
# probe-postremove - on "purge" the key goes, the measurements stay: they are
# the record of past incidents and are removed by hand (/var/lib/lcm-probe).
set -e

if [ -d /run/systemd/system ]; then
	systemctl daemon-reload >/dev/null 2>&1 || true
fi

if [ "$1" = "purge" ]; then
	rm -rf /etc/lcm-probe
fi

exit 0
