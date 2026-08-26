# LCM - Linux Centralized Management

<!-- Diese Datei ist die Quelle der Overview-Seite auf Docker Hub. Nach
     Änderungen dort neu einfügen - die Hub-API erlaubt Beschreibungs-Pflege
     nur mit Konto-Passwort, nicht mit Access-Tokens; automatisch geht es
     für persönliche Konten also nicht. -->

**Einmal installieren, beliebig viele Linux-Server über SSH verwalten.** LCM ist
ein zentrales Management-Werkzeug: Monitoring (Pakete, Updates, Hardware,
Ampel-Status je Server), CVE-Scan über Trivy ohne Agenten, Docker-Überwachung
samt Update-Aktionen, Benutzer- und Rechteverwaltung mit Berechtigungsprofilen,
Automatisierung über Gruppen-Regeln und Zeitpläne - und ein lückenloses Audit-Log
mit manipulationssicherer Hash-Kette. Backend in Go, Oberfläche in Svelte,
alles in **einem Binary**; die Zielserver brauchen nichts außer SSH.

Dieses Image ist das offizielle Container-Release der **Community Edition**
(AGPL-3.0). `:latest` ist das aktuelle stabile Release, `:beta` die
Vorabversion, dazu jede Version als eigenes Tag. Multi-Arch: amd64 und arm64.

## Schnellstart (ohne Compose)

```sh
mkdir -p data && sudo chown 1000 data   # der Container schreibt als UID 1000
docker run -d --name lcm \
  -p 9310:9310 \
  -v "$PWD/data:/data" \
  --read-only --tmpfs /tmp --cap-drop ALL --security-opt no-new-privileges:true \
  --restart unless-stopped \
  techeve/lcm:latest
docker logs -f lcm   # Erststart: hier steht das generierte Admin-Passwort
```

Danach: `https://<host>:9310` - LCM spricht standardmäßig **HTTPS** mit einem
selbstsignierten Zertifikat (die Browser-Warnung beim ersten Aufruf ist
erwartbar). Für öffentliche Deployments gehört ein Reverse-Proxy mit echtem
Zertifikat davor.

## Volumes

| Pfad | Zweck |
|---|---|
| `/data` | Alles Persistente: `config.json`, SQLite-Datenbank, Master-Key (`lcm.key`), TLS-Zertifikat, Backups, Logs, `version.json`. **Ohne diesen Mount ist nach `docker rm` alles weg - inklusive des Schlüssels, mit dem die Server-Zugänge verschlüsselt sind.** |
| `/tmp` | Zwischendateien (Backup-Wiederherstellung, SBOM für den CVE-Scan). Als `tmpfs` mounten, wenn der Container mit `--read-only` läuft. |

## Umgebungsvariablen

Alle optional - sie übersteuern die `config.json` aus `/data`, ohne sie zu ändern:

| Variable | Standard (Image) | Zweck |
|---|---|---|
| `LCM_HOST` | `0.0.0.0` | Bind-Adresse der Weboberfläche |
| `LCM_PORT` | `9310` | Port der Weboberfläche |
| `LCM_DATA` | `/data` | Datenverzeichnis |
| `LCM_AGENT_HOST` | wie `LCM_HOST` | Bind-Adresse des Agent-Listeners (LCM Remote) |
| `LCM_AGENT_PORT` | `9320` | Port des Agent-Listeners; `0` schaltet ihn ab. Nur veröffentlichen (`-p 9320:9320`), wenn Server über den LCM-Agenten statt SSH angebunden werden. |
| `LCM_TRIVY_URL` | - | URL des [lcm-trivyd-Sidecars](https://hub.docker.com/r/techeve/lcm-trivyd), z. B. `http://trivy:9330`. Ohne diese Variable sucht LCM ein lokales Trivy-Binary - im Scratch-Image gibt es keines, der CVE-Scan bleibt dann einfach aus. |
| `LCM_TRIVY_TOKEN` | - | Gemeinsames Token für den Sidecar (siehe unten) |
| `TZ` | `Etc/UTC` | Zeitzone für Logs und Zeitstempel, z. B. `Europe/Berlin` |

## Docker Compose (empfohlen, mit CVE-Scan)

Der CVE-Scanner läuft als eigener Begleitcontainer
[`techeve/lcm-trivyd`](https://hub.docker.com/r/techeve/lcm-trivyd): Das
LCM-Image ist ein Scratch-Image ohne Shell und ohne Trivy - der Sidecar hält
beides aus dem Anwendungscontainer heraus.

```yaml
# docker-compose.yml
services:
  lcm:
    image: techeve/lcm:latest
    container_name: lcm
    ports:
      - "9310:9310"
    volumes:
      - ./data:/data          # vorher: mkdir -p data && sudo chown 1000 data
    environment:
      TZ: Europe/Berlin
      LCM_TRIVY_URL: http://trivy:9330
      LCM_TRIVY_TOKEN: ${LCM_TRIVY_TOKEN:?bitte in .env setzen}
    depends_on:
      trivy:
        condition: service_healthy
    restart: unless-stopped
    # Härtung - der Service braucht nichts davon:
    read_only: true
    tmpfs: [/tmp]
    security_opt: [no-new-privileges:true]
    cap_drop: [ALL]

  trivy:
    image: techeve/lcm-trivyd:latest
    container_name: lcm-trivy
    # BEWUSST kein ports:-Eintrag - der Dienst gehört nur ins interne Netz.
    expose:
      - "9330"
    environment:
      LCM_TRIVY_TOKEN: ${LCM_TRIVY_TOKEN:?bitte in .env setzen}
      TZ: Europe/Berlin
    volumes:
      - trivy-cache:/cache    # Schwachstellen-DB (mehrere hundert MB) überlebt Neustarts
    restart: unless-stopped
    read_only: true
    tmpfs: [/tmp]
    security_opt: [no-new-privileges:true]
    cap_drop: [ALL]

volumes:
  trivy-cache:
```

```sh
mkdir -p data && sudo chown 1000 data
echo "LCM_TRIVY_TOKEN=$(openssl rand -hex 32)" > .env
docker compose up -d
docker compose logs -f lcm   # Erststart: Admin-Passwort
```

Die ausführlich kommentierte Vorlage liegt im Repo:
[`docker/docker-compose.yml`](https://gitlab.techeve.de/techeve/lcm-ce/-/blob/community/docker/docker-compose.yml).

## Enterprise

Neben der Community Edition gibt es **LCM Enterprise**: ein konservativ
gepflegter Wartungszweig mit **schnellen Sicherheitsupdates**, bewusst
**längeren Zyklen für Funktionsänderungen** - und **Support**. Anfragen über
[techeve.de](https://techeve.de).

## Links

- Produktseite: [techeve.de/lcm](https://techeve.de/lcm) · Firma: [techeve.de](https://techeve.de)
- Dokumentation: [doc.techeve.de/lcm/](https://doc.techeve.de/lcm/)
- Quellcode: [gitlab.techeve.de/techeve/lcm-ce](https://gitlab.techeve.de/techeve/lcm-ce) · Spiegel: [github.com/Techeve/LCM](https://github.com/Techeve/LCM)
- Lizenz: AGPL-3.0
