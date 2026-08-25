# LCM - Runtime-Image auf Basis `scratch`.
#
# Das Ergebnis enthält genau vier Dinge: das Go-Binary, die CA-Zertifikate,
# eine passwd/group-Zeile und zwei leere Verzeichnisse. Keine Shell, kein
# Paketmanager, keine Bibliotheken - also auch nichts, was eigene
# Sicherheitslücken mitbringen könnte. Das Binary ist statisch (CGO_ENABLED=0),
# die Zeitzonendaten stecken darin (time/tzdata), und LCM startet auf seinem
# eigenen Host genau einen fremden Prozess: Trivy. Der gehört im
# Container-Betrieb in einen eigenen Container.
#
# Nebeneffekt, der Multi-Arch billig macht: Im Runtime-Abschnitt gibt es kein
# RUN. Ein Image für arm64 lässt sich damit auf einem amd64-Rechner bauen,
# OHNE QEMU-Emulation - es wird nur kopiert. Alle Abschnitte, die wirklich
# etwas ausführen, laufen ausdrücklich auf der Bau-Plattform.
#
# Zwei Bauarten, eine Datei (BIN_SOURCE):
#
#   prebuilt (Standard)  Das auf dem Host gebaute Binary aus bin/ wird
#                        kopiert. Sekundenschnell, und es ist exakt dasselbe
#                        Binary wie in .deb und Release.
#                          make docker-build        # baut Binary + Image
#
#   source               Frontend und Backend werden im Container gebaut -
#                        für Umgebungen ohne Go/Node-Toolchain.
#                          make docker-build-full
#                          docker build --build-arg BIN_SOURCE=source .
#
# BuildKit baut nur die Abschnitte, die für das Ziel gebraucht werden; die
# jeweils andere Bauart kostet also nichts. Betrieb und Details:
# docs/reference/packaging.md

# Muss VOR dem ersten FROM stehen, damit er in der FROM-Zeile gilt.
ARG BIN_SOURCE=prebuilt

## ---- Zutaten für das Scratch-Image -------------------------------------------
# Läuft auf der Bau-Plattform: CA-Zertifikate und leere Verzeichnisse sind
# nicht architekturabhängig. Damit bleibt der Multi-Arch-Build emulationsfrei.
FROM --platform=$BUILDPLATFORM alpine:3 AS prep
RUN apk add --no-cache ca-certificates \
    # /data nimmt config.json, Datenbank und Zertifikat auf; /tmp braucht LCM
    # für Zwischendateien (Backup-Archive, SBOM). In scratch lassen sich beide
    # nicht mit RUN anlegen - sie werden hier vorbereitet und kopiert.
 && mkdir -p /rootfs/data /rootfs/tmp \
    # Fester Benutzer 1000: der übliche erste Linux-Benutzer, damit
    # Bind-Mounts vom Host ohne chown beschreibbar sind. Ohne diese Zeilen
    # zeigen Werkzeuge im Container nur die nackte UID.
 && printf 'app:x:1000:1000:LCM:/nonexistent:/sbin/nologin\n' > /rootfs/etc-passwd \
 && printf 'app:x:1000:\n' > /rootfs/etc-group

## ---- Bauart „prebuilt": fertiges Binary aus bin/ -----------------------------
FROM scratch AS prebuilt
# TARGETARCH setzt Docker automatisch passend zur Zielplattform.
ARG TARGETARCH
COPY bin/lcm-linux-${TARGETARCH} /lcm

## ---- Bauart „source": alles im Container -------------------------------------
FROM --platform=$BUILDPLATFORM node:lts-alpine AS frontend-builder
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
# --ignore-scripts ist Absicht: postinstall-Skripte sind der meistgenutzte
# Angriffsweg in npm. Details: docs/reference/dependencies.md
RUN npm ci --no-audit --ignore-scripts
COPY frontend/ ./
# Sicherheits-Gate wie im Makefile: unsichere Pakete brechen den Build ab.
RUN npm audit --audit-level=high && npm run build

FROM --platform=$BUILDPLATFORM golang:1-alpine AS backend-builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend-builder /src/frontend/dist ./frontend/dist
# Sicherheits-Gate: bekannte Go-Schwachstellen brechen den Build ab.
RUN go run golang.org/x/vuln/cmd/govulncheck@latest ./...
# Cross-Compile statt Emulation: Go baut für die Zielarchitektur selbst.
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -trimpath \
    -ldflags "-s -w \
      -X LCM/internal/version.Version=$(cat VERSION) \
      -X LCM/internal/version.Build=$(cat .buildnumber) \
      -X LCM/internal/version.BuiltAt=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -o /lcm ./cmd/app

FROM scratch AS source
COPY --from=backend-builder /lcm /lcm

## ---- Auswahl der Bauart ------------------------------------------------------
# Nur der hier referenzierte Abschnitt wird gebaut.
FROM ${BIN_SOURCE} AS binary

## ---- Runtime -----------------------------------------------------------------
FROM scratch

# TLS für ausgehende Verbindungen (OSV, EUVD, Trivy-Datenbank, SMTP).
COPY --from=prep /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=prep /rootfs/etc-passwd /etc/passwd
COPY --from=prep /rootfs/etc-group /etc/group
# Legt /data und /tmp an - beschreibbar für den Dienstbenutzer.
COPY --from=prep --chown=1000:1000 /rootfs/data /data
COPY --from=prep --chown=1000:1000 /rootfs/tmp /tmp

COPY --from=binary /lcm /app/lcm

# /data ist der Mountpoint für config.json, app.db, Zertifikat und version.json.
VOLUME /data

# Im Container auf allen Interfaces lauschen (Port-Mapping), Daten in /data.
# TZ steuert die Zeitzone der Anwendung (Logs, Zeitstempel), z.B. Europe/Berlin
# - dank eingebetteter Zeitzonendaten ohne zusätzliches Paket.
ENV LCM_HOST=0.0.0.0 \
    LCM_DATA=/data \
    TZ=Etc/UTC
EXPOSE 9310

USER 1000:1000

# Das Binary prüft sich selbst. Vorher stand hier ein wget-Aufruf auf
# http://…, obwohl LCM standardmäßig HTTPS spricht - der Healthcheck konnte
# nie gelingen, und jeder Container galt dauerhaft als „unhealthy".
# Im Scratch-Image gäbe es ohnehin weder Shell noch wget.
HEALTHCHECK --interval=30s --timeout=10s --start-period=15s --retries=3 \
  CMD ["/app/lcm", "-healthcheck"]

# Das Binary behandelt SIGTERM selbst (Graceful Shutdown) und ist als
# PID 1 unproblematisch.
ENTRYPOINT ["/app/lcm"]
