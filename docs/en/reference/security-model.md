---
sidebar:
  order: 21
title: Security Model
description: Authentication, JWT, RBAC, API keys, and LCM's other hardening measures.
---

## Overview

| Building block | Technology | Location |
|---|---|---|
| Password hashing | argon2id (OWASP parameters) | `internal/core/services/auth_service.go` |
| User auth | JWT (HS256), 60 min TTL | `AuthService` + `middlewares.Authenticate` |
| Service auth | API keys (SHA-256 hashed) | `APIKeyService` |
| Authorization | RBAC: user → roles → permissions | `middlewares.RequirePermission` |
| Secrets | generated `config.json` (0600) | `internal/config/config.go` |

## Passwords: argon2id

Passwords are hashed exclusively with **argon2id** (64 MiB memory, 4 threads - resistant to GPU cracking). There is nowhere in the entire system where a plaintext password is stored. For an unknown username, the login compares against a dummy hash so that "user does not exist" and "wrong password" cannot be distinguished by timing, and returns the same error in both cases (no user-enumeration leak).

## Password policy

The server is the **sole authority**: every place that sets a password (creating
a user, resetting a password, invitation/activation link, Linux user activation)
calls the same function, `services.EnforcePasswordPolicy`. The strength meter in
the UI merely mirrors these rules for instant feedback while typing - it can be
bypassed and is deliberately **not** a security control.

A password is rejected if any rule applies:

| Rule | Rationale |
|---|---|
| shorter than **12 characters** (counted in characters, not bytes) | floor against offline cracking |
| longer than 200 characters | abuse via oversized request bodies |
| fewer than **3 character classes** (upper, lower, digits, symbols) | from 20 characters 2 suffice - length replaces complexity (NIST SP 800-63B) |
| fewer than 6 **distinct** characters | "AAAAbbbb1111" is weak despite its length |
| contains **username, first/last name or an email fragment** | the first candidates in any targeted attack |
| **dominated** by an obvious term (`admin`, `password`, `lcm`, …) | "admin-admin-1A!" is guessed, not chosen |
| is a known **default password** | including leetspeak and an appended year: `P4ssw0rd!2026` is caught just like `password` |
| contains a **predictable sequence** (`1234`, `abcd`, `qwerty`, `1qaz2wsx`) | checked forwards and backwards |
| contains too many **repetitions** (`aaa`, or a short repeated block `abcabcabc`) | looks long, isn't |
| starts/ends with **whitespace** or contains **control characters** | input mistakes and copy artefacts |

The check returns **machine-readable codes** (`too_short`, `contains_identity`,
`common_password`, …). The UI translates them in both languages and shows
concretely what is missing - instead of a blanket rejection.

:::note[Existing passwords]
The policy applies when a password is **set**. Passwords already in use stay
valid until they are next changed.
:::

## Two-factor authentication (TOTP)

**In regular operation, 2FA is enabled by default for administrators.** When
seeding a fresh database, LCM sets `require_2fa_roles = admin`. Reason: the admin
account manages SSH access and root privileges for the entire fleet - a password
alone is not enough. The setting can be changed under *Settings → General*;
unknown role names are **rejected** so a typo cannot silently disable the
requirement.

In **development (`--dev`) and demo mode (`--demo`)** the requirement is
deliberately off - there it would only get in the way, and neither mode is
intended for production.

Enforcement is server-side and complete: `middlewares.AccountRemediation` works
as an allowlist (fail-closed) and lets an account that must set up 2FA reach only
the endpoints needed for that. All places that verify a TOTP code - login,
disabling 2FA, changing your own password - share the **same** per-account
brute-force counter, so switching endpoints does not bypass the lockout.

## Brute-force protection

Failed attempts are counted on **two** keys simultaneously:

- **per client IP** (threshold 5) - stops rapid guessing from a single source;
- **per account** (threshold 15) - stops password spraying distributed across many
  IPs that would deliberately evade the IP lockout. The higher threshold and the
  capped lockout duration prevent an attacker from cheaply locking out a
  legitimate account.

Both lockouts grow exponentially (max. 15 minutes) and decay after 15 minutes
without a failure. Check and count happen in **one** operation under the same
lock - otherwise hundreds of parallel requests could slip past the check together
before the first failure is recorded.

The client IP comes from the same function as the IP allowlist
(`middlewares.ClientIP`) and honours `trust_proxy_header`. Without that, the
address behind a reverse proxy would be identical for **all** clients - five
failed attempts by an attacker would have locked login for the entire
installation.

## Links in emails: `public_base_url`

Links in password-reset and invitation emails are built **exclusively** from the
`public_base_url` setting (*Settings → General*), never from the request's `Host`
header.

:::caution[Why this matters]
If the base address came from the request, an attacker could trigger a password
reset for someone **else's** account using a forged `Host` header. The victim
would receive a genuine LCM email - carrying a valid token on the attacker's
domain. One click would be enough for a full account takeover.
:::

If `public_base_url` is not set, LCM falls back to its own configuration (scheme,
host and port of the listener). Depending on the network setup the link may then
not be reachable from outside - but it is never misleading. For production, the
address should be set.

## JWT lifecycle

1. **Login** (`POST /api/v1/auth/login`): after argon2id verification, `AuthService` issues an HS256-signed JWT. Claims: user ID (`sub`), username, `iat`/`exp`.
2. **Request**: the frontend sends `Authorization: Bearer <jwt>`. The `Authenticate` middleware validates signature and expiry - the signature method is explicitly pinned to HS256 (`jwt.WithValidMethods`), which prevents algorithm-confusion attacks.
3. **Role resolution**: permissions are **not** stored in the token. On every request the user is loaded fresh from the DB together with their roles/permissions - role changes and deactivations take effect immediately, not only at the next login.
4. **Expiry**: after `access_token_ttl_minutes` (default 60) the token is invalid → the server responds 401 → the frontend logs out automatically (see [API reference](/en/reference/api/)).

The JWT secret is generated cryptographically at random on first start (48 bytes from `crypto/rand`) and lives only in the `config.json` (file permissions 0600). Secrets shorter than 32 characters are rejected on load.

**Session invalidation on every restart:** The effective HS256 signing material is **not** the `jwt_secret` directly, but `HMAC-SHA256(jwt_secret, instance nonce)`. The nonce is drawn fresh from `crypto/rand` on every process start and lives only in memory (`deriveSigningKey` in `auth_service.go`). So every (re)start produces a different signing key and **all** previously issued tokens fail signature verification - every session ends, a new login is required. This holds even with an unchanged `jwt_secret` and specifically covers the case where an old, still-valid session would otherwise trust a freshly seeded admin with the same ID (rebuild, process restart, newly created database).

## RBAC: user → role → permission

```
User "alice" ──> Role "admin"   ──> Permissions: users:read, users:write, servers:write, ...
User "bob"   ──> Role "manager" ──> Permissions: servers:read, servers:write, jobs:read, ...
```

Permission codes are constants in `internal/core/domain/rbac.go`. Routes are protected declaratively:

```go
// Logged in only:
auth.Post("/2fa/setup", middlewares.RequireAuth(), authCtrl.SetupTOTP)

// Logged in AND permission:
servers.Get("/:id/storage-history", middlewares.RequirePermission(domain.PermServersRead), serverCtrl.StorageHistory)
```

`RequirePermission` responds 401 (not logged in) or 403 (logged in, but the right is missing). Role resolution happens in the background: `Authenticate` loads the user with `Preload("Roles.Permissions")`, and `HasPermission` then only checks in-memory.

**Introducing a new permission:**

1. Define the constant in `domain/rbac.go` (convention: `resource:action`).
2. Describe it in `storage/seed.go` and assign it to the appropriate roles.
3. Protect the route with it.
4. Optionally use `auth.can('...')` in the frontend for the UI - this is purely cosmetic; the real check is always done by the server.

## API keys for service communication

For processes without a browser (CI, cron, other services) there are API keys (`X-API-Key` header):

- Created via `POST /api/v1/apikeys` (permission `apikeys:manage`); the plaintext key (`lcm_…`) is included **only in this one response**.
- Only the SHA-256 hash is stored. A DB leak does not reveal any valid keys.
- Each key runs in the permission context of the user who created it - RBAC applies unchanged.
- Keys can expire (`expires_in_days`) and be revoked.

**Scopes:** when creating a key you choose `"scope": "read"`, `"readwrite"` (default) or `"mcp"`. A `read` key may only use `GET`/`HEAD`/`OPTIONS` - any writing method is rejected with **403** in the `Authenticate` middleware, *before* controller code runs and in addition to the RBAC check. This lets you issue, for example, monitoring keys that cannot change anything even in an admin context. An `mcp` key is strictly isolated: it works **only** on the separate MCP listener (see below) and is rejected on the regular REST API/UI. Migration 1 automatically sets keys from older versions to `readwrite` (previous behavior).

**Rate limiting:** `api_key_rate_limit_per_minute` in config.json (default 120, `0` = off) limits requests **per API key per minute** (fixed window, in-memory). The limit applies before key validation - even brute-force attempts with invalid keys are throttled. On exceeding it: **429** with a `Retry-After` header. JWT browser sessions are not affected. For multi-instance deployments the limit belongs in the reverse proxy or a shared store.

The seeding user `system` exists for background processes without a user context; it cannot log in with a password (`IsSystem`).

## Logging & access log

The log service (`internal/logging/logging.go`) is based on `log/slog`:

- **Level** via `log_level` in config.json (`debug`, `info`, `warn`, `error`).
- **Debug mode at startup:** `./lcm -debug` raises the level to `debug` without changing the config - for development and troubleshooting.
- **Access log** (`access_log: true`): every API request is logged with method, path, status, duration, IP, and username; 4xx as `WARN`, 5xx as `ERROR`. At debug level, additionally the query string and user agent.
- Passwords, tokens, and request bodies are never logged.

## CVE scan of the package inventory (Trivy)

The package inventory of all servers, centrally recorded in the DB, is checked daily (cron configurable, default `30 2 * * *`) against the Trivy vulnerability database - **agentless**: for each server a CycloneDX SBOM is generated from the package records and scanned locally with `trivy sbom`; the managed servers are not contacted for this.

- **Status effect:** critical CVE → red light, high CVE → yellow (with an insight explanation).
- **Graceful degrade:** if Trivy is not installed on the LCM host, the feature disables itself cleanly (notice in the UI and job log); everything else runs normally. Trivy is set up separately (see Installation).
- **Alerts:** the "Security/CVE" alert rule notifies from a configurable minimum severity (see Settings → Alerts).
- Code: `internal/core/services/cvescan.go` (SBOM/PURL building), `internal/infrastructure/trivy/` (CLI binding).

## Restricted mode of the management user

During onboarding (or later via *restrict privileges*) the LCM user can be
moved from `NOPASSWD:ALL` to a sudoers whitelist: package management (apt,
dnf/yum, zypper, pacman, apk), Docker, ufw and the strictly validating
`lcm-helper`. Free-form scripts, custom actions and reboot are then blocked.

**Proof instead of assumption.** After switching, LCM checks - as the
restricted user - whether the helper and the system's package manager really
are reachable via `sudo`. If not, full mode is restored within the same run and
the failure is reported, rather than leaving behind a server whose core
functions are dead and whose way back leads only through the server console.
Part of this is that LCM sets the sudo search path (`secure_path`) itself: RHEL
10 and its clones do not include `/usr/local/sbin`, where the helper lives; on
openSUSE LCM additionally disables `targetpw` for that user.

**What the mode achieves - and what it does not.** It reduces the attack
surface against operating mistakes and accidental changes, and makes explicit
what LCM is allowed to do on the system. It is **not** a protection against an
attacker who has obtained the service key:

- `apt-get`/`dpkg` run arbitrary code as root via hooks
  (`-o APT::Update::Pre-Invoke::=…`),
- `docker run -v /:/host …` mounts the entire host filesystem.

Both are the very purpose of these programs, and `sudo` cannot reliably filter
their arguments. Without them the mode would be pointless, since package
updates and Docker are exactly the actions that should keep working.

Anyone needing real protection against a compromised service account would
have to put these commands behind narrowly validating `lcm-helper`
subcommands (only certain apt transactions without `-o` overrides, Docker
without host mounts and without `--privileged`) - that cuts the feature set
considerably and is deliberately not the current state.

## Privilege profiles: the arguments are part of the rule

A Linux user distributed by LCM gets its root rights through a **privilege
profile** (see [Linux users](/en/guides/linux-users/)). The security boundary
there is the sudoers allowlist, and it stands or falls with the arguments:
`sudo` compares the **complete** command line.

Input validation therefore rejects whatever cannot be bounded:

| Rejected | Why |
|---|---|
| relative path | the user's search path would decide which program runs as root |
| wildcards (`*`, `?`, `[]`) | `apt-get install *` permits any package - package scripts run as root |
| shell metacharacters, **comma** | in sudoers the comma **separates** commands; one comma would smuggle a second command into the same rule |
| bare `systemctl`, `apt-get`, `docker` … | without a sub-action every one of them is allowed, including `systemctl edit` - an editor as root |

Two additions LCM makes itself:

- **`--no-pager`** for `systemctl` and `journalctl`. Without it the pager runs
  as root, and in `less` a plain `!sh` is enough for a root shell - an
  apparently read-only `status` command would be a full privilege escalation.
- **`sudoedit` instead of an editor command.** `sudo nano /etc/…` is
  effectively a root shell. `sudoedit` starts the editor as the user and writes
  the file back as root afterwards.

Programs from which an arbitrary command can be started as root **regardless of
the arguments** - shells, interpreters, editors, pagers, `dd`, `tee`, `chmod` …
- are not forbidden, but they require an explicit confirmation per rule plus an
audit entry. The list is deliberately **not** exhaustive: it catches the common
cases, the responsibility for composing a profile stays with the operator.

In **restricted mode** it is not LCM that writes the file but the
`lcm-helper` - and it does not take the specification on trust: every line must
be issued to its own profile group and must not carry `ALL` as the command,
after which `visudo` runs. Without that check a compromised LCM could hand the
restricted service user exactly the full rights back through a profile file
that the mode is meant to prevent.

## SSH hardening: proven, not claimed

Hardening writes its own drop-in (`60-lcm-hardening.conf`) and then reads back
the **effective** configuration (`sshd -T` evaluates includes and match
blocks). A server counts as hardened only if `sshd` demonstrably reports
password authentication as `no`:

- if it still reports `yes` - for instance because a lexically earlier drop-in
  file wins - the drop-in is rolled back and the failure reported;
- if the check yields **no result at all**, that does not count as success
  either: `ssh_hardened` stays off and the response names the missing proof.
  For a security function an unproven success message is more harmful than an
  honest failure - whoever reads "hardened" stops looking.

The configuration file is not assumed to live under `/etc/ssh`: openSUSE Leap
16 has a stateless `/etc` and ships it as `/usr/etc/ssh/sshd_config`.

## Self-management of the LCM host

Installing the package sets up the machine itself as a managed server named
**`lcm-host`**. Without it, the host-specific features (Trivy, apt-cacher-ng,
CrowdSec LAPI) would stay out of reach on a fresh install until someone
onboarded the host by hand.

:::caution[What this means]
`postinstall.sh` creates the account **`lcm-svc` with `NOPASSWD:ALL`**
(`/etc/sudoers.d/lcm-svc`, mode 0440) and stores an SSH key for it. **The
service can then act as root on this machine without anyone having entered
credentials.**

This is a deliberate trade-off: a tool that manages its own host needs the same
rights there as on any other managed server. The installation output states the
fact on **every** install.
:::

### How the key is handed over

The private key is **not left on the filesystem**:

1. `postinstall.sh` generates the key pair locally, adds the public key to
   `lcm-svc`'s `authorized_keys` and writes the private key to
   `/var/lib/lcm/self-onboard.json` (mode 0600, owned by `lcm`).
2. On the next start LCM reads that file, encrypts the key into the database
   with the master key and **deletes the file** - including when the
   registration fails or is deliberately skipped. A clear-text key must not be
   left behind.

The host key of `127.0.0.1` is probed and stored as a trust anchor just like for
any other server: self-management does not waive strict host-key checking.

### When LCM does NOT add itself

| Case | Behaviour |
|---|---|
| `LCM_NO_SELF_MANAGE=1` during installation | Account, sudoers rule and handover file are never created |
| Container (Docker/Podman/LXC) | No entry - "localhost" there is the container, not the host |
| No SSH service reachable | No entry |
| localhost already managed | No duplicate (detected via loopback + port 22, not by name) |
| The entry was deleted | It does not come back - deleting sets `self_server_disabled` |

The last case is the important escape hatch: anyone who does not want
self-management deletes the server in the web interface. That removes the entry
**and** records that it must not be re-created - otherwise it would return at
the next `apt upgrade`, because `postinstall.sh` runs again.

:::note[Manual cleanup]
Deleting the server removes LCM's access. The account and the sudoers rule stay
on the host; to remove those as well:

```bash
sudo rm -f /etc/sudoers.d/lcm-svc
sudo userdel -r lcm-svc
```
:::

## At-rest encryption & master-key rotation

All secrets in the database are encrypted field by field with **AES-256-GCM**. The **master key** lives separately from the DB in `lcm.key` (file permissions 0600) in the data directory and is created on first start (`internal/infrastructure/crypto`). Without it the encrypted fields are unreadable - which is why it belongs in every [backup](/en/guides/backups/).

Stored encrypted are, among others (full list in `internal/storage/rotate.go`):

- **SSH credentials:** the server private key (`servers.private_key_enc`), the default SSH password, and the onboarding SSH key in the settings;
- the **RouterOS login password** (`servers.login_password_enc`) for devices using password authentication;
- users' **2FA secrets** (`users.totp_secret_enc`) and Linux-user passwords;
- the **system mailer** SMTP password and **notification-channel secrets** (SMTP password or webhook URL);
- **CrowdSec credentials** on the LCM host: the LAPI machine password (`crowd_sec_lapi_password_enc`) and console key (`crowd_sec_console_key_enc`);
- the stored **TLS key PEM**.

Large console output (job/SSH output) as well as the server host/name go through a GORM serializer (`aesgcm`); the server name additionally carries a **blind index** derived from the master key for searching without storing the plaintext.

**Rotation:** the subcommand `lcm rotate-db-key` generates a new master key and re-encrypts all registered fields in **one** transaction - the DB never ends up in a mixed state. The server-name blind index is recomputed with the new key. Newly introduced encrypted columns must be registered in `encryptedColumns` (or `serializerColumns`) so that rotation picks them up.

## LCM Remote (agent listener)

Servers behind NAT connect **outbound** via `lcm-agent` (MQTT over WebSocket) on a **separate, dedicated port** (default `9320`, `internal/remote`) - separate from UI/REST. The enrollment token is shown in plaintext only on first display; at rest only its hash is stored. The agent listener uses the same TLS certificate as the UI, whose fingerprint the agent **pins** during enrollment (MitM protection). Commands over the agent transport are subject to the same runtime cap as the job watchdog. Details: [LCM Remote](/en/guides/remote/).

## MCP interface (AI agents)

The optional MCP listener (`internal/mcp`, off by default, bind `127.0.0.1:9330`) exposes **read-only** server properties to AI agents. Security-relevant points:

- **Dedicated scope:** access only with an API key of scope `mcp` (bearer token); such keys work nowhere else.
- **Whitelist DTO:** only the curated `ServerView` struct is serialized - **never** `domain.Server`. By construction it carries no passwords, login users, private/public keys, host-key fingerprints, or agent tokens.
- **No write tools:** only `list_servers`, `get_server`, and `fleet_summary` - no configuring actions.
- **Toggleable at runtime:** via *Settings → MCP*. The endpoint speaks HTTP; for remote access put a TLS-terminating reverse proxy in front. Details: [MCP interface](/en/guides/mcp/).

## Further measures

- **Error handling:** internal errors only reach the client as a generic "internal server error" - no stack traces or SQL details.
- **Security headers:** `X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`, `Permissions-Policy` (see `middlewares/security.go`).
- **XSS:** the frontend renders exclusively via Svelte templating (automatic escaping); `{@html}` is not used.
- **Build gates:** `make build` aborts on `npm audit` or `govulncheck` findings.
- **Default bind:** `127.0.0.1` - anyone exposing it externally deliberately sets `"host": "0.0.0.0"` and should terminate TLS via a reverse proxy (Caddy, nginx).
- **IP allowlist:** `allowed_ips` in config.json restricts network access to allowed client addresses (keywords `localhost`/`private` or IP/CIDR); non-matching clients get an early **403** (`IPAllowlist` middleware, before auth/logging). Filtering uses the direct TCP connection; behind a reverse proxy set `trust_proxy_header: true` (evaluates `X-Forwarded-For` - only with a trusted proxy). The matcher lives in the `internal/netfilter` package. See [Security & CVE Scans](/en/guides/security-cve/).

## Deliberate simplifications of the template

- **No refresh-token flow:** after token expiry a new login is required. For longer sessions, raise the TTL or add a refresh endpoint.
- **Token in localStorage:** simple and appropriate for desktop/intranet apps. If you need stricter XSS protection, switch to httpOnly cookies (then add CSRF protection, e.g. the Fiber CSRF middleware).
- **No rate limiting:** for exposed deployments, apply Fiber's `limiter` middleware to `/auth/login`.
