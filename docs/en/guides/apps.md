---
sidebar:
  order: 13
title: Applications
description: Monitor software installed outside the package manager - detection, version state, backup and update.
---

AdGuard Home lives under `/opt`, Nextcloud in the web root, mailcow is a git
checkout: for apt or dnf none of this exists. Yet these are exactly the
applications that are reachable from outside, carry vulnerabilities and want
updating - and without deliberate effort none of them appears on any list.

The **Applications** tab on the server detail page closes that gap. It has two
parts with different ambitions.

## Detected applications

Whatever LCM knows from the **application catalog** is looked for during the
full scan and listed with its location and installed version. If a source is
configured, the latest version appears next to it - and the row turns yellow
when the installed one falls behind.

Applications that come from the **package manager** on this server are
deliberately absent: they belong in the *Packages* tab and are updated there.
So whether a catalog entry applies is decided per server - the same software
can be a package on one host and a hand installation on another.

## Unassigned

Below that sits what the catalog does not know: running systemd services whose
unit file belongs to no package. LCM does not know what these are - only that
they were installed past the package manager.

This part is the more important one. A catalog only shows what someone entered
beforehand; the generic sweep also shows what nobody had in mind. To monitor
one of these, add a catalog entry - service name and program path are right
there in the row.

## The application catalog

**Settings → Applications.** LCM ships entries for common software (AdGuard
Home, Nextcloud, mailcow, Seafile, MinIO, RustDesk, Odoo, Intrexx and the
Techeve products); your own can be added. An entry consists of:

**Markers** - one per line, `kind value`, first hit wins:

| Kind | Meaning |
|---|---|
| `path` | File or directory exists. The location found becomes the install path. |
| `unit` | A systemd unit of that name exists. |
| `bin` | The program is in PATH. |
| `proc` | A process of that name is running. |

`proc` deliberately comes last as the weakest marker: Intrexx runs as `java`,
Nextcloud as `php-fpm` - as the only identifier that would be worthless.

**Version command** - `{path}` is replaced by the location found. A version
file is covered too: `cat {path}/VERSION`. The command runs as root during the
scan on every server where a marker matches, capped at 15 seconds. Leave it
empty and LCM only reports *that* the application is present.

**Version pattern** - a regular expression; the first capture group is the
version. `AdGuard Home, version v0.107.52` becomes `0.107.52` with
`v?([0-9]+\.[0-9]+\.[0-9]+)`.

**Comparison** - `version numbers` splits into digits (1.10 is newer than 1.9),
`exact match` fits date stamps and build identifiers, `display only` never
judges. That third option is not a cop-out: a tab that wrongly cries
"outdated" is burnt after the second false alarm.

**Source of the latest version** - `github:owner/repo` for the latest GitHub
release, otherwise `url:https://…` together with a pattern. The lookup belongs
to the application, not the server: with 40 servers running the same
application anything else would be the same request 40 times over. It runs
daily as the **application check** in the system schedule.

**Backup and update** - both are [custom actions](/en/guides/groups-rules/).
Referencing one instead of storing a raw command line is deliberate: they then
run through the same vetted path as every other action, with a job log and an
audit trail. The tab shows *Back up* and *Update* buttons for them.

:::caution[Back up first, then update]
If a backup is configured it runs before the update. **If it fails, the update
does not run** - a failed backup is the moment you can least afford an update.
:::

## Bilingual entries

Name and description exist per entry once in German and once in English - the
catalog lives in the database, not in the language catalog of the interface,
and without a second text field the English view would show German text. The
built-in entries carry both. For your own entries the English version is
**optional**: if left empty, the English interface shows the German text as
well - better than an empty field.

## Built-in entries

Markers, version command and source of built-in entries are reset to the
shipped state on start - otherwise a marker shipped once wrong would stay wrong
forever. What survives is the on/off switch and the two actions; those are
operational decisions. If you permanently need different detection rules,
create your own entry.

## Limits

- **Without systemd** (Alpine with OpenRC) the generic sweep is skipped.
  Catalog detection via paths and programs keeps working.
- **In restricted mode** detection runs without root. The system answers the
  markers anyway; a version command that needs root stays unanswered. Routing
  it through the LCM helper would mean letting arbitrary commands past its
  whitelist - precisely what restricted mode prevents.
- **One application, several installations**: currently the first hit per entry
  and server is recorded.
