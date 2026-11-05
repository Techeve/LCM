---
sidebar:
  order: 30
title: Tastatur & Barrierefreiheit
description: Tastenkürzel, Bedienung ohne Maus, Screenreader und Kontraste in der LCM-Oberfläche.
---

Die Oberfläche lässt sich vollständig mit der Tastatur bedienen, und sie ist
für Screenreader aufgebaut: Landmarken, beschriftete Bedienelemente,
Überschriften in Reihenfolge, Live-Ansagen bei Seitenwechseln und Meldungen,
Kontraste nach WCAG 2.1 AA in beiden Farbmodi.

## Tastenkürzel

**`?`** öffnet jederzeit die Übersicht aller Kürzel - auch über das Kontomenü
oben rechts (Eintrag *Tastenkürzel*). Die Übersicht zeigt zusätzlich die Kürzel
der gerade geöffneten Seite.

Kürzel gelten in der angemeldeten Oberfläche, solange kein Eingabefeld den
Fokus hat. Wer tippt, tippt Buchstaben - nur Speichern (`Strg+S` bzw. `⌘S`)
greift auch im Feld. Auf dem Anmeldebildschirm sind die Kürzel aus: Dort gibt
es nichts anzuspringen, und das Kontomenü erscheint erst nach der Anmeldung.

### Springen

Zwei Tasten nacheinander: erst `G`, dann der Buchstabe der Seite.

| Kürzel | Ziel |
|---|---|
| `G` `D` | Dashboard |
| `G` `V` | Server |
| `G` `G` | Gruppen |
| `G` `U` | Linux-Benutzer |
| `G` `S` | Sicherheit |
| `G` `C` | Container |
| `G` `J` | Jobs |
| `G` `E` | Einstellungen |
| `G` `A` | Mein Konto |
| `G` `H` | Doku |

Seiten, für die das Konto keine Rechte hat, fehlen in der Übersicht und
reagieren nicht.

### Überall

| Kürzel | Wirkung |
|---|---|
| `Strg+K` / `⌘K` | Befehlspalette öffnen |
| `?` | Kürzel-Übersicht öffnen oder schließen |
| `/` | In das Suchfeld der Seite springen |
| `Strg+S` / `⌘S` | Formular speichern |
| `Esc` | Dialog oder Menü schließen |
| `←` `→` | In der Hauptnavigation und in Tabellenzeilen zwischen Bedienelementen wandern |
| `↑` `↓` | In Menüs zwischen Einträgen, in Tabellen zwischen Zeilen wandern |
| `Pos1` / `Ende` | Erster / letzter Eintrag eines Menüs |
| `Tab` / `Umschalt+Tab` | Nächstes / voriges Bedienelement |

### Auf der Seite

Befehle mit eigenem Kürzel (etwa **„Server hinzufügen“**, `N`) löst die Taste
direkt aus; die Übersicht (`?`) listet alle Kürzel der Seite unter
*Auf dieser Seite*. Dieselben Befehle stehen in der Befehlspalette.

| Kürzel | Wirkung |
|---|---|
| `N` | Neu anlegen: Server, Gruppe, Linux-Benutzer, Profil, Kanal, Alarmregel, App, Repository, APT-Cache, CrowdSec-LAPI, Allowlist, Eigene Aktion |
| `U` | Sicherheit: Sammel-Update (alle Server aktualisieren) |
| `/` | Suchfeld der Seite, sofern sie eines hat: Serverliste, Jobs, Pakete eines Servers |

## Ohne Maus durch die Seite

- **Sprunglink.** Das erste `Tab` auf jeder Seite trifft „Zum Inhalt
  springen" - ein Druck auf `Enter` überspringt die Navigationsleiste.
- **Seitenwechsel.** Nach einem Wechsel steht der Fokus im Inhalt, der
  Fenstertitel nennt die Seite („Jobs · LCM"), und ein Screenreader liest den
  Seitennamen vor.
- **Dialoge.** Ein geöffneter Dialog nimmt den Fokus auf (erstes Feld), `Tab`
  bleibt darin, `Esc` schließt ihn, und der Fokus kehrt zur auslösenden
  Schaltfläche zurück.
- **Meldungen.** Erfolgsmeldungen werden höflich nachgereicht, Fehler
  unterbrechen - über zwei getrennte Live-Regionen.
- **Fokus sichtbar.** Der Tastaturfokus ist als kräftiger Ring zu sehen, auch
  auf der dunklen Topleiste. Mausklicks zeigen keinen Ring.

## Kontraste und Bewegung

Alle Texte erreichen 4,5:1 (WCAG AA) - im hellen wie im dunklen Modus, auch
auf den gelb und rot getönten Tabellenzeilen der Sicherheitsübersicht. Wer im
Betriebssystem *weniger Bewegung* eingestellt hat, bekommt keine
Einblend- und Schwebe-Animationen.

## Geprüft wird automatisch

Der Testlauf prüft die wichtigsten Seiten in beiden Farbmodi mit
[axe-core](https://github.com/dequelabs/axe-core) gegen WCAG 2.1 AA
(`ui/e2e/a11y.spec.ts`) und die Tastaturbedienung mit Playwright
(`ui/e2e/hotkeys.spec.ts`). Ein neuer Verstoß fällt im Testlauf auf,
nicht erst beim Anwender.
