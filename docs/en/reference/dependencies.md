---
sidebar:
  order: 28
title: Dependencies & Supply Chain
description: How LCM evaluates new dependencies, when writing it ourselves is the better call, and how the update path is hardened against supply chain attacks.
---

LCM compiles into a single binary that runs on other people's servers with
far-reaching privileges. Every dependency we take on runs there too. This page
defines how we evaluate dependencies and what rules apply when adding, updating,
and removing them.

## Principle

**No external dependency for a problem that a few lines of our own code can
solve.** The reverse holds just as firmly: no in-house implementation of
cryptography or protocol parsing - there, the hole we build ourselves is more
likely than the supply chain attack we were trying to avoid.

## The two axes

Every dependency is rated along two axes. Only both together produce a decision.

### Axis 1 - Stewardship (trust 1-5)

How hard would it be for an attacker to push a malicious release through the
publishing process?

| Level | Characteristic | Examples in our tree |
|:-:|---|---|
| 5 | Go team at Google or equivalent: multi-stage review, public history, security team | `golang.org/x/*`, `google/uuid`, `lit`, `vite`, `@playwright/test` |
| 4 | Organization with several active maintainers and a release process | `gofiber/fiber`, `gorm.io/gorm`, `golang-jwt/jwt`, `eclipse/paho.mqtt.golang`, `@xterm/xterm` |
| 3 | Small organization or a well-regarded individual, demonstrably active | `mochi-mqtt/server`, `fasthttp/websocket`, `simple-icons`, `filippo.io/age` |
| 2 | Individual, sporadic releases, bus factor 1 | `glebarez/sqlite` |
| 1 | Individual, inactive for years, no release process, untagged versions | - (deliberately empty, see below) |

Level 1 is deliberately empty: such dependencies are not taken on, and existing
ones get replaced.

### Axis 2 - Surface used and replaceability

Not "how big is the library" but **how much of it do we actually use**, and what
does replacing it cost - including the risk we build into the replacement
ourselves.

A library with 90 lines of used surface can still be irreplaceable if those 90
lines are cryptography. Conversely, a dependency we call exactly one function
from is a candidate for removal, regardless of its size.

## Decision rule

Both axes lead to the rule that applies when in doubt:

**Write it ourselves** when all three hold:

- no cryptography,
- no parsing of data that comes from outside (network frames, foreign file formats),
- a bug has a harmless failure mode and shows up immediately.

**Do not write it ourselves** as soon as any of these appear: passwords, tokens,
TLS, signatures, network protocols, time zone data, Unicode tables.

Examples from our tree: log rotation (`internal/logging/rotate.go`) and QR
encoding (`internal/infrastructure/totp/qr.go`) are ours - no crypto, no foreign
input, harmless failure mode. Password hashing, JWT, SSH, and the MQTT broker
stay external even though the surface we use is small in places.

## Checklist before every new dependency

Answer before `go get` or `npm install`, and record it in the merge request:

1. **Stewardship** - organization or individual? When was the last release? Is
   the repository still moving? (Go: `https://proxy.golang.org/<module>/@latest`,
   npm: `npm view <package> time.modified maintainers`)
2. **Surface** - which functions do we concretely need? If it is one or two: can
   we do without?
3. **Transitive freight** - what does the package drag in? (`go mod graph`,
   `npm ls`) A package with twenty micro-dependencies is twenty new risks.
4. **Decision rule** - does the task fall into the write-it-ourselves category?
5. **Current version** - never from memory, always ask the registry.

Stewardship below level 3 is only taken on with an explicit justification.

## The update path is the actual risk

`go.sum` with its checksum log, and the `integrity` hashes in
`package-lock.json`, make it practically impossible to tamper with a version we
already depend on. **A supply chain attack therefore always arrives as a _new_
version.** Three rules follow, and all three are wired into the repo:

- **Cooling-off period.** [renovate.json](https://gitlab.techeve.de/techeve/lcm/-/blob/community/renovate.json?mtm_campaign=linking&mtm_kwd=doc)
  sets `minimumReleaseAge` to seven days - the window in which compromised
  packages are usually discovered and pulled.
- **Automerge only for levels 4 and 5.** Patch updates merge on their own only
  for publishers with a solid release process. Everything else needs a human
  look, patches included. Security updates (`vulnerabilityAlerts`) never
  automerge.
- **No install scripts.** CI and `make` install npm packages with
  `--ignore-scripts`. `postinstall` scripts are the most-used attack path in npm,
  and they run on the runner with access to CI variables and signing keys.
  Playwright fetches its browsers explicitly via `npx playwright install`.

`govulncheck` and `npm audit` are mandatory gates, but they do **not** replace
the above: they check known vulnerabilities, and a fresh attack is by definition
unknown. Never disable either gate to turn a pipeline green.

## Build time counts as much as runtime

Vite, TypeScript, and Playwright never reach the shipped binary - they run on the CI
runner, with access to registry tokens, signing keys, and CI variables. Build
tooling is held to the same rules as runtime dependencies.

## Keeping an eye on the tree

**`glebarez/sqlite` is the one place where high criticality meets weak
stewardship** (level 2): a single maintainer, infrequent releases - and the
entire data layer sits on top of it. That is not an acute problem today, but it
is the point where losing the maintainer would cost us the most.

**Plan B:** [`github.com/ncruces/go-sqlite3`](https://github.com/ncruces/go-sqlite3)
is the actively maintained CGO-free alternative (WASM-based) and ships its own
GORM driver. Triggers for switching - any one is enough:

- a known vulnerability goes more than 30 days without a release,
- the repository sees no activity at all for twelve months,
- the project is archived or handed to an unknown third party.

The switch only touches `internal/storage/database.go`; the repository layer is
unaffected. Before switching, test migrations and backup/restore against an
existing database - the drivers differ in details of type conversion.

**`robfig/cron` has slipped to level 1:** last tag v3.0.1 from 2020, over 170
open issues. Only the expression parser (five fields plus descriptors) and the
scheduler in `internal/core/services/scheduler.go` and `group_service.go` are
used. The job falls into the build-it-ourselves category: no cryptography, the
expressions come from the administrator, and an error shows up in the schedule
immediately. Replacement by an in-house implementation with a differential test
against `robfig/cron` (a few days of effort); the fallback is the maintained
fork `netresearch/go-cron`.

**Watch `mochi-mqtt/server`:** the broker is indispensable for the agent
connection and is protocol parsing, so not something to build ourselves. Last
release 03/2025, many open issues - trending towards level 2. A way out would be
an agent channel over WebSocket (`fasthttp/websocket` is in the tree anyway).

**Watch `molecule-man/go-brrr`:** arrives transitively since fasthttp 1.75
(replaces `andybalholm/brotli`). Pure Go, fuzz-tested, co-maintained by the
fasthttp maintainer, but young and with a bus factor of 1.

**uPlot is embedded as a file in `lcm-probe`** (1.6.32, MIT, Leon Sorokin,
level 3): the charts of the HTML report. It runs only in the browser of whoever
opens the report, never in LCM and never on a managed server, and reads only
numbers `lcm-probe` wrote itself. Because it lives in
`internal/probe/analysis/assets/` and not in a `package.json`, Renovate does not
see it: update by hand with `npm pack uplot@<version>`, then replace
`dist/uPlot.iife.min.js`, `dist/uPlot.min.css` and `LICENSE`. Building it
ourselves does not pay off: synchronised zoom across several time series is a
few hundred lines for a tool whose bugs would be harmless.

## Removing a dependency

When a dependency is replaced, a **differential test** is part of the job: while
both implementations exist side by side, ours is checked against the old one and
the result is frozen as fixed test vectors. Only then does the dependency leave
`go.mod` or `package.json`. Template: the test vectors in
`internal/infrastructure/totp/qr_test.go`.
