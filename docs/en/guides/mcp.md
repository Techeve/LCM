---
sidebar:
  order: 23
title: MCP interface (AI agents)
description: Expose read-only server properties to AI agents - over a separate, encrypted and authenticated port.
---

The **MCP interface** (Model Context Protocol) exposes **read-only** properties
of the servers managed by LCM to AI agents. It is deliberately narrow:

- **No secrets.** Passwords, login users, private/public keys, host-key
  fingerprints and tokens are **never** returned. Data goes through a curated
  whitelist DTO.
- **Read-only.** There are no write or configuration tools.
- **Own port + mandatory authentication** via an MCP API key (bearer token).
- **Always encrypted.** MCP speaks HTTPS only.
- **Fully toggleable** with one switch.

## Enabling

The *Settings → MCP* page shows the **MCP interface** switch, the state, the
effective address with a copy button, the MCP keys and a ready-made example.

1. Turn on **MCP interface**. The separate listener starts immediately (no
   restart); turning it off stops it just as immediately. The same setting is in
   *Settings → General* below the MCP address
   ([Public addresses & address mode](/en/guides/adressen/#switching-agent-and-mcp-on-and-off)).
2. You set **address and port** in *Settings → General* in the **Public
   addresses** block, not on this page (default port `9330`). The page only shows
   what follows from it.
3. Create an **MCP API key** (its own separate list on the same page). The
   plaintext is shown **once**.

In address mode C, MCP has a name of its own on the shared port. If the name is
missing, MCP is off and the switch stays locked until you enter a name under
*Settings → General*.

## Encryption and address

MCP **always speaks HTTPS**, there is no switch for it. LCM picks the
certificate from the pool by the name under which the client calls MCP. The
listener binds to the same address as the web UI (`host` in the `config.json`),
there is no field of its own for that. Clients call `https://<name>:9330/mcp`,
over the shared port (mode C) without a port.

:::caution[`http://` becomes `https://`]
Up to 2.2, MCP spoke plain HTTP without TLS and listened locally only. From 2.3
that is gone, on existing installations too. An MCP client or reverse proxy that
still calls `http://` has to switch to `https://`. The proxy then has to speak
HTTPS to MCP as well.
:::

Which certificate applies to the name you set at the gear of the MCP address
under *Settings → General*. With Let's Encrypt the certificate carries the MCP
name as soon as MCP is on. Everything about it is under
[Certificates, Let's Encrypt & port 443](/en/guides/zertifikate/#mcp-with-tls).
Who may connect to MCP is limited by the
[Allowed networks](/en/guides/netzbereiche/).

## Security: isolated scope

An MCP API key has its **own scope `mcp`** and is strictly separated from the rest
of the application:

- It works **only** on the MCP listener (`POST /mcp`).
- On the **REST API/UI** an MCP key is rejected with **403 Forbidden** - it can
  neither read nor write there.
- Conversely, normal API keys (`read`/`readwrite`) do **not** work on the MCP
  endpoint.
- If the bearer token is missing or invalid, the endpoint responds with **401
  Unauthorized** (header `WWW-Authenticate: Bearer`).

That way an agent key intended for read-only server data can never accidentally
act with write access.

## Tools

| Tool | Purpose |
| --- | --- |
| `list_servers` | All servers with their read-only properties (OS, version, status, reachability, hardware, update state, security posture). |
| `get_server` | One server by id or exact name. |
| `fleet_summary` | Aggregate: total count, reachable, status distribution, servers with updates/critical CVEs. |

### Return fields (`list_servers` / `get_server`)

Both tools return the same server view. The whitelist covers **only** these
fields - deliberately no credentials:

| Field | Meaning |
| --- | --- |
| `id`, `name`, `host`, `ip_addresses` | identifier & addresses |
| `os_name`, `os_version`, `os_id` | operating system |
| `transport` | `ssh`, `agent` or `routeros` |
| `reachable`, `last_seen_at` | reachability |
| `status` | traffic light: `excellent` / `green` / `yellow` / `red` |
| `insights` | plaintext findings behind the status (e.g. "firewall not active") |
| `cpu_model`, `cpu_cores` | CPU |
| `mem_total_mb`, `mem_used_mb` | memory |
| `disk_total_mb`, `disk_used_mb`, `disk_usage_percent` | storage |
| `kernel_version`, `virtualization` | kernel & virtualization |
| `outdated_packages`, `reboot_required` | package/update state |
| `routeros_channel`, `routeros_latest_version`, `routeros_update_available` | RouterOS-specific |
| `cve_critical`, `cve_high` | vulnerability counts |
| `ssh_hardened`, `firewall_active`, `firewall_tool` | security posture |
| `hardening_index`, `proxmox_type` | hardening index & Proxmox role |

### `fleet_summary`

Aggregates all visible servers. Example result:

```json
{
  "total": 12,
  "reachable": 11,
  "unreachable": 1,
  "by_status": { "excellent": 5, "green": 3, "yellow": 3, "red": 1 },
  "updates_available": 4,
  "servers_with_critical_cve": 1
}
```

`updates_available` counts servers with outdated packages **or** an available
RouterOS update; `servers_with_critical_cve` counts servers with at least one
critical CVE.

## Connecting

The settings page shows a ready-made example with your effective address. For an
MCP client with HTTP transport (e.g. Claude Desktop / VS Code MCP clients):

```json
{
  "mcpServers": {
    "lcm": {
      "type": "http",
      "url": "https://lcm.example.com:9330/mcp",
      "headers": { "Authorization": "Bearer <YOUR_MCP_KEY>" }
    }
  }
}
```

Several instances can be registered in parallel - each with its own key:

```json
{
  "mcpServers": {
    "lcm-main": {
      "type": "http",
      "url": "https://mcp.example.com/mcp",
      "headers": { "Authorization": "Bearer <MAIN_KEY>" }
    },
    "lcm-lab": {
      "type": "http",
      "url": "https://lab.example.com:9330/mcp",
      "headers": { "Authorization": "Bearer <LAB_KEY>" }
    }
  }
}
```

(The `lcm-main` URL here stands for mode C on the shared port 443, `lcm-lab` for
own ports.)

Test directly (JSON-RPC 2.0 via curl). If the name carries no certificate from a
public authority, curl trusts the LCM certificate only with `--cacert`. As an
admin you download the file from `GET /api/v1/system/tls/ca.pem`:

```bash
# 1) list available tools
curl -s --cacert lcm-ca.pem https://lcm.example.com:9330/mcp \
  -H "Authorization: Bearer <YOUR_MCP_KEY>" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'

# 2) fetch one server by name
curl -s --cacert lcm-ca.pem https://lcm.example.com:9330/mcp \
  -H "Authorization: Bearer <YOUR_MCP_KEY>" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call",
       "params":{"name":"get_server","arguments":{"server":"web01"}}}'

# 3) fleet aggregate
curl -s --cacert lcm-ca.pem https://lcm.example.com:9330/mcp \
  -H "Authorization: Bearer <YOUR_MCP_KEY>" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call",
       "params":{"name":"fleet_summary","arguments":{}}}'
```

## Protocol & operations

- The endpoint speaks **JSON-RPC 2.0** over `POST /mcp` (MCP "Streamable HTTP",
  stateless). Methods: `initialize`, `tools/list`, `tools/call`, `ping` plus the
  notifications `notifications/initialized` / `notifications/cancelled`
  (acknowledged with HTTP 202).
- Offered protocol version: `2025-06-18`; if the client requests a different one,
  the server mirrors it in `initialize`.
- `GET /mcp` is not used (no server-initiated SSE) and answers with **405 Method
  Not Allowed**.
- The switch and the port can be changed at runtime at any time, without
  restarting the application. The port is in *Settings → General*.
- Through the API (`PUT /api/v1/settings/mcp`) LCM accepts only `enabled`. It
  ignores other fields.
