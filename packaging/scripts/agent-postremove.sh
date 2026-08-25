#!/bin/sh
# agent-postremove - cleans up after removal. On "purge" the enrollment
# configuration (holding the connection secret) is removed as well; a plain
# "remove" keeps it for a reinstall.
set -e

if [ -d /run/systemd/system ]; then
	systemctl daemon-reload >/dev/null 2>&1 || true
fi

if [ "$1" = "purge" ]; then
	rm -rf /etc/lcm-agent
fi

exit 0
