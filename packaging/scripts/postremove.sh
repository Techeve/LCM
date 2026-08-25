#!/bin/sh
# postremove - cleans up after removal. On "purge" it additionally removes
# the configuration, the data (including the encrypted DB and master key) and
# the system user. A plain "remove" keeps the data.
set -e

if [ -d /run/systemd/system ]; then
	systemctl daemon-reload >/dev/null 2>&1 || true
fi

if [ "$1" = "purge" ]; then
	rm -rf /var/lib/lcm /etc/lcm
	if getent passwd lcm >/dev/null 2>&1; then
		deluser --system lcm >/dev/null 2>&1 || true
	fi
	if getent group lcm >/dev/null 2>&1; then
		delgroup --system lcm >/dev/null 2>&1 || true
	fi
fi

exit 0
