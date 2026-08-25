#!/bin/sh
# setup-enterprise.sh - switch a machine from the community package channel to
# the LCM enterprise channel.
#
# This script runs on the CUSTOMER's machine. It is published on the repository
# server next to setup.sh:
#
#   curl -fsSL https://repo.techeve.de/setup-enterprise.sh | sudo sh -s -- \
#     LCM-E-XXXX-XXXX-XXXX <key>
#
# Run without arguments to be asked for the key instead - that keeps it out of
# the shell history and the process list.
#
# To go back to the free channel:
#   curl -fsSL https://repo.techeve.de/setup-enterprise.sh | sudo sh -s -- --revert
#
# All output is plain ASCII on purpose: it ends up in terminals and logs whose
# encoding we do not control. The German texts therefore use ue/ae/oe/ss.
set -eu

REPO_HOST="repo.techeve.de"
REPO_URL="https://$REPO_HOST"
KEYRING_FALLBACK="/etc/apt/keyrings/techeve.asc"
AUTH_FILE="/etc/apt/auth.conf.d/lcm-enterprise.conf"
ENT_LIST="/etc/apt/sources.list.d/lcm-enterprise.list"
# The community source exists in two spellings: setup.sh writes it as a deb822
# file (.sources) today, older installations have the classic one-line .list.
# Knowing only one of them means the other one stays active - and then both
# channels are configured at once, which defeats the whole point.
COM_LIST="/etc/apt/sources.list.d/techeve.list"
COM_SOURCES="/etc/apt/sources.list.d/techeve.sources"
# Preference rule: keeps the LCM packages on the enterprise channel without
# switching the community source off - that one carries other packages too.
PREF_FILE="/etc/apt/preferences.d/lcm-enterprise.pref"
# How the channels mark themselves in the Release file (aptly: Label per
# publish point, set with set-channel-metadata.sh). Only this lets apt tell
# them apart: same host, same suite.
LABEL_COMMUNITY="techeve-community"
LABEL_BETA="techeve-beta"
PINNED="lcm lcm-agent"
SUITE="stable"

# lcm_lang picks the output language: English by default, German only if the
# system is actually set to German. Same rule and same order as the package
# maintainer scripts (POSIX: LC_ALL beats LC_MESSAGES beats LANG), including
# the fallback to the system-wide locale file for stripped environments.
lcm_lang() {
	_l="${LC_ALL:-}"
	[ -z "$_l" ] && _l="${LC_MESSAGES:-}"
	[ -z "$_l" ] && _l="${LANG:-}"
	for _f in /etc/default/locale /etc/locale.conf; do
		[ -n "$_l" ] && break
		[ -r "$_f" ] || continue
		_l=$(grep -E '^[[:space:]]*(LANG|LC_ALL|LC_MESSAGES)=' "$_f" 2>/dev/null |
			head -n 1 | cut -d= -f2- | tr -d '"'\''' | tr -d '[:space:]')
	done
	case "$_l" in
	de | de_* | De_* | DE_*) echo de ;;
	*) echo en ;;
	esac
}
UI_LANG=$(lcm_lang)

# say prints the English or the German variant - the shell equivalent of
# i18n.T(en, de) in the Go code.
say() {
	if [ "$UI_LANG" = de ]; then echo "$2"; else echo "$1"; fi
}

die() {
	say "ERROR: $1" "FEHLER: $2" >&2
	exit 1
}

[ "$(id -u)" = "0" ] || die \
	"please run as root (sudo)." \
	"bitte als root ausfuehren (sudo)."

command -v apt-get > /dev/null 2>&1 || die \
	"this script needs apt (Debian/Ubuntu)." \
	"dieses Skript benoetigt apt (Debian/Ubuntu)."

# detect_keyring finds the signing key the community setup already installed -
# both channels are signed with the same key. Its name has changed between
# setup script versions, so look for it instead of assuming one: first in the
# source files themselves, then in the usual directories. Prints nothing when
# there is no key yet.
detect_keyring() {
	_k=$(sed -n 's/^[Ss]igned-[Bb]y:[[:space:]]*//p' "$COM_SOURCES" 2> /dev/null | head -n 1)
	[ -n "$_k" ] || _k=$(sed -n 's/.*signed-by=\([^]]*\)\].*/\1/p' \
		"$COM_LIST" "$COM_LIST.disabled" 2> /dev/null | head -n 1)
	[ -n "$_k" ] || _k=$(ls /etc/apt/keyrings/techeve*.gpg /etc/apt/keyrings/techeve*.asc \
		/usr/share/keyrings/techeve*.gpg 2> /dev/null | head -n 1)
	echo "$_k"
}

# disable_community switches the free channel off - in both spellings. The
# classic file is renamed (apt only reads *.list), the deb822 file gets
# "Enabled: no", a switch apt understands itself: name and content stay, so
# the way back is clean.
disable_community() {
	_dis=0
	if [ -e "$COM_LIST" ]; then
		mv "$COM_LIST" "$COM_LIST.disabled"
		_dis=1
	fi
	[ -e "$COM_LIST.disabled" ] && _dis=1
	if [ -e "$COM_SOURCES" ]; then
		awk '{k=tolower($1)} k=="enabled:"{next} {print} k=="types:"{print "Enabled: no"}' \
			"$COM_SOURCES" > "$COM_SOURCES.tmp"
		# Only write back when awk actually produced something: a failed run
		# must never empty the only package source of the machine.
		[ -s "$COM_SOURCES.tmp" ] && cat "$COM_SOURCES.tmp" > "$COM_SOURCES"
		rm -f "$COM_SOURCES.tmp"
		_dis=1
	fi
	[ "$_dis" = 1 ] || say \
		"Note: no community source found ($COM_LIST / $COM_SOURCES) - please check whether a differently named source still points at the free channel." \
		"Hinweis: keine Community-Quelle gefunden ($COM_LIST / $COM_SOURCES) - bitte pruefen, ob eine anders benannte Quelle weiterhin auf den freien Kanal zeigt."
}

# enable_community undoes disable_community. An "Enabled: yes" set by the
# operator is left alone - only our own "Enabled: no" is removed.
enable_community() {
	[ -e "$COM_LIST.disabled" ] && mv "$COM_LIST.disabled" "$COM_LIST"
	if [ -e "$COM_SOURCES" ]; then
		awk '{k=tolower($1)} k=="enabled:" && tolower($2)=="no"{next} {print}' \
			"$COM_SOURCES" > "$COM_SOURCES.tmp"
		[ -s "$COM_SOURCES.tmp" ] && cat "$COM_SOURCES.tmp" > "$COM_SOURCES"
		rm -f "$COM_SOURCES.tmp"
	fi
	return 0
}

# separate_channels keeps LCM on the enterprise channel - gently if the server
# allows it. Preferred way: a preference rule telling apt that the LCM packages
# from the community channel are never an option (priority -1). The community
# source stays active and keeps delivering its OTHER packages; switching the
# whole source off would take those away too.
#
# That needs the channels to be distinguishable in the Release file (Label,
# see set-channel-metadata.sh). Old publish points carry none - then only the
# blunt way is left, and the script says why. The check runs against the
# RE-ENABLED community source: otherwise an earlier switch would block the
# better way forever.
#
# LC_ALL=C is required: apt-cache translates its output.
separate_channels() {
	enable_community
	apt-get update -qq || true
	if LC_ALL=C apt-cache policy 2> /dev/null | grep -q "l=${LABEL_COMMUNITY}"; then
		mkdir -p "$(dirname "$PREF_FILE")"
		printf '%s\n' \
			'# Managed by setup-enterprise.sh: LCM comes from the enterprise channel only.' \
			'# The community source stays usable for every other package.' \
			"Package: ${PINNED}" "Pin: release l=${LABEL_COMMUNITY}" 'Pin-Priority: -1' '' \
			"Package: ${PINNED}" "Pin: release l=${LABEL_BETA}" 'Pin-Priority: -1' > "$PREF_FILE"
		chmod 0644 "$PREF_FILE"
		say "Channels separated by preference rule - the community source stays usable for other packages." \
			"Kanaltrennung per Vorrang-Regel - die Community-Quelle bleibt fuer andere Pakete nutzbar."
	else
		rm -f "$PREF_FILE"
		disable_community
		say "Note: this repository server does not mark its channels yet (no Label in the Release file), so the community source was switched off. Other packages from it are unavailable for now." \
			"Hinweis: dieser Repository-Server kennzeichnet seine Kanaele noch nicht (kein Label im Release-File), deshalb wurde die Community-Quelle stillgelegt. Andere Pakete von dort sind vorerst nicht verfuegbar."
	fi
	apt-get update -qq || true
}

# proof_channel answers the question that actually matters: where would lcm
# come from now? Sharper than "is the community source gone", and correct for
# both ways above. Without a candidate there is nothing to compare.
proof_channel() {
	_cand=$(LC_ALL=C apt-cache policy lcm 2> /dev/null | sed -n 's/^ *Candidate: *//p' | head -n 1)
	_src=$(LC_ALL=C apt-cache policy lcm 2> /dev/null | awk -v v="$_cand" '
		/Version table:/ {t=1; next}
		t && !f && $0 ~ ("(^| )" v "( |$)") {f=1; next}
		f {if ($0 ~ /:\/\//) print $2; exit}')
	[ -n "$_src" ] || return 0
	case "$_src" in
	"$REPO_URL/enterprise"*)
		say "Check: lcm $_cand comes from $_src." \
			"Gegenprobe: lcm $_cand kommt aus $_src."
		;;
	*)
		say "WARNING: lcm $_cand would still come from $_src instead of the enterprise channel - please check the apt sources of this machine." \
			"WARNUNG: lcm $_cand kaeme weiterhin aus $_src statt aus dem Enterprise-Kanal - bitte die apt-Quellen dieser Maschine pruefen."
		;;
	esac
}

# --- revert -----------------------------------------------------------------

if [ "${1:-}" = "--revert" ]; then
	# The preference rule has to go as well: it would keep holding lcm away
	# from the community channel, and the machine would never see an update.
	rm -f "$ENT_LIST" "$AUTH_FILE" "$PREF_FILE"
	enable_community
	if [ ! -e "$COM_LIST" ] && [ ! -e "$COM_SOURCES" ]; then
		KEYRING=$(detect_keyring)
		[ -n "$KEYRING" ] || KEYRING="$KEYRING_FALLBACK"
		printf 'deb [signed-by=%s] %s %s main\n' \
			"$KEYRING" "$REPO_URL" "$SUITE" > "$COM_LIST"
	fi
	apt-get update -qq || true
	say "Switched back to the free community channel." \
		"Zurueck auf den freien Community-Kanal gewechselt."
	exit 0
fi

# --- collect the key --------------------------------------------------------

KEY_ID="${1:-}"
KEY="${2:-}"

if [ -z "$KEY_ID" ] || [ -z "$KEY" ]; then
	# Reading from the terminal keeps the key out of `ps` and the history.
	# With `curl | sh` stdin is the script itself, so read from /dev/tty.
	[ -r /dev/tty ] || die \
		"no terminal available - pass key id and key as arguments." \
		"kein Terminal verfuegbar - Key-ID und Key als Argumente uebergeben."
	[ -n "$KEY_ID" ] || {
		say "Key ID (LCM-E-...):" "Key-ID (LCM-E-...):"
		read -r KEY_ID < /dev/tty
	}
	[ -n "$KEY" ] || {
		say "Key:" "Key:"
		stty -echo < /dev/tty 2> /dev/null || true
		read -r KEY < /dev/tty
		stty echo < /dev/tty 2> /dev/null || true
		echo
	}
fi

[ -n "$KEY_ID" ] && [ -n "$KEY" ] || die \
	"key id and key must not be empty." \
	"Key-ID und Key duerfen nicht leer sein."

case "$KEY_ID" in
LCM-E-*) ;;
*) die "key id should look like LCM-E-XXXX-XXXX-XXXX." \
	"die Key-ID sollte wie LCM-E-XXXX-XXXX-XXXX aussehen." ;;
esac

# --- 1. signing key ---------------------------------------------------------
#
# Prefer the key that is already there: taking it from the community source
# keeps a single key on the machine and works no matter which name the setup
# script of the day used.
KEYRING=$(detect_keyring)
if [ -z "$KEYRING" ]; then
	KEYRING="$KEYRING_FALLBACK"
	say "Fetching the repository signing key ..." \
		"Signaturschluessel des Repositorys wird geholt ..."
	mkdir -p "$(dirname "$KEYRING")"
	curl -fsSL "$REPO_URL/repo-key.gpg" -o "$KEYRING" || die \
		"could not download the signing key from $REPO_URL." \
		"Signaturschluessel konnte nicht von $REPO_URL geladen werden."
	chmod 0644 "$KEYRING"
fi

# --- 2. credentials ---------------------------------------------------------
#
# The machine line is scoped to protocol, host AND path, so the key is only
# ever sent over TLS to the enterprise channel - never to the public part of
# the same server, and never over plain HTTP.
#
# Spelling out the protocol matters: without it apt uses the entry for https
# only and silently refuses to authenticate over http (it warns "the protocol
# is not encrypted"). Being explicit turns that implicit rule into something
# visible in the file.
say "Storing the subscription key ..." "Subscription-Key wird hinterlegt ..."
mkdir -p "$(dirname "$AUTH_FILE")"
OLD_UMASK=$(umask)
umask 077
cat > "$AUTH_FILE" <<EOF
machine $REPO_URL/enterprise
login $KEY_ID
password $KEY
EOF
umask "$OLD_UMASK"
chown root:root "$AUTH_FILE"
chmod 0600 "$AUTH_FILE"

# --- 3. sources -------------------------------------------------------------

printf 'deb [signed-by=%s] %s/enterprise %s main\n' \
	"$KEYRING" "$REPO_URL" "$SUITE" > "$ENT_LIST"

# --- 4. verify --------------------------------------------------------------
#
# Only refresh our own list here. A wrong key must fail loudly and be undone,
# without a full `apt-get update` masking it among other repositories.
say "Checking the subscription ..." "Subscription wird geprueft ..."
if ! apt-get update -qq \
	-o Dir::Etc::sourcelist="sources.list.d/$(basename "$ENT_LIST")" \
	-o Dir::Etc::sourceparts="-" \
	-o APT::Get::List-Cleanup="0" 2> /tmp/lcm-ent-check.$$; then

	head -5 /tmp/lcm-ent-check.$$ >&2 || true
	rm -f /tmp/lcm-ent-check.$$
	# Undo, so the machine is never left without a package source.
	rm -f "$ENT_LIST" "$AUTH_FILE" "$PREF_FILE"
	enable_community
	apt-get update -qq || true
	die "the subscription key was not accepted - nothing was changed. Check key id and key, or whether the subscription has expired." \
		"der Subscription-Key wurde nicht akzeptiert - es wurde nichts geaendert. Bitte Key-ID und Key pruefen, oder ob die Subscription abgelaufen ist."
fi
rm -f /tmp/lcm-ent-check.$$

# --- 5. separate the channels ----------------------------------------------
#
# Both channels serving LCM at once would defeat the point: apt would install
# whichever version is higher, and sooner or later that is the one from the
# free channel.
separate_channels
proof_channel

if [ "$UI_LANG" = de ]; then
	cat <<EOF

============================================================
  Enterprise-Kanal ist aktiv.

  Quelle:  $REPO_URL/enterprise $SUITE main
  Key:     $KEY_ID
           (Zugangsdaten in $AUTH_FILE, nur fuer root lesbar)

  Updates wie gewohnt:  apt update && apt upgrade

  Dieser Kanal erhaelt Funktions-Updates erst, nachdem sie
  sich im freien Kanal bewaehrt haben. Sicherheitsupdates
  kommen in beiden Kanaelen zeitgleich.

  Zurueck auf den freien Kanal:
      curl -fsSL $REPO_URL/setup-enterprise.sh | sudo sh -s -- --revert
============================================================

EOF
else
	cat <<EOF

============================================================
  The enterprise channel is active.

  Source:  $REPO_URL/enterprise $SUITE main
  Key:     $KEY_ID
           (credentials in $AUTH_FILE, readable by root only)

  Update as usual:  apt update && apt upgrade

  This channel receives feature updates only after they have
  proven themselves in the free channel. Security updates
  reach both channels at the same time.

  Back to the free channel:
      curl -fsSL $REPO_URL/setup-enterprise.sh | sudo sh -s -- --revert
============================================================

EOF
fi
