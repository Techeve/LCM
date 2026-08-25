#!/bin/bash
# Upgrade-Test: alte Fassung mit Demo-Daten, dann Sprung auf den Pruefstand.
#
# Warum es diesen Test gibt: Am 20.08.2026 startete LCM nach dem Update auf
# 1.24.0-beta.1 nicht mehr. Die Ursache war eine Migration, die nur auf einer
# BESTEHENDEN Datenbank zuschlaegt - waehrend die komplette Testsuite gruen
# lief, weil sie ausnahmslos gegen frische Datenbanken arbeitet.
#
# Der Test laeuft NICHT in jeder Pipeline (siehe .gitlab-ci.yml): automatisch
# nur vor dem Enterprise-Zug, sonst auf Anforderung.
#
#   ALT_VERSION=1.11.0 packaging/upgrade-test/upgrade-test.sh
set -euo pipefail

ALT_VERSION="${ALT_VERSION:-1.11.0}"
ARBEIT="${ARBEIT:-$(mktemp -d)}"
PROJEKT_ID="${CI_PROJECT_ID:-1}"
BASIS="${CI_API_V4_URL:-https://gitlab.techeve.de/api/v4}"
ERWARTUNGEN="packaging/upgrade-test/erwartungen.json"

schritt() { printf '\n=== %s ===\n' "$1"; }

warte_auf_start() {
  local log=$1 sekunden=${2:-60}
  for _ in $(seq 1 "$sekunden"); do
    grep -qa "LCM service started" "$log" 2>/dev/null && return 0
    sleep 1
  done
  echo "FEHLER: Dienst kam nicht hoch. Letzte Zeilen:"; tail -20 "$log"; return 1
}

# Beendet den Dienst und wartet, bis die Datenbank wirklich geschlossen ist -
# sonst liest die Bestandsaufnahme einen halb geschriebenen WAL-Stand.
stoppe() {
  pkill -f "$1" 2>/dev/null || true
  for _ in $(seq 1 20); do pgrep -f "$1" >/dev/null || break; sleep 1; done
  sleep 1
}

schritt "Werkzeuge bauen"
go build -o "$ARBEIT/upgradecheck" ./tools/upgradecheck
go build -trimpath -o "$ARBEIT/lcm-pruefstand" ./cmd/app
echo "  upgradecheck und Pruefstand-Binary gebaut"

schritt "Alte Fassung $ALT_VERSION holen"
curl -fsSL ${CI_JOB_TOKEN:+-H "JOB-TOKEN: $CI_JOB_TOKEN"} \
     ${PRIVATE_TOKEN:+-H "PRIVATE-TOKEN: $PRIVATE_TOKEN"} \
     -o "$ARBEIT/lcm-alt" \
     "$BASIS/projects/$PROJEKT_ID/packages/generic/lcm/$ALT_VERSION/lcm-linux-amd64"
chmod +x "$ARBEIT/lcm-alt"
echo "  $("$ARBEIT/lcm-alt" -version)"

schritt "Alte Fassung mit Demo-Daten starten"
mkdir -p "$ARBEIT/daten"
"$ARBEIT/lcm-alt" -data "$ARBEIT/daten" -dev -demo > "$ARBEIT/alt.log" 2>&1 &
warte_auf_start "$ARBEIT/alt.log"
sleep 5   # Demo-Daten werden nach dem Start geschrieben
stoppe lcm-alt
"$ARBEIT/upgradecheck" erfassen -db "$ARBEIT/daten/app.db" -version "$ALT_VERSION" -out "$ARBEIT/vorher.json"

schritt "Sprung auf den Pruefstand"
"$ARBEIT/lcm-pruefstand" -data "$ARBEIT/daten" -dev > "$ARBEIT/neu.log" 2>&1 &
warte_auf_start "$ARBEIT/neu.log"
sleep 3
stoppe lcm-pruefstand

if grep -qa "level=ERROR\|constraint failed\|panic:" "$ARBEIT/neu.log"; then
  echo "FEHLER: Fehlermeldungen beim ersten Start nach dem Upgrade:"
  grep -a "level=ERROR\|constraint failed\|panic:" "$ARBEIT/neu.log" | head -5
  exit 1
fi
echo "  Dienst gestartet, keine Fehlermeldung"

schritt "Zweiter Start - Migrationen muessen wiederholbar sein"
"$ARBEIT/lcm-pruefstand" -data "$ARBEIT/daten" -dev > "$ARBEIT/neu2.log" 2>&1 &
warte_auf_start "$ARBEIT/neu2.log"
sleep 2
stoppe lcm-pruefstand
if grep -qa "level=ERROR\|constraint failed\|panic:" "$ARBEIT/neu2.log"; then
  echo "FEHLER: Der ZWEITE Start meldet Fehler - eine Migration ist nicht wiederholbar:"
  grep -a "level=ERROR\|constraint failed\|panic:" "$ARBEIT/neu2.log" | head -5
  exit 1
fi
echo "  Zweiter Start ebenfalls sauber"

schritt "Daten vergleichen"
"$ARBEIT/upgradecheck" erfassen -db "$ARBEIT/daten/app.db" -version "Pruefstand" -out "$ARBEIT/nachher.json"
"$ARBEIT/upgradecheck" vergleichen \
  -vorher "$ARBEIT/vorher.json" -nachher "$ARBEIT/nachher.json" -erwartungen "$ERWARTUNGEN"

schritt "Fertig"
echo "  Upgrade $ALT_VERSION -> Pruefstand ohne Datenverlust."
