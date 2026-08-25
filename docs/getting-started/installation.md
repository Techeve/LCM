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

- Einen Host für LCM: **Debian 12/13 oder Ubuntu 22.04/24.04** (amd64 oder
  arm64). Andere Linux-Distributionen laufen erfahrungsgemäß, sind aber nicht
  Teil unserer Tests.
- SSH-Zugriff (Passwort oder Key) auf die zu verwaltenden Server - LCM legt
  dort beim Onboarding einen eigenen Service-User an.
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

**Am einfachsten über das TechEve-APT-Repository** - einmal einrichten, danach
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

`setup.sh` legt die Paketquelle und den GPG-Schlüssel an (Debian/Ubuntu, amd64 &
arm64); danach ist `lcm` ein normales apt-Paket. So kommen Updates automatisch
mit dem System.

**Alternativ ohne Repository** - ein einzelnes Paket aus den
[Releases](https://gitlab.techeve.de/techeve/lcm/-/releases) laden
(`lcm_<version>_amd64.deb` bzw. `..._arm64.deb`, Architektur via
`dpkg --print-architecture`) und installieren:

```sh
sudo apt install ./lcm_<version>_amd64.deb
```

Beide Wege richten LCM als unprivilegierten `systemd`-Dienst ein (Autostart,
HTTPS).

### Update per Klick

Liegt im eingestellten Paketkanal eine neuere Version, erscheint oben ein
Balken - samt Schaltfläche **Jetzt updaten**. LCM spielt sein eigenes Paket
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

*Einstellungen → Info* (Klick auf den Copyright-Vermerk unten) hat außerdem
**Jetzt prüfen**: Das fragt den Paketkanal sofort ab und zeigt das Ergebnis
im Balken an - auch dann, wenn LCM bereits aktuell ist.

### Trivy für den CVE-Scan nachinstallieren

Der CVE-Scan braucht [Trivy](https://trivy.dev). Es liegt in **keiner**
Standard-Paketquelle von Debian oder Ubuntu und muss deshalb aus der
Hersteller-Quelle eingerichtet werden - LCM läuft ohne Trivy normal weiter,
der CVE-Scan bleibt dann aber deaktiviert:

```sh
# Voraussetzung - gnupg fehlt auf Minimal-/Cloud-Images
sudo apt-get install -y gnupg

wget -qO- https://get.trivy.dev/deb/public.key \
  | sudo gpg --dearmor -o /usr/share/keyrings/trivy.gpg
echo "deb [signed-by=/usr/share/keyrings/trivy.gpg] https://get.trivy.dev/deb generic main" \
  | sudo tee /etc/apt/sources.list.d/trivy.list
sudo apt update && sudo apt install trivy
```

Läuft LCM auf demselben Host, geht es auch per Klick: *Server-Detail des
LCM-Hosts → Trivy einrichten*.

| Pfad | Inhalt |
|------|--------|
| `/usr/bin/lcm` | die Programmdatei (Binary mit eingebettetem Web-UI) |
| `/lib/systemd/system/lcm.service` | die gehärtete systemd-Unit |
| `/etc/lcm/config.json` | Konfiguration (erstellt mit zufälligem JWT-Secret) |
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

```sh
make docker-build          # Linux-Binary bauen (inkl. Audits) + Image erzeugen
docker compose up -d
docker compose logs -f     # Erststart: hier steht das generierte Admin-Passwort
```

Beim ersten Start entstehen im Host-Ordner `./data` die Konfiguration, die
SQLite-Datenbank und `version.json`. Das Runtime-Image ist minimal gehärtet
(Alpine, non-root, `read-only`, `cap_drop: ALL`). Der Container spricht HTTP -
für öffentliche Deployments einen Reverse-Proxy mit TLS davorschalten.

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

Das Binary kennt nur wenige Flags - alles Weitere steht in der `config.json`:

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
(evtl. read-only gemountete) `config.json` anzufassen:

| Variable | Wirkung |
|----------|---------|
| `LCM_DATA` | Datenverzeichnis (wie `--data`). |
| `LCM_HOST` | Bind-Adresse der Weboberfläche/REST-API (überschreibt `host`). |
| `LCM_PORT` | Port der Weboberfläche/REST-API (überschreibt `port`). |
| `LCM_AGENT_HOST` | Bind-Adresse des Agent-Listeners (überschreibt `agent_host`). |
| `LCM_AGENT_PORT` | Port des Agent-Listeners (überschreibt `agent_port`); `0` schaltet ihn ab. |
| `LCM_BACKUP_PASSPHRASE` | Passphrase für **automatische** Backups (siehe [Backups](/guides/backups/)). |
| `LCM_RESTORE_AUTO_RESTART` | `1`/`true` = nach einem vorbereiteten Restore automatisch neu starten. |
| `TZ` | Zeitzone, z.&nbsp;B. `Europe/Berlin` - die tzdata sind ins Binary eingebettet, funktioniert also auch in minimalen Containern. |

```sh
LCM_HOST=0.0.0.0 LCM_PORT=443 ./lcm            # UI/REST an alle Interfaces, Port 443
LCM_AGENT_PORT=0 ./lcm                          # LCM Remote (Agent-Listener) abschalten
```

## Netzwerk-Ports

LCM bindet bis zu **drei** getrennte Listener - bewusst auf eigenen Ports:

| Port (Default) | Listener | Bind (Default) | Protokoll |
|---|---|---|---|
| `9310` | **Weboberfläche + REST-API** (`host`/`port`) | `127.0.0.1` | HTTPS (self-signed; `--dev` = HTTP) |
| `9320` | **Agent-Listener** - LCM Remote, ausschließlich `/mqtt` (`agent_host`/`agent_port`); `agent_port: 0` schaltet ihn ab | `0.0.0.0` | HTTPS (dasselbe Zertifikat wie die UI) |
| `9330` | **MCP-Listener** - optional, standardmäßig **aus**; an-/abschaltbar unter *Einstellungen → MCP* | `127.0.0.1` | HTTP |

Auf dem Agent-Port liegt **nur** die Agent-Schnittstelle, auf dem UI/REST-Port
**keine** - und umgekehrt. Details: [LCM Remote](/guides/remote/) und
[MCP-Schnittstelle](/guides/mcp/).

:::note[Jeder Neustart beendet alle Sitzungen]
Das JWT-Signaturmaterial wird bei jedem Start neu an ein zufälliges,
nur-im-RAM-lebendes Instanz-Nonce gebunden. Folge: Nach einem (Neu-)Start
sind **alle** zuvor ausgestellten Tokens ungültig - jeder muss sich neu
anmelden. Das gilt auch bei unverändertem `jwt_secret` und deckt u.&nbsp;a.
Rebuild, Prozess-Neustart und ein frisches Datenbank-Seeding ab.
:::

## Erste Anmeldung

Das initiale Admin-Passwort steht in der Konsolen-/Journal-Ausgabe des ersten
Starts:

```sh
journalctl -u lcm | grep -A3 'Admin-Zugang'   # bei der .deb-Installation
```

Dann `https://<host>:9310` im Browser öffnen (self-signed Zertifikat - die
Browser-Warnung ist erwartbar), als `admin` anmelden und das Passwort ändern.

Weiter mit dem [Schnellstart](/getting-started/quickstart/).
