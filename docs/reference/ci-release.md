---
sidebar:
  order: 26
title: CI/CD & Releases (GitLab)
description: Branch-Schutz, CI-Pipeline, Conventional Commits, automatische Releases und der Renovate-Dependency-Bot auf dem GitLab-Server.
---

Dieses Dokument beschreibt, wie das LCM auf einem (selbst gehosteten) GitLab-Server eingerichtet ist und wie man dieselbe Einrichtung für ein neues Projekt reproduziert. Referenz-Installation: `https://gitlab.techeve.de/techeve/LCM`.

## Branching-Modell

```
develop    ── hier wird entwickelt (direkt oder über Feature-Branches mit MR).
beta       ── Vorabversionen (1.24.0-beta.1). Quelle: develop (Release-Zug)
              oder community (Fix-Aufwärtsmerge).
community  ── Default-Branch, der freie stabile Kanal. Quelle: beta (Release)
              oder enterprise (Fix-Aufwärtsmerge).
enterprise ── Wartungszweig mit vertraglicher Zusage. Quelle: fix/*- und
              hotfix/*-Branches - dort entstehen Fixes zuerst.
feature/*  ── optionale Feature-Branches, MR-Ziel ist immer develop.
```

Alle vier Kanal-Branches sind geschützt: kein direkter Push, nur Merge Requests,
Pipeline muss grün sein. Ein Merge **mit neuer VERSION** erzeugt dort automatisch
Tag + Release - ohne Versions-Bump passiert nichts (siehe `check:release-version`).

Der Weg einer Änderung: `feature/xyz` → MR → `develop` → MR → `beta` → MR →
`community`. Fixes für die Wartungslinie laufen umgekehrt: `fix/xyz` → MR →
`enterprise`, danach als Aufwärtsmerge nach `community` und `beta`.

Welcher Kanal welches Publikum bedient, steht in [Repository-Kanäle](/reference/repo-channels/).

## Einrichtung Schritt für Schritt

Alle Schritte gehen über die Web-UI oder - wie hier dokumentiert - über die [GitLab-REST-API](https://docs.gitlab.com/ee/api/) mit einem Personal Access Token (Scope `api`):

```sh
export GITLAB=https://gitlab.techeve.de/api/v4
export TOKEN=<personal-access-token>
```

### 1. Projekt in der Gruppe anlegen

```sh
# Gruppen-ID ermitteln:
curl -s -H "PRIVATE-TOKEN: $TOKEN" "$GITLAB/groups?search=techeve"

# Projekt anlegen (namespace_id = Gruppen-ID):
curl -s -X POST -H "PRIVATE-TOKEN: $TOKEN" "$GITLAB/projects" \
  --data-urlencode "name=LCM" \
  --data-urlencode "namespace_id=3" \
  --data-urlencode "visibility=internal" \
  --data-urlencode "initialize_with_readme=false" \
  --data-urlencode "auto_devops_enabled=false"
```

### 2. Code pushen, Branches anlegen

```sh
git init -b develop && git add -A && git commit -m "Initial import"
git remote add origin https://gitlab.techeve.de/techeve/LCM.git
git push -u origin develop
# Die drei Kanal-Branches zweigen vom selben Stand ab:
for b in beta community enterprise; do git branch "$b" && git push -u origin "$b"; done
```

`develop` als Standard-Branch setzen (neue Clones/MRs starten dort):

```sh
curl -s -X PUT -H "PRIVATE-TOKEN: $TOKEN" "$GITLAB/projects/<id>" \
  --data-urlencode "default_branch=develop"
```

### 3. Kanal-Branches schützen: kein direkter Push, nur Merge Requests

```sh
# Für JEDEN der vier Kanal-Branches (develop, beta, community, enterprise):
# Eventuellen Default-Schutz entfernen, dann strikt neu anlegen.
BRANCH=community
curl -s -X DELETE -H "PRIVATE-TOKEN: $TOKEN" "$GITLAB/projects/<id>/protected_branches/$BRANCH"
curl -s -X POST  -H "PRIVATE-TOKEN: $TOKEN" "$GITLAB/projects/<id>/protected_branches" \
  --data-urlencode "name=$BRANCH" \
  --data-urlencode "push_access_level=0" \
  --data-urlencode "merge_access_level=30" \
  --data-urlencode "allow_force_push=false"
```

- `push_access_level=0` - **niemand** darf direkt pushen (auch Maintainer nicht).
- `merge_access_level=30` - Developer und höher dürfen per Merge Request mergen.

`develop` wird ebenfalls geschützt, aber arbeitsfreundlich (Developer dürfen pushen und mergen, kein Force-Push):

```sh
curl -s -X POST -H "PRIVATE-TOKEN: $TOKEN" "$GITLAB/projects/<id>/protected_branches" \
  --data-urlencode "name=develop" \
  --data-urlencode "push_access_level=30" \
  --data-urlencode "merge_access_level=30" \
  --data-urlencode "allow_force_push=false"
```

**Erlaubte MR-Quellen:** GitLab kann die Quelle eines MR nicht nativ einschränken. Das erzwingt stattdessen der CI-Job `check:mr-source` (siehe `.gitlab-ci.yml`) - und weil die Kanal-Branches nur mit grüner Pipeline mergebar sind, ist die Regel bindend:

| Ziel | erlaubte Quellen |
|---|---|
| `beta` | `develop` (Release-Zug) oder `community` (Fix-Aufwärtsmerge) |
| `community` | `beta` (Release) oder `enterprise` (Fix-Aufwärtsmerge) |
| `enterprise` | `fix/*`, `hotfix/*` oder `community` |

### 4. Merge-Request-Regeln

```sh
curl -s -X PUT -H "PRIVATE-TOKEN: $TOKEN" "$GITLAB/projects/<id>" \
  --data-urlencode "only_allow_merge_if_pipeline_succeeds=true" \
  --data-urlencode "allow_merge_on_skipped_pipeline=false" \
  --data-urlencode "only_allow_merge_if_all_discussions_are_resolved=true" \
  --data-urlencode "remove_source_branch_after_merge=false"
```

- **Pipeline muss grün sein** - damit sind die Tests (Go, E2E, Audits) sowie die Jobs `check:mr-source` und `check:release-version` Pflicht für jeden Merge in einen Kanal-Branch.
- Offene Diskussionen blockieren den Merge (Review-Disziplin).

**Zustimmung eines weiteren Entwicklers:** Die Zahl erforderlicher Approvals wird gesetzt mit:

```sh
curl -s -X POST -H "PRIVATE-TOKEN: $TOKEN" "$GITLAB/projects/<id>/approval_rules" \
  --data-urlencode "name=Mindestens ein Reviewer" \
  --data-urlencode "approvals_required=1"
curl -s -X POST -H "PRIVATE-TOKEN: $TOKEN" "$GITLAB/projects/<id>/approvals" \
  --data-urlencode "merge_requests_author_approval=false"
```

:::note
**GitLab CE:** *Erzwungene* Approval-Regeln (Merge-Button gesperrt bis N Zustimmungen) sind ein Premium-Feature. Auf einem CE-Server (wie hier, `enterprise: false`) können Entwickler MRs zwar approven, die Zustimmung wird aber nicht hart erzwungen - die Vier-Augen-Regel gilt dann als verbindliche Team-Konvention, technisch abgesichert durch `push_access_level=0` (ohne MR geht gar nichts) und die Pflicht-Pipeline. Nach einem Upgrade auf Premium greifen die obigen Befehle sofort hart.
:::

## Die CI/CD-Pipeline

Definiert in `.gitlab-ci.yml`, läuft auf dem Runner mit Tag `docker` (Docker-Executor, Jobs laufen in Images):

| Stage | Job | Zweck |
|---|---|---|
| check | `check:mr-source` | erlaubte MR-Quellen je Kanal-Branch (bricht sonst ab) |
| check | `check:commits` | erzwingt Conventional Commits in MR-Pipelines |
| check | `check:release-version` | Release-Zug (`develop→beta`, `beta→community`): bricht ab, wenn die `VERSION` schon getaggt ist - der Merge erzeugte sonst kein Release |
| check | `version` | nur auf den Kanal-Branches: liest die vorbereitete `VERSION` und prüft, ob dafür schon ein Tag existiert |
| test | `frontend` | `npm ci` + **`npm audit`** (Gate) + Vite-Build; `dist/` als Artefakt |
| test | `backend` | `go vet` + `go test ./...` (In-Memory SQLite) |
| test | `govulncheck` | Go-Schwachstellen-Scan (Gate) |
| test | `e2e` | Playwright gegen das echte Binary (auf develop und den Kanal-Branches sowie in MRs) |
| test | `upgrade-test` | Upgrade von 1.11.0 auf den Prüfstand mit Demo-Daten - **nicht** in jeder Pipeline: Pflicht vor `enterprise`, sonst manuell |
| build | `binaries` | Cross-Compile: Linux amd64/arm64, Windows amd64, macOS arm64/amd64 |
| build | `packages:deb` | .deb-Pakete (amd64/arm64) mit systemd-Dienst via nfpm |
| release | `release` | nur auf den Kanal-Branches: Tag `v<VERSION>` + Release mit dem vorbereiteten Changelog (s.u.) |
| maintenance | `renovate` | Dependency-Bot: prüft Go-/npm-Abhängigkeiten, legt Update-MRs an (nur geplant/manuell, s.u.) |

Versionierung in der CI: Alle Jobs bauen mit dem Stand der `VERSION`-Datei aus dem Commit (auf den Kanal-Branches wird derselbe Wert vom `version`-Job als `NEXT_VERSION` durchgereicht); als Build-Nummer dient die eindeutige Pipeline-Nummer (`CI_PIPELINE_IID`) - die lokale `.buildnumber` wird in der CI nicht verändert.

### Welcher Code läuft dort eigentlich? (Commit-Kennung)

Version und Build-Nummer sagen **nicht** eindeutig, welcher Quellstand in einer Instanz läuft: ein lokal gebautes Binary kann dieselbe Versionsnummer tragen wie ein Release und trotzdem anderen Code enthalten. Deshalb brennt jeder Build zusätzlich seinen **Git-Commit** ein (`-X LCM/internal/version.Commit=…`):

- **CI-Builds** bekommen `$CI_COMMIT_SHA` - der Commit ist damit dem Repository eindeutig zuordenbar.
- **Lokale Builds** (`make build`) nehmen `git rev-parse HEAD` und hängen **`-dirty`** an, wenn der Arbeitsbaum uncommittete Änderungen enthält (`.buildnumber` ausgenommen - reines Build-Artefakt).

Sichtbar ist das an drei Stellen:

```bash
./bin/lcm --version          # LCM 1.5.1 (Build 42, 0a5154cf8f36)
curl .../api/v1/system/info  # {"commit":"0a5154cf8f36","dirty":false, …}
```

sowie im Footer der Oberfläche. Ein Dirty-Build trägt dort zusätzlich ein gelbes Abzeichen **Dev-Build** - er entspricht keinem Commit des Repositorys und darf nie mit einem Release verwechselt werden.

Damit lässt sich eine laufende Instanz jederzeit gegen den Quellstand prüfen:

```bash
git log -1 --oneline <commit-aus-system-info>
```

### Herkunft eines Releases

Der `version`-Job prüft vor jedem Release, dass der zu taggende Commit **tatsächlich auf dem Kanal-Branch liegt** (`git merge-base --is-ancestor`), und bricht sonst ab. Die `rules` binden Release und Deploy ohnehin schon an die Kanal-Branches; die Prüfung hält zusätzlich, falls die Pipeline später umgebaut wird (Tag-Pipeline, manueller Lauf, geänderte `rules`), und belegt die Herkunft im Job-Log.

### Commit-Konvention (Conventional Commits)

Versionsnummer und Changelog entstehen **automatisch aus den Commit-Messages** - deshalb erzwingt der CI-Job `check:commits` in jeder MR-Pipeline das Format:

```
typ(scope): beschreibung        # scope optional
```

| Commit-Typ | Wirkung auf die Version | Changelog-Rubrik |
|---|---|---|
| `feat!:` / `fix!:` oder `BREAKING CHANGE` im Body | **Major** (2.0.0) | 💥 Breaking Changes |
| `feat:` | **Minor** (1.2.0) | 🚀 Features |
| `fix:` | **Patch** (1.1.1) | 🐛 Bugfixes |
| `perf:` | Patch | ⚡ Performance |
| `refactor:` | Patch | ♻️ Refactoring |
| `docs:` `test:` `ci:` `chore:` `build:` `style:` `revert:` | kein Release-Auslöser | 🔧 Sonstiges |

Beispiele: `feat(api): notes-endpunkt`, `fix(ui): navbar-umbruch auf mobilgeräten`, `feat!: config-format v2`.

Der höchste Typ seit dem letzten Release bestimmt den Sprung (ein einziges `feat!` macht aus beliebig vielen `fix` ein Major-Release). Bestehen die Commits seit dem letzten Tag **nur** aus Typen ohne Release-Wirkung (docs, chore, …), entsteht beim Merge in einen Kanal-Branch **kein** neues Release.

Vorschau jederzeit lokal: `make next-version` (bzw. `go run ./tools/release`) zeigt die berechnete nächste Version und den Changelog-Abschnitt an. Die Logik steckt in `tools/release` (Go, mit Unit-Tests).

### Der Upgrade-Test

Am 20.08.2026 startete LCM nach dem Update auf 1.24.0-beta.1 auf dem
Produktivsystem nicht mehr. Die Ursache war eine Migration, die **nur auf einer
bestehenden Datenbank** zuschlägt - die komplette Testsuite lief währenddessen
grün, weil sie ausnahmslos gegen frische Datenbanken arbeitet.

`packaging/upgrade-test/upgrade-test.sh` schließt diese Lücke: Es holt das
Binary einer alten Fassung (`ALT_VERSION`, derzeit 1.11.0) aus den
Release-Assets, startet es mit `--demo` gegen ein frisches Datenverzeichnis,
nimmt den Bestand auf, lässt dann den **Prüfstand** auf dasselbe Verzeichnis
los und vergleicht.

Geprüft wird dreierlei:

1. **Der Dienst startet** - genau das schlug damals fehl.
2. **Ein zweiter Start läuft ebenfalls sauber** - Migrationen müssen
   wiederholbar sein.
3. **Die Daten sind vollständig** - keine Tabelle verschwunden, keine
   Zeilen verloren, jede Kennung wiederfindbar.

#### Warum der Test nicht bei jeder Weiterentwicklung rot wird

Ein stumpfer Vergleich „vorher gleich nachher" wäre unbrauchbar: Wir entwickeln
weiter, Felder kommen hinzu, Daten werden bewusst umgeformt. Ein Test, der
dabei jedes Mal ausschlägt, wird nach dem dritten Mal ignoriert.

Deshalb prüft er **Zusagen statt Zustände**. Alles, was sich absichtlich
ändert, wird einmal in `packaging/upgrade-test/erwartungen.json` erklärt:

```json
{
  "ab_version": "1.19.0",
  "betrifft": "rules",
  "art": "umgezogen",
  "nach": "schedules",
  "begruendung": "Regeln hängen seit 1.19 an Zeitplänen …"
}
```

Bei `umgezogen` gibt sich der Test nicht mit der Ankündigung zufrieden: Er
rechnet nach, ob die fehlenden Zeilen in der Zieltabelle **angekommen** sind.

Eine bewusste Datenänderung kostet damit drei Zeilen Erklärung - eine
unbewusste fällt auf. Wer eine Migration schreibt, sagt dazu, was sie tut.

#### Die Verschlüsselungsgrenze

Ab 1.15 werden Benutzer- und Servernamen at rest verschlüsselt; daneben tritt
ein Blindindex. Über diese Grenze hinweg sind Kennungen **nicht vergleichbar** -
vorher Klartext, nachher Hashwert. Der Test erkennt das und prüft dort
stattdessen, dass jede Zeile weiterhin eine eigene, nicht leere Kennung trägt.
Ein stiller Verlust fiele weiterhin auf: als fehlende oder doppelte Kennung.

### Release vorbereiten (auf develop) & veröffentlichen (im Kanal)

Der Changelog gehört in **genau den Commit, der getaggt wird**. Deshalb werden Version und Changelog **vor** dem Merge auf `develop` vorbereitet - nicht nachträglich in der CI erzeugt. Damit trägt der in den Kanal überführte Commit bereits den passenden Changelog, der Kanal hinkt nie einer Version hinterher, und die CI braucht **keinen Schreib-Token**.

**Schritt 1 - auf `develop` vorbereiten** (`packaging/prepare-release.sh`):

```sh
git switch develop && git pull
./packaging/prepare-release.sh          # Version aus den Commits seit dem letzten Tag
# oder eine explizite Version erzwingen (z.B. Beta -> Finale):
./packaging/prepare-release.sh 1.0.0
```

Das Skript ermittelt mit `tools/release` die nächste Version, schreibt `VERSION`, stellt den neuen Abschnitt oben in `CHANGELOG.md` fort und committet beides als `release: v<version> - Version & Changelog vorbereitet`. Gibt es seit dem letzten Tag keine release-relevanten Commits (nur `docs`/`chore`/…), passiert nichts - außer man gibt eine explizite Version an.

```sh
git push origin develop
```

**Schritt 2 - Merge Request `develop → beta`** erstellen und nach grüner Pipeline mergen (für ein finales Release danach `beta → community`). Danach läuft auf dem Ziel-Branch automatisch:

1. **`version`-Job**: liest `NEXT_VERSION` aus der committeten `VERSION`, prüft ob `v<version>` schon als Tag existiert (`RELEASE_NEEDED`) und schneidet den obersten Abschnitt aus `CHANGELOG.md` als Release-Beschreibung heraus.
2. **`binaries` / `packages:deb`**: bauen alle Plattform-Binaries und die `.deb`-Pakete mit dieser Version.
3. **`release`-Job** (nur wenn der Tag noch nicht existiert): lädt die Binaries in die Generic Package Registry und erzeugt **Tag `v<version>` + Release** mit dem Changelog als Beschreibung und den Binaries als Assets.
4. **`deploy:apt`**: rollt die `.deb`-Pakete auf den Repository-Server aus (s.u.).

**Der Bump ist Pflicht, und die CI besteht darauf.** Wird Schritt 1 vergessen, trägt der Merge die alte `VERSION` auf den Release-Branch. Der `version`-Job findet deren Tag vor, setzt `RELEASE_NEEDED=false` und lässt Release und Deploy leer laufen - der Code liegt dann auf `beta`, steckt aber in keinem Paket. Damit das nicht erst auffällt, wenn jemand das Artefakt sucht, rechnet `check:release-version` dieselbe Bedingung bereits in der MR-Pipeline durch und lässt den Merge Request scheitern, solange die Version nicht hochgezogen ist. Der Job läuft nur für die beiden Richtungen, in denen der Merge selbst releasen soll (`develop→beta`, `beta→community`); Fix-Aufwärtsmerges tragen bewusst keine neue Version und sind ausgenommen. War der ausbleibende Release Absicht, hebt die CI-Variable `ALLOW_NO_RELEASE=true` am MR die Prüfung auf.

**Kein Writeback, kein `RELEASE_TOKEN`:** Version und Changelog stehen bereits im getaggten Commit (und über den Merge auch auf `develop` und im Kanal). Die CI schreibt nichts ins Repo zurück - sie liest nur. Der Release-Job kommt mit dem automatischen `CI_JOB_TOKEN` aus.

### apt-Repository-Rollout (deploy)

Bei jedem Release rollt der Job `deploy:apt` die gebauten `.deb`-Pakete (amd64 + arm64) auf den TechEve-Repository-Server (aptly) aus - danach ist LCM per `apt install lcm` aus dem eigenen Repo installier- und aktualisierbar. Der Job läuft nur auf den Kanal-Branches und nur, wenn tatsächlich ein Release ansteht (`RELEASE_NEEDED=true`).

Benötigte CI-Variablen (als **maskiert** hinterlegen):

| Variable | Beispiel | Zweck |
|---|---|---|
| `REPO_URL` | `https://repo.techeve.de` | Basis-URL der aptly-HTTP-API |
| `REPO_USER` | `gitlab-ci` | Basic-Auth-Benutzer |
| `REPO_PASS` | *(geheim)* | Basic-Auth-Passwort |

Optional: `REPO_NAME` (Default `techeve`), `DISTRO` (Default `stable`), `GPG_KEY` (Default `repo@techeve.de`). Ablauf und Skript: `packaging/publish-deb.sh`. Fehlen die Pflicht-Variablen, bricht der Job mit einer klaren Meldung ab. SemVer-Prereleases (`-beta.1`) werden für das Debian-Paket zu `~beta.1` umgeschrieben, damit die Beta laut apt korrekt **vor** dem späteren Finale sortiert.

### Container-Images (deploy)

Der Job `images` baut zwei Images für **amd64 und arm64** und veröffentlicht sie in der Projekt-Registry (`registry.techeve.de/techeve/lcm`):

| Image | Inhalt |
|---|---|
| `…/lcm` | Das Runtime-Image auf Basis `scratch` - rund 37 MB |
| `…/lcm/trivyd` | Der Trivy-Sidecar (offizielles Trivy-Image + `cmd/trivyd`) |

Tags je Kanal, die Version immer plus der bewegliche Zeiger:

| Branch | Tags |
|---|---|
| `beta` | `:<version>` und `:beta` |
| `community` | `:<version>` und `:latest` |
| `enterprise` | `:<version>` und `:enterprise` (bleibt in der privaten Registry) |

Zwei Dinge sind dabei bewusst so gebaut:

**Gepusht wird nur zusammen mit einem Release.** Der Job liegt in der `deploy`-Stage und hängt über `needs: [release]` (optional) am Release-Job - ein Image-Tag entsteht also erst, wenn das Release wirklich existiert. Ohne `RELEASE_NEEDED=true` wird nichts veröffentlicht; sonst gäbe es Tags ohne Release dahinter.

**Auf `develop` und in MRs wird trotzdem gebaut** (`--output=type=cacheonly`, das Ergebnis landet nirgends). Ein kaputtes Dockerfile fällt damit vor dem Release auf und nicht erst, wenn es zählt.

Multi-Arch kostet dabei fast nichts: Im Runtime-Abschnitt beider Dockerfiles gibt es **kein `RUN`** - es wird nur kopiert, also braucht arm64 keine QEMU-Emulation. Gebaut wird per `docker buildx` mit dem `docker-container`-Treiber (der Standard-Treiber kann keine Manifest-Listen).

Voraussetzung: ein Runner mit **privilegiertem dind** (wie beim Doku-Builder-Image). Der `29.6`-Pin stammt von dort und ist dort begründet - ab `>=29.7` meldet der Push gegen diese GitLab-Registry „blob unknown to registry".

### Warum die Go-Version in `go.mod` steht und nicht nur im CI-Image

Das Sicherheits-Tor `govulncheck` bewertet auch die **Standardbibliothek**. Welche Go-Version dabei zum Einsatz kommt, entschied früher allein das CI-Image `golang:1-alpine` - ein beweglicher Tag, den Runner zwischenspeichern. Ergebnis: Derselbe Code lief auf einem Runner mit frischem Image grün und auf einem mit altem Cache rot, ohne dass jemand etwas geändert hatte. Genau das ist einmal passiert (sechs Lücken der Standardbibliothek, behoben in Go 1.26.6).

Der `toolchain`-Eintrag in `go.mod` ist der verlässlichere Hebel: Go lädt bei Bedarf selbst die passende Toolchain nach, unabhängig davon, was im Image liegt. Damit ist die Untergrenze im Repository festgeschrieben und für jeden Bau gleich - lokal wie in der CI. Renovate hält den Eintrag über den `gomod`-Manager aktuell.

### Dependency-Bot (Renovate)

Damit die Abhängigkeiten nicht veralten, prüft **Renovate** regelmäßig die
Go-Module (`gomod`) und die npm-Pakete (`npm`) auf neuere Versionen und legt
die Aktualisierungen **automatisch als Merge Requests nach develop** an. Jeder
dieser MRs durchläuft die volle Pipeline (Tests, E2E, Build, Paketierung) - ein
Bump wird also erst grün, wenn er nachweislich kompiliert und die Tests besteht.

Renovate läuft **ausschließlich als CI-Image** (`renovate/renovate`) und ist
damit **keine Projekt-Abhängigkeit** - go.mod und package.json bleiben unberührt.
Konfiguration: `renovate.json` im Repo-Root.

**Verhalten (aus `renovate.json`):**

- **Ziel-Branch** ist `develop` (`baseBranches`), niemals ein Kanal-Branch.
- **Gruppierung:** alle Go-minor/patch-Updates in einem MR, alle npm-minor/patch-Updates in einem MR; **Major-Updates** kommen einzeln (bessere Review-Kontrolle).
- **Automerge:** **Patch-Updates** werden nach grüner Pipeline automatisch gemergt (`platformAutomerge` = „mergen, wenn Pipeline erfolgreich"); **minor/major** bleiben als MR zur manuellen Review offen.
- **Conventional Commits:** Renovate committet als `chore(deps): …` - passt den `check:commits`-Job und löst als `chore` **kein** Release aus (die neue Version entsteht erst, wenn die Bumps später über den regulären Release-Zug fließen).
- `go mod tidy` bzw. `npm dedupe` laufen nach jedem Update (`postUpdateOptions`), damit `go.sum`/Lockfile konsistent bleiben.
- Sicherheits-relevante Updates (bekannte CVEs in Abhängigkeiten) werden über OSV erkannt, mit dem Label `security` markiert und **nicht** automatisch gemergt.
- Ein **Dependency-Dashboard**-Issue gibt jederzeit den Überblick über anstehende/ignorierte Updates.

**Einmalige Einrichtung:**

1. **Access Token** für den Bot erzeugen (Project- oder Group Access Token, Scope `api` + `write_repository`, Rolle Developer genügt zum Anlegen/Mergen auf develop):

   ```sh
   curl -s -X POST -H "PRIVATE-TOKEN: $TOKEN" "$GITLAB/projects/<id>/access_tokens" \
     --data-urlencode "name=renovate-bot" \
     --data-urlencode "scopes[]=api" \
     --data-urlencode "scopes[]=write_repository" \
     --data-urlencode "access_level=30" \
     --data-urlencode "expires_at=2027-07-04"
   ```

2. Token als **maskierte, geschützte** CI-Variable `RENOVATE_TOKEN` hinterlegen. `protected=true` ist möglich, weil `develop` ein geschützter Branch ist (auf dem die geplante Bot-Pipeline läuft) - so ist der Token nicht in Pipelines beliebiger Feature-Branches sichtbar:

   ```sh
   curl -s -X POST -H "PRIVATE-TOKEN: $TOKEN" "$GITLAB/projects/<id>/variables" \
     --data-urlencode "key=RENOVATE_TOKEN" --data-urlencode "value=<bot-token>" \
     --data-urlencode "masked=true" --data-urlencode "protected=true"
   ```

   Optional für Changelog-Abruf von GitHub-gehosteten Deps: dieselbe Prozedur mit `RENOVATE_GITHUB_TOKEN` (ein GitHub-PAT ohne Scopes reicht - nur zum Lesen öffentlicher Release-Notes).

3. **Geplante Pipeline** anlegen, die den Bot z.B. wöchentlich auf `develop` mit `RENOVATE_BOT=true` startet:

   ```sh
   curl -s -X POST -H "PRIVATE-TOKEN: $TOKEN" "$GITLAB/projects/<id>/pipeline_schedules" \
     --data-urlencode "description=Renovate Dependency-Bot" \
     --data-urlencode "ref=develop" \
     --data-urlencode "cron=0 6 * * 1" \
     --data-urlencode "cron_timezone=Europe/Berlin" \
     --data-urlencode "active=true"
   # ID der eben angelegten Schedule merken und die Trigger-Variable setzen:
   curl -s -X POST -H "PRIVATE-TOKEN: $TOKEN" \
     "$GITLAB/projects/<id>/pipeline_schedules/<schedule-id>/variables" \
     --data-urlencode "key=RENOVATE_BOT" --data-urlencode "value=true"
   ```

Auf dieser geplanten Pipeline läuft **nur** der `renovate`-Job - die regulären
Test-/Build-Jobs sind über den `.skip-on-renovate`-Anker stillgelegt. Ein
manueller Lauf ist jederzeit über *Build → Pipeline schedules → ▶* oder über
*Run pipeline* mit der Variablen `RENOVATE_BOT=true` möglich. Fehlt
`RENOVATE_TOKEN`, bricht der Job mit einer klaren Meldung ab.

### Runner-Voraussetzungen

- Ein Runner mit **Docker-Executor** und Tag `docker` (die Jobs setzen `tags: [docker]`).
- Internetzugang für die Images (`golang:1-alpine`, `node:lts`, `alpine:3`, `docker:29.6-cli`/`-dind`, `aquasec/trivy`, `registry.gitlab.com/gitlab-org/release-cli`, `renovate/renovate`) sowie Go-Module/npm-Pakete/Playwright-Browser.
- Für den `images`-Job: **privilegiertes Docker-in-Docker** (`[runners.docker] privileged = true`, `volumes = ["/certs/client", "/cache"]`) - dieselbe Voraussetzung wie beim Doku-Builder-Image.
- Der Release-Job arbeitet allein mit dem automatischen `CI_JOB_TOKEN` - ein Schreib-Token ist **nicht** nötig, weil Version und Changelog vorab auf develop committet werden. Der **apt-Deploy** braucht `REPO_URL`/`REPO_USER`/`REPO_PASS` (s.o.) und der **Dependency-Bot** `RENOVATE_TOKEN` (s.o.). Alle übrigen Jobs brauchen keine Secrets.

## Rollen & Rechte im Projekt

| Rolle | darf |
|---|---|
| Developer | auf develop pushen, MRs stellen/mergen (develop und Kanal-Branches), Reviews |
| Maintainer | zusätzlich Einstellungen, geschützte Branches verwalten |
| - (alle) | **nicht** direkt auf einen Kanal-Branch pushen - ausnahmslos |
