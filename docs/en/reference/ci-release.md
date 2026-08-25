---
sidebar:
  order: 26
title: CI/CD & Releases (GitLab)
description: Branch protection, CI pipeline, Conventional Commits, automatic releases, and the Renovate dependency bot on the GitLab server.
---

This document describes how LCM is set up on a (self-hosted) GitLab server and how to reproduce the same setup for a new project. Reference installation: `https://gitlab.techeve.de/techeve/LCM`.

## Branching model

```
develop    ── where development happens (directly or via feature branches with MR).
beta       ── prereleases (1.24.0-beta.1). Source: develop (release train)
              or community (fix roll-up).
community  ── default branch, the free stable channel. Source: beta (release)
              or enterprise (fix roll-up).
enterprise ── maintenance branch with a contractual promise. Source: fix/* and
              hotfix/* branches - that is where fixes are born.
feature/*  ── optional feature branches, the MR target is always develop.
```

All four channel branches are protected: no direct push, merge requests only,
the pipeline must be green. A merge **carrying a new VERSION** automatically
creates a tag plus release there - without a version bump nothing happens (see
`check:release-version`).

The path of a change: `feature/xyz` → MR → `develop` → MR → `beta` → MR →
`community`. Fixes for the maintenance line travel the other way:
`fix/xyz` → MR → `enterprise`, then as a roll-up merge into `community` and `beta`.

Which channel serves which audience is described in [Repository channels](/en/reference/repo-channels/).

## Setup step by step

All steps work through the web UI or - as documented here - through the [GitLab REST API](https://docs.gitlab.com/ee/api/) with a personal access token (scope `api`):

```sh
export GITLAB=https://gitlab.techeve.de/api/v4
export TOKEN=<personal-access-token>
```

### 1. Create the project in the group

```sh
# Determine the group ID:
curl -s -H "PRIVATE-TOKEN: $TOKEN" "$GITLAB/groups?search=techeve"

# Create the project (namespace_id = group ID):
curl -s -X POST -H "PRIVATE-TOKEN: $TOKEN" "$GITLAB/projects" \
  --data-urlencode "name=LCM" \
  --data-urlencode "namespace_id=3" \
  --data-urlencode "visibility=internal" \
  --data-urlencode "initialize_with_readme=false" \
  --data-urlencode "auto_devops_enabled=false"
```

### 2. Push code, create branches

```sh
git init -b develop && git add -A && git commit -m "Initial import"
git remote add origin https://gitlab.techeve.de/techeve/LCM.git
git push -u origin develop
# The three channel branches start from the same state:
for b in beta community enterprise; do git branch "$b" && git push -u origin "$b"; done
```

Set `develop` as the default branch (new clones/MRs start there):

```sh
curl -s -X PUT -H "PRIVATE-TOKEN: $TOKEN" "$GITLAB/projects/<id>" \
  --data-urlencode "default_branch=develop"
```

### 3. Protect the channel branches: no direct push, only merge requests

```sh
# For EACH of the four channel branches (develop, beta, community, enterprise):
# remove any default protection, then create a strict one.
BRANCH=community
curl -s -X DELETE -H "PRIVATE-TOKEN: $TOKEN" "$GITLAB/projects/<id>/protected_branches/$BRANCH"
curl -s -X POST  -H "PRIVATE-TOKEN: $TOKEN" "$GITLAB/projects/<id>/protected_branches" \
  --data-urlencode "name=$BRANCH" \
  --data-urlencode "push_access_level=0" \
  --data-urlencode "merge_access_level=30" \
  --data-urlencode "allow_force_push=false"
```

- `push_access_level=0` - **nobody** may push directly (not even maintainers).
- `merge_access_level=30` - developers and above may merge via merge request.

`develop` is protected as well, but in a work-friendly way (developers may push and merge, no force push):

```sh
curl -s -X POST -H "PRIVATE-TOKEN: $TOKEN" "$GITLAB/projects/<id>/protected_branches" \
  --data-urlencode "name=develop" \
  --data-urlencode "push_access_level=30" \
  --data-urlencode "merge_access_level=30" \
  --data-urlencode "allow_force_push=false"
```

**Permitted MR sources:** GitLab cannot restrict the source of an MR natively. The CI job `check:mr-source` (see `.gitlab-ci.yml`) enforces it instead - and because the channel branches can only be merged with a green pipeline, the rule is binding:

| Target | permitted sources |
|---|---|
| `beta` | `develop` (release train) or `community` (fix roll-up) |
| `community` | `beta` (release) or `enterprise` (fix roll-up) |
| `enterprise` | `fix/*`, `hotfix/*` or `community` |

### 4. Merge request rules

```sh
curl -s -X PUT -H "PRIVATE-TOKEN: $TOKEN" "$GITLAB/projects/<id>" \
  --data-urlencode "only_allow_merge_if_pipeline_succeeds=true" \
  --data-urlencode "allow_merge_on_skipped_pipeline=false" \
  --data-urlencode "only_allow_merge_if_all_discussions_are_resolved=true" \
  --data-urlencode "remove_source_branch_after_merge=false"
```

- **The pipeline must be green** - which makes the tests (Go, E2E, audits) as well as the jobs `check:mr-source` and `check:release-version` mandatory for every merge into a channel branch.
- Open discussions block the merge (review discipline).

**Approval by another developer:** the number of required approvals is set with:

```sh
curl -s -X POST -H "PRIVATE-TOKEN: $TOKEN" "$GITLAB/projects/<id>/approval_rules" \
  --data-urlencode "name=At least one reviewer" \
  --data-urlencode "approvals_required=1"
curl -s -X POST -H "PRIVATE-TOKEN: $TOKEN" "$GITLAB/projects/<id>/approvals" \
  --data-urlencode "merge_requests_author_approval=false"
```

:::note
**GitLab CE:** *Enforced* approval rules (merge button locked until N approvals) are a premium feature. On a CE server (as here, `enterprise: false`) developers can approve MRs, but the approval is not hard-enforced - the four-eyes rule then holds as a binding team convention, technically backed by `push_access_level=0` (nothing happens without an MR) and the mandatory pipeline. After an upgrade to premium the commands above take effect immediately.
:::

## The CI/CD pipeline

Defined in `.gitlab-ci.yml`, running on the runner tagged `docker` (Docker executor, jobs run inside images):

| Stage | Job | Purpose |
|---|---|---|
| check | `check:mr-source` | permitted MR sources per channel branch (fails otherwise) |
| check | `check:commits` | enforces Conventional Commits in MR pipelines |
| check | `check:release-version` | release train (`develop→beta`, `beta→community`): fails if the `VERSION` is already tagged - the merge would otherwise produce no release |
| check | `version` | on the channel branches only: reads the prepared `VERSION` and checks whether a tag for it already exists |
| test | `frontend` | `npm ci` + **`npm audit`** (gate) + Vite build; `dist/` as an artifact |
| test | `backend` | `go vet` + `go test ./...` (in-memory SQLite) |
| test | `govulncheck` | Go vulnerability scan (gate) |
| test | `e2e` | Playwright against the real binary (on develop and the channel branches as well as in MRs) |
| test | `upgrade-test` | upgrade from 1.11.0 to the candidate with demo data - **not** in every pipeline: mandatory before `enterprise`, otherwise manual |
| build | `binaries` | cross-compile: Linux amd64/arm64, Windows amd64, macOS arm64/amd64 |
| build | `packages:deb` | .deb packages (amd64/arm64) with systemd service via nfpm |
| release | `release` | on the channel branches only: tag `v<VERSION>` + release with the prepared changelog (see below) |
| maintenance | `renovate` | dependency bot: checks Go/npm dependencies, opens update MRs (scheduled/manual only, see below) |

Versioning in CI: every job builds with the state of the `VERSION` file from the commit (on the channel branches the same value is passed on by the `version` job as `NEXT_VERSION`); the unique pipeline number (`CI_PIPELINE_IID`) serves as the build number - the local `.buildnumber` is not touched in CI.

### Which code is actually running there? (commit identity)

Version and build number do **not** identify unambiguously which source state runs in an instance: a locally built binary can carry the same version number as a release and still contain different code. Every build therefore burns in its **git commit** as well (`-X LCM/internal/version.Commit=…`):

- **CI builds** get `$CI_COMMIT_SHA` - the commit is thus unambiguously attributable to the repository.
- **Local builds** (`make build`) take `git rev-parse HEAD` and append **`-dirty`** when the working tree carries uncommitted changes (`.buildnumber` excluded - a pure build artifact).

This is visible in three places:

```bash
./bin/lcm --version          # LCM 1.5.1 (Build 42, 0a5154cf8f36)
curl .../api/v1/system/info  # {"commit":"0a5154cf8f36","dirty":false, …}
```

as well as in the footer of the interface. A dirty build additionally carries a yellow **dev build** badge there - it corresponds to no commit of the repository and must never be confused with a release.

A running instance can thus be checked against the source state at any time:

```bash
git log -1 --oneline <commit-from-system-info>
```

### Provenance of a release

Before every release the `version` job verifies that the commit to be tagged **actually sits on the channel branch** (`git merge-base --is-ancestor`) and aborts otherwise. The `rules` already bind release and deploy to the channel branches anyway; the check holds additionally in case the pipeline is rebuilt later (tag pipeline, manual run, changed `rules`), and it documents the provenance in the job log.

### Commit convention (Conventional Commits)

Version number and changelog arise **automatically from the commit messages** - which is why the CI job `check:commits` enforces the format in every MR pipeline:

```
type(scope): description        # scope optional
```

| Commit type | Effect on the version | Changelog section |
|---|---|---|
| `feat!:` / `fix!:` or `BREAKING CHANGE` in the body | **major** (2.0.0) | 💥 Breaking Changes |
| `feat:` | **minor** (1.2.0) | 🚀 Features |
| `fix:` | **patch** (1.1.1) | 🐛 Bugfixes |
| `perf:` | patch | ⚡ Performance |
| `refactor:` | patch | ♻️ Refactoring |
| `docs:` `test:` `ci:` `chore:` `build:` `style:` `revert:` | no release trigger | 🔧 Miscellaneous |

Examples: `feat(api): notes endpoint`, `fix(ui): navbar wrapping on mobile`, `feat!: config format v2`.

The highest type since the last release determines the jump (a single `feat!` turns any number of `fix` commits into a major release). If the commits since the last tag consist **only** of types without release effect (docs, chore, …), merging into a channel branch produces **no** new release.

Preview locally at any time: `make next-version` (or `go run ./tools/release`) shows the computed next version and the changelog section. The logic lives in `tools/release` (Go, with unit tests).

### The upgrade test

On 2026-08-20 LCM no longer started on the production system after the update
to 1.24.0-beta.1. The cause was a migration that only takes effect **on an
existing database** - the entire test suite ran green throughout, because it
works exclusively against fresh databases.

`packaging/upgrade-test/upgrade-test.sh` closes that gap: it fetches the binary
of an old version (`ALT_VERSION`, currently 1.11.0) from the release assets,
starts it with `--demo` against a fresh data directory, records the inventory,
then lets the **candidate** loose on the same directory and compares.

Three things are checked:

1. **The service starts** - precisely what failed back then.
2. **A second start runs cleanly as well** - migrations must be repeatable.
3. **The data is complete** - no table gone, no rows lost, every identifier
   still findable.

#### Why the test does not go red on every further development

A blunt "before equals after" comparison would be useless: we keep developing,
fields are added, data is deliberately reshaped. A test that trips over that
every time is ignored after the third occurrence.

It therefore checks **promises instead of states**. Everything that changes on
purpose is declared once in `packaging/upgrade-test/erwartungen.json`:

```json
{
  "ab_version": "1.19.0",
  "betrifft": "rules",
  "art": "umgezogen",
  "nach": "schedules",
  "begruendung": "Regeln hängen seit 1.19 an Zeitplänen …"
}
```

For `umgezogen` (moved) the test does not settle for the announcement: it
verifies that the missing rows have actually **arrived** in the target table.

A deliberate data change thus costs three lines of explanation - an
unintentional one stands out. Whoever writes a migration says what it does.

#### The encryption boundary

From 1.15 onwards user and server names are encrypted at rest, accompanied by a
blind index. Across that boundary identifiers are **not comparable** - plain
text before, hash value after. The test recognises this and instead verifies
that every row still carries its own, non-empty identifier. A silent loss would
still stand out: as a missing or duplicate identifier.

### Preparing a release (on develop) & publishing it (in the channel)

The changelog belongs in **exactly the commit that gets tagged**. Version and changelog are therefore prepared **before** the merge, on `develop` - not generated afterwards in CI. That way the commit carried into the channel already holds the matching changelog, the channel never lags a version behind, and CI needs **no write token**.

**Step 1 - prepare on `develop`** (`packaging/prepare-release.sh`):

```sh
git switch develop && git pull
./packaging/prepare-release.sh          # version from the commits since the last tag
# or force an explicit version (e.g. beta -> final):
./packaging/prepare-release.sh 1.0.0
```

The script determines the next version with `tools/release`, writes `VERSION`, prepends the new section to `CHANGELOG.md` and commits both as `release: v<version> - Version & Changelog vorbereitet`. If there are no release-relevant commits since the last tag (only `docs`/`chore`/…), nothing happens - unless an explicit version is given.

```sh
git push origin develop
```

**Step 2 - create the merge request `develop → beta`** and merge it once the pipeline is green (for a final release afterwards `beta → community`). On the target branch the following then runs automatically:

1. **`version` job**: reads `NEXT_VERSION` from the committed `VERSION`, checks whether `v<version>` already exists as a tag (`RELEASE_NEEDED`) and cuts the topmost section out of `CHANGELOG.md` as the release description.
2. **`binaries` / `packages:deb`**: build all platform binaries and the `.deb` packages with that version.
3. **`release` job** (only if the tag does not exist yet): uploads the binaries into the generic package registry and creates **tag `v<version>` + release** with the changelog as the description and the binaries as assets.
4. **`deploy:apt`**: rolls the `.deb` packages out to the repository server (see below).

**The bump is mandatory, and CI insists on it.** If step 1 is forgotten, the merge carries the old `VERSION` onto the release branch. The `version` job finds its tag, sets `RELEASE_NEEDED=false` and lets release and deploy run empty - the code then sits on `beta` but is in no package. So that this does not surface only when somebody looks for the artifact, `check:release-version` evaluates the same condition already in the MR pipeline and fails the merge request as long as the version has not been raised. The job runs only for the two directions in which the merge itself is meant to release (`develop→beta`, `beta→community`); fix roll-ups deliberately carry no new version and are exempt. If the absent release was intentional, the CI variable `ALLOW_NO_RELEASE=true` on the MR lifts the check.

**No writeback, no `RELEASE_TOKEN`:** version and changelog are already in the tagged commit (and through the merge also on `develop` and in the channel). CI writes nothing back into the repository - it only reads. The release job gets by with the automatic `CI_JOB_TOKEN`.

### apt repository rollout (deploy)

On every release the job `deploy:apt` rolls the built `.deb` packages (amd64 + arm64) out to the TechEve repository server (aptly) - after which LCM can be installed and updated from your own repository with `apt install lcm`. The job runs on the channel branches only, and only when a release is actually due (`RELEASE_NEEDED=true`).

Required CI variables (store them as **masked**):

| Variable | Example | Purpose |
|---|---|---|
| `REPO_URL` | `https://repo.techeve.de` | base URL of the aptly HTTP API |
| `REPO_USER` | `gitlab-ci` | basic auth user |
| `REPO_PASS` | *(secret)* | basic auth password |

Optional: `REPO_NAME` (default `techeve`), `DISTRO` (default `stable`), `GPG_KEY` (default `repo@techeve.de`). Procedure and script: `packaging/publish-deb.sh`. If the mandatory variables are missing, the job aborts with a clear message. SemVer prereleases (`-beta.1`) are rewritten to `~beta.1` for the Debian package, so that apt sorts the beta correctly **before** the later final.

### Container images (deploy)

The job `images` builds two images for **amd64 and arm64** and publishes them in the project registry (`registry.techeve.de/techeve/lcm`):

| Image | Content |
|---|---|
| `…/lcm` | the runtime image based on `scratch` - around 37 MB |
| `…/lcm/trivyd` | the Trivy sidecar (official Trivy image + `cmd/trivyd`) |

Tags per channel, always the version plus the moving pointer:

| Branch | Tags |
|---|---|
| `beta` | `:<version>` and `:beta` |
| `community` | `:<version>` and `:latest` |
| `enterprise` | `:<version>` and `:enterprise` (stays in the private registry) |

Two things are deliberately built this way:

**Pushing happens only together with a release.** The job sits in the `deploy` stage and hangs off the release job via `needs: [release]` (optional) - an image tag therefore only comes into being once the release really exists. Without `RELEASE_NEEDED=true` nothing is published; otherwise there would be tags with no release behind them.

**On `develop` and in MRs it is built anyway** (`--output=type=cacheonly`, the result lands nowhere). A broken Dockerfile thus surfaces before the release and not when it counts.

Multi-arch costs almost nothing here: the runtime section of both Dockerfiles contains **no `RUN`** - only copying, so arm64 needs no QEMU emulation. Building goes through `docker buildx` with the `docker-container` driver (the default driver cannot produce manifest lists).

Prerequisite: a runner with **privileged dind** (the same as for the docs builder image). The `29.6` pin comes from there and is justified there - from `>=29.7` onwards the push against this GitLab registry reports "blob unknown to registry".

### Why the Go version lives in `go.mod` and not only in the CI image

The security gate `govulncheck` also assesses the **standard library**. Which Go version is used for that used to be decided solely by the CI image `golang:1-alpine` - a moving tag that runners cache. The result: the same code ran green on a runner with a fresh image and red on one with an old cache, without anyone having changed anything. That happened exactly once (six standard library vulnerabilities, fixed in Go 1.26.6).

The `toolchain` entry in `go.mod` is the more reliable lever: Go downloads the matching toolchain itself when needed, independently of what sits in the image. The lower bound is thus pinned in the repository and identical for every build - locally as in CI. Renovate keeps the entry current through the `gomod` manager.

### Dependency bot (Renovate)

So that the dependencies do not go stale, **Renovate** regularly checks the Go
modules (`gomod`) and the npm packages (`npm`) for newer versions and opens the
updates **automatically as merge requests into develop**. Each of these MRs runs
the full pipeline (tests, E2E, build, packaging) - a bump therefore only turns
green once it demonstrably compiles and passes the tests.

Renovate runs **exclusively as a CI image** (`renovate/renovate`) and is thus
**not a project dependency** - go.mod and package.json stay untouched.
Configuration: `renovate.json` in the repository root.

**Behaviour (from `renovate.json`):**

- The **target branch** is `develop` (`baseBranches`), never a channel branch.
- **Grouping:** all Go minor/patch updates in one MR, all npm minor/patch updates in one MR; **major updates** come individually (better review control).
- **Automerge:** **patch updates** are merged automatically once the pipeline is green (`platformAutomerge` = "merge when pipeline succeeds"); **minor/major** stay open as MRs for manual review.
- **Conventional Commits:** Renovate commits as `chore(deps): …` - which fits the `check:commits` job and, as `chore`, triggers **no** release (the new version only arises when the bumps later flow through the regular release train).
- `go mod tidy` and `npm dedupe` respectively run after every update (`postUpdateOptions`), so that `go.sum`/lockfile stay consistent.
- Security-relevant updates (known CVEs in dependencies) are recognised through OSV, labelled `security` and **not** merged automatically.
- A **dependency dashboard** issue gives an overview of pending/ignored updates at any time.

**One-time setup:**

1. Create an **access token** for the bot (project or group access token, scope `api` + `write_repository`; the developer role suffices for opening/merging on develop):

   ```sh
   curl -s -X POST -H "PRIVATE-TOKEN: $TOKEN" "$GITLAB/projects/<id>/access_tokens" \
     --data-urlencode "name=renovate-bot" \
     --data-urlencode "scopes[]=api" \
     --data-urlencode "scopes[]=write_repository" \
     --data-urlencode "access_level=30" \
     --data-urlencode "expires_at=2027-07-04"
   ```

2. Store the token as a **masked, protected** CI variable `RENOVATE_TOKEN`. `protected=true` is possible because `develop` is a protected branch (on which the scheduled bot pipeline runs) - so the token is not visible in pipelines of arbitrary feature branches:

   ```sh
   curl -s -X POST -H "PRIVATE-TOKEN: $TOKEN" "$GITLAB/projects/<id>/variables" \
     --data-urlencode "key=RENOVATE_TOKEN" --data-urlencode "value=<bot-token>" \
     --data-urlencode "masked=true" --data-urlencode "protected=true"
   ```

   Optional, for fetching changelogs of GitHub-hosted deps: the same procedure with `RENOVATE_GITHUB_TOKEN` (a GitHub PAT without scopes suffices - only for reading public release notes).

3. Create a **scheduled pipeline** that starts the bot, for example weekly, on `develop` with `RENOVATE_BOT=true`:

   ```sh
   curl -s -X POST -H "PRIVATE-TOKEN: $TOKEN" "$GITLAB/projects/<id>/pipeline_schedules" \
     --data-urlencode "description=Renovate Dependency-Bot" \
     --data-urlencode "ref=develop" \
     --data-urlencode "cron=0 6 * * 1" \
     --data-urlencode "cron_timezone=Europe/Berlin" \
     --data-urlencode "active=true"
   # Note the ID of the schedule just created and set the trigger variable:
   curl -s -X POST -H "PRIVATE-TOKEN: $TOKEN" \
     "$GITLAB/projects/<id>/pipeline_schedules/<schedule-id>/variables" \
     --data-urlencode "key=RENOVATE_BOT" --data-urlencode "value=true"
   ```

On that scheduled pipeline **only** the `renovate` job runs - the regular
test/build jobs are silenced through the `.skip-on-renovate` anchor. A manual
run is possible at any time through *Build → Pipeline schedules → ▶* or through
*Run pipeline* with the variable `RENOVATE_BOT=true`. If `RENOVATE_TOKEN` is
missing, the job aborts with a clear message.

### Runner requirements

- A runner with a **Docker executor** and the tag `docker` (the jobs set `tags: [docker]`).
- Internet access for the images (`golang:1-alpine`, `node:lts`, `alpine:3`, `docker:29.6-cli`/`-dind`, `aquasec/trivy`, `registry.gitlab.com/gitlab-org/release-cli`, `renovate/renovate`) as well as Go modules/npm packages/Playwright browsers.
- For the `images` job: **privileged Docker-in-Docker** (`[runners.docker] privileged = true`, `volumes = ["/certs/client", "/cache"]`) - the same prerequisite as for the docs builder image.
- The release job works with the automatic `CI_JOB_TOKEN` alone - a write token is **not** needed, because version and changelog are committed on develop beforehand. The **apt deploy** needs `REPO_URL`/`REPO_USER`/`REPO_PASS` (see above) and the **dependency bot** needs `RENOVATE_TOKEN` (see above). All other jobs need no secrets.

## Roles & permissions in the project

| Role | may |
|---|---|
| Developer | push to develop, open/merge MRs (develop and channel branches), reviews |
| Maintainer | additionally settings, managing protected branches |
| - (everyone) | **not** push directly to a channel branch - without exception |
