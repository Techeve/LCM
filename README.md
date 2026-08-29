<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="logo/lcm-wordmark-dark.svg">
    <img src="logo/lcm-wordmark-light.svg" alt="LCM - Linux Centralized Management" width="380">
  </picture>
</p>

<h1 align="center">LCM - Linux Centralized Management</h1>

<p align="center">
  <strong>Agentless management for your entire Linux fleet - one binary, any number of servers.</strong>
</p>

<p align="center">
  <a href="https://techeve.de/produkte/lcm/">Website</a> ·
  <a href="https://doc.techeve.de/lcm/en/">Documentation</a> ·
  <a href="https://github.com/Techeve/LCM/releases">Releases</a> ·
  <a href="README.de.md">Deutsch</a>
</p>

LCM is a self-hosted control center for Linux servers. Install it once - a single
binary with no external dependencies - and manage any number of machines over
plain SSH: nothing to roll out on the managed servers, no agents to keep
updated, no configuration management language to learn. Go backend (Fiber v3,
GORM, SQLite) plus Svelte 5 frontend (Vite, Bootstrap 5) in **one executable**,
running on Linux, Windows and macOS.

It is built for the daily reality of running a server fleet - whether that is a
homelab, a company's infrastructure, or customer machines: knowing at a glance
which servers need attention, patching them on schedule, catching CVEs and full
disks before they hurt, and being able to prove afterwards who did what.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/static/screenshots/dashboard-dark.webp">
  <img src="docs/static/screenshots/dashboard-light.webp" alt="LCM dashboard with traffic-light status and server list">
</picture>

## What you can do with it

- **See the state of every server at a glance** - a traffic-light status
  (🟢🟡🔴) per machine, driven by pending updates, CVE findings, disk capacity
  and reachability, with detail insights one click away.
- **Patch entire groups on schedule** - server groups with rules (daily
  updates, scripts, sync), an internal cron scheduler, and a concurrency lock
  per server so jobs never overlap.
- **Catch vulnerabilities early** - the centrally collected package inventory
  of all servers is checked daily against the Trivy vulnerability database,
  SBOM-based, with no agent and no extra server contact.
- **Keep Docker deployments current** - container and image inventory per
  server, central registry checks for newer digests, CVE scanning of the images
  in use, and one-click `pull && up -d` for Compose projects.
- **Know before the disk runs full** - hourly capacity measurements, condensed
  into daily averages, extrapolated by linear regression into a "time until
  full" forecast, with configurable alert rules and e-mail notifications.
- **Manage users and SSH keys centrally** - users and their public keys in one
  place, `authorized_keys` distributed automatically to assigned servers and
  groups.
- **Prove who did what** - every action is a job with the exact SSH console
  output, recorded in a tamper-evident hash chain.

## Screenshots

All pages come in light and dark mode - the images below follow your system
theme. Taken from the built-in demo mode (`lcm --demo`).

<table>
  <tr>
    <td width="50%">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="docs/static/screenshots/server-web01-dark.webp">
        <img src="docs/static/screenshots/server-web01-light.webp" alt="Server detail page with hardware, firewall and kernel status">
      </picture>
      <em>Server detail: hardware, updates, firewall, kernel</em>
    </td>
    <td width="50%">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="docs/static/screenshots/security-dark.webp">
        <img src="docs/static/screenshots/security-light.webp" alt="Security overview with CVE findings across all servers">
      </picture>
      <em>Security: CVE findings across the whole fleet</em>
    </td>
  </tr>
  <tr>
    <td width="50%">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="docs/static/screenshots/groups-dark.webp">
        <img src="docs/static/screenshots/groups-light.webp" alt="Server group with members, schedules and policy rules">
      </picture>
      <em>Groups: members, schedules and policy rules</em>
    </td>
    <td width="50%">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="docs/static/screenshots/docker-dark.webp">
        <img src="docs/static/screenshots/docker-light.webp" alt="Docker overview with containers and image updates">
      </picture>
      <em>Docker: containers, Compose projects, image updates</em>
    </td>
  </tr>
  <tr>
    <td width="50%">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="docs/static/screenshots/alerts-dark.webp">
        <img src="docs/static/screenshots/alerts-light.webp" alt="Alert rules with thresholds and notification channels">
      </picture>
      <em>Alerts: rules, thresholds, notification channels</em>
    </td>
    <td width="50%">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="docs/static/screenshots/jobs-dark.webp">
        <img src="docs/static/screenshots/jobs-light.webp" alt="Job history with logged actions">
      </picture>
      <em>Jobs: every action logged with full console output</em>
    </td>
  </tr>
  <tr>
    <td width="50%">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="docs/static/screenshots/linux-users-dark.webp">
        <img src="docs/static/screenshots/linux-users-light.webp" alt="Central management of Linux users and SSH keys">
      </picture>
      <em>Linux users: central user and SSH key provisioning</em>
    </td>
    <td width="50%">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="docs/static/screenshots/server-pve01-dark.webp">
        <img src="docs/static/screenshots/server-pve01-light.webp" alt="Detected Proxmox VE system in the detail view">
      </picture>
      <em>Proxmox VE hosts are detected and shown as such</em>
    </td>
  </tr>
</table>

## Features in detail

- **Server onboarding (join)** - guided enrolment with SSH host key fingerprint
  confirmation (MitM protection), automatic provisioning of a dedicated service
  user and **a unique SSH key pair per server** (zero trust, limited blast
  radius)
- **Monitoring** - packages & versions, overdue updates, repositories, hardware
  (CPU, RAM, disk, IPs); traffic-light status 🟢🟡🔴 per server with detail
  insights (CVE-driven too: critical vulnerability → red, high → yellow)
- **CVE scanning (Trivy)** - the centrally collected package inventory of all
  servers is checked daily against the Trivy vulnerability database (SBOM-based,
  agentless, without contacting the servers again); findings per server in the
  security tab and fleet-wide on the security page. Trivy is optional - without
  it the feature disables itself cleanly
- **Docker monitoring & updates** - the system scan records containers
  (including Compose projects) and images of every server; the Docker check
  (part of the daily system sync) centrally queries the registry API for newer
  digests behind a tag and scans the images in use - deduplicated - with Trivy
  for CVEs (same traffic light/alerts as the package inventory). Compose
  projects can be updated from LCM via `pull && up -d`, standalone images via
  `docker pull`; unused images can be removed individually or cleaned up by
  group rule
- **APT cache (apt-cacher-ng)** - a central package cache for the whole fleet:
  cache URL in the settings with live reachability check, servers attached by
  one-click action (with function test and automatic rollback) or enforced by
  group rule (drift check on every connection). The catalogue of known package
  sources (Docker, PostgreSQL, …) is maintained under Settings → Repositories.
  Guide: [docs/guides/apt-cache.mdx](docs/guides/apt-cache.mdx)
- **Disk history & forecast** - the health check measures disk usage hourly,
  condenses it into daily averages (retention configurable, 90-365 days) and
  extrapolates by linear regression how long the capacity will last
  ("unlimited" beyond one year)
- **Alerts & notifications** - configurable alert rules (disk threshold,
  capacity forecast, CVE findings, overdue updates, heartbeat) with severity,
  cooldown and e-mail delivery over configurable channels
- **Automation** - server groups with rules (daily updates, scripts, sync), an
  internal cron scheduler, health checks every 15 minutes, a concurrency lock
  per server against overlapping jobs
- **User provisioning** - central management of users and their SSH public
  keys, automatic distribution of `authorized_keys` to assigned servers/groups
- **Security automation** - SSH hardening (certificate-only login), firewall
  checks (ufw)
- **Complete auditing** - every action as a job/audit log with the exact SSH
  console output, a tamper-evident hash chain, configurable log retention
  (default 90 days)
- **Roles & permissions** - admins, managers (only assigned servers, tenant
  isolation at query level), service accounts (API keys with expiry), regular
  users as key identities
- **Security by design** - AES-256-GCM encryption of sensitive data at rest
  (master key in `lcm.key` or `LCM_ENCRYPTION_KEY`), key rotation
  (`rotate-db-key`, SSH certificate rotation), TOTP 2FA with enforcement, HTTPS
  by default (self-signed out of the box, custom certificates configurable),
  rate limiting, strict security headers, optional **IP allowlist**
  (`allowed_ips` in config.json: localhost only, private networks, or selected
  addresses/CIDRs)
- **Dark mode** - switchable colour scheme (system/dark/light) in the
  navigation bar; follows the operating system by default and remembers the
  choice in the browser
- **System backups** - passphrase-encrypted, portable `.lcmbak` archive
  (database snapshot + master key + config + TLS) with configurable
  interval/retention; download, rollback to an earlier state and restore from
  an uploaded archive (even on a fresh instance), applied at startup

## How does LCM compare?

- **Ansible & friends** are task runners: they execute what you scripted, then
  forget. LCM is a permanent control center **with state** - monitoring, CVE
  findings, disk history, audit trail - and needs no playbooks. The two
  combine well; LCM covers the operating part.
- **Foreman, Uyuni & co.** are powerful but heavyweight platforms: several
  services, real hardware requirements, days of setup. LCM is one binary and
  is answering questions five minutes after the download.
- **Canonical Landscape** is tied to one distribution and a vendor
  subscription. LCM manages Debian, Ubuntu, RHEL derivatives, openSUSE and
  Proxmox VE side by side - and the Community Edition is AGPL open source.

## Installation (Debian/Ubuntu, recommended)

Via the TechEve APT repository - set up once, then install and keep current
with `apt upgrade`:

```sh
curl -fsSL https://repo.techeve.de/setup.sh | sudo sh   # repo + signing key
sudo apt install lcm                                    # install
sudo apt update && sudo apt upgrade                     # update later
```

This sets LCM up as an unprivileged `systemd` service (autostart, HTTPS). The
initial admin password is in the journal:
`journalctl -u lcm | grep -A3 'Admin-Zugang'`. Without the repo: install a
single `.deb` from the
[releases](https://gitlab.techeve.de/techeve/lcm/-/releases) with
`sudo apt install ./lcm_<version>_<arch>.deb`. Further options (Docker, from
source): [installation docs](docs/getting-started/installation.md).

## Quickstart (from source)

```sh
make build     # npm audit → vite build → govulncheck → go build
./bin/lcm      # first start: creates config.json, lcm.key + DB, prints the admin password
```

Then open the printed address and sign in with the initial admin account.

**Demo mode** (test servers, simulated hardware, job histories - no real Linux
servers required). Only available through this flag (deliberately not a
config.json field) and only effective on first start with a fresh database -
without the flag a new installation starts empty:

```sh
./bin/lcm --demo
```

**Development** with hot reload:

```sh
make dev       # backend :9310 + Vite dev server :5173
```

**Tests:**

```sh
make test      # Go unit/integration tests (in-memory SQLite)
make test-e2e  # Playwright E2E against the real binary
```

## Documentation

The full documentation is published at
**[doc.techeve.de/lcm](https://doc.techeve.de/lcm/en/)** (English and German)
and lives as **plain Markdown** in this repository under [`docs/`](docs/) -
German at the root, English under [`docs/en/`](docs/en/). The repo deliberately
contains **no** Astro/Node/npm scaffolding: CI builds the pages with the
central [Astro Starlight](https://starlight.astro.build/) builder image
(`techeve/docs-builder`) and deploys them (jobs `docs-build`/`docs-deploy` in
[.gitlab-ci.yml](.gitlab-ci.yml)). The sidebar is generated from the folder
structure; each page only needs a `title` frontmatter.

Structure (DE + EN each):

| Section | Contents |
|---|---|
| **Getting started** | overview, installation, quickstart |
| **Guides** | monitoring, groups/rules, security & CVE, Docker, APT cache, Linux users, backups, alerts |
| **Examples** | typical workflows step by step |
| **Reference** | architecture, database & migrations, frontend & API, security model, dependencies & supply chain, packaging, CI/CD & releases |

## Open source & contributing

LCM is **open source under the [AGPL-3.0](LICENSE)**. Development happens in an
internal repository; on every release the public repository automatically
receives the full state of the branches `community` (stable releases) and
`beta` (pre-releases) - including history and release tags.

**Ideas, feature requests and bug reports are very welcome** - as an issue in
the public repository. We discuss every proposal internally and report the
outcome back in the issue. Code contributions via merge request are welcome
too - the process (review there, adoption by cherry-pick preserving your
authorship, [CLA](CLA.md)) is described in [CONTRIBUTING.md](CONTRIBUTING.md).

LCM ships in three channels from the TechEve APT repository: **stable**
(Community Edition, every release), **beta** (pre-releases for early testing)
and **enterprise** (conservative maintenance line with subscription - same
features, functional updates only after a proven field phase, fixes all the
faster for it).

## Development & releases (GitLab)

The project lives at `https://gitlab.techeve.de/techeve/LCM` with a protected
workflow:

- **Work happens on `develop`** (default branch) or on feature branches with an
  MR to develop.
- **The channel branches `beta`, `community` and `enterprise` are locked**: no
  direct pushes - changes arrive exclusively via merge request with a green CI
  pipeline. Which channel serves which audience is described in
  [docs/reference/repo-channels.md](docs/reference/repo-channels.md).
- **CI/CD** ([.gitlab-ci.yml](.gitlab-ci.yml)): npm audit → Go tests →
  govulncheck → Playwright E2E → cross-compile for all platforms.
- **Dependency bot** ([renovate.json](renovate.json)): Renovate checks the Go
  modules and npm packages for updates weekly and opens merge requests
  automatically - at the earliest seven days after publication, and only
  publishers with a solid release process are auto-merged
  ([rationale](docs/reference/dependencies.md)). Runs as a CI image only - not
  a project dependency. Details: [docs/reference/ci-release.md](docs/reference/ci-release.md).
- **Automated releases**: commits follow
  [Conventional Commits](docs/reference/ci-release.md) (`feat:`, `fix:`,
  `feat!:` …). Version and [CHANGELOG.md](CHANGELOG.md) are prepared on
  `develop` with `make prepare-release` - so they are part of exactly the
  commit that will be tagged. On merge into a channel branch the pipeline
  creates the tag and the release with all binaries; it writes nothing back to
  the repository and therefore needs no write token. Preview:
  `make next-version`.

## Stack

Go · Fiber v3 · GORM · SQLite (CGO-free) · x/crypto/ssh · argon2id · JWT ·
AES-256-GCM - Svelte 5 (runes) · Bootstrap 5 · Vite - Playwright · govulncheck ·
npm audit

## Dependencies & supply chain

**No external packages for problems a few lines of our own code can solve** -
the system stays lightweight, secure and maintainable. The reverse holds just
as firmly: **no home-grown cryptography or protocol parsing** - there, the
self-built vulnerability is more likely than the supply chain attack you were
trying to avoid. Concretely:

- **Assessment along two axes** - stewardship (an organisation with a release
  process, or an individual with a bus factor of 1?) and the surface actually
  used. We do not adopt dependencies on effectively abandoned projects.
- **Build it ourselves only** when no cryptography, no parsing of foreign data
  and a harmless failure mode come together - that is how log rotation and QR
  encoding came to be in-project.
- **The update path is the real risk.** `go.sum` and `package-lock.json`
  prevent after-the-fact manipulation; an attack therefore always arrives as a
  *new* version. Renovate keeps a seven-day grace period and auto-merges only
  publishers with a solid release process.
- **No npm install scripts** in CI and build (`--ignore-scripts`) - they would
  run on the runner with access to tokens and signing keys.

Rules, checklist and the assessment of the current inventory:
[docs/reference/dependencies.md](docs/reference/dependencies.md).
