---
sidebar:
  order: 26
title: Configuration file config.json
description: Every field of the config.json at a glance - structure, defaults, meaning, and how the file relates to the settings in the interface and to environment variables.
---

The `config.json` is the start-up file of LCM. It decides where LCM listens,
where it keeps its data and logs, and how it finds certificates and the CVE
scanner. Most settings of LCM are **not** in this file. They live in the database
and in the interface. A few values live in both places. This page shows the file
in full once and tells you who wins when the two disagree.

The short version:

- The file has **34 top-level keys**. LCM shares 16 of them with the settings in
  the interface (**mirrored**), two only provide a **default**, and the rest live
  **only in the file**.
- An **environment variable** beats the file. For the ports it also locks the
  field in the interface.
- The file holds **no secret** that LCM needs. The master key exists only as the
  file `lcm.key`, as an environment variable or as a systemd credential.
- LCM reads the file **at start**. Only the `tls_certificates` section can be
  reloaded while LCM runs.

## Where the file lives and when it appears

| Installation | Path of the file | Data directory |
|---|---|---|
| **Debian/Ubuntu package** | `/etc/lcm/config.json` | `/var/lib/lcm` |
| **Docker** | `/data/config.json` | `/data` (on the host the mounted folder, for example `./data`) |
| **Binary without package** | `config.json` in the data directory | the directory the program sits in |

Two flags and one environment variable set the path:

- `--data <directory>` or `LCM_DATA` sets the data directory. It holds the
  database, `lcm.key`, the certificate, backups and logs.
- `--config <path>` sets the file itself. Without the flag it is `config.json` in
  the data directory.
- The service file of the package runs `lcm -config /etc/lcm/config.json -data
  /var/lib/lcm`, and the Docker image sets `LCM_DATA=/data`.

LCM refuses a system directory such as `/usr/bin` as data directory. Relative
paths in the file (such as `database_path`) count from the data directory.

**When it appears:**

- **Package:** On the first installation the package creates the file with ten
  keys (`host`, `port`, `database_path`, `access_token_ttl_minutes`,
  `admin_initial_password`, `log_level`, `access_log`,
  `api_key_rate_limit_per_minute`, `allowed_ips`, `trust_proxy_header`), and only
  if there is none yet. Updates leave it alone.
- **Binary and Docker:** If the file is missing at start, LCM creates it with all
  defaults and the permissions `0600`.
- **After that LCM completes it.** At start LCM adds every key that is missing
  from your file, with the value that applies right now. So after an update you
  find new options by opening the file instead of searching for their names.
  Existing values and unknown keys stay as they are.

The file is plain JSON. Comments and a comma after the last entry are not
allowed, and LCM will not start with them.

## A complete example

This is how the file looks after the first start of a binary or container. LCM
appends the last eight keys (from `address_mode` on) at the first start, because
they belong to the mirrored settings.

```json
{
  "host": "127.0.0.1",
  "port": 9310,
  "agent_host": "0.0.0.0",
  "agent_port": 9320,
  "database_path": "app.db",
  "access_token_ttl_minutes": 60,
  "admin_initial_password": "",
  "log_level": "info",
  "log_file": "",
  "access_log": true,
  "api_key_rate_limit_per_minute": 120,
  "acme_http_listen": ":80",
  "acme_dns_exec": "",
  "trivy_path": "trivy",
  "trivy_url": "",
  "trivy_token": "",
  "backup_dir": "",
  "allowed_ips": [],
  "agent_allowed_ips": [],
  "mcp_allowed_ips": [],
  "trust_proxy_header": false,
  "trusted_proxies": [],
  "tls_ca_cert": "",
  "tls_ca_key": "",
  "tls_certificates": [],
  "shared_tls_listen": "",
  "address_mode": "separate",
  "public_host": "",
  "public_base_url": "",
  "public_agent_url": "",
  "public_mcp_url": "",
  "mcp_port": 9330,
  "agent_enabled": true,
  "mcp_enabled": false
}
```

An example for running behind a reverse proxy on the same host. You change only
these keys, all others stay:

```json
{
  "host": "127.0.0.1",
  "public_base_url": "https://lcm.example.com",
  "trust_proxy_header": true,
  "trusted_proxies": ["127.0.0.1"],
  "allowed_ips": ["private"]
}
```

The tables in the next section explain the fields. The *Interface* column says
whether you can also set the value in LCM:

- **mirrored**: file and interface carry the same value, see
  [File, interface and environment](#file-interface-and-environment).
- **wins**: the interface knows the value and wins, the file only provides the
  default.
- **no**: the value lives only in the file.

## The fields

### Web UI and addresses

| Field | Type | Default | Meaning | Interface |
|---|---|---|---|---|
| `host` | text | `127.0.0.1` (package: `0.0.0.0`) | Bind address of the web UI and the REST API. MCP binds to the same address. Environment: `LCM_HOST`. | no |
| `port` | number | `9310` | Port of the web UI, 1 to 65535. Environment: `LCM_PORT`. | mirrored |
| `address_mode` | text | `separate` | Address mode: `separate` (A), `one_name` (B) or `shared_port` (C). | mirrored |
| `public_host` | text | empty | Mode B: the one name for all services, without scheme, port and path. | mirrored |
| `public_base_url` | text | empty | Address of the web UI from outside (modes A and C). LCM builds the links in e-mails from it. | mirrored |
| `shared_tls_listen` | text | empty | Address of the shared TLS port, such as `:443` or `127.0.0.1:443`. When set, it means mode C. The host in front is the bind address and lives only in the file. In development mode (`--dev`) the key is ignored. | mirrored (the port) |

The modes and what LCM derives from them are described under
[Public addresses & address mode](/en/guides/adressen/).

### Agent

| Field | Type | Default | Meaning | Interface |
|---|---|---|---|---|
| `agent_host` | text | `0.0.0.0` | Bind address of the agent listener. Empty means all interfaces. Environment: `LCM_AGENT_HOST`. | no |
| `agent_port` | number | `9320` | Port of the agent listener, 0 to 65535. Environment: `LCM_AGENT_PORT`. | mirrored |
| `agent_enabled` | bool | `true` | Switch for agent access (LCM Remote). | mirrored |
| `public_agent_url` | text | empty | Address of the agent port from outside. Empty means: the name of the web UI with the port of the agent. | mirrored |

An `agent_port` of `0` comes from old files and meant "agent off". LCM reads it as
`agent_enabled: false` and keeps the previous port. In mode C this does not apply:
there `0` only means no port of its own next to the shared one.

### MCP

| Field | Type | Default | Meaning | Interface |
|---|---|---|---|---|
| `mcp_port` | number | `9330` | Port of the MCP interface, 1 to 65535. | mirrored |
| `mcp_enabled` | bool | `false` (new installation) | Switch for MCP. | mirrored |
| `public_mcp_url` | text | empty | Address of the MCP interface from outside. | mirrored |

There is no field of its own for the bind address: MCP binds to `host`. More under
[MCP interface](/en/guides/mcp/).

### Access and login

| Field | Type | Default | Meaning | Interface |
|---|---|---|---|---|
| `allowed_ips` | list | `[]` | Allowed networks for the web UI and API. Empty means all networks. Entries are `localhost`, `private`, an IP address or a CIDR network, at most 64. Other clients get `403`. | mirrored |
| `agent_allowed_ips` | list | `[]` | The same for the agent endpoint. | mirrored |
| `mcp_allowed_ips` | list | `[]` | The same for MCP. | mirrored |
| `trust_proxy_header` | bool | `false` | `true`: LCM takes the client address from `X-Forwarded-For`, but only from the peers in `trusted_proxies`. Turn it on only when a reverse proxy sits in front. | mirrored |
| `trusted_proxies` | list | `[]` | Addresses, CIDR networks or `localhost`/`private` of the proxies whose header LCM believes. Empty means: nobody. LCM does not adopt `0.0.0.0/0` and `::/0`. | mirrored |
| `api_key_rate_limit_per_minute` | number | `120` | Requests per API key and minute. `0` turns the limit off, a negative value is an error. Browser sessions do not count. | no |
| `access_token_ttl_minutes` | number | `60` | Idle time of a browser session in minutes, greater than 0. | wins (*Session length in minutes*) |
| `admin_initial_password` | text | empty | Password of the `admin` account, only when a fresh database is first created. Empty means: LCM generates one and prints it once on the console (`journalctl -u lcm \| grep -A3 'Admin account'`). It must be changed at the first login. | no |

More on this: [Allowed networks](/en/guides/netzbereiche/) and
[Reverse proxy & external hardening](/en/guides/reverse-proxy/#trusted_proxies-whose-x-forwarded-for-is-believed).

### TLS, certificates and ACME

| Field | Type | Default | Meaning | Interface |
|---|---|---|---|---|
| `tls_ca_cert` | text | empty | Certificate of a CA of your own (PEM, absolute path) that LCM uses to issue its server certificate. Only together with `tls_ca_key`. Empty means: LCM creates its own CA in the data directory. | no |
| `tls_ca_key` | text | empty | Key of that CA (PEM, absolute path). The two files are not part of the backup. | no |
| `tls_certificates` | list | `[]` | Certificates of your own. Each entry has `cert` (certificate with chain) and `key` (key without password), both as absolute paths, no certificate twice. If one does not load, LCM starts anyway and warns in the log. | no |
| `acme_http_listen` | text | `:80` | Address of the listener for the HTTP-01 challenge of Let's Encrypt, `host:port` or `:port`. It is open only while a challenge runs. | no |
| `acme_dns_exec` | text | empty | Absolute path of a program for the DNS provider `exec` in DNS-01. Settable only here, never through the interface or the API. LCM checks that the file exists only when you pick the provider. | no |

Let's Encrypt itself (names, mode, provider) is not in the file, only in the
interface. Neither are the certificates you upload in LCM. More under
[Certificates, Let's Encrypt & port 443](/en/guides/zertifikate/).

### Logging

| Field | Type | Default | Meaning | Interface |
|---|---|---|---|---|
| `log_level` | text | `info` | `debug`, `info`, `warn` or `error`. The flag `--debug` raises the value to `debug` at runtime without changing the file. | no |
| `log_file` | text | empty | Path of the log file. Empty means `<data directory>/logs/lcm.log`. The file rotates (from 10 MB, up to 7 compressed old files, at most 7 days). | no |
| `access_log` | bool | `true` | Logs every API request with method, path, status, duration, IP and user. | no |

### Data and backups

| Field | Type | Default | Meaning | Interface |
|---|---|---|---|---|
| `database_path` | text | `app.db` | Path of the SQLite database, relative to the data directory or absolute. LCM refuses to restore a backup whose `database_path` lies outside the data directory. | no |
| `backup_dir` | text | empty | Target directory of the backups (absolute). Empty means `<data directory>/backups`. The file only provides the default: at start LCM writes it into the settings if nothing is there yet. | wins (*Settings → Backups*, field *Directory*) |

### CVE scan (Trivy)

| Field | Type | Default | Meaning | Interface |
|---|---|---|---|---|
| `trivy_path` | text | `trivy` | The Trivy program on the LCM host, via `PATH` or as an absolute path. If it is missing, the CVE scan switches itself off cleanly. | no |
| `trivy_url` | text | empty | Address of a Trivy sidecar, such as `http://trivy:9330`, with `http://` or `https://`. When set, LCM starts no local Trivy. Needed in container operation. Environment: `LCM_TRIVY_URL`. | no |
| `trivy_token` | text | empty | Bearer token for the sidecar. Required as soon as `trivy_url` is set. Environment: `LCM_TRIVY_TOKEN`, or a systemd credential `trivy_token`. | no |

More on the sidecar under [Packaging](/en/reference/packaging/#trivy-in-container-mode).

### What is not in the file

These values do not belong in the `config.json`, and LCM does not write them
there:

- **Development and demo mode:** exist only as the flags `--dev`, `--demo` and
  `--demo-public`. LCM does not read a `demo_mode` or `dev_mode` in the file.
  Otherwise an old or restored file could weaken operation unnoticed or fill a
  fresh database with demo data.
- **Everything else from the settings:** users, roles, mail, backups, alerts,
  Let's Encrypt and so on live in the database.

## File, interface and environment

### The order of precedence

The environment is on top. Below it, it depends on which group the field belongs
to:

| Group | What applies | Fields |
|---|---|---|
| **Mirrored** | The settings in the database. The file follows them. What you change by hand in the file, LCM adopts into the settings at start. For `port` and `agent_port` an environment variable beats both. | `port`, `agent_port`, `mcp_port`, `address_mode`, `public_host`, `public_base_url`, `public_agent_url`, `public_mcp_url`, `shared_tls_listen`, `agent_enabled`, `mcp_enabled`, `trust_proxy_header`, `trusted_proxies`, `allowed_ips`, `agent_allowed_ips`, `mcp_allowed_ips` |
| **Default** | The interface, once it has a value. The file applies only while nothing is set there. | `access_token_ttl_minutes` (*Session length in minutes*, `0` means: default from the file), `backup_dir` |
| **File only** | The file. The environment overrides some fields. | all others |

The environment variables one by one:

| Variable | Effect | Rank |
|---|---|---|
| `LCM_HOST` | `host` | environment before file |
| `LCM_PORT` | port of the web UI | environment before settings and file. **Locks the field in the interface.** |
| `LCM_AGENT_HOST` | `agent_host` | environment before file |
| `LCM_AGENT_PORT` | port of the agent listener; `0` turns it off | environment before settings and file. **Locks the field.** |
| `LCM_TRIVY_URL` | `trivy_url` | environment before file |
| `LCM_TRIVY_TOKEN` | `trivy_token` | A systemd credential `trivy_token` comes before the environment, the environment before the file. |
| `LCM_DATA` | data directory | The flag `--data` comes first. |

The Docker container sets `LCM_HOST=0.0.0.0`. There a `host` in the file changes
nothing unless you override the variable.

The flag `--debug` is the only thing that overrides `log_level`.

### Mirrored values: what happens on save and at start

**Saving in the interface.** LCM writes the value into the database first and then
into the file. In the file it changes only the mirrored keys that are missing or
differ. All other keys keep value and order, new keys go to the end. The file is
replaced atomically and keeps its permissions. The indentation afterwards is
LCM's own (two spaces), formatting you set by hand is lost. LCM writes the network
lists in a uniform form: `10.0.0.5` becomes `10.0.0.5/32`.

If writing fails, for example because the file is mounted read-only, the change
still applies. LCM warns in the log, and the API response carries
`config_sync_error`.

**LCM start.** Before it opens the ports, LCM compares the file with the settings:

1. A key that is missing from the file does not count as a change. LCM adds it with
   the value of the settings.
2. A key that differs from the settings **and** from the state LCM last wrote
   counts as a manual change. LCM adopts it into the settings and names it in the
   log (`value changed by hand, adopted into the settings`). The same checks apply
   as for an entry in the interface.
3. A value that only matches the state LCM last wrote is stale, for example after a
   write error or an old file copied back. Then the database applies, and LCM
   brings the file to its state.
4. If a value is invalid, LCM starts anyway. The value in the settings keeps
   applying, the key stays in the file as it is, and the log warns at **every**
   start (`value not adopted, the database value applies`) until you fix it. Values
   that belong together (such as `trust_proxy_header` and `trusted_proxies`) may
   then be adopted only at the second start. So fix the error first.

This is how you get back into LCM if you locked yourself out with the allowed
networks: set `"allowed_ips": []` in the file and restart LCM. Without a restart,
`sudo lcm network reset ui` works
([Allowed networks](/en/guides/netzbereiche/)).

**What the environment locks.** Only an environment variable locks a field, because
LCM cannot change it. That applies to `LCM_PORT` (port of the web UI) and
`LCM_AGENT_PORT` (port of the agent, and with `0` the agent switch too). The
interface then shows the field locked with the note "set by an environment
variable". LCM adopts neither `port` nor `agent_port` from the file then, and it
does not write them either. A value in the file locks nothing.

**While LCM runs, do not change the mirrored keys in the file.** LCM does not read
the file during operation. At the next save in the interface it overwrites your
change with the value from the settings. Stop LCM, edit the file and start again.

## Making changes take effect

| What you change | How it takes effect |
|---|---|
| Mirrored values **in the interface** | immediately, no restart. The new port of the web UI opens first, the old one closes after 10 seconds. Networks and proxy trust apply from the next request. |
| `tls_certificates` in the file | `sudo lcm tls reload` or `sudo systemctl reload lcm` (signal `SIGHUP`). In the container: `docker kill -s HUP lcm`. This reloads only that section, plus uploads and ACME certificates. A faulty certificate does not replace a good one. |
| **everything else** in the file | Restart: `sudo systemctl restart lcm`, in the container `docker compose restart`. |

A restart ends all sessions, everyone has to log in again.

This is how you edit the file safely:

```sh
sudoedit /etc/lcm/config.json
python3 -m json.tool /etc/lcm/config.json > /dev/null && echo "JSON ok"
sudo systemctl restart lcm
journalctl -u lcm -n 50
```

The log names every value LCM adopted or rejected.

## Permissions and secrets

**Permissions.** The package creates the file as `root:lcm` with `0660` in a
directory with `0770`: LCM must be able to write it, because it adds new options
and a restore replaces it. If LCM creates the file itself, it has `0600`. LCM
checks the permissions at start and every 15 minutes. If the file belongs to the
service and grants anything to "others", LCM takes it back and logs it. A Docker
volume must be writable for the container user (UID 1000). If you mount the file
read-only, LCM still starts, but it cannot complete options or write anything back.

**Whoever may write the file can bypass the access protection.** Through
`allowed_ips` and `trusted_proxies` it controls who reaches LCM and which proxies
LCM believes. Grant that right only to those who look after the LCM host.

**What never belongs in it:**

| Secret | Where it lives instead |
|---|---|
| **Master key** | `lcm.key` in the data directory, the environment variable `LCM_ENCRYPTION_KEY` or a systemd credential. See [Security model](/en/reference/security-model/). |
| **Key of the 2FA challenge** | Created in memory at every start. |
| **Passphrase for backups** | encrypted in the settings, or `LCM_BACKUP_PASSPHRASE`, or a credential `backup_passphrase`. |
| **Passwords and credentials** (SSH, mail, CrowdSec, providers) | encrypted in the database. |

Two fields can carry a secret in clear text: `trivy_token` and
`admin_initial_password`. Both end up in every backup together with the file. Use
`LCM_TRIVY_TOKEN` or a credential for the token, and clear the initial password
after the first login.

## Typical mistakes

| Symptom | Cause and fix |
|---|---|
| LCM does not start, the log says `config.json parsen` | The JSON is invalid: one comma too many, a comment, a number in quotes. Check the file with `python3 -m json.tool`. |
| A change has no effect | A typo in the key name. LCM ignores unknown keys without warning. Compare the name with the tables above. |
| LCM does not start, the log names a value | The check rejected it: `port` or `agent_port` outside 1 to 65535 (from 0 for the agent), unknown `log_level`, `access_token_ttl_minutes` not greater than 0, negative `api_key_rate_limit_per_minute`, `acme_http_listen` without a port, a path in `tls_ca_cert`, `tls_ca_key`, `tls_certificates` or `acme_dns_exec` that is not absolute, only one of `tls_ca_cert` and `tls_ca_key` set, `trivy_url` without `http://` or without `trivy_token`. |
| `port` or `agent_port` in the file changes nothing | `LCM_PORT` or `LCM_AGENT_PORT` is set and wins. Remove the variable or change it there. |
| `host` changes nothing in the container | The image sets `LCM_HOST=0.0.0.0`. Set the variable again if you want something else. |
| A mirrored value jumps back | You edited the file while LCM was running, and the next save in the interface overwrote it. Stop LCM before you change the file. |
| A mirrored value from the file does not arrive | The value is invalid or matches the state last written. The log names the key (`value not adopted`). |
| A certificate of your own is not served | It does not load (path, permissions for the user `lcm`, key does not match). LCM starts anyway. The log gives the reason, `sudo lcm tls status` shows the state. |
| After an update the file has new keys | That is intended, LCM adds them with their default value. You need not do anything. |
| LCM cannot complete the file | It belongs to another user or is read-only. LCM starts anyway and warns in the log (`could not add new options`). |

## See also

- [Public addresses & address mode](/en/guides/adressen/): modes, ports and the
  mirrored keys in detail
- [Allowed networks](/en/guides/netzbereiche/): `allowed_ips` and `lcm network reset`
- [Certificates, Let's Encrypt & port 443](/en/guides/zertifikate/): `tls_certificates`,
  `tls_ca_cert`, `acme_http_listen`, `acme_dns_exec`
- [Backups](/en/guides/backups/): contents of the archive, `backup_dir`
- [Installation](/en/getting-started/installation/): flags and environment variables
- [Security model](/en/reference/security-model/): master key and credentials
