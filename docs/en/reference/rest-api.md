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

Two equivalent methods:

| Method | Header | For |
|---|---|---|
| Session token | `Authorization: Bearer <jwt>` | interactive use, scripts with login |
| API key | `X-API-Key: <key>` | service-to-service, automation |

**Session login:**

```sh
curl -s -X POST https://lcm.example.com/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"…"}'
# → {"token":"eyJ…", "user": {...}}
```

If 2FA is enabled for the account, `/auth/login` returns a challenge instead
of a token; `POST /auth/login/2fa` with the TOTP code completes the login.

**Creating an API key** (under *Settings → API Keys*, requires
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

## Example workflow

```sh
TOKEN=$(curl -s -X POST https://lcm.example.com/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"…"}' | jq -r .token)

# List all servers
curl -s https://lcm.example.com/api/v1/servers \
  -H "Authorization: Bearer $TOKEN" | jq '.[].name'

# Trigger security updates on a server (async, returns a job ID)
curl -s -X POST https://lcm.example.com/api/v1/servers/3/packages/upgrade-security \
  -H "Authorization: Bearer $TOKEN"
# → {"status":"started", "job_id":"…", "job_name":"…"}

# Poll job progress/result
curl -s https://lcm.example.com/api/v1/jobs/history?server_id=3 \
  -H "Authorization: Bearer $TOKEN"
```

Server-mutating actions (updates, hardening, Docker actions, …) run
**asynchronously as a job**: the endpoint returns a `job_id` immediately, the
actual SSH contact runs in the background. Progress and console output are
available at `GET /jobs/:id/ssh-output` and in the job history.

## Full reference (OpenAPI)

All **258 endpoints** with method, path, required permission, and (where
evident from the source) request body are available machine-readably as an
OpenAPI 3.0 specification, ready to download:

**[openapi.yaml](/static/openapi.yaml)**

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
