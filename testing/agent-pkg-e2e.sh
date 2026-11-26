#!/bin/sh
# agent-pkg-e2e - prueft die Agent-Skripte von LCM (Versionen lesen, gezielt
# installieren, halten) gegen die echten Paketverwaltungen in Containern:
# dnf4 (AlmaLinux 9 und 10), dnf5 (Fedora), zypper (Leap, Tumbleweed), apk
# (Alpine 3.22 mit apk-tools 2, 3.24 mit apk-tools 3) und pacman (Arch).
#
#   sh testing/agent-pkg-e2e.sh [Filter]     z. B. "zypper" oder "pacman"
#
# Braucht Docker und Netz (Paketquellen der Distributionen). Es baut mit nfpm
# lcm-agent-Pakete 2.2.1, 2.3.0, 2.3.1 und 2.4.0-beta.1/2 (mit einem
# Platzhalter-Binary, aber den echten Paketskripten) und legt sie je Container
# in ein lokales Repository (testing/agent-pkg/prep-*.sh). Geprueft wird dann
# mit den Skripten, die LCM auf dem Server ausfuehrt (internal/core/services,
# TestAgentPkgDocker). LCM_AGENT_PKG_RECORD=<Verzeichnis> schreibt die rohen
# Ausgaben fuer die Fixtures in internal/core/services/testdata/agent-pkg.
#
# Nicht Teil der CI: braucht Docker, Netz und mehrere Minuten.
set -eu

ROOT=$(cd "$(dirname "$0")/.." && pwd)
NFPM_VERSION=${NFPM_VERSION:-v2.48.0}
WORK=${LCM_AGENT_PKG_WORK:-$(mktemp -d)}
PKGS="$WORK/pkgs"
mkdir -p "$PKGS" "$WORK/stage"

# Platzhalter-Binary und eine nfpm-Konfiguration, die darauf zeigt.
printf '#!/bin/sh\necho "lcm-agent test"\n' >"$WORK/stage/lcm-agent"
chmod 755 "$WORK/stage/lcm-agent"
sed "s#./bin/lcm-agent-pkg#$WORK/stage/lcm-agent#" "$ROOT/packaging/nfpm-agent.yaml" >"$WORK/nfpm-agent.yaml"

cd "$ROOT"
for v in 2.2.1 2.3.0 2.3.1 2.4.0-beta.1 2.4.0-beta.2; do
	for p in rpm apk archlinux; do
		LCM_ARCH=amd64 LCM_VERSION=$(sh packaging/pkg-version.sh "$p" "$v") \
			go run "github.com/goreleaser/nfpm/v2/cmd/nfpm@$NFPM_VERSION" package \
			--config "$WORK/nfpm-agent.yaml" --packager "$p" --target "$PKGS/" >/dev/null
	done
done

LCM_AGENT_PKG_DOCKER=1 LCM_AGENT_PKG_DIR="$PKGS" LCM_AGENT_PKG_ONLY="${1:-}" \
	go test ./internal/core/services -run TestAgentPkgDocker -count=1 -timeout 60m -v
