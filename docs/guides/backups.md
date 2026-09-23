---
sidebar:
  order: 17
title: Backups
description: Verschlüsselte, portable .lcmbak-Archive erstellen, herunterladen und wiederherstellen.
---

LCM sichert seinen **eigenen** Zustand in ein verschlüsseltes, portables
Archiv. Ein Backup enthält alles, was eine Instanz ausmacht - es lässt sich auf
einer **frischen** Instanz vollständig wiederherstellen.

## Was im Archiv steckt

Ein `.lcmbak` ist ein verschlüsseltes ZIP. Es bündelt:

- die **Datenbank-Momentaufnahme** (konsistenter SQLite-Snapshot),
- den **Master-Key** (`lcm.key`) - ohne ihn wären die at-rest verschlüsselten
  Felder unlesbar,
- die **Konfiguration** (`config.json`),
- das **TLS-Zertifikat**.

Verschlüsselt wird auf eine von zwei Arten - die Spalte *Verschlüsselung* in
der Backup-Liste zeigt, welche:

| Art | Wie | Zum Wiederherstellen nötig |
|---|---|---|
| **Empfänger-Schlüssel** (empfohlen) | [age](https://age-encryption.org), X25519: das Archiv wird an einen oder mehrere **öffentliche** Schlüssel verschlüsselt. Auf dem Server liegt kein Geheimnis. | der **private** Schlüssel eines Empfängers |
| **Passphrase** | scrypt-Ableitung + AES-256-GCM. | die Passphrase |

:::caution[Ohne Schlüssel kein Restore]
Weder den privaten Empfänger-Schlüssel noch die Passphrase kann LCM
wiederbeschaffen. Ohne sie ist ein Backup nicht wiederherstellbar - sicher
aufbewahren, an einem Ort, der das Backup überlebt (Passwortmanager, Tresor,
zwei Personen).
:::

![Backup-Einstellungen: automatisches Backup, erstellte Archive, Wiederherstellen](./img/backups-settings.png)

## Backup jetzt erstellen

Unter *Einstellungen → Backups* legt **„Jetzt sichern"** sofort ein Archiv an.
Bleibt das Passphrase-Feld daneben leer, gilt dieselbe Reihenfolge wie beim
geplanten Backup (Empfänger-Schlüssel, sonst hinterlegte Passphrase); eine
eingetippte Passphrase gewinnt und verschlüsselt genau dieses Archiv damit.
LCM zieht dafür eine **konsistente** Momentaufnahme der Datenbank
(`VACUUM INTO`) - ein Backup lässt sich also im laufenden Betrieb erstellen.

## Empfänger-Schlüssel (empfohlen)

Mit Empfänger-Schlüsseln braucht das geplante Backup **kein Geheimnis auf dem
Server**: Zum Verschlüsseln genügt der öffentliche Schlüssel, der private liegt
bei dir. Wer den Host übernimmt, hat damit die Archive - aber nicht den
Schlüssel dazu.

1. Unter *Einstellungen → Backups* auf **„Schlüsselpaar erzeugen"** klicken.
   LCM zeigt den privaten Schlüssel (`AGE-SECRET-KEY-1…`) **genau einmal** und
   speichert ihn nirgends - jetzt in den Passwortmanager übernehmen.
2. Der öffentliche Schlüssel (`age1…`) steht danach in der Empfänger-Liste;
   mit **Speichern** wird er wirksam. Mehrere Empfänger sind erlaubt, einer je
   Zeile - jeder davon kann später wiederherstellen.
3. Das Badge wechselt auf **„Empfänger-Schlüssel gesetzt"**; die Passphrase
   wird ab jetzt nicht mehr benutzt (sie bleibt gespeichert und greift wieder,
   sobald die Liste leer ist).

Ein Schlüsselpaar lässt sich genauso mit dem Werkzeug `age-keygen` erzeugen -
das Format ist dasselbe; dann nur den öffentlichen Teil eintragen. Ein Archiv
lässt sich mit dem privaten Schlüssel auch ohne LCM öffnen:

```bash
age -d -i schluessel.txt lcm-backup-20260910-033000.lcmbak > backup.zip
```

## Automatische Backups

Damit automatische Backups laufen, braucht es **genau zwei Dinge** - beides
zeigt die Seite *Einstellungen → Backups* direkt an:

1. **„Automatische Backups aktiv"** ist eingeschaltet (Standard: an, alle
   24 Stunden).
2. **Ein Schlüssel ist hinterlegt**: Empfänger-Schlüssel (siehe oben) oder
   eine Passphrase - im Feld „Passphrase für geplante Backups"
   (verschlüsselt gespeichert), als systemd-Credential `backup_passphrase`
   oder als Umgebungsvariable `LCM_BACKUP_PASSPHRASE`. Ein unbeaufsichtigtes
   Backup kann nach nichts fragen; ohne Schlüssel lässt sich der Zeitplan gar
   nicht erst aktivieren, und ein beim Start vorgefundener aktiver Zeitplan
   ohne Schlüssel wird abgeschaltet (Warnung im Protokoll).

Was hinterlegt ist, zeigt die Backups-Seite als Badge (**„Empfänger-Schlüssel
gesetzt"** / **„Passphrase gesetzt"** / **„Schlüssel fehlt"**); fehlt beides
bei aktivierten automatischen Backups, erscheint zusätzlich eine deutliche
Warnung mit Anleitung. Die Passphrase per Umgebung setzt du so - als
systemd-Drop-in (`/etc/systemd/system/lcm.service.d/backup.conf`):

```ini
[Service]
Environment=LCM_BACKUP_PASSPHRASE=ein-langes-geheimnis
```

danach:

```bash
systemctl daemon-reload && systemctl restart lcm
```

Oder im Docker-Compose:

```yaml
services:
  lcm:
    environment:
      LCM_BACKUP_PASSPHRASE: ein-langes-geheimnis
```

:::caution
Ohne Empfänger-Schlüssel und ohne Passphrase schlägt ein geplantes Backup mit
einem Schlüssel-Fehler fehl - es entsteht **nie** ein unverschlüsseltes
Archiv. Manuelle Backups funktionieren weiterhin (Passphrase im Formular).
:::

Weitere Einstellungen:

- **Intervall** (Stunden) und **Aufbewahrung** (Anzahl) - ältere Backups
  werden automatisch bereinigt.
- **Uhrzeit** - verankert den Zeitplan an einer festen Uhrzeit (Serverzeit).
  Teilt das Intervall den Tag (1, 2, 3, 4, 6, 8, 12 oder 24&nbsp;Stunden),
  läuft das Backup zu festen, daraus abgeleiteten Zeiten - z.&nbsp;B.
  Intervall 12&nbsp;h und Uhrzeit 03:30 → Läufe um 03:30 und 15:30. Andere
  Intervalle laufen relativ; dort sichert der Nachhol-Watchdog den Takt ab.
- **Zielverzeichnis** - ist immer gesetzt und mit dem Standard vorbelegt
  (`backup_dir` aus config.json, sonst `<Datenverzeichnis>/backups`). Es lässt
  sich frei ändern - praktisch für ein persistentes/externes Volume; ein
  geleertes Feld setzt beim Speichern den Standard wieder ein.

### Überfällige Backups werden nachgeholt

Bei Intervallen, die sich nicht als feste Uhrzeit ausdrücken lassen, zählt
der Zeitplan ab dem Start der Instanz. Damit eine Instanz, die
**häufiger neu startet als das Intervall lang ist** (z.&nbsp;B. durch
regelmäßige Updates), trotzdem gesichert wird, prüft ein Watchdog alle paar
Minuten: Ist das jüngste Backup älter als das Intervall (oder existiert noch
keines), wird **sofort ein Backup nachgeholt** - kurz nach dem Start, ohne
Zutun. Auch ein frisches manuelles Backup zählt dabei als Abdeckung des
Intervalls.

### Reste eines abgebrochenen Laufs

Ein Sicherungslauf legt zwei Zwischendateien an: eine Momentaufnahme der
Datenbank (`.snap-…​.db`) und das halbfertige Archiv (`….lcmbak.part`). Im
Regelfall räumt er sie selbst wieder weg. Wird der Dienst mitten im Kopieren
hart beendet, etwa durch einen Neustart, bleiben sie liegen - und die
Momentaufnahme ist, anders als das fertige Archiv, **nicht verschlüsselt**.

LCM räumt solche Reste deshalb selbst ab: beim Start sofort, danach nach jeder
Sicherung, dort nur, was älter als sechs Stunden ist (ein gerade laufender
Lauf soll seine eigene Datei behalten). Jede entfernte Datei steht mit Größe
und Datum im Protokoll:

```bash
journalctl -u lcm | grep 'leftover file of an interrupted backup'
```

## Wiederherstellen

Zwei Wege:

1. **Aus der Historie** - ein früheres Backup direkt zur Wiederherstellung
   auswählen.
2. **Aus hochgeladenem Archiv** - ein `.lcmbak` hochladen, auch auf einer
   **frischen** Instanz (Fresh-Instance-Restore).

In das Feld „Passphrase oder privater age-Schlüssel" kommt, womit das Archiv
verschlüsselt wurde - LCM erkennt am Präfix `AGE-SECRET-KEY-1`, dass es ein
Schlüssel ist. Eine Passphrase für ein Empfänger-verschlüsseltes Archiv wird
mit einer klaren Meldung abgewiesen.

Der Restore läuft über **Staging + Apply-on-Startup**: LCM legt die
wiederherzustellenden Dateien bereit und wendet sie beim nächsten Start an.
Ob LCM sich dafür selbst neu startet, steuert `RestoreAutoRestart` bzw. die
Umgebungsvariable `LCM_RESTORE_AUTO_RESTART` - sinnvoll nur unter einem
Prozess-Supervisor (systemd/Docker mit Restart-Policy). Ist Auto-Restart aus,
bleibt der Restore vorbereitet und der Betreiber startet manuell.

Die Umgebungsvariable hat **Vorrang** vor der UI-Einstellung (truthy sind
`1`, `true`, `yes`, `on`). Beim geordneten Neustart beendet sich LCM mit einem
Nicht-Null-Exit-Code, damit `Restart=on-failure` (systemd) bzw. eine
Docker-Restart-Policy greift:

```ini
[Service]
Environment=LCM_RESTORE_AUTO_RESTART=1
Restart=on-failure
```

:::tip
Nach dem Anstoßen einer Wiederherstellung meldet die UI dich automatisch ab -
die Sitzung der alten Instanz ist danach nicht mehr gültig.
:::
