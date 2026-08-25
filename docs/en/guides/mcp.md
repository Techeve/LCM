---
sidebar:
  order: 23
title: MCP interface (AI agents)
description: Expose read-only server properties to AI agents - over a separate, authenticated port.
---

The **MCP interface** (Model Context Protocol) exposes **read-only** properties
of the servers managed by LCM to AI agents. It is deliberately narrow:

- **No secrets.** Passwords, login users, private/public keys, host-key
  fingerprints and tokens are **never** returned. Data goes through a curated
  whitelist DTO.
- **Read-only.** There are no write or configuration tools.
- **Own port + mandatory authentication** via an MCP API key (bearer token).
- **Fully toggleable** under *Settings → MCP*.

## Enabling

*Settings → MCP*:

1. Turn on **Enable MCP interface**. The separate listener starts immediately
   (no restart); turning it off stops it just as immediately. Changing the bind
   address/port also takes effect at runtime (the old listener is shut down
   cleanly).
2. Set **bind address** and **port** (default `127.0.0.1:9330`). Binding to
   `127.0.0.1` means local access only.
3. Create an **MCP API key** (its own separate list on the same page). The
   plaintext is shown **once**.

:::caution[Plain HTTP - secure remote access]
The MCP endpoint speaks plain **HTTP** (no TLS), matching the local default
bind. If a remote agent needs access, put a **TLS-terminating reverse proxy** or
an SSH tunnel in front - otherwise the bearer token would travel in cleartext.
:::

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

The settings page shows a ready-made example. For an MCP client with HTTP
transport (e.g. Claude Desktop / VS Code MCP clients):

```json
{
  "mcpServers": {
    "lcm": {
      "type": "http",
      "url": "http://127.0.0.1:9330/mcp",
      "headers": { "Authorization": "Bearer <YOUR_MCP_KEY>" }
    }
  }
}
```

Several instances can be registered in parallel - each with its own key:

```json
{
  "mcpServers": {
    "lcm-prod": {
      "type": "http",
      "url": "https://lcm.example.com/mcp",
      "headers": { "Authorization": "Bearer <PROD_KEY>" }
    },
    "lcm-lab": {
      "type": "http",
      "url": "http://127.0.0.1:9330/mcp",
      "headers": { "Authorization": "Bearer <LAB_KEY>" }
    }
  }
}
```

(The `lcm-prod` URL here points at a TLS-terminating reverse proxy.)

Test directly (JSON-RPC 2.0 via curl):

```bash
# 1) list available tools
curl -s http://127.0.0.1:9330/mcp \
  -H "Authorization: Bearer <YOUR_MCP_KEY>" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'

# 2) fetch one server by name
curl -s http://127.0.0.1:9330/mcp \
  -H "Authorization: Bearer <YOUR_MCP_KEY>" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call",
       "params":{"name":"get_server","arguments":{"server":"web01"}}}'

# 3) fleet aggregate
curl -s http://127.0.0.1:9330/mcp \
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
- Enable state, bind address and port can be switched at runtime via *Settings →
  MCP* at any time - without restarting the application.
