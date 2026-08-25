---
sidebar:
  order: 23
title: Synology DSM
description: Monitor Synology NAS devices via the DSM web API - version, updates, packages, storage, time and Security Advisor.
---

LCM supports **Synology DSM** as its own device type - connected through the
**DSM web API**, not over SSH.

## Why the API instead of SSH

DSM is Linux-based, but it is not a manageable Linux server:

- There is **no `/etc/os-release`** - LCM's operating-system detection would
  come up empty.
- The kernel is an **old fork maintained by Synology** (e.g. 4.4.x). A CVE scan
  against it would produce false alarms by the dozen: the security fixes live
  in Synology's own builds, not in the version numbers a CVE database compares
  against.
- Packages are managed by **`synopkg`**, not apt or dnf.
- **Users, services and the firewall are managed by DSM itself.** An LCM service
  user with `sudo` would sit next to DSM's own configuration management - and
  might be overwritten by the next DSM update.

The documented web API, by contrast, delivers exactly what matters for
monitoring - and it does so the way DSM itself sees things.

## What LCM collects

| Area | Contents |
| --- | --- |
| **System** | model, DSM version, serial number, CPU cores, RAM, uptime |
| **Updates** | whether a newer DSM release is available (and which) - the central status criterion |
| **Packages** | installed DSM packages with version (visible in the packages tab) |
| **Storage** | volumes: total size, usage and health - feeds the disk status and the [storage history](/en/guides/monitoring/) |
| **Time** | time zone and NTP state including the time server (see [Time & NTP](/en/guides/time/)) |
| **Security** | the findings of the **DSM Security Advisor** (levels *risk* and *danger*), broken down by category |

For the Security Advisor, LCM deliberately adopts DSM's own assessment instead
of rebuilding it without shell access - Synology knows best what counts as a
misconfiguration on a DSM.

## What does not work on DSM

Firewall management, CVE scan, repositories, package updates, user sync, SSH
hardening, restricted mode and script/custom actions are **blocked** - they
require a shell or a package manager. Called through the API, LCM answers with
a message that names the reason (HTTP&nbsp;409) instead of running into a
follow-up error. Inside a server group such rules are **skipped by name** on
DSM devices - a mixed schedule therefore stays green.

**Health check and system sync** do run: on an API device they re-collect the
device state. That is the factual equivalent of an availability ping there, and
it keeps status, update state and storage history current.

## Adding a device

1. In DSM, create a **dedicated account for LCM**: *Control Panel → User &
   Group*, member of the **administrators** group.

   :::caution[Two-factor authentication]
   This account must **not have 2FA enforced** - an unattended scan cannot enter
   a one-time code. Secure it instead via *Control Panel → Security → Account*
   with an **IP restriction to the LCM host**.
   :::

2. In LCM click **“+ Add server”** and choose the **Synology DSM** mode at the
   top.

3. Enter name, host, **DSM port** (default `5001`), account and password, then
   click *Next*.

4. LCM shows the **SHA-256 fingerprint of the TLS certificate**. Compare it in
   DSM under *Control Panel → Security → Certificate* and confirm.

   DSM ships a self-signed certificate - there is no chain to verify here. LCM
   pins this fingerprint and **aborts the connection if it ever changes**
   (protection against man-in-the-middle, exactly like the SSH host key).

5. After adding, LCM collects the state immediately and shows the device online.

The password is stored **AES-GCM encrypted** (like all credentials in LCM) and
is used solely to authenticate against the DSM API.

## Status

Without package and CVE visibility, two criteria carry the assessment:

- **A newer DSM release is available** → yellow, with the version.
- **Security Advisor findings** (risk/danger) → yellow, with count and
  categories.

On top of that come the general criteria that apply to every server:
reachability, disk usage and storage forecast.

:::note[Certificate renewed?]
If you renew the DSM certificate (e.g. via Let's Encrypt), the fingerprint
changes and LCM reports the device as unreachable - with exactly that reason.
Add the device once more to confirm the new fingerprint.
:::
