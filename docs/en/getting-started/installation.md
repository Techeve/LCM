---
sidebar:
  order: 2
title: Installation
description: Install LCM as a binary, as a Debian/Ubuntu package or with Docker.
---

LCM is a single binary with an embedded frontend. There are three ways to run
it. For production, prefer the **`.deb` package** (systemd service) or
**Docker**.

## Prerequisites

- A host for LCM: **Debian 12/13 or Ubuntu 22.04/24.04** (amd64 or arm64).
  Other Linux distributions generally work but are not part of our testing.
- SSH access (password or key) to the servers you want to manage - LCM creates
  a dedicated service user there during onboarding.
- Optionally **Trivy** on the LCM host for the CVE scan. Without it, the
  feature disables itself cleanly. **bubblewrap** is installed alongside it -
  the sandbox LCM runs the scanner in (see
  [CVE scan](/en/guides/security-cve#the-scanner-runs-sandboxed)).

:::caution[Windows and macOS are not supported]
The binary can be cross-compiled for Windows and macOS, but we neither ship
nor test it there. Core functionality requires Linux - the CVE scan sandbox,
the systemd integration and the LCM host setup. For a workstation, the Docker
route (option 2) is the right one.
:::

## Option 1: Debian/Ubuntu package (recommended)

**Easiest via the TechEve APT repository** - set it up once, then install and
keep it current with `apt upgrade`:

```sh
# 1. Set up the repository (including the signing key)
# 0. Prerequisites - minimal/cloud images do not ship curl
sudo apt-get install -y curl ca-certificates

curl -fsSL https://repo.techeve.de/setup.sh | sudo sh

# 2. Install LCM
sudo apt install lcm

# 3. Update later (together with the rest of the system)
sudo apt update && sudo apt upgrade
```

`setup.sh` adds the package source and GPG key (Debian/Ubuntu, amd64 & arm64);
`lcm` is then a normal apt package, so updates arrive automatically with the
system.

**Alternatively, without the repository** - download a single package from the
[releases](https://gitlab.techeve.de/techeve/lcm-ce/-/releases)
(`lcm_<version>_amd64.deb` or `..._arm64.deb`; check the architecture with
`dpkg --print-architecture`) and install it:

```sh
sudo apt install ./lcm_<version>_amd64.deb
```

Both paths set LCM up as an unprivileged `systemd` service (autostart, HTTPS).

### Updating from the interface

When a newer version is available in the configured package channel, a banner
appears at the top with an **Update now** button. LCM then installs its own
package. Three things matter here:

- **A backup is taken first.** Before installing, LCM creates a system backup -
  and if that fails, it does **not** update. Whoever updates their own
  management system has no second one to help them if it goes wrong. The banner
  names the file created; without a stored backup passphrase the run aborts
  with exactly that reason.
- **Running jobs are waited out.** The banner names what it is waiting for;
  the update starts only once no job is running. After 30 minutes the wait is
  cancelled with a message.
- **LCM restarts in the process.** The apt run lives in its own systemd unit
  (`lcm-self-update`) and therefore survives the restart; its log is in
  `journalctl -u lcm-self-update`. The interface notices the version change
  on its own and reloads.

The button only shows where it can do something: a Debian-package install
whose host is registered as an apt server. Otherwise the banner states the
reason. In a container you replace the image instead.

The info dialog (click the copyright notice at the bottom) also offers
**Check now**: it queries the package channel right away and shows the result
in the banner - including when LCM is already up to date.

### Installing Trivy for the CVE scan

The CVE scan requires [Trivy](https://trivy.dev). It is in **no** standard
package source of Debian or Ubuntu and must therefore be set up from the
vendor's repository - LCM runs fine without Trivy, but the CVE scan stays
disabled:

```sh
# Prerequisite - gnupg is missing on minimal/cloud images
sudo apt-get install -y gnupg

wget -qO- https://get.trivy.dev/deb/public.key \
  | sudo gpg --dearmor -o /usr/share/keyrings/trivy.gpg
echo "deb [signed-by=/usr/share/keyrings/trivy.gpg] https://get.trivy.dev/deb generic main" \
  | sudo tee /etc/apt/sources.list.d/trivy.list
sudo apt update && sudo apt install trivy
```

If LCM runs on the same host you can also do it with one click: *server detail
of the LCM host → set up Trivy*.

| Path | Contents |
|------|----------|
| `/usr/bin/lcm` | the program (binary with embedded web UI) |
| `/lib/systemd/system/lcm.service` | the hardened systemd unit |
| `/etc/lcm/config.json` | configuration (created with a random JWT secret) |
| `/var/lib/lcm/` | state: encrypted DB, master key, TLS certificate, backups |
| `/var/lib/lcm/logs/lcm.log` | persistent, rotating log file (see below) |

The service runs as its own system user **`lcm`** without root privileges.

```sh
systemctl status lcm      # status
journalctl -u lcm -f      # live logs
```

### Persistent log file & service monitoring

In addition to `stdout` (journald/Docker), LCM writes a **persistent log file** at
`<data-dir>/logs/lcm.log` (package: `/var/lib/lcm/logs/lcm.log`; configurable via
`log_file` in `config.json`). It **rotates** automatically (at 10 MB, up to 7
compressed backups, max 7 days) so you can review restarts, crashes and actions
after the fact:

```sh
grep 'LCM-Dienst' /var/lib/lcm/logs/lcm.log   # every start/stop
```

- **`=== LCM-Dienst gestartet ===`** - on EVERY (re)start, with version, build and **PID**.
- **`=== LCM-Dienst wird beendet ===`** - only on a clean stop (signal). A start **without**
  a following stop line means a **crash / hard kill** - that's how you spot unplanned restarts.
- Actions such as **backups** (`system-backup erstellt`), CVE/Docker scans etc. are logged too.

## Option 2: Docker / Docker Compose

### Prebuilt image from Docker Hub (recommended)

Releases are published as multi-arch images (amd64/arm64) on Docker Hub:
[`techeve/lcm`](https://hub.docker.com/r/techeve/lcm) - `:latest` is the
current stable release, `:beta` the pre-release, plus every version as its
own tag. The CVE scanner sidecar is
[`techeve/lcm-trivyd`](https://hub.docker.com/r/techeve/lcm-trivyd).

```sh
mkdir -p data && sudo chown 1000 data   # the container writes as UID 1000
docker run -d --name lcm \
  -p 9310:9310 -v "$PWD/data:/data" \
  --read-only --tmpfs /tmp --cap-drop ALL \
  --restart unless-stopped \
  techeve/lcm:latest
docker logs -f lcm     # first start: the generated admin password appears here
```

With Docker Compose: take the bundled
[`docker/docker-compose.yml`](https://gitlab.techeve.de/techeve/lcm-ce/-/blob/community/docker/docker-compose.yml),
remove the `build:` block and set `image: techeve/lcm:latest` - all
hardening flags stay in place.

### Build it yourself

```sh
make docker-build          # build Linux binary (incl. audits) + create image
docker compose up -d
docker compose logs -f     # first start: the generated admin password appears here
```

On first start, the host folder `./data` gets the configuration, the SQLite
database and `version.json`. The runtime image is minimally hardened (Alpine,
non-root, `read-only`, `cap_drop: ALL`). The container speaks HTTPS with a
self-signed certificate by default - put a reverse proxy with a real
certificate in front for public deployments.

Details and all hardening flags: [Docker operation](/en/guides/docker/) and
[Packaging](/en/reference/packaging/).

## Option 3: Build from source

```sh
make build     # npm audit → vite build → govulncheck → go build
./bin/lcm      # creates config.json, lcm.key + DB on first start
```

The first start prints the initial admin password **once** to the console.

### Demo mode

To try things out safely with example servers and simulated data:

```sh
./bin/lcm --demo
```

Demo mode can only be enabled via this flag (it is not a config.json field) and
only takes effect when seeding a fresh database. A regular installation starts
empty.

## Command-line options

The binary knows only a few flags - everything else lives in `config.json`:

| Flag | Effect |
|------|--------|
| `--data <dir>` | Data directory for `config.json`, `app.db`, `lcm.key` and `version.json`. Default: the binary's directory; in a container typically `/data`. |
| `--config <path>` | Path to `config.json` (default: inside the data directory). |
| `--demo` | Seed test data (servers, packages, job histories) on the first seeding of a fresh DB. |
| `--dev` | Development mode: allows **plain HTTP** (otherwise always HTTPS). |
| `--debug` | Raises the log level to `debug` at runtime without changing `config.json`. |
| `--version` | Print the version and exit. |

:::caution[The data flag is `--data`]
Not `--data-dir`. Example for container operation with a mounted volume:

```sh
./lcm --data /data
```
:::

There is also a subcommand for **master-key rotation** (see
[Security model](/en/reference/security-model/)):

```sh
./lcm rotate-db-key      # generate a new master key, re-encrypt all fields
```

## Environment variables

Handy in container/service operation to override values without touching the
(possibly read-only mounted) `config.json`:

| Variable | Effect |
|----------|--------|
| `LCM_DATA` | Data directory (same as `--data`). |
| `LCM_HOST` | Bind address of the web UI / REST API (overrides `host`). |
| `LCM_PORT` | Port of the web UI / REST API (overrides `port`). |
| `LCM_AGENT_HOST` | Bind address of the agent listener (overrides `agent_host`). |
| `LCM_AGENT_PORT` | Port of the agent listener (overrides `agent_port`); `0` disables it. |
| `LCM_BACKUP_PASSPHRASE` | Passphrase for **automatic** backups (see [Backups](/en/guides/backups/)). |
| `LCM_RESTORE_AUTO_RESTART` | `1`/`true` = restart automatically after a staged restore. |
| `TZ` | Time zone, e.g. `Europe/Berlin` - tzdata is embedded in the binary, so it works even in minimal containers. |

```sh
LCM_HOST=0.0.0.0 LCM_PORT=443 ./lcm            # UI/REST on all interfaces, port 443
LCM_AGENT_PORT=0 ./lcm                          # disable LCM Remote (agent listener)
```

## Network ports

LCM binds up to **three** separate listeners - deliberately on their own ports:

| Port (default) | Listener | Bind (default) | Protocol |
|---|---|---|---|
| `9310` | **Web UI + REST API** (`host`/`port`) | `127.0.0.1` | HTTPS (self-signed; `--dev` = HTTP) |
| `9320` | **Agent listener** - LCM Remote, `/mqtt` only (`agent_host`/`agent_port`); `agent_port: 0` disables it | `0.0.0.0` | HTTPS (same certificate as the UI) |
| `9330` | **MCP listener** - optional, **off** by default; toggle under *Settings → MCP* | `127.0.0.1` | HTTP |

The agent port carries **only** the agent interface, the UI/REST port carries
**none** - and vice versa. Details: [LCM Remote](/en/guides/remote/) and
[MCP interface](/en/guides/mcp/).

:::note[Every restart ends all sessions]
The JWT signing material is re-bound on every start to a random, RAM-only
instance nonce. As a result, after a (re)start **all** previously issued tokens
are invalid - everyone has to log in again. This holds even with an unchanged
`jwt_secret` and covers, among others, rebuilds, process restarts and a fresh
database seeding.
:::

## First login

The initial admin password is in the console/journal output of the first start:

```sh
journalctl -u lcm | grep -A3 'Admin-Zugang'   # for the .deb install
```

Then open `https://<host>:9310` in the browser (self-signed certificate - the
browser warning is expected), log in as `admin` and change the password.

Continue with the [Quickstart](/en/getting-started/quickstart/).
