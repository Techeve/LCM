#!/bin/sh
# Wie prep-dnf.sh, fuer pacman: signiertes Repo "techeve" (SigLevel Required),
# in der Datenbank nur die neueste Version (repo-add behaelt je Paket eine), die
# aelteren Dateien samt .sig bleiben im Verzeichnis. Erstinstallation 2.3.0
# per pacman -U, wie es ein Anwender mit einer aelteren Datei taete.
set -eu
export GNUPGHOME=/root/gnupg-test
mkdir -p "$GNUPGHOME"
chmod 700 "$GNUPGHOME"
gpg --batch --quiet --passphrase "" --quick-gen-key "Techeve Test <t@example.org>" rsa2048 sign never >/dev/null 2>&1
FPR=$(gpg --list-keys --with-colons | awk -F: '/^fpr/ {print $10; exit}')
pacman -Sy --noconfirm >/dev/null 2>&1
mkdir -p /srv/repo/x86_64
cp "$PKGS"/*.pkg.tar.zst /srv/repo/x86_64/
cd /srv/repo/x86_64
for v in 2.2.1 2.3.0 2.3.1 2.4.0beta.1 2.4.0beta.2; do
	repo-add -q techeve.db.tar.zst "lcm-agent-$v-1-x86_64.pkg.tar.zst"
done
for f in *.pkg.tar.zst techeve.db.tar.zst techeve.files.tar.zst; do
	gpg --batch --yes --quiet --detach-sign --output "$f.sig" "$f"
done
ln -sf techeve.db.tar.zst techeve.db
ln -sf techeve.db.tar.zst.sig techeve.db.sig
ln -sf techeve.files.tar.zst techeve.files
ln -sf techeve.files.tar.zst.sig techeve.files.sig
gpg --export "$FPR" >/srv/techeve.gpg
pacman-key --init >/dev/null 2>&1
pacman-key --add /srv/techeve.gpg >/dev/null 2>&1
pacman-key --lsign-key "$FPR" >/dev/null 2>&1
# $arch soll pacman auswerten, nicht die Shell.
# shellcheck disable=SC2016
printf '\n[techeve]\nSigLevel = Required DatabaseRequired\nServer = file:///srv/repo/$arch\n' >>/etc/pacman.conf
pacman -Sy --noconfirm >/dev/null 2>&1
pacman -U --noconfirm file:///srv/repo/x86_64/lcm-agent-2.3.0-1-x86_64.pkg.tar.zst >/dev/null
