#!/bin/sh
# agent-postinstall - registers the unit and keeps the package out of normal
# upgrades. The service deliberately does NOT start automatically: the unit
# carries ConditionPathExists=/etc/lcm-agent/agent.json, and only
# `lcm-agent enroll <url> <token>` writes that config and activates it.
#
# It NEVER restarts a running agent, neither on a first install nor on an
# upgrade: A restart (even from outside LCM) stops the unit, and a running LCM
# command would go with it - or the package transaction that runs inside it. The
# running agent notices the new binary on its own and switches to it (exec,
# same PID) once it is idle, and the guard script (lcm-agent-start) takes the
# way back if the new binary does not come up (internal/agent/switch.go,
# update.go).
#
# It also keeps the agent out of normal system upgrades, so that it is updated
# only on purpose, by LCM. Per package format:
#   deb     pin_apt: apt preferences pin the installed version
#           (LCM: apt-get install lcm-agent=<version>)
#   rpm     pin_dnf: exclude=lcm-agent in the repo file techeve-lcm
#           (LCM: dnf --setopt=disable_excludes=techeve-lcm install lcm-agent-<ver>)
#           pin_zypper: a package lock in /etc/zypp/locks
#           (LCM: zypper removelock, install, addlock)
#   pacman  pin_pacman: IgnorePkg = lcm-agent in /etc/pacman.conf
#           (LCM: pacman -S / pacman -U, both ignore IgnorePkg)
#   apk     nothing to write: apk rewrites /etc/apk/world at the end of the
#           transaction, which overwrites anything done here. `apk add
#           lcm-agent=<version>` pins the version there; LCM installs that way.
#           A first install gets a hint (hint_apk).
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

# tell_upgrade explains why nothing restarts.
tell_upgrade() {
	if [ "$(lcm_lang)" = "de" ]; then
		echo "lcm-agent: Der laufende Agent wechselt selbst auf die neue Version, sobald er untaetig ist (kein Neustart)."
	else
		echo "lcm-agent: the running agent switches to the new version by itself once it is idle (no restart)."
	fi
}

# pin_apt writes /etc/apt/preferences.d/lcm-agent: the installed version, with a
# priority above 1000. apt then treats exactly this version as the candidate -
# apt upgrade, full-upgrade and unattended-upgrades find nothing newer and leave
# the agent alone. An explicit `apt-get install lcm-agent=<version>` (add
# --allow-downgrades for an older one) still works: the version named on the
# command line wins over any pin, and this script then pins the new version.
#
# Why not apt-mark hold: it runs dpkg --set-selections / writes the dpkg status
# while dpkg holds its own lock - from a maintainer script that waits for
# itself. A plain file in preferences.d has no such problem.
#
# Only for deb (first argument "configure"): apt-based systems only. The
# installed version comes from dpkg-query; if that fails, nothing is pinned.
pin_apt() {
	[ "${1:-}" = configure ] || return 0
	[ -d "$ROOT/etc/apt" ] || return 0
	_v=$(dpkg-query -W -f='${Version}' lcm-agent 2>/dev/null) || return 0
	[ -n "$_v" ] || return 0
	mkdir -p "$ROOT/etc/apt/preferences.d" || return 0
	_pin="$ROOT/etc/apt/preferences.d/lcm-agent"
	{
		echo "# Written by the lcm-agent package: keeps lcm-agent at the installed version."
		echo "# LCM updates it on purpose: apt-get install lcm-agent=<version>. Removed with the package."
		echo "Package: lcm-agent"
		echo "Pin: version $_v"
		echo "Pin-Priority: 1001"
	} >"$_pin.tmp" || return 0
	mv -f "$_pin.tmp" "$_pin" || rm -f "$_pin.tmp"
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
# install; LCM disables the exclude of this one repo for its own installs.
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
# locked package is skipped by zypper update; LCM lifts the lock around its own
# install and sets it again. The file is written directly: zypper holds its own
# lock during the transaction that runs this script.
pin_zypper() {
	[ -d "$ROOT/etc/zypp" ] || return 0
	_locks="$ROOT/etc/zypp/locks"
	grep -qs '^solvable_name: lcm-agent$' "$_locks" && return 0
	printf '\ntype: package\nmatch_type: glob\ncase_sensitive: on\nsolvable_name: lcm-agent\n' >>"$_locks"
}

# pin_pacman adds IgnorePkg = lcm-agent to the [options] section: pacman -Syu
# skips the agent with a warning, while pacman -S lcm-agent and pacman -U (what
# LCM runs) still install it.
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
		echo "lcm-agent: apk upgrade wuerde den Agent mitaktualisieren. Version halten: apk add lcm-agent=$_v (LCM haelt ihn bei jedem Update selbst)."
	else
		echo "lcm-agent: apk upgrade would update the agent with the system. To keep this version: apk add lcm-agent=$_v (LCM does this itself on every update)."
	fi
}

if [ -d "$ROOT/run/systemd/system" ]; then
	systemctl daemon-reload >/dev/null 2>&1 || true
fi

pin_apt "${1:-}" || true
pin_manager "${1:-}" || true

if is_upgrade "${1:-}" "${2:-}"; then
	tell_upgrade
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
