# Mitmachen bei LCM

*English summary: LCM is developed in an internal repository; this public
repository receives the `community` and `beta` branches on every release. Ideas
and feature requests are welcome as issues, code contributions as merge
requests. Accepted MRs are reviewed here, then cherry-picked into the internal
`develop` branch - your authorship is preserved - and the MR is closed with a
note naming the release your change will ship in. All contributions require
agreeing to the [Contributor License Agreement](CLA.md).*

## Wie LCM entwickelt wird

Die Entwicklung von LCM findet in einem **internen Repository** statt. Dieses
öffentliche Repository ist der Quellcode der Community-Edition: Es erhält bei
jedem Release automatisch den Stand der Branches `community` (stabile Releases)
und `beta` (Vorabversionen) - inklusive vollständiger History und aller
Release-Tags. Hier wird nicht direkt gearbeitet; die Branches werden von der CI
gespiegelt.

Das ist bewusst so gewählt (interne Roadmap, Enterprise-Wartungszweig), ändert
aber nichts am Open-Source-Charakter: Der komplette Quellcode jedes Releases
steht hier unter der [AGPL-3.0](LICENSE).

## Ideen, Wünsche, Fehlermeldungen

**Issues sind der Briefkasten des Projekts** - sehr gerne für:

- Fehlermeldungen (bitte mit LCM-Version, Distribution und Schritten zur
  Reproduktion)
- Feature-Wünsche und Verbesserungsvorschläge
- Fragen zur Nutzung, die die [Dokumentation](https://doc.techeve.de/lcm/) nicht
  beantwortet

Wir besprechen die Vorschläge intern und geben das Ergebnis im Issue zurück -
auch wenn etwas nicht umgesetzt wird, erfährst du warum.

## Code beitragen (Merge Requests)

Merge Requests sind willkommen. Der Ablauf weicht wegen des internen
Entwicklungsmodells etwas vom Üblichen ab:

1. **Fork & MR wie gewohnt** - Basis ist der `community`-Branch (Fixes für die
   Beta gegen `beta`). Bitte Conventional Commits verwenden
   (`feat: …`, `fix: …`) und dafür sorgen, dass `make test` und `go vet ./...`
   grün sind.
2. **Review findet hier statt** - Diskussion und Änderungswünsche laufen ganz
   normal im MR.
3. **Übernahme per Cherry-pick** - angenommene Commits werden in den internen
   `develop`-Branch übernommen. **Deine Autorenschaft bleibt dabei erhalten**
   und erscheint mit dem nächsten Release in der öffentlichen History.
4. **MR wird manuell geschlossen** - mit einem Kommentar, in welchem Release
   deine Änderung erscheint. (GitLab kann den MR nicht automatisch als
   „merged" markieren, weil sich der Commit-Hash beim Cherry-pick ändert.)

### Contributor License Agreement (CLA)

Damit wir Beiträge annehmen können, brauchen wir dein Einverständnis zur
[CLA](CLA.md) - einmalig, formlos per Kommentar im ersten MR:

> Ich stimme der CLA (CLA.md) zu. / I agree to the CLA (CLA.md).

Ohne diese Zustimmung dürfen wir den Beitrag nicht übernehmen.

## Sicherheitslücken

Bitte **nicht** als öffentliches Issue melden, sondern per E-Mail an
**security@techeve.de**. Wir bestätigen den Eingang, halten dich auf dem
Laufenden und nennen dich (wenn gewünscht) in den Release Notes.
