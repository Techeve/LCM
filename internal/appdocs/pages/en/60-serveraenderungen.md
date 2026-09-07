# What LCM changes on the server

This page is for looking things up: for every action LCM performs on a server
it states **which files and commands are involved** and **how to undo it by
hand** - in case LCM is unreachable, you are taking the server out of
management, or you simply want to see what happened.

Three principles apply throughout:

- **LCM writes into its own files**, not yours. Wherever possible it adds a
  drop-in (`sshd_config.d`, `sudoers.d`, `apt.conf.d`) instead of rewriting an
  existing file. Delete the drop-in and the original state is back.
- **If LCM does have to change an existing file**, it puts a backup next to it
  first - usually with the suffix `.lcm-bak`.
- **Files owned by LCM carry `lcm` in the name.** What is named that way came
  from LCM; anything else LCM did not touch.

All commands below run as `root` (or with `sudo` in front).

## SSH access

### Harden SSH

Creates `/etc/ssh/sshd_config.d/60-lcm-hardening.conf` with
`PasswordAuthentication no`, `ChallengeResponseAuthentication no`,
`PubkeyAuthentication yes`, `PermitRootLogin prohibit-password`. If the main
file does not read drop-ins yet, LCM adds the `Include` line. sshd is then
reloaded and **measured**: if sshd still reports password authentication, LCM
rolls back on its own.

To undo - in the interface via *SSH hardened ✓ - lift*, by hand:

```sh
rm -f /etc/ssh/sshd_config.d/60-lcm-hardening.conf
systemctl reload sshd
```

### Disable root login, custom SSH port

Both live in a second drop-in: `/etc/ssh/sshd_config.d/10-lcm-ssh.conf` with
`Port …` and/or `PermitRootLogin no`. Before each change LCM writes
`10-lcm-ssh.conf.lcmbak` and rolls back if sshd rejects the new configuration.

```sh
rm -f /etc/ssh/sshd_config.d/10-lcm-ssh.conf
systemctl reload sshd
```

**Careful with socket activation:** if `ssh.socket` is running (rather than
`sshd.service` alone), the socket unit decides the port - the sshd drop-in
would have no effect. LCM then also writes
`/etc/systemd/system/ssh.socket.d/10-lcm-port.conf`. If you delete only the
first file, the server keeps listening on the old port:

```sh
rm -f /etc/systemd/system/ssh.socket.d/10-lcm-port.conf
systemctl daemon-reload && systemctl restart ssh.socket
```

If the firewall is active, LCM opened the new port there as well - see the
*Firewall* section.

### Restricted mode

Instead of full rights the management user gets an allowlist:
`/etc/sudoers.d/<service-user>` with `Cmnd_Alias LCM_ALLOWED = …` and exactly
those commands without a password. In addition the LCM helper sits on the
server; the permitted privileged steps run through it.

```sh
rm -f /etc/sudoers.d/<service-user>
```

After that the user has no special rights at all - to manage the server fully
again, lift the mode in LCM rather than just deleting the file.

## Users and rights

### Create or assign a user

LCM creates the account (`useradd -m`, on BusyBox `adduser`), sets the shell,
lifts the password lock (`usermod -p '*'`) and writes the public keys into
`~/.ssh/authorized_keys` - **between two markers**:

```
# >>> LCM managed keys >>>
…
# <<< LCM managed keys <<<
```

Only that block belongs to LCM. Keys you added yourself sit outside it and are
never touched.

To undo: delete the block between the markers, or remove the account:

```sh
sed -i '/# >>> LCM managed keys >>>/,/# <<< LCM managed keys <<</d' /home/<name>/.ssh/authorized_keys
# or entirely:
userdel -r <name>
rm -f /etc/sudoers.d/lcm-<name>
```

### Privilege profiles

A profile is a group `lcm-prof-<slug>` plus the sudo rules in
`/etc/sudoers.d/lcm-prof-<slug>`. An account is a member of **exactly one**
profile group. Before applying, LCM checks the rules with `visudo -c` and
rejects unrestricted ones (`NOPASSWD: ALL`).

```sh
gpasswd -d <name> lcm-prof-<slug>
rm -f /etc/sudoers.d/lcm-prof-<slug>
groupdel lcm-prof-<slug>
```

### Lock or disable an account

Locking sets the password to locked (`usermod -L` or `passwd -l`), disabling
additionally sets the shell to `nologin`. Both are reversible:

```sh
usermod -U <name>
usermod -s /bin/bash <name>
```

## Package sources

### Add a package source

Two files: the signing key at `/etc/apt/keyrings/<key>.asc` and the source at
`/etc/apt/sources.list.d/lcm-<key>.list`. Both carry `lcm` in the name.

```sh
rm -f /etc/apt/sources.list.d/lcm-<key>.list /etc/apt/keyrings/<key>.asc
apt-get update
```

### Switch to HTTPS

LCM replaces `http://` with `https://` in all apt sources. Before the change
each affected file is backed up as `<file>.lcm-bak`. Then `apt-get update`
runs: if it fails, LCM restores the backups immediately by itself. If it
succeeds, the backups move to `/var/backups/lcm-apt-https/` - the previous
state is kept there.

```sh
cp -a /var/backups/lcm-apt-https/etc/apt/. /etc/apt/
apt-get update
```

The interface offers *Revert* for this as long as the backup exists.

### Register an apt cache

One drop-in: `/etc/apt/apt.conf.d/02lcm-apt-cache` with `Acquire::http::Proxy`
and `Acquire::https::Proxy`. If the cache refuses HTTPS tunnels, LCM sets HTTPS
to `DIRECT` so the sources stay reachable.

```sh
rm -f /etc/apt/apt.conf.d/02lcm-apt-cache
apt-get update
```

## Packages

### Updates, removal, cleanup

These actions are ordinary package management - LCM leaves nothing of its own
behind. `All updates` corresponds to `apt-get upgrade`, `Cleanup` to
`apt-get autoremove`. Undoing works only as far as the package manager allows:
install an older version explicitly.

### Remove old kernels

`apt-get -y purge` on the selected kernel packages. The running kernel, newer
ones and one fallback always remain. Undo by reinstalling the package - the
data is gone, the package can be fetched again.

### Package pins

Two effects, depending on the pin:

- **Do not remove** is remembered inside LCM only and excludes the package from
  cleanup. Nothing changes on the server.
- **Freeze version** sets `apt-mark hold`. When applying, LCM first lifts all
  existing holds and then sets those from the pins.

```sh
apt-mark showhold      # what is frozen?
apt-mark unhold <package>
```

## Network and time

### DNS

With systemd-resolved: `/etc/systemd/resolved.conf.d/lcm-dns.conf`. Without
resolved LCM writes `/etc/resolv.conf` directly and backs it up to
`/etc/resolv.conf.lcm-bak` first. After setting it LCM checks a test domain; if
resolution fails, LCM rolls back on its own.

```sh
rm -f /etc/systemd/resolved.conf.d/lcm-dns.conf && systemctl restart systemd-resolved
# without resolved:
mv -f /etc/resolv.conf.lcm-bak /etc/resolv.conf
```

### Time servers

Depending on the service present: `/etc/chrony/chrony.conf` (backup as
`chrony.conf.lcm-bak`), `/etc/systemd/timesyncd.conf.d/lcm-ntp.conf` or
`/etc/ntp.conf` (backup `ntp.conf.lcm-bak`). LCM then waits for actual
synchronisation instead of merely starting the service.

```sh
mv -f /etc/chrony/chrony.conf.lcm-bak /etc/chrony/chrony.conf && systemctl restart chronyd
# or:
rm -f /etc/systemd/timesyncd.conf.d/lcm-ntp.conf && systemctl restart systemd-timesyncd
```

### Time zone

`timedatectl set-timezone`, without systemd the `/etc/localtime` symlink plus
`/etc/timezone`. LCM reads the value back afterwards and reports an error if
the system still says something else.

```sh
timedatectl set-timezone Europe/Berlin
```

## Firewall

LCM uses whichever tool is present - `ufw`, `firewalld` or `nftables` - and
opens the released ports there. With nftables its own rule file lives at
`/etc/nftables.d/lcm.nft`.

```sh
ufw status numbered && ufw delete <number>          # ufw
firewall-cmd --permanent --remove-port=<port>/tcp && firewall-cmd --reload
rm -f /etc/nftables.d/lcm.nft && systemctl reload nftables
```

## Security tools

LCM installs fail2ban and CrowdSec through the package manager
(`apt-get install -y`, then `systemctl enable --now`). They are ordinary
packages - removing them works like any other:

```sh
systemctl disable --now fail2ban
apt-get purge -y fail2ban
```

## Removing a server from LCM

When removing a server you can choose whether LCM should **clean it up**.
Without cleanup only the entry in LCM disappears; everything stays on the
server. With cleanup LCM removes, in this order:

1. the user accounts created by LCM including `/etc/sudoers.d/lcm-<name>`
2. `~/.ssh/authorized_keys` of the management user - **first**, so that access
   is revoked for certain even if a later step fails
3. `/etc/sudoers.d/<service-user>` and finally the account itself

**Not removed** are the changes from the sections above: hardening, package
sources, DNS, time servers and firewall rules stay in place. That is
deliberate - they concern the operation of the server, not LCM's access. If you
want those gone too, work through the relevant sections **before** removing the
server; afterwards the interface is no longer available for it.
