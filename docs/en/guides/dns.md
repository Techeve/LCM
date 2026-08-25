---
sidebar:
  order: 19
title: Set & test DNS
description: Set up to three nameservers per server and check DNS availability.
---

LCM can set up to **three nameservers** per server and check **DNS availability** -
as a server action and as a group rule. Setting is deliberately **self-guarding**:
if name resolution breaks after writing, LCM rolls the change back automatically.

## Maintain the defaults

Under *Settings → DNS* two lists are maintained:

- **Preset nameservers** - offered as a choice when setting a server’s DNS. One
  entry per line, either `Label = IP` (e.g. `Cloudflare = 1.1.1.1`) or just the IP.
- **Test domains** - the domains whose resolvability the DNS test checks on the
  server (one per line).

Empty fields = the built-in default lists:

| Preset nameservers (default) | Test domains (default) |
| --- | --- |
| `Cloudflare = 1.1.1.1` | `deb.debian.org` |
| `Cloudflare (2) = 1.0.0.1` | `github.com` |
| `Google = 8.8.8.8` | `cloudflare.com` |
| `Google (2) = 8.8.4.4` | |
| `Quad9 = 9.9.9.9` | |

:::tip[Internal resolvers as presets]
If you run your own resolvers (e.g. Pi-hole/Unbound on the LAN), add them as
presets - then they're one click away when setting DNS:

```
LAN resolver = 10.0.0.53
LAN resolver (2) = 10.0.0.54
```
:::

## Set DNS on a server

On the server detail page via the **gear → "DNS servers" section**: enter up to
three nameservers (free IP **or** a choice from the presets) and **"Apply DNS"**.

- **systemd-resolved** active → LCM writes the drop-in
  `/etc/systemd/resolved.conf.d/lcm-dns.conf` and restarts the service:

  ```ini
  [Resolve]
  DNS=1.1.1.1 9.9.9.9
  ```

- otherwise → LCM writes `/etc/resolv.conf` (with a `*.lcm-bak` backup; an
  existing symlink is removed first):

  ```text
  nameserver 1.1.1.1
  nameserver 9.9.9.9
  ```

- After writing, LCM verifies resolution (against the first maintained test
  domain, falling back to `deb.debian.org`). **If it fails, the change is rolled
  back automatically** - drop-in deleted or backup restored, the server stays
  operational. The job then ends with a clear error.
- **All fields empty + Apply** = LCM releases DNS management again (drop-in
  removed / backup restored).

At most **three** nameservers can be set; each must be a valid IPv4 or IPv6
address (not a hostname).

### What happens on broken resolution (example)

You accidentally set `10.0.0.99` (a resolver not reachable from this server):

1. LCM writes the drop-in or `/etc/resolv.conf` and restarts systemd-resolved if
   applicable.
2. The resolution test against `deb.debian.org` fails.
3. LCM discards the new file, restores the backup and restarts the service.
4. Job outcome: *"DNS test after setting failed - rolled back"* (exit 1). The
   server keeps using its previous resolvers.

:::note[NetworkManager / netplan / Proxmox]
When DNS is managed by NetworkManager, netplan or (on Proxmox VE) the host network
configuration, a directly written config may be overwritten later. The resolution
test guards against broken name resolution; **permanent** maintenance then belongs
in the respective network tool.
:::

:::caution[Restricted sudo mode]
In restricted sudo mode it is not an inline script but the validating
`lcm-helper` that writes the DNS configuration - with the **same** logic including
resolution test and rollback. RouterOS devices know neither `/etc/resolv.conf` nor
systemd-resolved: there LCM cleanly rejects the DNS action instead of sending a
Linux shell script into the RouterOS CLI.
:::

## DNS test

The test checks read-only (`getent` / `nslookup`) whether the maintained test
domains resolve on the server. Three-way result:

| Status | Meaning |
|---|---|
| **full** (green) | all test domains resolved |
| **partial** (yellow) | some resolved, others not |
| **no resolution** (red) | no test domain resolvable |

The test is read-only and needs **no** sudo - so it also runs in restricted mode.

Trigger:

- **Automatically on every scan** - the active DNS configuration is read and
  the resolution test runs along with it. This applies to **all** scan paths:
  *Refresh hardware*, *Refresh everything*, the scan when onboarding a server
  and the scheduled **system sync**. Read-only and without `sudo`, so it costs
  nothing.

  :::note[Current without lifting a finger]
  For a long time the DNS check ran only on a manual refresh. On servers touched
  exclusively by the scheduled sync the DNS data stayed empty forever - which
  looks as if the check does no DNS test at all. Since it is part of every scan
  path, the finding is as current as the last contact everywhere.
  :::
- **Server action** - server detail → *Actions → DNS test*.
- **Group rule** - add a rule of type **"DNS test"** to a group (scheduled or
  manually triggerable); it uses the centrally maintained test domains.

The result appears on the server detail page under **System & Security** next to
the firewall - along with the server’s **currently active DNS** (for
systemd-resolved the real upstreams via `resolvectl`, otherwise the `nameserver`
entries from `/etc/resolv.conf` without the `127.0.0.53` stub). The exact
per-domain outcome is in the job output and the status tooltip, e.g.:

```text
OK: github.com, cloudflare.com | FAIL: deb.debian.org
```

This example would yield status **partial** (yellow) - a hint that a resolver
only serves part of the zones, or that a domain is (temporarily) not resolvable.
