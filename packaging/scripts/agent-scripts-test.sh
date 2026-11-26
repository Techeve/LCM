#!/bin/sh
# agent-scripts-test - runs the lcm-agent package scripts against stubbed
# systemctl, rc-service and rc-update, with the argument conventions of deb,
# rpm, apk and pacman, and checks which service actions they take:
#
# - stop and disable only on a real removal, never on an upgrade
# - never restart the agent after an upgrade, wherever it comes from (the running
#   agent switches to the new binary itself); the unit is only reloaded
# - pin the installed version for apt (and remove the pin with the package)
# - keep the binary that is replaced as the way back (preinst)
# - lcm-agent-start: count starts, roll back to the previous binary, never give up
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
# dpkg-query answers with the version of the package that postinst just configured.
cat >"$WORK/bin/dpkg-query" <<EOF
#!/bin/sh
cat "$WORK/dpkgversion" 2>/dev/null || exit 1
EOF
chmod +x "$WORK/bin/dpkg-query"
# sleep is stubbed: the waits of lcm-agent-start cost nothing, and the last one is logged.
cat >"$WORK/bin/sleep" <<EOF
#!/bin/sh
echo "\$1" >>"$WORK/slept"
EOF
chmod +x "$WORK/bin/sleep"
# timeout (not on macOS): runs the command; the limit is of no interest here.
printf '#!/bin/sh\nshift\nexec "$@"\n' >"$WORK/bin/timeout"
chmod +x "$WORK/bin/timeout"

# setup <systemd|openrc> [started]: fresh root and an agent that runs a
# command (pid 4001, a shell below lcm-agent with pid 4000).
setup() {
	rm -rf "$WORK/root" "$WORK/started" "$WORK/dpkgversion"
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

STOP="systemctl stop lcm-agent.service"
DISABLE="systemctl disable lcm-agent.service"
RELOAD="systemctl daemon-reload"

for s in agent-preremove.sh agent-preinstall.sh agent-postinstall.sh agent-postremove.sh; do
	$TEST_SH -n "$DIR/$s"
done
$TEST_SH -n "$DIR/../lcm-agent-start.sh"

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

# --- postinstall: never a restart, wherever the upgrade comes from ---------
# The unit is reloaded and that is all - an admin's apt run, LCM's own update job
# (through the agent) and the other package formats alike. The running agent
# switches to the new binary itself once it is idle.
setup systemd
run agent-postinstall.sh admin configure ""
check "deb postinst on install only reloads" "$RELOAD"
setup systemd
run agent-postinstall.sh admin configure 2.0.0
check "deb postinst upgrade by an admin does not restart" "$RELOAD"
check_output "deb postinst upgrade says why" "no restart"
setup systemd
run agent-postinstall.sh agent configure 2.0.0
check "deb postinst upgrade through the agent does not restart" "$RELOAD"
setup systemd
run agent-postinstall.sh admin abort-upgrade 2.1.0
check "deb postinst abort-upgrade does not restart" "$RELOAD"
setup systemd
run agent-postinstall.sh admin 1
check "rpm %post 1 on install only reloads" "$RELOAD"
setup systemd
run agent-postinstall.sh admin 2
check "rpm %post 2 by an admin does not restart" "$RELOAD"
setup systemd
run agent-postinstall.sh agent 2
check "rpm %post 2 through the agent does not restart" "$RELOAD"
setup openrc started
run agent-postinstall.sh admin 2.1.0-r0
check "apk .post-install does not restart" ""
setup openrc started
run agent-postinstall.sh admin 2.1.0-r0 2.0.0-r0
check "apk .post-upgrade by an admin does not restart" ""
setup openrc
run agent-postinstall.sh admin 2.1.0-r0 2.0.0-r0
check "apk .post-upgrade leaves a stopped agent stopped" ""
setup openrc started
run agent-postinstall.sh agent 2.1.0-r0 2.0.0-r0
check "apk .post-upgrade through the agent does not restart" ""

# --- postinstall: the apt pin ------------------------------------------------
PIN=etc/apt/preferences.d/lcm-agent
UPD=var/lib/lcm-agent/update

# pin_body: the lines apt reads (the comments above them are not compared).
pin_body() {
	grep -v '^#' "$WORK/root/$PIN"
}

# check_pin <name> <version>: the pin file exists and pins exactly that version.
check_pin() {
	_want=$(printf 'Package: lcm-agent\nPin: version %s\nPin-Priority: 1001' "$2")
	if [ "$(pin_body 2>&1)" = "$_want" ]; then
		echo "ok   $1"
		return 0
	fi
	echo "FAIL $1: pin file is '$(pin_body 2>&1 | tr '\n' '|')'"
	FAILS=$((FAILS + 1))
}

# check_absent <name> <file below the fake root>
check_absent() {
	if [ ! -e "$WORK/root/$2" ]; then
		echo "ok   $1"
		return 0
	fi
	echo "FAIL $1: $2 exists"
	FAILS=$((FAILS + 1))
}

setup systemd
mkdir -p "$WORK/root/etc/apt"
echo "2.4.0" >"$WORK/dpkgversion"
run agent-postinstall.sh admin configure ""
check_pin "deb postinst pins the installed version" 2.4.0
echo "2.5.0~beta.1" >"$WORK/dpkgversion"
run agent-postinstall.sh admin configure 2.4.0
check_pin "deb postinst upgrade moves the pin to the new version" "2.5.0~beta.1"
echo "2.3.0" >"$WORK/dpkgversion"
run agent-postinstall.sh admin configure 2.5.0~beta.1
check_pin "deb postinst downgrade moves the pin to the older version" 2.3.0
check_absent "pin: no temporary file left behind" "$PIN.tmp"
setup systemd
mkdir -p "$WORK/root/etc/apt"
echo "2.4.0" >"$WORK/dpkgversion"
run agent-postinstall.sh admin abort-upgrade 2.1.0
check_absent "pin: abort-upgrade writes none" "$PIN"
run agent-postinstall.sh admin 2
check_absent "pin: rpm %post writes no apt pin" "$PIN"
setup systemd
mkdir -p "$WORK/root/etc/apt"
run agent-postinstall.sh admin configure ""
check_absent "pin: none when dpkg-query knows no version" "$PIN"
setup systemd
echo "2.4.0" >"$WORK/dpkgversion"
run agent-postinstall.sh admin configure ""
check_absent "pin: no /etc/apt is created on a system without apt" etc/apt

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

# --- preinstall: keep the binary that is replaced ----------------------------
# setup_bin <content>: an installed agent binary.
setup_bin() {
	mkdir -p "$WORK/root/usr/bin"
	printf '%s' "$1" >"$WORK/root/usr/bin/lcm-agent"
	chmod 755 "$WORK/root/usr/bin/lcm-agent"
}

# check_prev <name> <content>: the previous binary has this content and can run.
check_prev() {
	if [ "$(cat "$WORK/root/$UPD/lcm-agent.prev" 2>/dev/null)" = "$2" ] && [ -x "$WORK/root/$UPD/lcm-agent.prev" ]; then
		echo "ok   $1"
	else
		echo "FAIL $1: previous binary is '$(cat "$WORK/root/$UPD/lcm-agent.prev" 2>&1)'"
		FAILS=$((FAILS + 1))
	fi
}

setup systemd
run agent-preinstall.sh admin install
check_absent "preinst on a first install keeps nothing" "$UPD/lcm-agent.prev"
setup_bin "binary-A"
run agent-preinstall.sh admin upgrade 2.3.0
check_prev "preinst on an upgrade keeps the old binary" "binary-A"
setup_bin "binary-B"
run agent-preinstall.sh admin upgrade 2.4.0
check_prev "preinst keeps the binary of the next upgrade as well" "binary-B"
echo 1 >"$WORK/root/$UPD/starts"
setup_bin "binary-C"
run agent-preinstall.sh admin upgrade 2.5.0
check_prev "preinst leaves the way back alone while the current binary is unproven" "binary-B"
echo 0 >"$WORK/root/$UPD/starts"
echo "from=2.5.0" >"$WORK/root/$UPD/rolled-back"
run agent-preinstall.sh admin upgrade 2.5.0
check_prev "preinst leaves the way back alone during a rollback" "binary-B"
rm "$WORK/root/$UPD/rolled-back"
run agent-preinstall.sh admin upgrade 2.5.0
check_prev "preinst keeps the binary again once the agent is healthy" "binary-C"
_mode=$(stat -c %a "$WORK/root/$UPD" 2>/dev/null || stat -f %Lp "$WORK/root/$UPD")
if [ "$_mode" = 700 ]; then
	echo "ok   preinst: update state is 0700"
else
	echo "FAIL preinst: update state is $_mode"
	FAILS=$((FAILS + 1))
fi
setup systemd
setup_bin "binary-A"
mkdir -p "$WORK/root/var/lib"
: >"$WORK/root/var/lib/lcm-agent"
if run agent-preinstall.sh admin upgrade 2.3.0; then
	echo "ok   preinst does not fail the installation when it cannot copy"
else
	echo "FAIL preinst failed the installation"
	FAILS=$((FAILS + 1))
fi

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
	check "pacman post_upgrade by an admin does not restart" "$RELOAD"
	setup systemd
	run_pacman agent-postinstall.sh post_upgrade agent 2.1.0-1 2.0.0-1
	check "pacman post_upgrade through the agent does not restart" "$RELOAD"
else
	echo "skip pacman cases (no bash)"
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
mkdir -p "$WORK/root/var/lib/lcm-agent/jobs"
run agent-postremove.sh admin purge
check "deb postrm purge reloads" "$RELOAD"
[ ! -e "$WORK/root/etc/lcm-agent" ] || {
	echo "FAIL deb postrm purge kept the configuration"
	FAILS=$((FAILS + 1))
}
[ ! -e "$WORK/root/var/lib/lcm-agent" ] || {
	echo "FAIL deb postrm purge kept the job store"
	FAILS=$((FAILS + 1))
}
setup systemd
run agent-postremove.sh admin 1
[ -e "$WORK/root/etc/lcm-agent/agent.json" ] || {
	echo "FAIL rpm %postun 1 deleted the configuration"
	FAILS=$((FAILS + 1))
}


# The pin and the update state go with the package on every real removal, and
# stay on an upgrade (the new package has just written its own pin).
for case in "deb remove:remove" "deb purge:purge" "rpm 0:0" "apk 2.1.0-r0:2.1.0-r0" "pacman 2.1.0-1:2.1.0-1"; do
	name=${case%%:*}
	arg=${case#*:}
	setup systemd
	mkdir -p "$WORK/root/etc/apt/preferences.d" "$WORK/root/$UPD"
	echo pin >"$WORK/root/$PIN"
	echo 0 >"$WORK/root/$UPD/starts"
	run agent-postremove.sh admin "$arg"
	if [ ! -e "$WORK/root/$PIN" ] && [ ! -e "$WORK/root/$UPD" ]; then
		echo "ok   $name removes the pin and the update state"
	else
		echo "FAIL $name left the pin or the update state"
		FAILS=$((FAILS + 1))
	fi
done
for case in "deb upgrade:upgrade" "deb failed-upgrade:failed-upgrade" "deb abort-install:abort-install" "rpm 1:1" "rpm 2:2"; do
	name=${case%%:*}
	arg=${case#*:}
	setup systemd
	mkdir -p "$WORK/root/etc/apt/preferences.d" "$WORK/root/$UPD"
	echo pin >"$WORK/root/$PIN"
	echo 0 >"$WORK/root/$UPD/starts"
	run agent-postremove.sh admin "$arg"
	if [ -e "$WORK/root/$PIN" ] && [ -e "$WORK/root/$UPD/starts" ]; then
		echo "ok   $name keeps the pin and the update state"
	else
		echo "FAIL $name removed the pin or the update state"
		FAILS=$((FAILS + 1))
	fi
done

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

# --- lcm-agent-start: counting starts, rollback, never giving up ----------------
START="$DIR/../lcm-agent-start.sh"
SROOT="$WORK/sroot"

# fake_bin <path> <version>: a binary that records how it was started and
# answers "version".
fake_bin() {
	cat >"$1" <<EOF2
#!/bin/sh
[ "\$1" = version ] && { echo "lcm-agent $2"; exit 0; }
echo "$2 \$*" >>"$WORK/ran"
EOF2
	chmod 755 "$1"
}

# start_setup [prev]: current binary 2.4.0, and previous 2.3.0 when asked.
start_setup() {
	rm -rf "$SROOT" "$WORK/ran"
	: >"$WORK/slept"
	mkdir -p "$SROOT/state"
	fake_bin "$SROOT/lcm-agent" 2.4.0
	if [ "${1:-}" = prev ]; then
		fake_bin "$SROOT/state/lcm-agent.prev" 2.3.0
	fi
}

# start_once: one start by the service manager, which would run it again after
# every exit. Waits are stubbed (see above); the last one is in $WORK/slept.
start_once() {
	: >"$WORK/slept"
	LCM_AGENT_BIN="$SROOT/lcm-agent" LCM_AGENT_STATE="$SROOT/state" \
		PATH="$WORK/bin:$PATH" $TEST_SH "$START" run >"$WORK/out" 2>&1
}

# starts <n>: n starts in a row.
starts() {
	_i=0
	while [ "$_i" -lt "$1" ]; do
		start_once
		_i=$((_i + 1))
	done
}

# ran_last: version of the binary the last start ran.
ran_last() { tail -n 1 "$WORK/ran" | cut -d' ' -f1; }

# agent_healthy: what a healthy agent does (internal/agent/update.go).
agent_healthy() {
	echo 0 >"$SROOT/state/starts"
	rm -f "$SROOT/state/cycles" "$SROOT/state/fail-reason"
}

check_value() { # <name> <want> <got>
	if [ "$2" = "$3" ]; then
		echo "ok   $1"
	else
		echo "FAIL $1: want '$2', got '$3'"
		FAILS=$((FAILS + 1))
	fi
}

start_setup prev
start_once
check_value "start: the current binary runs with its arguments" "2.4.0 run" "$(tail -n 1 "$WORK/ran")"
check_value "start: the start is counted" 1 "$(cat "$SROOT/state/starts")"
starts 2
check_value "start: 3 unhealthy starts still run the current binary" "2.4.0" "$(ran_last)"
start_once
check_value "start: the 4th start runs the previous binary" "2.3.0" "$(ran_last)"
check_value "start: mode is prev after the rollback" prev "$(cat "$SROOT/state/mode")"
check_value "rollback record: from" "from=2.4.0" "$(grep ^from= "$SROOT/state/rolled-back")"
check_value "rollback record: to" "to=2.3.0" "$(grep ^to= "$SROOT/state/rolled-back")"
check_value "rollback record: reason" "reason=crash-loop" "$(grep ^reason= "$SROOT/state/rolled-back")"
if grep -q '^at=[0-9][0-9]*$' "$SROOT/state/rolled-back"; then
	echo "ok   rollback record: time"
else
	echo "FAIL rollback record: time"
	FAILS=$((FAILS + 1))
fi
_i=0
while [ "$_i" -lt 5 ]; do
	agent_healthy
	start_once
	_i=$((_i + 1))
done
check_value "start: a healthy previous binary stays" "2.3.0" "$(ran_last)"
check_value "start: and nothing was waited for" "" "$(cat "$WORK/slept")"

# the agent's own verdict (not ready within 5 minutes) rolls back at once and
# names the reason
start_setup prev
start_once
check_value "verdict: first start of a new binary runs it" "2.4.0" "$(ran_last)"
echo 3 >"$SROOT/state/starts"
echo not-ready >"$SROOT/state/fail-reason"
start_once
check_value "verdict: the next start runs the previous binary" "2.3.0" "$(ran_last)"
check_value "verdict: reason of the agent" "reason=not-ready" "$(grep ^reason= "$SROOT/state/rolled-back")"
[ ! -e "$SROOT/state/fail-reason" ] || {
	echo "FAIL verdict: fail-reason is not cleared"
	FAILS=$((FAILS + 1))
}

# both fail: back to the current binary, with a wait that grows, never given up
start_setup prev
starts 4
check_value "loop: previous runs after the rollback" "2.3.0" "$(ran_last)"
starts 2
check_value "loop: still the previous binary within its 3 starts" "2.3.0" "$(ran_last)"
start_once
check_value "loop: previous failed too - the current binary is tried again" "2.4.0" "$(ran_last)"
check_value "loop: it waits before that" "15" "$(cat "$WORK/slept")"
starts 2
check_value "loop: the current binary has its 3 starts again" "2.4.0" "$(ran_last)"
start_once
check_value "loop: failed again - the previous binary" "2.3.0" "$(ran_last)"
check_value "loop: and waits before that" "15" "$(cat "$WORK/slept")"
_max=0
_i=0
while [ "$_i" -lt 60 ]; do
	start_once
	_w=$(cat "$WORK/slept")
	[ -z "$_w" ] || [ "$_w" -le "$_max" ] || _max=$_w
	_i=$((_i + 1))
done
check_value "loop: the longest wait is capped at 2 minutes" 120 "$_max"
check_value "loop: the service still starts after 60 more failures" "true" "$([ -n "$(ran_last)" ] && echo true)"

# no previous binary: keep trying the current one, ever more slowly
start_setup
starts 5
check_value "no way back: the current binary keeps running" "2.4.0" "$(ran_last)"
check_value "no way back: and waits more each time" "30" "$(cat "$WORK/slept")"
# a previous binary that is the current one is no way back either
start_setup
cp "$SROOT/lcm-agent" "$SROOT/state/lcm-agent.prev"
starts 5
check_value "previous equals current: no rollback" "2.4.0" "$(ran_last)"
[ ! -e "$SROOT/state/rolled-back" ] || {
	echo "FAIL previous equals current: a rollback was recorded"
	FAILS=$((FAILS + 1))
}

# a new package (another file) starts over
start_setup prev
starts 4
check_value "new package: rolled back before" "2.3.0" "$(ran_last)"
fake_bin "$SROOT/lcm-agent.new" 2.5.0
mv "$SROOT/lcm-agent.new" "$SROOT/lcm-agent"
start_once
check_value "new package: runs the new binary again" "2.5.0" "$(ran_last)"
check_value "new package: counting starts over" 1 "$(cat "$SROOT/state/starts")"
[ ! -e "$SROOT/state/rolled-back" ] || {
	echo "FAIL new package: the old rollback record stays"
	FAILS=$((FAILS + 1))
}

# an agent that switched to the new binary by itself (no script involved yet) may
# have written its verdict before the script ever ran
start_setup prev
echo 3 >"$SROOT/state/starts"
echo not-ready >"$SROOT/state/fail-reason"
start_once
check_value "first run of the script keeps the agent's verdict" "2.3.0" "$(ran_last)"
check_value "first run of the script: reason" "reason=not-ready" "$(grep ^reason= "$SROOT/state/rolled-back")"

# whatever is wrong with the state, the agent starts
start_setup prev
echo "garbage" >"$SROOT/state/starts"
echo "garbage" >"$SROOT/state/mode"
start_once
check_value "broken state: the agent still starts" "2.4.0" "$(ran_last)"
start_setup prev
rm -rf "$SROOT/state"
: >"$SROOT/state"
start_once
check_value "unusable state directory: the agent still starts" "2.4.0" "$(ran_last)"
start_setup
echo prev >"$SROOT/state/mode"
start_once
check_value "mode prev without a previous binary: the current one runs" "2.4.0" "$(ran_last)"

if [ "$FAILS" -gt 0 ]; then
	echo "$FAILS case(s) failed"
	exit 1
fi
echo "all package script cases passed"
