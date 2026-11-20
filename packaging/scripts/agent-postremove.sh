#!/bin/sh
# agent-postremove - cleans up after removal. On "purge" the enrollment
# configuration (holding the connection secret) is removed as well; a plain
# "remove" keeps it for a reinstall.
#
# Only deb knows "purge". rpm calls this script with 0 (removal) or 1
# (upgrade), apk and pacman with the old version, and only on removal - none
# of them removes the configuration.
#
# LCM_PKG_ROOT is for packaging/scripts/agent-scripts-test.sh only.
set -e

ROOT="${LCM_PKG_ROOT:-}"

if [ -d "$ROOT/run/systemd/system" ]; then
	systemctl daemon-reload >/dev/null 2>&1 || true
fi

if [ "${1:-}" = "purge" ]; then
	rm -rf "$ROOT/etc/lcm-agent"
fi

exit 0
