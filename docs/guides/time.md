---
sidebar:
  order: 14
title: Zeit & NTP
description: Zeitzone, Uhrenvergleich und Zeitserver der verwalteten Server.
---

Eine falsch gehende Uhr ist der unauffälligste Fehler im Betrieb: das System
läuft weiter und meldet nichts. Die Folgen treten woanders auf und sehen nach
etwas anderem aus.

:::caution[Was eine falsche Uhr kaputtmacht]
- **TLS** - Zertifikate erscheinen als „noch nicht gültig" oder „abgelaufen";
  HTTPS-Verbindungen scheitern mit irreführenden Meldungen.
- **Paketquellen** - signierte Metadaten haben eine Gültigkeitsspanne. Steht
  die Uhr zu früh, lehnt `apt` sie mit „not valid yet" ab.
- **Protokolle** - Ereignisse mehrerer Server lassen sich nicht mehr
  nebeneinanderlegen; eine Ursachenanalyse wird zum Ratespiel.
- **Einmalpasswörter (TOTP)** - Codes gelten in Zeitfenstern von 30 Sekunden.
- **Kerberos/AD** - Tickets brechen ab etwa 5 Minuten Abweichung.
:::

## Was LCM erfasst

Bei jedem Scan - und über **Zeit prüfen** auch auf Zuruf - liest LCM rein
lesend und ohne Sonderrechte:

| Wert | Herkunft |
|---|---|
| **Zeitzone** | `timedatectl`, sonst `/etc/timezone` bzw. der `/etc/localtime`-Symlink; ohne all das die Abkürzung aus `date +%Z` |
| **Zeitdienst** | chrony, systemd-timesyncd, ntpd oder busybox-ntpd - welcher läuft |
| **Synchronisiert?** | die Meldung des Dienstes selbst (`chronyc tracking`, `timedatectl`, `ntpq`) |
| **Zeitserver** | die in der Konfiguration eingetragenen Server |
| **Abweichung** | Vergleich der Server-Uhr mit der von LCM |

Die Abweichung enthält die SSH-Laufzeit und ist auf ein bis zwei Sekunden
genau. Für die Frage, um die es geht - *geht die Uhr überhaupt richtig?* -
reicht das; gemeldet wird deshalb erst ab **30 Sekunden**.

## Einstellen

Server-Detail → **Einstellungen** (Zahnrad) → Abschnitt *Zeit & Zeitabgleich*:

- **Zeitzone setzen** - LCM schreibt sie und **liest sie zurück**. Meldet das
  System danach etwas anderes, gilt sie als nicht gesetzt; eine geschriebene
  Datei allein ist kein Beleg.
- **Zeitserver einrichten** - trägt die Server ein, startet den Zeitdienst und
  **belegt die Synchronisierung**. Gelingt der Nachweis im Zeitfenster nicht,
  meldet LCM das ehrlich (HTTP 502); die Konfiguration bleibt dabei bestehen,
  denn sie ist ja nicht falsch, sie hat nur noch nicht gegriffen. Vorhandene
  Einträge in `chrony.conf`/`ntp.conf` werden zeilenweise ersetzt, der Rest der
  Datei bleibt unangetastet, und es entsteht eine `.lcm-bak`-Sicherung.

:::note[Container haben keine eigene Uhr]
In einem Container (LXC, Docker, …) **kommt die Uhr vom Virtualisierungs-Host**
und ist dort nicht einstellbar; `systemd-timesyncd` startet aus genau diesem
Grund gar nicht erst (`ConditionVirtualization=!container`). LCM lehnt die
Aktion deshalb mit einem klaren Hinweis ab, statt etwas Unmögliches
anzubieten. Die **Abweichung wird trotzdem gemeldet** - ein falsch gehender
Host reißt alle seine Container mit, und dann ist der Abgleich auf dem Host
einzurichten.
:::

## Vorgaben

Unter **Einstellungen → Zeit & NTP** lassen sich hinterlegen:

- **Vorgabe-Zeitserver** (je Zeile `Label = Host`) - sie erscheinen als
  Auswahl beim Einrichten. Leer = eingebaute Liste (NTP-Pool, Cloudflare,
  Google).
- **Standard-Zeitzone** - belegt das Formular vor.

Beides sind reine Vorbelegungen: Gesetzt wird die Zeit immer bewusst je
Server, nie automatisch im Hintergrund.

## In der Ampel

- Abweichung **≥ 30 Sekunden** → **Warnung** (in Containern mit dem Hinweis
  auf den Host).
- **Kein Zeitdienst** auf einem Nicht-Container → Hinweis. Die Uhr stimmt
  gerade, aber nichts hält sie - noch ist nichts kaputt.
- Dienst läuft, meldet aber **nicht synchronisiert** → Hinweis.
