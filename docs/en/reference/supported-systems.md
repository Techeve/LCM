---
sidebar:
  order: 19
title: Supported systems
description: Which systems LCM and the lcm-agent run on, which package manager delivers them, and what was tested when.
---

This page lists where LCM itself runs, which systems the `lcm-agent` supports and
what has **actually been tested**. "Tested" means: run on a real system or
container, with a date. Everything else is "supported": the code is built for it,
but a test run is still pending.

## LCM host

| System | Status |
|---|---|
| Debian 12, 13 | tested (installation, setup wizard, Trivy sandbox) |
| Ubuntu 22.04, 24.04, 26.04 | tested |
| Docker (amd64, arm64) | supported, image on Docker Hub |
| Windows, macOS | not supported |

Installation details: [Installation](/en/getting-started/installation/).

## Architectures

LCM and the `lcm-agent` exist for **x86-64 (amd64)** and **ARM (arm64)**. Both
architectures are built and published: the `.deb` package and the Docker image of
LCM, and the agent packages for apt, dnf, zypper, apk and pacman.

**All tests on this page ran on x86-64.** Nothing has been tested on real ARM hardware
yet (planned on a Raspberry Pi). No differences are expected, though, since the same
code runs on both architectures. The matrix applies to ARM by analogy, but this is not
verified.

Servers that LCM manages over SSH need no program from us. Their architecture does not
matter. There are no agent packages for 32-bit systems (such as Raspberry Pi OS
32-bit), they can only be managed over SSH.

## Matrix by distribution

Three levels: ✅ **tested** (run on a real system with the current release line),
🟡 **should work** (the code supports it, a test is pending) and ❌ **not
supported**. The same package manager does not mean "tested": Debian and Ubuntu
both use apt but still behave differently around systemd, Bubblewrap and paths.

### apt distributions: server

| Distribution | Host | SSH | SSHE | Agent | CVE |
|---|---|---|---|---|---|
| Ubuntu 22.04, 24.04, 26.04 | ✅ | ✅ | ✅ | ✅ | full |
| Debian 12, 13 | ✅ | ✅ | ✅ | ✅ | full |
| Proxmox VE, Proxmox Backup Server |  | 🟡 | 🟡 | 🟡 | partial¹ |
| TurnKey Linux | 🟡 | 🟡 | 🟡 | 🟡 | full |
| Kali Linux | 🟡 | 🟡 | 🟡 | 🟡 | none |
| Univention Corporate Server (UCS) |  | 🟡 | 🟡 | 🟡 | none |
| Raspberry Pi OS (64-bit) | 🟡 | 🟡 | 🟡 | 🟡 | partial² |
| Raspberry Pi OS (32-bit) |  | 🟡 | 🟡 | ❌ | none |

### Non-apt distributions: client only

Here LCM itself only runs in the Docker container.

| Distribution | Host | SSH | SSHE | Agent | CVE |
|---|---|---|---|---|---|
| Red Hat Enterprise Linux | ❌ | 🟡 | 🟡³ | 🟡 | not verified |
| AlmaLinux 10 | ❌ | ✅ | ✅³ | ✅ | not verified |
| AlmaLinux 9 | ❌ | 🟡 | 🟡³ | ✅ | not verified |
| Rocky Linux 10 | ❌ | ✅ | ✅³ | ✅ | none⁴ |
| CentOS Stream 10 | ❌ | ✅ | ✅³ | ✅ | none |
| Fedora 44 | ❌ | ✅ | ✅³ | ✅ | none |
| Fedora 45 | ❌ | 🟡 | 🟡³ | ✅ | none |
| openSUSE Leap 16 | ❌ | ✅⁵ | ✅³ | ✅ | not verified |
| openSUSE Tumbleweed | ❌ | 🟡 | 🟡³ | ✅ | not verified |
| Arch Linux | ❌ | 🟡⁶ | 🟡³⁶ | ✅ | none |
| Alpine 3.23 | ❌ | ✅⁷ | ✅³ | ✅ | full |
| Alpine 3.22, 3.24 | ❌ | 🟡 | 🟡³ | ✅ | full |

### Desktop distributions

| Distribution | SSH | SSHE | Agent | CVE |
|---|---|---|---|---|
| Ubuntu Desktop 24.04, 26.04 | 🟡 | 🟡 | 🟡 | full |
| Debian 13 Desktop | 🟡 | 🟡 | 🟡 | full |
| Linux Mint | 🟡 | 🟡 | 🟡 | none |
| Zorin OS | 🟡 | 🟡 | 🟡 | none |

The columns:

- **Host:** LCM is installed on the system (`.deb` package or Docker). For
  Proxmox, UCS and the desktop distributions it is left empty: LCM itself does not
  make sense on such systems, they are only managed.
- **SSH:** the server is managed over SSH without an agent, with full privileges.
- **SSHE:** SSH connection in restricted mode. LCM then works with restricted
  privileges on the server. The agent has no such mode, since it runs as a
  root service.
- **Agent:** the server runs `lcm-agent`.
- **CVE:** LCM can run CVE scans on this distribution. The value says how far
  they reach: **full** = all packages of the distribution, **partial** = the
  base distribution's packages but not its own packages, **none** = there is
  no CVE data for the distribution, so a scan says nothing, **not verified** =
  Trivy supports the distribution according to its documentation, but our tests
  found nothing there. Whether the scan works is open.

With **none**, LCM reports no error: the scan ends without findings and looks like
a clean server. Only CentOS Stream reports an error.

¹ Debian packages are covered; Proxmox's own packages (`pve-*`, `proxmox-*`) are not.

² Debian packages are covered; packages from `archive.raspberrypi.com` are not.

³ Updates, firewall and package pinning also work in restricted mode. Package
sources are only managed in restricted mode on apt systems.

⁴ Trivy supports Rocky Linux, but LCM finds nothing there in this version: the
package identifier lacks the architecture.

⁵ With no outdated packages, the effect of the update is only half checked.

⁶ In a container without Landlock, onboarding and package jobs fail, see
[Notes per system](#notes-per-system).

⁷ Package pinning has no effect on Alpine, see [Notes per system](#notes-per-system).

We are testing the 🟡 systems and will update this table.

## lcm-agent

The agent is a statically built program for `amd64` (x86_64) and `arm64`
(aarch64). It comes as a package from repo.techeve.de, set up with one command
on every system:

```sh
curl -fsSL https://repo.techeve.de/setup.sh | sudo sh
```

| Package manager | Systems | Packages since | Update by button since | Tested |
|---|---|---|---|---|
| apt | Debian 12, 13; Ubuntu 22.04, 24.04, 26.04 | 2.0 | 2.4 | 2026-11-24/25 |
| dnf | AlmaLinux 9, 10; Rocky Linux 10; CentOS Stream 10; Fedora 44, 45 | 2.3.2 | 2.4 | 2026-11-25 |
| zypper | openSUSE Leap 16; openSUSE Tumbleweed | 2.3.2 | 2.4 | 2026-11-25 |
| apk | Alpine 3.22, 3.23, 3.24 (OpenRC) | 2.3.2 | 2.4 | 2026-11-25 |
| pacman | Arch Linux | 2.3.2 | 2.4 | 2026-11-25 |

A normal system upgrade (`apt upgrade`, `dnf upgrade`, `zypper update`,
`apk upgrade`, `pacman -Syu`) leaves the agent at its version. It is updated
through LCM, by button in the server view or by a rule in the group. Up to LCM 2.3
you update it by hand on dnf, zypper, apk and pacman systems.

Not supported for the update by button: systems with `yum` but no `dnf`
(CentOS 7, Amazon Linux 2). The agent runs there but is updated by hand.

### What was tested

**apt, 2026-11-24 and 25**, on Ubuntu 22.04, 24.04, 26.04 and Debian 12, 13
(containers on the Proxmox test bed):

- install, enroll, service active, correct version, online in LCM
- a job through the agent, reconnect after an agent restart
- `lcm-agent status` and the runtime directory `/run/lcm-agent`
- on Debian 12 and 13 additionally: agent killed by the kernel (out of memory),
  network blocked for four minutes, server reboot in the middle of an
  installation, `unattended-upgrades` and `needrestart`, update by button from
  2.3.0 to a newer version, late result after an LCM restart

**dnf, zypper, apk and pacman, 2026-11-25:**

- In containers: AlmaLinux 9 and 10, Fedora 45, openSUSE Leap 16 and Tumbleweed,
  Alpine 3.22 to 3.24, Arch Linux. Checked: set up the source, install, package
  and index signatures, tampered packages are rejected, a system upgrade leaves
  the agent alone, installing a specific version, going back to an older version
  after a failed update.
- On the Proxmox test bed: AlmaLinux 10, CentOS Stream 10, Rocky Linux 10,
  Fedora 44, openSUSE Leap 16, Alpine 3.23, Arch Linux. Checked: set up, install,
  system upgrade, enroll against LCM, update by button to a newer version, the
  hold stays in place afterwards.
- Against repo.techeve.de with agent 2.3.2: AlmaLinux 9 and 10, Fedora 45,
  openSUSE Leap 16 and Tumbleweed, Alpine 3.24, Arch Linux.

**Older agents:** agents from 2.0.0 work with current LCM versions. CI checks the
real agents 2.0.0, 2.1.0, 2.2.0, 2.2.1 and 2.3.0 against the current LCM on every
change (command, abort, inventory, console, result after a network outage).

**Not tested yet:** `arm64` on real hardware (planned on a Raspberry Pi). The
arm64 packages are built and published.

### Notes per system

- **openSUSE:** the agent is held by a zypper lock. Before `zypper rm lcm-agent`
  remove the lock: `zypper rl lcm-agent`.
- **Alpine:** `lcm-agent=<version>` in `/etc/apk/world` holds the version. When
  installing by hand, use `apk add lcm-agent=<version>`.
  When managed over SSH, package pinning has no effect because `apk` has no
  "hold". LCM writes this to the job log, the job still ends successfully, and the
  update also raises `apk-tools`.
- **Arch Linux in containers:** if the kernel lacks Landlock, pacman 7 cannot use
  its download sandbox. From LCM 2.4 the agent update retries the download once
  without the sandbox; `setup.sh` does the same.
  When managed over SSH, onboarding (LCM cannot install `sudo`) and all package
  jobs fail on the sandbox in such a container. With `DisableSandbox` in
  `pacman.conf` everything worked. An old Arch image also needs an initialized
  keyring and a full upgrade (`pacman -Syu`) first. On a VM or hardware with
  Landlock the problem is not expected, but untested.
- **CentOS Stream:** Trivy does not support CentOS Stream. The CVE scan reports an
  error about the Trivy database there and returns no result.

## Servers without agent (SSH)

Servers that LCM manages over SSH need no agent. Supported package managers are
apt, dnf, yum, zypper, pacman and apk. Tested are Debian 12 and 13 and Ubuntu
22.04, 24.04 and 26.04 (onboarding, service user, restricted mode, 2026-11-21).

On the Proxmox test bed, AlmaLinux 10, Rocky Linux 10, CentOS Stream 10, Fedora 44,
openSUSE Leap 16, Alpine 3.23 and Arch Linux were added on 2026-11-26 (LCM 2.4.0
beta 1), each in both modes, full and restricted: onboarding, scan, package list,
update, firewall and package pinning. Not tested are AlmaLinux 9, Fedora 45, Alpine
3.22 and 3.24, Red Hat, openSUSE Tumbleweed, package sources in restricted mode and
`arm64`.

## CVE coverage

Measured on 2026-11-26 with Trivy 0.76.2 on package lists with deliberately old
versions per distribution:

- **Findings:** Debian 12 and 13, Ubuntu 24.04, Alpine 3.22 and 3.23.
- **Error instead of findings:** CentOS Stream. Trivy finds no usable Red Hat data
  there.
- **No findings:** Fedora, Arch, Kali, Linux Mint, Zorin OS, UCS and Raspberry Pi OS
  (32-bit). Trivy does not know these distributions.
- **No findings for Rocky Linux**, although Trivy knows it: with the architecture in
  the package identifier, over 900 findings turned up. LCM does not send it in this
  version.
- **No findings, not verified:** AlmaLinux, Red Hat and openSUSE.

Proxmox VE, Proxmox Backup Server, TurnKey Linux and Raspberry Pi OS (64-bit)
identify as Debian. The Debian packages are thus covered, the systems' own packages
are not. This was not tested on these systems themselves.
