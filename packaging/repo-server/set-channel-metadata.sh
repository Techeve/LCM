#!/bin/sh
# set-channel-metadata.sh - give each package channel its own Origin/Label.
#
# Why: all three channels live on one host and use the same suite name, so apt
# cannot tell them apart. Without a distinguishing mark the only way to keep an
# enterprise machine off the free channel is to switch the community source off
# entirely - which also takes away every OTHER package on that source. With an
# Origin/Label per channel a machine can pin just the LCM packages to its
# channel and keep using the rest:
#
#   Package: lcm lcm-agent
#   Pin: release l=techeve-community
#   Pin-Priority: -1
#
# aptly stores Origin and Label with the PUBLISH POINT, and they can only be
# set when it is created. Changing them therefore means dropping the publish
# and creating it again from the same sources. The packages themselves live in
# the aptly repo, not in the publish, so nothing is lost - but the channel is
# unreachable for the second or two in between, and clients running `apt
# update` in exactly that moment see a 404 and retry later.
#
# Run this ONCE per channel, on or next to the repository server. Afterwards
# the CI keeps Origin/Label: `publish update` (PUT) carries them over.
#
# Required environment:
#   REPO_URL   base URL of the aptly API, e.g. https://repo.techeve.de
#   REPO_USER  API user (NOT a customer subscription key)
#   REPO_PASS  API password
# Optional:
#   ORIGIN     Origin field for every channel (default: TechEve)
#   GPG_KEY    signing key                    (default: repo@techeve.de)
#
# Usage:
#   set-channel-metadata.sh              show what is set and what would change
#   set-channel-metadata.sh apply        apply it
#   set-channel-metadata.sh apply beta   only that channel (community|beta|enterprise)
#
# All output is plain ASCII on purpose - it ends up in terminals and logs whose
# encoding we do not control.
set -eu

: "${REPO_URL:?REPO_URL required}"
: "${REPO_USER:?REPO_USER required}"
: "${REPO_PASS:?REPO_PASS required}"
ORIGIN="${ORIGIN:-TechEve}"
GPG_KEY="${GPG_KEY:-repo@techeve.de}"

# channel definitions: <name> <publish-prefix> <suite> <label>
# The prefix ":." is aptly's spelling for the root of the public tree; in the
# publish listing the same point is reported as "." - publish_info maps both.
CHANNELS="community :. stable techeve-community
beta :. beta techeve-beta
enterprise enterprise stable techeve-enterprise"

api() {
	_method="$1"
	_path="$2"
	shift 2
	curl -fsS --retry 3 --retry-delay 2 -u "${REPO_USER}:${REPO_PASS}" \
		-X "$_method" "${REPO_URL}${_path}" "$@"
}

die() {
	echo "ERROR: $*" >&2
	exit 1
}

command -v python3 > /dev/null 2>&1 || die "python3 is required (JSON handling)."

# publish_info <prefix> <suite> prints "<origin>|<label>|<sourcekind>|<sources-json>"
# for that publish point, or nothing when it does not exist.
publish_info() {
	api GET /api/publish | python3 -c '
import json, sys
prefix, dist = sys.argv[1], sys.argv[2]
want = "" if prefix == ":." else prefix
try:
    published = json.load(sys.stdin)
except ValueError:
    # No JSON on stdin means curl already failed and said why - do not bury
    # that message under a Python traceback.
    sys.exit(1)
for p in published:
    have = p.get("Prefix") or ""
    if have == ".":
        have = ""
    if have == want and p.get("Distribution") == dist:
        print("|".join([
            p.get("Origin") or "",
            p.get("Label") or "",
            p.get("SourceKind") or "",
            json.dumps(p.get("Sources") or []),
        ]))
        break
' "$1" "$2"
}

field() { # field <n> <a|b|c|d>
	echo "$2" | cut -d'|' -f"$1"
}

handle() { # handle <name> <prefix> <suite> <label> <apply?>
	_name="$1"
	_prefix="$2"
	_dist="$3"
	_label="$4"
	_apply="$5"

	_info=$(publish_info "$_prefix" "$_dist") || die "cannot reach the aptly API at ${REPO_URL}."
	[ -n "$_info" ] || die "no publish point ${_prefix}/${_dist} - is the channel set up? (see README.md)"

	_origin_now=$(field 1 "$_info")
	_label_now=$(field 2 "$_info")
	_kind=$(field 3 "$_info")
	# Sources may contain the separator, so take everything after the third one.
	_sources=$(echo "$_info" | cut -d'|' -f4-)

	printf '%-11s %s/%s: Origin=%s Label=%s\n' \
		"$_name" "$_prefix" "$_dist" "${_origin_now:-<none>}" "${_label_now:-<none>}"

	if [ "$_origin_now" = "$ORIGIN" ] && [ "$_label_now" = "$_label" ]; then
		echo "            already set - nothing to do"
		return 0
	fi
	if [ "$_apply" != "apply" ]; then
		echo "            would become Origin=${ORIGIN} Label=${_label}"
		return 0
	fi

	echo "            re-publishing with Origin=${ORIGIN} Label=${_label} ..."
	api DELETE "/api/publish/${_prefix}/${_dist}" > /dev/null
	# From here the channel is down until the POST succeeds - keep it short and
	# say so loudly if it fails, so nobody walks away from a dead channel.
	if ! api POST "/api/publish/${_prefix}" \
		-H "Content-Type: application/json" \
		--data "{\"SourceKind\":\"${_kind}\",\"Sources\":${_sources},\"Distribution\":\"${_dist}\",\"Origin\":\"${ORIGIN}\",\"Label\":\"${_label}\",\"Signing\":{\"Batch\":true,\"GpgKey\":\"${GPG_KEY}\"}}" \
		> /dev/null; then
		echo "ERROR: re-publishing ${_prefix}/${_dist} FAILED - the channel is currently NOT published." >&2
		echo "       Publish it again by hand from repo '${_kind}' sources: ${_sources}" >&2
		exit 1
	fi

	_after=$(publish_info "$_prefix" "$_dist")
	echo "            now: Origin=$(field 1 "$_after") Label=$(field 2 "$_after")"
}

MODE="${1:-show}"
case "$MODE" in
show | apply) ;;
help | -h | --help)
	sed -n '33,36p' "$0" | sed 's/^# \{0,1\}//'
	exit 0
	;;
*) die "unknown argument: $MODE (try 'show', 'apply' or 'help')" ;;
esac
ONLY="${2:-}"

# Deliberately a here-doc instead of "echo | while": a pipe would run the loop
# in a subshell, so an abort (die) would only end the loop - the rest would keep
# going and the script would exit successfully although a channel was not set.
while read -r name prefix dist label; do
	[ -n "$name" ] || continue
	if [ -n "$ONLY" ] && [ "$ONLY" != "$name" ]; then
		continue
	fi
	handle "$name" "$prefix" "$dist" "$label" "$MODE"
done << EOF
$CHANNELS
EOF

if [ "$MODE" = show ]; then
	echo
	echo "Nothing was changed. Re-run with 'apply' to set it."
fi
