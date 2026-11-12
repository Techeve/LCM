#!/bin/sh
# agent-scripts-test - runs the lcm-agent package scripts against stubbed
# systemctl, rc-service and rc-update, with the argument conventions of deb,
# rpm, apk and pacman, and checks which service actions they take:
#
# - stop and disable only on a real removal, never on an upgrade
# - restart after an upgrade, but never from inside an agent command
#   (that would kill the package transaction running the upgrade)
#
#   sh packaging/scripts/agent-scripts-test.sh
#
# Needs neither root nor Linux; go test runs it (internal/agent). The scripts
# read the filesystem below LCM_PKG_ROOT, a fake /proc included.
set -eu

DIR=$(cd "$(dirname "$0")" && pwd)
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
	LC_ALL=C PATH="$WORK/bin:$PATH" LCM_PKG_ROOT="$WORK/root" sh -c '
		parent=1; [ "$1" = agent ] && parent=4001
		mkdir -p "$LCM_PKG_ROOT/proc/$$"
		echo sh >"$LCM_PKG_ROOT/proc/$$/comm"
		printf "PPid:\t%s\n" "$parent" >"$LCM_PKG_ROOT/proc/$$/status"
		shift; exec sh "$@"' sh "$_caller" "$DIR/$_script" "$@" >"$WORK/out" 2>&1
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

STOP="systemctl stop lcm-agent.service"
DISABLE="systemctl disable lcm-agent.service"
RELOAD="systemctl daemon-reload"
RESTART="systemctl try-restart lcm-agent.service"

for s in agent-preremove.sh agent-postinstall.sh agent-postremove.sh; do
	sh -n "$DIR/$s"
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

if [ "$FAILS" -gt 0 ]; then
	echo "$FAILS case(s) failed"
	exit 1
fi
echo "all package script cases passed"
