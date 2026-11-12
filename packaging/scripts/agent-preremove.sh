#!/bin/sh
# agent-preremove - stops and disables the service, but only when the package
# is really removed.
#
# On an upgrade the agent keeps running. The upgrade may run THROUGH the
# agent (LCM patches this server via lcm-agent, and lcm-agent is part of the
# package set). Stopping the unit then would kill the package manager with it:
# systemd stops the whole control group of the unit, apt and dpkg included.
# The running agent switches to the new binary by itself once it is idle (see
# agent-postinstall.sh and internal/agent/switch.go).
#
# How the package formats call this script:
#   deb     prerm remove | upgrade <new> | failed-upgrade <old> | deconfigure ...
#   rpm     %preun 0 on removal, 1 on upgrade (runs AFTER the new %post)
#   apk     .pre-deinstall <old>, only on removal (upgrades run .pre-upgrade)
#   pacman  pre_remove <old>, only on removal (upgrades run pre_upgrade)
#
# LCM_PKG_ROOT is for packaging/scripts/agent-scripts-test.sh only.
set -e

ROOT="${LCM_PKG_ROOT:-}"

# is_removal tells a real removal from an upgrade.
is_removal() {
	case "$1" in
	remove | 0 | "") return 0 ;;
	upgrade | failed-upgrade | deconfigure) return 1 ;;
	*[!0-9]*) return 0 ;; # apk, pacman: a version, passed on removal only
	*) return 1 ;;        # rpm: number of versions left installed
	esac
}

is_removal "${1:-}" || exit 0

if [ -d "$ROOT/run/systemd/system" ]; then
	systemctl stop lcm-agent.service >/dev/null 2>&1 || true
	systemctl disable lcm-agent.service >/dev/null 2>&1 || true
elif command -v rc-service >/dev/null 2>&1; then
	rc-service lcm-agent stop >/dev/null 2>&1 || true
	rc-update del lcm-agent default >/dev/null 2>&1 || true
fi

exit 0
