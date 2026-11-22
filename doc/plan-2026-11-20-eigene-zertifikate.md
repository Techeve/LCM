# LCM: eigene Zertifikate, Netzbereiche und ACME für UI, Agent und MCP

Branch `feat/eigene-zertifikate` von `develop` (`~/dev/LCM/LCM`, GitLab `techeve/lcm-intern`). Bei Umsetzung kommt dieser Plan als `doc/plan-2026-11-20-eigene-zertifikate.md` ins Repo, mit Status je Paket (offen / in Arbeit / zu prüfen / fertig).

## Context

Wer LCM ohne Reverse-Proxy betreibt, kann heute kein eigenes Zertifikat setzen, keins erneuern lassen und kein Let's Encrypt nutzen. Der Stand im Code:

- LCM hat eine **eigene CA** (20 Jahre) und stellt daraus ein 90-Tage-Serverzertifikat aus, das es alle ~30 Tage selbst erneuert (`internal/infrastructure/tlsx/`, `internal/core/services/cert_renewal.go`). Das Zertifikat nennt nur `localhost`/`127.0.0.1`, deshalb warnt jeder Browser, der LCM über einen echten Namen aufruft.
- Eigene Zertifikate gibt es nicht. Die Felder `TLSCertPEM`/`TLSKeyPEMEnc` in `domain/settings.go:283` sind tot, der Kommentar in `cmd/app/main.go:1086` ist veraltet.
- UI (9310) und Agent (9320) teilen sich einen `CertStore`, der das Zertifikat **bei jedem Handshake** holt (`tlsx/renew.go:140`). Austausch ohne Neustart ist also angelegt. MCP (9330) läuft als reines HTTP und nur auf Loopback.
- Agenten prüfen zuerst per Pin (Zertifikat, Schlüssel oder LCM-CA, ohne Namensprüfung), danach gegen die Systemroots **mit** Namensprüfung (`internal/agent/peer.go:35`).
- Netzbereiche gibt es nur für die UI (`allowed_ips` in der config.json, `middlewares/ipallowlist.go`). Sie werden beim Start gesetzt und sind in der Oberfläche nicht einstellbar. Agent und MCP haben keinen Filter.
- Es gibt keine Steuerung des laufenden Prozesses und keine ACME-Bibliothek.

## Entschieden (20./21.11.2026)

1. Port 80/443 nur **optional**: Ports in der config.json, die Capability `CAP_NET_BIND_SERVICE` gibt der Betreiber per Drop-in frei (`systemctl edit lcm`). Die gehärtete Unit bleibt.
2. MCP bekommt **optional TLS**. Nur mit TLS darf MCP außerhalb von Loopback binden.
3. Zertifikate werden **nach Namen (SNI)** ausgewählt, das LCM-CA-Zertifikat bleibt Rückfall. Bestehende Agenten reißen nicht ab.
4. **Steuerbuchse** `<data>/control.sock` für `lcm tls …` ist freigegeben (Tony, 21.11.).
5. **Netzbereiche je Endpunkt** (UI, Agent, MCP) sind in der Web-UI einstellbar. Die ACME-Challenge bleibt immer erreichbar.
6. Beide Betriebsarten müssen in der Konfiguration gehen: **mehrere Ports mit einem Namen** (ein Zertifikat) und **alles auf 443 mit drei Namen** (bis zu drei Zertifikate oder eins mit drei Namen).
7. ACME mit **HTTP-01 und DNS-01**. DNS-01 braucht kein LCM im Internet.

8. Zertifikate und Netzbereiche ändern **nur Admins**, nicht Manager mit `settings:manage`.
9. **Alle Pakete P1–P8 kommen auf `develop`**, also auch P6 und P7. Release C bleibt davon unberührt: Worktree `LCM-release-c` und Branch `feat/release-c` werden nicht angefasst, gearbeitet wird nur in `~/dev/LCM/LCM`.

## Zu lego

Der Eindruck „riesig“ stimmt, er kommt aber vom Einbinden aller Anbieter. ZPO importiert die Fabrik `providers/dns` (`NewDNSChallengeProviderByName`), und damit linkt Go alle ~230 DNS-Anbieter samt SDKs. Daher ist `zpo` ~80 MB groß, `lcm` heute ~40 MB.

Bindet man nur einzelne Anbieterpakete direkt ein, linkt Go nur diese. `go.sum` wird lang, das Binary nicht. Version: **lego v5** (aktuell v5.6.0, mit ARI über `GetRenewalInfo`/`UseARICertID`). v4 hat kein belegtes ARI, und für LCM ist es neuer Code, deshalb fällt der v5-Umbau, der ZPO bremst, hier weg. Die schlanke Alternative (acmez + x/crypto/acme + eigener RFC2136-Code) bedeutet mehr eigenen Code und bleibt der Rückfall, falls die Messung schlecht ausfällt.

**Vor der Entscheidung messen:** Probe-Build mit genau dieser Auswahl. Ziel: höchstens +10 MB.

## Pakete

### P1 Zertifikatspool mit Auswahl nach Namen (zu prüfen)
- Statt einer Zuordnung je Endpunkt gibt es einen **Pool**. Jeder Listener wählt beim Handshake aus dem Pool das Zertifikat, dessen Namen zu `hello.ServerName` passen. Ohne Treffer oder ohne SNI (Aufruf per IP) gilt das LCM-CA-Zertifikat. Damit ist beides abgedeckt: ein Zertifikat für einen Namen auf drei Ports, oder drei Zertifikate (bzw. eins mit drei Namen) auf 443.
- config.json, neu:
  ```json
  "tls_certificates": [
    { "cert": "/etc/lcm/tls/lcm.pem",   "key": "/etc/lcm/tls/lcm.key" },
    { "cert": "/etc/lcm/tls/agent.pem", "key": "/etc/lcm/tls/agent.key" }
  ]
  ```
  Validierung in `internal/config/config.go` nach dem Muster von `tls_ca_cert`.
- Quellen im Pool: config.json, Upload, ACME. Bei gleichen Namen gilt diese Rangfolge.
- `tlsx.CertStore` wird zum Pool mit `GetCertificate(hello)`. Das atomare Laden aus `tlsx/renew.go` bleibt.
- Prüfung beim Laden: PEM, Schlüssel passt, gültig, serverAuth, Kette vollständig (sonst Warnung). **Ein Fehler lässt den bisherigen Stand aktiv.**
- Uploads unter `<data>/tls/uploads/<id>/{cert,key}.pem` (0600), in der Sicherung wie `lcm-ca-key.pem` (`backup_service.go:223`).
- Aufräumen: tote Felder `TLSCertPEM`/`TLSKeyPEMEnc`/`TLSCustomUsed` samt Migration weg, Kommentare in `main.go` berichtigen.

### P2 Neu laden zur Laufzeit und CLI (zu prüfen)
- Beim Start immer laden. `lcm tls reload` und `lcm tls status` über `<data>/control.sock` (0600, Dienstnutzer und root). Die Ausgabe zeigt je Zertifikat Quelle, Namen, Aussteller, Ablauf und Fingerprint, dazu, welcher Endpunkt es für seine öffentliche Adresse ausliefert, oder den Ablehnungsgrund.
- `SIGHUP` macht dasselbe. In der Unit `ExecReload=/bin/kill -HUP $MAINPID`.
- Offene Verbindungen behalten ihr Zertifikat, Agenten werden nicht getrennt. Audit `settings.tls.reload`.
- Doku: certbot-`--deploy-hook` kopiert nach `/etc/lcm/tls/` (Gruppe `lcm`, 0640) und ruft `lcm tls reload`.

### P3 Netzbereiche je Endpunkt (zu prüfen)
- Je Endpunkt (UI, Agent, MCP) eine Liste erlaubter Netze (CIDR). Leer heißt alles erlaubt, das ist der Standard und das heutige Verhalten bei Agent und MCP. Einstellbar in der Web-UI (DB) und in der config.json.
- **config.json gewinnt:** Steht ein Endpunkt dort (das bisherige `allowed_ips` gilt als UI-Liste, neu `agent_allowed_ips`, `mcp_allowed_ips`), ist das Feld in der UI nur lesbar. So kommt man bei einer Aussperrung über die Konsole wieder rein.
- Wirkt zur Laufzeit ohne Neustart: Die Allowlist wird ein atomarer Wert, gelesen von `IPAllowlist`. Dieselbe Middleware sitzt auch vor Agent-Gateway und MCP und bestimmt die Client-Adresse über `ResolveClientIP`, hinter einem vertrauten Proxy also aus X-Forwarded-For.
- **Schutz vor Selbstaussperrung:** Speichern wird abgelehnt, wenn die eigene Adresse des Admins nicht im neuen UI-Bereich liegt. Loopback ist immer erlaubt. Engt jemand den Agent-Bereich ein, warnt die UI vor Agenten, die gerade von außerhalb verbunden sind (Liste aus dem Hub).
- Der öffentliche Binary-Download (`/api/v1/agent/download/`) bleibt wie heute ausgenommen.
- Der ACME-Listener auf Port 80 beantwortet `/.well-known/acme-challenge/` **immer**. Die Weiterleitung auf die UI bekommen nur Adressen aus dem UI-Bereich, alle anderen eine 404.
- Audit `settings.network.*`.

### P4 UI: Einstellungen → Allgemein (zu prüfen)
- Karte „Öffentliche Adresse“ (`ui/src/pages/settings/general.ts:146`): ein Zahnrad hinter UI-, Agent- und MCP-Adresse öffnet einen Dialog mit zwei Bereichen.
  - **Zertifikat:** zeigt, was für den Namen dieser Adresse ausgeliefert wird (Quelle, Namen, Aussteller, gültig bis, Fingerprint). Bietet Hochladen (PEM, Fullchain), Entfernen („LCM-Zertifikat verwenden“) und ab P7 ACME.
  - **Netzbereiche:** Liste erlaubter Netze mit den Schutzregeln aus P3.
- Was die config.json festlegt, ist nur lesbar, mit Hinweis.
- API: `GET /api/v1/system/tls`, `POST /api/v1/system/tls/certificates` (multipart, Muster `RestoreUpload` in `ops_controller.go:231`), `DELETE …/certificates/{id}`, `POST /api/v1/system/tls/reload`, `PUT /api/v1/system/network/{endpoint}`. Admin-only, Audit.
- Texte in `ui/src/locales/de.json` und `en.json`.

### P5 Agent-Vertrauen für eigene Zertifikate (zu prüfen)
- Öffentliche CA: funktioniert schon über die Systemroots mit Namensprüfung.
- Private CA (Heim-CA): `verifyIssuedBy` in `peer.go` nimmt eine gepinnte CA auch ohne `urn:lcm:server`-Kennung an, dann aber **nur mit Namensprüfung**. Das Token trägt als `c` den Fingerprint der ausstellenden CA. Ältere Agenten kommen in den Release-Hinweis.
- Assistent „Server aufnehmen“: Token-Pins und `--pinnedpubkey` kommen aus dem Zertifikat, das für den Namen der Agent- bzw. UI-Adresse tatsächlich ausgeliefert wird (`main.go:540`, `join-model.ts:246`).

### P6 Gemeinsamer Port mit Namensweiche (zu prüfen)
- Wird für die Betriebsart „alles auf 443“ gebraucht.
- config.json `shared_tls_listen` (z. B. `":443"`). Ein TLS-Listener mit dem Pool. Nach dem Handshake wird die Verbindung anhand von SNI in einen speicherinternen `net.Listener` der UI-, Agent- oder MCP-App gelegt (`fiber.App.Listener`). Es wird nichts kopiert und nichts weitergereicht.
- Die Zuordnung Name → Dienst kommt aus den drei öffentlichen Adressen (`public_base_url`, `public_agent_url`, `public_mcp_url`). Damit hat `public_mcp_url` endlich einen Leser. Ohne SNI geht es an die UI. Teilen sich zwei Dienste einen Namen auf dem gemeinsamen Port, ist das ein Konfigurationsfehler.
- Die Einzelports bleiben zusätzlich nutzbar oder lassen sich abschalten (`agent_port: 0` wie heute).
- MCP-TLS (Entscheidung 2) ist Teil von P6.

### P7 ACME: HTTP-01 und DNS-01 mit lego v5 (zu prüfen)
- Zuerst der Probe-Build zur Größe (siehe oben).
- Einstellungen: Namen (bis zu drei, ein Zertifikat mit allen Namen oder je Name eins), Kontakt-Mail optional, Verzeichnis-URL (Let's Encrypt, Staging oder eigene ACME-CA wie step-ca), Challenge-Art.
- **HTTP-01:** Listener `acme_http_listen` (Standard `:80` mit Drop-in, alternativ ein hoher Port hinter Portweiterleitung), Verhalten wie in P3. Das Ziel der Weiterleitung ist `public_base_url`, **nie** der Host-Header (siehe `fallbackBaseURL`, `main.go:1155`).
- **DNS-01:** direkt eingebundene Anbieter, keine Fabrik. Gemessen 21.11.: lego v5.6.0 mit allen unten genannten Anbietern +2,9 MB (Kern 1,4 MB, `dnsupdate` = RFC2136 0,9 MB). In v5 heißt RFC2136 `providers/dns/dnsupdate`.
  - Allgemein: `rfc2136` (PowerDNS, BIND, Knot, Technitium …) und `acmedns`.
  - Feste Anbieter: Cloudflare, Hetzner, PowerDNS-API, INWX, IONOS. Die Auswahl passt Tony an.
  - Generisch: `httpreq` (eigener HTTP-Endpunkt) und `exec` (eigenes Skript). **`exec` nur über die config.json**, damit ein UI-Konto keinen Befehl auf dem LCM-Host setzen kann.
  - Zugangsdaten der Anbieter verschlüsselt in der DB (bestehende AES-GCM-Verschlüsselung der Einstellungen).
- Erneuerung nach ARI mit Rückfall auf ein Drittel der Restlaufzeit, im täglichen Lauf von `CertRenewal.RunDaily`. Bei einem Fehlschlag kommt ein Alarm, dazu der bestehende `tlsCertExpiring`. Der Kontoschlüssel liegt in `<data>/tls/acme/`.

### P8 Doku und Release-Hinweise (zu prüfen)
- Neue Seite `docs/guides/zertifikate.md` + `docs/en/…`:
  - die zwei Betriebsarten (Ports + ein Name, 443 + drei Namen)
  - Datei, Upload, ACME HTTP-01/DNS-01
  - Netzbereiche
  - certbot-Hook, Drop-in für 80/443, Verhalten der Agenten
- Nachziehen: `reverse-proxy.md`, `installation.md` (Port-Tabelle, `LCM_PORT=443` braucht die Capability), `remote.md`, `mcp.md`, `architecture.md:128`, `security-model.md`, `rest-api.md`, `docker/README.md`, README.
- `release-info/<version>.md`, Commits mit Betreff für Anwender (Skill `lcm-release`).

## Umsetzung

- Reihenfolge: P1 → P2 → P3 → P5 → P6 → P7 → P4 → P8. Die UI kommt nach Backend, Weiche und ACME, damit sie gleich alles abbildet.
- Sicherheitsrelevante Pakete (P1, P2, P3, P5, P6, P7) und die UI (P4) bauen Sonnet-Agenten mit `effort: high` und vollständigem Briefing. Ich prüfe jedes Ergebnis selbst, `code-reviewer` liest vor dem Push gegen. MR erst, wenn Tony es sagt.

## Prüfung

- Unit-Tests:
  - Pool-Auswahl nach SNI samt Rückfall; ein kaputtes Zertifikat lässt den alten Stand aktiv
  - Reload über Socket und SIGHUP; Config-Validierung
  - Netzbereiche je Endpunkt mit Proxy-Adresse, Aussperrschutz, Challenge-Pfad offen und Weiterleitung gesperrt
  - `peer.go` mit privater CA (mit und ohne Namen)
  - SNI-Weiche (UI/Agent/MCP, ohne SNI); Rechte (Manager wird abgewiesen)
- E2E (`LCM_E2E_TLS=1`, `ui/e2e/`):
  - Zertifikat hochladen; Handshake liefert es für den Namen und das LCM-Zertifikat per IP
  - Netzbereich setzen, Aussperrschutz greift
- ACME gegen **Pebble** (Test-CA von Let's Encrypt, Container) mit HTTP-01. DNS-01 über `rfc2136` gegen einen lokalen PowerDNS oder BIND (z. B. die PowerDNS-Testumgebung).
- Proxmox-Testumgebung (Skill `lcm-proxmox-tests`):
  - echter Agent verbunden, Zertifikat tauschen per Upload und per `lcm tls reload`: der Agent bleibt verbunden und kommt nach einem Neustart wieder
  - Betriebsart 443 mit drei Namen
  - Agent-Bereich einengen, Gegenprobe mit einem alten Agent
- Lokal vor dem Push alles, was die CI prüft (Skill `gitlab-workflow`).
