#!/bin/bash
# Withdraws a specific lcm version from the aptly repository (removes the
# packages and re-publishes the suite) - for corrections, e.g. an accidentally
# published version.
#
#   REPO_URL, REPO_USER, REPO_PASS erforderlich (wie beim Deploy).
#   Optional: REPO_NAME (techeve), DISTRO (stable), GPG_KEY (repo@techeve.de).
#   Usage:   REPO_URL=... REPO_USER=... REPO_PASS=... ./packaging/remove-deb.sh 1.0.0
set -euo pipefail

: "${REPO_URL:?REPO_URL erforderlich}"
: "${REPO_USER:?REPO_USER erforderlich}"
: "${REPO_PASS:?REPO_PASS erforderlich}"
REPO_NAME="${REPO_NAME:-techeve}"
DISTRO="${DISTRO:-stable}"
GPG_KEY="${GPG_KEY:-repo@techeve.de}"
VER="${1:?Version als Argument angeben, z.B. 1.0.0}"

CURL="curl -fsS -u ${REPO_USER}:${REPO_PASS}"

echo ">> Suche lcm-Pakete der Version ${VER} im Repo '${REPO_NAME}'"
# Fetch all package keys and filter locally on (name lcm, version VER) -
# Key-Format: "P<arch> <name> <version> <hash>".
REFS=$(${CURL} "${REPO_URL}/api/repos/${REPO_NAME}/packages" | python3 -c "
import json,sys
ver='${VER}'
ks=json.load(sys.stdin)
sel=[k for k in ks if len(k.split())>=3 and k.split()[1]=='lcm' and k.split()[2]==ver]
print(json.dumps(sel))
")
COUNT=$(echo "$REFS" | python3 -c "import json,sys; print(len(json.load(sys.stdin)))")
if [ "$COUNT" -eq 0 ]; then
  echo ">> No matching packages found - nothing to do."
  exit 0
fi
echo ">> Entferne ${COUNT} Paket(e): $REFS"
${CURL} -X DELETE -H "Content-Type: application/json" \
  --data "{\"PackageRefs\": ${REFS}}" \
  "${REPO_URL}/api/repos/${REPO_NAME}/packages" >/dev/null

echo ">> Suite '${DISTRO}' neu publizieren und signieren"
${CURL} -X PUT -H "Content-Type: application/json" \
  --data "{\"Signing\":{\"Batch\":true,\"GpgKey\":\"${GPG_KEY}\"}}" \
  "${REPO_URL}/api/publish/:./${DISTRO}" >/dev/null

echo ">> Done - version ${VER} removed from ${REPO_URL}."
