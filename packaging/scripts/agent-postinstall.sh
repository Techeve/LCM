#!/bin/sh
# agent-postinstall - registers the unit. The service deliberately does NOT
# start automatically: the unit carries
# ConditionPathExists=/etc/lcm-agent/agent.json, and only
# `lcm-agent enroll <url> <token>` writes that config and activates it.
#
# On an upgrade it brings a running agent onto the new binary - but never by
# restarting it from inside one of its own commands (see restart_after_upgrade).
#
# The same script is the post-upgrade script of apk and pacman
# (nfpm-agent.yaml). How the package formats call it:
#   deb     postinst configure [<old>] | abort-upgrade | abort-remove ...
#   rpm     %post 1 on install, 2 on upgrade
#   apk     .post-install <new>  |  .post-upgrade <new> <old>
#   pacman  post_install <new>   |  post_upgrade <new> <old>
#
# All output is plain ASCII on purpose (see postinstall.sh); the German texts
# use ue/ae/oe/ss.
#
# LCM_PKG_ROOT is for packaging/scripts/agent-scripts-test.sh only.
set -e

ROOT="${LCM_PKG_ROOT:-}"

# lcm_lang picks the output language: English by default, German only if the
# system is actually set to German. Duplicated from postinstall.sh - dpkg
# maintainer scripts run standalone and cannot share a helper file.
lcm_lang() {
	_l="${LC_ALL:-}"
	[ -z "$_l" ] && _l="${LC_MESSAGES:-}"
	[ -z "$_l" ] && _l="${LANG:-}"
	# grep -E/cut/tr instead of sed: BSD and busybox sed do not support
	# \| alternation in BREs, but this script runs on all three.
	for _f in /etc/default/locale /etc/locale.conf; do
		[ -n "$_l" ] && break
		[ -r "$_f" ] || continue
		_l=$(grep -E '^[[:space:]]*(LANG|LC_ALL|LC_MESSAGES)=' "$_f" 2>/dev/null |
			head -n 1 | cut -d= -f2- | tr -d '"'\''' | tr -d '[:space:]')
	done
	case "$_l" in
	de | de_* | De_* | DE_*) echo de ;;
	*) echo en ;;
	esac
}

# is_upgrade tells an upgrade from a first installation (see the table above).
is_upgrade() {
	case "$1" in
	configure) [ -n "$2" ] ;;
	abort-* | triggered | "") return 1 ;;
	*[!0-9]*) [ -n "$2" ] ;; # apk, pacman: <new> [<old>]
	*) [ "$1" -ge 2 ] ;;     # rpm: number of installed versions
	esac
}

# under_agent tells whether this script runs inside a command of the agent:
# lcm-agent is one of its ancestors. That is the case when LCM patches this
# server through the agent.
under_agent() {
	_pid=$$
	while [ "${_pid:-0}" -gt 1 ]; do
		[ "$(cat "$ROOT/proc/$_pid/comm" 2>/dev/null)" = "lcm-agent" ] && return 0
		_pid=$(awk '$1 == "PPid:" { print $2 }' "$ROOT/proc/$_pid/status" 2>/dev/null) || _pid=0
	done
	return 1
}

# restart_after_upgrade brings a running agent onto the new binary.
#
# Not from inside one of its commands: A restart stops the unit, and systemd
# kills the whole control group with it - this package transaction included,
# dpkg would be left half-configured. The running agent notices the new
# binary itself and switches to it (exec, same PID) once its command has
# finished and the result is delivered. That is the only moment that is safe
# for sure; a delayed restart from here (systemd-run --on-active, try-restart
# --no-block) can only guess it.
#
# Outside the agent (an admin runs apt), restarting right away is safe.
restart_after_upgrade() {
	if under_agent; then
		if [ "$(lcm_lang)" = "de" ]; then
			echo "lcm-agent: Der Agent wechselt auf die neue Version, sobald das laufende LCM-Kommando fertig ist."
		else
			echo "lcm-agent: the agent switches to the new version once the running LCM command has finished."
		fi
		return 0
	fi
	if [ -d "$ROOT/run/systemd/system" ]; then
		systemctl try-restart lcm-agent.service >/dev/null 2>&1 || true
	elif command -v rc-service >/dev/null 2>&1; then
		if rc-service lcm-agent status >/dev/null 2>&1; then
			rc-service lcm-agent restart >/dev/null 2>&1 || true
		fi
	fi
}

if [ -d "$ROOT/run/systemd/system" ]; then
	systemctl daemon-reload >/dev/null 2>&1 || true
fi

if is_upgrade "${1:-}" "${2:-}"; then
	restart_after_upgrade
fi

if [ ! -e "$ROOT/etc/lcm-agent/agent.json" ]; then
	if [ "$(lcm_lang)" = "de" ]; then
		cat <<'EOF'

============================================================
  lcm-agent wurde installiert (noch nicht verbunden).

  Einrichtung mit dem Token aus dem LCM-Onboarding
  (Modus "Agent"):

      sudo lcm-agent enroll <lcm-server-url> <token>

============================================================

EOF
	else
		cat <<'EOF'

============================================================
  lcm-agent has been installed (not connected yet).

  Set it up with the token from the LCM onboarding
  (mode "Agent"):

      sudo lcm-agent enroll <lcm-server-url> <token>

============================================================

EOF
	fi
fi

exit 0
