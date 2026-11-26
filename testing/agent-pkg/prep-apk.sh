#!/bin/sh
# Wie prep-dnf.sh, fuer apk: lokales Repo mit signiertem APKINDEX, die Pakete
# heissen wie apk sie sucht (<name>-<version>.apk). Erstinstallation wie in der
# Doku: apk add lcm-agent (der Paketskript-Hinweis nennt das Festhalten).
set -eu
apk add -q abuild >/dev/null 2>&1
mkdir -p /srv/repo/x86_64
for f in "$PKGS"/lcm-agent_*.apk; do
	v=${f##*/lcm-agent_}
	v=${v%_x86_64.apk}
	cp "$f" "/srv/repo/x86_64/lcm-agent-$v.apk"
done
cd /srv/repo/x86_64
abuild-keygen -a -n >/dev/null 2>&1
cp /root/.abuild/*.pub /etc/apk/keys/
apk index --allow-untrusted -q -o APKINDEX.tar.gz ./*.apk
abuild-sign -k /root/.abuild/*.rsa -q APKINDEX.tar.gz
echo "file:///srv/repo" >/etc/apk/repositories
apk update >/dev/null
apk add lcm-agent=2.3.0 >/dev/null
