---
sidebar:
  order: 22
title: MikroTik RouterOS
description: RouterOS-Geräte überwachen - Fokus auf der Aktualität der Betriebssystem-Version.
---

LCM unterstützt **MikroTik RouterOS** als eigenen Gerätetyp. Anders als bei
Linux-Servern gibt es hier **keine Paketverwaltung** und keine POSIX-Shell -
RouterOS bringt eine eigene CLI mit, die einzelne Kommandos über SSH-Exec
entgegennimmt. Deshalb sind Firewall-Verwaltung, CVE-Scan, Paketquellen und
Benutzer-Sync **nicht möglich**. LCM konzentriert sich auf das, was zählt: die
**Aktualität der RouterOS-Version**.

## Was überwacht wird

- **Versions-Aktualität**: Der Router meldet über
  `/system package update check-for-updates` selbst, ob eine neuere Version
  seines Kanals verfügbar ist. LCM vergleicht `installed-version` mit
  `latest-version`; sind sie verschieden, ist ein Update verfügbar und die
  Statusampel schaltet auf **Gelb** - mit dem Befund
  *„Neuere RouterOS-Version verfügbar (x.y.z) - Update empfohlen"*. Ein
  aktuelles, erreichbares Gerät erreicht die **Bestnote**.
- **Basis-Inventar** aus `/system resource print` und `/system routerboard print`:

  | Feld | Quelle (RouterOS-CLI) |
  | --- | --- |
  | Version + Kanal | `version` (z. B. `7.15.3 (stable)`) bzw. `check-for-updates` |
  | Modell / Board | `routerboard model`, sonst `board-name`/`platform` |
  | Architektur | `architecture-name` (z. B. `arm64`, `x86_64`) |
  | CPU / Kerne | `cpu`, `cpu-count` |
  | RAM (gesamt/belegt) | `total-memory`, `free-memory` |
  | Speicher (gesamt/belegt) | `total-hdd-space`, `free-hdd-space` |

Der **Kanal** (stable / long-term / testing) wird aus der Versionsangabe in
Klammern bzw. dem `channel`-Feld übernommen.

Firewall-Aktivität, SSH-Härtung und Paket-CVEs fließen bei RouterOS **nicht** in
die Bewertung ein - LCM kann diese Bereiche dort nicht verwalten und wertet sie
deshalb nicht als Mangel.

## Gerät hinzufügen

*Server hinzufügen → Modus **MikroTik RouterOS***. Es genügen **Name**,
**Host/Port** (Standard-SSH-Port 22) und ein (möglichst nur lesender)
**RouterOS-Benutzer**. Nach der Bestätigung des Host-Key-Fingerprints
(MitM-Schutz, Trust-on-First-Use) verbindet sich LCM rein lesend. Zwei
Authentifizierungswege:

- **Passwort**: LCM verbindet sich sofort, liest Version und Inventar und legt
  das Gerät **online** an. Das Passwort wird AES-GCM-verschlüsselt gespeichert.
  Erkennt LCM kein RouterOS (kein `/system resource print`-Ergebnis), bricht das
  Onboarding mit einem klaren Hinweis ab.
- **Public Key**: LCM erzeugt ein Schlüsselpaar und zeigt den Public Key an. Das
  Gerät bleibt zunächst **offline**, bis du den Schlüssel auf dem Router
  importierst:

  ```
  /user ssh-keys import public-key-file=lcm.pub user=<benutzer>
  ```

  Danach verbindet der nächste Refresh und das Gerät geht online.

:::tip[Nur-Lese-Benutzer anlegen]
LCM braucht auf dem Router nur Leserechte. Ein dedizierter Benutzer der
eingebauten `read`-Gruppe genügt:

```
/user add name=lcm group=read password=<stark>
```

Für den Key-Login danach den von LCM angezeigten Public Key auf diesen Benutzer
importieren (siehe oben).
:::

## Was auf RouterOS gesperrt ist

| Funktion | Auf RouterOS | Warum |
| --- | --- | --- |
| Versions-Überwachung | ✅ | RouterOS-Eigencheck `check-for-updates` |
| Basis-Inventar | ✅ | `/system resource`/`routerboard print` |
| Aktualisieren (Neu-Scan) / Entfernen | ✅ | reine Überwachung |
| Firewall-Verwaltung | ❌ | keine ufw/firewalld/nftables - eigene RouterOS-Firewall |
| CVE-Scan | ❌ | kein Paketinventar/keine SBOM-Basis |
| Paketquellen (Repos) | ❌ | keine Linux-Paketverwaltung |
| Paket-Updates | ❌ | RouterOS aktualisiert das System als Ganzes |
| Benutzer-Sync | ❌ | keine POSIX-Benutzer/`/etc/passwd` |
| SSH-Härtung | ❌ | kein `sshd_config`/keine Root-Shell |
| Sicherheits-Tools (fail2ban/CrowdSec) | ❌ | keine Paketverwaltung |
| DNS setzen | ❌ | kein `/etc/resolv.conf`/systemd-resolved |

Diese Funktionen sind in der UI ausgeblendet bzw. werden serverseitig abgewiesen -
sie setzen eine Linux-Paketverwaltung oder eine Root-Shell voraus, die RouterOS
nicht bietet. Die RouterOS-Kommandos laufen bewusst **roh** (kein `sudo`/`sh -c`)
über die RouterOS-CLI.
