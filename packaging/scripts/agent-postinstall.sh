#!/bin/sh
# agent-postinstall - registers the unit. The service deliberately does NOT
# start automatically: the unit carries
# ConditionPathExists=/etc/lcm-agent/agent.json, and only
# `lcm-agent enroll <url> <token>` writes that config and activates it.
#
# All output is plain ASCII on purpose (see postinstall.sh); the German texts
# use ue/ae/oe/ss.
set -e

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

if [ -d /run/systemd/system ]; then
	systemctl daemon-reload >/dev/null 2>&1 || true
	# If the agent is already running (upgrade), pick up the new binary.
	if systemctl is-active --quiet lcm-agent.service 2>/dev/null; then
		systemctl restart lcm-agent.service >/dev/null 2>&1 || true
	fi
fi

if [ ! -e /etc/lcm-agent/agent.json ]; then
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
