#!/bin/sh
# Prüft die Trivy-Anleitung aus docs/getting-started/installation.md: führt
# den dort stehenden Befehlsblock WÖRTLICH aus und kontrolliert, dass danach
# trivy und bwrap laufen.
#
# Warum: Die Anleitung war nur auf Ubuntu erprobt. Auf Debian fiel erst spät
# auf, dass Voraussetzungen fehlten (wget, bubblewrap) und dass sich die
# Sandbox unter der Dienst-Härtung von Debian 13 nicht starten ließ.
#
# Läuft als root in einem frischen Debian oder Ubuntu (Container genügt):
#   sh testing/install-doc-test.sh
# In der CI als Matrix über alle unterstützten Distributionen (Job
# install-doc-test), von Hand in den Test-Containern auf pve-test.
set -eu

DOC="${DOC:-docs/getting-started/installation.md}"
UEBERSCHRIFT="### Trivy für den CVE-Scan nachinstallieren"
SNIPPET="$(mktemp)"

[ "$(id -u)" = 0 ] || { echo "FEHLER: als root ausführen" >&2; exit 1; }
export DEBIAN_FRONTEND=noninteractive LC_ALL=C

# Frische Images haben keine Paketlisten; ein echter Server hat sie.
apt-get update -qq

# Den ersten ```sh-Block unter der Überschrift herausziehen.
awk -v h="$UEBERSCHRIFT" '
  $0 == h { drin = 1; next }
  drin && /^```sh/ { block = 1; next }
  drin && block && /^```/ { exit }
  drin && block { print }
' "$DOC" > "$SNIPPET"
[ -s "$SNIPPET" ] || { echo "FEHLER: kein Befehlsblock unter '$UEBERSCHRIFT' gefunden" >&2; exit 1; }

# root braucht kein sudo; Container haben es nicht. Der Block läuft sonst
# unverändert.
{ echo 'sudo() { "$@"; }'; cat "$SNIPPET"; } > "$SNIPPET.run"

echo "=== Befehlsblock aus der Anleitung ==="
cat "$SNIPPET"
echo "=== Ausführung ==="
bash -eo pipefail "$SNIPPET.run"

echo "=== Ergebnis ==="
trivy --version | head -1
bwrap --version

# Die Sandbox muss mit der Aufrufzeile starten, die LCM benutzt (siehe
# internal/infrastructure/sandbox) - und zwar unter derselben Härtung wie der
# Dienst. Die Optionen kommen deshalb aus packaging/lcm.service selbst: Wer dort
# eine Option ergänzt, die Bubblewrap bricht, fällt hier auf. So blieb
# RestrictSUIDSGID auf Debian 13 unbemerkt. Nur dort prüfbar, wo systemd läuft
# (nicht in einem Docker-Container).
UNIT="${UNIT:-packaging/lcm.service}"
if [ -d /run/systemd/system ] && command -v systemd-run >/dev/null 2>&1 && [ -f "$UNIT" ]; then
  # Eine Option je Zeile; die Werte können Leerzeichen enthalten.
  grep -E '^(NoNewPrivileges|Protect[A-Za-z]+|PrivateTmp|Restrict[A-Za-z]+|LockPersonality|MemoryDenyWriteExecute|CapabilityBoundingSet|AmbientCapabilities)=' "$UNIT" \
    | grep -v '^ProtectSystem=' > "$SNIPPET.opts"
  echo "ProtectSystem=strict" >> "$SNIPPET.opts"
  set --
  while IFS= read -r opt; do set -- "$@" -p "$opt"; done < "$SNIPPET.opts"
  echo "Härtung aus $UNIT: $*"
  systemd-run --wait --pipe -q -p User=nobody "$@" \
    bwrap --die-with-parent --new-session --proc /proc --dev /dev --clearenv \
      --tmpfs /tmp --ro-bind-try /usr /usr --ro-bind-try /lib /lib \
      --ro-bind-try /lib64 /lib64 --ro-bind-try /bin /bin --unshare-all \
      -- /usr/bin/env true
  echo "Sandbox unter Dienst-Härtung: ok"
else
  echo "Sandbox-Probe übersprungen (kein systemd oder keine Unit)"
fi
echo "OK"
