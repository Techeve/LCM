#!/bin/sh
# agent-scripts-test - runs the lcm-agent package scripts against stubbed
# systemctl, rc-service and rc-update, with the argument conventions of deb,
# rpm, apk and pacman, and checks which service actions they take:
#
# - stop and disable only on a real removal, never on an upgrade
# - restart after an upgrade, but never from inside an agent command
#   (that would kill the package transaction running the upgrade)
# - hold the agent out of system upgrades on rpm (dnf exclude, zypper lock) and
#   pacman (IgnorePkg), hint at the pin command on apk, and lift the holds again
#   on removal - without touching the entries of other sections and packages
# - move the apt source of the community channel from the suite stable to
#   community - and nothing else (enterprise, beta, foreign repositories)
#
#   sh packaging/scripts/agent-scripts-test.sh
#
# The scripts run under the shell in LCM_TEST_SHELL (default "sh"), e.g.
# LCM_TEST_SHELL=dash or "busybox sh" or "bash --posix": the CI job
# packaging:shells runs the test once for each, since dpkg (dash), apk (busybox)
# and rpm (bash) run package scripts with different shells.
#
# Needs neither root nor Linux; go test runs it (internal/agent). The scripts
# read the filesystem below LCM_PKG_ROOT, a fake /proc included.
set -eu

DIR=$(cd "$(dirname "$0")" && pwd)
# Left unquoted where it is used: "busybox sh" is two words.
TEST_SH=${LCM_TEST_SHELL:-sh}
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
FAILS=0

# stub writes a command that only logs its call; "status" succeeds when
# $WORK/started exists.
stub() {
	cat >"$WORK/bin/$1" <<EOF
#!/bin/sh
echo "$1 \$*" >>"$WORK/calls"
case "\$*" in *status*) [ -e "$WORK/started" ] ;; esac
EOF
	chmod +x "$WORK/bin/$1"
}

# fake_proc adds a process to the fake /proc.
fake_proc() {
	mkdir -p "$WORK/root/proc/$1"
	echo "$2" >"$WORK/root/proc/$1/comm"
	printf 'Name:\t%s\nPPid:\t%s\n' "$2" "$3" >"$WORK/root/proc/$1/status"
}

# The stubs are written once: macOS checks every new executable on its first
# start, fresh stubs per case would cost seconds. The scripts pick systemd by
# $LCM_PKG_ROOT/run/systemd/system, so rc-service being there does no harm.
mkdir -p "$WORK/bin"
for c in systemctl rc-service rc-update; do
	stub "$c"
done

# setup <systemd|openrc> [started]: fresh root and an agent that runs a
# command (pid 4001, a shell below lcm-agent with pid 4000).
setup() {
	rm -rf "$WORK/root" "$WORK/started"
	mkdir -p "$WORK/root/etc/lcm-agent"
	: >"$WORK/calls"
	: >"$WORK/root/etc/lcm-agent/agent.json"
	fake_proc 4000 lcm-agent 1
	fake_proc 4001 sh 4000
	if [ "$1" = systemd ]; then
		mkdir -p "$WORK/root/run/systemd/system"
	fi
	if [ "${2:-}" = started ]; then
		: >"$WORK/started"
	fi
}

# run <script> <agent|admin> <args...>: runs a script the way dpkg, rpm and
# apk do - as its own shell. "agent" makes it a descendant of lcm-agent.
# exec keeps the PID, so the fake /proc entry written first is the script's.
run() {
	_script=$1 _caller=$2
	shift 2
	LC_ALL=C PATH="$WORK/bin:$PATH" LCM_PKG_ROOT="$WORK/root" LCM_TEST_SHELL="$TEST_SH" $TEST_SH -c '
		parent=1; [ "$1" = agent ] && parent=4001
		mkdir -p "$LCM_PKG_ROOT/proc/$$"
		echo sh >"$LCM_PKG_ROOT/proc/$$/comm"
		printf "PPid:\t%s\n" "$parent" >"$LCM_PKG_ROOT/proc/$$/status"
		shift; exec $LCM_TEST_SHELL "$@"' sh "$_caller" "$DIR/$_script" "$@" >"$WORK/out" 2>&1
}

# run_pacman <script> <function> <agent|admin> <args...>: pacman wraps the
# script into a bash function in .INSTALL (nfpm) and calls that function.
run_pacman() {
	_script=$1 _func=$2 _caller=$3
	shift 3
	{
		echo "$_func() {"
		cat "$DIR/$_script"
		echo "}"
	} >"$WORK/.INSTALL"
	LC_ALL=C PATH="$WORK/bin:$PATH" LCM_PKG_ROOT="$WORK/root" bash -c '
		parent=1; [ "$1" = agent ] && parent=4001
		mkdir -p "$LCM_PKG_ROOT/proc/$$"
		echo bash >"$LCM_PKG_ROOT/proc/$$/comm"
		printf "PPid:\t%s\n" "$parent" >"$LCM_PKG_ROOT/proc/$$/status"
		. "$2"; func=$3; shift 3; "$func" "$@"' bash "$_caller" "$WORK/.INSTALL" "$_func" "$@" >"$WORK/out" 2>&1
}

# check <name> <expected call>...: compares the logged calls.
check() {
	_name=$1
	shift
	_want=$(printf '%s\n' "$@")
	_got=$(cat "$WORK/calls")
	if [ "$_got" = "$_want" ]; then
		echo "ok   $_name"
		return 0
	fi
	echo "FAIL $_name"
	echo "     want: $(echo "$_want" | tr '\n' '|')"
	echo "     got:  $(echo "$_got" | tr '\n' '|')"
	FAILS=$((FAILS + 1))
}

# check_output <name> <text>: the script told the admin something.
check_output() {
	if grep -q "$2" "$WORK/out"; then
		echo "ok   $1"
		return 0
	fi
	echo "FAIL $1: output lacks '$2':"
	sed 's/^/     /' "$WORK/out"
	FAILS=$((FAILS + 1))
}

# check_file <name> <file below the fake root> <expected content>: compares a
# file of the fake root with the expected text.
check_file() {
	_got=$(cat "$WORK/root/$2")
	if [ "$_got" = "$3" ]; then
		echo "ok   $1"
		return 0
	fi
	echo "FAIL $1"
	echo "     want: $(echo "$3" | tr '\n' '|')"
	echo "     got:  $(echo "$_got" | tr '\n' '|')"
	FAILS=$((FAILS + 1))
}

# check_absent <name> <file below the fake root>: the file does not exist.
check_absent() {
	if [ ! -e "$WORK/root/$2" ]; then
		echo "ok   $1"
		return 0
	fi
	echo "FAIL $1: $2 exists"
	FAILS=$((FAILS + 1))
}

STOP="systemctl stop lcm-agent.service"
DISABLE="systemctl disable lcm-agent.service"
RELOAD="systemctl daemon-reload"
RESTART="systemctl try-restart lcm-agent.service"

for s in agent-preremove.sh agent-postinstall.sh agent-postremove.sh; do
	$TEST_SH -n "$DIR/$s"
done

# --- preremove: stop only on removal ---------------------------------------
setup systemd
run agent-preremove.sh agent remove
check "deb prerm remove stops and disables" "$STOP" "$DISABLE"
setup systemd
run agent-preremove.sh agent upgrade 2.1.0
check "deb prerm upgrade leaves the agent running" ""
setup systemd
run agent-preremove.sh agent failed-upgrade 2.0.0
check "deb prerm failed-upgrade leaves the agent running" ""
setup systemd
run agent-preremove.sh admin 0
check "rpm %preun 0 stops and disables" "$STOP" "$DISABLE"
setup systemd
run agent-preremove.sh agent 1
check "rpm %preun 1 leaves the agent running" ""
setup openrc started
run agent-preremove.sh admin 2.0.0-r0
check "apk .pre-deinstall stops and removes from the runlevel" \
	"rc-service lcm-agent stop" "rc-update del lcm-agent default"

# --- postinstall: restart after upgrade, never from inside the agent -------
setup systemd
run agent-postinstall.sh admin configure ""
check "deb postinst on install only reloads" "$RELOAD"
setup systemd
run agent-postinstall.sh admin configure 2.0.0
check "deb postinst upgrade by an admin restarts" "$RELOAD" "$RESTART"
setup systemd
run agent-postinstall.sh agent configure 2.0.0
check "deb postinst upgrade through the agent does not restart" "$RELOAD"
check_output "deb postinst through the agent says so" "LCM command"
setup systemd
run agent-postinstall.sh admin abort-upgrade 2.1.0
check "deb postinst abort-upgrade does not restart" "$RELOAD"
setup systemd
run agent-postinstall.sh admin 1
check "rpm %post 1 on install only reloads" "$RELOAD"
setup systemd
run agent-postinstall.sh admin 2
check "rpm %post 2 by an admin restarts" "$RELOAD" "$RESTART"
setup systemd
run agent-postinstall.sh agent 2
check "rpm %post 2 through the agent does not restart" "$RELOAD"
setup openrc started
run agent-postinstall.sh admin 2.1.0-r0
check "apk .post-install does not restart" ""
setup openrc started
run agent-postinstall.sh admin 2.1.0-r0 2.0.0-r0
check "apk .post-upgrade by an admin restarts a started agent" \
	"rc-service lcm-agent status" "rc-service lcm-agent restart"
setup openrc
run agent-postinstall.sh admin 2.1.0-r0 2.0.0-r0
check "apk .post-upgrade leaves a stopped agent stopped" "rc-service lcm-agent status"
setup openrc started
run agent-postinstall.sh agent 2.1.0-r0 2.0.0-r0
check "apk .post-upgrade through the agent does not restart" ""

# --- postinstall: apt source stable -> community -----------------------------
KEY="[signed-by=/etc/apt/keyrings/techeve.asc]"
SRCD=etc/apt/sources.list.d

# setup_apt: an apt system with the community source in every notation, next
# to sources that must stay exactly as they are.
setup_apt() {
	setup systemd
	_apt="$WORK/root/etc/apt"
	mkdir -p "$_apt/sources.list.d"
	cat >"$_apt/sources.list" <<EOF
# main sources
deb http://archive.ubuntu.com/ubuntu noble main
deb https://repo.techeve.de stable main
deb https://repo.example stable main
EOF
	echo "deb $KEY https://repo.techeve.de stable main" >"$_apt/sources.list.d/techeve.list"
	echo "deb [ arch=amd64  signed-by=/k.asc ] http://repo.techeve.de/ stable main contrib" >"$_apt/sources.list.d/slash.list"
	echo "deb $KEY https://repo.techeve.de/enterprise/lcm enterprise main" >"$_apt/sources.list.d/lcm-enterprise.list"
	echo "deb $KEY https://repo.techeve.de/enterprise stable main" >"$_apt/sources.list.d/old-enterprise.list"
	echo "deb $KEY https://repo.techeve.de/internal stable main" >"$_apt/sources.list.d/internal.list"
	echo "deb $KEY https://repo.techeve.de beta main" >"$_apt/sources.list.d/lcm-beta.list"
	echo "deb $KEY https://repo.techeve.de.example stable main" >"$_apt/sources.list.d/lookalike.list"
	echo "deb $KEY https://repo.techeve.de stable-updates main" >"$_apt/sources.list.d/other-suite.list"
	cat >"$_apt/sources.list.d/techeve.sources" <<EOF
# Techeve package repository
Types: deb
URIs: https://repo.techeve.de
Suites: stable
Components: main
Signed-By: /etc/apt/keyrings/techeve.gpg
EOF
	cat >"$_apt/sources.list.d/mixed.sources" <<EOF
Types: deb
URIs: https://deb.example
Suites: stable
Components: main

Types: deb
URIs: https://repo.techeve.de/
Suites: stable
Components: main
EOF
	cat >"$_apt/sources.list.d/enterprise.sources" <<EOF
Types: deb
URIs: https://repo.techeve.de/enterprise/lcm
Suites: stable
Components: main
EOF
	cat >"$_apt/sources.list.d/beta.sources" <<EOF
Types: deb
URIs: https://repo.techeve.de
Suites: beta
Components: main
EOF
}

# check_apt_migrated <prefix>: every source after the migration.
check_apt_migrated() {
	check_file "$1: sources.list moves only the techeve line" etc/apt/sources.list \
		"# main sources
deb http://archive.ubuntu.com/ubuntu noble main
deb https://repo.techeve.de community main
deb https://repo.example stable main"
	check_file "$1: one-line source with signed-by" $SRCD/techeve.list \
		"deb $KEY https://repo.techeve.de community main"
	check_file "$1: http, trailing slash, spaced options" $SRCD/slash.list \
		"deb [ arch=amd64  signed-by=/k.asc ] http://repo.techeve.de/ community main contrib"
	check_file "$1: deb822 stanza" $SRCD/techeve.sources \
		"# Techeve package repository
Types: deb
URIs: https://repo.techeve.de
Suites: community
Components: main
Signed-By: /etc/apt/keyrings/techeve.gpg"
	check_file "$1: deb822 only the techeve stanza" $SRCD/mixed.sources \
		"Types: deb
URIs: https://deb.example
Suites: stable
Components: main

Types: deb
URIs: https://repo.techeve.de/
Suites: community
Components: main"
	check_file "$1: enterprise line untouched" $SRCD/lcm-enterprise.list \
		"deb $KEY https://repo.techeve.de/enterprise/lcm enterprise main"
	check_file "$1: old enterprise path untouched" $SRCD/old-enterprise.list \
		"deb $KEY https://repo.techeve.de/enterprise stable main"
	check_file "$1: internal path untouched" $SRCD/internal.list \
		"deb $KEY https://repo.techeve.de/internal stable main"
	check_file "$1: beta line untouched" $SRCD/lcm-beta.list \
		"deb $KEY https://repo.techeve.de beta main"
	check_file "$1: lookalike host untouched" $SRCD/lookalike.list \
		"deb $KEY https://repo.techeve.de.example stable main"
	check_file "$1: other suite untouched" $SRCD/other-suite.list \
		"deb $KEY https://repo.techeve.de stable-updates main"
	check_file "$1: deb822 enterprise untouched" $SRCD/enterprise.sources \
		"Types: deb
URIs: https://repo.techeve.de/enterprise/lcm
Suites: stable
Components: main"
	check_file "$1: deb822 beta untouched" $SRCD/beta.sources \
		"Types: deb
URIs: https://repo.techeve.de
Suites: beta
Components: main"
}

# apt_state: one checksum over all apt sources.
apt_state() {
	cat "$WORK/root/etc/apt/sources.list" "$WORK/root/$SRCD"/* | cksum
}

setup_apt
run agent-postinstall.sh admin configure ""
check_apt_migrated "agent deb postinst"
check_output "agent postinst says which source it moved" "techeve.list moved from suite stable to community"
_before=$(apt_state)
run agent-postinstall.sh admin configure 2.0.0
if [ "$_before" = "$(apt_state)" ] && ! grep -q "moved from suite" "$WORK/out"; then
	echo "ok   agent postinst: second run changes nothing and says nothing"
else
	echo "FAIL agent postinst: second run changed something"
	FAILS=$((FAILS + 1))
fi
setup systemd
run agent-postinstall.sh admin 2
[ ! -e "$WORK/root/etc/apt" ] || {
	echo "FAIL agent postinst created /etc/apt on a system without apt"
	FAILS=$((FAILS + 1))
}

# postinstall.sh of the lcm package does much more (user, sudo, service) and
# cannot run in here. Its migration is a copy of the one above (dpkg scripts
# cannot share code): check that it is the same text, and run that copy too.
migration_of() {
	awk '/^# migrate_apt_suite moves/ { on = 1 } /^# (is_upgrade tells|1\. Dedicated)/ { on = 0 } on' "$DIR/$1"
}
if [ -n "$(migration_of agent-postinstall.sh)" ] &&
	[ "$(migration_of agent-postinstall.sh)" = "$(migration_of postinstall.sh)" ]; then
	echo "ok   postinstall.sh carries the same migration as agent-postinstall.sh"
else
	echo "FAIL postinstall.sh and agent-postinstall.sh differ in migrate_apt_suite"
	FAILS=$((FAILS + 1))
fi
setup_apt
{
	echo 'lcm_lang() { echo en; }'
	migration_of postinstall.sh
	echo 'migrate_apt_suite'
} >"$WORK/migration.sh"
ROOT="$WORK/root" $TEST_SH "$WORK/migration.sh" >"$WORK/out" 2>&1
check_apt_migrated "lcm postinst"
_before=$(apt_state)
ROOT="$WORK/root" $TEST_SH "$WORK/migration.sh" >"$WORK/out" 2>&1
[ "$_before" = "$(apt_state)" ] && [ ! -s "$WORK/out" ] || {
	echo "FAIL lcm postinst: second run changed something"
	FAILS=$((FAILS + 1))
}

if command -v bash >/dev/null 2>&1; then
	setup systemd
	run_pacman agent-preremove.sh pre_remove admin 2.0.0-1
	check "pacman pre_remove stops and disables" "$STOP" "$DISABLE"
	setup systemd
	run_pacman agent-postinstall.sh post_install admin 2.1.0-1
	check "pacman post_install only reloads" "$RELOAD"
	setup systemd
	run_pacman agent-postinstall.sh post_upgrade admin 2.1.0-1 2.0.0-1
	check "pacman post_upgrade by an admin restarts" "$RELOAD" "$RESTART"
	setup systemd
	run_pacman agent-postinstall.sh post_upgrade agent 2.1.0-1 2.0.0-1
	check "pacman post_upgrade through the agent does not restart" "$RELOAD"
else
	echo "skip pacman cases (no bash)"
fi

# --- postinstall: the holds of dnf, zypper and pacman -----------------------
REPOFILE=etc/yum.repos.d/techeve-lcm.repo
LOCKS=etc/zypp/locks
PACMANCONF=etc/pacman.conf
LOCK_STANZA='
type: package
match_type: glob
case_sensitive: on
solvable_name: lcm-agent'

setup systemd
mkdir -p "$WORK/root/etc/yum.repos.d"
printf '[techeve-lcm]\nname=Techeve\nenabled=1\n' >"$WORK/root/$REPOFILE"
run agent-postinstall.sh admin 1
check_file "rpm %post puts the exclude into the repo file" "$REPOFILE" '[techeve-lcm]
exclude=lcm-agent
name=Techeve
enabled=1'
run agent-postinstall.sh admin 2
check_file "rpm %post leaves a repo file with the exclude alone" "$REPOFILE" '[techeve-lcm]
exclude=lcm-agent
name=Techeve
enabled=1'
printf '[techeve-lcm]\nexclude=foo bar\nenabled=1\n' >"$WORK/root/$REPOFILE"
run agent-postinstall.sh admin 2
check_file "rpm %post extends an existing exclude line" "$REPOFILE" '[techeve-lcm]
exclude=foo bar lcm-agent
enabled=1'
# A repo file with more than one section: only [techeve-lcm] counts.
printf '[other]\nexclude=foo\nenabled=1\n[techeve-lcm]\nname=Techeve\n[third]\nexclude=lcm-agent\n' >"$WORK/root/$REPOFILE"
run agent-postinstall.sh admin 1
check_file "rpm %post ignores the excludes of other sections" "$REPOFILE" '[other]
exclude=foo
enabled=1
[techeve-lcm]
exclude=lcm-agent
name=Techeve
[third]
exclude=lcm-agent'
printf '[other]\nexclude=lcm-agent\n[techeve-lcm]\nname=Techeve\n[third]\nexclude=bar\n' >"$WORK/root/$REPOFILE"
run agent-postinstall.sh admin 1
check_file "rpm %post does not take an exclude of another section for its own" "$REPOFILE" '[other]
exclude=lcm-agent
[techeve-lcm]
exclude=lcm-agent
name=Techeve
[third]
exclude=bar'
printf '[other]\nexclude=foo\n[techeve-lcm]\nexcludepkgs=bar\nname=Techeve\n[third]\nexclude=baz\n' >"$WORK/root/$REPOFILE"
run agent-postinstall.sh admin 2
check_file "rpm %post extends the exclude of its own section only" "$REPOFILE" '[other]
exclude=foo
[techeve-lcm]
excludepkgs=bar lcm-agent
name=Techeve
[third]
exclude=baz'
printf '[techeve-lcm]\nexclude=lcm-agent-foo\n' >"$WORK/root/$REPOFILE"
run agent-postinstall.sh admin 2
check_file "rpm %post checks the exact package name" "$REPOFILE" '[techeve-lcm]
exclude=lcm-agent-foo lcm-agent'
printf '[techeve-lcm]\nexclude=foo,lcm-agent\n' >"$WORK/root/$REPOFILE"
run agent-postinstall.sh admin 2
check_file "rpm %post leaves a comma separated exclude alone" "$REPOFILE" '[techeve-lcm]
exclude=foo,lcm-agent'

setup systemd
run agent-postinstall.sh admin 1
check_absent "rpm %post creates no repo file" "$REPOFILE"
check_absent "rpm %post without zypp writes no locks" "$LOCKS"
mkdir -p "$WORK/root/etc/yum.repos.d"
printf '[techeve-lcm]\nname=Techeve\n' >"$WORK/root/$REPOFILE"
run agent-postinstall.sh admin configure ""
check_file "deb postinst leaves the repo file alone" "$REPOFILE" '[techeve-lcm]
name=Techeve'

setup systemd
mkdir -p "$WORK/root/etc/zypp"
run agent-postinstall.sh admin 1
check_file "rpm %post locks the package for zypper" "$LOCKS" "$LOCK_STANZA"
run agent-postinstall.sh admin 2
check_file "rpm %post does not lock twice" "$LOCKS" "$LOCK_STANZA"
printf '\ntype: package\nsolvable_name: other\n' >"$WORK/root/$LOCKS"
run agent-postinstall.sh admin 2
if [ "$(grep -c 'solvable_name' "$WORK/root/$LOCKS")" = 2 ]; then
	echo "ok   rpm %post adds its lock next to the admin's"
else
	echo "FAIL rpm %post adds its lock next to the admin's"
	FAILS=$((FAILS + 1))
fi

setup systemd
mkdir -p "$WORK/root/etc"
printf '[options]\nHoldPkg = pacman\n[core]\nInclude = /etc/pacman.d/mirrorlist\n' >"$WORK/root/$PACMANCONF"
run agent-postinstall.sh admin 2.1.0-1
check_file "pacman post_install sets IgnorePkg in [options]" "$PACMANCONF" '[options]
IgnorePkg = lcm-agent
HoldPkg = pacman
[core]
Include = /etc/pacman.d/mirrorlist'
run agent-postinstall.sh admin 2.2.0-1 2.1.0-1
check_file "pacman post_upgrade does not set it twice" "$PACMANCONF" '[options]
IgnorePkg = lcm-agent
HoldPkg = pacman
[core]
Include = /etc/pacman.d/mirrorlist'
printf '[options]\nIgnorePkg = foo bar\n' >"$WORK/root/$PACMANCONF"
run agent-postinstall.sh admin 2.2.0-1 2.1.0-1
check_file "pacman post_upgrade adds a line next to the admin's IgnorePkg" "$PACMANCONF" '[options]
IgnorePkg = lcm-agent
IgnorePkg = foo bar'
printf '[options]\nIgnorePkg = foo lcm-agent\n' >"$WORK/root/$PACMANCONF"
run agent-postinstall.sh admin 2.2.0-1 2.1.0-1
check_file "pacman post_upgrade respects an IgnorePkg the admin set" "$PACMANCONF" '[options]
IgnorePkg = foo lcm-agent'
run agent-postinstall.sh admin configure ""
check_file "deb postinst leaves pacman.conf alone" "$PACMANCONF" '[options]
IgnorePkg = foo lcm-agent'

# apk: the pin lives in /etc/apk/world, which apk rewrites after the scripts. A
# first install only gets a hint.
setup openrc
mkdir -p "$WORK/root/etc/apk"
run agent-postinstall.sh admin 2.1.0-r0
check_output "apk .post-install hints at the pin command" "apk add lcm-agent=2.1.0"
run agent-postinstall.sh admin 2.1.0-r0 2.0.0-r0
if grep -q "apk add lcm-agent=" "$WORK/out"; then
	echo "FAIL apk .post-upgrade must not repeat the hint"
	FAILS=$((FAILS + 1))
else
	echo "ok   apk .post-upgrade does not repeat the hint"
fi
printf 'busybox\nlcm-agent=2.1.0\n' >"$WORK/root/etc/apk/world"
run agent-postinstall.sh admin 2.1.0-r0
if grep -q "apk add lcm-agent=" "$WORK/out"; then
	echo "FAIL apk .post-install must not hint when the pin is there"
	FAILS=$((FAILS + 1))
else
	echo "ok   apk .post-install stays quiet when the version is pinned"
fi

# --- postremove: purge removes the configuration --------------------------
setup systemd
run agent-postremove.sh admin remove
check "deb postrm remove reloads" "$RELOAD"
[ -e "$WORK/root/etc/lcm-agent/agent.json" ] || {
	echo "FAIL deb postrm remove deleted the configuration"
	FAILS=$((FAILS + 1))
}
setup systemd
run agent-postremove.sh admin purge
check "deb postrm purge reloads" "$RELOAD"
[ ! -e "$WORK/root/etc/lcm-agent" ] || {
	echo "FAIL deb postrm purge kept the configuration"
	FAILS=$((FAILS + 1))
}
setup systemd
run agent-postremove.sh admin 1
[ -e "$WORK/root/etc/lcm-agent/agent.json" ] || {
	echo "FAIL rpm %postun 1 deleted the configuration"
	FAILS=$((FAILS + 1))
}

# The holds of dnf, zypper and pacman go with the package on a real removal -
# and only the part that belongs to lcm-agent.
LOCKS_OTHER='
type: package
solvable_name: other'
setup systemd
mkdir -p "$WORK/root/etc/yum.repos.d" "$WORK/root/etc/zypp"
for arg in 0 2.1.0-1 remove; do
	printf '[other]\nexclude=lcm-agent\n[techeve-lcm]\nexclude=lcm-agent\nname=Techeve\n' >"$WORK/root/$REPOFILE"
	run agent-postremove.sh admin "$arg"
	check_file "postremove $arg takes the exclude out of the repo file" "$REPOFILE" '[other]
exclude=lcm-agent
[techeve-lcm]
name=Techeve'
done
printf '[techeve-lcm]\nexclude=foo lcm-agent bar\n' >"$WORK/root/$REPOFILE"
run agent-postremove.sh admin 0
check_file "postremove keeps the other excludes" "$REPOFILE" '[techeve-lcm]
exclude=foo bar'
printf '[techeve-lcm]\nexcludepkgs = lcm-agent, foo\n[x]\nexclude=lcm-agent\n' >"$WORK/root/$REPOFILE"
run agent-postremove.sh admin 0
check_file "postremove handles excludepkgs and commas" "$REPOFILE" '[techeve-lcm]
excludepkgs = foo
[x]
exclude=lcm-agent'
printf '[techeve-lcm]\nexclude=lcm-agent-foo\n' >"$WORK/root/$REPOFILE"
run agent-postremove.sh admin 0
check_file "postremove touches the exact package name only" "$REPOFILE" '[techeve-lcm]
exclude=lcm-agent-foo'
printf '[techeve-lcm]\nexclude=lcm-agent\n' >"$WORK/root/$REPOFILE"
run agent-postremove.sh admin 1
check_file "rpm %postun 1 (upgrade) keeps the exclude" "$REPOFILE" '[techeve-lcm]
exclude=lcm-agent'
run agent-postremove.sh admin upgrade
check_file "deb postrm upgrade keeps the exclude" "$REPOFILE" '[techeve-lcm]
exclude=lcm-agent'

for arg in 0 2.1.0-1 remove; do
	printf '%s\n\ntype: package\nsolvable_name: lcm-agent\n' "$LOCKS_OTHER" >"$WORK/root/$LOCKS"
	run agent-postremove.sh admin "$arg"
	check_file "postremove $arg takes the lock out of /etc/zypp/locks" "$LOCKS" "$LOCKS_OTHER"
	run agent-postremove.sh admin "$arg"
	check_file "postremove $arg is idempotent for the locks" "$LOCKS" "$LOCKS_OTHER"
done
printf '\ntype: package\nsolvable_name: lcm-agent\n\ntype: package\nsolvable_name: other\n\ntype: package\nsolvable_name: lcm-agent-foo\n' >"$WORK/root/$LOCKS"
run agent-postremove.sh admin 0
check_file "postremove takes the lock from the middle of the file" "$LOCKS" '
type: package
solvable_name: other

type: package
solvable_name: lcm-agent-foo'
printf '\ntype: package\nsolvable_name: lcm-agent\n' >"$WORK/root/$LOCKS"
run agent-postremove.sh admin 0
if [ -f "$WORK/root/$LOCKS" ] && [ ! -s "$WORK/root/$LOCKS" ]; then
	echo "ok   postremove leaves an empty locks file when it was the only lock"
else
	echo "FAIL postremove with only the lcm-agent lock"
	FAILS=$((FAILS + 1))
fi
printf '%s\n' "$LOCK_STANZA" >"$WORK/root/$LOCKS"
run agent-postremove.sh admin 2
check_file "rpm %postun 2 (upgrade) keeps the lock" "$LOCKS" "$LOCK_STANZA"
rm -f "$WORK/root/$LOCKS"
run agent-postremove.sh admin 0
check_absent "postremove does not create the locks file" "$LOCKS"

for arg in 0 2.1.0-1 remove; do
	printf '[options]\nIgnorePkg = lcm-agent\nHoldPkg = pacman\n[core]\nInclude = /etc/pacman.d/mirrorlist\n' >"$WORK/root/$PACMANCONF"
	run agent-postremove.sh admin "$arg"
	check_file "postremove $arg drops the IgnorePkg line it added" "$PACMANCONF" '[options]
HoldPkg = pacman
[core]
Include = /etc/pacman.d/mirrorlist'
	run agent-postremove.sh admin "$arg"
	check_file "postremove $arg is idempotent for pacman.conf" "$PACMANCONF" '[options]
HoldPkg = pacman
[core]
Include = /etc/pacman.d/mirrorlist'
done
printf '[options]\nIgnorePkg = foo lcm-agent bar\nIgnorePkg = lcm-agent\n' >"$WORK/root/$PACMANCONF"
run agent-postremove.sh admin 2.1.0-1
check_file "postremove keeps the other entries of IgnorePkg" "$PACMANCONF" '[options]
IgnorePkg = foo bar'
printf '[options]\n#IgnorePkg = lcm-agent\nIgnorePkg = lcm-agent-foo\n' >"$WORK/root/$PACMANCONF"
run agent-postremove.sh admin 2.1.0-1
check_file "postremove touches neither comments nor other packages" "$PACMANCONF" '[options]
#IgnorePkg = lcm-agent
IgnorePkg = lcm-agent-foo'
printf '[options]\nIgnorePkg = lcm-agent\n' >"$WORK/root/$PACMANCONF"
mkdir -p "$WORK/root/etc/apk"
run agent-postremove.sh admin 1
check_file "pacman post_upgrade-like removal arguments of an upgrade keep IgnorePkg" "$PACMANCONF" '[options]
IgnorePkg = lcm-agent'

if [ "$FAILS" -gt 0 ]; then
	echo "$FAILS case(s) failed"
	exit 1
fi
echo "all package script cases passed"
