---
sidebar:
  order: 22
title: MikroTik RouterOS
description: Monitor RouterOS devices - focused on operating-system version currency.
---

LCM supports **MikroTik RouterOS** as its own device type. Unlike Linux servers,
there is **no package management** and no POSIX shell here - RouterOS ships its
own CLI that accepts individual commands over SSH exec. Firewall management, CVE
scanning, package sources and user sync are therefore **not possible**. LCM
focuses on what matters: the **currency of the RouterOS version**.

## What is monitored

- **Version currency**: via `/system package update check-for-updates` the router
  itself reports whether a newer version of its channel is available. LCM compares
  `installed-version` with `latest-version`; if they differ, an update is
  available and the status light turns **yellow** - with the finding *"A newer
  RouterOS version is available (x.y.z) - update recommended"*. A current,
  reachable device reaches the **top grade**.
- **Basic inventory** from `/system resource print` and `/system routerboard print`:

  | Field | Source (RouterOS CLI) |
  | --- | --- |
  | Version + channel | `version` (e.g. `7.15.3 (stable)`) or `check-for-updates` |
  | Model / board | `routerboard model`, else `board-name`/`platform` |
  | Architecture | `architecture-name` (e.g. `arm64`, `x86_64`) |
  | CPU / cores | `cpu`, `cpu-count` |
  | RAM (total/used) | `total-memory`, `free-memory` |
  | Storage (total/used) | `total-hdd-space`, `free-hdd-space` |

The **channel** (stable / long-term / testing) is taken from the parenthesized
version or the `channel` field.

Firewall activity, SSH hardening and package CVEs do **not** factor into the grade
for RouterOS - LCM cannot manage those areas there and therefore does not count
them as shortcomings.

## Adding a device

*Add server → mode **MikroTik RouterOS***. **Name**, **host/port** (default SSH
port 22) and a (preferably read-only) **RouterOS user** are enough. After
confirming the host-key fingerprint (MitM protection, trust-on-first-use), LCM
connects read-only. Two authentication methods:

- **Password**: LCM connects immediately, reads version and inventory, and adds
  the device **online**. The password is stored AES-GCM encrypted. If LCM detects
  no RouterOS (no `/system resource print` result), onboarding aborts with a clear
  hint.
- **Public key**: LCM generates a key pair and shows the public key. The device
  stays **offline** at first until you import the key on the router:

  ```
  /user ssh-keys import public-key-file=lcm.pub user=<user>
  ```

  The next refresh then connects and the device goes online.

:::tip[Create a read-only user]
LCM only needs read rights on the router. A dedicated user in the built-in `read`
group is enough:

```
/user add name=lcm group=read password=<strong>
```

For key login afterwards, import the public key shown by LCM onto this user (see
above).
:::

## What is disabled on RouterOS

| Function | On RouterOS | Why |
| --- | --- | --- |
| Version monitoring | ✅ | RouterOS self-check `check-for-updates` |
| Basic inventory | ✅ | `/system resource`/`routerboard print` |
| Refresh (re-scan) / remove | ✅ | monitoring only |
| Firewall management | ❌ | no ufw/firewalld/nftables - RouterOS has its own firewall |
| CVE scanning | ❌ | no package inventory / SBOM basis |
| Package sources (repos) | ❌ | no Linux package manager |
| Package updates | ❌ | RouterOS updates the system as a whole |
| User sync | ❌ | no POSIX users / `/etc/passwd` |
| SSH hardening | ❌ | no `sshd_config` / no root shell |
| Security tools (fail2ban/CrowdSec) | ❌ | no package manager |
| Set DNS | ❌ | no `/etc/resolv.conf` / systemd-resolved |

These functions are hidden in the UI or rejected server-side - they require a Linux
package manager or a root shell, which RouterOS does not provide. The RouterOS
commands run deliberately **raw** (no `sudo`/`sh -c`) over the RouterOS CLI.
