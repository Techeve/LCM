#!/bin/sh
# agent-preinstall - keeps the binary that is about to be replaced as the way
# back: /var/lib/lcm-agent/update/lcm-agent.prev. Only the preinst of the NEW
# package can do that for sure: afterwards /usr/bin/lcm-agent is already the
# new one, and an old agent has no code to save itself. If the new binary does
# not come up, lcm-agent-start starts this copy instead.
#
# Not copied when the current binary is not known to be good: a rollback is in
# effect (the previous binary runs, /usr/bin/lcm-agent is the failed one), or
# the current binary has started without reaching "healthy" yet (starts > 0).
# The way back then stays what it was.
#
# Never fails the installation. On a first install there is no binary yet and
# nothing happens, so the script does not look at its arguments (deb: install |
# upgrade <old>; rpm: 1 | 2; apk .pre-install/.pre-upgrade; pacman pre_install/pre_upgrade).
#
# LCM_PKG_ROOT is for packaging/scripts/agent-scripts-test.sh only.

ROOT="${LCM_PKG_ROOT:-}"
BIN="$ROOT/usr/bin/lcm-agent"
DIR="$ROOT/var/lib/lcm-agent/update"
PREV="$DIR/lcm-agent.prev"

keep_previous() {
	[ -f "$BIN" ] || return 0
	[ ! -e "$DIR/rolled-back" ] || return 0
	_starts=$(cat "$DIR/starts" 2>/dev/null) || _starts=0
	case "$_starts" in '' | *[!0-9]*) _starts=0 ;; esac
	[ "$_starts" -eq 0 ] || return 0
	mkdir -p "$DIR" || return 0
	chmod 700 "$DIR" 2>/dev/null
	cmp -s "$BIN" "$PREV" 2>/dev/null && return 0
	if cp "$BIN" "$PREV.tmp" 2>/dev/null && chmod 755 "$PREV.tmp" && mv -f "$PREV.tmp" "$PREV"; then
		return 0
	fi
	rm -f "$PREV.tmp"
}

keep_previous || true
exit 0
