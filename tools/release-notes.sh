#!/bin/sh
# Schneidet aus dem CHANGELOG je Version eine Datei <version>.md für das Fenster
# nach einem Update. Die Überschrift der Version nennt das Fenster selbst;
# Emojis und Commit-Kürzel haben dort nichts zu suchen.
#
# Aufruf: tools/release-notes.sh CHANGELOG.md docs/static/release-notes
set -eu
source=$1
target=$2
mkdir -p "$target"

awk -v target="$target" '
  /^## v/ {
    if (file) close(file)
    version = substr($2, 2)
    file = target "/" version ".md"
    printf "" > file
    next
  }
  !file { next }
  /^### / { sub(/^### [^ ]+ /, "### ") }
  { sub(/ \([0-9a-f]{7,40}\)$/, ""); print > file }
' "$source"
