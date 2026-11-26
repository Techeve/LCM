---
sidebar:
  order: 27
title: Package channels (community, beta & enterprise)
description: The package channels of repo.techeve.de - community, beta and enterprise - for apt, dnf, zypper, apk and pacman - differences, setup with setup.sh, keys, subscription and the update check.
---

LCM is shipped through its own package repository: `repo.techeve.de`. It serves
packages for **apt** (Debian, Ubuntu), **dnf** and **zypper** (rpm), **apk**
(Alpine) and **pacman** (Arch Linux). It serves **three channels**: an open
community channel, an equally open beta channel carrying pre-releases, and an
enterprise channel with a subscription.

The page shows how the channels differ, how you set up and check the sources, how
the switch to enterprise and beta works in LCM, and how LCM checks for updates
per channel. If you only want to **install** LCM, see
[Installation](/en/getting-started/installation/).

## The model

With apt, one rule holds for all channels: the **URL path decides access**, the
**suite is the channel**, and the component is always `main`. The other package
formats have a path of their own per channel (see below).

All channels ship **the same packages**, built from the same commit and signed
with the same key. The only difference is timing:

| | Beta | Community | Enterprise |
|---|---|---|---|
| apt line | `deb … https://repo.techeve.de beta main` | `deb … https://repo.techeve.de community main` | `deb … https://repo.techeve.de/enterprise/lcm enterprise main` |
| Access | open | open | subscription key |
| Feature updates | pre-releases, ahead of the release | immediately, every release | only after they prove themselves in the free channel |
| Security updates | immediately | immediately | **immediately as well** |
| Support | community | community | contractual |

This is deliberately not a feature split: there is no capability missing from
the free channel. What is sold is **stability and someone to call**, not a
crippled product. That also makes the free channel the proving ground for the
enterprise one - and the beta channel the stage before that.

:::caution[Never delay security updates]
A security release goes into all channels at once. Leaving paying customers
exposed for longer than free users would be neither defensible nor sellable.
:::

The table applies to apt. **Enterprise exists for apt only.** For dnf, zypper,
apk and pacman there are the community and beta channels.

## Package managers and sources

Which systems these are and what is tested is listed under
[Supported systems](/en/reference/supported-systems/). This section covers where
the source lives and what it needs. The "Source" column shows the community
channel.

| Package manager | Packages | Source | File that `setup.sh` creates | Key |
|---|---|---|---|---|
| apt | `lcm`, `lcm-agent` | `https://repo.techeve.de`, suite `community`, component `main` | `/etc/apt/sources.list.d/techeve.sources` (deb822) | `/etc/apt/keyrings/techeve-repo.gpg`, fetched from `/repo-key.gpg` |
| dnf | `lcm-agent` | `https://repo.techeve.de/rpm/community/$basearch/` | `/etc/yum.repos.d/techeve-lcm.repo` | `/keys/techeve-packages.asc`, imported with `rpm --import` |
| zypper | `lcm-agent` | same as dnf | `/etc/zypp/repos.d/techeve-lcm.repo` (plus `type=rpm-md`, `autorefresh=1`) | same as dnf |
| apk | `lcm-agent` | `https://repo.techeve.de/apk/community` | one line in `/etc/apk/repositories` | `/etc/apk/keys/techeve-apk.rsa.pub`, fetched from `/keys/techeve-apk.rsa.pub` |
| pacman | `lcm-agent` | `https://repo.techeve.de/arch/community/$arch` | section `[techeve]` in `/etc/pacman.conf` (`SigLevel = Required`) | `/keys/techeve-packages.asc`, added to the pacman keyring and signed locally |

Three points about the packages:

- **`lcm` exists for apt only.** That is the LCM server. On the other systems LCM
  runs in a Docker container, see [Installation](/en/getting-started/installation/).
  On those systems you manage servers with the `lcm-agent`, which exists for all
  five package managers.
- **Architectures:** apt serves `amd64` and `arm64`, the other formats `x86_64`
  and `aarch64`. A package without an architecture (`noarch`, `any`) sits in both
  directories.
- **Enterprise** exists for apt only, at `https://repo.techeve.de/enterprise/lcm`,
  suite `enterprise`, with credentials (see [Switching to Enterprise](#switching-to-enterprise)). It
  carries `lcm`, not `lcm-agent`.

The directories of the other formats below `https://repo.techeve.de`:

| Path | Content |
|---|---|
| `/rpm/<channel>/<arch>/` | `repodata/` with `repomd.xml` and `repomd.xml.asc`, plus the `.rpm` files |
| `/apk/<channel>/<arch>/` | `APKINDEX.tar.gz` and the `.apk` files |
| `/arch/<channel>/<arch>/` | the databases `techeve.db` and `techeve.files`, the `.pkg.tar.zst` files, each with a `.sig` |
| `/keys/` | the public keys, see below |

`<channel>` is `community` or `beta`. Every channel has a valid, signed index,
even before the first package is in it. All indexes are signed, and for rpm and
Arch the packages too; `setup.sh` switches the verification on (`gpgcheck=1` and
`repo_gpgcheck=1` for dnf and zypper, `SigLevel = Required` for pacman).

Old versions stay available. For rpm and apk the index lists all versions; you
install a specific one with
`dnf --setopt=disable_excludes=techeve-lcm install lcm-agent-<version>`,
`zypper install lcm-agent=<version>` or `apk add lcm-agent=<version>`. pacman
lists only the latest version in its database; fetch an older one with
`pacman -U` from its fixed address below `/arch/<channel>/<arch>/`.

## Setup with setup.sh

One command sets up the source on all five package managers. It runs as `root`:

```bash
# Community channel
curl -fsSL https://repo.techeve.de/setup.sh | sudo sh

# Beta channel
curl -fsSL https://repo.techeve.de/setup.sh | sudo sh -s -- --beta

# Remove the source and the key again
curl -fsSL https://repo.techeve.de/setup.sh | sudo sh -s -- --uninstall
```

`setup.sh` takes the first package manager it finds (order: apt, dnf, zypper,
apk, pacman), creates key and source, and reads the index once. You can run it
again.

| Call | Effect |
|---|---|
| no option | community channel |
| `--beta` | beta channel instead of community |
| `--uninstall` (also `--remove`) | removes source and key, not the installed packages |
| `--help` | shows the help |
| `SUITE=beta` (apt only) | selects the beta channel like `--beta` |
| `TECHEVE_REPO_URL=<address>` | replaces `https://repo.techeve.de`, for example for tests or your own server |

What you should know:

- **Running it again replaces the source.** `--beta` replaces the community
  source with the beta source, a run without `--beta` switches back to community.
  With apt it swaps the suite in `techeve.sources`. If you want beta **next to**
  community on apt, use the switch in LCM (see
  [Beta channel: no subscription, no console](#beta-channel-no-subscription-no-console)).
- **The agent stays on its version.** After installation the `lcm-agent` package
  holds itself, so a system update does not move it (apt, from agent 2.4: a file in
  `/etc/apt/preferences.d/`, dnf: `exclude=lcm-agent` in the source, zypper: a
  package lock, pacman: `IgnorePkg`). `setup.sh` does not write this up front, so
  the first installation goes through without a detour; a repeated run keeps an
  existing `exclude`. The agent is updated through LCM (from LCM 2.4), see
  [Supported systems](/en/reference/supported-systems/#lcm-agent).
- **dnf and zypper:** `--uninstall` removes the source only. The imported key
  stays in the rpm database.
- **pacman:** `--uninstall` removes the `[techeve]` section and exactly the
  Techeve key whose fingerprint is in `/etc/pacman.d/techeve-packages.asc`. If the
  fingerprint cannot be determined, the keyring stays untouched. If the kernel
  has no Landlock (Arch in an LXC container, for example), `setup.sh` runs pacman
  once with `--disable-sandbox`, because the download fails otherwise.
  `pacman.conf` never gets this setting.
- **Old name `stable`:** The suite `stable` is the old name of `community`.
  Existing installations keep working, and the `lcm` and `lcm-agent` packages
  move the source to `community` themselves on update. This only affects entries
  for `https://repo.techeve.de` with the suite `stable`, never enterprise, beta or
  foreign sources.
- **Language:** messages are in English unless the system is set to German.

### Beta channel per package format

| Package manager | Source in the beta channel |
|---|---|
| apt | `https://repo.techeve.de`, suite `beta`, component `main` |
| dnf, zypper | `https://repo.techeve.de/rpm/beta/$basearch/` |
| apk | `https://repo.techeve.de/apk/beta` |
| pacman | `https://repo.techeve.de/arch/beta/$arch` |

Each format writes pre-release versions differently. `2.4.0-beta.1` becomes
`2.4.0~beta.1` for apt and rpm, `2.4.0_beta1` for apk and `2.4.0beta.1` for Arch.
Every format sorts the pre-release before the final version.

## Keys

All channels are signed. The public keys are freely available on the server:

| File | Used for |
|---|---|
| `https://repo.techeve.de/keys/techeve-packages.asc` | GPG key for rpm (dnf, zypper) and Arch (pacman) |
| `https://repo.techeve.de/keys/techeve-apk.rsa.pub` | RSA key for apk |
| `https://repo.techeve.de/repo-key.gpg` | GPG key for apt (also as `/repo-key.asc`) |

The apt key is not in `/keys/`. It is a key of its own and covers the suites
`community`, `beta` and `enterprise` together. `setup.sh` always fetches the key
that the detected package manager needs. For apk, the file name in
`/etc/apk/keys/` is also the name of the key in the index; do not rename the
file.

## Switching to Enterprise

Switching to the enterprise channel applies to the LCM host with apt and runs in
LCM under *Settings → Subscription* (see
[Subscription in LCM](#subscription-in-lcm-settings--subscription)). The
subscription key itself does not open the repository. LCM exchanges it for an
access key bound to the instance. LCM signs in to the repository with the
instance ID as login and the access key as password.

The switch creates these files:

| File | Content |
|---|---|
| `/etc/apt/auth.conf.d/lcm-enterprise.conf` | credentials, readable by `root` only (`0600`) |
| `/etc/apt/sources.list.d/lcm-enterprise.list` | `deb [signed-by=…] https://repo.techeve.de/enterprise/lcm enterprise main`, using the keyring of the community source |
| `/etc/apt/preferences.d/lcm-enterprise.pref` | the preference rule below |

The switch clears a beta source (`lcm-beta.list`). After that the channels have to
be separated - having both serve LCM would defeat the purpose, because apt always
picks the higher version, and sooner or later that is the one from the free
channel.

### Separating the channels: a preference rule by suite

apt does not know about channels, it tells sources apart by what is in the
**Release file**. There every channel has its own suite (`community`, `beta`,
`enterprise`, plus the old name `stable`). The switch uses exactly that: *the
`lcm` package* comes from the enterprise channel, everything else may stay where
it is. It writes `/etc/apt/preferences.d/lcm-enterprise.pref`:

```
Package: lcm
Pin: release a=community
Pin-Priority: -1

Package: lcm
Pin: release a=stable
Pin-Priority: -1

Package: lcm
Pin: release a=beta
Pin-Priority: -1
```

Priority `-1` means "never an option". The community source stays active and
keeps delivering its other packages - switching the whole source off would take
those away too. The rule always applies; it needs no check against the server.
A community source that an older LCM disabled during the switch is re-enabled.

Only `lcm` is pinned. The `lcm-agent` is not in the enterprise channel; if it
were pinned, it would come from no source at all on the LCM host.

The community source exists in two spellings: `setup.sh` writes the deb822 file
`techeve.sources`, older installations have the classic `techeve.list`. Both stay
in place.

:::caution[The counter-check is part of it]
At the end the switch asks the question that actually matters: **where would
`lcm` come from now?** (`apt-cache policy lcm`, the suite below the candidate
version). If that is not `enterprise`, it reports a warning in the job log.
:::

The credentials are scoped to host **and path**:

```
machine https://repo.techeve.de/enterprise/lcm
login <instance ID>
password <access key>
```

That way the access key is only ever sent to the enterprise path, never to the
public part of the same server.

If the access is rejected - the subscription has expired or the key is invalid -
the switch undoes every change and restores the community channel, so a machine
is never left without a package source. The way back stays open later, too: in
LCM, choose the community channel under *Package channel of the LCM host*. That
removes the credentials, the enterprise source, the preference rule and any beta
source.

## Subscription in LCM (Settings → Subscription)

The subscription is managed directly in the LCM interface. For that, LCM asks the
**subscription service** of the repository server. The flow:

1. On first start LCM creates a permanent **instance identity** (UUID). It
   lives in the database and travels with backups - after a restore-based move
   it remains the same instance.
2. The operator enters the subscription key under *Settings → Subscription*.
   LCM reports key + instance identity to the service and receives an
   **access key bound to this instance** - the key itself does not open the
   repository. The first instance wins; a key already bound elsewhere is
   rejected with a clear message.
3. A **daily heartbeat** (verify) keeps the contract status current - the page
   shows "expires in X days", the last check and errors. If the service is
   unreachable, the status honestly says "unreachable" (unknown ≠ bad).
4. Under **"Package channel of the LCM host"** all three channels are offered;
   switching runs as a job on the LCM host (which uses apt). For the enterprise channel:
   credentials into `/etc/apt/auth.conf.d/` (0600), dedicated source, channel
   separation, verification against the new source only - with a full rollback
   on failure. The way back ("community channel") aborts rather than leaving a
   machine without a package source.

Key and access key are stored AES-GCM encrypted; the access key is redacted
in the SSH log of the switch job.

### Beta channel: no subscription, no console

The beta channel is open - it needs **no subscription**. The channel picker is
therefore available on installations without a stored key as well; only the
enterprise entry is locked there.

Unlike the enterprise switch, the beta source is placed **next to** the
community source instead of replacing it:

```
deb [signed-by=<keyring of the community source>] https://repo.techeve.de beta main
```

The line lives in `/etc/apt/sources.list.d/lcm-beta.list`. The address is taken
from the host's existing community source (own repository
servers included), and so is the signing keyring - all channels are signed with
the same key. There is deliberately **no preference rule**: apt takes the newer
version of the two sources. A beta tester therefore gets the pre-release while
it is ahead, and the final release as soon as it appears - security updates
included. Pinning to the beta channel would prevent exactly that whenever a
final release falls between two pre-releases.

Verification runs against the new source only: if the server does not know the
`beta` suite, the job fails and the source is removed again - a host is never
left with a dead package source. Switching to another channel clears the beta
source out.

:::note[apt does not downgrade]
Switching back to community leaves the host on the installed pre-release until
the final release overtakes it. That is apt behaviour, not a defect - a
downgrade would have to be done by hand (`apt install lcm=<version>`) and is
not something to do casually with database migrations in the picture.
:::

## LCM updates itself

The LCM host is part of the managed inventory, and the `lcm` package sits in
the same channel as everything else. Updating packages there - from the server
detail view, through a rule, or via the beta switch above - sooner or later
updates LCM itself.

That inevitably produces an unusual situation: the package restarts the
service, and the service is at that moment running the very job that replaces
it. The running action loses its own executor before it can write a result.

LCM detects this on the next start from three circumstances coinciding: it is
running a different version than before (`version.json`), the open job was
installing packages, and it ran on the LCM host. The job then counts as
**successful** - the update demonstrably went through, otherwise the new
version would not be running. The log records the version change as the
reason.

Afterwards LCM rescans its own host so the overview shows the version that is
actually installed rather than the state from before the update.

Two boundaries keep this from becoming whitewashing:

- Only **package jobs on the LCM host** qualify. An interrupted run on any
  other server stays a failure - LCM must not claim anything about a remote
  machine it has not verified.
- Without a version change there is no special case. A restart for any other
  reason (crash, manual restart) still leaves interrupted jobs as failures.

The **interface** follows suit: an open page keeps its JavaScript in memory and
would never notice the files being swapped underneath it. It therefore polls
the running build identifier and reloads itself when it changes - with a brief
notice, so the screen does not vanish without a word.

For a reload to achieve anything, the server has to hand out the new
`index.html` in the first place. Asset files carry a hash in their name and are
therefore cacheable indefinitely; the `index.html` that points at them must not
be. It is served with `Cache-Control: no-cache` and an **ETag derived from the
build identifier**: for the same build the server answers `304 Not Modified`
(nothing is transferred), for a new build `200` with the new file.

:::note[Why not Last-Modified]
Inside the binary every embedded file carries the same zero timestamp as its
modification date, so a `Last-Modified` would be identical across all versions.
The build identifier is the only value that reliably changes with every
release.
:::

### The check follows the configured channel

"Is there a newer version?" is answered against **the channel the host is
actually on** - not against Community across the board. LCM reads the same
package index that `apt update` gets its version from:

| Channel | Index queried | Access |
|---|---|---|
| Community | `<repo>/dists/community/…/Packages` | open |
| Beta | `<repo>/dists/beta/…/Packages` | open |
| Enterprise | `<subscription-repo>/dists/enterprise/…/Packages` | instance ID + access key |

This is the difference between "current" and "current **for me**": a
pre-release sits above the stable version, a matured enterprise release below
it.

The check runs every three hours and once at startup. The **info window**
(click the copyright notice in the footer) shows the version last determined
along with its channel, plus a **"Check for latest version"** button for an
immediate query - handy right after a channel switch, where the cached result
still refers to the previous channel.

If the Enterprise channel is configured but no repository or valid access key
is stored, the check reports that as an error. It deliberately does **not**
fall back to the Community channel - presenting a version from a foreign
channel as "the latest" would be worse than an honest failure notice.

## Server count

Community and Enterprise can both manage unlimited servers. LCM reports the
number of servers with the daily heartbeat; it is displayed only and never
enforced as a limit.
