#!/bin/sh
# agent-postinstall - registers the unit. The service deliberately does NOT
# start automatically: the unit carries
# ConditionPathExists=/etc/lcm-agent/agent.json, and only
# `lcm-agent enroll <url> <token>` writes that config and activates it.
#
# On an upgrade it brings a running agent onto the new binary - but never by
# restarting it from inside one of its own commands (see restart_after_upgrade).
#
# On rpm, zypper and pacman systems it also keeps the agent out of normal
# system upgrades, so that it is updated only on purpose, by an admin. Per
# package format:
#   deb     nothing (apt behaves as before)
#   rpm     pin_dnf: exclude=lcm-agent in the repo file techeve-lcm
#           (update: dnf --setopt=disable_excludes=techeve-lcm install lcm-agent)
#           pin_zypper: a package lock in /etc/zypp/locks
#           (update: zypper removelock lcm-agent, install, zypper addlock lcm-agent)
#   pacman  pin_pacman: IgnorePkg = lcm-agent in /etc/pacman.conf
#           (update: pacman -S lcm-agent or pacman -U, both ignore IgnorePkg)
#   apk     nothing to write: apk rewrites /etc/apk/world at the end of the
#           transaction, which overwrites anything done here. `apk add
#           lcm-agent=<version>` pins the version there. A first install gets
#           a hint (hint_apk).
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

# migrate_apt_suite moves the apt source of the community channel from the
# suite "stable" to "community": the repository server renamed the channel and
# keeps "stable" only for old installations, until it is switched off.
#
# Only entries whose URI is exactly https://repo.techeve.de (also http://,
# trailing slash allowed) are rewritten - never /enterprise..., /internal...,
# beta lines or foreign repositories. It handles the one-line format
# (sources.list, *.list, options in [] allowed) and deb822 (*.sources: URIs and
# Suites in the same stanza). Running it again changes nothing. It never
# fails the installation: whatever goes wrong, the source stays as it was.
#
# Duplicated in postinstall.sh and agent-postinstall.sh - dpkg maintainer
# scripts run standalone and cannot share a helper file. sed -E and plain
# POSIX awk only: the scripts run with GNU, BSD and busybox tools.
migrate_apt_suite() {
	[ -d "$ROOT/etc/apt" ] || return 0
	_tmp=$(mktemp 2>/dev/null) || return 0
	for _f in "$ROOT/etc/apt/sources.list" "$ROOT"/etc/apt/sources.list.d/*.list; do
		[ -f "$_f" ] || continue
		sed -E 's#^([[:space:]]*deb(-src)?[[:space:]]+(\[[^]]*\][[:space:]]+)?https?://repo\.techeve\.de/?[[:space:]]+)stable([[:space:]]|$)#\1community\4#' \
			"$_f" >"$_tmp" 2>/dev/null || continue
		apt_suite_replace "$_f" "$_tmp"
	done
	for _f in "$ROOT"/etc/apt/sources.list.d/*.sources; do
		[ -f "$_f" ] || continue
		awk '
			function flush(   i) {
				for (i = 1; i <= n; i++) {
					if (uri_ok && i == suite_line) sub(/stable[ \t]*$/, "community", buf[i])
					print buf[i]
				}
				n = 0; uri_ok = 0; suite_line = 0
			}
			/^[ \t]*$/ { flush(); print; next }
			{
				buf[++n] = $0
				if ($0 ~ /^[ \t]/ || index($0, ":") == 0) next
				key = tolower(substr($0, 1, index($0, ":") - 1))
				val = substr($0, index($0, ":") + 1)
				gsub(/^[ \t]+|[ \t]+$/, "", val)
				if (key == "uris" && val ~ /^https?:\/\/repo\.techeve\.de\/?$/) uri_ok = 1
				if (key == "suites" && val == "stable") suite_line = n
			}
			END { flush() }
		' "$_f" >"$_tmp" 2>/dev/null || continue
		apt_suite_replace "$_f" "$_tmp"
	done
	rm -f "$_tmp"
}

# apt_suite_replace overwrites $1 with $2 if they differ and $2 is not empty
# (cat > keeps owner, mode and symlinks of the original), and says so.
apt_suite_replace() {
	[ -s "$2" ] || return 0
	cmp -s "$1" "$2" 2>/dev/null && return 0
	cat "$2" >"$1" || return 0
	if [ "$(lcm_lang)" = "de" ]; then
		echo "lcm: Paketquelle ${1#"$ROOT"} von Suite stable auf community umgestellt."
	else
		echo "lcm: apt source ${1#"$ROOT"} moved from suite stable to community."
	fi
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

# pkg_kind names the package format from the arguments (see the table above):
# deb, rpm, apk or pacman. apk and pacman both pass a version; only apk has
# /etc/apk.
pkg_kind() {
	case "$1" in
	configure | abort-* | triggered) echo deb ;;
	*[!0-9]*) if [ -d "$ROOT/etc/apk" ]; then echo apk; else echo pacman; fi ;;
	[0-9]*) echo rpm ;;
	esac
}

# pin_dnf adds lcm-agent to the exclude list of the repo file written by the
# repository's setup script. dnf then leaves the agent out of upgrade and
# install; an update by hand lifts the exclude of this one repo
# (--setopt=disable_excludes=techeve-lcm).
# Only the section [techeve-lcm] counts (the file may hold more repositories),
# and only the exact package name: lcm-agent-foo is not lcm-agent.
# Nothing happens without the repo file (installed from a downloaded rpm).
pin_dnf() {
	_repo="$ROOT/etc/yum.repos.d/techeve-lcm.repo"
	[ -f "$_repo" ] || return 0
	_tmp=$(mktemp 2>/dev/null) || return 0
	awk '
		function has(v,   n, t, i) {
			n = split(v, t, "[ \t,]+")
			for (i = 1; i <= n; i++) if (t[i] == "lcm-agent") return 1
			return 0
		}
		{ line[NR] = $0 }
		/^\[/ { sec = ($0 ~ /^\[techeve-lcm\][ \t]*$/); if (sec && !hdr) hdr = NR; next }
		sec && /^(exclude|excludepkgs)[ \t]*=/ {
			if (has(substr($0, index($0, "=") + 1))) have = 1
			else if (!ex) ex = NR
		}
		END {
			for (i = 1; i <= NR; i++) {
				if (!have && i == ex) print line[i] " lcm-agent"; else print line[i]
				if (!have && !ex && i == hdr) print "exclude=lcm-agent"
			}
		}
	' "$_repo" >"$_tmp" 2>/dev/null && [ -s "$_tmp" ] && ! cmp -s "$_tmp" "$_repo" && cat "$_tmp" >"$_repo"
	rm -f "$_tmp"
}

# pin_zypper locks the package (what `zypper addlock lcm-agent` writes). A
# locked package is skipped by zypper update; an update by hand lifts the lock
# around the install and sets it again. The file is written directly: zypper
# holds its own lock during the transaction that runs this script.
pin_zypper() {
	[ -d "$ROOT/etc/zypp" ] || return 0
	_locks="$ROOT/etc/zypp/locks"
	grep -qs '^solvable_name: lcm-agent$' "$_locks" && return 0
	printf '\ntype: package\nmatch_type: glob\ncase_sensitive: on\nsolvable_name: lcm-agent\n' >>"$_locks"
}

# pin_pacman adds IgnorePkg = lcm-agent to the [options] section: pacman -Syu
# skips the agent with a warning, while pacman -S lcm-agent and pacman -U still
# install it.
pin_pacman() {
	_conf="$ROOT/etc/pacman.conf"
	[ -f "$_conf" ] || return 0
	grep -Eq '^[[:space:]]*IgnorePkg[[:space:]]*=.*(=|[[:space:]])lcm-agent([[:space:]]|$)' "$_conf" && return 0
	_tmp=$(mktemp 2>/dev/null) || return 0
	awk '{ print } /^\[options\][ \t]*$/ && !done { print "IgnorePkg = lcm-agent"; done = 1 }' "$_conf" >"$_tmp" 2>/dev/null &&
		[ -s "$_tmp" ] && cat "$_tmp" >"$_conf"
	rm -f "$_tmp"
}

# pin_manager pins on rpm (dnf, zypper) and pacman systems.
pin_manager() {
	case "$(pkg_kind "${1:-}")" in
	rpm)
		pin_dnf
		pin_zypper
		;;
	pacman) pin_pacman ;;
	esac
}

# hint_apk tells a first install on Alpine how to keep the version through
# apk upgrade (see the table above).
hint_apk() {
	[ "$(pkg_kind "${1:-}")" = apk ] || return 0
	_world="$ROOT/etc/apk/world"
	grep -qs '^lcm-agent=' "$_world" && return 0
	_v=${1%-r[0-9]*}
	if [ "$(lcm_lang)" = "de" ]; then
		echo "lcm-agent: apk upgrade wuerde den Agent mitaktualisieren. Version halten: apk add lcm-agent=$_v."
	else
		echo "lcm-agent: apk upgrade would update the agent with the system. To keep this version: apk add lcm-agent=$_v."
	fi
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

pin_manager "${1:-}" || true

if is_upgrade "${1:-}" "${2:-}"; then
	restart_after_upgrade
else
	hint_apk "${1:-}" || true
fi

# Only apt systems have /etc/apt; the same script also serves rpm, apk, pacman.
migrate_apt_suite || true

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
