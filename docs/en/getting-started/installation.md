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

- A host for LCM: **Debian 12/13 or Ubuntu 22.04/24.04/26.04** (amd64 or
  arm64), or Docker on any Linux system. What is tested is listed under
  [Supported systems](/en/reference/supported-systems/).
- The servers you want to manage need either SSH access (password or key) -
  LCM creates a dedicated service user there during onboarding - or the
  `lcm-agent`, which connects outbound to LCM. You can manage Debian, Ubuntu,
  the RHEL family, openSUSE, Arch Linux and Alpine.
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

**Easiest via the Techeve APT repository** - set it up once, then install and
keep it current with `apt upgrade`:

```sh
# 0. Prerequisites - minimal/cloud images do not ship curl
sudo apt-get install -y curl ca-certificates

# 1. Set up the repository (including the signing key)
curl -fsSL https://repo.techeve.de/setup.sh | sudo sh

# 2. Install LCM
sudo apt install lcm

# 3. Update later (together with the rest of the system)
sudo apt update && sudo apt upgrade
```

`setup.sh` detects the system's package manager (apt, dnf, zypper, apk or
pacman) and sets up the package source and signing key (amd64 and arm64). On
the LCM host that is apt; `lcm` is then a normal apt package, and updates
arrive automatically with the system. On managed servers the same script sets
up the source for the `lcm-agent` (see [Quickstart](/en/getting-started/quickstart/)).
`--beta` selects the beta channel, `--uninstall` removes source and key again.

The `lcm` package exists for apt only. On the other systems LCM runs in a
Docker container (option 2).

The source points at the community channel (`https://repo.techeve.de community main`).
Older installations using the suite `stable` keep working, and the package moves
them to `community` on update. You pick the other channels (beta, enterprise) in
LCM under Settings, Subscription; see [Package channels](/en/reference/repo-channels/).

**Alternatively, without the repository** - download a single package from the
[releases](https://gitlab.techeve.de/techeve/lcm/-/releases?mtm_campaign=linking&mtm_kwd=doc)
(`lcm_<version>_amd64.deb` or `..._arm64.deb`; check the architecture with
`dpkg --print-architecture`) and install it:

```sh
sudo apt install ./lcm_<version>_amd64.deb
```

Both paths set LCM up as an unprivileged `systemd` service (autostart, HTTPS).

### Fresh install with a new data set

To set LCM up again completely - without the database, keys or backups from
before - remove the old installation with **`purge`**:

```sh
sudo apt purge lcm      # removes everything LCM created
sudo apt install lcm    # fresh installation
```

`purge` deletes the database, the master key (including an encrypted systemd
credential), the backups and the configuration under `/var/lib/lcm` and
`/etc/lcm`, the management account `lcm-svc` on this machine, and the host
settings LCM applied there (sshd include and drop-ins, apt cache, nftables
table). The original `sshd_config` is restored.

:::caution
After `purge`, the database, keys and backups are **gone for good**. Without the
master key the stored credentials of the managed servers cannot be read again.
Back up `/var/lib/lcm` first if you still need the data.
:::

By contrast, `apt remove lcm` keeps everything. After a reinstall, LCM picks up
where it stopped.

Not removed: Linux users that LCM distributed to other servers (including their
home directories), ufw rules, and programs such as Trivy, bubblewrap and
apt-cacher-ng.

### Updating from the interface

When a newer version is available in the configured package channel, a banner
appears at the top with an **Install now** button. LCM then installs its own
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

The `lcm-agent` packages on managed servers are not part of this: LCM updates
them on instruction, by button or group rule
([Update the agent](/en/guides/remote/#update-the-agent)).

The *About LCM* dialog (account menu at the top right) also offers
**Check now**: it queries the package channel right away and shows the result
right next to the button - including when LCM is already up to date.

**New in this version.** After an update LCM shows each administrator, once, a
window *New in version …* with what changed. LCM fetches the notes from the
docs server (doc.techeve.de); if that is not reachable, the window stays closed
and appears the next time, as soon as it answers. After *Close* it does not come
back.

### Installing Trivy for the CVE scan

The CVE scan requires [Trivy](https://trivy.dev). It is in **no** standard
package source of Debian or Ubuntu and must therefore be set up from the
vendor's repository - LCM runs fine without Trivy, but the CVE scan stays
disabled:

```sh
# Prerequisites - wget and gnupg are missing on minimal/cloud images;
# bubblewrap is the sandbox LCM starts the scanner in
sudo apt-get install -y wget gnupg ca-certificates bubblewrap

wget -qO- https://get.trivy.dev/deb/public.key \
  | sudo gpg --batch --yes --dearmor -o /usr/share/keyrings/trivy.gpg
echo "deb [signed-by=/usr/share/keyrings/trivy.gpg] https://get.trivy.dev/deb generic main" \
  | sudo tee /etc/apt/sources.list.d/trivy.list
sudo apt update && sudo apt install trivy
```

The steps are the same for Debian and Ubuntu; tested on Debian 12 and 13 and
Ubuntu 22.04, 24.04 and 26.04 (overview: [Supported systems](/en/reference/supported-systems/)). **Debian 11** is no longer supported (out of
vendor support).

If LCM runs on the same host you can also do it with one click: *server detail
of the LCM host → set up Trivy*. If Trivy was set up without bubblewrap,
*Add sandbox* adds it.

| Path | Contents |
|------|----------|
| `/usr/bin/lcm` | the program (binary with embedded web UI) |
| `/lib/systemd/system/lcm.service` | the hardened systemd unit |
| `/etc/lcm/config.json` | configuration (created with safe defaults, no secret) |
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
[`docker/docker-compose.yml`](https://gitlab.techeve.de/techeve/lcm/-/blob/community/docker/docker-compose.yml?mtm_campaign=linking&mtm_kwd=doc),
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
certificate in front for public deployments (examples and what to set in LCM:
[Reverse proxy & external hardening](/en/guides/reverse-proxy/)).

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

The binary knows only a few flags. Start-up and operating parameters live in
`config.json`, the other settings in the database and the interface
(all fields of the file: [Configuration file config.json](/en/guides/config-json/)):

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
(possibly read-only mounted) `config.json`. Who wins when file, interface and
environment disagree is described under
[Configuration file config.json](/en/guides/config-json/#file-interface-and-environment):

| Variable | Effect |
|----------|--------|
| `LCM_DATA` | Data directory (same as `--data`). |
| `LCM_HOST` | Bind address of the web UI / REST API (overrides `host`). |
| `LCM_PORT` | Port of the web UI / REST API (overrides `port`). |
| `LCM_AGENT_HOST` | Bind address of the agent listener (overrides `agent_host`). |
| `LCM_AGENT_PORT` | Port of the agent listener (overrides `agent_port`); `0` disables it. A set environment variable locks the field in the interface. |
| `LCM_TRIVY_URL` | Address of a Trivy sidecar for the CVE scan (overrides `trivy_url`), see [Packaging](/en/reference/packaging/#trivy-in-container-mode). |
| `LCM_TRIVY_TOKEN` | Token for the sidecar (overrides `trivy_token`); required as soon as an address is set. |
| `LCM_ENCRYPTION_KEY` | Master key (base64, 32 bytes). Wins over `lcm.key` and the systemd credential, see [Security model](/en/reference/security-model/). |
| `LCM_BACKUP_PASSPHRASE` | Passphrase for **automatic** backups (see [Backups](/en/guides/backups/)). |
| `LCM_RESTORE_AUTO_RESTART` | `1`/`true` = restart automatically after a staged restore. |
| `TZ` | Time zone, e.g. `Europe/Berlin` - tzdata is embedded in the binary, so it works even in minimal containers. |

```sh
LCM_HOST=0.0.0.0 LCM_PORT=443 ./lcm            # UI/REST on all interfaces, port 443
LCM_AGENT_PORT=0 ./lcm                          # disable LCM Remote (agent listener)
```

The service of the package installation may open ports below 1024 such as
`443`: the service file shipped with LCM grants it the capability
`CAP_NET_BIND_SERVICE` for that, so no drop-in is needed. See
[Certificates, Let's Encrypt & port 443](/en/guides/zertifikate/#freeing-port-80-and-443).

## Network ports

LCM binds up to **three** separate listeners - deliberately on their own ports.
Two **optional** ports come on top (see below):

| Port (default) | Listener | Bind (default) | Protocol |
|---|---|---|---|
| `9310` | **Web UI + REST API** (`host`/`port`) | `127.0.0.1` | HTTPS (LCM certificate, self-signed, or one of your own; `--dev` = HTTP) |
| `9320` | **Agent listener** - LCM Remote, `/mqtt` only (`agent_host`/`agent_port`); the switch for the agent disables it | `0.0.0.0` | HTTPS (certificate by the requested name, otherwise the LCM certificate) |
| `9330` | **MCP listener** - optional, **off** by default; switch under *Settings → General* and *Settings → MCP* (`mcp_port`) | like the web UI | always HTTPS (certificate by the requested name) |
| `443` (freely selectable) | **Shared port** - optional, address mode C (in the interface or via `shared_tls_listen`): web UI, agent and MCP by the requested name | off | HTTPS |
| `80` (`acme_http_listen`) | **Let's Encrypt, HTTP-01** - optional, open only during a check, closed otherwise | off | HTTP |

The agent port carries **only** the agent interface, the UI/REST port carries
**none** - and vice versa. Details: [LCM Remote](/en/guides/remote/) and
[MCP interface](/en/guides/mcp/). You can change the ports under
*Settings → General* ([Public addresses & address mode](/en/guides/adressen/)).
The two optional ports, certificates of your own and Let's Encrypt are described in
[Certificates, Let's Encrypt & port 443](/en/guides/zertifikate/). Which networks
may reach the services is set by the [Allowed networks](/en/guides/netzbereiche/).

:::note[Every restart ends all sessions]
Browser sessions and the signing material of the 2FA challenge live only in
memory; there is no stored secret behind them. As a result, after a (re)start
**all** sessions are invalid - everyone has to log in again.
This covers, among others, rebuilds, process restarts and a fresh database
seeding.
:::

## First login

The initial admin password is in the console/journal output of the first start:

```sh
journalctl -u lcm | grep -A3 'Admin-Zugang'   # for the .deb install
```

Then open `https://<host>:9310` in the browser (self-signed certificate - the
browser warning is expected), log in as `admin` and change the password.

After that LCM asks once **how sign-in should work in the browser**: in the tab's
memory only (default, safest: no cookie, after a reload or in a new tab you sign in
again) or as a session cookie (more convenient, HTTPS only). You can change this at
any time under *Settings → General → Sign-in in the browser*, right next to the
*Session length in minutes*; switching signs all users out. Scripts use an
API key, independent of this.

After the password and two-factor sign-in, the **setup assistant** leads you
through the first settings. It exists on a fresh installation only. It asks in six
steps:

1. **How is LCM reached?** Address mode, addresses and ports. This step cannot be
   skipped.
2. **Which services should run?** Agent and MCP.
3. **Who may reach LCM?** Allowed networks per service.
4. **Certificate:** Let's Encrypt or a reverse proxy in front. Then the assistant
   redirects to the new address; with a new name you sign in again.
5. **Adding new servers:** the onboarding key (can be skipped).
6. **Automatic backup** (can be skipped).

At the end LCM opens the page of the LCM host with an introduction to Trivy,
apt-cacher-ng and CrowdSec LAPI. The steps in detail:
[Public addresses & address mode](/en/guides/adressen/#setup-assistant). Later you
change everything under *Settings → General*.

Continue with the [Quickstart](/en/getting-started/quickstart/).
