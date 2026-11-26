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
| Proxmox VE, Proxmox Backup Server | 🟡 | 🟡 | 🟡 | 🟡 | teilweise¹ |
| TurnKey Linux | 🟡 | 🟡 | 🟡 | 🟡 | voll |
| Kali Linux | 🟡 | 🟡 | 🟡 | 🟡 | keiner |
| Univention Corporate Server (UCS) | 🟡 | 🟡 | 🟡 | 🟡 | keiner |
| Raspberry Pi OS (64 bit) | 🟡 | 🟡 | 🟡 | 🟡 | teilweise² |
| Raspberry Pi OS (32 bit) | 🟡 | 🟡 | 🟡 | 🟡 | keiner |

### Distributionen ohne apt: nur als Client

LCM selbst läuft hier nur im Docker-Container.

| Distribution | Host | SSH | SSHE | Agent | CVE |
|---|---|---|---|---|---|
| Red Hat Enterprise Linux | ❌ | 🟡 | 🟡³ | 🟡 | voll |
| AlmaLinux 9, 10 | ❌ | 🟡 | 🟡³ | ✅ | voll |
| Rocky Linux 10 | ❌ | 🟡 | 🟡³ | ✅ | voll |
| CentOS Stream 10 | ❌ | 🟡 | 🟡³ | ✅ | wird geprüft |
| Fedora 44, 45 | ❌ | 🟡 | 🟡³ | ✅ | wird geprüft |
| openSUSE Leap 16 | ❌ | 🟡 | 🟡³ | ✅ | voll |
| openSUSE Tumbleweed | ❌ | 🟡 | 🟡³ | ✅ | keiner |
| Arch Linux | ❌ | 🟡 | 🟡³ | ✅ | keiner |
| Alpine 3.22 bis 3.24 | ❌ | 🟡 | 🟡³ | ✅ | voll |

### Desktop-Distributionen

| Distribution | Host | SSH | SSHE | Agent | CVE |
|---|---|---|---|---|---|
| Ubuntu Desktop 24.04, 26.04 | 🟡 | 🟡 | 🟡 | 🟡 | voll |
| Debian 13 Desktop | 🟡 | 🟡 | 🟡 | 🟡 | voll |
| Linux Mint | 🟡 | 🟡 | 🟡 | 🟡 | keiner |
| Zorin OS | 🟡 | 🟡 | 🟡 | 🟡 | keiner |

Die Spalten:

- **Host:** LCM ist auf dem System installiert (`.deb`-Paket oder Docker).
- **SSH:** Server wird per SSH ohne Agent verwaltet, mit vollen Rechten.
- **SSHE:** SSH-Verbindung mit eingeschränktem Modus. LCM arbeitet dann mit
  eingeschränkten Rechten auf dem Server. Den Agenten gibt es nur ohne diesen
  Modus, weil er als Root-Dienst läuft.
- **Agent:** Server läuft mit `lcm-agent`.
- **CVE:** Auf dieser Distribution kann LCM CVE-Scans ausführen. Die Angabe sagt,
  wie weit sie reichen: **voll** = alle Pakete der Distribution, **teilweise** =
  die Pakete der Basis-Distribution, aber nicht die Eigenpakete, **keiner** = für
  die Distribution gibt es keinen CVE-Bestand, ein Scan sagt nichts aus.

¹ Die Debian-Pakete sind abgedeckt, die Proxmox-eigenen Pakete (`pve-*`,
`proxmox-*`) nicht.
² Die Debian-Pakete sind abgedeckt, die Pakete aus `archive.raspberrypi.com` nicht.
³ Updates, Firewall und Paket-Fixierung gehen auch eingeschränkt. Paketquellen
verwaltet LCM im eingeschränkten Modus nur auf apt-Systemen.

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
- **Arch Linux in Containern:** Fehlt dem Kernel Landlock, verwendet pacman 7
  seine Download-Sandbox nicht. Ab LCM 2.4 wiederholt das Agent-Update den
  Download dann einmal ohne Sandbox; `setup.sh` macht dasselbe.

## Server ohne Agent (SSH)

Server, die LCM per SSH verwaltet, brauchen keinen Agenten. Unterstützt sind die
Paketverwaltungen apt, dnf, yum, zypper, pacman und apk. Getestet sind Debian 12
und 13 sowie Ubuntu 22.04, 24.04 und 26.04 (Aufnahme, Service-Benutzer,
eingeschränkter Modus, 21.11.2026).
