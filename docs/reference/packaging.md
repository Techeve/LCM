---
sidebar:
  order: 25
title: Paketierung & Deployment
description: LCM als natives Debian-/Ubuntu-Paket (.deb) mit systemd-Dienst oder als gehärteter Docker-Container betreiben.
---

LCM lässt sich auf zwei Wegen betreiben: als natives **Debian-/Ubuntu-Paket
(.deb)**, das die Anwendung als unprivilegierten `systemd`-Dienst einrichtet,
oder als **Docker-Container**. Beide Wege liefern exakt dasselbe Binary; diese
Seite beschreibt beide.

## Debian-/Ubuntu-Paket (.deb)

LCM wird für **Ubuntu- und Debian-Server** als installierbares `.deb`-Paket
ausgeliefert - für **amd64** (x86-64) und **arm64** (aarch64). Das Paket
richtet LCM als unprivilegierten `systemd`-Dienst ein: ein Befehl, und der
Dienst läuft, startet beim Booten und liefert die Weboberfläche über HTTPS.

### Installation

Das passende Paket aus dem [Release](https://gitlab.techeve.de/techeve/lcm/-/releases?mtm_campaign=linking&mtm_kwd=doc)
laden (`lcm_<version>_amd64.deb` bzw. `..._arm64.deb`) und installieren:

```sh
sudo apt install ./lcm_<version>_amd64.deb
```

`apt` zieht die Abhängigkeit (`adduser`) automatisch nach. Danach ist der
Dienst installiert, aktiviert (Autostart) und gestartet.

**`trivy`** wird für den CVE-Scan des Paketbestands gebraucht, ist aber
bewusst **keine** Paket-Abhängigkeit: es liegt in keiner Standard-Paketquelle
von Debian/Ubuntu, sodass `apt` ein `Recommends` stillschweigend nicht
auflösen könnte. Die Einrichtung ist deshalb ein eigener Schritt (siehe
[Installation](/getting-started/installation/)); fehlt Trivy, deaktiviert sich
nur der CVE-Scan, alles andere läuft normal.

Die Architektur ermittelst du mit `dpkg --print-architecture`
(`amd64` oder `arm64`).

### Was das Paket einrichtet

| Pfad | Inhalt |
|------|--------|
| `/usr/bin/lcm` | die Programmdatei (einzelne Binary, Web-UI eingebettet) |
| `/lib/systemd/system/lcm.service` | die gehärtete systemd-Unit |
| `/etc/lcm/config.json` | Konfiguration (beim ersten Mal mit zufälligem JWT-Secret erzeugt) |
| `/var/lib/lcm/` | Zustand: verschlüsselte SQLite-DB, Master-Key (`lcm.key`), TLS-Zertifikat, Backups |

Der Dienst läuft unter dem eigens angelegten System-Benutzer **`lcm`** ohne
root-Rechte - LCM verwaltet *andere* Server per SSH und braucht auf dem
eigenen Host keine erhöhten Rechte.

### Erste Anmeldung

Das initiale Admin-Passwort wird beim ersten Start **einmalig ins Journal**
geschrieben:

```sh
journalctl -u lcm | grep -A3 'Admin-Zugang'
```

Dann im Browser öffnen: `https://<server-ip>:9310` (self-signed Zertifikat -
die Warnung des Browsers ist erwartbar). Admin-Login, Passwort ändern, los.

### Dienst verwalten

```sh
systemctl status lcm      # Zustand
systemctl restart lcm     # neu starten
journalctl -u lcm -f      # Logs live
```

### Konfiguration

Alle Einstellungen stehen in `/etc/lcm/config.json` - nach Änderungen den
Dienst neu starten (`systemctl restart lcm`). Wichtige Werte:

- `host` (Default `0.0.0.0`): Bind-Adresse. **Sicherheitshinweis:** In der
  Voreinstellung ist die Oberfläche im Netz erreichbar (HTTPS, self-signed).
  Für den Produktivbetrieb den Zugriff per Firewall einschränken und/oder
  einen Reverse-Proxy mit gültigem Zertifikat davorschalten. Für rein lokalen
  Betrieb `host` auf `127.0.0.1` setzen.
- `port` (Default `9310`).
- `jwt_secret`: signiert die Sitzungen - **nicht** ändern (sonst werden alle
  Anmeldungen ungültig) und **nicht** weitergeben.

### Aktualisieren

Neues Paket über das alte installieren:

```sh
sudo apt install ./lcm_<neue-version>_amd64.deb
```

Konfiguration, Datenbank und das JWT-Secret bleiben erhalten; ausstehende
Datenbank-Migrationen laufen beim Start automatisch.

### Entfernen

```sh
sudo apt remove lcm     # Dienst + Programm entfernen, Daten behalten
sudo apt purge  lcm     # zusätzlich /etc/lcm, /var/lib/lcm und den User lcm löschen
```

:::caution
`purge` löscht die verschlüsselte Datenbank **und** den Master-Key
unwiderruflich. Vorher ggf. `/var/lib/lcm` sichern.
:::

### Selbst bauen

Lokal (auch auf macOS - nfpm ist plattformunabhängig, die Binaries werden
cross-kompiliert):

```sh
make deb            # baut bin/lcm_<version>_amd64.deb und ..._arm64.deb
```

In der CI erzeugt der Job `packages:deb` die Pakete aus den Binaries; beim
Release auf `main` werden sie automatisch als Release-Assets angehängt.

### Sprache der Ausgaben

**Englisch ist der Standard.** Deutsch erscheint nur, wenn das System
tatsächlich auf Deutsch eingestellt ist. Ausgewertet wird in POSIX-Reihenfolge
`LC_ALL` → `LC_MESSAGES` → `LANG`; ist die Umgebung leer - was bei `dpkg`
regelmäßig vorkommt - greifen die Paketskripte zusätzlich auf die systemweite
Einstellung in `/etc/default/locale` bzw. `/etc/locale.conf` zurück.

| Ausgabe | Sprache |
|---|---|
| Installation (`apt install lcm`) | EN, bei deutschem System DE |
| Konsole beim Dienststart (Admin-Passwort, Master-Key, Config) | EN, bei deutschem System DE |
| `lcm-agent`-Kommandozeile | EN, bei deutschem System DE |
| `systemctl status` (Unit-`Description=`) | immer EN - eine Unit-Datei ist statisch und kann der Systemsprache nicht folgen |
| `journalctl -u lcm` (Logmeldungen) | immer EN - siehe unten |

Die **Journal-Logmeldungen sind bewusst einheitlich englisch**: Sie werden im
Support-Fall weitergegeben, und eine je nach Kundensystem wechselnde Sprache
würde die Auswertung unnötig erschweren. Die Weboberfläche ist davon nicht
betroffen - sie bleibt vollständig zweisprachig (DE/EN, umschaltbar).

:::note[Warum alle Ausgaben umlautfrei sind]
Paket- und Dienstausgaben landen in Terminals, Logdateien, Journal-Exporten
und CI-Protokollen, deren Zeichenkodierung LCM nicht kontrolliert. Umlaute
werden dort regelmäßig zu unleserlichem Zeichensalat (`Weboberflächeâ€œ`).
Deutsche Ausgaben verwenden deshalb durchgängig **ue/ae/oe/ss** - im Quelltext
dürfen die Texte normal geschrieben werden, die Umwandlung übernimmt
`internal/i18n` zentral. Die Weboberfläche ist ausgenommen: Sie liefert UTF-8
über HTTP aus und zeigt Umlaute korrekt an.
:::

Erzwingen lässt sich die Sprache jederzeit über die Umgebung:

```sh
LC_ALL=en_US.UTF-8 apt install lcm    # englische Installationsausgabe
LC_ALL=de_DE.UTF-8 lcm-agent          # deutsche Kommandozeilen-Hilfe
```

## Docker

Alles rund um die Container liegt unter `docker/` - zwei Dockerfiles und eine `docker-compose.yml` als Beispiel:

| Datei | Inhalt |
|---|---|
| `docker/Dockerfile` | Das LCM-Runtime-Image auf Basis **`scratch`** - rund 37 MB |
| `docker/Dockerfile.trivyd` | Der **Trivy-Sidecar**: das offizielle Trivy-Image plus ein kleines Binary von uns |

### Warum `scratch`

Das Runtime-Image enthält genau vier Dinge: das Go-Binary, die CA-Zertifikate, eine `passwd`/`group`-Zeile und zwei leere Verzeichnisse. Keine Shell, kein Paketmanager, keine Bibliotheken - also auch nichts, was eigene Sicherheitslücken mitbringen könnte. Das Binary ist statisch (`CGO_ENABLED=0`), die Zeitzonendaten stecken darin.

Ein Nebeneffekt macht Multi-Arch billig: Im Runtime-Abschnitt gibt es kein `RUN`. Ein arm64-Image lässt sich damit auf einem amd64-Rechner bauen, **ohne QEMU-Emulation** - es wird nur kopiert.

### Zwei Bauarten, eine Datei

`docker/Dockerfile` kennt den Build-Arg `BIN_SOURCE`:

| Wert | Ansatz | Wann verwenden |
|---|---|---|
| `prebuilt` (Standard) | Kopiert das **fertig auf dem Host gebaute** Linux-Binary aus `bin/` | Normalfall: Image-Build in Sekunden, keine Toolchain im Build-Kontext, exakt dasselbe Binary wie bei allen anderen Deployment-Wegen |
| `source` | Baut Frontend + Backend **komplett im Container** (Node LTS → Go 1.x, mit `npm audit` und `govulncheck` als Gates) | Umgebungen ohne Go/Node auf dem Host |

```sh
make docker-build          # prebuilt (Standard)
make docker-build-full     # entspricht --build-arg BIN_SOURCE=source
```

BuildKit baut nur die Abschnitte, die für das gewählte Ziel gebraucht werden - die jeweils andere Bauart kostet nichts.

### Trivy im Container-Betrieb

Der CVE-Scan braucht Trivy. Im Scratch-Image gibt es keines - und dort hineinzupacken hieße, sich einen Paketmanager und ein zweites Angriffsziel zurückzuholen. Trivy läuft deshalb in einem **eigenen Container** (`docker/Dockerfile.trivyd`), und LCM spricht ihn über HTTP an.

Der Container ist dabei zugleich die Abschottung: Er enthält weder LCMs Datenbank noch den Master-Key - es gibt schlicht nichts zu erreichen. Auf einer Host-Installation übernimmt das bubblewrap bzw. Landlock; im Container wäre das zusätzlich nur Theater, denn es bräuchte Rechte, die ein gehärteter Container gerade nicht hat. Die Oberfläche meldet den Zustand als `container` statt „ohne Sandbox" - beides wäre sonst irreführend.

```sh
cp docker/.env.example docker/.env
sed -i "s/BITTE-ERSETZEN/$(openssl rand -hex 32)/" docker/.env
make docker-build docker-build-trivyd
docker compose -f docker/docker-compose.yml up -d
```

Das Token schützt den Sidecar; er führt Prozesse aus und lädt Images aus fremden Registries. **Ohne Token startet er nicht** - und Compose bricht ebenfalls ab, statt still ohne Schutz zu laufen. Der Port des Sidecars wird bewusst **nicht** veröffentlicht: Er ist nur im Container-Netz erreichbar.

#### Der Sidecar trägt die Trivy-Version, nicht die von LCM

Veröffentlicht wird er als `…/trivyd:<trivy-version>` (heute `0.74.0`) plus dem
beweglichen Kanal-Zeiger (`:beta`, `:latest`). Bewusst **nicht** mit der
LCM-Version: Er ändert sich nur, wenn Trivy hochgezogen oder der Adapter
angefasst wird. Mit der LCM-Version im Tag entstünde bei jedem Release ein
neuer Digest für unveränderten Inhalt - die Registry füllte sich mit Kopien,
und die Tag-Liste sähe aus, als hätte sich am Scanner etwas bewegt.

Für die eigene Compose-Datei heißt das: den Sidecar über den Kanal-Zeiger
beziehen (`:beta` bzw. `:latest`) oder die Trivy-Version ausdrücklich nennen.

Ohne Sidecar läuft LCM weiterhin - dann eben ohne CVE-Scan, genau wie bisher im Docker-Betrieb. Ein **konfigurierter, aber unerreichbarer** Sidecar ist dagegen ein Fehler und wird als solcher gemeldet: Sonst sähe ein Ausfall aus wie ein abgeschalteter Scan, und eine leere Fundliste wie Entwarnung.

### Was der Container NICHT kann

`localhost` ist im Container der Container selbst, nicht die Maschine darunter. Deshalb:

- LCM nimmt sich dort **nicht** selbst als verwalteten Server auf.
- Der Join-Wizard lehnt `localhost`/`127.0.0.1` ab und verweist auf die Netzwerk-Adresse des Docker-Hosts.
- Die Einrichtungs-Aktionen der LCM-Host-Karte (Trivy, Sandbox, apt-cacher-ng, CrowdSec-LAPI) werden nicht angeboten - sie richten etwas auf einem Host mit `apt` und systemd ein.
- Aktualisiert wird über ein **neues Image**, nicht über den apt-Kanal.

### Schnellstart mit Docker Compose (empfohlen)

```sh
make docker-build          # Linux-Binary bauen (inkl. Audits) + Image erzeugen
docker compose -f docker/docker-compose.yml up -d
docker compose -f docker/docker-compose.yml logs -f     # Erststart: hier steht das generierte Admin-Passwort!
```

`make docker-build` erledigt beides in einem Schritt: erst der normale, sicherheitsgeprüfte Build (npm audit → Vite → govulncheck → Cross-Compile für Linux, Architektur wird automatisch erkannt, überschreibbar mit `DOCKER_ARCH=arm64`), dann der Sekunden-schnelle Image-Build, der das Binary nur noch hineinkopiert.

Danach läuft die App auf <http://localhost:9310>. Beim ersten Start entstehen im Host-Ordner `./data`:

```
data/
├── config.json    Konfiguration (inkl. generiertem JWT-Secret)
├── app.db         SQLite-Datenbank (+ -wal/-shm im Betrieb)
└── version.json   installierte Version (Update-Erkennung)
```

Alle drei Dateien liegen durch den Bind-Mount **auf dem Host** - einsehen, sichern, anpassen (nach Änderungen an der config.json: `docker compose -f docker/docker-compose.yml restart`).

Stoppen/Updaten:

```sh
docker compose -f docker/docker-compose.yml down                    # stoppen (Daten bleiben in ./data)
make docker-build                      # neue Version bauen
docker compose -f docker/docker-compose.yml up -d                   # starten
docker compose -f docker/docker-compose.yml logs | grep update      # -> "update erkannt - von=… auf=…"
```

Beim Start einer neuen Version greift automatisch das [Update-Migrationssystem](/reference/database/): Der Container vergleicht `data/version.json` mit seiner Binary-Version und führt ausstehende Migrationsskripte aus.

### Nur mit dem Docker-Befehl (ohne Compose)

Image bauen - Standard-Weg (Binary auf dem Host bauen, dann kopieren):

```sh
make build-linux                # bzw. make build-linux-arm64 auf ARM-Hosts
docker build -t lcm .
```

Oder komplett im Container (ohne Go/Node auf dem Host):

```sh
docker build --build-arg BIN_SOURCE=source -t lcm .
```

Container starten - mit denselben Härtungs-Optionen wie im Compose-Beispiel:

```sh
docker run -d \
  --name lcm \
  -p 9310:9310 \
  -v "$(pwd)/data:/data" \
  --read-only \
  --security-opt no-new-privileges \
  --cap-drop ALL \
  --restart unless-stopped \
  lcm
```

Admin-Passwort des Erststarts anzeigen, stoppen, entfernen:

```sh
docker logs lcm | grep -A3 Seeding
docker stop lcm
docker rm lcm          # ./data bleibt erhalten
```

Minimalvariante (nur zum Ausprobieren, ohne Härtung und ohne persistente Daten):

```sh
docker run --rm -p 9310:9310 lcm
```

### Wie das Image aufgebaut ist

**Bauart `prebuilt` (Standard):** Das auf dem Host gebaute Linux-Binary wird kopiert (`bin/lcm-linux-<arch>`; `ARG TARGETARCH` wählt automatisch das zur Zielplattform passende). Sämtliche Sicherheits-Gates (npm audit, govulncheck) und die Versions-Injektion laufen dabei im Makefile-Build auf dem Host.

**Bauart `source`:** Das Frontend entsteht im Node-LTS-Abschnitt (`npm ci` + `npm audit`), das Go-Binary im Go-Abschnitt (`golang:1-alpine` = jeweils neuestes stabiles Go 1.x, mit `govulncheck` und `CGO_ENABLED=0`). Beide laufen ausdrücklich auf der Bau-Plattform und cross-kompilieren - deshalb braucht auch ein arm64-Image keine Emulation.

**Das Runtime-Image ist in beiden Fällen dasselbe:** `FROM scratch`, hinein kommen nur Binary, CA-Zertifikate, eine `passwd`/`group`-Zeile und die leeren Verzeichnisse `/data` und `/tmp`. Build-Werkzeuge und Quellcode sind **nicht** enthalten - und darüber hinaus auch keine Shell, kein Paketmanager und keine Bibliotheken.

### Sicherheits-Härtung im Detail

Im **Image**:

| Maßnahme | Wirkung |
|---|---|
| `FROM scratch` | keine Shell, kein Paketmanager, keine Bibliotheken - nichts, was eigene Lücken mitbringt oder einem Angreifer als Werkzeug dient |
| Nur `ca-certificates` mitgegeben | TLS nach außen funktioniert, mehr wird nicht gebraucht |
| `USER 1000:1000` | Prozess läuft nie als root |
| `CGO_ENABLED=0` | statisches Binary, keine libc-Schwachstellen |
| `npm audit` + `govulncheck` im Build | verwundbare Abhängigkeiten brechen den Build ab (auf dem Host via Makefile bzw. in der Bauart `source`) |
| `HEALTHCHECK` über das Binary selbst | Orchestrierung erkennt hängende Container - und die Prüfung spricht HTTPS, wie der Dienst selbst |

Zur **Laufzeit** (Compose/`docker run`-Flags):

| Maßnahme | Wirkung |
|---|---|
| `read_only: true` | Root-Dateisystem unveränderlich; beschreibbar ist nur das `/data`-Volume |
| `no-new-privileges` | keine Privilegien-Eskalation (setuid & Co.) |
| `cap_drop: ALL` | sämtliche Linux-Capabilities entfernt - der Service braucht keine |
| Port-Mapping statt `--network host` | Container sieht nur seinen eigenen Netzwerk-Namespace |

**TLS:** Der Container spricht HTTP. Für öffentliche Deployments einen Reverse-Proxy (Caddy, Traefik, nginx) davorschalten, der TLS terminiert - typischerweise als weiterer Compose-Service.

### Berechtigungen des Daten-Volumes

Der Container schreibt als **UID 1000** (der übliche erste Linux-User). Auf den meisten Hosts funktioniert der Bind-Mount `./data:/data` damit direkt. Hat dein Host-User eine andere UID, dem Container-User das Verzeichnis geben:

```sh
mkdir -p data && sudo chown 1000 data
```

Alternativ ein Named Volume verwenden (Docker verwaltet die Rechte selbst, dafür liegen die Dateien nicht direkt im Projektordner):

```yaml
volumes:
  - lcm-data:/data
# ...
volumes:
  lcm-data:
```

### Konfiguration im Container

Die normale Konfiguration passiert über `data/config.json` (entsteht beim Erststart). Umgebungsvariablen erlauben Container-typische Overrides, ohne die Datei zu ändern:

| Variable | Bedeutung | Default im Image |
|---|---|---|
| `TZ` | Zeitzone der Anwendung (Logs, Zeitstempel), z.B. `Europe/Berlin` | `Etc/UTC` |
| `LCM_HOST` | Listen-Adresse | `0.0.0.0` (nötig fürs Port-Mapping) |
| `LCM_PORT` | Listen-Port | Wert aus config.json (9310) |
| `LCM_DATA` | Datenverzeichnis | `/data` |

Beispiel (Compose):

```yaml
environment:
  TZ: Europe/Berlin
  LCM_PORT: "9000"
ports:
  - "9000:9000"
```

**Zur Zeitzone:** Normalerweise braucht ein Alpine-Container dafür das `tzdata`-Paket - dieses Template bettet die IANA-Zeitzonendaten stattdessen direkt ins Go-Binary ein (`import _ "time/tzdata"` in `cmd/app/main.go`). `TZ` funktioniert dadurch in jedem noch so minimalen Container und auch unter Windows, ganz ohne Zusatzpakete.

Das `-debug`-Flag funktioniert auch im Container: `docker run … lcm -debug` (Argumente nach dem Image-Namen gehen an das Binary, dank `ENTRYPOINT`).
