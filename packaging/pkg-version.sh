#!/bin/sh
# pkg-version - LCM-Version in der Schreibweise, die nfpm für ein Paketformat
# braucht, damit eine Beta gültig ist und vor dem Finale einsortiert wird.
#
#   sh packaging/pkg-version.sh <deb|rpm|apk|archlinux> <version>
#
# Für deb und rpm genügt die SemVer-Version: nfpm macht aus 2.0.0-beta.16 selbst
# 2.0.0~beta.16. Für die beiden anderen Formate nicht (geprüft mit nfpm 2.48):
#
#   apk        nfpm schreibt 2.0.0_beta.16, und apk hält den Punkt in der
#              Vorabkennung für ungültig. Gültig ist 2.0.0_beta16.
#   archlinux  nfpm übernimmt die Vorabkennung nur in den Dateinamen. Im Paket
#              selbst steht ohne Epoche 2.0.0, die Beta gäbe sich als Finale
#              aus. Ohne Bindestrich ist die Version kein SemVer mehr, und nfpm
#              übernimmt 2.0.0beta.16 wörtlich. pacman sortiert das vor 2.0.0.
set -eu

[ $# -eq 2 ] || { echo "Aufruf: pkg-version.sh <deb|rpm|apk|archlinux> <version>" >&2; exit 2; }
format=$1
version=$2

case "$format" in
deb | rpm) echo "$version" ;;
apk) echo "$version" | sed 's/^\([^-]*-[a-z]*\)\.\([0-9][0-9]*\)$/\1\2/' ;;
archlinux) echo "$version" | tr -d '-' ;;
*)
	echo "pkg-version.sh: unbekanntes Paketformat $format" >&2
	exit 2
	;;
esac
