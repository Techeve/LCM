#!/bin/bash
# Bereitet einen Release VOR - auf dem Branch, dessen Kanal releasen soll.
#
# Warum: Der Changelog gehört in genau den Commit, der später getaggt wird.
# Deshalb ermitteln wir hier die nächste Version aus den Conventional Commits
# seit dem letzten Tag, schreiben VERSION + CHANGELOG.md und committen das.
# Der in den Release-Branch überführte Commit trägt dann bereits den passenden
# Changelog, und die CI muss nichts zurückschreiben (kein RELEASE_TOKEN nötig).
#
# Einsatz je Branch (Branch-Modell: develop → beta → community → enterprise):
#   develop     nächstes Feature-Release vorbereiten, bevor der Release-Zug
#               nach beta abfährt (Version aus den Conventional Commits)
#   beta        Vorabversionen, immer explizit: ./packaging/prepare-release.sh 1.12.0-beta.1
#               Die finale Version (ohne -beta.N) wird ebenfalls hier gesetzt,
#               bevor der MR nach community geht - releast und ausgerollt wird
#               sie aber erst auf community (siehe version-Job in .gitlab-ci.yml).
#   enterprise  Fix-Releases des Wartungszweigs (Version aus den Fix-Commits
#               seit dem letzten Tag der Wartungslinie)
#
# Ablauf:
#   1. ./packaging/prepare-release.sh            # Version aus Commits
#      oder: ./packaging/prepare-release.sh 1.0.0     # explizite Version
#   2. git push origin <branch>
#   3. Merge Request in den Release-Branch (develop→beta, beta→community);
#      auf beta/enterprise genügt der direkte Push des Release-Commits.
#      -> release-Job taggt v<VERSION> und erzeugt das Release aus CHANGELOG.md,
#         deploy-Job rollt das .deb in den apt-Kanal des Branches aus.
#
# Optionen:
#   PUSH=1   committeten Stand direkt nach origin/<branch> pushen
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

BRANCH=$(git rev-parse --abbrev-ref HEAD)
case "$BRANCH" in
  develop|beta|enterprise) ;;
  *)
    echo "FEHLER: bitte auf develop, beta oder enterprise ausführen (aktuell: $BRANCH)." >&2
    exit 1
    ;;
esac
if [ -n "$(git status --porcelain)" ]; then
  echo "FEHLER: Arbeitsverzeichnis nicht sauber - bitte erst committen/stashen." >&2
  exit 1
fi

echo ">> Tags aktualisieren"
git fetch --tags --quiet origin

# develop führt ausschließlich Vorabversionen. Das Werkzeug liest die
# Release-Reihe aus dem Suffix der VERSION-Datei - und genau das Suffix geht
# verloren, sobald ein Aufwärtsmerge (enterprise -> community -> beta ->
# develop) eine finale Version mitbringt. Ohne diesen Riegel schlüge das
# nächste Release auf develop still als FINALE Version vor; sie liefe durch
# beta bis community und nähme dort den Tag vorweg, der dem Community-Kanal
# gehört. Genau so passiert nach dem Enterprise-Release v1.30.6.
if [ "$BRANCH" = "develop" ] && [ -z "${1:-}" ]; then
  case "$(tr -d ' \n' < VERSION)" in
    *-*) ;;
    *)
      echo ">> VERSION trägt kein Prerelease-Suffix (Aufwärtsmerge?) - ergänze -beta.1 für die Ableitung."
      printf '%s-beta.1\n' "$(tr -d ' \n' < VERSION)" > VERSION
      ;;
  esac
fi

VER_ARG="${1:-}"
TMP_ENV=$(mktemp)
TMP_SNIP=$(mktemp)
trap 'rm -f "$TMP_ENV" "$TMP_SNIP"' EXIT

# -rest liefert das bestehende Changelog ohne Kopfzeile - und bei einem
# FINALEN Release ohne die führenden Vorabversions-Abschnitte: die gehen im
# neuen Abschnitt auf (community sammelt die Betas ein, siehe
# tools/release/changelog.go).
if [ -n "$VER_ARG" ]; then
  go run ./tools/release -version "$VER_ARG" -env "$TMP_ENV" -changelog "$TMP_SNIP" -rest "${TMP_SNIP}.rest"
else
  go run ./tools/release -env "$TMP_ENV" -changelog "$TMP_SNIP" -rest "${TMP_SNIP}.rest"
fi

# shellcheck disable=SC1090
. "$TMP_ENV"

if [ "${RELEASE_NEEDED:-false}" != "true" ]; then
  echo ">> Keine release-relevanten Commits seit ${LAST_TAG:-Anfang} - nichts vorzubereiten."
  echo "   (Für einen erzwungenen Release eine explizite Version angeben, z.B. '$0 1.0.0'.)"
  exit 0
fi

echo ">> Nächste Version: ${NEXT_VERSION}"

# VERSION aktualisieren.
printf '%s\n' "$NEXT_VERSION" > VERSION

# Neuen Abschnitt oben in CHANGELOG.md einfügen (bestehende Historie behalten).
# tail -n +3 überspringt die "# Changelog"-Kopfzeile + Leerzeile.
# Den Rest hat das Werkzeug geschrieben (siehe -rest oben); die Rückfalllinie
# deckt nur den Fall ab, dass es gar kein Changelog gab.
[ -f "${TMP_SNIP}.rest" ] || : > "${TMP_SNIP}.rest"
{
  echo "# Changelog"
  echo
  cat "$TMP_SNIP"
  echo
  cat "${TMP_SNIP}.rest"
} > CHANGELOG.md
rm -f "${TMP_SNIP}.rest"

git add VERSION CHANGELOG.md
git commit -m "release: v${NEXT_VERSION} - Version & Changelog vorbereitet"

echo
echo ">> Vorbereitet und committet: release: v${NEXT_VERSION}"
if [ "${PUSH:-0}" = "1" ]; then
  echo ">> Push nach origin/${BRANCH}"
  git push origin "$BRANCH"
  echo ">> Fertig."
else
  echo "   Nächste Schritte:"
  echo "     git push origin ${BRANCH}"
fi
# Was der Push tatsächlich auslöst, hängt auf beta an der Versionsform -
# dieselbe Unterscheidung trifft der version-Job in .gitlab-ci.yml.
case "$BRANCH" in
  develop) echo "   Dann: Merge Request develop -> beta (Release-Zug) erstellen und nach grüner Pipeline mergen." ;;
  beta)
    case "$NEXT_VERSION" in
      *-*) echo "   Der Push auf beta releast v${NEXT_VERSION} in den apt-Kanal 'beta'." ;;
      *)   echo "   Der Push auf beta taggt v${NEXT_VERSION} NICHT - eine finale Version"
           echo "   gehört dem Community-Kanal, der version-Job überlässt sie ihm."
           echo "   Dann: Merge Request beta -> community erstellen und nach grüner Pipeline mergen." ;;
    esac ;;
  enterprise) echo "   Der Push auf enterprise releast v${NEXT_VERSION} in den Enterprise-Kanal." ;;
esac
