---
sidebar:
  order: 24
title: Public addresses & address mode
description: How LCM is reached from outside - three address modes, ports, switches for agent and MCP, reverse proxy, the config.json in step with the interface and the setup assistant.
---

LCM has three services: the web UI with the REST API, the agent port for LCM
Remote and the MCP interface. Under *Settings → General*, in the **Public
addresses** block, you set the names and ports that browsers, agents and MCP
clients use to reach them. At the top you choose the **address mode**. The rest
of the block follows that choice: address fields, ports, switches for agent and
MCP, Let's Encrypt. A help column beside it (below it on narrow screens) matches the chosen
mode.

LCM builds everything that points outward from these addresses: the
`lcm-agent enroll` command, the MCP address, the *Add server* assistant and the
links in emails. LCM also takes the names for the Let's Encrypt certificate from
them ([Certificates, Let's Encrypt & port 443](/en/guides/zertifikate/)).

## The three address modes

| Mode | Ports | Names | Fields | When it fits |
|---|---|---|---|---|
| **A: Own ports, own names** (default) | one per service, adjustable (defaults 9310, 9320, 9330) | one address per service, with or without a port | three address fields and three port fields | A reverse proxy sits in front, or each service should run under its own name. This is how earlier versions worked. |
| **B: Own ports, one name** | one per service, adjustable | one name for all services | one name field and three port fields | Operation without a proxy, with one certificate. |
| **C: One port, own names** | one shared port, default 443 | one name of its own per service; required only for the web UI | three name fields and *Shared port* | No port in the browser. LCM tells the services apart by the requested name (SNI). |

**Mode A.** You enter the addresses freely, such as `https://lcm.example.com`. If
you leave the address for agents or MCP empty, LCM uses the name of the web UI
with the port of the service. Without an address for the web UI, LCM shows a
warning: links in emails then come from the local configuration and may not be
reachable from outside.

**Mode B.** You enter one name, without `https://`, port and path. LCM works out
the three addresses from it and shows them read-only below, for example
`https://lcm.example.com:9310`, `https://lcm.example.com:9320` and
`https://lcm.example.com:9330`.

**Mode C.** The web UI needs a name. Agent and MCP each get a name of their own,
but do not have to have one. All names point to the LCM host in DNS. The
addresses are `https://<name>:<port>`, without the port at 443. Without a name
(call by IP) and for any unknown name, the connection goes to the web UI. A
service without a name is off, see
[Switching agent and MCP on and off](#switching-agent-and-mcp-on-and-off).

When you switch modes, what is already in the form stays. Empty ports get their
default, and when you switch to mode C the page cuts the addresses down to their
names. Nothing is saved until you click *Save*.

### What is checked on save

- **Ports:** Each port is between 1 and 65535, and no port serves two services.
  The port of the Let's Encrypt check (`acme_http_listen`, default 80) is taken
  as well. The shared port only counts in mode C.
- **Swapping:** LCM refuses a port that another service still holds, even when
  you swap the web UI and the agent. Move to a free port first.
- **Mode B:** There is exactly one name.
- **Mode C:** The name of the web UI is required. The names that are set differ.
  You cannot switch on a service without a name.

Existing installations keep running in mode A. If the `config.json` has a
`shared_tls_listen`, LCM starts in mode C with that port on the first start after
the update. After that the mode can be changed in the interface like any other.

## Which ports to open

| Mode | Open on the LCM host | In addition |
|---|---|---|
| **A, with a reverse proxy** | web UI, agent and MCP only for the proxy, the agent port also for agents that reach it directly | Only the proxy needs port 443 from outside. |
| **A, without a proxy** | web UI, agent and MCP with their ports (defaults 9310, 9320, 9330) | |
| **B** | web UI, agent and MCP with their ports | Port 80 briefly when Let's Encrypt checks with HTTP-01. |
| **C** | only the shared port (default 443) | Port 80 briefly with HTTP-01. |

The web UI binds to `host` from the `config.json`. MCP binds to the same address,
it has no field of its own for that. If someone should reach them from outside,
change `host`. The agent port binds to `agent_host`, to all interfaces out of the
box.

In mode C the single ports keep listening. Block them in the firewall if only the
shared port should be open. You switch the agent port off with the switch for the
agent, MCP with its switch.

LCM needs port 80 only for HTTP-01, and only while the check runs. With DNS-01
you never need it.

The service of the package installation may open ports below 1024: `lcm.service`
grants it exactly one capability for this (`CAP_NET_BIND_SERVICE`). You need not
set anything. All other hardening in the service file stays.

## Mode A behind a reverse proxy

The proxy gets a route per name to the matching port of LCM. It has to pass on
three things:

| Service | Port | What the proxy passes on |
|---|---|---|
| **Web UI** | `ui_port` (9310) | All paths over HTTPS. WebSocket for `/api/v1/events` (live display) and `/api/v1/servers/<id>/terminal` (web console), server-sent events for `/api/v1/system/logs/stream` without buffering. |
| **Agent** | `agent_port` (9320) | `/mqtt` as a WebSocket (subprotocol `mqtt`) with upgrade forwarding. The connection stays open for good: the read timeout has to be well above 30 seconds, better an hour. |
| **MCP** | `mcp_port` (9330) | `/mcp` over HTTPS. MCP always speaks TLS, so the proxy has to speak HTTPS to this port as well. |

For all three:

- The proxy leaves the host name unchanged and sets `X-Forwarded-Proto` and
  `X-Forwarded-For`. In LCM, `trust_proxy_header` and `trusted_proxies` (the
  address of the proxy) belong in the settings, in the setup assistant at the
  "Certificate" step under "Reverse proxy in front". They take effect at once,
  without a restart.
- Pass the agent port through with TCP passthrough if you can. If the proxy
  terminates TLS, it sees the agents' management channel.
- Enter the names under which the proxy publishes the services in the address
  fields.

Ready-made configurations are in
[Reverse proxy & external hardening](/en/guides/reverse-proxy/) for the web UI
and in [LCM Remote](/en/guides/remote/#behind-a-reverse-proxy) for the agent.

If the proxy has a certificate of its own, you do not need Let's Encrypt in LCM.
If you want it anyway, use DNS-01. With HTTP-01 port 80 would have to point to
LCM, and behind a proxy that usually does not work.

## Switching agent and MCP on and off

Below the address of agent and MCP there is one switch each. Both take effect
without a restart.

- **Agent:** On out of the box, existing installations stay on. When it is off,
  the agent port accepts no connection, and connected agents disconnect. In mode C
  the agent name gets no answer. Before you switch it off, the page tells you how
  many servers use the agent: they go offline when you save. The *Add server*
  assistant then creates no enroll command and points to the settings instead.
- **MCP:** When it is off, LCM accepts no MCP requests. Existing MCP keys stay but
  have no effect.
- **A port alone switches nothing on.** In mode B the preview of the addresses
  shows "(not active)" behind agent and MCP while their switch is off.
- **Modes A and B:** Without an address of their own, agent and MCP carry the name
  of the web UI. The switches are free.
- **Mode C without a name:** If the name of agent or MCP is missing, the service is
  off and its switch is locked, with the note "No domain is set up for this
  service." If you enter a name, the switch jumps to on. You can switch it off
  again before saving. If you save the name empty, LCM switches the service off
  with it. Through the API, `PATCH /api/v1/settings/global` refuses to switch on
  without a name, with `settings.invalid`.

The switches sit in two places, and each is the same setting: the agent switch in
the **Onboarding** card ([Onboarding](#onboarding)), the MCP switch on the *MCP*
page ([MCP interface](/en/guides/mcp/)).

## Changing ports

In modes A and B you set the ports of the web UI, the agent and MCP in the
interface, in mode C the shared port. The fields carry the defaults from the
table above.

- **Port of the web UI:** LCM binds the new port first. If that fails (taken, no
  permission), the old port stays in operation and LCM saves nothing. Before you
  save, the page names the new address. After saving it redirects you there. The
  old port closes after 10 seconds, open connections are left alone.
- **Agent port, MCP port and shared port:** LCM switches them without a restart.
  The same goes for the switches of agent and MCP.
- **Agents that are already set up** carry the address from their setup. LCM
  sends them no new one. After changing the agent port or the name, set them up
  again with a new token (`lcm-agent enroll`).
- You adjust the firewall yourself. The help beside the block names the ports of
  the chosen mode.

### The `config.json` in step with the interface

The interface and the `config.json` carry the same values. The table shows which:

| Key in the `config.json` | Setting |
|---|---|
| `address_mode`, `public_host` | address mode and the one name (mode B) |
| `public_base_url`, `public_agent_url`, `public_mcp_url` | the addresses |
| `port`, `agent_port`, `mcp_port` | the ports of web UI, agent and MCP |
| `shared_tls_listen` | the shared port, mode C only (empty otherwise). A host in front (`127.0.0.1:443`) sets the bind address and is kept. |
| `agent_enabled`, `mcp_enabled` | the switches for agent and MCP |
| `trust_proxy_header`, `trusted_proxies` | where LCM takes the client address from, and which proxies it believes |
| `allowed_ips`, `agent_allowed_ips`, `mcp_allowed_ips` | the [allowed networks](/en/guides/netzbereiche/) |

- **Saving in the interface** writes the values into the file, atomically and with
  the permissions it has. All other keys (certificates, paths, secrets) stay as
  they are. If writing fails, the change still applies, and the response carries
  `config_sync_error`.
- **On start** LCM reads the file. If you changed a value by hand, it applies and
  goes into the settings; the log names every adopted key. LCM checks it like an
  input in the interface. If it is invalid, the value in the settings keeps
  applying, and the log warns on every start until you correct the file. A missing
  key is added with the value from the settings.
- **Locked** is only what an environment variable sets, because LCM cannot change
  it: `LCM_PORT` locks the port of the web UI, `LCM_AGENT_PORT` that of the agent
  (`0` switches the agent off). The interface then shows the field locked, with the
  note "set by an environment variable". A value in the `config.json` locks
  nothing.
- **Let's Encrypt** is not in the `config.json`, only in the interface.
- **Old files:** An `agent_port` of `0` meant "agent off". LCM adopts it as
  `agent_enabled: false` and keeps the previous agent port. In mode C this does not
  apply: there `0` only means no port of its own next to the shared one.

## Access, certificate and Let's Encrypt in the block

- **Gear behind an address:** *Certificate & security*. The dialog has two parts.
  At the top is the certificate LCM serves for the name. You can upload one by
  hand or remove it again. Below it are the **allowed networks** of this service
  ([Allowed networks](/en/guides/netzbereiche/)). There is no separate *Access*
  section in the block.
- **Let's Encrypt:** collapsed, available in every mode. The names come from the
  addresses, so there is one field fewer than before. After you save, LCM issues
  the certificate by itself
  ([Certificates, Let's Encrypt & port 443](/en/guides/zertifikate/#lets-encrypt-acme)).

Only admins see these parts (right `system:endpoints`).

## Onboarding

*Settings → General*, **Onboarding** card: this is where new servers get to LCM.
The card has three collapsed sub-cards.

1. **Defaults for new servers:** the user and port LCM pre-fills for new servers.
2. **Onboarding key:** the public key of LCM with a copy button. The guide below
   it shows how the key gets onto the server: for Debian, Ubuntu and Linux Mint,
   for the RHEL family (Rocky, Alma, Fedora) and for other systems. It names user,
   directory and permissions (`~/.ssh/authorized_keys`, directory `700`, file
   `600`).
3. **Use with agent:** what the agent is and when it fits, the switch for the
   agent, the address agents connect to (in mode B with the agent port) and a
   button to the *Add agent server* assistant. Without a domain in mode C the card
   points to the address block.

## Setup assistant

On a fresh installation the assistant leads the first administrator step by step
through the setup after the password change and the two-factor setup. Each step
saves on its own and uses the same building blocks as the settings. The server
remembers the position (`setup_step`), so the assistant carries on at the same
place after the address changed and you signed in again. It only moves forward.

| Step | Content |
|---|---|
| 1. **How is LCM reached?** | Address mode, addresses and ports as in the settings block. This step cannot be skipped. |
| 2. **Which services should run?** | One switch each for agent and MCP, with one sentence of explanation. In mode C only for services with a name, the others show as "no domain set up". |
| 3. **Who may reach LCM?** | Per service "all networks" or networks of your own, for the web UI and the services that are on. You save each group on its own. |
| 4. **Certificate** | The step says that LCM has no valid certificate for your own names without help. You choose *Let's Encrypt* (the block below it as in the settings, saving issues) or *Reverse proxy in front* (below it the addresses of the proxy whose client address LCM believes: `trust_proxy_header` and `trusted_proxies`). With neither and no uploaded certificate, LCM serves its own. Then the assistant redirects to the new address of the web UI. If the name changes, you sign in again and carry on with the next step. |
| 5. **Adding new servers** | The default user for new servers and the onboarding key with a copy button, plus the hint to deposit it at your provider right away (Hetzner: SSH key in the project, Proxmox: cloud-init, otherwise `authorized_keys`). Can be skipped. |
| 6. **Automatic backup** | On or off, interval, time, retention and the passphrase, because scheduled backups are encrypted. Can be skipped. LCM takes a backup before every update started through LCM anyway. |

Skipping means: "You can set this up later in Settings." After the sixth step LCM
opens the page of the **LCM host** in the server list. An introduction explains
that the host LCM runs on is itself a server in the list, and offers more
services to install: **Trivy** (the CVE scanner, recommended, runs in a sandbox),
**apt-cacher-ng** (a cache for packages) and **CrowdSec LAPI** (a shared block
list). You can close the introduction, and it does not come back.

Later you change everything under *Settings → General*. The assistant does not
appear in demo and development mode or on existing installations.

## Through the API

`GET` and `PATCH /api/v1/settings/global` know these fields:

| Field | Meaning |
|---|---|
| `address_mode` | `separate` (mode A, default), `one_name` (B) or `shared_port` (C) |
| `public_host` | mode B: the one name, without scheme, port and path |
| `public_base_url`, `public_agent_url`, `public_mcp_url` | the addresses in modes A and C |
| `ui_port`, `agent_port`, `mcp_port` | the ports in modes A and B |
| `shared_tls_port` | the shared port in mode C, default 443 |
| `agent_enabled`, `mcp_enabled` | the switches for agent (default on) and MCP |
| `trust_proxy_header` | LCM believes the client address from `X-Forwarded-For`, but only from `trusted_proxies` |
| `trusted_proxies` | list of IP addresses or networks (CIDR); LCM rejects `0.0.0.0/0` and `::/0`, and an empty list while `trust_proxy_header` is on |
| `setup_step` | the step of the setup assistant (`addresses`, `services`, `access`, `certificate`, `onboarding`, `mail`, `retention`, `backup`, `subscription`, `intro`), empty = done. The `PATCH` may move forward and back between `session` and `subscription`, from there also to `intro` or empty; from `intro` only to empty. It is also in `GET /api/v1/auth/me`. |

Only the response carries `address_locks` (per field true if an environment
variable sets it), `agent_servers` (the number of servers that use the agent, for
the question before switching off) and `effective_urls` (the addresses LCM
actually writes into commands, derived in mode B). If a `PATCH` changes the port of the web UI, the response
also carries `ui_url`: the address the browser should move to. If LCM could not
write the `config.json` after saving, `config_sync_error` carries the reason. The full requests
and responses are in the OpenAPI specification ([REST API](/en/reference/rest-api/)).
