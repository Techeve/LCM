#!/bin/bash
# publish-pkgs-test - prüft packaging/publish-pkgs.sh gegen einen Attrappen-Server:
# Upload (201, 409, Fehler), Gegenprobe im veröffentlichten Index von rpm, apk und
# Arch und der Fehlschlag bei falscher Version.
#
#   bash packaging/publish-pkgs-test.sh
#
# Braucht bash, python3, curl, tar, gzip. Die Attrappe spielt RSM nach: Sie nimmt
# POST /pkg/v1/<Format>/<Kanal> an (der Rumpf der Test-Pakete ist die Zeile
# "<Name> <Version> <Arch>", das echte RSM liest das aus dem Paket) und liefert
# danach die Indizes aus, die RSM bauen würde - je nach Modus mit einer anderen
# Version oder gar nicht.
set -euo pipefail

DIR=$(cd "$(dirname "$0")" && pwd)
WORK=$(mktemp -d)
trap 'kill "${SERVER_PID:-0}" 2>/dev/null || true; rm -rf "$WORK"' EXIT
FAILS=0

cat >"$WORK/fake.py" <<'PY'
import base64, gzip, http.server, io, os, sys, tarfile, time

mode = sys.argv[1]          # ok | stale | wrong | conflict | forbidden | broken
store = {}                  # (format, channel) -> [(version, arch)]

def tar_gz(files):
    buf = io.BytesIO()
    with tarfile.open(fileobj=buf, mode="w:gz") as tar:
        for name, data in files.items():
            info = tarfile.TarInfo(name); info.size = len(data); info.mtime = time.time()
            tar.addfile(info, io.BytesIO(data))
    return buf.getvalue()

def index(fmt, channel, arch, shift):
    versions = [v for v, a in store.get((fmt, channel), []) if a == arch]
    if shift:
        versions = [v + "9" for v in versions]
    if fmt == "rpm":
        pkgs = "".join(
            '<package type="rpm"><name>lcm-agent</name><arch>%s</arch>\n<version epoch="0" ver="%s" rel="1"/></package>\n' % (arch, v)
            for v in versions)
        return "primary", gzip.compress(('<metadata>' + pkgs + '</metadata>').encode())
    if fmt == "apk":
        text = "".join("C:Q1xx\nP:lcm-agent\nV:%s-r0\nA:%s\n\n" % (v, arch) for v in versions)
        return "apk", tar_gz({".SIGN.RSA.techeve": b"sig", "APKINDEX": text.encode()})
    files = {}
    for v in versions:
        rev = "lcm-agent-%s-1" % v
        files[rev + "/desc"] = ("%%NAME%%\nlcm-agent\n\n%%VERSION%%\n%s-1\n" % v).encode()
    return "arch", tar_gz(files)

class H(http.server.BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def send(self, code, body=b"", ctype="application/octet-stream"):
        self.send_response(code); self.send_header("Content-Length", str(len(body)))
        self.send_header("Content-Type", ctype); self.end_headers(); self.wfile.write(body)
    def do_POST(self):
        n = int(self.headers.get("Content-Length", 0)); body = self.rfile.read(n).decode().split()
        parts = self.path.strip("/").split("/")           # pkg v1 <format> <channel>
        auth = self.headers.get("Authorization")
        if auth is None: return self.send(401)
        # EXPECT_AUTH "Benutzer:Passwort": Die Zugangsdaten müssen unverändert ankommen.
        expect = os.environ.get("EXPECT_AUTH")
        if expect is not None and base64.b64decode(auth.split()[-1]).decode() != expect: return self.send(401)
        if mode == "forbidden": return self.send(403, b'{"error":"kein Recht"}')
        if mode == "broken": return self.send(500, b"kaputt")
        fmt, channel = parts[2], parts[3]
        name, version, arch = body
        have = store.setdefault((fmt, channel), [])
        known = (version, arch) in have
        if not known:
            have.append((version, arch))
        if mode == "conflict" or known:
            return self.send(409, b'{"error":"liegt schon"}')
        self.send(201, ('{"name":"%s","version":"%s","arch":"%s","path":"x"}' % (name, version, arch)).encode())
    def do_GET(self):
        parts = self.path.strip("/").split("/")           # <format> <channel> <arch> ...
        fmt, channel, arch = parts[0], parts[1], parts[2]
        if mode == "stale": return self.send(404)
        shift = mode == "wrong"
        if fmt == "rpm" and parts[3:] == ["repodata", "repomd.xml"]:
            return self.send(200, b'<repomd><data type="primary"><location href="repodata/aa-primary.xml.gz"/></data></repomd>')
        if fmt == "rpm" and parts[3:] == ["repodata", "aa-primary.xml.gz"]:
            return self.send(200, index("rpm", channel, arch, shift)[1])
        if fmt == "apk" and parts[3:] == ["APKINDEX.tar.gz"]:
            return self.send(200, index("apk", channel, arch, shift)[1])
        if fmt == "arch" and parts[3:] == ["techeve.db"]:
            return self.send(200, index("arch", channel, arch, shift)[1])
        self.send(404)

srv = http.server.ThreadingHTTPServer(("127.0.0.1", 0), H)
print(srv.server_address[1], flush=True)
srv.serve_forever()
PY

# fake_pkgs <Version> <rpm-Version> <apk-Version> <arch-Version>: Test-Pakete mit den
# Dateinamen, die nfpm vergibt; im Rumpf steht, was der Server "aus dem Paket liest".
fake_pkgs() {
  rm -rf "${WORK:?}/bin"
  mkdir -p "$WORK/bin"
  for arch in x86_64 aarch64; do
    echo "lcm-agent $2 $arch" >"$WORK/bin/lcm-agent-$2-1.$arch.rpm"
    echo "lcm-agent $3 $arch" >"$WORK/bin/lcm-agent_$3_$arch.apk"
    echo "lcm-agent $4 $arch" >"$WORK/bin/lcm-agent-$4-1-$arch.pkg.tar.zst"
  done
  # Das Binary und andere Dateien in bin/ gehören nicht dazu.
  : >"$WORK/bin/lcm-agent-linux-amd64"
  : >"$WORK/bin/lcm_2.4.0_amd64.deb"
}

# start_server <Modus>
start_server() {
  if [ -n "${SERVER_PID:-}" ]; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  : >"$WORK/port"
  python3 "$WORK/fake.py" "$1" >"$WORK/port" &
  SERVER_PID=$!
  disown "$SERVER_PID" 2>/dev/null || true
  for _ in $(seq 50); do [ -s "$WORK/port" ] && break; sleep 0.1; done
  PORT=$(cat "$WORK/port")
}

# run <Modus> <Kanal> [Env=Wert...]: Skript gegen die Attrappe; Ausgabe in $WORK/out.
run() {
  local mode=$1 channel=$2
  shift 2
  start_server "$mode"
  env REPO_URL="http://127.0.0.1:$PORT" REPO_PUBLIC_URL="http://127.0.0.1:$PORT" REPO_USER=u REPO_PASS=p \
    CHANNEL="$channel" PKG_DIR="$WORK/bin" CHECK_TRIES=2 CHECK_DELAY=0 "$@" \
    bash "$DIR/publish-pkgs.sh" >"$WORK/out" 2>&1
}

expect_ok() {
  if run "$@"; then echo "ok   $1 $2"; else echo "FAIL $1 $2 (erwartet Erfolg)"; sed 's/^/     /' "$WORK/out"; FAILS=$((FAILS + 1)); fi
}
expect_fail() { # <Text> <Modus> <Kanal> ...
  local text=$1
  shift
  if run "$@"; then
    echo "FAIL $1 $2 (erwartet Fehlschlag)"; sed 's/^/     /' "$WORK/out"; FAILS=$((FAILS + 1))
  elif grep -q "$text" "$WORK/out"; then
    echo "ok   $1 $2 scheitert mit '$text'"
  else
    echo "FAIL $1 $2: Ausgabe nennt '$text' nicht"; sed 's/^/     /' "$WORK/out"; FAILS=$((FAILS + 1))
  fi
}
expect_out() { # <Text>
  if grep -q "$1" "$WORK/out"; then echo "ok   Ausgabe nennt '$1'"; else echo "FAIL Ausgabe nennt '$1' nicht"; sed 's/^/     /' "$WORK/out"; FAILS=$((FAILS + 1)); fi
}

# Release (2.3.0) und Beta (2.4.0-beta.1: rpm ~beta.1, apk _beta1, Arch beta.1).
fake_pkgs 2.3.0 2.3.0 2.3.0 2.3.0
expect_ok ok community
expect_out "lcm-agent-2.3.0-1.x86_64.rpm: hochgeladen"
expect_out "rpm/community/aarch64: lcm-agent 2.3.0 bestätigt"
expect_out "apk/community/x86_64: lcm-agent 2.3.0 bestätigt"
expect_out "arch/community/aarch64: lcm-agent 2.3.0 bestätigt"
fake_pkgs 2.4.0-beta.1 '2.4.0~beta.1' 2.4.0_beta1 2.4.0beta.1
expect_ok ok beta
expect_out "apk/beta/x86_64: lcm-agent 2.4.0_beta1 bestätigt"
expect_out "arch/beta/x86_64: lcm-agent 2.4.0beta.1 bestätigt"
expect_out "rpm/beta/x86_64: lcm-agent 2.4.0~beta.1 bestätigt"

# Zugangsdaten mit Anführungszeichen und Backslash kommen unverändert beim Server an.
EXPECT_AUTH='u:pa"ss\wo\\rd"' expect_ok ok community REPO_PASS='pa"ss\wo\\rd"'
EXPECT_AUTH='u:p' expect_ok ok community

# 409 heißt: liegt schon - kein Fehler, die Gegenprobe läuft trotzdem.
expect_ok conflict community
expect_out "liegt schon (409)"

# Fehlschläge: Der Index nennt die Version nicht (der Server baut sie nicht ein, oder
# eine andere), der Upload wird abgelehnt oder scheitert, der Kanal ist falsch.
expect_fail "steht NICHT im veröffentlichten Index" stale community
expect_fail "steht NICHT im veröffentlichten Index" wrong community
expect_fail "HTTP 403" forbidden community
expect_fail "HTTP 500" broken beta
expect_fail "nicht community oder beta" ok enterprise
expect_fail "Keine rpm-, apk- oder Arch-Pakete" ok community PKG_DIR="$WORK/leer"

if [ "$FAILS" -gt 0 ]; then
  echo "$FAILS Fall/Fälle fehlgeschlagen"
  exit 1
fi
echo "all publish cases passed"
