#!/bin/sh
# distro-matrix - prueft das lcm-agent-Paket auf allen Distributionen des
# Testbetts auf pve-test (Paket F-3, doc/plan-2026-11-23-agent-robustheit.md).
#
#   tools/distro-matrix.sh [-s SNAPSHOT] [-c "CT ..."] DEB LCM-URL [AGENT-URL]
#
#   DEB        das gebaute Paket, z.B. lcm-agent_2.4.0_amd64.deb
#   LCM-URL    Oberflaeche und REST-Schnittstelle der Test-LCM, z.B. https://lcm.example.org
#   AGENT-URL  Adresse, mit der sich der Agent verbindet (Vorgabe: LCM-URL)
#   -s         Snapshot, auf den jeder Container zurueckgesetzt wird (Vorgabe vor-agent-robust)
#   -c         Container (Vorgabe: 300 310 311 312 320 321 322 330 331 332 333)
#
# Der API-Schluessel der Test-LCM kommt aus der Umgebung, nicht aus der
# Argumentliste (die jeder auf dem Rechner mit ps lesen koennte):
#
#   LCM_API_KEY=... tools/distro-matrix.sh lcm-agent_amd64.deb https://lcm.example.org
#
# LCM_CURL_OPTS reicht Optionen an curl durch, z.B. "--cacert ca.pem" oder "-k".
#
# Je Container: Snapshot zurueck, starten, .deb per pct push einspielen und
# installieren, in der Test-LCM einen Agent-Server anlegen und mit dessen Token
# einrichten, dann pruefen: Dienst aktiv, Version, online in LCM, ein Auftrag
# ueber den Agent, und der Agent kommt nach `systemctl restart` von selbst
# wieder. Am Ende steht eine Tabelle je Distribution; der Rueckgabewert ist 1,
# sobald eine Zelle scheitert.
#
# Laeuft auf dem Rechner des Entwicklers, nicht in der CI: Die Runner sollen
# pve-test nicht erreichen. SSH zum Proxmox-Host geht ueber sshc.
# Der Test-LCM bleiben je Lauf die angelegten Agent-Server (matrix-ct...); LCM
# kennt keinen Weg, sie per Schnittstelle zu loeschen.
set -u

SSHC="${SSHC:-$HOME/.claude/tools/techeve/sshc}"
PVE="${PVE:-pve-test}"
SNAPSHOT=vor-agent-robust
CTS="300 310 311 312 320 321 322 330 331 332 333"

usage() {
	sed -n '2,/^set -u/p' "$0" | sed '$d' | sed 's/^# \{0,1\}//'
	exit 2
}

while getopts s:c:h opt; do
	case $opt in
	s) SNAPSHOT=$OPTARG ;;
	c) CTS=$OPTARG ;;
	*) usage ;;
	esac
done
shift $((OPTIND - 1))
[ $# -ge 2 ] && [ $# -le 3 ] || usage
DEB=$1
LCM_URL=${2%/}
AGENT_URL=${3:-$LCM_URL}
[ -f "$DEB" ] || {
	echo "Paket $DEB nicht gefunden" >&2
	exit 2
}
[ -n "${LCM_API_KEY:-}" ] || {
	echo "LCM_API_KEY fehlt (API-Schluessel der Test-LCM, Recht servers:write)" >&2
	exit 2
}

RESULTS=$(mktemp)
BODY=$(mktemp)
trap 'rm -f "$RESULTS" "$RESULTS.table" "$BODY"' EXIT
STAMP=$(date +%Y%m%d%H%M)
REMOTE_DEB=/root/lcm-agent-matrix.deb
FAILED=0

# pve <befehl>: Befehl auf dem Proxmox-Host.
pve() {
	"$SSHC" "$PVE" "$@"
}

# api <methode> <pfad> [json]: ruft die Test-LCM auf; Antwort in $BODY,
# Rueckgabewert 0 bei 2xx. LCM_CURL_OPTS bewusst ohne Anfuehrungszeichen.
api() {
	_method=$1 _path=$2
	# shellcheck disable=SC2086
	_code=$(curl -sS ${LCM_CURL_OPTS:-} -o "$BODY" -w '%{http_code}' -X "$_method" \
		-H "X-API-Key: $LCM_API_KEY" -H 'Content-Type: application/json' \
		${3:+--data "$3"} "$LCM_URL/api/v1$_path") || return 1
	case $_code in 2??) return 0 ;; *) return 1 ;; esac
}

# json_value <schluessel>: erster Wert des Schluessels in $BODY (Zahl, Text, true/false).
json_value() {
	grep -o "\"$1\" *: *\(\"[^\"]*\"\|[^,}]*\)" "$BODY" | head -n 1 | sed 's/^[^:]*: *//; s/^"//; s/"$//'
}

# wait_until <sekunden> <befehl...>: wiederholt den Befehl alle 2 s.
wait_until() {
	_left=$1
	shift
	while [ "$_left" -gt 0 ]; do
		"$@" && return 0
		sleep 2
		_left=$((_left - 2))
	done
	return 1
}

# agent_connected <id>: LCM sieht den Agent verbunden.
agent_connected() {
	api GET "/servers/$1" && [ "$(json_value agent_connected)" = true ]
}

# agent_disconnected <id>: LCM sieht den Agent nicht mehr verbunden.
agent_disconnected() {
	api GET "/servers/$1" && [ "$(json_value agent_connected)" = false ]
}

# job_idle <id>: auf dem Server laeuft kein Auftrag mehr.
job_idle() {
	api GET "/servers/$1/active-job" && grep -q '"job": *null' "$BODY"
}

# ct_ready <ct>: der Container nimmt Befehle an und hat ein Netz.
ct_ready() {
	pve "pct exec $1 -- sh -c 'ip -4 route | grep -q default'" >/dev/null 2>&1
}

# mark <zelle> <ok|FEHLER>: haengt das Ergebnis an die Zeile des Containers.
mark() {
	CELLS="$CELLS|$1"
	[ "$1" = ok ] || FAILED=1
}

# step <befehl...>: fuehrt den Befehl aus und merkt ok oder FEHLER.
step() {
	if "$@" >/dev/null 2>&1; then
		mark ok
		return 0
	fi
	mark FEHLER
	return 1
}

install_package() {
	pve "cat > $REMOTE_DEB" <"$DEB" &&
		pve "pct push $1 $REMOTE_DEB /root/lcm-agent.deb" &&
		pve "pct exec $1 -- sh -c 'DEBIAN_FRONTEND=noninteractive apt-get install -y /root/lcm-agent.deb'"
}

enroll_agent() {
	api POST /servers/agent "{\"name\":\"matrix-ct$1-$STAMP\"}" || return 1
	SERVER_ID=$(json_value id)
	_token=$(json_value token)
	[ -n "$SERVER_ID" ] && [ -n "$_token" ] || return 1
	pve "pct exec $1 -- lcm-agent enroll '$AGENT_URL' '$_token'"
}

service_active() {
	pve "pct exec $1 -- systemctl is-active --quiet lcm-agent"
}

# version_matches <ct>: der laufende Agent traegt die Version des Pakets.
version_matches() {
	_pkg=$(pve "pct exec $1 -- dpkg-query -W -f='\${Version}' lcm-agent") || return 1
	pve "pct exec $1 -- lcm-agent version" | grep -qF "$_pkg"
}

# command_roundtrip <id>: ein Auftrag der Test-LCM laeuft ueber den Agent bis zum Ende.
command_roundtrip() {
	wait_until 60 job_idle "$1" || return 1
	api POST "/servers/$1/refresh-hardware" || return 1
	sleep 2
	wait_until 120 job_idle "$1"
}

# reconnects <ct> <id>: nach einem Neustart des Dienstes ist der Agent wieder da.
reconnects() {
	pve "pct exec $1 -- systemctl restart lcm-agent" || return 1
	wait_until 10 agent_disconnected "$2" || true
	wait_until 120 agent_connected "$2"
}

run_container() {
	_ct=$1
	CELLS=""
	SERVER_ID=""
	echo "== CT $_ct: Snapshot $SNAPSHOT" >&2
	pve "pct stop $_ct" >/dev/null 2>&1
	if ! pve "pct rollback $_ct $SNAPSHOT" >&2 || ! pve "pct start $_ct" >&2 || ! wait_until 90 ct_ready "$_ct"; then
		echo "$_ct|?|FEHLER|-|-|-|-|-|-" >>"$RESULTS"
		FAILED=1
		return
	fi
	sleep 5
	_name=$(pve "pct exec $_ct -- sh -c '. /etc/os-release && echo \$PRETTY_NAME'" 2>/dev/null | tr '|' ' ')
	CELLS="$_ct|${_name:-?}"
	if ! step install_package "$_ct"; then
		CELLS="$CELLS|-|-|-|-|-|-" # Einrichtung und alle Pruefungen entfallen
	elif ! step enroll_agent "$_ct"; then
		CELLS="$CELLS|-|-|-|-|-" # alle Pruefungen entfallen
	else
		step service_active "$_ct"
		step version_matches "$_ct"
		step wait_until 90 agent_connected "$SERVER_ID"
		step command_roundtrip "$SERVER_ID"
		step reconnects "$_ct" "$SERVER_ID"
	fi
	echo "$CELLS" >>"$RESULTS"
}

for ct_id in $CTS; do
	run_container "$ct_id"
done
pve "rm -f $REMOTE_DEB" >/dev/null 2>&1

echo
echo "CT|Distribution|Installation|Einrichtung|Dienst|Version|Online|Auftrag|Neustart" >"$RESULTS.table"
cat "$RESULTS" >>"$RESULTS.table"
# column ist nicht ueberall da; printf mit festen Breiten genuegt.
while IFS='|' read -r c d i e s v o a n; do
	printf '%-5s %-32s %-12s %-12s %-7s %-8s %-7s %-8s %s\n' "$c" "$d" "$i" "$e" "$s" "$v" "$o" "$a" "$n"
done <"$RESULTS.table"

if [ "$FAILED" -ne 0 ]; then
	echo
	echo "Mindestens eine Zelle ist FEHLER: nicht nach community releasen." >&2
	exit 1
fi
echo
echo "Alle Zellen ok."
