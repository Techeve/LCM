#!/bin/bash
# Rollt die gebauten LCM-.deb-Pakete (amd64 + arm64 aus bin/) auf den TechEve-
# repository server (aptly): upload -> add to the repo -> re-publish the
# neu publizieren und signieren. Danach ist LCM per `apt install lcm` aus dem
# eigenen Repo installier- und aktualisierbar.
#
# Required environment/CI variables:
#   REPO_URL   Basis-URL der aptly-HTTP-API, z.B. https://repo.techeve.de
#   REPO_USER  Basic-Auth-Benutzer
#   REPO_PASS  Basic-Auth-Passwort (masked)
# Optional:
#   REPO_NAME       aptly-Repo-Name          (Default: techeve)
#   DISTRO          zu publizierende Suite   (Default: stable)
#   PUBLISH_PREFIX  aptly-Publish-Prefix     (Default: ":." = Wurzel;
#                   der Enterprise-Kanal nutzt "enterprise", damit sein
#                   pool/-Baum hinter der Zugriffskontrolle liegt)
#   GPG_KEY    signing key            (default: repo@techeve.de)
#
# Die Kanal-Zuordnung (Branch → REPO_NAME/DISTRO/PUBLISH_PREFIX) trifft die CI
# (.gitlab-ci.yml, Job deploy:apt); das Kanal-Layout auf dem Server beschreibt
# packaging/repo-server/README.md.
set -euo pipefail

: "${REPO_URL:?REPO_URL erforderlich}"
: "${REPO_USER:?REPO_USER erforderlich}"
: "${REPO_PASS:?REPO_PASS erforderlich}"
REPO_NAME="${REPO_NAME:-techeve}"
DISTRO="${DISTRO:-stable}"
PUBLISH_PREFIX="${PUBLISH_PREFIX:-:.}"
GPG_KEY="${GPG_KEY:-repo@techeve.de}"

shopt -s nullglob
DEBS=(bin/*.deb)

# Der lcm-agent gehört NICHT in den Enterprise-Kanal. Dieser Kanal ist
# zugangsbeschränkt und wird ausschließlich auf dem LCM-Server selbst
# eingerichtet; die verwalteten Maschinen, auf denen der Agent läuft, kommen
# nie an ihn heran und beziehen ihn immer aus dem Community-Kanal. Ein
# Agent-Paket hier wäre also von vornherein unerreichbar - und würde die
# Erwartung wecken, es gäbe einen eigenen Enterprise-Agenten.
if [ "${PUBLISH_PREFIX}" = "enterprise" ]; then
  FILTERED=()
  for deb in "${DEBS[@]}"; do
    case "$(basename "$deb")" in
      lcm-agent_*) echo "   (übersprungen, gehört nicht in den Enterprise-Kanal: $(basename "$deb"))" ;;
      *) FILTERED+=("$deb") ;;
    esac
  done
  DEBS=("${FILTERED[@]}")
fi

[ "${#DEBS[@]}" -gt 0 ] || { echo "Keine .deb-Dateien in bin/ gefunden"; exit 1; }

CURL="curl -fsS --retry 3 --retry-delay 2 -u ${REPO_USER}:${REPO_PASS}"
# Eindeutiges Staging-Verzeichnis je Pipeline-Lauf.
UPDIR="lcm-${CI_PROJECT_ID:-x}-${CI_PIPELINE_IID:-0}"

echo ">> 1/3 Pakete nach Staging '${UPDIR}' hochladen"
for DEB in "${DEBS[@]}"; do
  echo "   - ${DEB}"
  ${CURL} -X POST -F "file=@${DEB}" "${REPO_URL}/api/files/${UPDIR}" >/dev/null
done

echo ">> 2/3 Adding to repository '${REPO_NAME}'"
${CURL} -X POST "${REPO_URL}/api/repos/${REPO_NAME}/file/${UPDIR}" >/dev/null

echo ">> 3/3 Suite '${DISTRO}' (Prefix '${PUBLISH_PREFIX}') neu publizieren und signieren"
${CURL} -X PUT -H "Content-Type: application/json" \
  --data "{\"Signing\":{\"Batch\":true,\"GpgKey\":\"${GPG_KEY}\"}}" \
  "${REPO_URL}/api/publish/${PUBLISH_PREFIX}/${DISTRO}" >/dev/null

# Gegenprobe: Steht die soeben gelieferte Version auch WIRKLICH im
# veröffentlichten Index?
#
# Der Publish-Aufruf oben kann gelingen, ohne etwas auszurichten: Hängt der
# Publish-Punkt an einem SNAPSHOT statt am lokalen Repo, veröffentlicht aptly
# gehorsam wieder den Snapshot - HTTP 200, alte Pakete. Genau so lieferte der
# Enterprise-Kanal über vier Releases hinweg eine alte Version aus, während
# jeder Job „Done" meldete. Ohne diese Prüfung merkt es niemand.
VERSION_FILE="${VERSION_FILE:-VERSION}"
if [ -f "$VERSION_FILE" ]; then
  WANT="$(tr -d ' \n' < "$VERSION_FILE" | tr '-' '~')"   # 1.16.1-beta.1 -> 1.16.1~beta.1
  case "${PUBLISH_PREFIX}" in
    ":.") IDX="${REPO_URL}/dists/${DISTRO}/main/binary-amd64/Packages" ;;
    *)    IDX="${REPO_URL}/${PUBLISH_PREFIX}/dists/${DISTRO}/main/binary-amd64/Packages" ;;
  esac
  if [ "${PUBLISH_PREFIX}" = "enterprise" ]; then
    # Der Enterprise-Kanal ist zugangsgeschützt - ein Abruf des Index ohne
    # Subscription-Key antwortet mit 401, und einen Key hat die CI bewusst
    # nicht. Die Gegenprobe läuft deshalb über die aptly-API und prüft die
    # beiden Glieder, aus denen die Auslieferung besteht:
    #
    #   1. Der Publish-Punkt hängt am LOKALEN Repo. Das ist die Falle, für
    #      die es diese Prüfung gibt: An einem Snapshot veröffentlicht aptly
    #      gehorsam den alten Stand - HTTP 200, alte Pakete.
    #   2. Die Version steht in genau diesem Repo.
    #
    # Zusammen heißt das: genau sie wird ausgeliefert.
    echo ">> Gegenprobe (Enterprise): Kanal ist zugangsgeschützt - Prüfung über die aptly-API"
    PUB_STATE="$(${CURL} "${REPO_URL}/api/publish")"
    PUB_SOURCE="$(jq -r --arg d "${DISTRO}" \
      '.[] | select(.Prefix=="enterprise" and .Distribution==$d) | .SourceKind + " " + (.Sources[].Name)' \
      <<<"${PUB_STATE}")"
    if [ "${PUB_SOURCE}" != "local ${REPO_NAME}" ]; then
      echo "FEHLER: Publish-Punkt 'enterprise/${DISTRO}' hängt nicht am lokalen Repo '${REPO_NAME}'"
      echo "  (ist: '${PUB_SOURCE:-nicht vorhanden}' - siehe packaging/repo-server/README.md, Migration)."
      exit 1
    fi
    if ! ${CURL} "${REPO_URL}/api/repos/${REPO_NAME}/packages?q=lcm" | jq -r '.[]' | grep -q " lcm ${WANT} "; then
      echo "FEHLER: lcm ${WANT} steht NICHT im Repository '${REPO_NAME}'."
      exit 1
    fi
    # Und der Schutz selbst: Antwortet der Kanal anonym mit etwas anderem als
    # 401/403, ist er offen - dann ist der Fehlschlag hier Absicht.
    ANON_CODE="$(curl -s -o /dev/null -w '%{http_code}' "${IDX}")"
    case "${ANON_CODE}" in
      401|403) ;;
      *)
        echo "FEHLER: der Enterprise-Index antwortet anonym mit HTTP ${ANON_CODE} statt 401/403 -"
        echo "  der Kanal wäre damit ohne Subscription-Key lesbar."
        exit 1
        ;;
    esac
    echo "   bestätigt (Publish-Punkt lokal, Version im Repo, Kanal geschützt)."
  else
    echo ">> Gegenprobe: erwarte lcm ${WANT} in ${IDX}"
    # Index erst VOLLSTÄNDIG laden, dann prüfen. `curl | grep -q` scheitert unter
    # pipefail ausgerechnet im Erfolgsfall: grep -q beendet sich beim ersten
    # Treffer, curl kann den Rest nicht mehr in die Pipe schreiben und meldet
    # exit 23 - der erste Beta-Lauf dieser Prüfung schlug genau daran fehl,
    # obwohl die Version längst im Index stand.
    INDEX_CONTENT="$(${CURL} "${IDX}")"
    if ! grep -qx "Version: ${WANT}" <<<"${INDEX_CONTENT}"; then
      echo "FEHLER: lcm ${WANT} steht NICHT im veröffentlichten Index."
      echo "  Die Pakete liegen im Repository '${REPO_NAME}', werden aber nicht ausgeliefert."
      echo "  Häufigste Ursache: der Publish-Punkt '${PUBLISH_PREFIX}/${DISTRO}' hängt an einem"
      echo "  Snapshot statt am lokalen Repo (siehe packaging/repo-server/README.md, Migration)."
      exit 1
    fi
    echo "   bestätigt."
  fi
fi

echo ">> Done - LCM packages are available in repository ${REPO_URL}."
