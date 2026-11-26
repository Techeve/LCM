#!/bin/sh
# Baut im Container ein lokales rpm-Repository aus $PKGS und installiert lcm-agent
# 2.3.0 so, wie es ein Anwender nach setup.sh tut. Die Repo-Datei hat wie bei
# setup.sh exclude=lcm-agent; ausgenommen davon ist nur die Erstinstallation.
set -eu
dnf -y -q install createrepo_c >/dev/null 2>&1
mkdir -p /srv/repo
cp "$PKGS"/*.rpm /srv/repo/
createrepo_c -q /srv/repo
cat >/etc/yum.repos.d/techeve-lcm.repo <<REPO
[techeve-lcm]
name=Techeve LCM
baseurl=file:///srv/repo
enabled=1
gpgcheck=0
exclude=lcm-agent
REPO
dnf -y --setopt=disable_excludes=techeve-lcm install lcm-agent-2.3.0 >/dev/null
