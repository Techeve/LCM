# lcm-trivyd - Trivy-Sidecar für LCM

<!-- Diese Datei ist die Quelle der Overview-Seite auf Docker Hub. Nach
     Änderungen dort neu einfügen - die Hub-API erlaubt Beschreibungs-Pflege
     nur mit Konto-Passwort, nicht mit Access-Tokens; automatisch geht es
     für persönliche Konten also nicht. -->

Der CVE-Scanner-Begleitcontainer für [**LCM - Linux Centralized
Management**](https://hub.docker.com/r/techeve/lcm). Er kapselt
[Trivy](https://trivy.dev) als kleinen, token-gesicherten HTTP-Dienst
(Port 9330), gegen den LCM seine SBOM-basierten Schwachstellen-Scans fährt.

Warum ein eigener Container: Das LCM-Image ist ein Scratch-Image ohne Shell
und ohne Trivy-Binary - der Sidecar hält beides aus dem Anwendungscontainer
heraus und enthält selbst weder LCMs Datenbank noch den Master-Key. Das Tag
trägt die **Trivy-Version** (nicht die LCM-Version); `:latest` gehört zum
stabilen LCM-Release, `:beta` zur Vorabversion.

**Allein ist dieses Image nicht nutzbar** - es beantwortet nur
token-authentifizierte Anfragen von LCM.

## Umgebungsvariablen und Volumes

| | | |
|---|---|---|
| `LCM_TRIVY_TOKEN` | **erforderlich** | Gemeinsames Token; ohne startet der Dienst nicht. Dasselbe Token gehört in den LCM-Container. |
| `TZ` | optional | Zeitzone für Logs, z. B. `Europe/Berlin` |
| `/cache` (Volume) | empfohlen | Die Schwachstellen-Datenbank ist mehrere hundert MB groß - ohne Volume lädt der Sidecar sie bei jedem Start neu. |

Den Port 9330 **nicht** auf den Host veröffentlichen - der Dienst gehört nur
ins interne Container-Netz (`expose`, nicht `ports`).

## Docker Compose (LCM + Sidecar komplett)

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
    read_only: true
    tmpfs: [/tmp]
    security_opt: [no-new-privileges:true]
    cap_drop: [ALL]

  trivy:
    image: techeve/lcm-trivyd:latest
    container_name: lcm-trivy
    expose:
      - "9330"
    environment:
      LCM_TRIVY_TOKEN: ${LCM_TRIVY_TOKEN:?bitte in .env setzen}
      TZ: Europe/Berlin
    volumes:
      - trivy-cache:/cache
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
```

Alle Details (Variablen, Mounts, Betrieb ohne Compose):
[techeve/lcm auf Docker Hub](https://hub.docker.com/r/techeve/lcm) und die
kommentierte Vorlage
[`docker/docker-compose.yml`](https://gitlab.techeve.de/techeve/lcm/-/blob/community/docker/docker-compose.yml).

## Enterprise

Neben der Community Edition gibt es **LCM Enterprise**: ein konservativ
gepflegter Wartungszweig mit **schnellen Sicherheitsupdates**, bewusst
**längeren Zyklen für Funktionsänderungen** - und **Support**. Anfragen über
[techeve.de](https://techeve.de).

## Links

- Produktseite: [techeve.de/lcm](https://techeve.de/lcm) · Firma: [techeve.de](https://techeve.de)
- Dokumentation: [doc.techeve.de/lcm/](https://doc.techeve.de/lcm/)
- Quellcode: [gitlab.techeve.de/techeve/lcm](https://gitlab.techeve.de/techeve/lcm) · Spiegel: [github.com/Techeve/LCM](https://github.com/Techeve/LCM)
- Lizenz: AGPL-3.0
