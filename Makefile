# LCM - Build, Test & Maintenance
#
# Wichtigste Targets:
#   make build          Vollständiger, sicherheitsgeprüfter Build (aktuelle Plattform)
#   make dev            Backend (:9310) + Vite-Dev-Server (:5173) parallel
#   make test           Go-Tests (In-Memory SQLite)
#   make test-e2e       Playwright End-to-End-Tests gegen das echte Binary
#   make audit          npm audit + govulncheck
#   make update-deps    Go- und npm-Abhängigkeiten aktualisieren
#
# Versionierung:
#   Die Semantic Version pflegst du in der Datei VERSION (z.B. 1.2.0).
#   Die Build-Nummer (.buildnumber) zählt bei jedem Build-Target automatisch
#   hoch. Beides wird per -ldflags in internal/version injiziert.

BINARY      := lcm
BUILD_DIR   := bin
FRONTEND    := frontend

VERSION_PKG := LCM/internal/version
APP_VERSION := $(shell cat VERSION)
# "=" statt ":=": wird erst im Rezept ausgewertet - also NACH bump-build.
BUILD_NUM    = $(shell cat .buildnumber)
BUILT_AT     = $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
# Commit-SHA des Builds, mit "-dirty" bei uncommitteten Änderungen. Damit ist
# einem laufenden Binary immer anzusehen, aus welchem Quellstand es stammt -
# und ein lokaler Dev-Build ist nie mit einem Release verwechselbar.
GIT_DIRTY    = $(shell git status --porcelain 2>/dev/null | head -1)
GIT_COMMIT   = $(shell git rev-parse HEAD 2>/dev/null || echo unbekannt)$(if $(GIT_DIRTY),-dirty,)
GO_LDFLAGS   = -s -w \
	-X $(VERSION_PKG).Version=$(APP_VERSION) \
	-X $(VERSION_PKG).Build=$(BUILD_NUM) \
	-X $(VERSION_PKG).BuiltAt=$(BUILT_AT) \
	-X $(VERSION_PKG).Commit=$(GIT_COMMIT)

.PHONY: all build bump-build frontend-audit frontend-build go-vulncheck go-build \
        build-linux build-linux-arm64 build-windows build-macos build-macos-intel build-all \
        build-agent deb deb-amd64 deb-arm64 deb-agent deb-agent-amd64 deb-agent-arm64 \
        agent-packages \
        docker-build docker-build-full docker-build-trivyd docker-run \
        dev test test-e2e audit update-deps lint clean version next-version prepare-release

all: build

version:
	@echo "$(APP_VERSION) (Build $(BUILD_NUM))"

# Vorschau: Welche Version ergibt sich aus den Commits seit dem letzten
# Release-Tag, und wie sieht der Changelog-Abschnitt aus?
next-version:
	@go run ./tools/release

# Release vorbereiten: schreibt VERSION + CHANGELOG.md und committet das auf
# develop (danach: push + MR develop->main). Optional Version erzwingen:
#   make prepare-release VERSION_OVERRIDE=1.0.0
prepare-release:
	@./packaging/prepare-release.sh $(VERSION_OVERRIDE)

# Erhöht die Build-Nummer. Läuft pro make-Aufruf genau einmal, auch wenn
# mehrere Build-Targets davon abhängen (z.B. bei build-all).
bump-build:
	@echo $$(($$(cat .buildnumber 2>/dev/null || echo 0)+1)) > .buildnumber
	@echo "==> Version $(APP_VERSION), Build $$(cat .buildnumber)"

## ---- Build-Pipeline (Reihenfolge gemäß Spezifikation) ----------------------

# 1. Frontend Audit  2. Frontend Build  3. govulncheck  4. Go Build
build: frontend-audit frontend-build go-vulncheck go-build

frontend-audit:
	cd $(FRONTEND) && npm audit --audit-level=high

# --ignore-scripts: postinstall-Skripte sind der meistgenutzte Angriffsweg in
# npm. Details: docs/reference/dependencies.md
frontend-build:
	cd $(FRONTEND) && npm ci --no-audit --ignore-scripts && npm run build

go-vulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

go-build: bump-build
	mkdir -p $(BUILD_DIR)
	go build -trimpath -ldflags "$(GO_LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) ./cmd/app

## ---- Cross-Compiling (CGO-frei dank modernc.org/sqlite) --------------------

build-linux: frontend-audit frontend-build go-vulncheck bump-build
	mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(GO_LDFLAGS)" -o $(BUILD_DIR)/$(BINARY)-linux-amd64 ./cmd/app

build-linux-arm64: frontend-audit frontend-build go-vulncheck bump-build
	mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(GO_LDFLAGS)" -o $(BUILD_DIR)/$(BINARY)-linux-arm64 ./cmd/app

build-windows: frontend-audit frontend-build go-vulncheck bump-build
	mkdir -p $(BUILD_DIR)
	GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$(GO_LDFLAGS)" -o $(BUILD_DIR)/$(BINARY)-windows-amd64.exe ./cmd/app

build-macos: frontend-audit frontend-build go-vulncheck bump-build
	mkdir -p $(BUILD_DIR)
	GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "$(GO_LDFLAGS)" -o $(BUILD_DIR)/$(BINARY)-darwin-arm64 ./cmd/app

build-macos-intel: frontend-audit frontend-build go-vulncheck bump-build
	mkdir -p $(BUILD_DIR)
	GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags "$(GO_LDFLAGS)" -o $(BUILD_DIR)/$(BINARY)-darwin-amd64 ./cmd/app

build-all: build build-linux build-linux-arm64 build-windows build-macos build-macos-intel build-agent

# LCM Remote: lcm-agent (nur Linux - läuft auf den verwalteten Servern).
# Kein Frontend/Audit nötig, das Binary ist schlank (stdlib + paho + wire).
build-agent: bump-build
	mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(GO_LDFLAGS)" -o $(BUILD_DIR)/lcm-agent-linux-amd64 ./cmd/agent
	GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(GO_LDFLAGS)" -o $(BUILD_DIR)/lcm-agent-linux-arm64 ./cmd/agent

## ---- Debian-Pakete (.deb via nfpm) ------------------------------------------

# Erzeugt installierbare .deb-Pakete mit systemd-Dienst für Ubuntu/Debian.
# nfpm läuft plattformunabhängig (auch auf macOS) und packt die zuvor
# cross-kompilierten Linux-Binaries. Version = Inhalt der VERSION-Datei.
NFPM = go run github.com/goreleaser/nfpm/v2/cmd/nfpm@latest

# Das Server-Paket enthält zusätzlich beide Agent-Binaries
# (/usr/share/lcm/agent - der Server liefert sie per Download-Endpunkt aus).
deb-amd64: build-linux build-agent
	cp $(BUILD_DIR)/$(BINARY)-linux-amd64 $(BUILD_DIR)/lcm-pkg
	LCM_ARCH=amd64 LCM_VERSION=$(APP_VERSION) $(NFPM) package \
		--config packaging/nfpm.yaml --packager deb --target $(BUILD_DIR)/
	rm -f $(BUILD_DIR)/lcm-pkg

deb-arm64: build-linux-arm64 build-agent
	cp $(BUILD_DIR)/$(BINARY)-linux-arm64 $(BUILD_DIR)/lcm-pkg
	LCM_ARCH=arm64 LCM_VERSION=$(APP_VERSION) $(NFPM) package \
		--config packaging/nfpm.yaml --packager deb --target $(BUILD_DIR)/
	rm -f $(BUILD_DIR)/lcm-pkg

# LCM Remote: eigenständiges lcm-agent-Paket (für apt install lcm-agent).
deb-agent-amd64: build-agent
	cp $(BUILD_DIR)/lcm-agent-linux-amd64 $(BUILD_DIR)/lcm-agent-pkg
	LCM_ARCH=amd64 LCM_VERSION=$(APP_VERSION) $(NFPM) package \
		--config packaging/nfpm-agent.yaml --packager deb --target $(BUILD_DIR)/
	rm -f $(BUILD_DIR)/lcm-agent-pkg

deb-agent-arm64: build-agent
	cp $(BUILD_DIR)/lcm-agent-linux-arm64 $(BUILD_DIR)/lcm-agent-pkg
	LCM_ARCH=arm64 LCM_VERSION=$(APP_VERSION) $(NFPM) package \
		--config packaging/nfpm-agent.yaml --packager deb --target $(BUILD_DIR)/
	rm -f $(BUILD_DIR)/lcm-agent-pkg

deb-agent: deb-agent-amd64 deb-agent-arm64

# LCM Remote auf den uebrigen Distributionen: dasselbe Agent-Paket als RPM,
# APK und Arch-Paket. Der Agent ist ein statisches Go-Binary und sein
# postinstall reines POSIX-sh - es gibt also nichts Debian-Spezifisches, was
# den anderen Formaten im Weg staende. Nur ausgeliefert wurden sie bisher
# nicht, und damit war LCM Remote faktisch Debian-only.
#
# Getrennt vom deb-Ziel, weil nur die .deb in den apt-Kanal wandern; die
# uebrigen Formate haengen als Release-Asset am Tag.
agent-packages: build-agent
	@for A in amd64 arm64; do \
		cp $(BUILD_DIR)/lcm-agent-linux-$$A $(BUILD_DIR)/lcm-agent-pkg; \
		for P in rpm apk archlinux; do \
			LCM_ARCH=$$A LCM_VERSION=$(APP_VERSION) $(NFPM) package \
				--config packaging/nfpm-agent.yaml --packager $$P --target $(BUILD_DIR)/; \
		done; \
	done
	@rm -f $(BUILD_DIR)/lcm-agent-pkg
	@ls -lh $(BUILD_DIR)/lcm-agent-*.rpm $(BUILD_DIR)/lcm-agent*.apk $(BUILD_DIR)/lcm-agent*.pkg.tar.zst

deb: deb-amd64 deb-arm64 deb-agent
	@ls -lh $(BUILD_DIR)/*.deb

## ---- Docker (siehe docs/docker.md) -------------------------------------------

# Architektur des Docker-Hosts (amd64/arm64) - überschreibbar:
#   make docker-build DOCKER_ARCH=arm64
DOCKER_ARCH ?= $(shell uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')

# Baut erst das Linux-Binary auf dem Host (inkl. aller Audits, wie jeder
# andere Build), dann in Sekunden das Runtime-Image, das es nur kopiert.
docker-build: frontend-audit frontend-build go-vulncheck bump-build
	mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=$(DOCKER_ARCH) CGO_ENABLED=0 go build -trimpath -ldflags "$(GO_LDFLAGS)" \
		-o $(BUILD_DIR)/$(BINARY)-linux-$(DOCKER_ARCH) ./cmd/app
	docker build -f docker/Dockerfile -t lcm:$(APP_VERSION) -t lcm:latest .

# Trivy-Sidecar: das offizielle Trivy-Image plus unser trivyd-Binary. Im
# Container-Betrieb läuft Trivy dort statt im LCM-Image (das ist scratch -
# ohne Shell und ohne Trivy).
docker-build-trivyd:
	mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=$(DOCKER_ARCH) CGO_ENABLED=0 go build -trimpath -ldflags "$(GO_LDFLAGS)" \
		-o $(BUILD_DIR)/lcm-trivyd-linux-$(DOCKER_ARCH) ./cmd/trivyd
	docker build -f docker/Dockerfile.trivyd -t lcm-trivyd:$(APP_VERSION) -t lcm-trivyd:latest .

# Alternative ohne Host-Toolchain: kompletter Build im Container.
# Dieselbe Datei, andere Bauart (siehe Kopf des Dockerfile).
docker-build-full: bump-build
	docker build -f docker/Dockerfile --build-arg BIN_SOURCE=source -t lcm:$(APP_VERSION) -t lcm:latest .

# Startet den Container gehärtet mit ./data als Daten-Volume.
docker-run:
	docker run -d --name lcm -p 9310:9310 \
		-v "$$(pwd)/data:/data" \
		--read-only --security-opt no-new-privileges --cap-drop ALL \
		--restart unless-stopped lcm:latest

## ---- Entwicklung ------------------------------------------------------------

# Startet Go-Backend (Debug-Logs) und Vite-Dev-Server parallel.
# Vite proxied /api an :9310 - Frontend-Änderungen sind sofort sichtbar.
dev:
	@trap 'kill 0' INT TERM; \
	go run ./cmd/app -config ./config.dev.json -debug & \
	cd $(FRONTEND) && npm run dev & \
	wait

## ---- Tests & Qualität --------------------------------------------------------

test:
	go test ./...

# Browser explizit holen: frontend-build installiert ohne postinstall-Skripte.
test-e2e: frontend-build
	cd $(FRONTEND) && npx playwright install chromium && npx playwright test

audit: frontend-audit go-vulncheck

lint:
	@find . -name '*.go' -not -path './.cache/*' -exec gofmt -l {} + | (! grep .) \
		|| (echo "Nicht gofmt-formatiert (siehe oben) - 'gofmt -w .' ausführen." && false)
	go vet ./...
	cd $(FRONTEND) && npx svelte-check --no-tsconfig 2>/dev/null || true

## ---- Wartung -----------------------------------------------------------------

update-deps:
	go get -u ./...
	go mod tidy
	cd $(FRONTEND) && npm update

clean:
	rm -rf $(BUILD_DIR) $(FRONTEND)/dist $(FRONTEND)/e2e/.run
