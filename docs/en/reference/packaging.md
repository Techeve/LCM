---
sidebar:
  order: 25
title: Packaging & Deployment
description: Run LCM as a native Debian/Ubuntu package (.deb) with a systemd service or as a hardened Docker container.
---

LCM can be run in two ways: as a native **Debian/Ubuntu package (.deb)** that
sets up the application as an unprivileged `systemd` service, or as a **Docker
container**. Both paths deliver exactly the same binary; this page describes
both.

## Debian/Ubuntu package (.deb)

LCM is shipped for **Ubuntu and Debian servers** as an installable `.deb`
package - for **amd64** (x86-64) and **arm64** (aarch64). The package sets up
LCM as an unprivileged `systemd` service: one command, and the service runs,
starts at boot, and serves the web interface over HTTPS.

### Installation

Download the matching package from the [release](https://gitlab.techeve.de/techeve/lcm/-/releases)
(`lcm_<version>_amd64.deb` or `..._arm64.deb`) and install it:

```sh
sudo apt install ./lcm_<version>_amd64.deb
```

`apt` pulls in the dependency (`adduser`) automatically. Afterwards the
service is installed, enabled (autostart), and started.

**`trivy`** is needed for the CVE scan of the package inventory but is
deliberately **not** a package dependency: it is in no standard package source
of Debian/Ubuntu, so `apt` would silently fail to resolve a `Recommends`.
Setting it up is therefore a separate step (see
[Installation](/en/getting-started/installation/)); if Trivy is missing, only
the CVE scan is disabled and everything else runs normally.

Determine the architecture with `dpkg --print-architecture`
(`amd64` or `arm64`).

### What the package sets up

| Path | Contents |
|------|--------|
| `/usr/bin/lcm` | the program file (single binary, web UI embedded) |
| `/lib/systemd/system/lcm.service` | the hardened systemd unit |
| `/etc/lcm/config.json` | configuration (created on first run with a random JWT secret) |
| `/var/lib/lcm/` | state: encrypted SQLite DB, master key (`lcm.key`), TLS certificate, backups |

The service runs under the dedicated system user **`lcm`** without root
privileges - LCM manages *other* servers via SSH and needs no elevated rights
on its own host.

### First login

The initial admin password is written **once to the journal** on first start:

```sh
journalctl -u lcm | grep -A3 'Admin-Zugang'
```

Then open in the browser: `https://<server-ip>:9310` (self-signed certificate -
the browser warning is expected). Admin login, change the password, go.

### Managing the service

```sh
systemctl status lcm      # state
systemctl restart lcm     # restart
journalctl -u lcm -f      # logs live
```

### Configuration

All settings live in `/etc/lcm/config.json` - after changes restart the service
(`systemctl restart lcm`). Important values:

- `host` (default `0.0.0.0`): bind address. **Security note:** by default the
  interface is reachable on the network (HTTPS, self-signed). For production,
  restrict access via firewall and/or put a reverse proxy with a valid
  certificate in front. For purely local operation set `host` to `127.0.0.1`.
- `port` (default `9310`).
- `jwt_secret`: signs the sessions - do **not** change it (otherwise all logins
  become invalid) and do **not** share it.

### Updating

Install the new package over the old one:

```sh
sudo apt install ./lcm_<new-version>_amd64.deb
```

Configuration, database, and the JWT secret are preserved; pending database
migrations run automatically at startup.

### Removal

```sh
sudo apt remove lcm     # remove service + program, keep data
sudo apt purge  lcm     # additionally delete /etc/lcm, /var/lib/lcm and the lcm user
```

:::caution
`purge` deletes the encrypted database **and** the master key irreversibly.
Back up `/var/lib/lcm` beforehand if needed.
:::

### Building it yourself

Locally (also on macOS - nfpm is platform-independent, the binaries are
cross-compiled):

```sh
make deb            # builds bin/lcm_<version>_amd64.deb and ..._arm64.deb
```

In CI the `packages:deb` job produces the packages from the binaries; on release
to `main` they are automatically attached as release assets.

### Output language

**English is the default.** German only appears if the system is actually set
to German. Evaluation follows POSIX order `LC_ALL` → `LC_MESSAGES` → `LANG`; if
the environment is empty - which happens routinely under `dpkg` - the package
scripts additionally fall back to the system-wide setting in
`/etc/default/locale` or `/etc/locale.conf`.

| Output | Language |
|---|---|
| Installation (`apt install lcm`) | EN, DE on a German system |
| Console on service start (admin password, master key, config) | EN, DE on a German system |
| `lcm-agent` command line | EN, DE on a German system |
| `systemctl status` (unit `Description=`) | always EN - a unit file is static and cannot follow the system language |
| `journalctl -u lcm` (log messages) | always EN - see below |

The **journal log messages are deliberately English-only**: they get shared for
support purposes, and a language that changes with the customer's system would
make analysis needlessly harder. The web interface is unaffected - it stays
fully bilingual (DE/EN, switchable).

:::note[Why all output avoids umlauts]
Package and service output ends up in terminals, log files, journal exports and
CI logs whose character encoding LCM does not control. Umlauts routinely turn
into mojibake there (`Weboberflächeâ€œ`). German output therefore consistently
uses **ue/ae/oe/ss** - in the source the texts may be written normally, the
conversion is handled centrally by `internal/i18n`. The web interface is
exempt: it serves UTF-8 over HTTP and displays umlauts correctly.
:::

The language can be forced through the environment at any time:

```sh
LC_ALL=en_US.UTF-8 apt install lcm    # English installation output
LC_ALL=de_DE.UTF-8 lcm-agent          # German command-line help
```

## Docker

Everything container-related lives under `docker/` - two Dockerfiles and a `docker-compose.yml` as an example:

| File | Contents |
|---|---|
| `docker/Dockerfile` | The LCM runtime image based on **`scratch`** - about 37 MB |
| `docker/Dockerfile.trivyd` | The **Trivy sidecar**: the official Trivy image plus a small binary of ours |

### Why `scratch`

The runtime image contains exactly four things: the Go binary, the CA certificates, a `passwd`/`group` line and two empty directories. No shell, no package manager, no libraries - and therefore nothing that could bring vulnerabilities of its own. The binary is static (`CGO_ENABLED=0`) and carries its timezone data inside.

One side effect makes multi-arch cheap: the runtime section contains no `RUN`. An arm64 image can therefore be built on an amd64 machine **without QEMU emulation** - it is only copying.

### Two build modes, one file

`docker/Dockerfile` understands the build arg `BIN_SOURCE`:

| Value | Approach | When to use |
|---|---|---|
| `prebuilt` (default) | Copies the **Linux binary built beforehand on the host** from `bin/` | Normal case: image build in seconds, no toolchain in the build context, exactly the same binary as with all other deployment paths |
| `source` | Builds frontend + backend **entirely in the container** (Node LTS → Go 1.x, with `npm audit` and `govulncheck` as gates) | Environments without Go/Node on the host |

```sh
make docker-build          # prebuilt (default)
make docker-build-full     # equivalent to --build-arg BIN_SOURCE=source
```

BuildKit only builds the stages required for the chosen target - the other build mode costs nothing.

### Trivy in container mode

The CVE scan needs Trivy. There is none in the scratch image - and putting it there would mean bringing back a package manager and a second attack target. Trivy therefore runs in its **own container** (`docker/Dockerfile.trivyd`), and LCM talks to it over HTTP.

The container is the confinement here: it holds neither LCM's database nor the master key - there is simply nothing to reach. On a host installation bubblewrap or Landlock does that job; inside a container it would be theatre on top, because it needs privileges a hardened container deliberately lacks. The interface reports the state as `container` rather than "no sandbox" - either alternative would be misleading.

```sh
cp docker/.env.example docker/.env
sed -i "s/BITTE-ERSETZEN/$(openssl rand -hex 32)/" docker/.env
make docker-build docker-build-trivyd
docker compose -f docker/docker-compose.yml up -d
```

The token protects the sidecar; it runs processes and pulls images from foreign registries. **Without a token it does not start** - and Compose aborts as well, instead of quietly running unprotected. The sidecar's port is deliberately **not** published: it is reachable only inside the container network.

#### The sidecar carries the Trivy version, not the LCM one

It is published as `…/trivyd:<trivy-version>` (currently `0.74.0`) plus the
moving channel tag (`:beta`, `:latest`) - deliberately **not** with the LCM
version. It only changes when Trivy is bumped or our adapter is touched. With
the LCM version in the tag, every release would produce a new digest for
unchanged content: the registry would fill up with copies, and the tag list
would look as if the scanner had moved.

For your own compose file that means: pull the sidecar via the channel tag
(`:beta` or `:latest`), or name the Trivy version explicitly.

LCM still runs without the sidecar - just without a CVE scan, exactly as before in Docker mode. A **configured but unreachable** sidecar, however, is an error and is reported as one: otherwise an outage would look like a disabled scan, and an empty findings list like an all-clear.

### What the container cannot do

Inside a container `localhost` is the container itself, not the machine underneath. Therefore:

- LCM does **not** add itself there as a managed server.
- The join wizard rejects `localhost`/`127.0.0.1` and points to the Docker host's network address.
- The setup actions on the LCM host card (Trivy, sandbox, apt-cacher-ng, CrowdSec LAPI) are not offered - they set something up on a host with `apt` and systemd.
- Updates arrive as a **new image**, not through the apt channel.

### Quickstart with Docker Compose (recommended)

```sh
make docker-build          # build Linux binary (incl. audits) + create image
docker compose -f docker/docker-compose.yml up -d
docker compose -f docker/docker-compose.yml logs -f     # first start: the generated admin password is here!
```

`make docker-build` does both in one step: first the normal, security-checked build (npm audit → Vite → govulncheck → cross-compile for Linux, architecture detected automatically, overridable with `DOCKER_ARCH=arm64`), then the seconds-fast image build that only copies the binary in.

Afterwards the app runs at <http://localhost:9310>. On first start the following are created in the host folder `./data`:

```
data/
├── config.json    configuration (incl. generated JWT secret)
├── app.db         SQLite database (+ -wal/-shm during operation)
└── version.json   installed version (update detection)
```

All three files live **on the host** thanks to the bind mount - inspect, back up, adjust (after changes to config.json: `docker compose -f docker/docker-compose.yml restart`).

Stopping/updating:

```sh
docker compose -f docker/docker-compose.yml down                    # stop (data stays in ./data)
make docker-build                      # build new version
docker compose -f docker/docker-compose.yml up -d                   # start
docker compose -f docker/docker-compose.yml logs | grep update      # -> "update erkannt - von=… auf=…"
```

When a new version starts, the [update migration system](/en/reference/database/) kicks in automatically: the container compares `data/version.json` with its binary version and runs any pending migration scripts.

### With the Docker command only (without Compose)

Build the image - standard path (build the binary on the host, then copy it):

```sh
make build-linux                # or make build-linux-arm64 on ARM hosts
docker build -t lcm .
```

Or entirely in the container (without Go/Node on the host):

```sh
docker build --build-arg BIN_SOURCE=source -t lcm .
```

Start the container - with the same hardening options as in the Compose example:

```sh
docker run -d \
  --name lcm \
  -p 9310:9310 \
  -v "$(pwd)/data:/data" \
  --read-only \
  --security-opt no-new-privileges \
  --cap-drop ALL \
  --restart unless-stopped \
  lcm
```

Show the first-start admin password, stop, remove:

```sh
docker logs lcm | grep -A3 Seeding
docker stop lcm
docker rm lcm          # ./data is preserved
```

Minimal variant (just for trying it out, without hardening and without persistent data):

```sh
docker run --rm -p 9310:9310 lcm
```

### How the image is built

**Build mode `prebuilt` (default):** the Linux binary built on the host is copied in (`bin/lcm-linux-<arch>`; `ARG TARGETARCH` automatically picks the one matching the target platform). All security gates (npm audit, govulncheck) and the version injection run in the Makefile build on the host. The time zone data is embedded in the Go binary (`time/tzdata`), so no `tzdata` package is needed.

**Build mode `source`:** the frontend is built in the Node LTS section (`npm ci` + `npm audit`), the Go binary in the Go section (`golang:1-alpine` = the latest stable Go 1.x, with `govulncheck` and `CGO_ENABLED=0`). Both run explicitly on the build platform and cross-compile - which is why an arm64 image needs no emulation either.

**The runtime image is the same in both cases:** `FROM scratch`, containing only the binary, the CA certificates, a `passwd`/`group` line and the empty directories `/data` and `/tmp`. Build tools and source code are **not** included - and neither is a shell, a package manager or any library.

### Security hardening in detail

In the **image** (both Dockerfiles):

| Measure | Effect |
|---|---|
| `FROM scratch` | no shell, no package manager, no libraries - nothing that could carry vulnerabilities of its own |
| Only `ca-certificates` carried over | TLS to the outside works, nothing more is needed |
| `USER 1000:1000` | process never runs as root |
| `CGO_ENABLED=0` | static binary, no libc vulnerabilities |
| `npm audit` + `govulncheck` in the build | vulnerable dependencies abort the build (on the host via `make audit`) |
| `HEALTHCHECK` through the binary itself | orchestration detects hung containers - and the check needs no shell, which a scratch image does not have |

At **runtime** (Compose/`docker run` flags):

| Measure | Effect |
|---|---|
| `read_only: true` | root file system immutable; only the `/data` volume is writable |
| `no-new-privileges` | no privilege escalation (setuid & co.) |
| `cap_drop: ALL` | all Linux capabilities removed - the service needs none |
| Port mapping instead of `--network host` | container only sees its own network namespace |

**TLS:** the container speaks HTTP. For public deployments put a reverse proxy (Caddy, Traefik, nginx) in front that terminates TLS - typically as another Compose service.

### Permissions of the data volume

The container writes as **UID 1000** (the usual first Linux user). On most hosts the bind mount `./data:/data` works directly with that. If your host user has a different UID, give the directory to the container user:

```sh
mkdir -p data && sudo chown 1000 data
```

Alternatively use a named volume (Docker manages the permissions itself, but the files no longer live directly in the project folder):

```yaml
volumes:
  - lcm-data:/data
# ...
volumes:
  lcm-data:
```

### Configuration in the container

Normal configuration happens via `data/config.json` (created on first start). Environment variables allow container-typical overrides without changing the file:

| Variable | Meaning | Default in the image |
|---|---|---|
| `TZ` | application time zone (logs, timestamps), e.g. `Europe/Berlin` | `Etc/UTC` |
| `LCM_HOST` | listen address | `0.0.0.0` (needed for port mapping) |
| `LCM_PORT` | listen port | value from config.json (9310) |
| `LCM_DATA` | data directory | `/data` |

Example (Compose):

```yaml
environment:
  TZ: Europe/Berlin
  LCM_PORT: "9000"
ports:
  - "9000:9000"
```

**On the time zone:** normally an Alpine container needs the `tzdata` package for this - this template instead embeds the IANA time zone data directly in the Go binary (`import _ "time/tzdata"` in `cmd/app/main.go`). `TZ` therefore works in even the most minimal container and also on Windows, without any additional packages.

The `-debug` flag also works in the container: `docker run … lcm -debug` (arguments after the image name go to the binary, thanks to `ENTRYPOINT`).
