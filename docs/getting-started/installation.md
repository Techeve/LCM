---
sidebar:
  order: 2
title: Installation
description: LCM als Binary, als Debian-/Ubuntu-Paket oder mit Docker installieren.
---

LCM ist ein einzelnes Binary mit eingebettetem Frontend. Es gibt drei Wege, es
in Betrieb zu nehmen. Für Produktivsysteme empfiehlt sich das **`.deb`-Paket**
(systemd-Dienst) oder **Docker**.

## Voraussetzungen

- Einen Host für LCM: **Debian 12/13 oder Ubuntu 22.04/24.04/26.04** (amd64 oder
  arm64), oder Docker auf einem beliebigen Linux-System. Was getestet ist,
  steht unter [Unterstützte Systeme](/reference/supported-systems/).
- Die zu verwaltenden Server brauchen entweder SSH-Zugriff (Passwort oder Key) -
  LCM legt dort beim Onboarding einen eigenen Service-User an - oder den
  `lcm-agent`, der sich ausgehend mit LCM verbindet. Verwalten lassen sich
  Debian, Ubuntu, die RHEL-Familie, openSUSE, Arch Linux und Alpine.
- Optional **Trivy** auf dem LCM-Host für den CVE-Scan. Fehlt es, deaktiviert
  sich das Feature sauber. Mitinstalliert wird dabei **bubblewrap** - die
  Sandbox, in der LCM den Scanner startet (siehe
  [CVE-Scan](/guides/security-cve#der-scanner-läuft-eingesperrt)).

:::caution[Windows und macOS werden nicht unterstützt]
Das Binary lässt sich zwar für Windows und macOS übersetzen, wir liefern und
testen es dort aber nicht. Zentrale Funktionen setzen Linux voraus - die
Sandbox für den CVE-Scan, die systemd-Einbindung und die Einrichtung des
LCM-Hosts. Für einen Arbeitsplatzrechner ist der Docker-Weg (Variante 2) der
richtige.
:::

## Variante 1: Debian-/Ubuntu-Paket (empfohlen)

**Am einfachsten über das Techeve-APT-Repository** - einmal einrichten, danach
installieren und dauerhaft per `apt upgrade` aktuell halten:

```sh
# 0. Voraussetzungen - Minimal-/Cloud-Images liefern curl nicht mit
sudo apt-get install -y curl ca-certificates

# 1. Repository (inkl. Signaturschlüssel) einrichten
curl -fsSL https://repo.techeve.de/setup.sh | sudo sh

# 2. LCM installieren
sudo apt install lcm

# 3. Später aktualisieren (mit dem übrigen System)
sudo apt update && sudo apt upgrade
```

`setup.sh` erkennt die Paketverwaltung des Systems (apt, dnf, zypper, apk oder
pacman) und legt Paketquelle und Signaturschlüssel an (amd64 und arm64). Auf dem
LCM-Host ist das apt; `lcm` ist danach ein normales apt-Paket, und Updates kommen
automatisch mit dem System. Auf verwalteten Servern richtet dasselbe Skript die
Quelle für den `lcm-agent` ein (siehe [Schnellstart](/getting-started/quickstart/)).
`--beta` wählt den Beta-Kanal, `--uninstall` entfernt Quelle und Schlüssel wieder.

Das Paket `lcm` gibt es nur für apt. Auf den anderen Systemen läuft LCM im
Docker-Container (Variante 2).

Die Quelle zeigt auf den Community-Kanal (`https://repo.techeve.de community main`).
Ältere Installationen mit der Suite `stable` laufen weiter, das Paket stellt sie
beim Update selbst auf `community` um. Die anderen Kanäle (Beta, Enterprise)
wählst du in LCM unter Einstellungen, Subscription; siehe
[Paketkanäle](/reference/repo-channels/).

**Alternativ ohne Repository** - ein einzelnes Paket aus den
[Releases](https://gitlab.techeve.de/techeve/lcm/-/releases?mtm_campaign=linking&mtm_kwd=doc) laden
(`lcm_<version>_amd64.deb` bzw. `..._arm64.deb`, Architektur via
`dpkg --print-architecture`) und installieren:

```sh
sudo apt install ./lcm_<version>_amd64.deb
```

Beide Wege richten LCM als unprivilegierten `systemd`-Dienst ein (Autostart,
HTTPS).

### Neu installieren mit neuem Datenbestand

Wer LCM komplett neu aufsetzen will - ohne Datenbank, ohne Schlüssel, ohne
Sicherungen von früher -, entfernt die alte Installation mit **`purge`**:

```sh
sudo apt purge lcm      # entfernt alles, was LCM angelegt hat
sudo apt install lcm    # frische Installation
```

`purge` löscht die Datenbank, den Master-Key (auch ein verschlüsseltes
systemd-Credential), die Sicherungen unter `/var/lib/lcm` und `/etc/lcm`, das
Verwaltungskonto `lcm-svc` auf diesem Rechner und die Host-Einstellungen, die
LCM dort gesetzt hat (sshd-Include und Drop-ins, apt-Cache, nftables-Tabelle).
Die Original-`sshd_config` wird zurückgespielt.

:::caution
Mit `purge` sind Datenbank, Schlüssel und Sicherungen **unwiderruflich** weg.
Ohne den Master-Key sind die Zugangsdaten der verwalteten Server nicht mehr
lesbar. Vorher `/var/lib/lcm` sichern, wenn die Daten noch gebraucht werden.
:::

Ein einfaches `apt remove lcm` behält dagegen alles. Nach einer erneuten
Installation macht LCM dort weiter, wo es aufgehört hat.

Nicht entfernt werden: Linux-Benutzer, die LCM auf andere Server verteilt hat
(samt Home-Verzeichnissen), ufw-Regeln und Programme wie Trivy, bubblewrap und
apt-cacher-ng.

### Update per Klick

Liegt im eingestellten Paketkanal eine neuere Version, erscheint oben ein
Balken - samt Schaltfläche **Jetzt installieren**. LCM spielt sein eigenes Paket
dann selbst ein. Drei Dinge sind daran wichtig:

- **Vorher wird gesichert.** Vor dem Einspielen erstellt LCM ein
  System-Backup - und schlägt das fehl, wird **nicht** aktualisiert. Wer sein
  eigenes Verwaltungssystem aktualisiert, hat im Fehlerfall kein zweites, das
  ihm hilft. Der Balken nennt die erstellte Datei; ohne hinterlegte
  Backup-Passphrase bricht der Vorgang mit genau dieser Begründung ab.
- **Laufende Jobs werden abgewartet.** Der Balken sagt an, worauf gewartet
  wird („es läuft noch: …"); erst wenn kein Job mehr läuft, geht es los.
  Nach 30 Minuten bricht das Warten mit einer Meldung ab.
- **LCM startet dabei neu.** Der apt-Lauf hängt in einer eigenen
  systemd-Unit (`lcm-self-update`) und überlebt den Neustart deshalb;
  das Protokoll steht danach in `journalctl -u lcm-self-update`. Die
  Oberfläche merkt den Versionswechsel von selbst und lädt sich neu.

Die Schaltfläche gibt es nur, wo sie etwas bewirken kann: bei einer
Installation aus dem Debian-Paket, deren Host als apt-Server in der
Verwaltung steht. Andernfalls nennt der Balken den Grund. Im Container wird
statt dessen das Image getauscht.

Die `lcm-agent`-Pakete auf verwalteten Servern gehören nicht dazu: Sie
aktualisiert LCM auf Anweisung, per Knopf oder Gruppenregel (ab LCM 2.4,
[Agent aktualisieren](/guides/remote/#agent-aktualisieren)).

Der Dialog *Über LCM* (Kontomenü oben rechts) hat außerdem
**Jetzt prüfen**: Das fragt den Paketkanal sofort ab und zeigt das Ergebnis
direkt neben dem Knopf an - auch dann, wenn LCM bereits aktuell ist.

**Neu in dieser Version.** Nach einem Update zeigt LCM jedem Administrator
einmal ein Fenster *Neu in Version …* mit den Neuerungen. Die Hinweise holt
LCM vom Doku-Server (doc.techeve.de); ist der nicht erreichbar, bleibt das
Fenster zu und erscheint beim nächsten Mal, sobald er wieder antwortet.
Nach *Schließen* kommt es nicht wieder.

### Trivy für den CVE-Scan nachinstallieren

Der CVE-Scan braucht [Trivy](https://trivy.dev). Es liegt in **keiner**
Standard-Paketquelle von Debian oder Ubuntu und muss deshalb aus der
Hersteller-Quelle eingerichtet werden - LCM läuft ohne Trivy normal weiter,
der CVE-Scan bleibt dann aber deaktiviert:

```sh
# Voraussetzungen - wget und gnupg fehlen auf Minimal-/Cloud-Images;
# bubblewrap ist die Sandbox, in der LCM den Scanner startet
sudo apt-get install -y wget gnupg ca-certificates bubblewrap

wget -qO- https://get.trivy.dev/deb/public.key \
  | sudo gpg --batch --yes --dearmor -o /usr/share/keyrings/trivy.gpg
echo "deb [signed-by=/usr/share/keyrings/trivy.gpg] https://get.trivy.dev/deb generic main" \
  | sudo tee /etc/apt/sources.list.d/trivy.list
sudo apt update && sudo apt install trivy
```

Die Schritte gelten unverändert für Debian und Ubuntu; getestet auf Debian 12
und 13 sowie Ubuntu 22.04, 24.04 und 26.04 (Übersicht: [Unterstützte Systeme](/reference/supported-systems/)). **Debian 11** wird nicht mehr
unterstützt (aus dem Support gelaufen).

Läuft LCM auf demselben Host, geht es auch per Klick: *Server-Detail des
LCM-Hosts → Trivy einrichten*. Wurde Trivy ohne bubblewrap eingerichtet, rüstet
*Sandbox nachrüsten* es nach.

| Pfad | Inhalt |
|------|--------|
| `/usr/bin/lcm` | die Programmdatei (Binary mit eingebettetem Web-UI) |
| `/lib/systemd/system/lcm.service` | die gehärtete systemd-Unit |
| `/etc/lcm/config.json` | Konfiguration (mit sicheren Standardwerten erzeugt, ohne Geheimnis) |
| `/var/lib/lcm/` | Zustand: verschlüsselte DB, Master-Key, TLS-Zertifikat, Backups |
| `/var/lib/lcm/logs/lcm.log` | persistente, rotierende Logdatei (s.u.) |

Der Dienst läuft als eigener System-Benutzer **`lcm`** ohne root-Rechte.

```sh
systemctl status lcm      # Zustand
journalctl -u lcm -f      # Logs live
```

### Persistente Logdatei & Dienst-Überwachung

Zusätzlich zu `stdout` (journald/Docker) schreibt LCM eine **dauerhafte Logdatei**
unter `<Datenverzeichnis>/logs/lcm.log` (Paket: `/var/lib/lcm/logs/lcm.log`;
konfigurierbar über `log_file` in der `config.json`). Sie **rotiert** automatisch
(ab 10 MB, bis zu 7 komprimierte Altstände, max. 7 Tage) und eignet sich, um im
Nachhinein Neustarts, Abstürze und Aktionen nachzuvollziehen:

```sh
grep 'LCM-Dienst' /var/lib/lcm/logs/lcm.log   # jeder Start/Stopp
```

- **`=== LCM-Dienst gestartet ===`** - bei JEDEM (Neu-)Start, mit Version, Build und **PID**.
- **`=== LCM-Dienst wird beendet ===`** - nur bei sauberem Stopp (Signal). Folgt einem
  Start **kein** solcher Eintrag, war es ein **Absturz/harter Kill** - genau daran
  erkennt man ungeplante Neustarts.
- Aktionen wie **Backups** (`system-backup erstellt`), CVE-/Docker-Scans usw. stehen ebenfalls drin.

## Variante 2: Docker / Docker Compose

### Fertiges Image von Docker Hub (empfohlen)

Die Releases liegen als Multi-Arch-Images (amd64/arm64) auf Docker Hub:
[`techeve/lcm`](https://hub.docker.com/r/techeve/lcm) - `:latest` ist das
aktuelle stabile Release, `:beta` die Vorabversion, dazu jede Version als
eigenes Tag. Der CVE-Scanner-Sidecar heißt
[`techeve/lcm-trivyd`](https://hub.docker.com/r/techeve/lcm-trivyd).

```sh
mkdir -p data && sudo chown 1000 data   # der Container schreibt als UID 1000
docker run -d --name lcm \
  -p 9310:9310 -v "$PWD/data:/data" \
  --read-only --tmpfs /tmp --cap-drop ALL \
  --restart unless-stopped \
  techeve/lcm:latest
docker logs -f lcm     # Erststart: hier steht das generierte Admin-Passwort
```

Mit Docker Compose: in der mitgelieferten
[`docker/docker-compose.yml`](https://gitlab.techeve.de/techeve/lcm/-/blob/community/docker/docker-compose.yml?mtm_campaign=linking&mtm_kwd=doc)
den `build:`-Block entfernen und `image: techeve/lcm:latest` eintragen -
alle Härtungs-Flags bleiben erhalten.

### Selbst bauen

```sh
make docker-build          # Linux-Binary bauen (inkl. Audits) + Image erzeugen
docker compose up -d
docker compose logs -f     # Erststart: hier steht das generierte Admin-Passwort
```

Beim ersten Start entstehen im Host-Ordner `./data` die Konfiguration, die
SQLite-Datenbank und `version.json`. Das Runtime-Image ist minimal gehärtet
(Alpine, non-root, `read-only`, `cap_drop: ALL`). Der Container spricht standardmäßig
HTTPS mit selbstsigniertem Zertifikat. Für öffentliche Deployments hinterlegst du
ein eigenes Zertifikat (Volume plus `tls_certificates`, Neuladen mit
`docker kill -s HUP lcm`) oder schaltest einen Reverse-Proxy mit echtem
Zertifikat davor (Beispiele und was dabei in LCM einzustellen ist:
[Reverse-Proxy & Absicherung von außen](/guides/reverse-proxy/)).

Details und alle Härtungs-Flags: [Docker-Betrieb](/guides/docker/) und
[Paketierung](/reference/packaging/).

## Variante 3: Aus dem Quellcode bauen

```sh
make build     # npm audit → vite build → govulncheck → go build
./bin/lcm      # erzeugt beim ersten Start config.json, lcm.key + DB
```

Der Erststart gibt das initiale Admin-Passwort **einmalig** auf der Konsole aus.

### Demo-Modus

Zum gefahrlosen Ausprobieren mit Beispiel-Servern und simulierten Daten:

```sh
./bin/lcm --demo
```

Der Demo-Modus ist ausschließlich über dieses Flag aktivierbar (kein
config.json-Feld) und wirkt nur beim ersten Seeding einer frischen Datenbank.
Eine reguläre Neuinstallation startet leer.

## Kommandozeilen-Optionen

Das Binary kennt nur wenige Flags. Start- und Betriebsparameter stehen in der
`config.json`, die übrigen Einstellungen in der Datenbank und in der Oberfläche
(alle Felder der Datei: [Konfigurationsdatei config.json](/guides/config-json/)):

| Flag | Wirkung |
|------|---------|
| `--data <verz>` | Datenverzeichnis für `config.json`, `app.db`, `lcm.key` und `version.json`. Default: Verzeichnis des Binaries; im Container typisch `/data`. |
| `--config <pfad>` | Pfad zur `config.json` (Default: im Datenverzeichnis). |
| `--demo` | Beim ersten Seeding einer frischen DB Testdaten anlegen (Server, Pakete, Job-Historien). |
| `--dev` | Entwicklungsmodus: erlaubt **unverschlüsseltes HTTP** (sonst immer HTTPS). |
| `--debug` | Hebt das Log-Level zur Laufzeit auf `debug` an, ohne die `config.json` zu ändern. |
| `--version` | Version ausgeben und beenden. |

:::caution[Das Daten-Flag heißt `--data`]
Nicht `--data-dir`. Beispiel für einen Container-Betrieb mit gemountetem Volume:

```sh
./lcm --data /data
```
:::

Zusätzlich gibt es ein Unterkommando zur **Master-Key-Rotation** (siehe
[Sicherheitsmodell](/reference/security-model/)):

```sh
./lcm rotate-db-key      # neuen Master-Key erzeugen, alle Felder neu verschlüsseln
```

## Umgebungsvariablen

Praktisch im Container-/Dienst-Betrieb, um Werte zu übersteuern, ohne die
(evtl. read-only gemountete) `config.json` anzufassen. Wer gewinnt, wenn Datei,
Oberfläche und Umgebung etwas anderes sagen, steht unter
[Konfigurationsdatei config.json](/guides/config-json/#datei-oberfläche-und-umgebung):

| Variable | Wirkung |
|----------|---------|
| `LCM_DATA` | Datenverzeichnis (wie `--data`). |
| `LCM_HOST` | Bind-Adresse der Weboberfläche/REST-API (überschreibt `host`). |
| `LCM_PORT` | Port der Weboberfläche/REST-API (überschreibt `port`). |
| `LCM_AGENT_HOST` | Bind-Adresse des Agent-Listeners (überschreibt `agent_host`). |
| `LCM_AGENT_PORT` | Port des Agent-Listeners (überschreibt `agent_port`); `0` schaltet ihn ab. Eine gesetzte Umgebungsvariable sperrt das Feld in der Oberfläche. |
| `LCM_TRIVY_URL` | Adresse eines Trivy-Sidecars für den CVE-Scan (überschreibt `trivy_url`), siehe [Paketierung](/reference/packaging/#trivy-im-container-betrieb). |
| `LCM_TRIVY_TOKEN` | Token für den Sidecar (überschreibt `trivy_token`); Pflicht, sobald eine Adresse gesetzt ist. |
| `LCM_ENCRYPTION_KEY` | Master-Key (Base64, 32 Byte). Gewinnt gegen `lcm.key` und das systemd-Credential, siehe [Sicherheitsmodell](/reference/security-model/). |
| `LCM_BACKUP_PASSPHRASE` | Passphrase für **automatische** Backups (siehe [Backups](/guides/backups/)). |
| `LCM_RESTORE_AUTO_RESTART` | `1`/`true` = nach einem vorbereiteten Restore automatisch neu starten. |
| `TZ` | Zeitzone, z.&nbsp;B. `Europe/Berlin` - die tzdata sind ins Binary eingebettet, funktioniert also auch in minimalen Containern. |

```sh
LCM_HOST=0.0.0.0 LCM_PORT=443 ./lcm            # UI/REST an alle Interfaces, Port 443
LCM_AGENT_PORT=0 ./lcm                          # LCM Remote (Agent-Listener) abschalten
```

Ports unter 1024 wie `443` darf der Dienst der Paketinstallation öffnen: Die
mitgelieferte Dienstdatei gibt ihm dafür die Berechtigung `CAP_NET_BIND_SERVICE`,
ein Drop-in ist nicht nötig. Siehe
[Zertifikate, Let's Encrypt & Port 443](/guides/zertifikate/#port-80-und-443-freigeben).

## Netzwerk-Ports

LCM bindet bis zu **drei** getrennte Listener - bewusst auf eigenen Ports. Dazu
kommen zwei **optionale** Ports (siehe unten):

| Port (Default) | Listener | Bind (Default) | Protokoll |
|---|---|---|---|
| `9310` | **Weboberfläche + REST-API** (`host`/`port`) | `127.0.0.1` | HTTPS (LCM-Zertifikat, selbstsigniert, oder ein eigenes; `--dev` = HTTP) |
| `9320` | **Agent-Listener** - LCM Remote, ausschließlich `/mqtt` (`agent_host`/`agent_port`); der Schalter für den Agenten schaltet ihn ab | `0.0.0.0` | HTTPS (Zertifikat nach dem angefragten Namen, sonst das LCM-Zertifikat) |
| `9330` | **MCP-Listener** - optional, standardmäßig **aus**; Schalter unter *Einstellungen → Allgemein* und *Einstellungen → MCP* (`mcp_port`) | wie die Weboberfläche | immer HTTPS (Zertifikat nach dem angefragten Namen) |
| `443` (frei wählbar) | **Gemeinsamer Port** - optional, Adressmodus C (in der Oberfläche oder per `shared_tls_listen`): Weboberfläche, Agent und MCP nach dem angefragten Namen | aus | HTTPS |
| `80` (`acme_http_listen`) | **Let's Encrypt, HTTP-01** - optional, nur während einer Prüfung offen, sonst geschlossen | aus | HTTP |

Auf dem Agent-Port liegt **nur** die Agent-Schnittstelle, auf dem UI/REST-Port
**keine** - und umgekehrt. Details: [LCM Remote](/guides/remote/) und
[MCP-Schnittstelle](/guides/mcp/). Die Ports lassen sich in *Einstellungen → Allgemein* ändern
([Öffentliche Adressen & Adressmodus](/guides/adressen/)). Die beiden
optionalen Ports, eigene Zertifikate und Let's Encrypt beschreibt
[Zertifikate, Let's Encrypt & Port 443](/guides/zertifikate/). Welche Netze die
Dienste erreichen dürfen, legen die [Erlaubten Netzbereiche](/guides/netzbereiche/)
fest.

:::note[Jeder Neustart beendet alle Sitzungen]
Browser-Sitzungen und das Signaturmaterial der 2FA-Challenge leben nur im
Arbeitsspeicher; ein gespeichertes Geheimnis gibt es dafür nicht. Folge: Nach
einem (Neu-)Start sind **alle** Sitzungen ungültig - jeder
muss sich neu anmelden. Das deckt u.&nbsp;a. Rebuild, Prozess-Neustart und
ein frisches Datenbank-Seeding ab.
:::

## Erste Anmeldung

Das initiale Admin-Passwort steht in der Konsolen-/Journal-Ausgabe des ersten
Starts:

```sh
journalctl -u lcm | grep -A3 'Admin-Zugang'   # bei der .deb-Installation
```

Dann `https://<host>:9310` im Browser öffnen (self-signed Zertifikat - die
Browser-Warnung ist erwartbar), als `admin` anmelden und das Passwort ändern.

Danach fragt LCM einmalig, **wie die Anmeldung im Browser funktionieren soll**:
nur im Arbeitsspeicher des Tabs (Standard, sicherste Variante: kein Cookie, nach
dem Neuladen oder in einem neuen Tab meldest du dich neu an) oder als
Sitzungs-Cookie (bequemer, nur über HTTPS). Das lässt sich jederzeit unter
*Einstellungen → Allgemein → Anmeldung im Browser* ändern, direkt neben der
*Sitzungsdauer in Minuten*; ein Wechsel meldet alle Benutzer ab. Skripte
nutzen davon unabhängig einen API-Schlüssel.

Nach dem Passwort und der Zwei-Faktor-Anmeldung führt der **Einrichtungsassistent**
durch die ersten Einstellungen. Das gibt es nur auf einer frischen Installation. Er
fragt in sechs Schritten:

1. **Wie ist LCM erreichbar?** Adressmodus, Adressen und Ports. Dieser Schritt lässt
   sich nicht überspringen.
2. **Welche Dienste sollen laufen?** Agent und MCP.
3. **Wer darf LCM erreichen?** Erlaubte Netzbereiche je Dienst.
4. **Zertifikat:** Let's Encrypt oder ein Reverse-Proxy davor. Danach leitet der
   Assistent auf die neue Adresse weiter, bei einem neuen Namen meldest du dich neu
   an.
5. **Neue Server aufnehmen:** der Onboarding-Schlüssel (überspringbar).
6. **Automatisches Backup** (überspringbar).

Zum Schluss öffnet LCM die Seite des LCM-Hosts mit einer Einführung in Trivy,
apt-cacher-ng und CrowdSec LAPI. Die Schritte im Einzelnen:
[Öffentliche Adressen & Adressmodus](/guides/adressen/#einrichtungsassistent).
Später änderst du alles unter *Einstellungen → Allgemein*.

Weiter mit dem [Schnellstart](/getting-started/quickstart/).
