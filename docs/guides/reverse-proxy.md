---
sidebar:
  order: 23
title: Reverse-Proxy & Absicherung von außen
description: nginx/Caddy vor der Weboberfläche - TLS, Anmelde-Bremse, Body-Limits, WebSocket/SSE und die vertrauenswürdige Proxy-Liste.
---

LCM spricht ab Werk HTTPS mit einem selbstsignierten Zertifikat und bindet die
Weboberfläche an `127.0.0.1`. Für eine Installation, die über das eigene Netz
hinaus erreichbar ist, gehört ein Reverse-Proxy davor - er terminiert TLS mit
einem echten Zertifikat, bremst Anmeldeversuche über **alle** Quellen hinweg
und begrenzt, was überhaupt bis zu LCM durchkommt.

Diese Seite behandelt den **UI/REST-Port** (`9310`). Der Agent-Port hat eigene
Anforderungen (dauerhafte WebSocket-Verbindung, Zertifikats-Pin) und eine eigene
Seite: [LCM Remote → Hinter einem Reverse-Proxy](/guides/remote/#hinter-einem-reverse-proxy).

## Was LCM selbst mitbringt

Der Proxy ergänzt, er ersetzt nichts. Unabhängig von ihm gilt:

| Schutz | in LCM |
|---|---|
| Anmelde-Bremse | 5 Fehlversuche je Client-IP, 15 je Konto, wachsende Sperre bis 15 min; TOTP-Codes gelten genau einmal |
| Rumpf-Budget | 1 MiB je Anfrage, 64 MiB nur für den Backup-Upload - geprüft an der Kopfzeile, bevor der Rumpf gelesen wird |
| Passwortlänge beim Login | über 1024 Bytes wird gar nicht erst gehasht |
| Sicherheits-Header | CSP, HSTS, `X-Frame-Options`, `Referrer-Policy`, `Permissions-Policy`, `Cache-Control: no-store` auf der API |
| Sicherheitsprotokoll | eine Journal-Zeile `security event=…` je Anmeldung, Sperre, Passwort-, 2FA- und Schlüssel-Änderung |

Was nur der Proxy leisten kann: ein gültiges Zertifikat, eine Bremse, die
alle Clients auf einmal sieht, und eine Grenze, die einen Angreifer beschäftigt,
bevor er den Go-Prozess überhaupt erreicht.

## `trusted_proxies`: wem X-Forwarded-For geglaubt wird

Hinter einem Proxy sieht LCM als direkte Gegenstelle nur den Proxy. Damit
IP-Allowlist, Anmeldesperre und Protokolle den echten Client sehen, schreibt
der Proxy `X-Forwarded-For`, und LCM liest es - aber **nur von Adressen, die
in `trusted_proxies` stehen**:

```json
{
  "trust_proxy_header": true,
  "trusted_proxies": ["127.0.0.1", "::1"]
}
```

Einträge sind IP-Adressen, CIDR-Bereiche oder die Schlüsselwörter
`localhost`/`private`. Kommt eine Anfrage von einer anderen Adresse, zählt
die Kopfzeile nicht - die Peer-Adresse gilt.

:::caution[Leere Liste heißt: jeder darf]
`trust_proxy_header: true` ohne `trusted_proxies` glaubt die Kopfzeile von
**jedem** Peer (das Verhalten älterer Versionen; LCM warnt beim Start). Das ist
nur vertretbar, wenn der LCM-Port ausschließlich vom Proxy erreichbar ist.
Sonst gibt sich ein Client, der den Port direkt erreicht, mit einer
gefälschten Kopfzeile jede beliebige Adresse - in die Allowlist hinein und
aus der Anmeldesperre heraus.
:::

Nach der Änderung Dienst neu starten. Prüfen: Fehlversuche im Journal tragen
die Client-Adresse, nicht die des Proxys:

```sh
journalctl -u lcm -p warning | grep 'security'
```

## nginx

```nginx
# Anmelde-Bremse: 10 Anfragen pro Minute und Quelle auf den Anmelde-Pfaden -
# zusätzlich zur Bremse in LCM, die nur je Prozess zählt.
limit_req_zone $binary_remote_addr zone=lcm_auth:10m rate=10r/m;

server {
    listen 443 ssl;
    http2 on;
    server_name lcm.example.com;

    ssl_certificate     /etc/letsencrypt/live/lcm.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/lcm.example.com/privkey.pem;
    ssl_protocols       TLSv1.2 TLSv1.3;

    # LCM sendet HSTS selbst; der Proxy reicht die Kopfzeilen nur durch.

    # Vorgabe für alles: kleine Rümpfe. LCM lehnt darüber ohnehin ab -
    # hier endet der Versuch schon am Proxy.
    client_max_body_size 1m;

    # Anmeldung, zweiter Faktor, Passwort-Reset, Aktivierungslinks.
    location ~ ^/api/v1/(auth/|users/activation-links/consume) {
        limit_req zone=lcm_auth burst=10 nodelay;
        proxy_pass https://127.0.0.1:9310;
        include /etc/nginx/lcm-proxy.conf;
    }

    # Backup-Upload: der einzige große Rumpf.
    location = /api/v1/system/backups/restore-upload {
        client_max_body_size 64m;
        proxy_request_buffering off;
        proxy_pass https://127.0.0.1:9310;
        include /etc/nginx/lcm-proxy.conf;
    }

    # Web-Konsole (WebSocket) - die Sitzung bleibt offen.
    location ~ ^/api/v1/servers/[0-9]+/terminal$ {
        proxy_pass https://127.0.0.1:9310;
        proxy_http_version 1.1;
        proxy_set_header Upgrade    $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 3600s;
        include /etc/nginx/lcm-proxy.conf;
    }

    # Live-Protokoll (Server-Sent Events) - nicht puffern.
    location = /api/v1/system/logs/stream {
        proxy_pass https://127.0.0.1:9310;
        proxy_buffering off;
        proxy_read_timeout 3600s;
        include /etc/nginx/lcm-proxy.conf;
    }

    location / {
        proxy_pass https://127.0.0.1:9310;
        include /etc/nginx/lcm-proxy.conf;
    }
}
```

`/etc/nginx/lcm-proxy.conf` - die Kopfzeilen, die jede Weiterleitung braucht:

```nginx
proxy_set_header Host              $host;
proxy_set_header X-Real-IP         $remote_addr;
proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
proxy_set_header X-Forwarded-Proto $scheme;
# LCM intern mit Selbstsignat: die Strecke Proxy → LCM läuft über Loopback.
proxy_ssl_verify off;
```

Dazu in LCM: `"trusted_proxies": ["127.0.0.1"]` und `public_base_url` auf
`https://lcm.example.com`.

## Caddy

```caddy
lcm.example.com {
    reverse_proxy https://127.0.0.1:9310 {
        transport http {
            tls_insecure_skip_verify   # nur bei internem Selbstsignat
        }
    }
    request_body {
        max_size 64MB
    }
}
```

Caddy setzt `X-Forwarded-For` selbst und erkennt WebSocket und SSE ohne
Sonderbehandlung. Eine Anfragen-Bremse gibt es in Caddy nur als Erweiterung
(`caddy-ratelimit`); wer sie nicht einbaut, verlässt sich auf die Bremse in LCM.

## Netz und Betrieb

- **Bind belassen.** `host` bleibt `127.0.0.1`, der Proxy läuft auf demselben
  Rechner. Muss LCM auf einer Netzschnittstelle lauschen, gehört eine
  Host-Firewall davor, die Port 9310 nur vom Proxy zulässt - sonst greift
  `trusted_proxies` als zweite Linie.
- **IP-Allowlist zusätzlich.** Erreichen nur bekannte Netze die Oberfläche
  (Büro, VPN), sperrt `allowed_ips` alle anderen, bevor die Anmeldung überhaupt
  antwortet - siehe [IP-Allowlist](/guides/security-cve/#netzwerk-zugriff-auf-lcm-einschränken-ip-allowlist).
- **fail2ban** kann das Sicherheitsprotokoll lesen. Muster für die
  Anmeldefehler: `security event=login.failed ip=<HOST>`, Journal-Einheit
  `lcm`. Die Adresse ist die des Clients, sobald `trusted_proxies` stimmt.
- **Journal weiterleiten.** Das Audit-Log liegt in der Datenbank auf demselben
  Rechner. Wer den Host übernimmt, kann es ändern - eine Kopie des Journals
  auf einem anderen System (Syslog, SIEM) ist der Nachweis, der bleibt.
- **Kein Skript-Injizieren.** Die Content-Security-Policy erlaubt nur eigene
  Skripte. Ein Proxy, der Analytics oder Banner einfügt, wird vom Browser
  blockiert - wer das braucht, ersetzt die Kopfzeile bewusst und weiß, welchen
  Schutz er aufgibt.
