---
sidebar:
  order: 28
title: Abhängigkeiten & Supply Chain
description: Wie LCM neue Abhängigkeiten bewertet, wann Eigenbau die bessere Wahl ist und wie der Update-Pfad gegen Supply-Chain-Angriffe abgesichert ist.
---

LCM kompiliert zu einem einzigen Binary, das auf fremden Servern mit weitreichenden
Rechten läuft. Jede Abhängigkeit, die wir aufnehmen, läuft dort mit. Diese Seite
legt fest, wie wir Abhängigkeiten bewerten und welche Regeln beim Aufnehmen,
Aktualisieren und Entfernen gelten.

## Grundsatz

**Keine externe Abhängigkeit für ein Problem, das sich mit wenigen Zeilen eigenem
Code lösen lässt.** Umgekehrt gilt aber genauso: kein Eigenbau bei Kryptographie
und Protokoll-Parsing - dort ist die selbst eingebaute Lücke wahrscheinlicher als
der Supply-Chain-Angriff, den man vermeiden wollte.

## Die zwei Achsen

Jede Abhängigkeit wird entlang zweier Achsen bewertet. Erst beide zusammen
ergeben eine Entscheidung.

### Achse 1 - Trägerschaft (Vertrauen 1-5)

Wie schwer hätte es ein Angreifer, eine bösartige Version durch den
Veröffentlichungsprozess zu bekommen?

| Stufe | Merkmal | Beispiele im Bestand |
|:-:|---|---|
| 5 | Go-Team/Google oder vergleichbar: mehrstufiges Review, öffentliche Historie, Security-Team | `golang.org/x/*`, `google/uuid`, `svelte`, `vite`, `@playwright/test` |
| 4 | Organisation mit mehreren aktiven Maintainern und Release-Prozess | `gofiber/fiber`, `gorm.io/gorm`, `golang-jwt/jwt`, `eclipse/paho.mqtt.golang`, `bootstrap` |
| 3 | Kleine Organisation oder renommierte Einzelperson, nachweislich aktiv | `mochi-mqtt/server`, `fasthttp/websocket`, `svelte-spa-router` |
| 2 | Einzelperson, sporadische Releases, Bus-Faktor 1 | `glebarez/sqlite` |
| 1 | Einzelperson, seit Jahren inaktiv, kein Release-Prozess, ungetaggte Versionen | - (bewusst leer, siehe unten) |

Stufe 1 ist im Bestand bewusst leer: Solche Abhängigkeiten werden nicht
aufgenommen und bestehende werden abgelöst.

### Achse 2 - Nutzfläche und Ablösbarkeit

Nicht „wie groß ist die Bibliothek", sondern **wie viel davon benutzen wir
wirklich** und was kostet der Ersatz - inklusive des Risikos, das wir uns beim
Ersatz selbst einbauen.

Eine Bibliothek mit 90 Zeilen Nutzfläche kann trotzdem unersetzbar sein, wenn
diese 90 Zeilen Kryptographie sind. Umgekehrt ist eine Abhängigkeit, von der wir
genau eine Funktion aufrufen, ein Streichkandidat - unabhängig von ihrer Größe.

## Entscheidungsregel

Aus beiden Achsen folgt die Regel, die im Zweifel gilt:

**Eigenbau ja**, wenn alle drei Punkte zutreffen:

- keine Kryptographie,
- kein Parsing von Daten, die von außen kommen (Netzwerk-Frames, fremde Dateiformate),
- das Fehlerbild bei einem Bug ist harmlos und fällt sofort auf.

**Eigenbau nein**, sobald einer dieser Punkte auftaucht: Passwörter, Token, TLS,
Signaturen, Netzwerkprotokolle, Zeitzonendaten, Unicode-Tabellen.

Beispiele aus dem Bestand: Logrotation (`internal/logging/rotate.go`) und
QR-Kodierung (`internal/infrastructure/totp/qr.go`) sind selbst gebaut - kein
Krypto, keine Fremdeingabe, harmloses Fehlerbild. Passwort-Hashing, JWT, SSH und
der MQTT-Broker bleiben extern, obwohl die Nutzfläche teils klein ist.

## Checkliste vor jeder neuen Abhängigkeit

Vor `go get` bzw. `npm install` beantworten - im Merge Request nachvollziehbar:

1. **Trägerschaft** - Organisation oder Einzelperson? Wann kam das letzte Release?
   Wird das Repository noch bewegt? (Go: `https://proxy.golang.org/<modul>/@latest`,
   npm: `npm view <paket> time.modified maintainers`)
2. **Nutzfläche** - welche Funktionen brauchen wir konkret? Wenn es eine oder zwei
   sind: geht es ohne?
3. **Transitive Fracht** - was zieht das Paket mit? (`go mod graph`, `npm ls`)
   Ein Paket mit zwanzig Kleinstabhängigkeiten ist zwanzig neue Risiken.
4. **Entscheidungsregel** - fällt die Aufgabe in die Eigenbau-Kategorie?
5. **Aktuelle Version** - nie die aus dem Gedächtnis, immer die Registry fragen.

Trägerschaft unter Stufe 3 wird nur mit ausdrücklicher Begründung aufgenommen.

## Der Update-Pfad ist das eigentliche Risiko

`go.sum` samt Checksum-Log und die `integrity`-Hashes in `package-lock.json`
machen es praktisch unmöglich, eine bereits eingebundene Version nachträglich zu
manipulieren. **Ein Supply-Chain-Angriff läuft deshalb immer über eine _neue_
Version.** Daraus folgen drei Regeln, die im Repo verankert sind:

- **Karenzzeit.** [renovate.json](https://gitlab.techeve.de/techeve/lcm/-/blob/community/renovate.json)
  setzt `minimumReleaseAge` auf sieben Tage. Das ist das Fenster, in dem
  kompromittierte Pakete üblicherweise entdeckt und zurückgezogen werden.
- **Automerge nur für Stufe 4 und 5.** Patch-Updates mergen selbsttätig nur bei
  Herausgebern mit belastbarem Release-Prozess. Alles andere braucht einen
  Menschenblick - auch Patches. Sicherheits-Updates (`vulnerabilityAlerts`)
  mergen grundsätzlich nie automatisch.
- **Keine Installationsskripte.** CI und `make` installieren npm-Pakete mit
  `--ignore-scripts`. `postinstall`-Skripte sind der meistgenutzte Angriffsweg in
  npm, und sie laufen auf dem Runner mit Zugriff auf CI-Variablen und
  Signaturschlüssel. Playwright holt seine Browser explizit per
  `npx playwright install`.

`govulncheck` und `npm audit` sind Pflicht-Gates, ersetzen das aber **nicht**:
Sie prüfen bekannte Schwachstellen, ein frischer Angriff ist per Definition
unbekannt. Beide Gates niemals deaktivieren, um eine Pipeline grün zu bekommen.

## Build-Zeit zählt genauso wie Laufzeit

Vite, Svelte und Playwright landen nie im ausgelieferten Binary - sie laufen auf
dem CI-Runner, mit Zugriff auf Registry-Tokens, Signaturschlüssel und
CI-Variablen. Für Build-Werkzeuge gelten dieselben Regeln wie für
Laufzeitabhängigkeiten.

## Bestand im Blick behalten

**`glebarez/sqlite` ist der einzige Punkt mit hoher Kritikalität und schwacher
Trägerschaft** (Stufe 2): Einzelmaintainer, seltene Releases - und darunter hängt
die komplette Datenhaltung. Das ist heute kein akutes Problem, aber der Punkt, an
dem ein Wegbrechen des Maintainers am teuersten käme.

**Plan B:** [`github.com/ncruces/go-sqlite3`](https://github.com/ncruces/go-sqlite3)
ist die aktiv gepflegte CGO-freie Alternative (WASM-basiert) und bringt einen
eigenen GORM-Treiber mit. Auslöser für den Wechsel - einer genügt:

- eine bekannte Schwachstelle bleibt länger als 30 Tage ohne Release,
- das Repository ist zwölf Monate ohne jede Aktivität,
- das Projekt wird archiviert oder an einen unbekannten Dritten übergeben.

Der Wechsel betrifft nur `internal/storage/database.go`; die Repository-Schicht
bleibt unberührt. Vor einem Wechsel Migrationen und Backup/Restore gegen eine
bestehende Datenbank testen - die Treiber unterscheiden sich in Details der
Typkonvertierung.

## Abhängigkeit entfernen

Wenn eine Abhängigkeit ersetzt wird, gehört ein **Differenztest** dazu: Solange
beide Implementierungen nebeneinander existieren, wird die eigene gegen die alte
geprüft und das Ergebnis als feste Testvektoren eingefroren. Erst danach fliegt
die Abhängigkeit aus `go.mod` bzw. `package.json`. Vorlage: die Testvektoren in
`internal/infrastructure/totp/qr_test.go`.
