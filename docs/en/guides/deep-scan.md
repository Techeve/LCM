---
sidebar:
  order: 13
title: Deep Scan
description: A deeper per-server security check - kernel reboot gap, kernel CVEs and misconfiguration.
---

The **Deep Scan** complements the routine CVE scan with a deeper check that runs
**on the server**. It answers three questions the central Trivy scan alone cannot.

## What the Deep Scan checks

- **Kernel/restart gap** (via `needrestart`): is the **running** kernel older than
  the **installed** one? Then already-applied kernel security fixes only take effect
  after a reboot - until then the server stays exposed. Likewise: which **services**
  still run with outdated libraries and need a restart. Trivy only sees the installed
  version, not the runtime state.
- **Kernel CVEs**: the kernel-related findings from the central Trivy scan,
  highlighted and linked to the reboot gap.
- **Misconfiguration / hardening** (via **Lynis**): a CIS-aligned hardening audit
  with a **hardening index (0-100)** plus warnings and suggestions. If Lynis is not
  installed, the **curated LCM checks** apply (SSH config, sysctl hardening, accounts
  without a password, automatic security updates …) with lower coverage.

:::note[Trivy is the right tool for CVEs - but not for everything]
Trivy checks installed packages (including the kernel package) for CVEs
backport-aware, running centrally on the LCM host. The **runtime gap** (running vs.
installed kernel) and **host hardening** need tools on the target - `needrestart`
and Lynis. The Deep Scan complements Trivy, it does not replace it.
:::

## Install the tools

`needrestart` and `lynis` are not preinstalled on many servers. LCM does **not**
install them automatically: if a tool is missing, the Deep Scan still runs and
honestly reports that part as "not checked". Use the **"Install tools"** button
(server detail → **Deep Scan** tab) to add both tools per package manager
(apt/dnf/zypper/pacman/apk; where a distribution doesn't offer a tool - e.g.
needrestart on Alpine - that is reported).

## Running

- **Single server**: server detail → **Deep Scan** tab → **"Run deep scan"**. The
  run may take ~30-60 s due to Lynis and appears as a job in the history.
- **Whole group**: a group rule of type **"Deep Scan"** (scheduled or triggered
  manually).

The Deep Scan is read-only and therefore also works in restricted-sudo mode.

## Result

The **Deep Scan** tab shows the hardening index, the kernel-related CVEs and the
**reports**.

### Reports instead of one endless list

Every run is stored as its own **dated report** and is kept. The list shows date
and finding counts by severity per run and - this is the point - the difference
to the preceding run:

| Marker | Meaning |
|---|---|
| **+n new** | that many findings were not present in the previous run |
| **−n resolved** | that many have gone since; expanding the run lists them by name |
| **current** | the latest run - it feeds the traffic light, insights and alerts |
| **Tools used** | a run without Lynis covers far less; this explains jumps in the finding count |

Clicking a run opens its findings, grouped by category, with new findings marked
individually as **new**. That answers what a flat list never could: *have I made
progress since last time?* For the very first run both markers stay empty -
there is nothing to compare against.

The **latest 40 runs per server** are kept; log cleanup removes older ones
together with their findings.

The result also feeds the traffic light and alerts:

- a **kernel reboot gap** sets "reboot required" (yellow),
- **hardening/configuration warnings** turn the server yellow - plain **Lynis
  suggestions** deliberately stay informational (no "all yellow"),
- the alert rule **"Deep Scan: findings"** actively reports warnings/kernel reboot.

## When the "root login permitted" finding will not go away

The deep scan reads the root login via `sshd -T` - the **effective**
configuration, not the contents of one particular file. If it reports the
access as open even though "disable root login" is set in LCM, the scan is
right and the setting genuinely does not take effect.

The reason is a peculiarity of sshd: with multiple definitions, the **FIRST**
one wins - unlike almost every other configuration. If `/etc/ssh/sshd_config`
contains a `PermitRootLogin yes` *before* the `Include` of the drop-ins, LCM's
drop-in (`10-lcm-ssh.conf`) stays without effect, even though it was written
correctly and accepted by `sshd -t`. Many cloud and hosting images ship exactly
that line.

**LCM now resolves this itself.** When disabling the root login it verifies the
effect; if the setting does not take hold, it silences the offending lines in
the main file with a marker:

```
#LCM-STILLGELEGT# PermitRootLogin yes
```

It then verifies again - only this second attempt decides between success and
an error message. When the root login is **re-enabled**, LCM undoes the change
and restores the file byte for byte; that is what the marker is for.

The main file is only touched when it demonstrably stands in the way: a
`PermitRootLogin` line *after* the include has no effect anyway and is left
alone. Lines that were already commented out, and the drop-ins themselves,
also stay untouched. Every change runs with a backup, `sshd -t` and rollback -
if sshd rejects the modified file, the previous state is restored immediately.
Whatever was silenced appears with its line number in the job log.
