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
| Key rotation, reconnect needed | **SSH** (these actions are hidden on the agent) |

A server is either one **or** the other - the transport is fixed at creation time.

## Dedicated agent port

Agent communication runs on its **own dedicated port** - separate from the web
interface and the REST API:

- The **agent port** carries **only** the agent endpoint (`GET /mqtt`). No UI, no
  REST API, no static files; everything else on this port answers with 404.
- The **UI/REST port** exposes **no** agent interface.

| Setting | Meaning | Default |
| --- | --- | --- |
| `agent_port` | Port of the agent listener; an old `0` is read by LCM as "agent off" (`agent_enabled: false`) | `9320` |
| `agent_host` | Bind address of the agent listener | `0.0.0.0` |

Both live in `config.json` and can be overridden via environment variables
(`LCM_AGENT_PORT`, `LCM_AGENT_HOST`) - `LCM_AGENT_PORT=0` disables the agent
listener entirely. The agent port binds to **all interfaces** (`0.0.0.0`) by
default, because agents connect from the outside; the UI stays on its own bind
address (default `127.0.0.1`, usually behind a reverse proxy).

**Agent on and off.** Under *Settings → General* the agent has a switch, in the
*Onboarding* card and below its address in the *Public addresses* block (on out of
the box). When it is off, the agent port accepts no connection, connected agents
disconnect, and the *Add server* assistant creates no enroll command. Before you
switch it off, the page tells you how many servers use the agent
([Public addresses & address mode](/en/guides/adressen/#switching-agent-and-mcp-on-and-off)).

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
The agent listener serves the certificate by the name under which the agent calls
it: a certificate of your own that names this name, otherwise the LCM certificate
(see [Certificates, Let's Encrypt & port 443](/en/guides/zertifikate/)). At
enrollment the **SHA-256 fingerprint** of the certificate LCM serves for the name
of the agent address is embedded into the token; the agent pins it on first
contact (MitM protection, analogous to SSH fingerprint confirmation). Open the
agent port (default **9320**) in the firewall so agents can reach it. In `--dev`
mode the listener runs without TLS (HTTP) and without a pin - for local testing
only.
:::

## Without a reverse proxy: agent over port 443

The agent needs no proxy in front. In address mode C (or with
`shared_tls_listen: ":443"` in the `config.json`) LCM accepts the web UI, agent
and MCP on one port and tells them apart by name
([Public addresses & address mode](/en/guides/adressen/)). If you enter
`agent.example.com` as the name for agents under *Settings → General*, you set up
the agent like this:

```sh
sudo lcm-agent enroll https://agent.example.com <token>
```

What the agent trusts:

- **Certificate from a public certificate authority (Let's Encrypt):** The agent
  checks it with its system's trusted certificates and the name of its address.
  This works with every agent.
- **Certificate from your own (private) certificate authority:** The token
  carries the authority. An agent that you set up with such a token pins it and
  trusts every certificate from it that carries the name of its address, even
  after a renewal. Older agents follow only the certificate's fingerprint, their
  remembered key, or the LCM certificate.
- **Call by IP or under an unknown name:** LCM serves the LCM certificate, which
  agents from 2.1 on trust through the remembered LCM certificate authority.
  Switching to a certificate of your own and back disconnects no agent.

More under [Certificates, Let's Encrypt & port 443](/en/guides/zertifikate/#agents-and-certificates).
To restrict where agents may connect from, set
[Allowed networks](/en/guides/netzbereiche/) for the agent.

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

The UI port is an ordinary HTTPS service, but it needs WebSocket forwarding on
two paths: for the web console (`/api/v1/servers/<id>/terminal`) and for the
event channel (`/api/v1/events`) that keeps the interface live. Without the
forwarding the console and the live display are missing (the nginx examples are
in [Reverse proxy & external hardening](/en/guides/reverse-proxy/#nginx)). These
things have to be set (address mode A,
[Public addresses & address mode](/en/guides/adressen/#mode-a-behind-a-reverse-proxy)):

- **`public_base_url`** under *Settings → General*. Activation and reset links
  are built from it - without it they point at the internal address nobody can
  reach from outside.
- **Address for agents** (`public_agent_url`), also under *Settings → General*,
  if the proxy publishes the agent port under its own name (for example
  `https://agent.example.com`). The *Add server* wizard then builds the
  `enroll` command with this address instead of the interface host and the
  agent port. The MCP interface has the same field there (`public_mcp_url`).
- Pass **`X-Forwarded-For`** through and set `trust_proxy_header` in LCM
  **together with** `trusted_proxies` (the proxy's address), otherwise the IP
  allowlist and the failed-login lockout only ever see the proxy's address.
  This applies to the agent port too: its throttle against connection floods
  counts per client address - behind a proxy without the list all agents would
  share one quota. Everything else about the UI behind the proxy:
  [Reverse proxy & external hardening](/en/guides/reverse-proxy/).

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

## Web console

An interactive shell on a server, in the browser - reachable via the
**Terminal** button on the far left of the action bar on the server detail page.

The console is limited at two levels, each of which applies independently:

| Level | Effect |
|---|---|
| **Permission `servers:console`** | governs WHO may use it; by default `admin` only |
| **Server settings → Access → "Disable console"** | disables it for ONE server |

A third, global switch exists only in the stored settings (`terminal_enabled`,
on by default): neither the interface nor the REST API offers it.

The permission is deliberately separate from `servers:write`: whoever may
configure servers should not automatically get a root shell on all of them.
For the same reason the per-server switch also requires the console
permission - otherwise a manager could grant themselves what they are not
entitled to.

If the console is disabled for a server, more than the button disappears: the
server rejects direct calls as well. Where there is no shell it is not offered
at all - demo servers, Synology DSM devices and servers in maintenance.

### Console on agent servers

On agent servers the console runs through the agent connection, so no
inbound SSH port is needed. The agent opens a **login shell as root** in a
pseudo terminal; permission, switches, console limit, audit and recording
apply as with SSH. It needs an `lcm-agent` from this version on - older agents
do not report the capability, and the button is then greyed out with a hint
to update the agent.

The operator of the server can switch the console off there, independent of
LCM:

```sh
sudo lcm-agent terminal off     # switch off
sudo lcm-agent terminal on      # allow again (default)
sudo lcm-agent terminal status
```

The switch is the file `/etc/lcm-agent/terminal-off`. It applies right away;
open consoles end with the next input as well. LCM shows it after the agent's
next reconnect (now: `systemctl restart lcm-agent`).

### Sessions

- **Open consoles are visible**: the server list and server detail show the label
  **"Console open"** with user and start time - everyone knows someone is
  working on the server by hand.
- **LCM keeps working**: a console has its own connection slots (two per
  server) and does not block schedules, updates or actions. If apt runs
  collide, LCM waits for the package manager (see
  [APT cache](/en/guides/apt-cache/#when-apt-is-busy)).
- **End**: *End*, closing the window, `exit` in the shell or **15 minutes
  without input** end the session including the SSH connection (with an agent:
  including the shell; if the agent connection drops, it ends as well). The console
  then has to be reopened. Opening and closing are in the audit log, the
  session itself in the SSH log.

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
   [release](https://gitlab.techeve.de/techeve/lcm/-/releases?mtm_campaign=linking&mtm_kwd=doc) as an RPM, APK
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

## Renew agent token

**Renew agent token** replaces the credential (e.g. on loss or suspected
compromise): the old secret is invalidated **immediately**, the active session is
dropped, and the agent must re-enroll with the new token. The new plaintext is
again shown only once.

## What differs on an agent server

LCM does not sign in to an agent server via SSH. What depends on that
connection - key rotation, reconnect, cleanup on delete, the external port scan -
is hidden there or rejected with a clear message. LCM still manages the server's
sshd: SSH hardening, root login, SSH port and SSH 2FA run as scripts over the
agent. The differences:

- **Port change:** LCM reads the current port from the server, requires a free
  new port and afterwards checks on the server itself that sshd listens there.
  Whether the port is reachable from outside (firewall or NAT in front), LCM
  cannot see through the agent. If the port is set in `sshd_config` itself or in
  a foreign drop-in, LCM aborts: port lines add up, and sshd would keep listening
  on the old port as well.
- **SSH hardening** aborts if no SSH key is present on the server (root,
  `/home/*`, or an `AuthorizedKeysCommand`) - otherwise nobody could sign in via
  SSH afterwards.
- **SSH 2FA** exempts no user, not even root. Instead of the lockout probe, LCM
  checks the effective configuration (`sshd -T`).

The agent
runs as a **root service** on the target system (no sudo wrapper needed); all
other functions - scans, package updates, Docker monitoring, firewall, DNS,
security tools - run over the agent transport unchanged. The SSH logging
(recorder) and the command limits (connection limiter, job watchdog, job
abort) apply exactly as with the SSH transport. The agent reports every 30
seconds that a running command is still working - that is how the watchdog
tells a slow update from a stalled one.
