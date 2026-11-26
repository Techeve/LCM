# Wochen-Release: Abhängigkeiten und automatische Patch-Releases

Stand 25.11.2026. Entscheidungen von Tony vom selben Tag (gelten für LCM, RSM und ZPO);
dieser Plan betrifft LCM. Beschreibung im Betrieb: `docs/reference/ci-release.md`,
Abschnitt „Automatische Wochen-Releases“.

## Ziel

Abhängigkeiten bleiben aktuell, und was davon ins Produkt geht, erscheint als Patch-Release
auf community und enterprise, ohne dass jemand etwas anstößt. Die Releases fließen von selbst
nach beta und develop zurück.

## Entscheidungen

1. Normale Renovate-Updates montags ab 8:30 (Zeitplan in `renovate.json`, geplante Pipeline
   täglich 8:30), Sicherheitsupdates jederzeit mit Label `security` und Suffix `[security]`.
   Patch-Release dienstags 6:00, bei Sicherheitsupdates am nächsten täglichen Lauf.
2. Alle Minor- und Patch-Updates mergen selbst (7 Tage Karenz, außer Sicherheit, Go-Toolchain,
   Playwright). Majors nur nach Freigabe im Dashboard. `prConcurrentLimit` 15.
3. Release-Anlass ist, was ins Produkt geht (`fix(deps)`: Go-Module, Toolchain, npm-
   `dependencies`, ausgelieferte Basis-Images). Werkzeuge bleiben `chore(deps)`. Der Changelog
   führt sie als eine Zeile „Abhängigkeiten aktualisiert“ (mit Sicherheit: „… mit
   Sicherheitskorrekturen aktualisiert“).
5. Renovate beliefert develop, community und enterprise (enterprise nur Patch und Sicherheit).
   Release-Job je Linie, nächste freie Patch-Nummer, MR aus `hotfix/auto-<version>` mit
   Automerge. Automatisches Zurückführen community → beta → develop, Konflikte in `VERSION`,
   `CHANGELOG.md`, `go.mod`, `go.sum` nach festen Regeln, sonst Issue `release-automatik`.
   enterprise wird nicht hochgeführt.
6. Token: vorhandenes `RENOVATE_TOKEN`, keine neuen Konten.

## Pakete

| Paket | Inhalt | Stand |
|---|---|---|
| W1 | MCP-Test deterministisch (Renovate-MR !191 hing daran) | fertig, zu prüfen |
| W2 | `renovate.json`: Basis-Branches, Zeitplan, Regeln je Linie, Commit-Typen, Playwright ohne Karenz | fertig, zu prüfen |
| W3 | `tools/release`: Abhängigkeits-Commits zu einer Zeile, Tests | fertig, zu prüfen |
| W4 | `tools/autorelease`: Entscheidung, Patch-Release, Aufwärtsmerge, Konfliktregeln, Tests | fertig, zu prüfen |
| W5 | `.gitlab-ci.yml`: `check:mr-source`, Jobs `release:patch` und `merge:up`, Bot-Regeln | fertig, zu prüfen |
| W6 | Doku (`ci-release.md`, `dependencies.md`, de und en) | fertig, zu prüfen |
| W7 | GitLab: Zeitpläne und Variablen anlegen (siehe unten) | offen, Tony |
| W8 | Rückport nach community (`hotfix/*`) und enterprise (`fix/*`) | offen, Tony |
| W9 | Offene Renovate-MRs (!191, !207, !214, !216, !238) nach dem Rückport neu bewerten | offen |

## Was in GitLab zu tun ist (W7)

Geplante Pipelines, jeweils `ref=develop`, Zeitzone Europe/Berlin:

| Beschreibung | cron | Variable |
|---|---|---|
| Renovate Dependency-Bot (vorhanden, Zeit ändern) | `30 8 * * *` | `RENOVATE_BOT=true` |
| Wochen-Release | `0 6 * * *` | `RELEASE_PATCH=true` |
| Aufwärtsmerge (Nachzügler) | `0 7 * * *` | `MERGE_UP=true` |

Der Bot-Token (Rolle Developer) muss in beta, community und enterprise MRs mergen dürfen.
Anhalten: CI-Variable `AUTO_RELEASE_PAUSE=true`.

## Rückport (W8)

Nach community über `hotfix/*`, nach enterprise über `fix/*`: `.gitlab-ci.yml`,
`renovate.json`, `tools/release/`, `tools/release-notes.sh`, `packaging/prepare-release.sh`.
`renovate.json` liest Renovate vom Default-Branch community, es wirkt also erst dort.

## Risiken

- Automerge für Minor-Updates, auch der Go-Toolchain, trägt ein Compilerwechsel ohne
  Menschenblick in ein Patch-Release auf community.
- npm-Konflikte (`ui/package.json`, `package-lock.json`) beim Aufwärtsmerge löst die
  Automatik nicht; sie meldet ein Issue.
- Der erste Lauf auf enterprise zeigt, ob die Regel „nur Patch“ auch Sicherheitsupdates mit
  Minor-Sprung durchlässt (`vulnerabilityAlerts.enabled`).
