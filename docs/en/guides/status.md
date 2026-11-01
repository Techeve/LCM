---
sidebar:
  order: 11
title: Status calculation
description: How LCM determines a server's traffic-light status - all factors, thresholds and special cases in detail.
---

Every server carries one of four statuses: 🟢 **Excellent** (solid green),
🟢 **OK** (light green), 🟡 **Warning** and 🔴 **Critical**. This page
explains exactly how the status is determined. The **ⓘ** next to the status
badge always opens a popover with the concrete reasons ("insights") - for
yellow/red the problems, for **OK** the points still missing for "Excellent".

## Evaluation order

The status is computed fresh from the most recently collected data on every
request, in this order:

### 1. Reachability → 🔴

If the server is **unreachable** (offline, auth/host-key error), it is
immediately **critical** - unless *"unreachability non-critical"* is set for
it (server settings). In that case it keeps its last computed status and is
only greyed out; only after the **grace period** expires (default 28 days,
configurable 1-365) does it become critical after all.

### 2. Red criteria → 🔴

- **At least one critical CVE** (after weighting, see below).
- **Operating system out of vendor support (EOL)** - or less than **one
  month** before support ends.
- **LCM is unable to act:** package management or the connection fails (see
  [Operational capability](#operational-capability)) - a busy dpkg lock only
  from the second consecutive failure.
- **Package state stale (red):** the last successful package refresh is
  older than 7 days (Settings → General).

### 3. Yellow criteria → 🟡

Each of the following makes the server a **warning** (and appears as its own
insight):

| Criterion | Threshold |
| --- | --- |
| High CVEs (weighted) | ≥ 1 |
| Overdue package updates | ≥ 1 |
| In-use Docker images with an available update | ≥ 1 |
| Usage of the root volume `/` | ≥ 85 % |
| System requests a reboot (e.g. after a kernel update) | yes |
| Rules/actions or the CVE scan fail | yes |
| Busy dpkg lock on the first failure | yes |
| A single package source is broken | yes |
| Package state stale (last successful package refresh older than 48 h, configurable) | yes |

### 4. "Excellent" or "OK"

If none of the above signals apply, the server is green. It only reaches
**Excellent** immaculately, when **all three** criteria are met:

1. **Zero counting CVEs** - not a single known vulnerability, not even low
   ones (Docker CVEs of non-relevant containers do not count, see below).
2. **SSH hardening active** (key login only, no root password login).
3. **Firewall (ufw) active** - Proxmox systems bring their own firewall and
   count as covered.

If any of these is missing, the server stays at **OK** - and the ⓘ popover
lists exactly what is still missing.

## Operational capability

For every server LCM keeps track of whether it can still act in four areas:
**connection**, **package management** (package list, updates, security
updates, single packages), **rules and actions** and **CVE scan**. The
"Operational capability" card in the server overview shows for each area
since when it has been failing, how often in a row, the cause and what to do.

- A failure only affects **its own** area. A run that already fails to
  connect counts against the connection, not against package management.
- **Only a success of the same action heals** an area. A successful health
  check therefore does not clear a failed update; a full update does clear
  failed security updates, though.
- A run aborted by the job watchdog counts as a failure, a manually aborted
  one does not. Rejected runs (server busy) do not count.
- "Refresh package list" evaluates the output of `apt-get update`: if all
  sources fail or apt reports a general error, the run fails; if only one of
  several sources fails, it counts as a success with a yellow "package source
  broken" hint.
- For an interrupted dpkg and broken dependencies the card offers a **repair
  button** (`dpkg --configure -a` or `apt -f install`, `zypper verify` on
  zypper). Afterwards **"Try again"** runs the failed action once more - only
  its success resets the area.
- On the dashboard, **"Only servers LCM is unable to act on"** shows all servers
  whose package management or connection failed or whose package state is
  critically stale. The **"Action failed"** alert reports such changes
  immediately (see [Alerts](/guides/alerts/)).

## CVE weighting

For the traffic light and alerts, the raw Trivy severity is weighted by
context; the security views keep showing the raw rating:

- **Docker CVEs do not count at all by default.** Containers are isolated,
  their packages are not directly reachable from outside, and the image
  vendor is responsible for image contents. Only containers **explicitly
  marked as "CVE-relevant" in the Docker tab** count - then at **full
  severity**. The mark is attached to the container name and survives image
  updates and inventory scans.
- **CVEs of exposed packages one level higher** - web servers, reverse
  proxies, mail/DNS servers, databases (list under *Settings → General*) as
  well as automatically detected packages listening on externally reachable
  ports.

:::tip[When the status reports high CVEs that are missing under Security]
That is the visible consequence of the weighting: a *medium* finding on an
exposed service counts as **high** here, while the security overview still
lists it as **medium** - the raw rating is deliberately left untouched there.
Anyone searching for high findings will come up empty and assume the rating is
stuck; rescanning changes nothing, because both figures are correct.

This is why the status explanation names the packages whose findings were
raised:

> Weighted higher because exposed or classified as high: nginx, openssh-server.
> Under Security these findings appear with their original, lower severity.

That resolves the discrepancy in one sentence - and shows immediately which
service the status actually hinges on.
:::

## Special cases

- **Offline-tolerated servers** (*"unreachability non-critical"*) keep their
  last status and appear greyed out until the grace period expires.
- **Demo servers** are never contacted; their status comes from the demo
  data.
- **Proxmox systems** fulfil the firewall criterion automatically
  (pve-firewall); ufw management is locked there.

Related pages: [Servers & monitoring](/en/guides/monitoring/),
[Security & CVE scans](/en/guides/security-cve/),
[Docker monitoring](/en/guides/docker/).
