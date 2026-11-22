---
sidebar:
  order: 25
title: Allowed networks
description: Sets which networks browsers, agents and MCP clients may connect to LCM from, per service, with protection against locking yourself out.
---

By default any address that reaches a port of LCM may knock on it. The login
then decides who gets in. With **allowed networks** you additionally decide
which networks LCM answers at all. This applies per service:

| Service | Who connects |
|---|---|
| **Web UI** | browsers and API clients (web UI and REST API) |
| **Agent** | the `lcm-agent` on your servers |
| **MCP** | AI assistants through the MCP interface |

A request from an address outside the list gets `403` from LCM, before the login
or a controller runs.

:::note[Not the same as the IP allowlists]
The [IP allowlists](/en/guides/allowlists/) under *Settings → Allowlists* are
named lists for firewall, fail2ban and CrowdSec on the **managed servers**. The
allowed networks on this page protect **LCM itself**.
:::

## Entries

An empty list allows **all** networks. That is the default. Each entry is:

- an IP address (`203.0.113.5`, `2001:db8::1`),
- a network in CIDR notation (`10.0.0.0/8`, `fd00::/8`),
- or one of the words `localhost` (the local machine) and `private` (private
  networks per RFC 1918, IPv6 ULA, link-local, plus the local machine).

A list has at most 64 entries. When saving, LCM writes it uniformly (single
addresses become `/32` or `/128`) and removes duplicates.

## Setting them in the interface

*Settings → General*, block **Public addresses**: a gear (*Certificate &
security*) sits behind the address of the web UI, the agent and MCP. Below the
certificate, the dialog shows the **allowed networks** of that service, with a
field of its own and a *Save networks* button of its own. One entry per line,
saving takes effect **immediately**, without a restart. The change appears in the
audit log (`settings.network.update`).

In the setup assistant you set the same lists in the step *Who may reach LCM?*
([Public addresses & address mode](/en/guides/adressen/#setup-assistant)).

The right for this, `system:endpoints`, belongs to an **admin** only. Neither a
manager with `settings:manage` nor any other account can change allowed
networks.

### Protection against locking yourself out

- **Web UI:** LCM refuses a list that lacks your own address. The local address
  too only counts if it is in the list. Include `localhost`, otherwise for
  example the Docker health check, which asks LCM through `localhost`, fails.
- **Agent:** If you narrow the range, LCM shows the agents that are currently
  connected from outside. Only after you confirm does LCM save and **disconnect**
  them. They can only reconnect once their address is allowed.
- **MCP:** The range applies from the next request.

## In `config.json`

The lists are also in `config.json`, in step with the interface
([Public addresses & address mode](/en/guides/adressen/#the-configjson-in-step-with-the-interface)):

```json
{
  "allowed_ips": ["private"],
  "agent_allowed_ips": ["203.0.113.0/24", "198.51.100.17"],
  "mcp_allowed_ips": ["10.0.0.0/8"]
}
```

| Key | Service |
|---|---|
| `allowed_ips` | web UI |
| `agent_allowed_ips` | agent |
| `mcp_allowed_ips` | MCP |

If you save a list in the interface, LCM writes it into the file. If you change it
in the file by hand, LCM adopts it on start when it is valid. The interface always
shows the ranges editable. That is how you get back in after a lockout from the
console: empty the list in the file and run `sudo systemctl restart lcm`.

## If you locked yourself out

This command resets the list, in the settings and in `config.json`. It runs on
the LCM host and needs no account in LCM:

```sh
sudo lcm network status            # shows each service's list
sudo lcm network reset ui          # web UI: all networks allowed again
sudo lcm network reset agent
sudo lcm network reset mcp
```

`lcm network status` looks like this (the program prints it in German):

```text
Erlaubte Netzbereiche je Dienst:
  Weboberfläche  10.0.0.0/8, 192.168.1.0/24
  Agent          alle Netze erlaubt
  MCP            10.0.0.0/8
```

The commands talk through the control socket `/var/lib/lcm/control.sock`, which
only the service user and root may open. Hence `sudo`. The reset appears in the
audit log (`settings.network.reset`).

## Behind a reverse proxy

LCM checks the address it recognizes as the client. With a proxy in front that
is, without further settings, the proxy's address, and the list would treat all
clients alike. So switch `trust_proxy_header` on **and** enter the proxy's
address in `trusted_proxies` (settings, or both in `config.json`). Then the entry from `X-Forwarded-For` counts, but
only from that proxy. Details and examples:
[Reverse proxy & external hardening](/en/guides/reverse-proxy/#trusted_proxies-whose-x-forwarded-for-is-believed).
This applies to all three services.

## What always stays reachable

- **The public agent download** (`/api/v1/agent/download/`) on the web UI. Whoever
  adds a server downloads the agent from a network you do not need to know.
- **The Let's Encrypt check** under `/.well-known/acme-challenge/` on the HTTP-01
  listener (port 80). The certificate authority comes from the internet. The
  port is open only during the check and answers nothing else
  ([Certificates](/en/guides/zertifikate/#http-01)).
