#!/bin/sh
# Prüft Texte für Anwender auf interne Namen aus tools/internal-terms.txt:
# Server, Container, Netze und Personen von Techeve gehören nicht ins
# Changelog, nicht ins Fenster nach dem Update und nicht in Commit-Betreffe.
#
# Aufruf:
#   tools/check-internal-terms.sh                 CHANGELOG.md und release-info/*.md
#   tools/check-internal-terms.sh DATEI...        die genannten Dateien
#   git log --format=%s A..B | tools/check-internal-terms.sh -   Zeilen von stdin
#
# Ende mit 1, wenn ein Name gefunden wurde; die Fundstellen stehen davor.
set -eu

terms="$(dirname "$0")/internal-terms.txt"
pattern=$(grep -v -e '^#' -e '^[[:space:]]*$' "$terms" | paste -sd '|' -)

[ $# -gt 0 ] || set -- CHANGELOG.md release-info/*.md

if grep -n -i -E "$pattern" "$@"; then
  echo >&2
  echo "FEHLER: interne Namen in Texten für Anwender (Liste: $terms)." >&2
  echo "Die Zeile so umformulieren, dass sie sagt, was sich für Anwender ändert," >&2
  echo "ohne eigene Server, Container, Netze oder Personen zu nennen." >&2
  exit 1
fi
echo "OK: keine internen Namen gefunden."
