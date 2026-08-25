---
sidebar:
  order: 19
title: DNS setzen & testen
description: Bis zu drei Nameserver pro Server setzen und die DNS-Verfügbarkeit prüfen.
---

LCM kann pro Server bis zu **drei Nameserver** setzen und die **DNS-Verfügbarkeit**
prüfen - als Server-Aktion und als Gruppen-Regel. Das Setzen ist bewusst
**selbstsichernd**: Bricht die Namensauflösung nach dem Schreiben, rollt LCM die
Änderung automatisch zurück.

## Vorgaben pflegen

Unter *Einstellungen → DNS* werden zwei Listen gepflegt:

- **Vorgabe-Nameserver** - erscheinen beim Setzen der Server-DNS als Auswahl. Je
  Zeile ein Eintrag, entweder `Label = IP` (z.&nbsp;B. `Cloudflare = 1.1.1.1`) oder
  nur die IP.
- **Test-Domains** - die Domains, deren Auflösbarkeit der DNS-Test auf dem Server
  prüft (je Zeile eine).

Leere Felder = die eingebauten Standardlisten:

| Vorgabe-Nameserver (Default) | Test-Domains (Default) |
| --- | --- |
| `Cloudflare = 1.1.1.1` | `deb.debian.org` |
| `Cloudflare (2) = 1.0.0.1` | `github.com` |
| `Google = 8.8.8.8` | `cloudflare.com` |
| `Google (2) = 8.8.4.4` | |
| `Quad9 = 9.9.9.9` | |

:::tip[Interne Resolver als Vorgabe]
Betreibst du eigene Resolver (z. B. Pi-hole/Unbound im LAN), trag sie als
Vorgaben ein - dann sind sie beim Setzen ein Klick entfernt:

```
LAN-Resolver = 10.0.0.53
LAN-Resolver (2) = 10.0.0.54
```
:::

## DNS auf einem Server setzen

Auf der Server-Detailseite über das **Zahnrad → Abschnitt „DNS-Server"**: bis zu
drei Nameserver eintragen (freie IP **oder** Auswahl aus den Vorgaben) und
**„DNS anwenden"**.

- **systemd-resolved** aktiv → LCM schreibt das Drop-in
  `/etc/systemd/resolved.conf.d/lcm-dns.conf` und startet den Dienst neu:

  ```ini
  [Resolve]
  DNS=1.1.1.1 9.9.9.9
  ```

- sonst → LCM schreibt `/etc/resolv.conf` (mit Sicherung `*.lcm-bak`, ein
  vorhandener Symlink wird zuvor entfernt):

  ```text
  nameserver 1.1.1.1
  nameserver 9.9.9.9
  ```

- Nach dem Schreiben prüft LCM die Auflösung (gegen die erste gepflegte
  Test-Domain, ersatzweise `deb.debian.org`). **Scheitert sie, wird die Änderung
  automatisch zurückgerollt** - Drop-in gelöscht bzw. Sicherung zurückgespielt,
  der Server bleibt arbeitsfähig. Der Job endet dann mit einem klaren Fehler.
- **Alle Felder leer + Anwenden** = LCM gibt die DNS-Verwaltung wieder frei
  (Drop-in entfernt bzw. Sicherung zurückgespielt).

Es lassen sich **höchstens drei** Nameserver setzen; jeder muss eine gültige
IPv4- oder IPv6-Adresse sein (kein Hostname).

### Ablauf bei gebrochener Auflösung (Beispiel)

Du setzt versehentlich `10.0.0.99` (ein Resolver, der von diesem Server aus nicht
erreichbar ist):

1. LCM schreibt das Drop-in bzw. `/etc/resolv.conf` und startet ggf.
   systemd-resolved neu.
2. Der Auflösungstest gegen `deb.debian.org` schlägt fehl.
3. LCM verwirft die neue Datei und spielt die Sicherung zurück, startet den
   Dienst erneut.
4. Job-Ausgang: *„DNS-Test nach dem Setzen fehlgeschlagen - zurueckgerollt"*
   (exit 1). Der Server nutzt weiterhin seine bisherigen Resolver.

:::note[NetworkManager / netplan / Proxmox]
Wird DNS von NetworkManager, netplan oder (bei Proxmox VE) der Host-Netzwerk-
Konfiguration verwaltet, kann eine direkt geschriebene Konfiguration später
überschrieben werden. Der Auflösungstest schützt vor einer kaputten
Namensauflösung; die **dauerhafte** Pflege gehört dann in das jeweilige
Netzwerk-Werkzeug.
:::

:::caution[Eingeschränkter Sudo-Modus]
Im eingeschränkten Sudo-Modus schreibt nicht ein Inline-Skript,
sondern der validierende `lcm-helper` die DNS-Konfiguration - mit **derselben**
Logik samt Auflösungstest und Rollback. RouterOS-Geräte kennen weder
`/etc/resolv.conf` noch systemd-resolved: dort weist LCM die DNS-Aktion sauber ab,
statt ein Linux-Shell-Skript in die RouterOS-CLI zu schicken.
:::

## DNS-Test

Der Test prüft rein lesend (`getent` / `nslookup`), ob die gepflegten
Test-Domains auf dem Server aufgelöst werden. Ergebnis dreistufig:

| Status | Bedeutung |
|---|---|
| **vollständig** (grün) | alle Test-Domains aufgelöst |
| **teilweise** (gelb) | einige aufgelöst, andere nicht |
| **keine Auflösung** (rot) | keine Test-Domain auflösbar |

Der Test ist rein lesend und braucht **kein** sudo - er läuft daher auch im
eingeschränkten Modus.

Auslösen:

- **Automatisch bei jedem Scan** - die aktive DNS-Konfiguration wird
  ausgelesen und der Auflösungstest gleich mit ausgeführt. Das gilt für
  **alle** Scan-Wege: *Hardware aktualisieren*, *Alles aktualisieren*, den
  Scan beim Anbinden eines Servers und den geplanten **System-Sync**.
  Rein lesend und ohne `sudo`, kostet also nichts.

  :::note[Auch ohne Handgriff aktuell]
  Der DNS-Befund lief lange nur beim manuellen Refresh. Auf Servern, die
  ausschließlich der geplante Sync anfasst, blieben die DNS-Daten damit
  dauerhaft leer - was aussieht, als würde der Check kein DNS prüfen. Seitdem
  er in jedem Scan-Weg steckt, ist der Befund überall so aktuell wie der letzte
  Kontakt.
  :::
- **Server-Aktion** - Server-Detail → *Aktionen → DNS-Test*.
- **Gruppen-Regel** - in einer Gruppe eine Regel vom Typ **„DNS-Test"** anlegen
  (geplant oder manuell auslösbar); sie nutzt die zentral gepflegten Test-Domains.

Das Ergebnis erscheint auf der Server-Detailseite unter **System & Security**
neben der Firewall - samt dem **aktuell aktiven DNS** des Servers (bei
systemd-resolved die echten Upstreams via `resolvectl`, sonst die `nameserver`
aus `/etc/resolv.conf` ohne den `127.0.0.53`-Stub). Der genaue Ausgang je Domain
steht im Job-Output und im Tooltip des Status, z. B.:

```text
OK: github.com, cloudflare.com | FAIL: deb.debian.org
```

Dieses Beispiel ergäbe den Status **teilweise** (gelb) - ein Hinweis, dass ein
Resolver nur einen Teil der Zonen bedient oder eine Domain (temporär) nicht
auflösbar ist.
