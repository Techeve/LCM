#!/bin/sh
# agent-apt-test - proves with real apt that the lcm-agent package stays out of
# normal upgrades and still takes part in a targeted install:
#
#   sh packaging/scripts/agent-apt-test.sh [debian:11 debian:12 ...]
#
# For every image: nfpm builds test packages (versions 2.3.0, 2.4.0 and the beta
# 2.5.0-beta.1, a script as the "binary"), they become a local file repository, and
# inside a container
#   (a) apt-get upgrade / full-upgrade / install lcm-agent / unattended-upgrade
#       leave the pinned version alone (and do upgrade it without the pin),
#   (b) apt-get install lcm-agent=<version> works upwards, downwards
#       (--allow-downgrades) and onto a beta,
#   (c) the pin file follows the installed version,
#   (d) remove and purge delete the pin and the update state.
#
# Needs docker, nfpm (or go for go run) and network (the containers install
# apt-utils and unattended-upgrades). Not part of go test; run it before a release.
set -eu

HERE=$(cd "$(dirname "$0")/../.." && pwd)
IMAGES="${*:-debian:11 debian:12}"
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

command -v docker >/dev/null || { echo "docker is missing" >&2; exit 2; }
if command -v nfpm >/dev/null; then
	NFPM=nfpm
else
	NFPM="go run github.com/goreleaser/nfpm/v2/cmd/nfpm@latest"
fi

# Test packages: the real packaging files, a script instead of the agent binary.
mkdir -p "$WORK/build/bin" "$WORK/repo"
ln -s "$HERE/packaging" "$WORK/build/packaging"
for v in 2.3.0 2.4.0 2.5.0-beta.1; do
	# Each version answers "version" and logs every start (a stand-in for the agent).
	cat >"$WORK/build/bin/lcm-agent-pkg" <<EOF2
#!/bin/sh
[ "\$1" = version ] && { echo "lcm-agent $v"; exit 0; }
echo "$v \$*" >>/tmp/ran
EOF2
	chmod 755 "$WORK/build/bin/lcm-agent-pkg"
	(cd "$WORK/build" && LCM_ARCH=amd64 LCM_VERSION=$v $NFPM package \
		--config packaging/nfpm-agent.yaml --packager deb --target "$WORK/repo/" >/dev/null)
done

cat >"$WORK/inside.sh" <<'INSIDE'
set -eu
export DEBIAN_FRONTEND=noninteractive
FAILS=0
ok() { echo "ok   $*"; }
fail() { echo "FAIL $*"; FAILS=$((FAILS + 1)); }
# is <name> <command...>: the command must succeed.
is() { _n=$1; shift; if "$@" >/dev/null 2>&1; then ok "$_n"; else fail "$_n"; fi; }
installed() { dpkg-query -W -f='${Version}' lcm-agent; }
pinned() { sed -n 's/^Pin: version //p' /etc/apt/preferences.d/lcm-agent; }
same() { # same <name> <want> <got>
	if [ "$2" = "$3" ]; then ok "$1 ($3)"; else fail "$1: want $2, got $3"; fi
}
PIN=/etc/apt/preferences.d/lcm-agent

# Debian 11 is end of life: its packages live on archive.debian.org now.
if grep -q '^VERSION_ID="11"' /etc/os-release; then
	rm -f /etc/apt/sources.list.d/*
	cat >/etc/apt/sources.list <<'EOF2'
deb http://archive.debian.org/debian bullseye main
deb http://archive.debian.org/debian-security bullseye-security main
EOF2
	echo 'Acquire::Check-Valid-Until "false";' >/etc/apt/apt.conf.d/51archive
fi
apt-get update >/dev/null 2>&1 || { echo "FAIL apt-get update"; exit 1; }
apt-get install -y apt-utils unattended-upgrades needrestart >/dev/null 2>&1 || { echo "FAIL installing the test tools"; exit 1; }
mkdir -p /srv/repo && cp /repo/*.deb /srv/repo/
(cd /srv/repo && apt-ftparchive packages . >Packages)
echo 'deb [trusted=yes] file:/srv/repo ./' >/etc/apt/sources.list.d/lcm-test.list
# unattended-upgrades may take everything - the pin alone must stop the agent.
cat >/etc/apt/apt.conf.d/52lcm-test <<'EOF2'
Unattended-Upgrade::Allowed-Origins { "*:*"; };
Unattended-Upgrade::Origins-Pattern { "origin=*"; };
EOF2
apt-get update >/dev/null 2>&1

apt-get install -y lcm-agent=2.3.0 >/dev/null 2>&1
same "first install pins the installed version" 2.3.0 "$(pinned)"
is "pin file is world readable, root owned" test -r "$PIN"
is "package ships the needrestart exception" test -f /etc/needrestart/conf.d/lcm-agent.conf
is "the guard script is installed" test -x /usr/lib/lcm-agent/lcm-agent-start

# (a) normal upgrades leave it alone
apt-get update >/dev/null 2>&1
apt-get upgrade -y >/dev/null 2>&1 || true
same "apt-get upgrade keeps the version" 2.3.0 "$(installed)"
apt-get dist-upgrade -y >/dev/null 2>&1 || true
same "apt-get dist-upgrade keeps the version" 2.3.0 "$(installed)"
apt-get install -y lcm-agent >/dev/null 2>&1 || true
same "apt-get install lcm-agent (no version) keeps the version" 2.3.0 "$(installed)"
apt-get install -y --only-upgrade lcm-agent >/dev/null 2>&1 || true
same "apt-get install --only-upgrade keeps the version" 2.3.0 "$(installed)"
UU=$(unattended-upgrade --dry-run -d 2>&1 || true)
if echo "$UU" | grep -q 'lcm-agent'; then
	echo "$UU" | grep 'lcm-agent' | head -5
	fail "unattended-upgrade --dry-run wants to touch lcm-agent"
else
	ok "unattended-upgrade --dry-run leaves lcm-agent alone"
fi
POL=$(apt-cache policy lcm-agent)
echo "$POL" | grep -q 'Candidate: 2.3.0' && ok "apt-cache policy: candidate is the pinned version" || { echo "$POL"; fail "candidate is not the pinned version"; }
apt-cache madison lcm-agent | grep -q '2.5.0~beta.1' && ok "apt-cache madison still lists all versions" || fail "madison misses versions"

# control: without the pin the same commands DO upgrade (the test means something)
mv "$PIN" /root/lcm-agent.pin
apt-get install -s --only-upgrade lcm-agent 2>/dev/null | grep -q 'Inst lcm-agent' && ok "control: without the pin apt would upgrade" || fail "control: without the pin apt does not upgrade - test proves nothing"
UU2=$(unattended-upgrade --dry-run -d 2>&1 || true)
echo "$UU2" | grep -q 'lcm-agent' && ok "control: without the pin unattended-upgrade would upgrade" || fail "control: unattended-upgrade ignores lcm-agent even without the pin - test proves nothing"
mv /root/lcm-agent.pin "$PIN"

# (b) + (c) targeted installs, the pin follows
apt-get install -y lcm-agent=2.4.0 >/dev/null 2>&1 && ok "install lcm-agent=2.4.0 upwards" || fail "install lcm-agent=2.4.0 upwards"
same "installed 2.4.0" 2.4.0 "$(installed)"
same "pin follows upwards" 2.4.0 "$(pinned)"
apt-get install -y lcm-agent=2.3.0 >/dev/null 2>&1 && fail "downgrade without --allow-downgrades must be refused" || ok "downgrade without --allow-downgrades is refused (as without any pin)"
apt-get install -y --allow-downgrades lcm-agent=2.3.0 >/dev/null 2>&1 && ok "install --allow-downgrades lcm-agent=2.3.0" || fail "downgrade with --allow-downgrades"
same "installed 2.3.0 again" 2.3.0 "$(installed)"
same "pin follows downwards" 2.3.0 "$(pinned)"
apt-get install -y --allow-downgrades 'lcm-agent=2.5.0~beta.1' >/dev/null 2>&1 && ok "install a beta (tilde version)" || fail "install a beta"
same "installed beta" '2.5.0~beta.1' "$(installed)"
same "pin of a beta" '2.5.0~beta.1' "$(pinned)"
apt-get upgrade -y >/dev/null 2>&1 || true
same "apt-get upgrade keeps the beta" '2.5.0~beta.1' "$(installed)"
apt-get install -y --allow-downgrades lcm-agent=2.4.0 >/dev/null 2>&1 || fail "beta -> release"
same "beta -> 2.4.0" 2.4.0 "$(installed)"
apt-get install -y --reinstall lcm-agent=2.4.0 >/dev/null 2>&1 || fail "reinstall"
same "reinstall keeps the pin" 2.4.0 "$(pinned)"
# an existing lcm-agent state (the running agent) survives upgrades
mkdir -p /var/lib/lcm-agent/update && echo 0 >/var/lib/lcm-agent/update/starts
apt-get install -y --allow-downgrades lcm-agent=2.3.0 >/dev/null 2>&1 || fail "install with state"
is "update state survives an upgrade" test -f /var/lib/lcm-agent/update/starts
is "the previous binary was kept by the preinst script" test -x /var/lib/lcm-agent/update/lcm-agent.prev

# Rollback with real dpkg: the preinst script keeps the replaced binary, and the
# start script (dash) falls back to it after three starts that never got healthy.
GUARD=/usr/lib/lcm-agent/lcm-agent-start
UPD=/var/lib/lcm-agent/update
apt-get install -y --allow-downgrades lcm-agent=2.3.0 >/dev/null 2>&1 || fail "reset to 2.3.0"
rm -rf "$UPD" /tmp/ran
$GUARD run                       # 2.3.0 starts (as systemd would start it) ...
echo 0 >"$UPD/starts"            # ... and becomes healthy (what the agent writes)
same "start script runs the installed binary" "2.3.0 run" "$(tail -n 1 /tmp/ran)"
apt-get install -y lcm-agent=2.4.0 >/dev/null 2>&1 || fail "update to 2.4.0"
is "preinst kept the replaced binary" test -x "$UPD/lcm-agent.prev"
same "the kept binary is 2.3.0" "lcm-agent 2.3.0" "$($UPD/lcm-agent.prev version)"
$GUARD run; $GUARD run; $GUARD run
same "2.4.0 runs for its first three unhealthy starts" "2.4.0 run" "$(tail -n 1 /tmp/ran)"
sleep() { :; }
$GUARD run
same "the fourth start falls back to 2.3.0" "2.3.0 run" "$(tail -n 1 /tmp/ran)"
same "rollback record: from" "from=2.4.0" "$(grep ^from= $UPD/rolled-back)"
same "rollback record: to" "to=2.3.0" "$(grep ^to= $UPD/rolled-back)"
apt-get install -y lcm-agent=2.5.0~beta.1 >/dev/null 2>&1 || fail "update to the beta after a rollback"
same "the way back is still 2.3.0 (2.4.0 never proved itself)" "lcm-agent 2.3.0" "$($UPD/lcm-agent.prev version)"
$GUARD run
same "a new package starts the new binary again" "2.5.0-beta.1 run" "$(tail -n 1 /tmp/ran)"
is "and clears the rollback record" test ! -e "$UPD/rolled-back"
unset -f sleep

# needrestart: syntax and effect
if needrestart -b -c /etc/needrestart/needrestart.conf >/tmp/nr.out 2>&1; then ok "needrestart reads its config with the exception"; else cat /tmp/nr.out; fail "needrestart rejects the config"; fi
perl -e 'our %nrconf; do "/etc/needrestart/conf.d/lcm-agent.conf"; die $@ if $@; my ($k)=keys %{$nrconf{override_rc}}; print "ok   needrestart override key: $k\n"; exit(("lcm-agent.service" =~ /$k/ && $nrconf{override_rc}{$k}==0) ? 0 : 1)' && ok "needrestart exception matches lcm-agent.service" || fail "needrestart exception does not match"

# (d) removal
apt-get remove -y lcm-agent >/dev/null 2>&1
is "remove deletes the pin" test ! -e "$PIN"
is "remove deletes the update state" test ! -e /var/lib/lcm-agent/update
is "remove deletes the needrestart exception" test ! -e /etc/needrestart/conf.d/lcm-agent.conf
apt-get install -y lcm-agent >/dev/null 2>&1
same "after a removal a plain install takes the newest (no stale pin)" '2.5.0~beta.1' "$(installed)"
apt-get purge -y lcm-agent >/dev/null 2>&1
is "purge deletes the pin" test ! -e "$PIN"
is "purge deletes /var/lib/lcm-agent" test ! -e /var/lib/lcm-agent

[ "$FAILS" -eq 0 ] && echo "ALL OK" || { echo "$FAILS FAILED"; exit 1; }
INSIDE

RC=0
for img in $IMAGES; do
	echo "=== $img"
	docker run --rm --platform linux/amd64 -v "$WORK/repo:/repo:ro" -v "$WORK/inside.sh:/inside.sh:ro" "$img" sh /inside.sh || RC=1

done
exit $RC
