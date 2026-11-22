---
sidebar:
  order: 22
title: REST API
description: Authentication, permissions, example requests, and the machine-readable OpenAPI specification.
---

The web frontend uses **exactly the same** REST API that is also available
for your own scripts, automation, or integrations - there is no separate
internal interface. All endpoints live under `/api/v1`.

## Authentication

Scripts, automation, and integrations use the **API key**; the web interface
signs in through a browser session:

| Method | Header | For |
|---|---|---|
| API key | `X-API-Key: <key>` | scripts, automation, service-to-service |
| Browser session | `X-LCM-Session: <value>` or cookie | the web interface |

`Authorization: Bearer` does **not** count as a sign-in at the REST API. There
is no username-and-password login for scripts - that is what API keys are for.
(The MCP port is a separate listener and takes the API key of scope `mcp` as a
bearer token, see [MCP interface](/en/guides/mcp/).)

**Browser session:** how the web interface carries the session is set by the
operator under *Settings → General → Sign-in in the browser*. More in the
[Security Model](/en/reference/security-model/#sign-in-in-the-browser).
`POST /auth/login` returns the session value in the response body
(`session_token`) in the default variant, which the interface sends as
`X-LCM-Session`; in the cookie variant it sets the cookie `__Host-lcm_session`
instead. If 2FA is enabled for the account, `/auth/login` returns a challenge;
`POST /auth/login/2fa` with the TOTP code completes the login.

**Creating an API key** (under *Settings → API keys*, requires
`apikeys:manage`): the plaintext key is shown **only once** in the response -
keep it safe, a lost key cannot be shown again, only revoked and re-created.

```sh
curl -s https://lcm.example.com/api/v1/servers \
  -H 'X-API-Key: lcm_xxxxxxxxxxxxxxxx'
```

## Permissions (RBAC)

Every endpoint checks a fixed permission scope (e.g. `servers:write`,
`settings:manage`) - without it, the API responds `403`. For servers, the
**visible data set** is additionally restricted to the caller's server
groups (managers only see their assigned groups, admins see everything).
Details on roles and scopes: [Security Model](/en/reference/security-model/).

## Certificates, allowed networks and Let's Encrypt

These endpoints require the right `system:endpoints`, which only `admin` has
([Certificates](/en/guides/zertifikate/), [Allowed networks](/en/guides/netzbereiche/)):

| Endpoint | Purpose |
|---|---|
| `GET /system/tls` | certificates in the pool and what each service serves for its public address |
| `POST /system/tls/certificates` | upload a certificate (multipart, fields `certificate` and `key`) |
| `DELETE /system/tls/certificates/{id}` | remove an uploaded certificate |
| `POST /system/tls/reload` | reload certificates, like `lcm tls reload` |
| `GET /system/tls/certificates/{id}/download` | the certificate chain as a PEM file, never the private key |
| `GET /system/network` | allowed networks per service (`ui`, `agent`, `mcp`), their source and whether `config.json` locks them |
| `PUT /system/network/{endpoint}` | set the allowed networks of a service |
| `GET /system/acme`, `PUT /system/acme` | settings and state of Let's Encrypt |
| `POST /system/acme/names-notice/ack` | dismiss the notice about the switch to the names from the addresses |
| `POST /system/acme/issue` | start a run immediately |
| `POST /system/acme/dns-test` | test the DNS provider for DNS-01 |

Address mode and ports are part of `GET` and `PATCH /settings/global`
([Public addresses & address mode](/en/guides/adressen/#through-the-api)). In addition `GET /system/tls/ca.pem` (right `settings:manage`) returns the public
part of LCM's own certificate authority, and `PUT /settings/mcp` takes only `enabled`
(MCP always speaks TLS; the port is `mcp_port` in `PATCH /settings/global`). Requests and responses are in the OpenAPI specification below.

## Example workflow

```sh
# The key comes from Settings → API keys (see above)
export LCM_KEY='lcm_xxxxxxxxxxxxxxxx'

# List all servers
curl -s https://lcm.example.com/api/v1/servers \
  -H "X-API-Key: $LCM_KEY" | jq '.items[].name'

# Trigger security updates on a server (async, returns a job ID)
curl -s -X POST https://lcm.example.com/api/v1/servers/3/packages/upgrade-security \
  -H "X-API-Key: $LCM_KEY"
# → {"status":"started", "job_id":"…", "job_name":"…"}

# Poll job progress/result
curl -s https://lcm.example.com/api/v1/jobs?server_id=3 \
  -H "X-API-Key: $LCM_KEY"
```

Server-mutating actions (updates, hardening, Docker actions, …) run
**asynchronously as a job**: the endpoint returns a `job_id` immediately, the
actual SSH contact runs in the background. Progress and console output are
available at `GET /jobs/:id/ssh-output` and in the job history.

## Full reference (OpenAPI)

All **311 endpoints** (combinations of method and path, spread over 276 paths) with required permission and (where
evident from the source) request body are available machine-readably as an
OpenAPI 3.0 specification, ready to download:

**[openapi.en.yaml](/static/openapi.en.yaml)**

The specification is maintained by hand - which is why it carries
explanations no generator could provide. To keep it from lagging behind, a test
compares the server's routes against the documented paths on every run: a new
endpoint without an entry fails the test run, so paths, methods and
permissions match the current state exactly. Open it in any OpenAPI tool (e.g.
[Swagger Editor](https://editor.swagger.io/), Postman import, Insomnia,
Bruno) for a searchable, interactive view grouped by area (servers, server
groups, Docker, security, jobs, users, settings, …).

:::note[Browsable API reference]
A browsable API reference embedded directly in this documentation is
prepared (the docs builder supports this via `starlight-openapi`), but is
waiting on a small fix in the shared docs building block - until then, open
the file above locally.
:::
