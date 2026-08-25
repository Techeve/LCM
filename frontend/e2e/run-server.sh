#!/bin/sh
# Baut das komplette Binary (Frontend muss bereits gebaut sein - passiert
# im Makefile-Target `test-e2e`) und startet es mit Test-Konfiguration.
set -e
cd "$(dirname "$0")/../.."

# Port und Arbeitsverzeichnis sind umstellbar, damit zwei Arbeitsbaeume
# desselben Repos gleichzeitig testen koennen (siehe playwright.config.js).
E2E_PORT=${LCM_E2E_PORT:-8090}
# Der Agent-Listener braucht einen eigenen Port und verschiebt sich um
# denselben Betrag wie der UI-Port - sonst kollidiert der zweite Lauf dort,
# auch wenn die UI-Ports verschieden sind. Beim Standard bleibt es bei 9320:
# ein Test prueft genau diese Zahl in der Oberflaeche.
E2E_AGENT_PORT=$((9320 + E2E_PORT - 8090))
# Eigenes Verzeichnis JE PORT: Playwright startet pro Worker eine eigene
# Instanz, und ein gemeinsames Verzeichnis wuerde sich gegenseitig loeschen
# (rm -rf unten) und dieselbe Datenbank teilen.
E2E_DIR=frontend/e2e/.run/$E2E_PORT
rm -rf "$E2E_DIR"
mkdir -p "$E2E_DIR"

# Versionsinfos wie im Makefile injizieren (VERSION/.buildnumber).
VERSION_PKG=LCM/internal/version
go build -ldflags "-X $VERSION_PKG.Version=$(cat VERSION) -X $VERSION_PKG.Build=$(cat .buildnumber 2>/dev/null || echo 0)" \
  -o "$E2E_DIR/app" ./cmd/app

cat > "$E2E_DIR/config.json" <<EOF
{
  "host": "127.0.0.1",
  "port": $E2E_PORT,
  "agent_port": $E2E_AGENT_PORT,
  "database_path": "e2e.db",
  "jwt_secret": "e2e-test-secret-nur-fuer-lokale-tests-1234",
  "access_token_ttl_minutes": 60,
  "admin_initial_password": "e2e-admin-passwort"
}
EOF

# --dev: unverschlüsseltes HTTP für die lokalen E2E-Tests (Playwright
# spricht http://127.0.0.1:8090; produktiv läuft LCM per HTTPS).
# --demo: die Tests laufen gegen die Demo-Daten (demo_mode ist bewusst
# kein config.json-Feld mehr, nur noch ein CLI-Flag).
exec "$E2E_DIR/app" -config "$E2E_DIR/config.json" --dev --demo
