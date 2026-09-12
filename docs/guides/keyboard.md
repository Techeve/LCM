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

**`?`** öffnet jederzeit die Übersicht aller Kürzel - auch über die
Schaltfläche `?` oben rechts in der Leiste und den Link in der Fußzeile. Die
Übersicht zeigt zusätzlich die Kürzel der gerade geöffneten Seite.

Kürzel gelten, solange kein Eingabefeld den Fokus hat. Wer tippt, tippt
Buchstaben - nur Speichern (`Strg+S` bzw. `⌘S`) greift auch im Feld.

### Springen

Zwei Tasten nacheinander: erst `G`, dann der Buchstabe der Seite.

| Kürzel | Ziel |
|---|---|
| `G` `D` | Dashboard |
| `G` `G` | Gruppen |
| `G` `U` | Linux-Benutzer |
| `G` `S` | Sicherheit |
| `G` `C` | Docker |
| `G` `J` | Jobs |
| `G` `E` | Einstellungen |
| `G` `A` | Mein Konto |
| `G` `H` | Doku |

Seiten, für die das Konto keine Rechte hat, fehlen in der Übersicht und
reagieren nicht.

### Überall

| Kürzel | Wirkung |
|---|---|
| `?` | Kürzel-Übersicht öffnen oder schließen |
| `/` | In das Suchfeld der Seite springen |
| `Strg+S` / `⌘S` | Formular speichern |
| `Esc` | Dialog oder Menü schließen |
| `←` `→` | In der Hauptnavigation und in Tabellenzeilen zwischen Bedienelementen wandern |
| `↑` `↓` | In Menüs zwischen Einträgen, in Tabellen zwischen Zeilen wandern |
| `Pos1` / `Ende` | Erster / letzter Eintrag eines Menüs |
| `Tab` / `Umschalt+Tab` | Nächstes / voriges Bedienelement |

### Auf der Seite

Schaltflächen mit eigenem Kürzel tragen es als kleines Abzeichen direkt am
Namen - **„+ Server hinzufügen `N`"**. Das Abzeichen zeigt die Taste, die die
Schaltfläche auslöst; die Übersicht (`?`) listet alle Kürzel der Seite unter
*Auf dieser Seite*. Die Abzeichen lassen sich dort abschalten; die Wahl
merkt sich der Browser.

| Kürzel | Wirkung |
|---|---|
| `N` | Neu anlegen: Server, Gruppe, Linux-Benutzer, Profil, Regel, Kanal, Anwendung, Repository, Allowlist, Custom-Aktion |
| `U` | Sicherheit: alle VMs aktualisieren |
| `/` | Suchfeld: Dashboard (Name), Jobs, Pakete eines Servers, Regelbausteine |

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
(`frontend/e2e/a11y.spec.js`) und die Tastaturbedienung mit Playwright
(`frontend/e2e/hotkeys.spec.js`). Ein neuer Verstoß fällt im Testlauf auf,
nicht erst beim Anwender.
