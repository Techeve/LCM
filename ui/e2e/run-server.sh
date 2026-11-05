#!/bin/sh
# Baut das Go-Binary und startet es mit Test-Konfiguration. Die Oberfläche
# muss bereits gebaut sein (`cd ui && npx vite build && touch dist/.gitkeep`):
# sie wird per go:embed (ui/embed.go) ins Binary gelegt.
set -e
cd "$(dirname "$0")/.."
REPO_ROOT=$(cd .. && pwd)

if [ ! -f dist/index.html ]; then
  echo "ui/dist fehlt - erst 'npx vite build' ausführen." >&2
  exit 1
fi

# Port und Arbeitsverzeichnis sind umstellbar, damit zwei Arbeitsbäume
# desselben Repos gleichzeitig testen können (siehe playwright.config.ts).
E2E_PORT=${LCM_E2E_PORT:-8090}
# Der Agent-Listener braucht einen eigenen Port und verschiebt sich um
# denselben Betrag wie der UI-Port, sonst kollidiert der zweite Lauf dort.
# Beim Standard bleibt es bei 9320: ein Test prüft genau diese Zahl.
E2E_AGENT_PORT=$((9320 + E2E_PORT - 8090))
# Eigenes Verzeichnis JE PORT: Playwright startet pro Worker eine eigene
# Instanz, und ein gemeinsames Verzeichnis würde sich gegenseitig löschen
# (rm -rf unten) und dieselbe Datenbank teilen.
E2E_DIR="$PWD/e2e/.run/$E2E_PORT"
rm -rf "$E2E_DIR"
mkdir -p "$E2E_DIR"

# Versionsinfos wie im Makefile injizieren (VERSION/.buildnumber).
VERSION_PKG=LCM/internal/version
(cd "$REPO_ROOT" && go build \
  -ldflags "-X $VERSION_PKG.Version=$(cat VERSION) -X $VERSION_PKG.Build=$(cat .buildnumber 2>/dev/null || echo 0)" \
  -o "$E2E_DIR/app" ./cmd/app)

cat > "$E2E_DIR/config.json" <<CONFIG
{
  "host": "127.0.0.1",
  "port": $E2E_PORT,
  "agent_port": $E2E_AGENT_PORT,
  "database_path": "e2e.db",
  "access_token_ttl_minutes": 60,
  "trust_proxy_header": true,
  "trusted_proxies": ["127.0.0.1"],
  "admin_initial_password": "e2e-admin-passwort"
}
CONFIG

# --dev: unverschlüsseltes HTTP für die lokalen E2E-Tests (Playwright spricht
# http://127.0.0.1:8090; produktiv läuft LCM per HTTPS).
# --demo: die Tests laufen gegen die Demo-Daten (demo_mode ist bewusst kein
# config.json-Feld, nur ein CLI-Flag).
# PATH ohne lokale Werkzeuge: Die CI hat kein Trivy. Liegt es auf dem Rechner des
# Entwicklers im Pfad, zeigt die Oberfläche anderes, und Fehler fallen erst oben auf.
cd "$E2E_DIR"
exec env PATH=/usr/bin:/bin ./app -config config.json --dev --demo
