#!/bin/sh
# Schneidet aus dem CHANGELOG je Version eine Datei <version>.md für das Fenster
# nach einem Update. Vorne steht der handgeschriebene Text der Release-Reihe aus
# release-info/<x.y.z>.md: Er gilt für die fertige Version und ihre Betas. Danach
# folgt der Auszug, der Anwender betrifft: Refactoring und Sonstiges bleiben
# draußen, die Überschriften sind Klartext, Emojis, Bereiche und Commit-Kürzel
# haben im Fenster nichts zu suchen.
#
# Aufruf: tools/release-notes.sh CHANGELOG.md docs/static/release-notes [release-info]
set -eu
source=$1
target=$2
info=${3:-release-info}
mkdir -p "$target"

awk -v target="$target" -v info="$info" '
  function start(version,    series, text, line) {
    if (file) close(file)
    file = target "/" version ".md"
    printf "" > file
    series = version
    sub(/-.*$/, "", series)
    text = info "/" series ".md"
    while ((getline line < text) > 0) print line > file
    close(text)
    keep = 0
  }
  function heading(name) {
    if (name == "Breaking Changes") return "Das musst du wissen"
    if (name == "Features") return "Neu"
    if (name == "Bugfixes") return "Behoben"
    if (name == "Performance") return "Schneller"
    return ""
  }
  /^## v/ { start(substr($2, 2)); next }
  !file { next }
  /^### / {
    name = heading(substr($0, index($0, $3)))
    keep = name != ""
    if (keep) print "\n### " name "\n" > file
    next
  }
  !keep || !/^- / { next }
  {
    sub(/ \([0-9a-f]{7,40}\)$/, "")
    sub(/^- \*\*[^*]+\*\*: /, "- ")
    print "- " toupper(substr($0, 3, 1)) substr($0, 4) > file
  }
' "$source"
