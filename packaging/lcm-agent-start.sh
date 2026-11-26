#!/bin/sh
# lcm-agent-start - entry point of the lcm-agent service (systemd ExecStart,
# OpenRC command). It starts the agent and keeps the way back open: if a new
# binary does not come up, the previous one runs instead.
#
# Why a script: a broken new binary cannot roll itself back, it may not even
# start. The package manager has replaced /usr/bin/lcm-agent, so the previous
# binary lives in /var/lib/lcm-agent/update/lcm-agent.prev (copied by the
# preinst script of the new package and by every healthy agent).
#
# State below /var/lib/lcm-agent/update (plain files, also written by the agent):
#   bin-id        identity (inode:size:mtime) of /usr/bin/lcm-agent the state belongs to
#   starts        starts in a row that did not reach "healthy" (the agent writes 0
#                 once it was ready and stable; after a failed health check, 3)
#   mode          normal | prev - which binary runs
#   cycles        how often both binaries failed one after the other (for the backoff)
#   fail-reason   set by the agent when it gave up itself (not-ready)
#   rolled-back   from= to= reason= at= - present while the previous binary runs
#
# Rules:
#   - A different /usr/bin/lcm-agent (new package) starts over: normal mode, no history
#     (the very first run of this script keeps what the agent wrote before).
#   - More than 3 unhealthy starts in a row in normal mode: previous binary
#     (if there is one and it differs from the current one).
#   - The same for the previous binary: back to the current one, with a wait
#     that grows to 2 minutes. The service is never given up.
#   - Anything that goes wrong with the state starts /usr/bin/lcm-agent plainly.
#
# Plain POSIX sh (dash, busybox ash); no set -e - every failure falls through to
# the start. LCM_AGENT_BIN and LCM_AGENT_STATE are for packaging/scripts/agent-scripts-test.sh only.

BIN="${LCM_AGENT_BIN:-/usr/bin/lcm-agent}"
STATE="${LCM_AGENT_STATE:-/var/lib/lcm-agent/update}"
PREV="$STATE/lcm-agent.prev"
MAX_STARTS=3
MAX_WAIT=120
WAIT_STEP=15

# say writes to the journal / log of the service.
say() { echo "lcm-agent-start: $*" >&2; }

# num prints the number in a state file, 0 if there is none.
num() {
	_n=$(cat "$STATE/$1" 2>/dev/null) || _n=0
	case "$_n" in '' | *[!0-9]*) _n=0 ;; esac
	echo "$_n"
}

# put writes a state file (atomically).
put() {
	printf '%s\n' "$2" >"$STATE/$1.tmp" 2>/dev/null && mv -f "$STATE/$1.tmp" "$STATE/$1" 2>/dev/null
}

# version_of asks a binary for its version ("lcm-agent 2.4.0" -> 2.4.0); the
# binary may be the broken one, so with a timeout - or not at all.
version_of() {
	command -v timeout >/dev/null 2>&1 || {
		echo unknown
		return
	}
	_v=$(timeout 5 "$1" version 2>/dev/null | head -n 1 | awk '{ print $2 }')
	echo "${_v:-unknown}"
}

# package_version is the installed version of the lcm-agent package - the
# fallback when the new binary cannot even name its own version.
package_version() {
	_v=$(dpkg-query -W -f='${Version}' lcm-agent 2>/dev/null) ||
		_v=$(rpm -q --qf '%{VERSION}' lcm-agent 2>/dev/null) ||
		_v=""
	echo "${_v:-unknown}"
}

# prev_usable: there is a previous binary and it is not the current one.
prev_usable() {
	[ -x "$PREV" ] && ! cmp -s "$PREV" "$BIN" 2>/dev/null
}

# wait_for sleeps n seconds (at most MAX_WAIT) before the next try.
wait_for() {
	_s=$1
	[ "$_s" -gt "$MAX_WAIT" ] && _s=$MAX_WAIT
	[ "$_s" -gt 0 ] || return 0
	say "waiting $_s s before the next try"
	sleep "$_s"
}

# roll_back switches to the previous binary and leaves the record the agent
# reports to LCM.
roll_back() {
	_reason=$(cat "$STATE/fail-reason" 2>/dev/null)
	[ -n "$_reason" ] || _reason=crash-loop
	_from=$(version_of "$BIN")
	[ "$_from" = unknown ] && _from=$(package_version)
	_to=$(version_of "$PREV")
	{
		echo "from=$_from"
		echo "to=$_to"
		echo "reason=$_reason"
		echo "at=$(date +%s)"
	} >"$STATE/rolled-back.tmp" 2>/dev/null && mv -f "$STATE/rolled-back.tmp" "$STATE/rolled-back" 2>/dev/null
	rm -f "$STATE/fail-reason"
	say "new binary ($_from) failed $MAX_STARTS starts in a row ($_reason) - starting the previous one ($_to)"
}

# guard decides which binary runs and counts this start. CHOSEN is the result.
CHOSEN="$BIN"
guard() {
	mkdir -p "$STATE" 2>/dev/null || return 0
	chmod 700 "$STATE" 2>/dev/null
	# GNU and busybox stat take -c; BSD (the tests on macOS) takes -f.
	_id=$(stat -c '%i:%s:%Y' "$BIN" 2>/dev/null || stat -f '%i:%z:%m' "$BIN" 2>/dev/null) || _id=unknown
	_known=$(cat "$STATE/bin-id" 2>/dev/null)
	if [ -n "$_known" ] && [ "$_id" != "$_known" ]; then
		rm -f "$STATE/starts" "$STATE/cycles" "$STATE/fail-reason" "$STATE/rolled-back" "$STATE/mode"
	fi
	# First run on this state (an agent switched to the new binary by itself, without
	# this script, and may already have written its verdict): adopt it.
	[ "$_id" = "$_known" ] || put bin-id "$_id"
	_mode=$(cat "$STATE/mode" 2>/dev/null)
	[ "$_mode" = prev ] || _mode=normal
	_starts=$(($(num starts) + 1))
	_cycles=$(num cycles)

	if [ "$_starts" -gt "$MAX_STARTS" ]; then
		if [ "$_mode" = normal ] && prev_usable; then
			roll_back
			_mode=prev
			_starts=1
			[ "$_cycles" -gt 0 ] && wait_for $((_cycles * WAIT_STEP))
		elif [ "$_mode" = prev ]; then
			_cycles=$((_cycles + 1))
			put cycles "$_cycles"
			say "the previous binary failed $MAX_STARTS starts in a row as well - trying the current one again"
			_mode=normal
			_starts=1
			wait_for $((_cycles * WAIT_STEP))
		else
			# Nothing to go back to: keep trying, ever more slowly.
			[ "$_starts" -gt 1000 ] && _starts=$((MAX_STARTS + 1))
			wait_for $(((_starts - MAX_STARTS) * WAIT_STEP))
		fi
	fi
	put starts "$_starts"
	put mode "$_mode"
	if [ "$_mode" = prev ] && [ -x "$PREV" ]; then
		CHOSEN="$PREV"
	fi
}

guard
[ -x "$CHOSEN" ] || CHOSEN="$BIN"
LCM_AGENT_GUARD=1
export LCM_AGENT_GUARD
exec "$CHOSEN" "$@"
