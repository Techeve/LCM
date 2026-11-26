---
sidebar:
  order: 21
title: SSH connection
description: Connect servers to LCM the classic way over SSH - the three access variants (root, sudo, su), what LCM sets up, restricted mode and the pitfalls per distribution.
---

With the classic connection, **LCM itself connects to the server over SSH**. No
program of its own runs on the server, only the SSH service that is there anyway.
This page explains how to add a server this way, which access variants exist,
what LCM sets up on the server and what to watch for on each distribution.

Whether SSH or the agent is the right way is covered under
[Agent or SSH?](/en/guides/remote/#agent-or-ssh). In short: SSH fits when LCM can
reach the server over the network. If it sits behind NAT or you do not want an
open SSH port, use the agent.

## How the onboarding works

On the dashboard, choose **"+ Add server"** and the connection type **SSH**. The
dialog has three steps:

1. **Connection.** You enter:
   - **Name** (display name in LCM),
   - **Host name or IP** (inside LCM's Docker container `localhost` is not
     allowed, it would be the container itself - use the network address of the
     host),
   - **SSH port** (default 22),
   - **Login user** (default `root`),
   - **Sign-in:** **Password** or **System SSH key**,
   - the switch **Restricted rights (recommended)**, see
     [Restricted mode](#restricted-mode).

   User and port that the dialog pre-fills are set under *Settings → General*
   (sub-card **Defaults for new servers**).

   **"Next: check host key"** takes you on.
2. **Host key.** LCM reads the server's key, still without credentials, and shows
   the **fingerprint**. Compare it on the server (at the console) and confirm with
   **"It matches – continue"**:

   ```sh
   ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub
   ```

   Depending on the key type the file is called `…_ecdsa_key.pub` or
   `…_rsa_key.pub`; the dialog names the right one.

   LCM remembers this key and aborts every later connection if it changes
   (protection against interception). If the server was rebuilt you expect a new
   key - that is what **Reconnect** is for
   ([below](#afterwards-reconnect-renew-the-key-remove)).
3. **Add.** LCM shows what it will do on the server (*What LCM does on … now*) and
   starts with **"Add server now"**. The full output is in the server's job
   afterwards. If the onboarding fails, the log is part of the error message.

LCM uses the **login password** only for this one setup. It is not stored and
never goes through the command line but through stdin, so it does not appear in
the log either. After that LCM signs in only with its own key.

## The three access variants

The **login user** is the access LCM uses to get in **once**. It needs root
rights, because LCM creates a dedicated account on the server (see
[What LCM sets up](#what-lcm-sets-up-on-the-server)). There are three ways to
get there. You do not pick one in the dialog: LCM tries them itself.

| Variant | Login user | How does it become root? |
| --- | --- | --- |
| **1. Directly as root** | `root` | It already is. |
| **2. User with sudo** | e.g. `admin` | via `sudo`, with its password or without (`NOPASSWD`) |
| **3. User who switches to root** | e.g. `admin` | via `su` or `login`; works only in narrow cases, see below |

### How LCM becomes root

If the login user is not `root`, LCM runs harmless probes one after the other
(they only start `true`). **The first one that works wins.** The result of each
attempt is recorded in the onboarding log.

| No. | Attempt | Probe | Meant for |
| --- | --- | --- | --- |
| 1 | Login user is `root` | no probe needed | Variant 1 |
| 2 | sudo with the login password | `sudo -S -p '' sh -c true`, password via stdin (only if you entered one) | Variant 2 |
| 3 | sudo without a password | `sudo -n sh -c true` | Variant 2 with `NOPASSWD` |
| 4 | su without a password | `su root -c true` with empty input | Variant 3: root without a password, or `pam_wheel` with trust |
| 5 | su with the **login password** | `su root -c true`, login password via stdin | Variant 3: root has the **same** password as the login user |
| 6 | `login` without a password | `login root` | root has no password but `su` is blocked (e.g. by group) |
| 7 (from LCM 2.4) | `login` with the **login password** | `login root`, login password via stdin (only if you entered one) | Variant 3: root has the **same** password as the login user, but `su` is blocked (e.g. by group) |

If LCM finds no way, the onboarding stops before anything was created on the
server. The message starts with *keine Root-Rechte für die Provisionierung* (no
root rights for provisioning) and says what you can change.

### Variant 1: directly as root

LCM signs in as `root` and sets everything up without a detour. This is the
simplest way. Two ways to get in:

- **By key (recommended).** Choose **System SSH key**. LCM created a key pair of
  its own at first start. You find the **public part** under *Settings → General*
  (**Onboarding** card, **Onboarding key** sub-card, with ready-made commands for
  Debian/Ubuntu, the RHEL family and other systems). Put it into
  `/root/.ssh/authorized_keys` on the new server. This works with the defaults of
  most distributions (`PermitRootLogin prohibit-password`: root may come in with
  a key, just not with a password), without changing the SSH configuration.
- **By password.** Choose **Password** and enter the root password. This requires
  that root may sign in over SSH with a password. Most distributions forbid that
  out of the box (see [Distributions](#distributions)). How to open root login
  only for the onboarding is described in the
  [Quick start](/en/getting-started/quickstart/) (step 2, collapsible section).

If `sudo` is missing on the server, LCM installs it (see below).

### Variant 2: user with sudo

The login user may become root via `sudo`. This is the normal case on Ubuntu and
on the cloud images of most distributions.

- **With a password:** enter user and password. LCM hands the password to
  `sudo -S` via stdin.
- **Without a password (`NOPASSWD`):** you usually choose **System SSH key**, with
  the key in the user's `~/.ssh/authorized_keys`. If the user gets in without a
  password you can also leave the password field empty. LCM then tries
  `sudo -n`. Cloud images (users such as `ubuntu`, `debian`, `fedora`) usually
  come with exactly this combination.

With **System SSH key** there is no password LCM could pass to `sudo`. The user
then needs `NOPASSWD`, otherwise the onboarding fails.

`sudo` must also work for the user without a terminal. If the sudoers
configuration contains `Defaults requiretty` (older RHEL systems), `sudo` rejects
LCM's call. Remove the line or exempt the login user.

### Variant 3: user who switches to root with su or login

Here LCM gets in with an ordinary user and switches to root with `su` or
`login`. **LCM supports this only in narrow cases:**

- **su without a password** (attempt 4): root has no password, or `pam_wheel`
  lets the user through without one.
- **su with the login password** (attempt 5): LCM gives `su` the password you
  entered in the dialog for the **login user**. But `su` asks for the password of
  **root**. So it only works if root has the **same password** as the login user.
- **login without a password** (attempt 6): `su` is blocked (e.g. by group), but
  the `login` program has its own PAM policy and lets root in because root has no
  password.
- **login with the login password** (attempt 7, from LCM 2.4): like attempt 5, but through
  `login`. It helps when `su` is blocked but root has the **same password** as the
  login user.

**There is no separate field for a different root password.** If your user has a
different password than root (the usual case, for example on a Debian installed
with a root password), variant 3 fails. This is what to do then:

1. **Better: set up sudo** and use variant 2. At the console, as root:

   ```sh
   su -
   apt install sudo               # Debian; otherwise the system's package manager
   usermod -aG sudo user          # group sudo (Debian, Ubuntu), on RHEL: wheel
   ```

   The user signs in again afterwards so the group takes effect.
2. **Or get in as root by key** (variant 1): put the system SSH key into
   `/root/.ssh/authorized_keys`.
3. **Or** give root the login user's password, only for the onboarding, and change
   it back afterwards. This is the least pleasant way.

Because the probe calls `su` without a terminal and feeds the password via stdin,
attempt 5 depends on your `su` accepting that (true for util-linux). This is not
verified for BusyBox `su` (Alpine) - use root or sudo there. Attempt 7 also feeds
the password via stdin; it never appears on the command line or in the onboarding
log.

### Sign-in: password or system SSH key

| Login user | Password | System SSH key |
| --- | --- | --- |
| `root` | works if root may sign in over SSH with a password | works; key in `/root/.ssh/authorized_keys` |
| User with `sudo` | works, `sudo` gets the password | works only with `NOPASSWD` |
| User with `su` or `login` | only if root password = login password | only without a password (su or login) |

If the server has turned password sign-in off (for example after **Harden SSH**),
LCM reports that the server does not offer password sign-in. Choose **System SSH
key** then.

## What LCM sets up on the server

With the login user's rights, LCM creates a **dedicated service user**. The login
user is not needed afterwards.

| What | Where | Content |
| --- | --- | --- |
| **Account** `lcm-svc` | `/home/lcm-svc`, mode 700 | Shell `/bin/bash`, without Bash `/bin/sh`. Created with `useradd`, on BusyBox systems with `adduser`. |
| **Key** | `/home/lcm-svc/.ssh/authorized_keys` | exactly **one** key: a **new ed25519 pair for this server only**. The private part stays encrypted in LCM. |
| **sudo rule** | `/etc/sudoers.d/lcm-svc`, mode 0440 | Full mode: `lcm-svc ALL=(ALL) NOPASSWD:ALL`. Restricted mode: a whitelist only. |
| **sudo** itself | package | installed only if missing (see below) |

Details:

- **Locked password field.** A fresh account is locked for passwords (`!`). Some
  sshd configurations (`UsePAM no`, for example on openSUSE) then reject the key
  too. So LCM sets an unusable password (`*`): the key works, password sign-in
  stays impossible. A password that is already set is left alone.
- **Installing sudo.** If `sudo` is missing, LCM installs it with the system's
  package manager (apt, dnf, yum, zypper, pacman or apk). This mostly concerns
  Debian, Proxmox (VE, PBS, PDM), Arch and Alpine. It applies to variants 1 and 3.
  With variant 2 `sudo` is already there. On Proxmox `/etc/sudoers.d/` exists
  without the program being installed; LCM therefore checks for the program, not
  the directory. **LCM never removes `sudo` again**, not even when rolling back a
  failed onboarding: other sign-ins may depend on it by then. What was installed
  is in the onboarding log.
- **Function test.** In full mode LCM signs in with the new key and runs
  `sudo -n id -u`. If the result is not `0`, the onboarding has failed. Typical
  cause: a later entry in `/etc/sudoers.d/` that excludes `lcm-svc` again, or a
  `sudoers` file that does not include the directory (missing
  `#includedir /etc/sudoers.d`).
- **Package manager.** LCM handles apt, dnf/yum, zypper, pacman and apk. On
  systems with anything else it does not add the server, rather than show a server
  without package inventory as healthy.
- **Rollback on errors.** If the onboarding fails after the account was created,
  LCM removes account, sudoers file and helper again. Nothing is left behind.

After that LCM reads the system (packages, sources, users, containers, storage)
and puts the server into the **System group** so health checks and sync run
right away. In operation LCM runs jobs as `lcm-svc`, in full mode as
`sudo sh -c '…'`.

### Afterwards: reconnect, renew the key, remove

- **Renew key** (server action): LCM generates a new pair, installs it, checks
  the sign-in with it and then withdraws the old one.
- **Reconnect:** for a server that was rebuilt or whose credentials changed. The
  dialog is the same as for the onboarding. Groups, user assignments and job
  history stay. For a server LCM currently reaches normally, LCM rejects the
  reconnect; the exception is restricted mode (see below).
- **Remove:** with the switch **Also delete the LCM access on the server**, LCM
  removes `authorized_keys`, the sudoers file and the account from the server when
  you remove it. Without the switch the server only disappears from LCM and the
  access stays.
- **Block root login** (server action): if you opened root only for the
  onboarding, this closes it again. LCM itself is not affected, it signs in as
  `lcm-svc`.

More on SSH hardening is under [Security tools](/en/guides/security-tools/) and in
the [Security model](/en/reference/security-model/).

## Restricted mode

In full mode `lcm-svc` gets `sudo` for everything. In **restricted mode** it gets a
fixed **whitelist** instead and no root shell.

### When it makes sense and when it does not

For most servers restricted mode is the right choice, which is why we
recommend it. A server that LCM keeps up to date, monitors and secures does not
need a root shell for LCM. Full mode pays off where LCM should do more than
upkeep, or where a restriction gains little.

| Server | Mode | Why |
| --- | --- | --- |
| Web, file and database servers with updates, reboot if needed and firewall by rule | restricted | LCM needs no more than package management, firewall and reboot there. |
| Servers with sensitive data or audit requirements | restricted | What LCM may do is fixed and traceable for auditors. |
| Servers with custom maintenance scripts: database dump before the update, deployment, reloading a service | full | Scripts and custom actions need a root shell. |
| Docker host with Compose projects that LCM updates by rule | restricted | Compose updates run via `docker` on the whitelist (from LCM 2.4). |
| Docker host with standalone containers without Compose that LCM should update by rule | full | The *Update container* rule needs the Docker socket directly. |
| Test, development and lab servers where many people have root anyway | full | The restriction gains little security there but costs features. |
| Servers on which LCM should set up SSH 2FA | full | The setup needs a root shell. |
| Distribution on which the mode is not tested yet | full, until the test is done | See [Supported systems](/en/reference/supported-systems/). |

A group may mix both kinds. A rule that a restricted server may not run is
skipped for that server with a note in the job instead of failing, so the
schedule stays green.

**What keeps working:** package management (apt, dnf, yum, zypper, pacman, apk,
snap), Docker (scans, Compose updates, pull, pruning and unused images; Compose updates and pull from LCM 2.4), ufw, the Deep Scan audit tools and the **LCM helper**. The helper is
a strictly validating program installed by LCM for everything that needs root and
is not on the whitelist: repositories, APT cache, SSH configuration, user sync,
firewalld and nftables, fail2ban, CrowdSec, reboot.

**What is blocked:** custom scripts, custom actions and SSH 2FA. They need a root
shell. Also the Docker rule *Update container*: it recreates containers directly
via the Docker socket, which the restricted user cannot reach.

**What it does not do:** it does not protect against someone who steals the key of
`lcm-svc`. Package management and Docker run code as root by design, and `sudo`
cannot reliably filter their arguments. The mode shrinks the attack surface against
operator mistakes and makes it traceable what LCM may do. Details:
[Security model](/en/reference/security-model/#restricted-mode-of-the-management-user).

**How to switch it on:** in the dialog the switch **Restricted rights
(recommended)** - it is *off* when the dialog opens - or later via the server
action **Restrict privileges**. After setting up, LCM checks as `lcm-svc` that the
helper and the package manager are really reachable. If the check fails, LCM
restores full mode and reports the error. You do not end up with a server whose
core functions are dead.

**The way back:** the switch by click is a one-way step, because `lcm-svc` itself
no longer has the rights to get root. **Restore full rights** shows you the
commands to run on the server yourself; then you run **Reconnect** without the
switch. **Reconnect** with the login password alone also leads back.

Which distributions support the mode is in
[Supported systems](/en/reference/supported-systems/) (column **SSHE**). The table
there records what is tested. In restricted mode LCM manages package sources only
on apt systems. LCM itself compensates for distribution quirks: sudo's
`secure_path` (RHEL 10 and clones), `targetpw` (openSUSE), `pacman-key` (Arch), the
path of `apk` and the missing `/usr/local/sbin` (Alpine).

## Preparing SSH on the target server

Before the onboarding an SSH service must be running and reachable for LCM.

**1. Is an SSH server installed and started?**

| System | Service name | Check |
| --- | --- | --- |
| Debian, Ubuntu | `ssh` | `systemctl status ssh` |
| RHEL family, Fedora, openSUSE, Arch | `sshd` | `systemctl status sshd` |
| Alpine (OpenRC) | `sshd` | `rc-service sshd status` |

If no SSH server is installed (for example on Linux Mint, Arch or Alpine), install
it with the package manager (`openssh-server` on Debian systems, otherwise
`openssh`) and start the service.

**2. Does LCM reach the port?** The server's firewall must have the SSH port open
for the address of the LCM host.

**3. What really applies?** `sshd` takes, for each option, the value found
**first**. The files in `/etc/ssh/sshd_config.d/` come first (alphabetically) if the
main file includes them at the top, then the rest of the main file. A drop-in named
`00-…` beats `50-…`. Do not check the files but the **effective** configuration:

```sh
sudo mkdir -p /run/sshd
sudo sshd -T | grep -iE 'permitrootlogin|passwordauthentication|pubkeyauthentication'
```

`mkdir -p /run/sshd` is needed when `sshd` is started via a socket and the
directory exists only for the lifetime of a connection. Whether the main file
includes the drop-ins at all you see here:

```sh
grep -i '^Include' /etc/ssh/sshd_config
```

**4. After every change** test first, then restart:

```sh
sudo sshd -t
sudo systemctl restart ssh       # Debian, Ubuntu
sudo systemctl restart sshd      # RHEL family, Fedora, openSUSE, Arch
sudo rc-service sshd restart     # Alpine
```

On newer Debian and Ubuntu versions `ssh.socket` accepts the connections. A
**restart** of the service (`restart`, not `reload`) makes sure the new
configuration applies.

## Distributions

The columns show the **starting point with the distribution's defaults**. This is
what you typically meet. Your server may differ (installer choices, cloud image,
hosting provider).

| Distribution | 1: root | 2: user with sudo | 3: user with su |
| --- | --- | --- | --- |
| **Ubuntu** | by key only (root locked, no password) | normal case (group `sudo`) | does not work (root has no password) |
| **Debian** | by key only (password for root over SSH blocked) | `sudo` often not installed, set it up first | only if root and login password are equal |
| **RHEL, Alma, Rocky, CentOS Stream, Fedora** | by key; password blocked in current versions | normal case (group `wheel`) | only with the same password |
| **openSUSE** | check sshd (drop-ins, Leap 16) | `sudo` asks for root's password (`targetpw`) | only with the same password |
| **Arch Linux** | set up `openssh` first, then by key | `sudo` missing at first | only with the same password |
| **Alpine** | set up `openssh` first, then by key | `sudo` missing (LCM does not know `doas`) | uncertain (BusyBox `su`) |
| **Proxmox VE, PBS** | the normal way (account root) | `sudo` missing, LCM installs it | only with the same password |

**Tested** is the SSH connection, according to
[Supported systems](/en/reference/supported-systems/), on **Debian 12 and 13 and
Ubuntu 22.04, 24.04 and 26.04** (onboarding, service user, restricted mode). On
**AlmaLinux 10, Rocky Linux 10, CentOS Stream 10, Fedora 44, openSUSE Leap 16, Alpine
3.23 and Arch Linux** a test with root password login (variant 1) ran on 2026-11-26,
in both modes. Variants 2 and 3 (`sudo`, `su`) are not tested there. For all other
distributions the code is prepared but a test run is still pending. The notes
below for them come from the defaults of the respective distribution and from how
LCM behaves, unless a section names a test run.

### Ubuntu (22.04, 24.04, 26.04) - tested

**Starting point:** root has no password. The first user is in the group `sudo`
and needs their password for `sudo`. `sshd` runs with
`PermitRootLogin prohibit-password`.

**Recommendation:** variant 2. Enter user and password, done. On cloud images
(user `ubuntu`, `sudo` without a password, sign-in by key only) choose **System SSH
key** with the key in the user's account.

**Pitfalls:**

- Anyone who wants variant 1 with a password first has to set a root password
  (`sudo passwd root`) **and** set `PermitRootLogin yes`. Better: root by key.
- On cloud images root's key often has a `command=` entry in front that asks you to
  sign in as `ubuntu`. Sign in as that user.
- `50-cloud-init.conf` in `sshd_config.d` can turn password sign-in on or off. A
  drop-in of your own with `00-` in front wins.
- The service is called `ssh`. Newer versions use `ssh.socket`; after a change run
  `systemctl restart ssh`.
- Ubuntu 26.04 ships `sudo-rs` instead of `sudo`. LCM accounts for that (it removes
  `!requiretty` from the whitelist file in that case).

### Debian (12, 13) - tested

**Starting point:** the installer asks for a root password. If you set one, the
first user does **not** get `sudo`, and `sudo` is not even installed. If you leave
the field empty, the user becomes sudo-capable and root is locked. `sshd` allows
root by key only.

**Recommendation:**

- With `sudo` set up: variant 2.
- Without `sudo`: root by key (variant 1) **or** add `sudo` at the console
  (commands above under
  [variant 3](#variant-3-user-who-switches-to-root-with-su-or-login)). Variant 3 works only
  with the same password.

**Pitfalls:**

- If `sudo` is missing, LCM installs it itself **as soon as it has root** (variant
  1 or 3). As a user without `sudo` you cannot use variant 2.
- So that `useradd` is found, LCM sets the search path itself on the `su` route (a
  `su` without `-` has no `/usr/sbin` in the path on Debian).
- Debian 13 uses `ssh.socket`. After a change run `systemctl restart ssh`.
- The service is called `ssh`.

### RHEL, AlmaLinux, Rocky Linux, CentOS Stream, Fedora - partly tested

**Status:** Tested on 2026-11-26 with root password login, in both modes: AlmaLinux
10, Rocky Linux 10, CentOS Stream 10 and Fedora 44. Not tested: Red Hat, AlmaLinux 9
and Fedora 45. On Rocky Linux, `sshd` had to be restarted after setting the drop-in
file.

**Starting point:** `sudo` is installed. The administrator is in the group `wheel`
and needs their password. Current versions have
`PermitRootLogin prohibit-password`. The service is called `sshd`, drop-ins live in
`/etc/ssh/sshd_config.d/`. Cloud images usually have a user with `sudo` without a
password (e.g. `ec2-user`, `almalinux`, `rocky`, `fedora`).

**Recommendation:** variant 2, or root by key.

**Pitfalls:**

- **SELinux.** If you put the key in by hand for the login user, set the context
  afterwards: `restorecon -R ~/.ssh`. Otherwise `sshd` rejects the file. The guide
  under *Onboarding key* contains the command.
- Older systems with `Defaults requiretty` refuse `sudo` without a terminal.
- In restricted mode LCM sets `secure_path` itself. RHEL 10 and clones leave out
  `/usr/local/sbin`, where the helper lives.
- Whether LCM can sign in `lcm-svc` under enforcing SELinux without further steps
  is **not tested**. If the onboarding reports a failed key login, check
  `/var/log/audit/audit.log`.

### openSUSE (Leap 16, Tumbleweed) - partly tested

**Status:** Leap 16 tested on 2026-11-26 with root password login, in both modes.
Tumbleweed not. Because no package was outdated, the effect of the update is only
half checked.

**Starting point:** the installer sets a root password. `sudo` is usually there but
by default asks for **root's password** (`Defaults targetpw`), not the user's.

**Recommendation:** root (variant 1). With variants 2 and 3 the password LCM passes
on fits only if user and root password are equal. That follows from the behavior of
`targetpw` and the order of the probes but is not backed by a test run.

**Pitfalls:**

- **Leap 16 ships the main file at `/usr/etc/ssh/sshd_config`** and does not
  include `sshd_config.d`. A drop-in such as `00-root-onboarding.conf` then stays
  without effect. Check with `sshd -T`. If `/etc/ssh/sshd_config` is missing, copy
  the file from `/usr/etc/ssh/` there and set the line in the copy. For its own
  hardening LCM adds the `Include` line itself at the very top of the file.
- `UsePAM no`: an account with a locked password is also rejected with a key. LCM
  therefore unlocks `lcm-svc` itself.
- In restricted mode LCM turns `targetpw` off for `lcm-svc` (the whitelist limits,
  not a password prompt).
- Service: `sshd`.

### Arch Linux - partly tested

**Status:** Tested on 2026-11-26 with root password login, in a container without
Landlock. There onboarding **fails** because LCM cannot install `sudo`, and all
package jobs fail on the pacman sandbox. With `sudo` preinstalled and `DisableSandbox`
in `/etc/pacman.conf` everything worked, in both modes. An old image needs
`pacman-key --init`, `pacman-key --populate` and `pacman -Syu` first. On a VM or
hardware with Landlock this is untested.

**Starting point:** a minimal installation. `openssh` is not installed, the
service is not active, `sudo` is missing, the `wheel` rule in `sudoers` is
usually commented out.

**Recommendation:** `pacman -S openssh`, `systemctl enable --now sshd` and root by
key (variant 1). LCM then installs `sudo` itself.

**Pitfalls:**

- LCM installs `sudo` with `pacman -Sy --noconfirm sudo`. That is **not a full
  update**, and Arch does not support such partial updates. Better install `sudo`
  yourself beforehand: `pacman -Syu sudo`.
- For variant 2 the user needs an entry: `visudo`, then uncomment the `%wheel`
  line.
- In restricted mode LCM puts `pacman` **and** `pacman-key` on the whitelist,
  because setting up repositories imports signing keys.

### Alpine (3.22 to 3.24) - partly tested

**Status:** Alpine 3.23 tested on 2026-11-26 with root password login, in both modes:
onboarding, scan, update and firewall worked. **Package pinning has no effect**:
`apk` has no "hold". LCM reports this in the job log, the job still ends
successfully. Alpine 3.22 and 3.24 are not tested.

**Starting point:** BusyBox and OpenRC. The SSH service is not necessarily part of
the installation (`apk add openssh`, `rc-update add sshd`,
`rc-service sshd start`). `sudo` is missing. Depending on the setup there is
`doas`.

**Recommendation:** root by key (variant 1). LCM installs `sudo` with
`apk add --no-cache sudo` and uses it afterwards.

**Pitfalls:**

- **LCM does not know `doas`.** Neither the probes nor operation call it. For a
  user with `doas` and without `sudo`, variant 2 fails.
- **`su` comes from BusyBox**, not util-linux. Whether attempt 5 accepts the
  password via stdin here is not verified. Use root or `sudo`.
- LCM creates `lcm-svc` with `adduser` and uses `/bin/sh` if `bash` is missing.
  Without that fallback `sshd` would reject the key login.
- LCM reloads the service with `rc-service sshd reload`. There is no `systemctl`.
- `/usr/local/sbin` is missing out of the box. In restricted mode LCM creates the
  directory for the helper.

### Proxmox VE, Backup Server, Datacenter Manager - not tested

**Starting point:** Debian base, with root as the normal account. `sudo` is
missing although `/etc/sudoers.d/` exists.

**Recommendation:** root (variant 1). LCM installs `sudo` afterwards. On a system
without a subscription `apt-get update` reports an error (401) for the enterprise
repository. LCM does not treat that as an abort but installs from the package lists
already present.

**Pitfalls:** LCM blocks actions there that would collide with Proxmox: adding
repositories, the ufw firewall and the Linux user sync. Proxmox manages those
itself. The CVE scan covers the Debian packages, not `pve-*` and `proxmox-*`.

## When the onboarding fails

The log of all steps is in the error message (*Provisionierungs-Protokoll*,
provisioning log). The detail text of the messages appears in German; the key
phrase is given in the table. The most common ones:

| Message starts with | Cause | Remedy |
| --- | --- | --- |
| *Anmeldung abgelehnt - Benutzername oder Passwort stimmt nicht* (sign-in rejected, user name or password wrong) | The server offers password sign-in but rejected the input. | Check user and password. For `root` the server must allow password sign-in (`PermitRootLogin`), see above. |
| *der Server bietet keine Passwort-Anmeldung an* (the server does not offer password sign-in) | `PasswordAuthentication no`, usually after SSH hardening. | Choose **System SSH key** and put its public part with the login user. |
| *keine Root-Rechte für die Provisionierung* (no root rights for provisioning) | None of the seven attempts gave root. | Set up variant 2 (sudo), use root by key, or with variant 3 use equal passwords. See [variant 3](#variant-3-user-who-switches-to-root-with-su-or-login). |
| *sudo ist auf … nicht installiert und ließ sich nicht nachinstallieren* (sudo is not installed and could not be installed) | The package manager could not fetch `sudo` (sources, network). | Install it by hand (`apt-get install sudo`, equivalent per system) and add the server again. |
| *der Service-User „lcm-svc“ erreicht … keine Root-Rechte* (the service user does not reach root rights) | LCM's sudoers file has no effect. | Check `visudo`: does a later entry in `/etc/sudoers.d/` exclude `lcm-svc`, is `#includedir /etc/sudoers.d` missing? |
| *SSH-Host-Key stimmt nicht mit dem gespeicherten Fingerprint überein* (SSH host key does not match the stored fingerprint) | The server has a different key than at onboarding. | If the server was rebuilt: **Reconnect**. Otherwise do not continue, someone might be intercepting the connection. |
| *die Paketverwaltung dieses Systems wird nicht unterstützt* (the package manager is not supported) | None of apt, dnf/yum, zypper, pacman, apk found. | The server cannot be added over SSH. |
| *kein System-Onboarding-SSH-Key verfügbar* (no system onboarding SSH key available) | **System SSH key** chosen, LCM has none. | Look under *Settings → General*. |

## See also

- [Quick start](/en/getting-started/quickstart/): the first onboarding step by step
- [LCM Remote (lcm-agent)](/en/guides/remote/): servers without inbound SSH
- [Supported systems](/en/reference/supported-systems/): what is tested
- [Linux users](/en/guides/linux-users/): manage accounts and keys on the servers
- [Security model](/en/reference/security-model/): what restricted mode achieves
