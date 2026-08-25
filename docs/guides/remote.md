---
sidebar:
  order: 21
title: LCM Remote (lcm-agent)
description: Server hinter NAT per ausgehendem Agent verwalten - auf einem eigenen, dedizierten Port.
---

Neben dem klassischen SSH-Onboarding kann ein Server auch per **lcm-agent**
verwaltet werden (**LCM Remote**). Der Agent läuft als Dienst auf dem Zielserver
und verbindet sich **ausgehend** per MQTT-über-WebSocket mit LCM - ideal für
Server hinter NAT, ohne feste IP oder unterwegs. Kommandos laufen dann über den
Agent statt per SSH, transparent für alle Funktionen (Scans, Updates, Docker,
Firewall …).

## Agent oder SSH?

Beide Transporte sind gleichwertig - dieselben Scans, Regeln und Jobs laufen
darüber. Die Wahl hängt an der Netz-Topologie:

| Situation | Empfehlung |
| --- | --- |
| Server hinter NAT / ohne eingehende Erreichbarkeit | **Agent** (verbindet ausgehend) |
| Wechselnde/dynamische IP, Roaming (Notebook) | **Agent** |
| Du willst keinen eingehenden SSH-Port öffnen | **Agent** |
| Klassischer Server mit erreichbarem SSH | **SSH** |
| SSH-Härtung, Zertifikats-Rotation, Reconnect nötig | **SSH** (diese Aktionen sind agentseitig ausgeblendet) |

Ein Server ist entweder das eine **oder** das andere - der Transport steht beim
Anlegen fest.

## Dedizierter Agent-Port

Die Agent-Kommunikation läuft auf einem **eigenen, dedizierten Port** - getrennt
von der Weboberfläche und der REST-API:

- Auf dem **Agent-Port** liegt **ausschließlich** der Agent-Endpunkt
  (`GET /mqtt`). Keine UI, keine REST-API, keine statischen Dateien; alles andere
  auf diesem Port antwortet mit 404.
- Auf dem **UI/REST-Port** gibt es **keine** Agent-Schnittstelle.

| Einstellung | Bedeutung | Default |
| --- | --- | --- |
| `agent_port` | Port des Agent-Listeners; `0` schaltet ihn ab | `9320` |
| `agent_host` | Bind-Adresse des Agent-Listeners | `0.0.0.0` |

Beides steht in der `config.json` und lässt sich per Umgebungsvariable
übersteuern (`LCM_AGENT_PORT`, `LCM_AGENT_HOST`) - `LCM_AGENT_PORT=0` deaktiviert
den Agent-Listener ganz. Der Agent-Port bindet standardmäßig an **alle
Interfaces** (`0.0.0.0`), weil sich die Agents von außen verbinden; die UI bleibt
auf ihrer eigenen Bind-Adresse (Default `127.0.0.1`, üblicherweise hinter einem
Reverse-Proxy).

`config.json` (Ausschnitt):

```json
{
  "host": "127.0.0.1",
  "port": 9310,
  "agent_host": "0.0.0.0",
  "agent_port": 9320
}
```

Der Agent-Port **darf nicht** mit dem UI/REST-Port übereinstimmen - LCM lehnt
eine solche Config beim Start ab (jeder Listener braucht einen eigenen Port).

:::note[TLS & Firewall]
Der Agent-Listener nutzt **dasselbe TLS-Zertifikat** wie die UI. Beim Enrollment
wird der **SHA-256-Fingerprint** des aktiven Zertifikats ins Token eingebettet;
der Agent pinnt ihn beim Erstkontakt (MitM-Schutz, analog zur SSH-Fingerprint-
Bestätigung). Öffne den Agent-Port (Default **9320**) in der Firewall, damit die
Agents ihn erreichen. Im `--dev`-Modus läuft der Listener ohne TLS (HTTP) und
ohne Pin - nur für lokale Tests.
:::

## Hinter einem Reverse-Proxy

Der Agent-Port lässt sich hinter nginx, Apache, Traefik oder Caddy legen. Er
verlangt dabei mehr als ein gewöhnlicher HTTP-Dienst, und zwar aus drei
Gründen:

1. **Es ist eine WebSocket-Verbindung.** Der Agent spricht MQTT über
   WebSocket (`GET /mqtt`, Subprotokoll `mqtt`, binäre Frames). Ohne
   Upgrade-Weiterleitung kommt gar keine Verbindung zustande.
2. **Sie bleibt offen.** Der Agent hält sie dauerhaft und schickt alle
   **30 Sekunden** ein Keepalive. Ein Proxy mit dem üblichen Lesetimeout von
   60 Sekunden kappt sie trotzdem regelmäßig - der Agent verbindet sich zwar
   neu, aber der Server wirkt dabei ständig kurz offline.
3. **Das Zertifikat entscheidet.** Im Enrollment-Token steckt der Fingerprint
   des Zertifikats, das LCM selbst ausliefert. Terminiert der Proxy TLS mit
   einem eigenen Zertifikat, passt der Pin nicht mehr - siehe unten.

### Zertifikat: was beim Proxy passiert

Der Agent prüft in dieser Reihenfolge:

1. Stimmt der **gepinnte Fingerprint** aus dem Token? Dann ist alles gut.
2. Sonst: Lässt sich die **Zertifikatskette regulär prüfen**, samt Hostname?
   Dann ebenfalls.

Daraus folgt der Regelfall hinter einem Proxy: Terminiert er TLS mit einem
öffentlich vertrauenswürdigen Zertifikat (Let's Encrypt), **greift Weg 2** -
der Pin passt nicht, die Kette schon. Wichtig ist nur, dass beim `enroll` die
**öffentliche Adresse** angegeben wird, auf die das Zertifikat lautet:

```sh
sudo lcm-agent enroll https://lcm.example.com:9320 <token>
```

Ein selbstsigniertes Zertifikat auf dem Proxy scheitert an beiden Wegen. Dann
gibt es zwei saubere Möglichkeiten: den Agent-Port **am Proxy vorbei**
durchreichen (TCP-Passthrough, Weg 1 bleibt intakt) - oder ein Zertifikat
verwenden, dem die Agents ohnehin vertrauen.

:::caution[TLS am Proxy beenden heißt: der Proxy sieht alles]
Der Agent-Kanal trägt Verwaltungs-Kommandos, die auf dem Zielsystem als root
laufen. Wer den Proxy kontrolliert, kontrolliert damit die Agents. Auf einem
fremdverwalteten Proxy gehört der Agent-Port deshalb im TCP-Passthrough
durchgereicht, nicht terminiert.
:::

### nginx

Eigener `server`-Block für den Agent-Port - er trägt nur `/mqtt`:

```nginx
server {
    listen 9320 ssl;
    http2 on;
    server_name lcm.example.com;

    ssl_certificate     /etc/letsencrypt/live/lcm.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/lcm.example.com/privkey.pem;

    location /mqtt {
        proxy_pass https://127.0.0.1:9320;

        # WebSocket-Upgrade - ohne das kommt keine Verbindung zustande.
        proxy_http_version 1.1;
        proxy_set_header Upgrade    $http_upgrade;
        proxy_set_header Connection "upgrade";
        # Das Subprotokoll gehört durchgereicht (MQTT über WebSocket).
        proxy_set_header Sec-WebSocket-Protocol $http_sec_websocket_protocol;

        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # Die Verbindung bleibt offen. 60s (Default) kappt sie im Takt des
        # Keepalives; 1h ist reichlich und stört nichts.
        proxy_read_timeout  3600s;
        proxy_send_timeout  3600s;
        # Puffern bringt hier nichts und verzögert nur.
        proxy_buffering off;
    }
}
```

Läuft LCM intern ohne TLS (etwa im abgeschotteten Netz mit `--dev`), lautet
das Ziel `http://127.0.0.1:9320`. Bei internem Selbstsignat zusätzlich
`proxy_ssl_verify off;` - die Strecke Proxy → LCM ist dann ungeprüft, was nur
über Loopback oder ein vertrauenswürdiges Netz vertretbar ist.

### TCP-Passthrough mit nginx (empfohlen bei Selbstsignat)

Ohne TLS-Terminierung bleibt der Fingerprint-Pin intakt - der Proxy reicht
nur Bytes durch. Das gehört in den `stream`-Block, nicht in `http`:

```nginx
stream {
    server {
        listen 9320;
        proxy_pass 127.0.0.1:9320;
        proxy_timeout 1h;
    }
}
```

### Apache

Braucht `mod_proxy`, `mod_proxy_http` und **`mod_proxy_wstunnel`**:

```apache
<VirtualHost *:9320>
    ServerName lcm.example.com

    SSLEngine on
    SSLCertificateFile    /etc/letsencrypt/live/lcm.example.com/fullchain.pem
    SSLCertificateKeyFile /etc/letsencrypt/live/lcm.example.com/privkey.pem

    SSLProxyEngine on
    # Nur nötig, wenn LCM intern ein selbstsigniertes Zertifikat nutzt:
    SSLProxyVerify none
    SSLProxyCheckPeerName off

    # wss:// statt https:// - sonst wird der Upgrade nicht durchgereicht.
    ProxyPass        /mqtt wss://127.0.0.1:9320/mqtt
    ProxyPassReverse /mqtt wss://127.0.0.1:9320/mqtt

    # Wie bei nginx: die Verbindung bleibt offen.
    ProxyTimeout 3600
</VirtualHost>
```

### Caddy

Caddy erkennt den Upgrade selbst und hat keine kurzen Lesetimeouts:

```caddy
lcm.example.com:9320 {
    reverse_proxy https://127.0.0.1:9320 {
        # Nur bei internem Selbstsignat:
        transport http {
            tls_insecure_skip_verify
        }
    }
}
```

### Traefik

```yaml
http:
  routers:
    lcm-agent:
      rule: "Host(`lcm.example.com`) && Path(`/mqtt`)"
      service: lcm-agent
      tls: {}
  services:
    lcm-agent:
      loadBalancer:
        servers:
          - url: "https://127.0.0.1:9320"
serversTransports:
  lcm-agent:
    insecureSkipVerify: true   # nur bei internem Selbstsignat
```

### Die Weboberfläche hinter dem Proxy

Der UI-Port ist ein gewöhnlicher HTTPS-Dienst und braucht keine
WebSocket-Sonderbehandlung. Zwei Dinge gehören trotzdem eingestellt:

- **`public_base_url`** unter *Einstellungen → Allgemein*. Aktivierungs- und
  Rücksetz-Links werden daraus gebaut - ohne die Angabe zeigen sie auf die
  interne Adresse, die von außen niemand erreicht.
- **`X-Forwarded-For`** durchreichen und in LCM den Schalter *hinter
  vertrauenswürdigem Reverse-Proxy* setzen, sonst sieht die IP-Allowlist und
  die Sperre nach Fehlversuchen nur die Adresse des Proxys.

### Prüfen, ob es trägt

```sh
# Erreicht der Upgrade den Server? Erwartet wird 101 Switching Protocols.
curl -isk -o /dev/null -w '%{http_code}\n' \
  -H "Connection: Upgrade" -H "Upgrade: websocket" \
  -H "Sec-WebSocket-Version: 13" -H "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==" \
  -H "Sec-WebSocket-Protocol: mqtt" \
  https://lcm.example.com:9320/mqtt
```

Danach am Agent-Server `lcm-agent enroll …` ausführen: Der Verbindungstest
läuft vor jeder Installation und meldet einen Proxy-Fehler sofort im Klartext,
statt ihn im Dienst-Log zu vergraben.

## Enrollment-Token

Das Token ist das **dauerhafte** Credential des Agents (wie ein API-Key). Es
kodiert drei Teile - die **Agent-ID** (UUID), ein **Secret** und den
**Zertifikats-Fingerprint**. **At rest** speichert LCM nur den SHA-256-**Hash**
des Secrets plus einen kurzen Prefix (die ersten 12 Zeichen, für die
Wiedererkennung in der UI) - der Klartext wird **einmalig** bei der Erzeugung
angezeigt und ist danach nicht mehr abrufbar.

Beim Connect authentifiziert sich der Agent am eingebetteten MQTT-Broker mit
**Agent-ID + Secret**; der Broker vergleicht den Hash constant-time. Eine
strikte ACL sperrt jeden Agent auf sein **eigenes Topic-Subtree** (er darf nur
sein Kommando-Topic abonnieren und nur auf seine eigenen Ergebnis-/Inventar-
Topics schreiben). Der `GET /mqtt`-Upgrade ist zusätzlich per-IP
ratenbegrenzt (10 Versuche/Minute).

## Agent-Server anlegen

*Server hinzufügen → Modus **Agent***: nur einen Namen vergeben. LCM legt den
Server (zunächst offline) an und erzeugt das Enrollment-Token. Die Oberfläche
zeigt die passenden Befehle:

1. **Repository einrichten** (`curl … | sudo sh`) - entfällt, wenn schon
   vorhanden.
2. **Agent installieren.** Auf Debian/Ubuntu aus dem Paketkanal
   (`sudo apt install lcm-agent`). Für die übrigen Distributionen hängt
   dasselbe Paket als RPM, APK und Arch-Paket am
   [Release](https://gitlab.techeve.de/techeve/lcm/-/releases) - es gibt dort
   bislang keinen eigenen Paketkanal, die Datei wird also direkt installiert:

   ```sh
   sudo dnf install ./lcm-agent-<version>.x86_64.rpm      # RHEL, Fedora, Rocky, Alma
   sudo zypper install ./lcm-agent-<version>.x86_64.rpm   # openSUSE, SLES
   sudo apk add --allow-untrusted ./lcm-agent_<version>_x86_64.apk   # Alpine
   sudo pacman -U ./lcm-agent-<version>-x86_64.pkg.tar.zst           # Arch
   ```

   Der Agent ist ein statisches Go-Binary ohne Abhängigkeiten; das Paket legt
   nur ihn und seine systemd-Unit ab. Wer gar kein Paket will, lädt das nackte
   Binary aus LCM selbst (`GET /api/v1/agent/download/<arch>`).
3. **Dienst einrichten** (`sudo lcm-agent enroll <agent-url> <token>`) - die
   `<agent-url>` zeigt auf den **Agent-Port**, z. B.:

   ```sh
   sudo lcm-agent enroll https://lcm.example.com:9320 eyJhZ2VudF…<token>
   ```

Alternativ ohne Repository: Das Binary wird per `curl` direkt vom LCM-Server
geladen (über den **UI/REST-Port**), das anschließende `enroll` verwendet
wiederum den **Agent-Port**.

Nach dem Enroll verbindet sich der Agent ausgehend, der Server geht **online**,
LCM übernimmt die gemeldete Agent-Version ins Inventar und der erste System-Scan
startet automatisch (sobald der Agent sein Kommando-Topic abonniert hat). Trennt
sich der Agent, geht der Server offline und laufende Kommandos scheitern sofort
(statt in einen Timeout zu laufen).

## Token neu erzeugen

Über **Token neu erzeugen** lässt sich das Credential ersetzen (z. B. bei Verlust
oder Verdacht auf Kompromittierung): Das alte Secret wird **sofort** ungültig, die
aktive Sitzung getrennt, und der Agent muss mit dem neuen Token erneut enrollt
werden. Der neue Klartext wird wieder nur einmal angezeigt.

## Was am Agent-Server anders ist

Ein Agent-Server hat kein SSH: SSH-spezifische Aktionen (SSH-Härtung,
Zertifikats-/Key-Rotation, Reconnect) sind dort ausgeblendet bzw. werden mit einem
klaren Hinweis abgewiesen. Der Agent läuft als **Root-Dienst** auf dem Zielsystem
(kein sudo-Wrapper nötig); alle übrigen Funktionen - Scans, Paket-Updates,
Docker-Monitoring, Firewall, DNS, Sicherheits-Tools - laufen unverändert über den
Agent-Transport. Auch die SSH-Protokollierung (Recorder) und die Kommando-Limits
(ConnLimiter, Laufzeit-Watchdog, Job-Abort) greifen genau wie beim SSH-Transport.
