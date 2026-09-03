---
sidebar:
  order: 27
title: Package channels (community, beta & enterprise)
description: Three apt channels from one aptly instance - community open and rolling, beta open with pre-releases, enterprise seasoned and protected by a subscription key.
---

LCM is shipped through its own apt repository. That repository serves **three
channels**: an open community channel, an equally open beta channel carrying
pre-releases, and an enterprise channel with a subscription. This page
describes the setup on the repository server.

:::note[Who is this page for?]
For running the repository server. If you only want to **install** LCM, see
[Installation](/en/getting-started/installation/).
:::

## The model

All channels ship **the same packages**, built from the same commit and signed
with the same key. The only difference is timing:

| | Beta | Community | Enterprise |
|---|---|---|---|
| apt line | `deb … repo.techeve.de beta main` | `deb … repo.techeve.de stable main` | `deb … repo.techeve.de/enterprise stable main` |
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

## Server layout

**A second server is not needed.** One aptly instance can serve several publish
points side by side:

```
repo.techeve.de/
├── dists/stable/…          community - published by the CI on every release
├── dists/beta/…            beta - published by the CI from the beta branch
├── pool/…                  packages of both open channels (shared pool)
├── enterprise/
│   ├── dists/stable/…      enterprise - one promoted snapshot
│   └── pool/…              its own package pool
├── repo-key.gpg            signing key (public)
└── setup.sh                setup script for the community channel
```

The community channel stays **exactly where it is**. Existing installations
carry `deb … repo.techeve.de stable main` in their sources; changing that path
would break them at the next `apt update`.

### Why a path prefix and not a second suite

The obvious alternative is to serve both channels from the same URL root with
different suites (`stable` and `enterprise`). That is a **security trap**: aptly
creates a separate `pool/` tree per publish prefix, but two suites under the
same prefix share one pool. The metadata under `dists/enterprise/` would be
protected - while the `.deb` files themselves would sit in the shared, public
`pool/` and could be downloaded by guessing the path.

With its own prefix, a single `location` rule protects metadata **and**
packages.

### Web server (nginx)

Full configuration: `packaging/repo-server/nginx-lcm-repo.conf`. The three
relevant blocks:

```nginx
# Enterprise channel - subscription key required.
# "^~" without a trailing slash so that /enterprise (no slash) is covered too.
location ^~ /enterprise {
    auth_basic           "LCM Enterprise Repository";
    auth_basic_user_file /etc/lcm-repo/enterprise.htpasswd;
    try_files $uri $uri/ =404;
}

# aptly API - CI credentials only, separate file.
location ^~ /api {
    auth_basic           "aptly API";
    auth_basic_user_file /etc/lcm-repo/api.htpasswd;
    proxy_pass           http://127.0.0.1:8080;
}

# Everything else is the open community channel.
location / {
    try_files $uri $uri/ =404;
}
```

Two points are not negotiable:

- **Separate `htpasswd` files.** The aptly API can publish *and delete*. A
  customer key must never be able to reach it.
- **HTTPS only.** Basic authentication merely base64-encodes the key. Port 80
  redirects and nothing else.

## Managing subscription keys

Keys live in a plain file from which the `htpasswd` is generated - no extra
service to run and monitor. Tool: `packaging/repo-server/lcm-subscriptions`.

```bash
# Add a customer (the key is displayed ONCE)
lcm-subscriptions add "Example Ltd" 2027-08-01

# Overview with status (active / expiring / expired)
lcm-subscriptions list

# Renew once the invoice is paid
lcm-subscriptions renew LCM-E-XXXX-XXXX-XXXX 2028-08-01

# Cancellation - takes effect immediately
lcm-subscriptions revoke LCM-E-XXXX-XXXX-XXXX
```

Only the **hash** of a key is stored; the key itself exists in clear text only
at the moment it is created and handed over. If it is lost it gets replaced,
not recovered.

The expiry date is enforced by the daily `sync`, which drops expired keys from
the `htpasswd`:

```
0 4 * * * root /usr/local/sbin/lcm-subscriptions sync >/dev/null
```

nginx re-reads the `htpasswd` on **every request**, so revoking and renewing
take effect at once, without a reload.

## Which version number does a train get?

**The pre-release already carries the number that will become final.**
`1.16.0-beta.1` becomes `1.16.0` - the same figure, just without the suffix.
The beta is the candidate for exactly that version, not for "some later one".

Which number that is follows from the commits since the last release
(Conventional Commits) and is decided **before** the first beta train:

```sh
make next-version     # shows the number the commits imply
```

| Commits included | Number | Beta is then called |
|---|---|---|
| only `docs:`, `chore:`, `test:`, `ci:` | **no release** | - |
| `fix:`, `perf:`, `refactor:` | patch (1.16.**1**) | `1.16.1-beta.1` |
| at least one `feat:` | minor (1.**17**.0) | `1.17.0-beta.1` |
| `feat!:` / `BREAKING CHANGE` | major (**2**.0.0) | `2.0.0-beta.1` |

Only `feat`, `fix`, `perf` and `refactor` are release-relevant. If the commits
since the last tag consist purely of documentation or maintenance work,
`make next-version` reports "no new release" and `prepare-release.sh` does
nothing without an argument. To ship such a state anyway - because a
documentation correction should reach users, say - state the version
explicitly; it then follows the same rule as above (a patch, in that case).

If a commit lands during stabilisation that raises the level - say a `feat:` in
a series planned as a patch - the beta number moves with it: the next
pre-release is called `1.17.0-beta.1`, not `1.16.1-beta.2`.

:::caution[A pre-release is LOWER than its final version]
`1.16.0-beta.1 < 1.16.0` - that is what SemVer and (using `~`) the Debian
version ordering prescribe. Creating a pre-release for an already published
version therefore makes no sense: it would be a step backwards and `apt` would
never install it.

Beta hosts do not fall behind because of this: for them the beta source sits
**next to** the community source, and `apt` takes the higher version of the
two. As soon as the community channel carries the final version they switch to
it automatically - until the next, higher pre-release appears.
:::

If the final number does end up differing from the beta (e.g. because it only
becomes apparent when promoting that a `feat:` was included), that is
technically harmless as long as it is **higher**: the version comparison holds
and the upgrade works. What remains is a gap in the sequence - the beta number
then never exists as a final version.

## Release workflow

The community channel is served by the CI as before
(`packaging/publish-deb.sh`, see [CI & release](/en/reference/ci-release/)).
The enterprise channel is moved forward by hand, using
`packaging/repo-server/lcm-channel`:

```bash
# 1. Right after the release: freeze that state.
#    It is immutable from now on, whatever follows in the community channel.
lcm-channel freeze 1.10.0

# 2. After the soak period (rule of thumb: 2-4 weeks without critical reports):
lcm-channel promote 1.10.0
```

`freeze` creates an aptly snapshot, `promote` switches the enterprise publish
point to it. Customers see the new version at their next `apt update`.

:::tip[Why the HTTP API and not the aptly CLI]
aptly locks its database for a single process. While `aptly api serve` runs as a
service - and it does, because the CI publishes through it - concurrent CLI
calls fail with a database error. `lcm-channel` therefore speaks the same HTTP
API as the CI.
:::

## Customer side

Switching to the enterprise channel is done by a script that sits next to
`setup.sh` on the repository server:

```bash
curl -fsSL https://repo.techeve.de/setup-enterprise.sh | sudo sh -s -- \
  LCM-E-XXXX-XXXX-XXXX <key>
```

Called without arguments it asks for the key interactively - which keeps it out
of the shell history and the process list.

The script stores the credentials in `/etc/apt/auth.conf.d/` (readable by `root`
only, `0600`) and adds the enterprise source. After that the channels have to be
separated - having both serve LCM would defeat the purpose, because apt always
picks the higher version, and sooner or later that is the one from the free
channel.

### Separating the channels: a preference rule, not a clear-cut

apt does not know about channels. It tells sources apart by what is in the
**Release file** - and all three channels carry the same suite on the same host.
So every publish point gets its own mark (aptly: `Origin` and `Label`, set once
with `packaging/repo-server/set-channel-metadata.sh`):

| Channel | Origin | Label |
|---|---|---|
| Community | `TechEve` | `techeve-community` |
| Beta | `TechEve` | `techeve-beta` |
| Enterprise | `TechEve` | `techeve-enterprise` |

That allows expressing exactly what is meant - *the LCM packages* come from the
enterprise channel, everything else may stay where it is. The switch writes
`/etc/apt/preferences.d/lcm-enterprise.pref` for that:

```
Package: lcm lcm-agent
Pin: release l=techeve-community
Pin-Priority: -1
```

Priority `-1` means "never an option". The community source stays active and
keeps delivering its other packages - switching the whole source off would take
those away too.

**Fallback for old publish points:** if the repository server carries no mark
yet, the rule would have no effect. The switch then disables the community
source as before and writes into the job log why. The check runs against the
*re-enabled* source so an earlier run cannot block the better way for good:
whoever switches first without and later with the mark gets the source back and
the rule in its place.

The community source exists in two spellings, and both are handled: `setup.sh`
writes it as the deb822 file `techeve.sources` today - that one gets
`Enabled: no`, the switch apt understands itself; name and content stay, so the
way back loses nothing. Older installations have the classic `techeve.list`,
which is renamed to `techeve.list.disabled` (in `sources.list.d` apt only reads
`*.list` and `*.sources`).

:::caution[The counter-check is part of it]
At the end the script asks the question that actually matters: **where would
`lcm` come from now?** (`apt-cache policy lcm`, the source below the candidate
version). If that is not the enterprise channel, it says so loudly.

That is exactly what went wrong up to LCM 1.12.5: the switch only looked
for `techeve.list` while `setup.sh` had long been writing `techeve.sources` - so
disabling grabbed at nothing, the switch reported success anyway, and both
channels ran side by side.

Affected hosts are put right with *Settings → Subscription → Apply the switch
again* (or by running `setup-enterprise.sh` once more).
:::

The credentials are scoped to host **and path**:

```
machine repo.techeve.de/enterprise
login LCM-E-XXXX-XXXX-XXXX
password <key>
```

That way the key is only ever sent to the enterprise path, never to the public
part of the same server.

If the key is rejected - mistyped, or the subscription has expired - the script
undoes every change and restores the community channel, so a machine is never
left without a package source. The way back stays open later, too:

```bash
curl -fsSL https://repo.techeve.de/setup-enterprise.sh | sudo sh -s -- --revert
```

## Subscription in LCM (Settings → Subscription)

Instead of the script path above, the subscription can be managed directly in
the LCM interface - provided the **subscription service** runs on the
repository server (separate repository:
[techeve/lcm-subscription-service](https://gitlab.techeve.de/techeve/lcm-subscription-service?mtm_campaign=linking&mtm_kwd=doc)).
The flow:

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
   switching runs as a job on the LCM host. For the enterprise channel:
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

The address is taken from the host's existing community source (own repository
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
The frontend is embedded in the binary as a file tree, and every file there
carries the zero timestamp as its modification date. A `Last-Modified` derived
from that is identical across all versions - so the browser revalidated
dutifully, always received a `304` and kept the old `index.html` along with its
old asset references. After an update the old interface kept running, and even
"reload now" ended up in the same place. The build identifier is the only value
that reliably changes with every release.
:::

### The check follows the configured channel

"Is there a newer version?" is answered against **the channel the host is
actually on** - not against Community across the board. LCM reads the same
package index that `apt update` gets its version from:

| Channel | Index queried | Access |
|---|---|---|
| Community | `<repo>/dists/stable/…/Packages` | open |
| Beta | `<repo>/dists/beta/…/Packages` | open |
| Enterprise | `<subscription-repo>/dists/stable/…/Packages` | instance ID + access key |

This is the difference between "current" and "current **for me**": a
pre-release sits above the stable version, a matured enterprise release below
it. Anyone on Beta previously got the stable version reported and therefore
saw no update at all, even when their own channel had one waiting.

The check runs every three hours and once at startup. The **info window**
(click the copyright notice in the footer) shows the version last determined
along with its channel, plus a **"Check for latest version"** button for an
immediate query - handy right after a channel switch, where the cached result
still refers to the previous channel.

If the Enterprise channel is configured but no repository or valid access key
is stored, the check reports that as an error. It deliberately does **not**
fall back to the Community channel - presenting a version from a foreign
channel as "the latest" would be worse than an honest failure notice.

## Limits of this setup

- **No copy protection.** All channels contain the same binary; whoever passes
  on the key passes on access. The protection is contractual, not technical -
  with identical packages, anything else would be self-deception. The
  subscription service prevents multiple *activation* of a key, not passing on
  packages.
- **Server count is reported, never enforced.** Community and Enterprise can
  both manage unlimited servers; the count reported with the heartbeat is
  display at the vendor, not a limit.
