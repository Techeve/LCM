---
sidebar:
  order: 21
title: LCM Remote (lcm-agent)
description: Manage servers behind NAT via an outbound agent - on its own dedicated port.
---

Besides the classic SSH onboarding, a server can also be managed via the
**lcm-agent** (**LCM Remote**). The agent runs as a service on the target server
and connects **outbound** to LCM over MQTT-over-WebSocket - ideal for servers
behind NAT, without a fixed IP, or on the move. Commands then run through the
agent instead of SSH, transparently for all functions (scans, updates, Docker,
firewall …).

## Agent or SSH?

Both transports are equivalent - the same scans, rules and jobs run over them.
The choice depends on the network topology:

| Situation | Recommendation |
| --- | --- |
| Server behind NAT / no inbound reachability | **Agent** (connects outbound) |
| Changing/dynamic IP, roaming (laptop) | **Agent** |
| You don't want to open an inbound SSH port | **Agent** |
| Classic server with reachable SSH | **SSH** |
| SSH hardening, certificate rotation, reconnect needed | **SSH** (these actions are hidden on the agent) |

A server is either one **or** the other - the transport is fixed at creation time.

## Dedicated agent port

Agent communication runs on its **own dedicated port** - separate from the web
interface and the REST API:

- The **agent port** carries **only** the agent endpoint (`GET /mqtt`). No UI, no
  REST API, no static files; everything else on this port answers with 404.
- The **UI/REST port** exposes **no** agent interface.

| Setting | Meaning | Default |
| --- | --- | --- |
| `agent_port` | Port of the agent listener; `0` disables it | `9320` |
| `agent_host` | Bind address of the agent listener | `0.0.0.0` |

Both live in `config.json` and can be overridden via environment variables
(`LCM_AGENT_PORT`, `LCM_AGENT_HOST`) - `LCM_AGENT_PORT=0` disables the agent
listener entirely. The agent port binds to **all interfaces** (`0.0.0.0`) by
default, because agents connect from the outside; the UI stays on its own bind
address (default `127.0.0.1`, usually behind a reverse proxy).

`config.json` (excerpt):

```json
{
  "host": "127.0.0.1",
  "port": 9310,
  "agent_host": "0.0.0.0",
  "agent_port": 9320
}
```

The agent port **must not** match the UI/REST port - LCM rejects such a config at
startup (each listener needs its own port).

:::note[TLS & firewall]
The agent listener uses the **same TLS certificate** as the UI. At enrollment the
**SHA-256 fingerprint** of the active certificate is embedded into the token; the
agent pins it on first contact (MitM protection, analogous to SSH fingerprint
confirmation). Open the agent port (default **9320**) in the firewall so agents
can reach it. In `--dev` mode the listener runs without TLS (HTTP) and without a
pin - for local testing only.
:::

## Behind a reverse proxy

The agent port can sit behind nginx, Apache, Traefik or Caddy. It demands more
than an ordinary HTTP service, for three reasons:

1. **It is a WebSocket connection.** The agent speaks MQTT over WebSocket
   (`GET /mqtt`, subprotocol `mqtt`, binary frames). Without upgrade
   forwarding no connection is established at all.
2. **It stays open.** The agent holds it permanently and sends a keepalive
   every **30 seconds**. A proxy with the usual 60-second read timeout still
   cuts it regularly - the agent reconnects, but the server keeps flickering
   offline.
3. **The certificate decides.** The enrollment token carries the fingerprint
   of the certificate LCM itself serves. If the proxy terminates TLS with its
   own certificate, the pin no longer matches - see below.

### What happens to the certificate at the proxy

The agent checks in this order:

1. Does the **pinned fingerprint** from the token match? Then all is well.
2. Otherwise: does the **certificate chain validate** normally, hostname
   included? Then that is fine too.

Hence the common case behind a proxy: if it terminates TLS with a publicly
trusted certificate (Let's Encrypt), **path 2 applies** - the pin does not
match, the chain does. The only requirement is that `enroll` is given the
**public address** the certificate is issued for:

```sh
sudo lcm-agent enroll https://lcm.example.com:9320 <token>
```

A self-signed certificate on the proxy fails both paths. Two clean options
then: pass the agent port **through** the proxy (TCP passthrough, path 1 stays
intact) - or use a certificate the agents already trust.

:::caution[Terminating TLS at the proxy means the proxy sees everything]
The agent channel carries management commands that run as root on the target.
Whoever controls the proxy controls the agents. On a proxy you do not manage
yourself, pass the agent port through as TCP rather than terminating it.
:::

### nginx

A dedicated `server` block for the agent port - it only carries `/mqtt`:

```nginx
server {
    listen 9320 ssl;
    http2 on;
    server_name lcm.example.com;

    ssl_certificate     /etc/letsencrypt/live/lcm.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/lcm.example.com/privkey.pem;

    location /mqtt {
        proxy_pass https://127.0.0.1:9320;

        # WebSocket upgrade - without this nothing connects.
        proxy_http_version 1.1;
        proxy_set_header Upgrade    $http_upgrade;
        proxy_set_header Connection "upgrade";
        # Pass the subprotocol through (MQTT over WebSocket).
        proxy_set_header Sec-WebSocket-Protocol $http_sec_websocket_protocol;

        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # The connection stays open. The 60s default cuts it in step with the
        # keepalive; an hour is plenty and hurts nothing.
        proxy_read_timeout  3600s;
        proxy_send_timeout  3600s;
        proxy_buffering off;
    }
}
```

If LCM runs without TLS internally, the target is `http://127.0.0.1:9320`.
With an internal self-signed certificate add `proxy_ssl_verify off;` - the
proxy-to-LCM leg is then unverified, which is only defensible over loopback or
a trusted network.

### TCP passthrough with nginx (recommended for self-signed setups)

Without TLS termination the fingerprint pin stays intact - the proxy only
forwards bytes. This belongs in the `stream` block, not in `http`:

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

Needs `mod_proxy`, `mod_proxy_http` and **`mod_proxy_wstunnel`**:

```apache
<VirtualHost *:9320>
    ServerName lcm.example.com

    SSLEngine on
    SSLCertificateFile    /etc/letsencrypt/live/lcm.example.com/fullchain.pem
    SSLCertificateKeyFile /etc/letsencrypt/live/lcm.example.com/privkey.pem

    SSLProxyEngine on
    # Only needed if LCM uses a self-signed certificate internally:
    SSLProxyVerify none
    SSLProxyCheckPeerName off

    # wss:// rather than https:// - otherwise the upgrade is not passed on.
    ProxyPass        /mqtt wss://127.0.0.1:9320/mqtt
    ProxyPassReverse /mqtt wss://127.0.0.1:9320/mqtt

    ProxyTimeout 3600
</VirtualHost>
```

### Caddy

Caddy detects the upgrade itself and has no short read timeouts:

```caddy
lcm.example.com:9320 {
    reverse_proxy https://127.0.0.1:9320 {
        # Only for an internal self-signed certificate:
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
    insecureSkipVerify: true   # only for an internal self-signed certificate
```

### The web interface behind the proxy

The UI port is an ordinary HTTPS service and needs no WebSocket handling. Two
things still have to be set:

- **`public_base_url`** under *Settings → General*. Activation and reset links
  are built from it - without it they point at the internal address nobody can
  reach from outside.
- Pass **`X-Forwarded-For`** through and enable *behind a trusted reverse
  proxy* in LCM, otherwise the IP allowlist and the failed-login lockout only
  ever see the proxy's address.

### Checking that it holds

```sh
# Does the upgrade reach the server? 101 Switching Protocols is expected.
curl -isk -o /dev/null -w '%{http_code}\n' \
  -H "Connection: Upgrade" -H "Upgrade: websocket" \
  -H "Sec-WebSocket-Version: 13" -H "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==" \
  -H "Sec-WebSocket-Protocol: mqtt" \
  https://lcm.example.com:9320/mqtt
```

Then run `lcm-agent enroll …` on the agent server: the connection test runs
before any installation and reports a proxy problem in plain text rather than
burying it in the service log.

## Enrollment token

The token is the agent's **permanent** credential (like an API key). It encodes
three parts - the **agent ID** (UUID), a **secret** and the **certificate
fingerprint**. **At rest** LCM stores only the SHA-256 **hash** of the secret plus
a short prefix (the first 12 characters, for recognition in the UI) - the
plaintext is shown **once** on creation and cannot be retrieved afterwards.

On connect the agent authenticates to the embedded MQTT broker with **agent ID +
secret**; the broker compares the hash constant-time. A strict ACL confines each
agent to its **own topic subtree** (it may only subscribe to its command topic and
only write to its own result/inventory topics). The `GET /mqtt` upgrade is
additionally rate-limited per IP (10 attempts/minute).

## Creating an agent server

*Add server → mode **Agent***: just give it a name. LCM creates the server
(initially offline) and generates the enrollment token. The UI shows the matching
commands:

1. **Set up the repository** (`curl … | sudo sh`) - skip if already present.
2. **Install the agent.** On Debian/Ubuntu from the package channel
   (`sudo apt install lcm-agent`). For the other distributions the same
   package is attached to the
   [release](https://gitlab.techeve.de/techeve/lcm/-/releases) as an RPM, APK
   and Arch package - there is no dedicated channel for those yet, so the file
   is installed directly:

   ```sh
   sudo dnf install ./lcm-agent-<version>.x86_64.rpm      # RHEL, Fedora, Rocky, Alma
   sudo zypper install ./lcm-agent-<version>.x86_64.rpm   # openSUSE, SLES
   sudo apk add --allow-untrusted ./lcm-agent_<version>_x86_64.apk   # Alpine
   sudo pacman -U ./lcm-agent-<version>-x86_64.pkg.tar.zst           # Arch
   ```

   The agent is a static Go binary with no dependencies; the package only
   places it and its systemd unit. If you want no package at all, fetch the
   bare binary from LCM itself (`GET /api/v1/agent/download/<arch>`).
3. **Set up the service** (`sudo lcm-agent enroll <agent-url> <token>`) - the
   `<agent-url>` points at the **agent port**, e.g.:

   ```sh
   sudo lcm-agent enroll https://lcm.example.com:9320 eyJhZ2VudF…<token>
   ```

Without a repository: the binary is fetched via `curl` directly from the LCM
server (over the **UI/REST port**), while the subsequent `enroll` again uses the
**agent port**.

After enrolling, the agent connects outbound, the server goes **online**, LCM
takes the reported agent version into the inventory, and the first system scan
starts automatically (once the agent has subscribed to its command topic). If the
agent disconnects, the server goes offline and in-flight commands fail immediately
(instead of running into a timeout).

## Regenerate token

**Regenerate token** replaces the credential (e.g. on loss or suspected
compromise): the old secret is invalidated **immediately**, the active session is
dropped, and the agent must re-enroll with the new token. The new plaintext is
again shown only once.

## What differs on an agent server

An agent server has no SSH: SSH-specific actions (SSH hardening, certificate/key
rotation, reconnect) are hidden there or rejected with a clear message. The agent
runs as a **root service** on the target system (no sudo wrapper needed); all
other functions - scans, package updates, Docker monitoring, firewall, DNS,
security tools - run over the agent transport unchanged. The SSH logging
(recorder) and the command limits (connection limiter, runtime watchdog, job
abort) apply exactly as with the SSH transport.
