#!/bin/bash
# Lädt die gebauten lcm-agent-Pakete für rpm, apk und Arch (amd64 + arm64 aus
# bin/) in den Techeve-Paketserver (RSM) und prüft danach, ob die Version im
# veröffentlichten Index steht. Der Server signiert und baut die Metadaten
# (repodata, APKINDEX, Arch-Datenbank) selbst; die CI liefert nur die rohen
# Pakete.
#
#   bash packaging/publish-pkgs.sh
#
# Upload je Datei: POST ${REPO_URL}/pkg/v1/<Format>/<Kanal> mit der Paketdatei
# als Rumpf (Format: rpm, apk, arch; Kanal: community, beta), Basic-Auth.
#   201  hochgeladen
#   409  genau diese Version liegt schon (RSM überschreibt nichts) - gilt als
#        Erfolg, ein wiederholter Lauf ist also unschädlich
#   sonst Fehler, mit der Antwort des Servers
#
# Required environment/CI variables:
#   REPO_URL   Basis-URL des Upload-Listeners, z. B. https://repo.techeve.de
#   REPO_USER  Basic-Auth-Benutzer (Recht: Publish-Eintrag <Format>/<Kanal>)
#   REPO_PASS  Basic-Auth-Passwort (masked)
#   CHANNEL    community oder beta (die CI setzt es je Branch)
# Optional:
#   REPO_PUBLIC_URL  öffentliche Adresse für die Gegenprobe
#                    (Default: https://repo.techeve.de)
#   PKG_DIR          Verzeichnis der Pakete (Default: bin)
#   CHECK_TRIES      Anläufe der Gegenprobe (Default: 5)
#   CHECK_DELAY      Sekunden zwischen den Anläufen (Default: 3)
#
# Die Gegenprobe liest den veröffentlichten Index je Format und Architektur und
# sucht die soeben hochgeladene Version darin:
#   rpm   repodata/repomd.xml -> primary.xml(.gz/.zst/.xz): <version ver="...">
#   apk   APKINDEX.tar.gz: Eintrag mit P:lcm-agent und V:<Version>
#   arch  techeve.db: Eintrag lcm-agent mit %VERSION% <Version>-<Revision>
#
# Die Versionen schreiben die Formate verschieden (siehe pkg-version.sh):
# 2.4.0-beta.1 heißt in rpm 2.4.0~beta.1, in apk 2.4.0_beta1, in Arch 2.4.0beta.1.
# Die Gegenprobe liest sie aus dem Dateinamen des Pakets, das sie hochgeladen hat.
set -euo pipefail

: "${REPO_URL:?REPO_URL erforderlich}"
: "${REPO_USER:?REPO_USER erforderlich}"
: "${REPO_PASS:?REPO_PASS erforderlich}"
: "${CHANNEL:?CHANNEL erforderlich (community oder beta)}"
REPO_PUBLIC_URL="${REPO_PUBLIC_URL:-https://repo.techeve.de}"
PKG_DIR="${PKG_DIR:-bin}"
CHECK_TRIES="${CHECK_TRIES:-5}"
CHECK_DELAY="${CHECK_DELAY:-3}"

case "$CHANNEL" in
  community | beta) ;;
  *)
    echo "FEHLER: Kanal '${CHANNEL}' ist nicht community oder beta - die Pakete gehören nur dorthin."
    exit 1
    ;;
esac

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
# Zugangsdaten über eine Konfigurationsdatei, nicht über die Befehlszeile.
CURL_AUTH="$WORK/curl-auth"
# curl_quote maskiert Backslash und Anführungszeichen für den Wert in
# Anführungszeichen (sonst bricht ein Passwort damit die Konfiguration oder
# schneidet den Rest ab).
curl_quote() {
  printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g'
}
(umask 077 && printf 'user = "%s:%s"\n' "$(curl_quote "$REPO_USER")" "$(curl_quote "$REPO_PASS")" >"$CURL_AUTH")

# parse_name <Datei>: setzt FORMAT, VERSION (wie das Paket sie schreibt) und
# ARCH aus dem Dateinamen, den nfpm vergibt:
#   lcm-agent-2.3.0-1.x86_64.rpm  lcm-agent_2.3.0_x86_64.apk
#   lcm-agent-2.3.0-1-x86_64.pkg.tar.zst
parse_name() {
  local name
  name="$(basename "$1")"
  case "$name" in
    lcm-agent-*.rpm)
      FORMAT=rpm
      name="${name%.rpm}"
      ARCH="${name##*.}"
      name="${name%.*}"        # lcm-agent-2.3.0-1
      name="${name#lcm-agent-}" # 2.3.0-1
      VERSION="${name%-*}"
      ;;
    lcm-agent_*.apk)
      FORMAT=apk
      name="${name%.apk}"
      name="${name#lcm-agent_}" # 2.3.0_x86_64 - die Architektur hat selbst einen Unterstrich
      case "$name" in
        *_x86_64) ARCH=x86_64 ;;
        *) ARCH="${name##*_}" ;;
      esac
      VERSION="${name%_"$ARCH"}"
      ;;
    lcm-agent-*.pkg.tar.zst)
      FORMAT=arch
      name="${name%.pkg.tar.zst}"
      name="${name#lcm-agent-}" # 2.3.0-1-x86_64
      ARCH="${name##*-}"
      name="${name%-*}"         # 2.3.0-1
      VERSION="${name%-*}"
      REVISION="${name##*-}"
      ;;
    *)
      echo "FEHLER: Paketname '${name}' nicht erkannt"
      return 1
      ;;
  esac
}

# upload <Datei> <Format>: ein Paket hochladen, 201 und 409 gelten als Erfolg.
upload() {
  local file="$1" format="$2" code
  code="$(curl -sS --retry 3 --retry-delay 2 -K "$CURL_AUTH" -o "$WORK/response" -w '%{http_code}' \
    -X POST -H 'Content-Type: application/octet-stream' --data-binary "@${file}" \
    "${REPO_URL}/pkg/v1/${format}/${CHANNEL}")"
  case "$code" in
    201) echo "   - $(basename "$file"): hochgeladen" ;;
    409) echo "   - $(basename "$file"): liegt schon (409), nichts zu tun" ;;
    *)
      echo "FEHLER: Upload von $(basename "$file") ergab HTTP ${code}:"
      cat "$WORK/response" || true
      echo
      return 1
      ;;
  esac
}

# fetch <URL> <Datei>: den öffentlichen Index laden, ohne Zugangsdaten.
fetch() {
  curl -fsS --retry 3 --retry-delay 2 -o "$2" "$1"
}

# unpack <Datei>: den Inhalt auf die Standardausgabe, gleich ob gzip, zstd oder xz.
unpack() {
  case "$(head -c 4 "$1" | od -An -tx1 | tr -d ' \n')" in
    1f8b*) gzip -dc "$1" ;;
    28b52ffd) zstd -dc "$1" ;;
    fd377a58) xz -dc "$1" ;;
    *) cat "$1" ;;
  esac
}

# check_rpm <Version> <Arch>: Steht die Version in primary.xml des Kanals?
check_rpm() {
  local base="${REPO_PUBLIC_URL}/rpm/${CHANNEL}/$2" href want="${1//./\\.}"
  fetch "${base}/repodata/repomd.xml" "$WORK/repomd.xml" || return 1
  href="$(tr '\n' ' ' <"$WORK/repomd.xml" | grep -Eo '<data type="primary">.*</data>' | grep -Eo 'href="[^"]*"' | head -n 1 | cut -d'"' -f2)"
  [ -n "$href" ] || return 1
  fetch "${base}/${href}" "$WORK/primary" || return 1
  unpack "$WORK/primary" | tr '\n' ' ' |
    grep -Eq "<name>lcm-agent</name> *<arch>$2</arch> *<version epoch=\"[0-9]+\" ver=\"${want}\""
}

# check_apk <Version> <Arch>: Steht P:lcm-agent mit V:<Version> im APKINDEX? Eine
# Revision (-r0) hinter der Version zählt nicht mit.
check_apk() {
  fetch "${REPO_PUBLIC_URL}/apk/${CHANNEL}/$2/APKINDEX.tar.gz" "$WORK/APKINDEX.tar.gz" || return 1
  tar -xzOf "$WORK/APKINDEX.tar.gz" APKINDEX 2>/dev/null |
    awk -v want="$1" '
      function check(   v) { v = ver; sub(/-r[0-9]+$/, "", v); if (name == "lcm-agent" && v == want) found = 1 }
      /^$/ { check(); name = ""; ver = ""; next }
      /^P:/ { name = substr($0, 3) }
      /^V:/ { ver = substr($0, 3) }
      END { check(); exit !found }
    '
}

# check_arch <Version> <Revision> <Arch>: Steht die Version in der Datenbank?
# repo-add legt je Paket ein Verzeichnis lcm-agent-<Version>-<Revision>/ mit der
# Datei desc an; %VERSION% steht dort samt Revision.
check_arch() {
  local want="${1//./\\.}-$2" d
  fetch "${REPO_PUBLIC_URL}/arch/${CHANNEL}/$3/techeve.db" "$WORK/techeve.db" || return 1
  rm -rf "$WORK/db"
  mkdir -p "$WORK/db"
  tar -xf "$WORK/techeve.db" -C "$WORK/db" 2>/dev/null || return 1
  for d in "$WORK"/db/*/desc; do
    tr '\n' ' ' <"$d" | grep -Eq "%NAME% +lcm-agent +.*%VERSION% +${want} " && return 0
  done
  return 1
}

# verify <Format> <Version> <Revision> <Arch>: bis zu CHECK_TRIES Anläufe, denn der
# Server baut die Metadaten nach dem Upload.
verify() {
  local format="$1" version="$2" revision="$3" arch="$4" i
  for ((i = 1; i <= CHECK_TRIES; i++)); do
    case "$format" in
      rpm) check_rpm "$version" "$arch" && return 0 ;;
      apk) check_apk "$version" "$arch" && return 0 ;;
      arch) check_arch "$version" "$revision" "$arch" && return 0 ;;
    esac
    [ "$i" -lt "$CHECK_TRIES" ] && sleep "$CHECK_DELAY"
  done
  return 1
}

shopt -s nullglob
FILES=("$PKG_DIR"/lcm-agent-*.rpm "$PKG_DIR"/lcm-agent_*.apk "$PKG_DIR"/lcm-agent-*.pkg.tar.zst)
[ "${#FILES[@]}" -gt 0 ] || { echo "Keine rpm-, apk- oder Arch-Pakete in ${PKG_DIR}/ gefunden"; exit 1; }

echo ">> 1/2 ${#FILES[@]} Pakete in den Kanal '${CHANNEL}' hochladen"
CHECKS=()
for f in "${FILES[@]}"; do
  FORMAT="" VERSION="" ARCH="" REVISION=1
  parse_name "$f"
  upload "$f" "$FORMAT"
  CHECKS+=("${FORMAT}|${VERSION}|${REVISION}|${ARCH}")
done

echo ">> 2/2 Gegenprobe im veröffentlichten Index (${REPO_PUBLIC_URL})"
FAILED=0
for c in "${CHECKS[@]}"; do
  IFS='|' read -r format version revision arch <<<"$c"
  if verify "$format" "$version" "$revision" "$arch"; then
    echo "   - ${format}/${CHANNEL}/${arch}: lcm-agent ${version} bestätigt"
  else
    echo "FEHLER: lcm-agent ${version} steht NICHT im veröffentlichten Index von ${format}/${CHANNEL}/${arch}."
    echo "  Die Datei ist hochgeladen, wird aber nicht ausgeliefert - der Server hat die Metadaten nicht erneuert."
    FAILED=1
  fi
done
[ "$FAILED" -eq 0 ] || exit 1

echo ">> Done - die Agent-Pakete liegen im Kanal ${CHANNEL} von ${REPO_PUBLIC_URL}."
