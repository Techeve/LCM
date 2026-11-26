---
sidebar:
  order: 19
title: Unterstützte Systeme
description: Auf welchen Systemen LCM und der lcm-agent laufen, über welche Paketverwaltung sie kommen und was davon wann getestet wurde.
---

Diese Seite nennt, wo LCM selbst läuft, auf welchen Systemen der `lcm-agent`
unterstützt wird und was dabei **tatsächlich getestet** ist. „Getestet“ heißt:
auf einem echten System oder Container durchgespielt, mit Datum. Alles andere ist
„unterstützt“: Der Code ist dafür gebaut, ein Testlauf steht aber noch aus.

## LCM-Host

| System | Stand |
|---|---|
| Debian 12, 13 | getestet (Installation, Einrichtungsassistent, Trivy-Sandbox) |
| Ubuntu 22.04, 24.04, 26.04 | getestet |
| Docker (amd64, arm64) | unterstützt, Image auf Docker Hub |
| Windows, macOS | nicht unterstützt |

Einzelheiten zur Installation: [Installation](/getting-started/installation/).

## Architekturen

LCM und der `lcm-agent` gibt es für **x86-64 (amd64)** und **ARM (arm64)**. Beide
Architekturen werden gebaut und veröffentlicht: das `.deb`-Paket und das Docker-Image
von LCM sowie die Pakete des Agenten für apt, dnf, zypper, apk und pacman.

**Alle Tests auf dieser Seite liefen auf x86-64.** Auf ARM ist bisher nichts auf echter
Hardware getestet (geplant auf einem Raspberry Pi). Es sind aber keine Unterschiede zu
erwarten, denn derselbe Code läuft auf beiden Architekturen. Die Matrix gilt für ARM
sinngemäß, belegt ist es nicht.

Server, die LCM per SSH verwaltet, brauchen kein Programm von uns. Ihre Architektur
spielt keine Rolle. Für 32-bit-Systeme (etwa Raspberry Pi OS 32 bit) gibt es keine
Agent-Pakete, sie lassen sich nur per SSH verwalten.

## Matrix je Distribution

Drei Stufen: ✅ **getestet** (auf einem echten System mit der aktuellen Linie
durchgespielt), 🟡 **sollte gehen** (der Code unterstützt es, ein Test steht aus)
und ❌ **nicht unterstützt**. Gleicher Paketmanager heißt nicht „getestet“:
Debian und Ubuntu teilen sich apt und verhalten sich bei systemd, Bubblewrap und
Pfaden trotzdem verschieden.

### apt-Distributionen: Server

| Distribution | Host | SSH | SSHE | Agent | CVE |
|---|---|---|---|---|---|
| Ubuntu 22.04, 24.04, 26.04 | ✅ | ✅ | ✅ | ✅ | voll |
| Debian 12, 13 | ✅ | ✅ | ✅ | ✅ | voll |
| Proxmox VE, Proxmox Backup Server |  | 🟡 | 🟡 | 🟡 | teilweise¹ |
| TurnKey Linux | 🟡 | 🟡 | 🟡 | 🟡 | voll |
| Kali Linux | 🟡 | 🟡 | 🟡 | 🟡 | keiner |
| Univention Corporate Server (UCS) |  | 🟡 | 🟡 | 🟡 | keiner |
| Raspberry Pi OS (64 bit) | 🟡 | 🟡 | 🟡 | 🟡 | teilweise² |
| Raspberry Pi OS (32 bit) |  | 🟡 | 🟡 | ❌ | keiner |

### Distributionen ohne apt: nur als Client

LCM selbst läuft hier nur im Docker-Container.

| Distribution | Host | SSH | SSHE | Agent | CVE |
|---|---|---|---|---|---|
| Red Hat Enterprise Linux | ❌ | 🟡 | 🟡³ | 🟡 | nicht belegt |
| AlmaLinux 10 | ❌ | ✅ | ✅³ | ✅ | nicht belegt |
| AlmaLinux 9 | ❌ | 🟡 | 🟡³ | ✅ | nicht belegt |
| Rocky Linux 10 | ❌ | ✅ | ✅³ | ✅ | keiner⁴ |
| CentOS Stream 10 | ❌ | ✅ | ✅³ | ✅ | keiner |
| Fedora 44 | ❌ | ✅ | ✅³ | ✅ | keiner |
| Fedora 45 | ❌ | 🟡 | 🟡³ | ✅ | keiner |
| openSUSE Leap 16 | ❌ | ✅⁵ | ✅³ | ✅ | nicht belegt |
| openSUSE Tumbleweed | ❌ | 🟡 | 🟡³ | ✅ | nicht belegt |
| Arch Linux | ❌ | 🟡⁶ | 🟡³⁶ | ✅ | keiner |
| Alpine 3.23 | ❌ | ✅⁷ | ✅³ | ✅ | voll |
| Alpine 3.22, 3.24 | ❌ | 🟡 | 🟡³ | ✅ | voll |

### Desktop-Distributionen

| Distribution | SSH | SSHE | Agent | CVE |
|---|---|---|---|---|
| Ubuntu Desktop 24.04, 26.04 | 🟡 | 🟡 | 🟡 | voll |
| Debian 13 Desktop | 🟡 | 🟡 | 🟡 | voll |
| Linux Mint | 🟡 | 🟡 | 🟡 | keiner |
| Zorin OS | 🟡 | 🟡 | 🟡 | keiner |

Die Spalten:

- **Host:** LCM ist auf dem System installiert (`.deb`-Paket oder Docker). Bei
  Proxmox, UCS und den Desktop-Distributionen steht dort nichts: LCM selbst läuft
  auf solchen Systemen nicht sinnvoll, sie werden nur verwaltet.
- **SSH:** Server wird per SSH ohne Agent verwaltet, mit vollen Rechten.
- **SSHE:** SSH-Verbindung mit eingeschränktem Modus. LCM arbeitet dann mit
  eingeschränkten Rechten auf dem Server. Den Agenten gibt es nur ohne diesen
  Modus, weil er als Root-Dienst läuft.
- **Agent:** Server läuft mit `lcm-agent`.
- **CVE:** Auf dieser Distribution kann LCM CVE-Scans ausführen. Die Angabe sagt,
  wie weit sie reichen: **voll** = alle Pakete der Distribution, **teilweise** =
  die Pakete der Basis-Distribution, aber nicht die Eigenpakete, **keiner** = für
  die Distribution gibt es keinen CVE-Bestand, ein Scan sagt nichts aus,
  **nicht belegt** = Trivy unterstützt die Distribution laut seiner Dokumentation,
  unsere Tests fanden dort aber keine Funde. Ob der Scan greift, ist offen.

Bei **keiner** meldet LCM keinen Fehler: Der Scan endet ohne Funde und sieht aus
wie ein sauberer Server. Nur CentOS Stream meldet einen Fehler.

¹ Die Debian-Pakete sind abgedeckt, die Proxmox-eigenen Pakete (`pve-*`,
`proxmox-*`) nicht.

² Die Debian-Pakete sind abgedeckt, die Pakete aus `archive.raspberrypi.com` nicht.

³ Updates, Firewall und Paket-Fixierung gehen auch eingeschränkt. Paketquellen
verwaltet LCM im eingeschränkten Modus nur auf apt-Systemen.

⁴ Trivy unterstützt Rocky Linux, LCM liefert dort in dieser Version aber keine
Funde: Der Paketkennung fehlt die Architekturangabe.

⁵ Mangels veralteter Pakete ist die Wirkung des Updates nur halb geprüft.

⁶ Im Container ohne Landlock scheitern Aufnahme und Paketjobs, siehe
[Hinweise je System](#hinweise-je-system).

⁷ Die Paket-Fixierung wirkt auf Alpine nicht, siehe
[Hinweise je System](#hinweise-je-system).

Die 🟡-Systeme testen wir nach und ziehen diese Tabelle nach.

## lcm-agent

Der Agent ist ein statisch gebautes Programm für `amd64` (x86_64) und `arm64`
(aarch64). Er kommt als Paket aus repo.techeve.de, eingerichtet mit einem Befehl
für alle Systeme:

```sh
curl -fsSL https://repo.techeve.de/setup.sh | sudo sh
```

| Paketverwaltung | Systeme | Pakete ab | Update per Knopf ab | Getestet |
|---|---|---|---|---|
| apt | Debian 12, 13; Ubuntu 22.04, 24.04, 26.04 | 2.0 | 2.4 | 24./25.11.2026 |
| dnf | AlmaLinux 9, 10; Rocky Linux 10; CentOS Stream 10; Fedora 44, 45 | 2.3.2 | 2.4 | 25.11.2026 |
| zypper | openSUSE Leap 16; openSUSE Tumbleweed | 2.3.2 | 2.4 | 25.11.2026 |
| apk | Alpine 3.22, 3.23, 3.24 (OpenRC) | 2.3.2 | 2.4 | 25.11.2026 |
| pacman | Arch Linux | 2.3.2 | 2.4 | 25.11.2026 |

Ein normales Systemupdate (`apt upgrade`, `dnf upgrade`, `zypper update`,
`apk upgrade`, `pacman -Syu`) lässt den Agenten auf seiner Version stehen.
Aktualisiert wird er über LCM, per Knopf in der Server-Ansicht oder über eine
Regel in der Gruppe. Bis LCM 2.3 aktualisierst du ihn auf dnf-, zypper-, apk-
und pacman-Systemen von Hand.

Nicht unterstützt für das Update per Knopf: Systeme mit `yum` ohne `dnf`
(CentOS 7, Amazon Linux 2). Der Agent läuft dort, wird aber von Hand
aktualisiert.

### Was getestet wurde

**apt, 24. und 25.11.2026**, auf Ubuntu 22.04, 24.04, 26.04 und Debian 12, 13
(Container auf dem Proxmox-Testbett):

- installieren, einrichten, Dienst aktiv, richtige Version, online in LCM
- ein Auftrag über den Agenten, Wiederverbinden nach einem Neustart des Agenten
- `lcm-agent status` und das Laufzeitverzeichnis `/run/lcm-agent`
- auf Debian 12 und 13 zusätzlich: Agent vom Kernel beendet (Speichermangel),
  Netz vier Minuten gesperrt, Neustart des Servers mitten in einer Installation,
  `unattended-upgrades` und `needrestart`, Update per Knopf von 2.3.0 auf eine
  neuere Version, spätes Ergebnis nach einem Neustart von LCM

**dnf, zypper, apk und pacman, 25.11.2026:**

- In Containern: AlmaLinux 9 und 10, Fedora 45, openSUSE Leap 16 und Tumbleweed,
  Alpine 3.22 bis 3.24, Arch Linux. Geprüft: Quelle einrichten, installieren,
  Signaturen von Paketen und Index, manipulierte Pakete werden abgewiesen,
  Systemupdate lässt den Agenten stehen, gezielte Installation einer Version,
  Rückkehr auf eine ältere Version nach einem gescheiterten Update.
- Auf dem Proxmox-Testbett: AlmaLinux 10, CentOS Stream 10, Rocky Linux 10,
  Fedora 44, openSUSE Leap 16, Alpine 3.23, Arch Linux. Geprüft: einrichten,
  installieren, Systemupdate, einrichten gegen LCM, Update per Knopf auf eine
  neuere Version, Sperre bleibt danach bestehen.
- Gegen repo.techeve.de mit Agent 2.3.2: AlmaLinux 9 und 10, Fedora 45,
  openSUSE Leap 16 und Tumbleweed, Alpine 3.24, Arch Linux.

**Ältere Agenten:** Agenten ab 2.0.0 arbeiten mit aktuellen LCM-Versionen. Die
CI prüft bei jeder Änderung die echten Agenten 2.0.0, 2.1.0, 2.2.0, 2.2.1 und
2.3.0 gegen das aktuelle LCM (Befehl, Abbruch, Inventar, Konsole, Ergebnis nach
einem Netzabbruch).

**Noch nicht getestet:** `arm64` auf echter Hardware (geplant auf einem
Raspberry Pi). Die arm64-Pakete werden gebaut und veröffentlicht.

### Hinweise je System

- **openSUSE:** Der Agent ist mit einer zypper-Sperre gehalten. Vor
  `zypper rm lcm-agent` die Sperre lösen: `zypper rl lcm-agent`.
- **Alpine:** Die Version hält `lcm-agent=<version>` in `/etc/apk/world`. Wer den
  Agenten von Hand installiert, nimmt `apk add lcm-agent=<version>`.
  Bei der Verwaltung per SSH wirkt die Paket-Fixierung nicht, weil `apk` kein
  „hold“ kennt. LCM schreibt das ins Job-Protokoll, der Job endet trotzdem
  erfolgreich, und das Update hebt auch `apk-tools` an.
- **Arch Linux in Containern:** Fehlt dem Kernel Landlock, verwendet pacman 7
  seine Download-Sandbox nicht. Ab LCM 2.4 wiederholt das Agent-Update den
  Download dann einmal ohne Sandbox; `setup.sh` macht dasselbe.
  Bei der Verwaltung per SSH scheitern in einem solchen Container die Aufnahme
  (LCM kann `sudo` nicht nachinstallieren) und alle Paketjobs an der Sandbox. Mit
  `DisableSandbox` in der `pacman.conf` lief alles. Ein altes Arch-Image braucht
  außerdem vorab einen initialisierten Schlüsselring und ein Komplett-Upgrade
  (`pacman -Syu`). Auf einer VM oder Hardware mit Landlock ist das Problem nicht
  zu erwarten, aber nicht getestet.
- **CentOS Stream:** Trivy unterstützt CentOS Stream nicht. Der CVE-Scan meldet
  dort einen Fehler zur Trivy-Datenbank und liefert kein Ergebnis.

## Server ohne Agent (SSH)

Server, die LCM per SSH verwaltet, brauchen keinen Agenten. Unterstützt sind die
Paketverwaltungen apt, dnf, yum, zypper, pacman und apk. Getestet sind Debian 12
und 13 sowie Ubuntu 22.04, 24.04 und 26.04 (Aufnahme, Service-Benutzer,
eingeschränkter Modus, 21.11.2026).

Auf dem Proxmox-Testbett kamen am 26.11.2026 (LCM 2.4.0 Beta 1) AlmaLinux 10,
Rocky Linux 10, CentOS Stream 10, Fedora 44, openSUSE Leap 16, Alpine 3.23 und
Arch Linux hinzu, je in beiden Modi, voll und eingeschränkt: Aufnahme, Scan,
Paketliste, Update, Firewall und Paket-Fixierung. Nicht getestet sind AlmaLinux 9,
Fedora 45, Alpine 3.22 und 3.24, Red Hat, openSUSE Tumbleweed, die Paketquellen im
eingeschränkten Modus und `arm64`.

## CVE-Abdeckung

Gemessen am 26.11.2026 mit Trivy 0.76.2 an Paketlisten mit bewusst alten Versionen
je Distribution:

- **Funde:** Debian 12 und 13, Ubuntu 24.04, Alpine 3.22 und 3.23.
- **Fehler statt Funde:** CentOS Stream. Trivy findet dort keine verwertbaren
  Red-Hat-Daten.
- **Keine Funde:** Fedora, Arch, Kali, Linux Mint, Zorin OS, UCS und Raspberry Pi OS
  (32 bit). Trivy kennt diese Distributionen nicht.
- **Keine Funde bei Rocky Linux**, obwohl Trivy es kennt: Mit Architekturangabe in der
  Paketkennung fanden sich über 900 Funde, LCM sendet sie in dieser Version nicht.
- **Keine Funde, nicht belegt:** AlmaLinux, Red Hat und openSUSE.

Proxmox VE, Proxmox Backup Server, TurnKey Linux und Raspberry Pi OS (64 bit) melden
sich als Debian. Die Debian-Pakete sind damit abgedeckt, die Eigenpakete nicht. Auf
diesen Systemen selbst wurde das nicht getestet.
