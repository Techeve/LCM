---
sidebar:
  order: 13
title: Anwendungen
description: Software überwachen, die nicht über die Paketverwaltung installiert wurde - Erkennung, Versionsstand, Sicherung und Update.
---

AdGuard Home liegt unter `/opt`, Nextcloud im Webroot, mailcow ist ein
git-Checkout: Für apt oder dnf existiert nichts davon. Gerade diese
Anwendungen sind aber von außen erreichbar, tragen Sicherheitslücken und
wollen aktualisiert werden - und ohne eigenes Zutun steht keine von ihnen auf
irgendeiner Liste.

Der Reiter **Anwendungen** auf der Server-Detailseite schließt diese Lücke.
Er besteht aus zwei Teilen mit unterschiedlichem Anspruch.

## Erkannte Anwendungen

Was LCM im **Anwendungskatalog** kennt, wird beim vollständigen Scan gesucht
und mit Fundort und installierter Version aufgeführt. Ist für die Anwendung
eine Quelle hinterlegt, steht daneben die neueste Version - und die Zeile wird
gelb, wenn die installierte dahinter zurückbleibt.

Anwendungen, die auf diesem Server aus der **Paketverwaltung** stammen,
erscheinen hier bewusst nicht: Sie stehen im Reiter *Pakete* und werden dort
auch aktualisiert. Ob ein Katalogeintrag greift, entscheidet also der einzelne
Server - dieselbe Software kann auf dem einen Host ein Paket und auf dem
anderen eine Handinstallation sein.

## Ohne Zuordnung

Darunter steht, was der Katalog nicht kennt: laufende systemd-Dienste, deren
Unit-Datei zu keinem Paket gehört. LCM weiß nicht, was das ist - aber dass es
an der Paketverwaltung vorbei installiert wurde.

Dieser Teil ist der wichtigere. Ein Katalog zeigt nur, was jemand vorher
eingetragen hat; der generische Fund zeigt auch, was niemand auf dem Schirm
hatte. Wer einen Eintrag daraus überwachen will, legt dafür einen
Katalogeintrag an - Dienstname und Programmpfad stehen in der Zeile.

## Der Anwendungskatalog

**Einstellungen → Anwendungen.** LCM liefert Einträge für gängige Software mit
(AdGuard Home, Nextcloud, mailcow, Seafile, MinIO, RustDesk, Odoo, Intrexx und
die Techeve-Produkte); eigene lassen sich ergänzen. Ein Eintrag besteht aus:

**Merkmale** - eine Zeile je Merkmal, `art wert`, erster Treffer gewinnt:

| Art | Bedeutung |
|---|---|
| `path` | Datei oder Verzeichnis existiert. Der Fundort wird zum Installationspfad. |
| `unit` | Es gibt eine systemd-Unit dieses Namens. |
| `bin` | Das Programm liegt im PATH. |
| `proc` | Ein Prozess dieses Namens läuft. |

`proc` steht bewusst als schwächstes Merkmal am Ende: Intrexx läuft als `java`,
Nextcloud als `php-fpm` - als alleiniges Kennzeichen wäre das wertlos.

**Versionskommando** - `{path}` wird durch den Fundort ersetzt. Auch eine
Versionsdatei ist damit abgedeckt: `cat {path}/VERSION`. Das Kommando läuft
beim Scan als root auf jedem Server, auf dem ein Merkmal greift, und ist auf
15 Sekunden begrenzt. Bleibt das Feld leer, meldet LCM nur, *dass* die
Anwendung da ist.

**Versions-Muster** - ein regulärer Ausdruck; die erste Klammergruppe ist die
Version. `AdGuard Home, version v0.107.52` wird mit
`v?([0-9]+\.[0-9]+\.[0-9]+)` zu `0.107.52`.

**Vergleich** - `Versionsnummern` zerlegt in Zahlen (1.10 ist neuer als 1.9),
`Zeichengleich` gilt für Datumsstände und Build-Kennungen, `Nur anzeigen`
bewertet nie. Die dritte Möglichkeit ist keine Verlegenheit: Ein Reiter, der zu
Unrecht „veraltet" meldet, ist nach dem zweiten Fehlalarm verbrannt.

**Quelle der neuesten Version** - `github:owner/repo` für die jüngste Freigabe
auf GitHub, sonst `url:https://…` zusammen mit einem Muster. Die Abfrage hängt
an der Anwendung, nicht am Server: Bei 40 Servern mit derselben Anwendung wäre
alles andere 40-mal dieselbe Anfrage. Sie läuft täglich als
**Anwendungs-Check** im System-Zeitplan mit.

**Sicherung und Update** - beides sind [Eigene Aktionen](/guides/groups-rules/).
Der Verweis statt einer rohen Kommandozeile ist Absicht: So laufen sie über
denselben geprüften Weg wie jede andere Aktion, mit Job-Protokoll und
Nachvollziehbarkeit. Im Reiter erscheinen dafür die Schaltflächen *Sichern* und
*Aktualisieren*.

:::caution[Erst sichern, dann aktualisieren]
Ist eine Sicherung hinterlegt, läuft sie vor dem Update. **Schlägt sie fehl,
läuft das Update nicht** - eine fehlgeschlagene Sicherung ist der Moment, in
dem man ein Update am wenigsten gebrauchen kann.
:::

## Zweisprachige Einträge

Name und Beschreibung stehen je Eintrag einmal deutsch und einmal englisch -
der Katalog liegt in der Datenbank, nicht im Sprachkatalog der Oberfläche, und
ohne zweites Textfeld stünde in der englischen Ansicht deutscher Text. Die
mitgelieferten Einträge tragen beide Fassungen. Bei eigenen ist die englische
**optional**: Bleibt sie leer, zeigt auch die englische Oberfläche den
deutschen Text - besser als ein leeres Feld.

## Mitgelieferte Einträge

Merkmale, Versionskommando und Quelle mitgelieferter Einträge werden beim Start
auf den Auslieferungsstand zurückgesetzt - sonst bliebe ein einmal
ausgeliefertes falsches Merkmal für immer stehen. Erhalten bleiben der
Ein/Aus-Schalter und die beiden Aktionen; das sind Betriebsentscheidungen.
Wer dauerhaft andere Erkennungsregeln braucht, legt einen eigenen Eintrag an.

## Grenzen

- **Ohne systemd** (Alpine mit OpenRC) entfällt der generische Fund. Die
  Katalog-Erkennung über Pfade und Programme läuft weiter.
- **Im eingeschränkten Modus** läuft die Erkennung ohne root. Die Merkmale
  beantwortet das System auch so; ein Versionskommando, das root braucht,
  bleibt ohne Antwort. Der Weg über den LCM-Helper stünde offen, hieße aber,
  beliebige Kommandos durch dessen Whitelist zu lassen - genau das, was der
  eingeschränkte Modus verhindert.
- **Eine Anwendung, mehrere Installationen**: Erkannt wird derzeit der erste
  Treffer je Eintrag und Server.
