---
sidebar:
  order: 12
title: Security tools (fail2ban / CrowdSec)
description: Install and configure fail2ban or CrowdSec at the push of a button.
---

LCM installs and configures **fail2ban** or **CrowdSec** on a server at the push of
a button - both ban repeatedly suspicious IPs (brute-force protection). fail2ban
is the lean, standalone choice (log analysis + IP ban on exactly this host);
CrowdSec is the distributed approach (a local agent, an optional firewall
bouncer, shared decisions via a Local API / the CrowdSec Console).

## Lockout protection

Either tool could lock LCM out via an SSH ban. That's why LCM automatically adds
its **own source IP** (the IP it reaches the server from) **to the allowlist**
before the protection goes live. This IP is read from `$SSH_CONNECTION` on the
server during the hardware scan and prefilled in the form; more IPs can be added.
In addition, **loopback** (`127.0.0.1/8` and `::1`) is always on the list.

:::note[Why the source IP is read unprivileged]
LCM reads `$SSH_CONNECTION` deliberately **without** sudo - a `sudo` wrapper would
discard the environment variable and the auto-detected IP would be empty. The
result overwrites the `LCMSourceIP` stored during the last scan, so that an IP
change of the LCM host (e.g. after a move) does not defeat lockout protection.
:::

## Install

Server detail → **Actions → "Security tools"**. Choose the tool in the modal:

- **fail2ban** - only asks for allowlist IPs. LCM installs fail2ban, writes its
  own drop-in `/etc/fail2ban/jail.d/99-lcm.local` (`backend = systemd`,
  `ignoreip` with the allowlist, sshd jail enabled) and starts the service. An
  existing `jail.local` is left **untouched** - your own jails and tightened
  settings (`bantime`, `maxretry`) stay in effect.
- **CrowdSec** - additionally selectable:
  - install the **firewall bouncer** (enforces bans; nftables preferred),
  - **collections** (default `crowdsecurity/sshd`),
  - **LAPI binding**: *Local* (standalone), *Central LAPI* or *CrowdSec Console* -
    the two central options use the credentials configured under *Settings →
    CrowdSec*.
  - **Allowlist IPs** (LCM IP prefilled).

The action requires **full sudo**; on servers in restricted-sudo mode it is
disabled. It runs as an **asynchronous, logged job**; afterwards LCM re-reads the
actual state (installed + active) and stores it.

### What fail2ban writes

The generated drop-in `/etc/fail2ban/jail.d/99-lcm.local` is deliberately
minimal and robust across distributions (`backend = systemd`):

```ini
[DEFAULT]
backend = systemd
ignoreip = 127.0.0.1/8 ::1 203.0.113.10 198.51.100.0/24

[sshd]
enabled = true
```

The `ignoreip` line is the union of loopback, the LCM source IP, the manually
added IPs and the resolved named allowlists. `ignoreip` accepts IP **and** CIDR -
both are allowed.

fail2ban reads `jail.conf` → `jail.d/*.conf` → `jail.local` → `jail.d/*.local`;
the LCM drop-in comes last and therefore takes precedence for `ignoreip` and
`[sshd]`. Everything else from your own `jail.local` - further jails,
`bantime`, `maxretry` - stays in effect. When removing, LCM only clears its own
file; `/etc/fail2ban` remains if it still holds foreign configuration.

### What CrowdSec sets up

CrowdSec is installed via the official packagecloud.io repo (or the Alpine
community repo).

:::caution[Not every distribution release is available there]
The repo script enters the running distribution release and reports success
even when no packages exist for it - on **Debian 13 (trixie)** that is the
case, the suite answers with 404. What then gets installed is the
distribution's own version, which can be several years old. LCM therefore
reports the **installed version** in the job and warns when the package does
not come from the vendor repo. If you need a current release, set up the repo
manually following the CrowdSec instructions.
:::

LCM then configures, depending on your choice:

- the selected **collections** (`cscli collections install crowdsecurity/sshd …`),
- the optional **firewall bouncer** - automatically
  `crowdsec-firewall-bouncer-nftables` if `nft` is present, otherwise
  `crowdsec-firewall-bouncer-iptables`,
- the **allowlist as a parser whitelist** at
  `/etc/crowdsec/parsers/s02-enrich/lcm-whitelist.yaml` (robust across all
  CrowdSec versions):

  ```yaml
  name: lcm/whitelist
  description: "LCM management allowlist"
  whitelist:
    reason: "LCM management"
    ip:
      - 127.0.0.1/8
      - ::1
      - 203.0.113.10
      - 198.51.100.0/24
  ```

Secrets (LAPI password, console key) are **transferred base64-encoded** and only
decoded on the target; they land in `/etc/crowdsec/local_api_credentials.yaml`
with `chmod 600`.

## The three LAPI modes

The **Local API (LAPI)** is CrowdSec's decision service: the agent reports attack
signals, the LAPI issues ban decisions, the bouncer enforces them. LCM offers
three bindings:

| Mode | What for | Prerequisite |
| --- | --- | --- |
| **Local** (`local`) | Each server runs its own LAPI (standalone). Ideal for single, isolated hosts. | none |
| **Central LAPI** (`remote`) | All servers report to **one** shared LAPI - fleet-wide shared ban lists. | URL + machine login + password under *Settings → CrowdSec*; the machine must be registered there (`cscli machines add`) |
| **CrowdSec Console** (`console`) | Additionally connect to CrowdSec's cloud console (`cscli console enroll`). | enrollment key under *Settings → CrowdSec* |

If you pick *remote* or *console* without stored credentials, LCM aborts the
action **before** the job starts with a clear message - no half-configured server
is left behind.

## Maintain central credentials

*Settings → CrowdSec*:

- **Self-hosted LAPI** - URL + machine login + password (stored encrypted).
- **CrowdSec Console** - enrollment key (stored encrypted).

Only configured options are selectable in the install form.

## CrowdSec LAPI on the LCM host

Instead of running an external LAPI server, LCM can set up the **CrowdSec Local
API directly on the LCM host** and wire up the credentials **automatically**.
Step by step:

1. Open the server detail of the **LCM host** (localhost) → **"LCM host setup"**
   card → **CrowdSec LAPI** → *Set up* (optionally install the local firewall
   bouncer too).
2. LCM installs CrowdSec, opens the LAPI on `0.0.0.0:8080`
   (`/etc/crowdsec/config.yaml.local`), generates a random password and creates
   the machine account **`lcm-managed`** (idempotent - an existing account is
   replaced).
3. LCM reads the generated password back from the job output and **stores
   URL/login/password in the CrowdSec settings automatically**. The URL points at
   the host's first non-loopback IP, e.g. `http://203.0.113.5:8080`, login
   `lcm-managed`.
4. From now on, managed servers can enroll in **remote mode** with no further
   input: just *Actions → Security tools → CrowdSec → Central LAPI*.

The plaintext password is replaced by `LCM-LAPI-PW: ********` in the job log - it
then exists only encrypted in the settings. (LCM host only, Debian/Ubuntu/apt only.)

:::caution[Make the LAPI port reachable]
In remote mode servers connect to **port 8080** of the LCM host. Open the port in
the [firewall](/en/guides/firewall) for the source IPs of the managed servers -
best via a named [IP allowlist](/en/guides/allowlists). The firewall bouncer in
turn needs `nftables` or `iptables` on the respective host. There is no port
conflict with LCM: LCM itself listens on **9310** (UI/API) by default, while the
LAPI keeps its CrowdSec default **8080**.
:::

## Monitoring the LAPI & connected servers

The **Settings → CrowdSec** page offers, around the central LAPI:

- **Check now** - a login probe from the LCM host (POST `/v1/watchers/login`
  with the stored machine credentials). It distinguishes three states:
  *reachable + login OK*, *reachable but login rejected* (stale credentials)
  and *unreachable*.
- **Monitoring** - LCM recommends an alert rule of type **"CrowdSec LAPI
  unreachable"**; it can be created here with one click. With the rule active,
  LCM checks the LAPI **automatically every 30 minutes** (with the alert
  evaluation) and reports outages via the assigned
  [notification channel](/en/guides/alerts). Without a channel, only the alert
  history is kept.
- **Connected servers** - all servers whose CrowdSec agent reports to the LAPI
  configured here according to its credentials file
  (`/etc/crowdsec/local_api_credentials.yaml`, read live during scans) -
  including connection mode and service status.

## Managing (service, allowlist, bans)

Once a tool is installed, the **Security** tab of the server detail page shows a
management card per tool. It covers exactly what would otherwise require an SSH
session on the machine - and the ban list is typically what you need when you
can **no longer** reach that machine.

| Area | Effect |
| --- | --- |
| **Service** | Start, stop, restart and autostart on/off. Covers systemd, SysV and OpenRC. |
| **Allowlist** | Rewrites `ignoreip` (fail2ban) or the parser whitelist (CrowdSec) and reloads it - without reinstalling. Free-form IPs plus the centrally managed [IP allowlists](/en/guides/allowlists) via multi-select. |
| **Ban list** | Shows the currently banned addresses (fail2ban: jail, CrowdSec: scenario, cause and remaining time) and lifts a ban with one click. |
| **Uninstall** | Removes package, service and configuration (for CrowdSec the bouncer as well). Only after an explicit confirmation. |

Every action runs as a **job** on the server. The card's buttons stay disabled
while the job runs; "completed" is only reported once the job has actually
finished - a failed job is reported as an error. Afterwards state and ban list
are reloaded automatically.

:::note[Lockout protection applies here too]
When applying the allowlist, LCM always adds the IP it currently reaches the
server from - even if the field is left empty. That source IP is read without
`sudo` (`$SSH_CONNECTION`), because `sudo` discards this variable.
:::

:::warning[Not in restricted mode]
All management actions touch services and system configuration and are therefore
blocked in [restricted sudo mode](/en/reference/security-model) - the card says so
instead of running into an error. If the tool is not (or no longer) installed on
the server, the job ends with an error rather than a misleading success message.
:::

## Display & detection

The **hardware scan** detects an already-installed fail2ban/CrowdSec (via
`fail2ban-client` or `cscli`/`crowdsec` on the path) and whether the service is
**active** (both systemd **and** OpenRC are checked). On the server detail page,
**System & Security** shows a **"Security tool"** row with the detected tool and
the active state.

## Selecting allowlists

In the install dialog (fail2ban/CrowdSec) you can, alongside ad-hoc IPs, also
assign the centrally managed **[IP allowlists](/en/guides/allowlists)** via
multi-select - their IPs are added to `ignoreip` (fail2ban) or the parser
whitelist (CrowdSec).

:::note[Allowlist resolution is visible]
If a selected allowlist cannot be resolved when applying, LCM does **not** abort
the protection - the LCM source IP and the manually entered IPs still apply - but
appends a visible **`LCM-WARNUNG`** to the job output. That way nobody assumes
protection that isn't there.
:::

## Distribution coverage

| Package manager | fail2ban | CrowdSec |
| --- | --- | --- |
| apt (Debian/Ubuntu) | ✅ | ✅ (packagecloud.io repo) |
| dnf (RHEL/Rocky/Alma/Fedora) | ✅ | ✅ (packagecloud.io repo) |
| zypper (openSUSE/SLES) | ✅ | ✅ (packagecloud.io repo) |
| apk (Alpine) | ✅ | ✅ (community repo, no extra repo) |
| pacman (Arch) | ✅ | ❌ - LCM reports this honestly (`ErrCrowdSecUnsupported`) |

## SSH 2FA (TOTP alongside the SSH key)

Under **Server → Security** you can additionally enable **SSH 2FA**: SSH
logins then require **key + TOTP one-time code**
(`google-authenticator-libpam`, works with any authenticator app).

What LCM sets up:

- The PAM module is installed and hooked in at the **very top** of
  `/etc/pam.d/sshd` (`pam_google_authenticator.so nullok`); the password
  stack in the auth section is disabled - otherwise a password prompt would
  follow the code. All changes are marker-based and fully reversible on
  removal (backup: `/etc/pam.d/sshd.lcm-backup`).
- An sshd drop-in (`55-lcm-2fa.conf`) sets
  `AuthenticationMethods publickey,keyboard-interactive:pam` - deliberately
  sorted alphabetically **before** the hardening drop-in (OpenSSH takes the
  first value found per option). Pure password logins via SSH are gone after
  this.

**Gentle rollout (`nullok`):** users **without** TOTP set up still get in
with their key - nobody is locked out by enabling the feature. Enrollment is
self-service: run `google-authenticator` on the server and scan the QR code
with the app. The **2FA** column on the
**[Users](/en/guides/linux-users#per-server-user-overview)** tab shows who is
already enrolled.

Two built-in lockout protections:

- The **LCM service user** stays on pure key auth via a `Match` exception -
  LCM's SSH client does not answer keyboard-interactive prompts.
- After enabling, LCM verifies with a **fresh connection** that access still
  works; if it fails, everything is **rolled back** automatically.

Distribution coverage: apt, dnf/yum (RHEL clones need EPEL), zypper and
pacman. The option is not available on **Alpine (apk)** - its sshd is built
without PAM by default. On removal, the users' TOTP secrets
(`~/.google_authenticator`) are kept; they apply again immediately when the
feature is re-enabled.
