---
sidebar:
  order: 18
title: Alarme & Benachrichtigungen
description: Regelbasierte Alarme (Festplatte, CVEs, Heartbeat …) mit Schwere, Cooldown und Versand per E-Mail oder Webhook (z.B. Microsoft Teams).
---

LCM wertet periodisch Monitoring-Kriterien aus und benachrichtigt bei
Überschreitung über konfigurierbare Kanäle. Die Auswertung läuft als
system-globaler Zeitplan - **fest alle 30 Minuten**, das Intervall ist wie die
übrigen Kernfunktionen nicht einstellbar.

## Benachrichtigungskanäle

Unter *Einstellungen → Benachrichtigungen* legst du Kanäle an. Ein Kanal wird
von Alarm-Regeln referenziert und ist gegen Löschen geschützt, solange ihn
eine Regel nutzt.

- **E-Mail (SMTP)** - eigener Postausgang je Kanal (Host, Port, Absender,
  Empfänger; Passwort verschlüsselt gespeichert).
- **Webhook** - HTTPS-POST an eine Ziel-URL, wahlweise als **generisches
  JSON** (für eigene Automationen) oder als **Microsoft-Teams-Format**
  (Adaptive Card). Für Teams legst du dort einen Workflow „Bei Empfang einer
  Webhookanforderung" an und trägst dessen URL in LCM ein. Die URL ist ein
  Geheimnis - sie wird verschlüsselt gespeichert und nie wieder angezeigt;
  HTTPS ist Pflicht (HTTP nur für localhost-Tests).
- **Standard-E-Mail (System)** - der verwaltete Kanal des
  [Standard-E-Mail-Versands](#standard-e-mail-versand-system-mailer):
  keine eigene Konfiguration, Empfänger sind die dort hinterlegten
  Admin-Adressen. Ein-/ausschalten unter *Einstellungen → Allgemein*.

### Webhook-Ziel im eigenen Netz

Ein öffentlicher Dienst wie Teams bringt sein Zertifikat mit - bei einem
eigenen Empfänger im internen Netz muss man es selbst stellen. Zwei Punkte
sind dabei nicht verhandelbar:

- **HTTPS ist Pflicht.** `http://` wird nur für `localhost`, `127.0.0.1` und
  `::1` akzeptiert; ein Ziel im LAN muss also `https://` sein.
- **Das Zertifikat wird geprüft.** LCM schaltet die Prüfung nicht ab. Ein
  selbstsigniertes Zertifikat, dem der LCM-Host nicht vertraut, führt zum
  Fehlschlag - auch wenn der Empfänger erreichbar ist.

Für einen eigenen Empfänger heißt das: ein Zertifikat ausstellen, das **die
IP-Adresse (oder den Namen) als SAN führt**, und die ausstellende CA auf dem
**LCM-Host** hinterlegen - nicht auf dem Empfänger.

```bash
# Auf dem LCM-Host: eigene CA als vertrauenswürdig eintragen
sudo cp meine-ca.crt /usr/local/share/ca-certificates/
sudo update-ca-certificates
sudo systemctl restart lcm
```

Der Neustart ist nötig, weil der Dienst den Zertifikatsspeicher beim Start
liest. Gegenprobe vor dem Anlegen des Kanals - schlägt sie fehl, scheitert
auch der Webhook:

```bash
curl https://<empfänger>:<port>/   # ohne -k: muss ohne Zertifikatsfehler durchlaufen
```

:::tip[Erst „Testen" drücken, dann Regeln bauen]
Jeder Kanal hat eine Schaltfläche **Testen**, die eine Beispiel-Nachricht
verschickt. Sie meldet den Fehler des Zielsystems im Klartext - deutlich
schneller, als auf einen echten Alarm zu warten und zu rätseln, warum nichts
ankommt.
:::

## Standard-E-Mail-Versand (System-Mailer)

Unabhängig von den Alarm-Kanälen gibt es unter *Einstellungen → Allgemein*
den **Standard-E-Mail-Versand**: den System-Postausgang für transaktionale
Mails - **Passwort-Reset-Links**, **Einladungslinks** für neue Benutzer und
Hinweise an die Admin-Empfänger. Eine Checkbox bietet ihn zusätzlich als
Benachrichtigungskanal an (siehe oben); der „Testnachricht senden"-Button
prüft die gespeicherte Konfiguration.

Damit Benutzer den Self-Service-Reset („Passwort vergessen?" auf der
Login-Seite) nutzen können, brauchen sie eine **E-Mail-Adresse** am Konto.
Der Reset-Link ist 1 Stunde gültig und einmal verwendbar; ob eine Adresse
existiert, verrät der Endpunkt nicht (kein User-Enumeration), und er ist pro
Client-IP gedrosselt.

## Alarm-Regeln

Unter *Einstellungen → Alarme*. Jede Regel bindet ein Kriterium an einen Kanal
und optional an **Servergruppen** (ohne Auswahl gilt sie für alle Server) - so
lassen sich Schwellen je Infrastruktur unterschiedlich streng setzen. Es lassen
sich mehrere Gruppen zugleich wählen; ein Server in mehreren gewählten Gruppen
wird trotzdem nur einmal ausgewertet.

| Typ | Auslöser |
|---|---|
| **Festplatten-Kapazität** | Belegung erreicht den prozentualen Schwellenwert |
| **Speicher-Prognose** | die lineare Hochrechnung überschreitet das Limit innerhalb der Frist |
| **Security/CVE** | CVE-Funde ab der konfigurierten Mindest-Schwere |
| **Überfällige Updates** | Zahl offener Paket-Updates übersteigt den Schwellenwert |
| **Heartbeat** | letzter Server-Kontakt liegt länger als *n* Stunden zurück |
| **Neustart erforderlich** | das System fordert nach einem Update selbst einen Neustart an (z.&nbsp;B. neuer Kernel) - reines Ja/Nein-Kriterium ohne Schwellenwert |
| **APT-Cache nicht erreichbar** | der zentrale [apt-cacher-ng](/guides/apt-cache/) antwortet nicht. Greift nur auf dem LCM-Host, auf dem der Dienst läuft, und bleibt stumm, solange unter *Einstellungen → APT-Cache* keine URL hinterlegt ist |
| **Deep Scan** | der letzte [Deep Scan](/guides/deep-scan/) hat Warnungen oder kritische Befunde ergeben (Härtung/Fehlkonfiguration oder eine Kernel-Reboot-Lücke) - reines Ja/Nein-Kriterium ohne Schwellenwert |
| **CrowdSec-LAPI nicht erreichbar** | die zentrale CrowdSec-LAPI antwortet nicht oder lehnt den hinterlegten Maschinen-Login ab. Greift nur auf dem LCM-Host und bleibt stumm, solange unter *Einstellungen → CrowdSec* keine LAPI konfiguriert ist |
| **CVE-Datenbank veraltet** | die Schwachstellen-Datenbank des CVE-Scanners ist älter als 48&nbsp;Stunden oder wurde nie geladen. Wichtig, weil eine alte Datenbank keinen Fehler meldet, sondern veraltete Ergebnisse liefert - nach außen sieht das wie „keine Sicherheitslücken" aus. Greift nur auf dem LCM-Host |
| **System-Backup überfällig** | automatische Backups sind aktiviert, aber das jüngste Backup ist älter als das **Doppelte des Intervalls** - oder es existiert noch gar keines. Misst bewusst das Ergebnis statt einzelner Fehlversuche: egal, auf welchem Weg das Backup ausblieb, gemeldet wird der fehlende Stand. Greift nur auf dem LCM-Host |

## Beispiele je Alarmtyp

Alle Beispiele werden unter *Einstellungen → Alarme* als Regel angelegt. Wo
kein Schwellenwert angegeben ist, greift der eingebaute Standard.

- **Festplatten-Kapazität** - „Root-Partition wird eng": Schwellenwert
  **85&nbsp;%**, Gruppe *Datenbanken*, Schwere *warning*. Ohne Schwellenwert
  gilt der Default **90&nbsp;%**.
- **Speicher-Prognose** - „Volläuft in Kürze": Frist **7 Tage**, alle Server,
  Schwere *warning*. Die lineare Hochrechnung aus dem
  [Speicher-Verlauf](/guides/monitoring/) meldet, bevor die Platte tatsächlich
  voll ist (Default-Frist: 10 Tage).
- **Security/CVE** - „Kritische Lücken sofort": Mindest-Schwere **critical**,
  alle Server, Schwere *critical*. Eine zweite, lockerere Regel mit
  Mindest-Schwere **high** nur für die Gruppe *Produktion* (Default: `high`).
- **Überfällige Updates** - „Patch-Rückstand": erlaubte Zahl offener Updates
  **0** (jedes ausstehende Update meldet), Gruppe *Produktion*. Für Staging
  großzügiger, z.&nbsp;B. **20**.
- **Heartbeat** - „Server meldet sich nicht": Timeout **6 Stunden**, alle
  Server, Schwere *critical* (Default: 24 Stunden).
- **Neustart erforderlich** - „Kernel wartet auf Reboot": kein Schwellenwert,
  Gruppe *Produktion*, Schwere *warning*. Löst aus, sobald der Server nach
  einem Update selbst einen Neustart anfordert.
- **APT-Cache nicht erreichbar** - kein Schwellenwert; nur sinnvoll, wenn der
  LCM-Host apt-cacher-ng betreibt und unter *Einstellungen → APT-Cache* eine
  URL hinterlegt ist.
- **Deep Scan** - „Härtungsbefunde melden": kein Schwellenwert, Schwere
  *warning*. Löst aus, sobald ein Deep Scan Warnungen oder kritische Befunde
  liefert.

## Schwere & Cooldown

Jede Regel hat eine **Schwere** und einen **Cooldown**: Der jüngste Alarm je
(Regel, Server) entprellt weitere Benachrichtigungen, damit kein Alarm-Spam
entsteht. **`0` bedeutet dabei nicht „keine Sperre"**, sondern den eingebauten
Standard von **360&nbsp;Minuten (6&nbsp;h)** - eine kürzere Sperre muss
ausdrücklich eingetragen werden (Minimum&nbsp;1). Ausgelöste Alarme landen in der **Alarm-Historie**
(inkl. Versand-Status); sie wird nach der Log-Aufbewahrungsfrist bereinigt.

## Zusammenspiel

Die Alarm-Auswertung nutzt dieselben Daten wie das Monitoring - u.&nbsp;a. die
CVE-Funde aus dem [CVE-Scan](/guides/security-cve/) und den
[Speicher-Verlauf](/guides/monitoring/). Es ist also keine doppelte Erfassung
nötig; Alarme sind die Benachrichtigungsschicht über dem vorhandenen Bestand.
