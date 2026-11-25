#!/bin/sh
# agent-postremove - cleans up after removal. On "purge" the enrollment
# configuration (holding the connection secret) is removed as well; a plain
# "remove" keeps it for a reinstall.
#
# Only deb knows "purge". rpm calls this script with 0 (removal) or 1
# (upgrade), apk and pacman with the old version, and only on removal - none
# of them removes the configuration. An upgrade (deb "upgrade", rpm 1) keeps
# everything: the new package has just written its own hold.
#
# The holds that agent-postinstall.sh wrote for rpm and pacman go with the
# package on a real removal (dnf exclude, zypper lock, pacman IgnorePkg): they
# would otherwise stay behind for a package that is no longer there, and a later
# install by hand would find itself held.
#
# LCM_PKG_ROOT is for packaging/scripts/agent-scripts-test.sh only.
set -e

ROOT="${LCM_PKG_ROOT:-}"

if [ -d "$ROOT/run/systemd/system" ]; then
	systemctl daemon-reload >/dev/null 2>&1 || true
fi

# is_removal tells a real removal from an upgrade (see the header).
is_removal() {
	case "$1" in
	remove | purge | 0 | "") return 0 ;;
	upgrade | failed-upgrade | abort-* | disappear) return 1 ;;
	*[!0-9]*) return 0 ;; # apk, pacman: the old version, passed on removal only
	*) return 1 ;;        # rpm: number of versions left installed
	esac
}

# unpin_dnf takes lcm-agent out of the exclude list of the section
# [techeve-lcm] in the Techeve repo file (other entries stay; a line that
# becomes empty goes).
unpin_dnf() {
	_repo="$ROOT/etc/yum.repos.d/techeve-lcm.repo"
	[ -f "$_repo" ] || return 0
	_tmp=$(mktemp 2>/dev/null) || return 0
	awk '
		/^\[/ { sec = ($0 ~ /^\[techeve-lcm\][ \t]*$/) }
		sec && /^(exclude|excludepkgs)[ \t]*=/ {
			eq = index($0, "=")
			rest = substr($0, eq + 1)
			match(rest, /^[ \t]*/)
			pre = substr($0, 1, eq + RLENGTH)
			n = split(rest, t, "[ \t,]+")
			out = ""; hit = 0
			for (i = 1; i <= n; i++) {
				if (t[i] == "lcm-agent") { hit = 1; continue }
				if (t[i] != "") out = out (out == "" ? "" : " ") t[i]
			}
			if (hit) { if (out != "") print pre out; next }
		}
		{ print }
	' "$_repo" >"$_tmp" 2>/dev/null && ! cmp -s "$_tmp" "$_repo" && cat "$_tmp" >"$_repo"
	rm -f "$_tmp"
}

# unpin_zypper removes the lock block of lcm-agent from /etc/zypp/locks (the
# blocks are separated by blank lines; the blank lines in front of the removed
# block go with it). The file stays, even if it ends up empty.
unpin_zypper() {
	_locks="$ROOT/etc/zypp/locks"
	grep -qs '^solvable_name:[[:space:]]*lcm-agent[[:space:]]*$' "$_locks" || return 0
	_tmp=$(mktemp 2>/dev/null) || return 0
	awk '
		function flush(   i) {
			if (n > 0) {
				if (!drop) { printf "%s", blanks; for (i = 1; i <= n; i++) print buf[i] }
				blanks = ""; n = 0; drop = 0
			}
		}
		/^[ \t]*$/ { flush(); blanks = blanks $0 "\n"; next }
		{ buf[++n] = $0; if ($0 ~ /^solvable_name:[ \t]*lcm-agent[ \t]*$/) drop = 1 }
		END { flush(); printf "%s", blanks }
	' "$_locks" >"$_tmp" 2>/dev/null && cat "$_tmp" >"$_locks"
	rm -f "$_tmp"
}

# unpin_pacman takes lcm-agent out of IgnorePkg in /etc/pacman.conf (other
# packages in the line stay; a line that becomes empty goes).
unpin_pacman() {
	_conf="$ROOT/etc/pacman.conf"
	[ -f "$_conf" ] || return 0
	_tmp=$(mktemp 2>/dev/null) || return 0
	awk '
		/^[ \t]*IgnorePkg[ \t]*=/ {
			n = split(substr($0, index($0, "=") + 1), t, "[ \t]+")
			out = ""; hit = 0
			for (i = 1; i <= n; i++) {
				if (t[i] == "lcm-agent") { hit = 1; continue }
				if (t[i] != "") out = out (out == "" ? "" : " ") t[i]
			}
			if (hit) { if (out != "") print "IgnorePkg = " out; next }
		}
		{ print }
	' "$_conf" >"$_tmp" 2>/dev/null && ! cmp -s "$_tmp" "$_conf" && cat "$_tmp" >"$_conf"
	rm -f "$_tmp"
}

if is_removal "${1:-}"; then
	unpin_dnf || true
	unpin_zypper || true
	unpin_pacman || true
fi

if [ "${1:-}" = "purge" ]; then
	rm -rf "$ROOT/etc/lcm-agent"
fi

exit 0
