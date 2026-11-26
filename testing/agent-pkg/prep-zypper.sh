#!/bin/sh
# Wie prep-dnf.sh, fuer zypper: lokales Repo, lcm-agent 2.3.0 per zypper install.
set -eu
zypper -n in createrepo_c >/dev/null 2>&1
mkdir -p /srv/repo
cp "$PKGS"/*.rpm /srv/repo/
createrepo_c -q /srv/repo
zypper -n ar -G -f file:///srv/repo techeve-lcm >/dev/null 2>&1
zypper -n in lcm-agent=2.3.0 >/dev/null
