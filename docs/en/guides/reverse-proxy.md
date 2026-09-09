---
sidebar:
  order: 23
title: Reverse proxy & external hardening
description: nginx/Caddy in front of the web UI - TLS, login throttling, body limits, WebSocket/SSE and the trusted proxy list.
---

Out of the box LCM speaks HTTPS with a self-signed certificate and binds the
web UI to `127.0.0.1`. An installation reachable beyond your own network
belongs behind a reverse proxy: it terminates TLS with a real certificate,
throttles login attempts across **all** sources, and limits what reaches LCM
at all.

This page covers the **UI/REST port** (`9310`). The agent port has its own
requirements (long-lived WebSocket, certificate pin) and its own page:
[LCM Remote → Behind a reverse proxy](/en/guides/remote/#behind-a-reverse-proxy).

## What LCM brings on its own

The proxy complements, it does not replace. Regardless of it:

| Protection | in LCM |
|---|---|
| Login throttling | 5 failures per client IP, 15 per account, growing lockout up to 15 min; TOTP codes are valid exactly once |
| Body budget | 1 MiB per request, 64 MiB only for the backup upload - checked on the headers, before the body is read |
| Password length at login | anything above 1024 bytes is not even hashed |
| Security headers | CSP, HSTS, `X-Frame-Options`, `Referrer-Policy`, `Permissions-Policy`, `Cache-Control: no-store` on the API |
| Security log | one journal line `security event=…` per login, lockout, password, 2FA and key change |

What only the proxy can do: a valid certificate, a throttle that sees all
clients at once, and a boundary that keeps an attacker busy before they ever
reach the Go process.

## `trusted_proxies`: whose X-Forwarded-For is believed

Behind a proxy the only peer LCM sees is the proxy. So that the IP allowlist,
the login lockout and the logs see the real client, the proxy writes
`X-Forwarded-For` and LCM reads it - but **only from addresses listed in
`trusted_proxies`**:

```json
{
  "trust_proxy_header": true,
  "trusted_proxies": ["127.0.0.1", "::1"]
}
```

Entries are IP addresses, CIDR ranges or the keywords `localhost`/`private`.
A request from any other address is taken at its peer address; the header
does not count.

:::caution[An empty list means: anyone may]
`trust_proxy_header: true` without `trusted_proxies` believes the header from
**every** peer (the behaviour of older versions; LCM warns at startup). That is
acceptable only if the LCM port is reachable from the proxy alone. Otherwise a
client that reaches the port directly gives itself any address with a forged
header - into the allowlist and out of the login lockout.
:::

Restart the service after the change. To check: failed attempts in the journal
carry the client's address, not the proxy's:

```sh
journalctl -u lcm -p warning | grep 'security'
```

## nginx

The complete reference configuration lives in the repository under
`packaging/nginx/lcm.conf` (with the `lcm-proxy.conf` snippet): TLS, both
throttles, body and time limits per path, health and agent download from the
own network only. What follows is the short version.

```nginx
# Login throttle: 10 requests per minute and source on the auth paths - on
# top of LCM's own throttle, which only counts per process.
limit_req_zone $binary_remote_addr zone=lcm_auth:10m rate=10r/m;

server {
    listen 443 ssl;
    http2 on;
    server_name lcm.example.com;

    ssl_certificate     /etc/letsencrypt/live/lcm.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/lcm.example.com/privkey.pem;
    ssl_protocols       TLSv1.2 TLSv1.3;

    # LCM sends HSTS itself; the proxy just passes the headers through.

    # Default for everything: small bodies. LCM rejects above this anyway -
    # here the attempt already ends at the proxy.
    client_max_body_size 1m;

    # Login, second factor, password reset, activation links.
    location ~ ^/api/v1/(auth/|users/activation-links/consume) {
        limit_req zone=lcm_auth burst=10 nodelay;
        proxy_pass https://127.0.0.1:9310;
        include /etc/nginx/lcm-proxy.conf;
    }

    # Backup upload: the only large body.
    location = /api/v1/system/backups/restore-upload {
        client_max_body_size 64m;
        proxy_request_buffering off;
        proxy_pass https://127.0.0.1:9310;
        include /etc/nginx/lcm-proxy.conf;
    }

    # Web console (WebSocket) - the session stays open.
    location ~ ^/api/v1/servers/[0-9]+/terminal$ {
        proxy_pass https://127.0.0.1:9310;
        proxy_http_version 1.1;
        proxy_set_header Upgrade    $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 3600s;
        include /etc/nginx/lcm-proxy.conf;
    }

    # Live log (Server-Sent Events) - do not buffer.
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

`/etc/nginx/lcm-proxy.conf` - the headers every forward needs:

```nginx
proxy_set_header Host              $host;
proxy_set_header X-Real-IP         $remote_addr;
proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
proxy_set_header X-Forwarded-Proto $scheme;
# LCM internally self-signed: the proxy → LCM hop runs over loopback.
proxy_ssl_verify off;
```

In LCM: `"trusted_proxies": ["127.0.0.1"]` and `public_base_url` set to
`https://lcm.example.com`.

## Caddy

```caddy
lcm.example.com {
    reverse_proxy https://127.0.0.1:9310 {
        transport http {
            tls_insecure_skip_verify   # only with an internal self-signed cert
        }
    }
    request_body {
        max_size 64MB
    }
}
```

Caddy sets `X-Forwarded-For` itself and handles WebSocket and SSE without
special treatment. A request throttle exists in Caddy only as an extension
(`caddy-ratelimit`); without it you rely on LCM's own throttle.

## Network and operations

- **Keep the bind.** `host` stays `127.0.0.1`, the proxy runs on the same
  machine. If LCM has to listen on a network interface, a host firewall
  belongs in front that admits port 9310 from the proxy only - `trusted_proxies`
  is then the second line.
- **IP allowlist on top.** If only known networks reach the UI (office, VPN),
  `allowed_ips` locks out everyone else before login even answers - see
  [IP allowlist](/en/guides/security-cve/#restricting-network-access-to-lcm-ip-allowlist).
- **fail2ban** can read the security log. Pattern for login failures:
  `security event=login.failed ip=<HOST>`, journal unit `lcm`. The address is
  the client's once `trusted_proxies` is right.
- **Forward the journal.** The audit log lives in the database on the same
  machine. Whoever takes over the host can edit it - a copy of the journal on
  another system (syslog, SIEM) is the evidence that remains. LCM also writes
  every audit entry as a journal line `audit …` and every login/permission
  event as `security …`; see below.
- **No script injection.** The Content-Security-Policy allows own scripts only.
  A proxy that injects analytics or banners is blocked by the browser - whoever
  needs that replaces the header deliberately and knows which protection they
  give up.

## Forwarding the journal (syslog/SIEM)

Everything LCM logs ends up in the journal of the `lcm` unit: the security
events (`security event=…`), the audit mirror (`audit action=…`), the access
log and the service messages. With `rsyslog` exactly these lines go to a
second machine - over TLS, so nobody reads or forges them on the way:

```rsyslog
# /etc/rsyslog.d/50-lcm-forward.conf
module(load="imjournal" StateFile="imjournal.state")
module(load="omfwd")
global(DefaultNetstreamDriver="gtls"
       DefaultNetstreamDriverCAFile="/etc/ssl/certs/ca-certificates.crt")

# Only the lines of the LCM unit; order and timestamps are preserved.
if ($programname == "lcm") then {
    action(type="omfwd" target="syslog.example.com" port="6514" protocol="tcp"
           StreamDriver="gtls" StreamDriverMode="1" StreamDriverAuthMode="x509/name"
           StreamDriverPermittedPeers="syslog.example.com"
           queue.type="LinkedList" queue.filename="lcm-fwd" queue.saveOnShutdown="on"
           action.resumeRetryCount="-1")
    stop
}
```

Then `apt install rsyslog rsyslog-gnutls`, `systemctl restart rsyslog`, and
check on the receiving side that `security event=login.ok` arrives after a
login. The on-disk queue makes sure an outage of the receiver loses no line.
Without a syslog server of your own, `systemd-journal-upload` to a
`systemd-journal-remote` works too - the journal format with the same fields
is kept.
