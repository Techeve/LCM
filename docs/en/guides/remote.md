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

How the SSH connection works is covered under [SSH connection](/en/guides/ssh/).

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
apply as with SSH. It needs an `lcm-agent` from 2.2 on - older agents do not
report the capability, and the button is then greyed out with a hint to update the
agent.

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
2. **Install the agent.** From the package source the previous command set up,
   with the server's package manager (from LCM 2.4 the UI offers the system as a choice):

   ```sh
   sudo apt install lcm-agent                                         # Debian, Ubuntu
   sudo dnf install --setopt=disable_excludes=techeve-lcm lcm-agent   # RHEL, Fedora, Rocky, Alma
   sudo zypper install lcm-agent                                      # openSUSE Leap, Tumbleweed
   sudo apk add lcm-agent                                             # Alpine
   sudo pacman -Syu lcm-agent                                          # Arch
   ```

   The agent is a static Go binary with no dependencies; the package only
   places it and its systemd unit (an OpenRC script on Alpine). If you want no package at all, fetch the
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
agent disconnects, the server goes offline. New commands fail immediately (instead
of running into a timeout). A command that is already running on an agent from 2.4 on
keeps running on the server when the connection drops or the agent restarts: LCM
waits for the agent to come back and receives the result afterwards. It is only
stopped when you abort the job explicitly. From LCM 2.4 and agent 2.4: if LCM itself restarts (for example for
an LCM update) while a command is running, the job first shows as interrupted and the
server is free again. As soon as the agent reports back, LCM enters its result
(success or error, exit code and output) into that job; further steps of the job do
not run any more, and the next scan brings the inventory up to date. Older agents
(before 2.4) fail running commands on disconnect; updating the agent fixes
that. Note: with an older LCM server a
disconnect still fails the call while the job keeps running on a new agent, so a
retry may run the command twice.

## Update the agent

Applies from LCM 2.4. The hold under apt and the rollback also require agent 2.4.

The agent is often the only way into a server. That is why it never updates on its
own or on the side: from agent 2.4 the `lcm-agent` package holds itself back from a
plain system update (including `unattended-upgrades`; under dnf, zypper, apk and pacman
this has applied since 2.3.2). A new version arrives only when
LCM tells it to - with the button or through a rule of the group. Which systems
support this and what has been tested is listed under
[Supported systems](/en/reference/supported-systems/#lcm-agent). How the package holds
itself depends on the package manager:

| Package manager | Hold | LCM installs with |
| --- | --- | --- |
| apt (from agent 2.4) | pin in `/etc/apt/preferences.d/lcm-agent` | `apt-get install lcm-agent=<version>` |
| dnf | `exclude=lcm-agent` in the `techeve-lcm` repository | `dnf install --setopt=disable_excludes=techeve-lcm lcm-agent-<version>` |
| zypper | package lock (`zypper addlock`) | lift the lock, `zypper install --oldpackage lcm-agent=<version>`, set the lock |
| apk | `lcm-agent=<version>` in `/etc/apk/world` | `apk add lcm-agent=<version>` |
| pacman | `IgnorePkg = lcm-agent` in `/etc/pacman.conf` | `pacman -S` or `pacman -U` (both override `IgnorePkg`) |

On pacman the repository database holds only the newest version; older files sit in
the repository directory, and LCM fetches them with `pacman -U` when needed (pacman
checks the signature). The package formats write versions differently
(`2.4.0~beta.1`, apk `2.4.0_beta1`, Arch `2.4.0beta.1`); LCM converts them.

**Target version.** LCM decides, not the package manager: the **highest version the
server's package source offers that fits LCM's feature series, including all bug fixes
in it.** LCM 2.3.0 therefore raises the agent to 2.3.7, but never to 2.4.0. There is
only one agent for all channels; servers of an enterprise LCM use it too. That way an
agent fix also reaches servers whose LCM does not have it yet, for example because the
enterprise channel lags behind. The agent never gets a higher feature series than LCM.
An LCM in a pre-release (beta) takes agents exactly up to its own version.

| LCM | Source offers | Agent becomes |
| --- | --- | --- |
| 2.3.0 (stable) | 2.2.1, 2.3.0, 2.3.4, 2.4.0 | 2.3.4 |
| 2.2.0 (enterprise, behind community) | 2.2.0, 2.2.1, 2.3.0 | 2.2.1 |
| 2.3.0 (stable) | 2.3.0\~beta.4, 2.2.1 | 2.2.1 (a stable LCM never takes a pre-release) |
| 2.4.0-beta.2 (beta) | 2.3.0, 2.4.0\~beta.1, 2.4.0\~beta.3 | 2.4.0\~beta.1 |
| 2.3.0 | agent already on 2.3.4 | no change |
| 2.3.0 | agent on 2.5.0 | no change - the agent is newer than LCM, update LCM |

An agent is never moved to an older version.

**With the button.** In the detail view of an agent server, **Update agent** first
shows what happens (installed version, target version, effect) and asks for
confirmation. The button sits in the "Agent outdated" band and - for an agent behind
LCM - in the button bar. It needs the `servers:write` permission.

**The job** (`Update agent`) runs through the agent like any other:

1. Refresh the package lists and read the versions of `lcm-agent`.
2. Pick the target version. If the agent is already on it, the job ends without a change.
3. Install exactly that version (under apt, LCM waits for busy apt locks and finishes
   interrupted installations) and read the installed version back. If the package
   manager reports a different one, the job fails at once.
4. Wait until the agent reports back with the new version. It switches to the new
   program on its own as soon as it has nothing left to do (at the earliest after 30
   seconds; open web consoles delay the switch by up to 30 minutes). LCM waits up to
   45 minutes. The server stays reserved for this job in the meantime.
5. Send a command through the new agent: connected does not yet mean able to answer.

Only then does the job count as successful. If the agent does not come back, the job
fails after 45 minutes; if it reports a rollback, the job fails at once (see
[When the update fails](#when-the-update-fails)).

**Source.** On managed servers `lcm-agent` always comes from the **community source**
that the server setup created (`setup.sh`); the enterprise channel carries
no `lcm-agent` and is only set up on the LCM server itself. The update therefore needs
no additional source, not even with an enterprise LCM. If a server has set up the beta
source, a beta LCM can pick beta agents too.

**Requirements.** The package must be installed through the server's package manager
(apt, dnf, zypper, apk or pacman) from the Techeve source: an agent copied by hand
(`/usr/local/bin/lcm-agent`) cannot be updated from LCM - the dialog names the reason.
The same applies to systems with `yum` but no `dnf` (CentOS 7, Amazon Linux 2): the
agent runs there but is updated by hand. The minimum agent version is 2.0.0; below it
LCM only offers the update. Agents from 2.0.0 on keep working; functions that only a
newer agent has are greyed out.

### When the update fails

Applies from LCM 2.4 and agent 2.4.

A broken new program cannot roll itself back. That is why the rollback lives in the
service's start script: it keeps the previous, proven program ready and starts it when
the new one does not become healthy. This happens in two cases:

- The new program **crashes repeatedly** (three starts in a row without becoming
  "healthy"). This shows after about half a minute.
- It runs but **does not become ready in time** (5 minutes without a connection to
  LCM). This shows after six to seven minutes.

The agent reports the rollback with the failed and the running version. After that:

- The job shows as **failed** in the history, with the reason ("crashed repeatedly" or
  "did not become ready in time").
- LCM aligns the package with the running program. Otherwise the package manager would
  list the failed version, and a new attempt would take the agent for up to date. If
  the running version is no longer in the source, the package state stays as it is; the
  agent runs anyway.
- The server's status pill shows the finding "Agent update failed", and the server
  detail shows a band with **Try again** (see
  [Agent status and findings](#agent-status-and-findings)). The button needs the
  `servers:write` permission and starts the same job again.
- The finding stays until an update to a different version succeeds; restarting the
  agent does not clear it. **Try again** clears it before the attempt.
- A group rule skips the server and pauses (see below).

Not every failure leads to a rollback. The agent only rolls back if there is a different
previous program, LCM is reachable from the server and LCM has not rejected the agent
(token revoked, server deleted, agent access off): otherwise the network or LCM is at
fault, not the new program. The agent then does not report back, and the job ends after
45 minutes. The same applies to agents before 2.4, which cannot report a rollback. In
each of these cases the server's journal (`journalctl -u lcm-agent`) and
[`lcm-agent status`](#lcm-agent-status-on-the-server) help.

### Group rule: update automatically

Applies from LCM 2.4.

Under **Groups → Overview → Agent updates** you can have LCM bring the agents of a
group up to date itself. **Off** is the default. When the rule is on:

- **Time window** like the schedules: start as a cron expression (default 03:00) and
  length in minutes. Servers start evenly spread within the window.
- **The first server goes ahead:** every run begins with one server of the group (always
  the same one, by name). Its job checks what the source offers, including a new bug fix
  while LCM stays on its version. After that only the servers whose agent is below it
  follow; if there is nothing new, the run stays at that one job.
- **Staged** (default): the rest follows only when the first server has reported back with
  the new version and answers commands, and at most up to the version it reached.
- **Pausing:** if the first server of a staged rule fails or any server reports a
  rollback, the rule does not continue for this LCM version. **Resume** (or saving the
  rule) lifts the pause, as does a new LCM version. The rule skips servers that rolled
  back until somebody has looked at them; likewise servers that are offline or in
  maintenance.
- **After an LCM update:** with the new LCM version the agents may catch up. If a window
  is open right then, the run begins a few minutes after LCM has started (the agents
  reconnect by then); otherwise the next window takes over.
- **Run now** starts a run immediately, regardless of the window.

The rule needs the `rules:manage` permission; anyone who may only see the group sees
the setting and the state of the last run. The jobs appear under **Jobs** as usual.

## Agent status and findings

Applies from LCM 2.4, except for "No connection to the server possible".

A server has exactly **one status pill** (calculation:
[Status calculation](/en/guides/status/)). Findings about the agent go into it as a
reason, with text; there is no second indicator next to it. The server detail also shows
a band with the matching button.

| Finding | Pill | Meaning | What to do |
| --- | --- | --- | --- |
| No connection to the server possible | red | The agent is disconnected, the server offline. | On the server, look at `lcm-agent status` and `systemctl status lcm-agent`, then check network, proxy and agent port. |
| Agent too old | red | The agent is below the minimum version 2.0.0. LCM only offers **Update agent**. | Update the agent. |
| Agent update failed | yellow | The last update was rolled back, the agent runs on its previous version (see [When the update fails](#when-the-update-fails)). | Look at the server's journal, then **Try again**. |
| Agent outdated | yellow | The agent does not know functions that LCM offers. What it cannot do is greyed out. | Update the agent. |
| Agent newer than LCM | yellow | The agent is newer than this LCM. LCM only offers what it knows itself. | Update LCM. |
| Connection unstable | yellow | The agent was disconnected unintentionally more than five times in the last hour (network error, missing sign of life, duplicate connection). Intentional disconnects do not count. | Check network, proxy timeouts (see [Behind a reverse proxy](#behind-a-reverse-proxy)) and the journal; look at `lcm-agent status` on the server. |

If the agent is both below the minimum version and rolled back, "Agent too old" applies.
Otherwise "Agent update failed" replaces the finding "Agent outdated": it says why the
agent lags behind. LCM counts the connection balance since its own start; restarting LCM
resets it.

### `lcm-agent status` on the server

Applies from agent 2.4.

When LCM cannot reach the agent, the view from the other side helps. On the agent server,

```sh
lcm-agent status
```

shows the service's connection, running jobs and update state, without `sudo`:

```text
lcm-agent:         2.4.0
Connection:        connected for 3 h
Last contact:      4 s ago
Reconnects:        2 since start
Last disconnect:   3 h ago - <reason>
Jobs:              none
Binary:            proven
```

- **Connection** and **Last contact**: whether the agent is currently connected to LCM and
  when LCM last got in touch (connection, ping or command).
- **Reconnects** and **Last disconnect**: how often it has reconnected since the start
  and why the last connection ended.
- **Jobs**: running commands and finished ones whose result LCM has not confirmed yet.
- **Binary**: *proven*, or *unproven* if the program is new from an update and not yet
  confirmed healthy. After a rollback there is an extra line *Rolled back* with the
  failed and the running version.

The output is English, or German on a system set to German. `lcm-agent status --json`
prints the raw data.

The service writes the state every 10 seconds and on every change to the file
`/run/lcm-agent/status.json`; the service (systemd or OpenRC) creates the directory
`/run/lcm-agent`. The file holds no secrets and is readable by everyone. The command only
reads it and opens no access to the agent. Under systemd it survives an automatic restart,
so the last state of a crashed agent stays readable, and it disappears on an intentional
stop.

If the file is older than 30 seconds, the agent no longer writes it: the command says the
agent probably is not running and shows the last known state. If the file is missing, it
says so too. In both cases it exits with an error code, which scripts can check.

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
