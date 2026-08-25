---
sidebar:
  order: 12
title: Sicherheit-Tools (fail2ban / CrowdSec)
description: fail2ban oder CrowdSec per Knopfdruck installieren und einrichten.
---

LCM installiert und konfiguriert **fail2ban** oder **CrowdSec** per Knopfdruck auf
einem Server - beides sperrt wiederholt auffällige IPs (Brute-Force-Schutz).
fail2ban ist die schlanke, eigenständige Wahl (Log-Auswertung + IP-Bann auf
genau diesem Host); CrowdSec ist der verteilte Ansatz (lokaler Agent, optionaler
Firewall-Bouncer, geteilte Entscheidungen über eine Local API / die CrowdSec
Console).

## Aussperr-Schutz

Beide Werkzeuge könnten LCM per SSH-Bann aussperren. Deshalb setzt LCM die
**eigene Quell-IP** (die IP, mit der es den Server erreicht) **automatisch in die
Allowlist**, bevor der Schutz scharf wird. Diese IP wird beim Hardware-Scan aus
`$SSH_CONNECTION` auf dem Server gelesen und im Formular vorbelegt; weitere IPs
lassen sich ergänzen. Zusätzlich stehen **Loopback** (`127.0.0.1/8` und `::1`)
immer auf der Liste.

:::note[Warum die Quell-IP unprivilegiert gelesen wird]
LCM liest `$SSH_CONNECTION` bewusst **ohne** sudo - ein `sudo`-Wrapper würde die
Umgebungsvariable verwerfen, und die automatisch ermittelte IP wäre leer. Das
Ergebnis überschreibt die beim letzten Scan gespeicherte `LCMSourceIP`, damit ein
IP-Wechsel des LCM-Hosts (z. B. nach Umzug) den Aussperr-Schutz nicht aushebelt.
:::

## Installieren

Server-Detail → **Aktionen → „Sicherheit-Tools"**. Im Modal das Tool wählen:

- **fail2ban** - nur die Allowlist-IPs abfragen. LCM installiert fail2ban, schreibt
  sein eigenes Drop-in `/etc/fail2ban/jail.d/99-lcm.local` (`backend = systemd`,
  `ignoreip` mit der Allowlist, sshd-Jail aktiv) und startet den Dienst. Eine
  vorhandene `jail.local` bleibt dabei **unangetastet** - dort gepflegte eigene
  Jails und Verschärfungen (`bantime`, `maxretry`) gelten weiter.
- **CrowdSec** - zusätzlich wählbar:
  - **Firewall-Bouncer** mitinstallieren (setzt Sperren durch; nftables bevorzugt),
  - **Collections** (Default `crowdsecurity/sshd`),
  - **LAPI-Anbindung**: *Lokal* (eigenständig), *Zentrale LAPI* oder *CrowdSec
    Console* - die beiden zentralen Optionen nutzen die unter *Einstellungen →
    CrowdSec* hinterlegten Zugangsdaten.
  - **Allowlist-IPs** (LCM-IP vorbelegt).

Die Aktion braucht **vollen Sudo-Zugriff**; auf Servern im eingeschränkten
Sudo-Modus ist sie gesperrt. Sie läuft als **asynchroner, protokollierter Job**;
danach liest LCM den Ist-Zustand (installiert + aktiv) frisch nach und speichert
ihn.

### Was fail2ban schreibt

Das erzeugte Drop-in `/etc/fail2ban/jail.d/99-lcm.local` ist bewusst minimal und
distributionsübergreifend robust (`backend = systemd`):

```ini
[DEFAULT]
backend = systemd
ignoreip = 127.0.0.1/8 ::1 203.0.113.10 198.51.100.0/24

[sshd]
enabled = true
```

Die `ignoreip`-Zeile ist die Vereinigung aus Loopback, LCM-Quell-IP, den
manuell ergänzten IPs und den aufgelösten benannten Allowlists. `ignoreip`
verträgt IP **und** CIDR - beides ist erlaubt.

fail2ban liest in der Reihenfolge `jail.conf` → `jail.d/*.conf` → `jail.local`
→ `jail.d/*.local`; das LCM-Drop-in kommt zuletzt und hat damit für `ignoreip`
und `[sshd]` Vorrang. Alles Übrige aus einer eigenen `jail.local` - weitere
Jails, `bantime`, `maxretry` - bleibt wirksam. Beim Entfernen räumt LCM nur
seine eigene Datei weg; `/etc/fail2ban` bleibt bestehen, sobald darin fremde
Konfiguration liegt.

### Was CrowdSec einrichtet

CrowdSec wird über das offizielle packagecloud.io-Repo installiert (bzw. das
Alpine-community-Repo).

:::caution[Nicht jede Distributionsfassung ist dort vertreten]
Das Repo-Skript trägt die laufende Distributionsfassung ein und meldet Erfolg,
auch wenn es dafür gar keine Pakete gibt - auf **Debian 13 (trixie)** ist das
der Fall, die Suite antwortet mit 404. Installiert wird dann die Fassung der
Distribution, die mehrere Jahre alt sein kann. LCM weist deshalb im Job die
**installierte Version** aus und warnt, wenn das Paket nicht aus dem
Herstellerrepo stammt. Wer eine aktuelle Fassung braucht, richtet das Repo von
Hand nach der CrowdSec-Anleitung ein.
:::

Danach richtet LCM je nach Auswahl ein:

- die gewählten **Collections** (`cscli collections install crowdsecurity/sshd …`),
- den optionalen **Firewall-Bouncer** - automatisch
  `crowdsec-firewall-bouncer-nftables`, wenn `nft` vorhanden ist, sonst
  `crowdsec-firewall-bouncer-iptables`,
- die **Allowlist als Parser-Whitelist** unter
  `/etc/crowdsec/parsers/s02-enrich/lcm-whitelist.yaml` (versionsrobust über alle
  CrowdSec-Stände):

  ```yaml
  name: lcm/whitelist
  description: "LCM management allowlist"
  whitelist:
    reason: "LCM management"
    ip:
      - 127.0.0.1/8
      - ::1
      - 203.0.113.10
      - 198.51.100.0/24
  ```

Geheimnisse (LAPI-Passwort, Console-Key) werden **base64-kodiert übertragen** und
erst auf dem Ziel dekodiert; sie landen in `/etc/crowdsec/local_api_credentials.yaml`
mit `chmod 600`.

## Die drei LAPI-Modi

Die **Local API (LAPI)** ist der Entscheidungsdienst von CrowdSec: Der Agent
meldet Angriffssignale, die LAPI vergibt Sperrentscheidungen, der Bouncer setzt
sie um. LCM bietet drei Anbindungen:

| Modus | Wofür | Voraussetzung |
| --- | --- | --- |
| **Lokal** (`local`) | Jeder Server betreibt seine eigene LAPI (Standalone). Ideal für einzelne, isolierte Hosts. | keine |
| **Zentrale LAPI** (`remote`) | Alle Server melden an **eine** gemeinsame LAPI - geteilte Sperrlisten flottenweit. | URL + Maschinen-Login + Passwort unter *Einstellungen → CrowdSec*; die Maschine muss dort registriert sein (`cscli machines add`) |
| **CrowdSec Console** (`console`) | Zusätzlich an die Cloud-Console von CrowdSec anschließen (`cscli console enroll`). | Enrollment-Key unter *Einstellungen → CrowdSec* |

Wählst du *remote* oder *console* ohne hinterlegte Zugangsdaten, bricht LCM die
Aktion **vor** dem Job-Start mit einer klaren Meldung ab - es wird kein halb
konfigurierter Server hinterlassen.

## Zentrale Zugangsdaten pflegen

*Einstellungen → CrowdSec*:

- **Self-hosted LAPI** - URL + Maschinen-Login + Passwort (verschlüsselt gespeichert).
- **CrowdSec Console** - Enrollment-Key (verschlüsselt gespeichert).

Nur hinterlegte Optionen sind im Installationsformular auswählbar.

## CrowdSec-LAPI auf dem LCM-Host

Statt einen externen LAPI-Server zu betreiben, kann LCM die **CrowdSec Local
API direkt auf dem LCM-Host** einrichten und die Zugangsdaten **selbsttätig**
verdrahten. Schritt für Schritt:

1. Server-Detail des **LCM-Hosts** (localhost) öffnen → Karte
   **„LCM-Host-Einrichtung"** → **CrowdSec LAPI** → *Einrichten* (optional den
   lokalen Firewall-Bouncer mitinstallieren).
2. LCM installiert CrowdSec, öffnet die LAPI auf `0.0.0.0:8080`
   (`/etc/crowdsec/config.yaml.local`), erzeugt ein zufälliges Passwort und legt
   das Maschinen-Konto **`lcm-managed`** an (idempotent - ein bestehendes Konto
   wird ersetzt).
3. LCM liest das erzeugte Passwort aus dem Job-Output zurück und **trägt
   URL/Login/Passwort automatisch in die CrowdSec-Einstellungen ein**. Die URL
   zeigt auf die erste Nicht-Loopback-IP des Hosts, z. B.
   `http://203.0.113.5:8080`, Login `lcm-managed`.
4. Ab jetzt können verwaltete Server ohne weitere Eingaben im **Remote-Modus**
   enrollen: einfach *Aktionen → Sicherheit-Tools → CrowdSec → Zentrale LAPI*.

Das Klartext-Passwort wird im Job-Protokoll durch `LCM-LAPI-PW: ********`
ersetzt - es steht danach nur noch verschlüsselt in den Einstellungen. (Nur auf
dem LCM-Host, nur auf Debian/Ubuntu/apt.)

:::caution[LAPI-Port erreichbar machen]
Im Remote-Modus verbinden sich die Server auf **Port 8080** des LCM-Hosts. Gib
den Port in der [Firewall](/guides/firewall) für die Quell-IPs der verwalteten
Server frei - am besten über eine benannte [IP-Allowlist](/guides/allowlists).
Der Firewall-Bouncer wiederum braucht `nftables` oder `iptables` auf dem
jeweiligen Host. Ein Port-Konflikt mit LCM besteht nicht: LCM selbst lauscht
standardmäßig auf **9310** (UI/API), die LAPI behält ihren CrowdSec-Standard
**8080**.
:::

## LAPI überwachen & angebundene Server

Die Seite **Einstellungen → CrowdSec** bietet rund um die zentrale LAPI:

- **Jetzt prüfen** - eine Login-Probe vom LCM-Host aus (POST
  `/v1/watchers/login` mit den hinterlegten Maschinen-Zugangsdaten). Sie
  unterscheidet drei Zustände: *erreichbar + Login OK*, *erreichbar, aber Login
  abgelehnt* (Zugangsdaten veraltet) und *nicht erreichbar*.
- **Überwachung** - LCM empfiehlt eine Alarm-Regel vom Typ **„CrowdSec-LAPI
  nicht erreichbar"**; sie lässt sich hier per Klick anlegen. Mit aktiver Regel
  prüft LCM die LAPI **automatisch alle 30 Minuten** (mit der
  Alarm-Auswertung) und meldet Ausfälle über den zugewiesenen
  [Benachrichtigungs-Kanal](/guides/alerts). Ohne Kanal wird nur die
  Alarm-Historie geführt.
- **Angebundene Server** - alle Server, deren CrowdSec-Agent laut seiner
  Credentials-Datei (`/etc/crowdsec/local_api_credentials.yaml`, beim Scan
  live gelesen) an die hier konfigurierte LAPI meldet - samt Anbindungs-Modus
  und Dienst-Status.

## Verwalten (Dienst, Allowlist, Sperren)

Ist ein Werkzeug installiert, erscheint im Tab **Sicherheit** der Server-Detailseite
je Werkzeug eine Verwaltungs-Karte. Sie deckt genau das ab, wofür man sich sonst
per SSH auf die Maschine verbinden müsste - und die Sperrliste braucht man
typischerweise dann, wenn man die Maschine gerade **nicht** mehr erreicht.

| Bereich | Wirkung |
| --- | --- |
| **Dienst** | Starten, Stoppen, Neu starten sowie Autostart an/aus. Deckt systemd, SysV und OpenRC ab. |
| **Allowlist** | Schreibt `ignoreip` (fail2ban) bzw. die Parser-Whitelist (CrowdSec) neu und lädt sie nach - ohne Neuinstallation. Freie IPs und die zentral gepflegten [IP-Allowlists](/guides/allowlists) per Mehrfachauswahl. |
| **Sperrliste** | Zeigt die aktuell gesperrten Adressen (fail2ban: Jail, CrowdSec: Szenario, Grund und Restdauer) und hebt eine Sperre per Klick auf. |
| **Deinstallieren** | Entfernt Paket, Dienst und Konfiguration (bei CrowdSec auch den Bouncer). Nur nach ausdrücklicher Rückfrage. |

Jede Aktion läuft als **Job** auf dem Server. Die Knöpfe der Karte bleiben
gesperrt, solange der Job läuft; die Meldung „abgeschlossen" erscheint erst,
wenn der Job tatsächlich durch ist - ein fehlgeschlagener Job wird als Fehler
gemeldet. Danach werden Zustand und Sperrliste automatisch neu geladen.

:::note[Aussperr-Schutz gilt auch hier]
Beim Übernehmen der Allowlist ergänzt LCM immer die IP, mit der es den Server
gerade erreicht - auch wenn das Feld leer bleibt. Die Quell-IP wird dafür ohne
`sudo` gelesen (`$SSH_CONNECTION`), weil `sudo` diese Variable verwirft.
:::

:::warning[Nicht im eingeschränkten Modus]
Alle Verwaltungs-Aktionen greifen in Dienste und Systemkonfiguration ein und
sind deshalb im [eingeschränkten Sudo-Modus](/reference/security-model) gesperrt - die
Karte weist darauf hin, statt in einen Fehler zu laufen. Ist das Werkzeug auf
dem Server gar nicht (mehr) installiert, endet der Job mit einem Fehler statt
mit einer irreführenden Erfolgsmeldung.
:::

## Anzeige & Erkennung

Der **Hardware-Scan** erkennt ein bereits installiertes fail2ban/CrowdSec (über
`fail2ban-client` bzw. `cscli`/`crowdsec` im Pfad) und ob der Dienst **aktiv**
ist (systemd **und** OpenRC werden geprüft). Auf der Server-Detailseite steht
unter **System & Security** die Zeile **„Sicherheits-Tool"** mit dem erkannten
Werkzeug und dem Aktiv-Status.

## Allowlists auswählen

Im Installationsdialog (fail2ban/CrowdSec) lassen sich neben den Ad-hoc-IPs auch
die zentral gepflegten **[IP-Allowlists](/guides/allowlists)** per Mehrfachauswahl
zuordnen - ihre IPs kommen zur `ignoreip` (fail2ban) bzw. Parser-Whitelist
(CrowdSec) hinzu.

:::note[Allowlist-Auflösung sichtbar]
Lässt sich eine gewählte Allowlist beim Anwenden nicht auflösen, bricht LCM den
Schutz **nicht** ab - die LCM-Quell-IP und die manuell angegebenen IPs greifen
weiter -, hängt aber eine sichtbare **`LCM-WARNUNG`** an den Job-Output. So
vermutet niemand Schutz, den es nicht gibt.
:::

## Distributions-Abdeckung

| Paketverwaltung | fail2ban | CrowdSec |
| --- | --- | --- |
| apt (Debian/Ubuntu) | ✅ | ✅ (packagecloud.io-Repo) |
| dnf (RHEL/Rocky/Alma/Fedora) | ✅ | ✅ (packagecloud.io-Repo) |
| zypper (openSUSE/SLES) | ✅ | ✅ (packagecloud.io-Repo) |
| apk (Alpine) | ✅ | ✅ (community-Repo, kein Extra-Repo) |
| pacman (Arch) | ✅ | ❌ - LCM meldet das ehrlich (`ErrCrowdSecUnsupported`) |

## SSH-2FA (TOTP neben dem SSH-Key)

Unter **Server → Sicherheit** lässt sich zusätzlich **SSH-2FA** aktivieren:
SSH-Logins verlangen dann **Schlüssel + TOTP-Einmalcode**
(`google-authenticator-libpam`, funktioniert mit jeder Authenticator-App).

Was LCM dabei einrichtet:

- Das PAM-Modul wird installiert und in `/etc/pam.d/sshd` **ganz oben**
  eingehängt (`pam_google_authenticator.so nullok`); der Passwort-Stack im
  auth-Bereich wird stillgelegt - sonst käme nach dem Code auch noch eine
  Passwortabfrage. Alle Änderungen sind marker-basiert und beim Entfernen
  vollständig umkehrbar (Sicherung: `/etc/pam.d/sshd.lcm-backup`).
- Ein sshd-Drop-in (`55-lcm-2fa.conf`) setzt
  `AuthenticationMethods publickey,keyboard-interactive:pam` - bewusst
  alphabetisch **vor** dem Härtungs-Drop-in (OpenSSH nimmt je Option den
  zuerst gefundenen Wert). Reine Passwort-Logins per SSH sind damit vorbei.

**Sanfter Rollout (`nullok`):** Benutzer **ohne** eingerichtetes TOTP kommen
weiterhin mit ihrem Key herein - niemand wird beim Aktivieren ausgesperrt.
Das Enrollment macht jeder Benutzer selbst: auf dem Server
`google-authenticator` ausführen und den QR-Code mit der App scannen. Wer
schon so weit ist, zeigt die Spalte **2FA** im Reiter
**[Benutzer](/guides/linux-users#benutzer-übersicht-je-server)**.

Zwei eingebaute Aussperr-Sicherungen:

- Der **LCM-Service-User** bleibt per `Match`-Ausnahme bei reiner Key-Auth -
  LCMs SSH-Client beantwortet keine keyboard-interactive-Abfragen.
- Nach dem Aktivieren prüft LCM mit einer **frischen Verbindung**, dass der
  Zugang noch trägt; scheitert sie, wird alles automatisch **zurückgerollt**.

Distributions-Abdeckung: apt, dnf/yum (RHEL-Klone brauchen EPEL), zypper und
pacman. Auf **Alpine (apk)** ist die Option nicht verfügbar - dessen sshd ist
standardmäßig ohne PAM gebaut. Beim Entfernen bleiben die TOTP-Secrets der
Benutzer (`~/.google_authenticator`) liegen; beim erneuten Aktivieren gelten
sie sofort wieder.
