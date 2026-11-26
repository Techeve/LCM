---
sidebar:
  order: 24
title: Certificates, Let's Encrypt & port 443
description: Your own certificates or Let's Encrypt for the web UI, the agent and MCP, without a reverse proxy, on separate ports or together on 443.
---

Out of the box LCM speaks HTTPS with a certificate from its own certificate
authority. That certificate names only `localhost` and `127.0.0.1`, so every
browser that opens LCM under a real name shows a warning. If you run LCM without
a reverse proxy, you either install a certificate of your own or let LCM fetch
and renew one from Let's Encrypt. A reverse proxy is possible, but not
required
([Reverse proxy & external hardening](/en/guides/reverse-proxy/)). You choose
which names and ports LCM uses with the address mode
([Public addresses & address mode](/en/guides/adressen/)).

## How LCM picks the certificate

LCM keeps all certificates in one **pool**. On every connection it looks at the
name the client asks for (SNI) and serves the certificate that names it. This
applies to the web UI, the agent port and MCP alike.

- **If no certificate matches**, or the client calls LCM **by IP address**, LCM
  serves its own certificate, the **LCM certificate**. It always remains the
  fallback, so existing agents never drop off.
- **If several certificates name the same name**, this order applies:
  `config.json`, then uploaded, then Let's Encrypt (ACME). LCM skips an
  expired certificate as long as another one fits.
- **A certificate may carry several names.** One certificate with three names is
  enough for three services, and so are three certificates with one name each.
- **Every certificate is checked when it loads:** PEM format, the key matches,
  it is valid right now, it may be used for servers (serverAuth) and it names at
  least one name. If the file lacks the issuer's certificate, LCM loads it
  anyway and warns, because some clients reject an incomplete chain. If a
  certificate fails the check, the previous state stays active.

LCM issues its own certificate from its own certificate authority (valid for
90 days, renewed monthly). With `tls_ca_cert` and `tls_ca_key` in
`config.json` you can name a certificate authority of your own infrastructure
instead.

## Certificate and address mode

Which certificate you need depends on the address mode
([Public addresses & address mode](/en/guides/adressen/)):

| Mode | Names in the certificate |
|---|---|
| **A: Own ports, own names** | One name per service. One certificate with all names is enough, and so are three separate ones. Behind a reverse proxy LCM often needs none at all. |
| **B: Own ports, one name** | The one name. One certificate covers the web UI, the agent and MCP. |
| **C: One port, own names** | The names of the services: the web UI always, agent and MCP if they have a name. |

In the example the certificate is a file. In mode B the `config.json` looks like
this, the ports stay at their defaults:

```json
{
  "host": "0.0.0.0",
  "port": 9310,
  "agent_host": "0.0.0.0",
  "agent_port": 9320,
  "tls_certificates": [
    { "cert": "/etc/lcm/tls/lcm.pem", "key": "/etc/lcm/tls/lcm.key" }
  ]
}
```

The certificate names `lcm.example.com`. Browsers open
`https://lcm.example.com:9310`, agents connect to
`https://lcm.example.com:9320`. You enter the name under
*Settings → General* in address mode B. LCM works out the three addresses from
it.

### Everything on 443: mode C

The web UI, the agent and MCP share **one** port, and the requested name decides
which service gets the connection. You choose the mode in the interface and enter
the shared port there (default 443). Alternatively you set it in the
`config.json`: LCM adopts `shared_tls_listen` on start as mode C with that port,
and the interface then shows it like any other setting
([The `config.json` in step with the interface](/en/guides/adressen/#the-configjson-in-step-with-the-interface)):

```json
{
  "host": "127.0.0.1",
  "port": 9310,
  "shared_tls_listen": ":443",
  "tls_certificates": [
    { "cert": "/etc/lcm/tls/lcm.pem", "key": "/etc/lcm/tls/lcm.key" }
  ]
}
```

The certificate names `lcm.example.com`, `agent.example.com` and
`mcp.example.com`. Three separate certificates work just as well. Enter the
names as the addresses:

| Service | Address |
|---|---|
| Web UI | `https://lcm.example.com` |
| Agent | `https://agent.example.com` |
| MCP | `https://mcp.example.com` |

All three names point to the LCM host in DNS. Then:

- **The web UI needs a name, agent and MCP only if they should run.** Names that
  are set must differ, otherwise LCM refuses to save. A service without a name is
  off.
- **Without a name** (call by IP) and for any unknown name, the connection goes
  to the web UI.
- **The single ports stay open as well**, unless you switch them off. In the
  example the web UI stays on `127.0.0.1:9310` and is not reachable from
  outside. Block the other single ports in the firewall, and only port 443 faces
  the network.
- **Agent and MCP** you switch on with their switch under *Settings → General*
  (MCP also on the *MCP* page). Clients call `https://mcp.example.com/mcp`.
- **Port 443** the service may open without you setting anything, see
  [Freeing port 80 and 443](#freeing-port-80-and-443).

`lcm tls status` shows which name leads to which service
([Reloading without a restart](#reloading-without-a-restart)).

## Providing the certificate

### From files (`config.json`)

Enter certificate and key as absolute paths in `tls_certificates`. The
certificate file holds the certificate followed by its chain, with Let's Encrypt
that is `fullchain.pem`. The key is unencrypted, without a passphrase. The
service runs as user `lcm` and must be able to read both files.

A certificate that does not load at startup does not stop LCM from starting. The
message appears in the journal (`journalctl -u lcm`).

**With certbot**, a deploy hook copies the renewed files to `/etc/lcm/tls` and
tells LCM. Create the script `/usr/local/sbin/lcm-deploy-cert`:

```sh
#!/bin/sh
# certbot sets RENEWED_LINEAGE to the directory of the certificate.
set -eu
install -d -m 0750 -o root -g lcm /etc/lcm/tls
install -m 0640 -o root -g lcm "$RENEWED_LINEAGE/fullchain.pem" /etc/lcm/tls/lcm.pem
install -m 0640 -o root -g lcm "$RENEWED_LINEAGE/privkey.pem" /etc/lcm/tls/lcm.key
lcm tls reload
```

Make it executable and hand it to certbot when you first issue the certificate.
Certbot remembers the hook and runs it on every renewal:

```sh
sudo chmod +x /usr/local/sbin/lcm-deploy-cert
sudo certbot certonly --standalone -d lcm.example.com \
  --deploy-hook /usr/local/sbin/lcm-deploy-cert
```

The hook runs after the first issue and after every renewal. The permissions
(group `lcm`, `0640`) let the service read the key and nobody but root else. If
you let LCM fetch certificates from Let's Encrypt itself, you do not need
certbot, see [Let's Encrypt](#lets-encrypt-acme).

### Uploading in the interface

*Settings → General*, block **Public addresses**: a gear (*Certificate &
security*) sits behind the address of the web UI, the agent and MCP. The dialog
shows which certificate LCM serves for the name of that address (source, names, issuer, valid until, fingerprint). There
you upload a PEM file with the certificate and its chain and the key file, up to
64 KiB each. Below the certificate the same dialog shows the
[allowed networks](/en/guides/netzbereiche/) of the service.

- LCM checks both before saving anything and loads the certificate right away,
  without a restart. The dialog then tells you whether it applies to this
  address, or why not (name missing, a certificate of higher rank takes
  precedence).
- You can remove uploaded certificates in the dialog again. LCM then serves
  another matching certificate for the name, or the LCM certificate.
  Certificates from `config.json` and from Let's Encrypt you remove where they
  come from.
- Uploaded certificates live under `/var/lib/lcm/tls/uploads/` (permissions
  `0600`) and are part of the system backup, just like the key of the LCM
  certificate authority.

Only an **admin** can change certificates. Nobody else holds the right
`system:endpoints`, not even a manager with `settings:manage`.

### Let's Encrypt (ACME)

LCM can fetch certificates from an ACME certificate authority itself and renew
them in time. You set it up under *Settings → General* in the **Public
addresses** block, in the collapsed **Let's Encrypt** section (visible to admins
only). It is available in every address mode. In mode A behind a reverse proxy
only DNS-01 usually makes sense, because HTTP-01 would need port 80 on LCM.

| Setting | Meaning |
|---|---|
| Obtain certificates automatically | Switch. Switching it off removes the obtained certificates, LCM then serves the others or the LCM certificate again. |
| Names | Read-only. They come from the addresses, see below. |
| Contact email | Optional. The certificate authority writes there before a certificate expires. |
| Certificate authority | Let's Encrypt, Let's Encrypt staging (test certificates browsers do not trust), or the address of your own ACME certificate authority. |
| Challenge | HTTP-01 or DNS-01 |
| Terms of service | You accept them once per certificate authority. Without your consent LCM creates no account. |

Optional fields of the DNS provider (such as TSIG with RFC2136, wait times and
nameservers) sit under **More settings**, collapsed.

**The names come from the addresses.** There is no name field of its own. LCM
takes the distinct DNS names of the addresses of the web UI, the agent and MCP,
in mode B the one name. Agent and MCP only count while they are switched on.
LCM skips IP addresses and names without a dot (`localhost`), because no public
certificate authority issues a certificate for them. If no address has a usable
name, Let's Encrypt cannot be switched on. Wildcard names such as `*.example.com`
are not possible, because they appear in no address. If the addresses change, the
new state applies once you save the page.

**Always one certificate for all names.** In every mode Let's Encrypt issues one
certificate that carries all names. That takes one run and one renewal.
Certificates you upload by hand remain possible per address.

**Issuing without an extra click.** If no valid Let's Encrypt certificate carries
all names, LCM issues one by itself:

- after you save Let's Encrypt (first setup),
- after you save the addresses, when a name is added,
- after you switch on agent or MCP, when that adds a name,
- at startup.

A certificate that carries more names than needed covers them. Toggling agent or
MCP then triggers no new issue. If such a run fails, LCM starts again for the same
names only after an hour, so there is no loop.

**Issue now** starts a run immediately, even after a failure. Otherwise LCM runs
shortly after startup and then daily, and after a failure again within an hour. The card shows the
state, the last run, an error and the time from which the certificate will be
renewed. If names or certificate authority change, LCM issues a new certificate
and removes the old one once all new ones are in place. The account and the
issued certificates live under `/var/lib/lcm/tls/acme/` and are part of the
system backup.

An obtained certificate ranks last. If a certificate from `config.json` or an
uploaded one names the same name, it takes precedence, even when the Let's
Encrypt certificate carries the name too. A renewal changes nothing about that.

**Downloading the certificate.** Each issued certificate has a *Download
certificate chain (PEM)* button. It downloads the certificate and its chain,
**never** the private key. Only an admin may do this (`system:endpoints`), and
the download appears in the audit log (`settings.tls.download`). The same is
available as `GET /api/v1/system/tls/certificates/<id>/download`; the ID is in
`GET /api/v1/system/tls` (such as `acme-<id>`).

**Upgrading from earlier versions.** Earlier versions had a field of their own,
*Domain names*, and the choice between *one certificate for all* and *one per
name*. On update LCM takes the names from the addresses instead and always
issues one certificate for all. If the old names did not cover the addresses, the
card shows a notice once, with the old and the new names. The old certificate
stays in use until the next run has fetched a new one. You can dismiss the
notice. The API still accepts the fields `names` and `mode` of older calls and
ignores them.

#### HTTP-01

The certificate authority fetches a file under
`http://<name>/.well-known/acme-challenge/`. For that, **port 80 must point to
LCM from the internet**.

- **Port 80 is open only during the check.** LCM opens a small listener of its
  own when a run needs a check and closes it afterwards. Before and after, the
  port refuses connections. Its address is `acme_http_listen` (default `:80`).
- **It answers open checks only.** For `GET /.well-known/acme-challenge/<token>`
  with a token that is open right now, it returns the answer. Everything else
  (other paths, methods, tokens) it ends without a reply, as if nothing were
  there. The allowed networks do not
  apply to it: it answers the check from any network.
- Port 80 needs the permission for ports below 1024. The service file
  `lcm.service` brings it along ([Freeing port 80 and 443](#freeing-port-80-and-443)).
  Alternatively set `acme_http_listen` to a high port such as `":8080"` and
  forward port 80 there with a firewall or router.
- **Behind a reverse proxy** prefer DNS-01. With HTTP-01 the proxy would have to
  pass port 80 on to LCM, and that usually does not work. If the proxy has a
  certificate of its own, you do not need Let's Encrypt in LCM.

If port 80 cannot be opened during a check, the run fails. The card names the
reason and, if the permission is missing, the hint about it.

#### DNS-01

With DNS-01, LCM proves ownership of the name through a TXT record
`_acme-challenge.<name>`. For that LCM **does not need to be reachable from the
internet** and needs no port 80, it only needs access to the DNS zone.

In the **Let's Encrypt** section you choose **DNS-01** under *Challenge*. Below it
appear the **DNS provider** with its fields, the **check nameservers** and the
**Test DNS** button. LCM stores secrets such as tokens and passwords encrypted
and never shows them again. If you leave a secret field empty in a later change,
it is kept, with *Delete* you remove it. Optional fields such as TSIG with
RFC2136, wait times and nameservers sit under *More settings*.

The same works through the API: `PUT /api/v1/system/acme` with
`"challenge": "dns-01"`, `dns_provider` and the provider's values in
`dns_values`. The names come from the addresses, the request does not name them.
If a secret field is missing, it is kept, an empty value deletes it.

```sh
curl -s -X PUT https://lcm.example.com/api/v1/system/acme \
  -H "X-API-Key: $LCM_KEY" -H "Content-Type: application/json" \
  -d '{
    "enabled": true,
    "accept_terms": true,
    "challenge": "dns-01",
    "dns_provider": "cloudflare",
    "dns_values": { "api_token": "…" }
  }'
```

The providers and their fields (`*` marks required fields, the names are those of
the API, the interface shows them as labeled fields):

| Provider (`dns_provider`) | Fields |
|---|---|
| `rfc2136` | `nameserver`\* (`host` or `host:port`), `tsig_key`, `tsig_secret` (base64), `tsig_algorithm` (`hmac-sha256` is the default, also `hmac-sha384`, `hmac-sha512`, `hmac-sha224`, `hmac-sha1`) |
| `acmedns` | `api_base`\* (address of the acme-dns server) |
| `httpreq` | `endpoint`\*, `mode` (`standard` or `raw`), `username`, `password` |
| `exec` | `mode` (`standard` or `raw`), the program is set in `config.json` |
| `cloudflare` | `api_token`\* |
| `hetzner` | `api_token`\* (Cloud API token) |
| `pdns` | `api_url`\*, `api_key`\*, `server_name` (default `localhost`) |
| `inwx` | `username`\*, `password`\*, `totp_secret`, `sandbox` (`true` or `false`) |
| `ionos` | `api_key`\* (in the form `<prefix>.<secret>`) |

For all providers except `acmedns` you can adjust `propagation_timeout` (how
long LCM waits until the record is visible) and `polling_interval` in seconds,
each 1 to 3600. `tsig_key` and `tsig_secret` belong together.

**RFC2136 (dynamic DNS update).** For PowerDNS, BIND, Knot, Technitium and other
servers that accept TSIG-signed updates. You create a TSIG key on the nameserver
that may change TXT records under `_acme-challenge`, and enter server, key name,
algorithm and the secret.

**acme-dns.** LCM creates the account at the acme-dns server itself on the first
run. The flow:

1. You set `acmedns` with the address of your acme-dns server and start a run.
2. The first run fails with a message that names the **CNAME** you need to set
   once: `_acme-challenge.<name>` points to the name the acme-dns server
   assigned.
3. You set the CNAME at your DNS provider and start the run again. LCM keeps the
   account, from now on it works without further action.

**httpreq.** LCM sends `POST <endpoint>/present` and `POST <endpoint>/cleanup` to
your own endpoint. In `standard` mode the body carries `fqdn` and `value` (the
full name of the TXT record and its value), in `raw` mode `domain`, `token` and
`keyAuth`. `username` and `password` go along as basic authentication.

**exec.** LCM runs a program on your host. Only `config.json` sets the program, **not** the interface or the API: an
account in LCM must not be able to define a command on the LCM host. Enter the
absolute path in `config.json`:

```json
{
  "acme_dns_exec": "/usr/local/sbin/lcm-dns-challenge"
}
```

Only once the path is there does LCM offer `exec`. The program gets two arguments
after the command: `present` or `cleanup`, the name of the TXT record and its
value (`<program> present <name> <value>`). In `raw` mode they are
`<program> present -- <domain> <token> <key authorization>`. The program runs as
user `lcm` and must be executable.

**Check nameservers.** Before LCM lets the certificate authority check, it
verifies that the TXT record is visible. For that it asks the system's
resolvers. With the check nameservers (in the API `dns_check_nameservers`, at most three,
`host` or `host:port`) you set others, for example the authoritative servers of your zone, if a
resolver is in the way or a split-DNS zone does not show the record.

**Test DNS.** The button (in the API `POST /api/v1/system/acme/dns-test`) sets a
TXT record `_acme-challenge.<first name>` with a random value, waits up to 60
seconds for the check nameservers to show it, and cleans it up again. That way you verify
credentials and zone without burdening the certificate authority. The API call takes
the same fields as `PUT /system/acme`. What is missing LCM fills in from the
saved settings. A provider that takes longer can still succeed in the real run,
where its own time limit applies.

**Several names take time.** With `rfc2136` and `exec`, LCM works through the
names one after another and waits 60 seconds between two names. A certificate
with three names therefore takes correspondingly longer. Each provider has its
own default wait until the record is visible: 2 minutes for Cloudflare, Hetzner
and PowerDNS, 6 for INWX, 15 minutes for IONOS.

#### Your own ACME certificate authority

Instead of Let's Encrypt you enter the address of the ACME directory under
*Certificate authority* (it must begin with `https://`), for example that of
your own certificate authority such as step-ca. The rest works as above.

If the certificate of the ACME server comes from a private certificate authority
that LCM does not trust, the ACME library used by LCM knows the environment
variable `LEGO_CA_CERTIFICATES`: it names a PEM file with the root certificate of
that authority. Give it to the service in a drop-in (`sudo systemctl edit lcm`):

```ini
[Service]
Environment=LEGO_CA_CERTIFICATES=/etc/lcm/tls/acme-ca.pem
```

Then `sudo systemctl restart lcm`. Check the path: the file must be readable by
user `lcm`, otherwise no run can take place.

#### Renewal and alert

On every run LCM checks whether a certificate is due. If the certificate
authority offers **ARI** (its recommendation when to renew), LCM renews within
that window. Without ARI it renews as soon as **less than a third of the
lifetime** is left. If a run fails, LCM tries again hourly.

So that you notice, there is the alert type **Certificate renewal failed** under
*Settings → Alerts*. It fires as long as the last run has an error, names the
reason and ends as soon as a run succeeds. The alert **TLS certificate expires
soon** additionally reports when the LCM certificate runs short.

## Reloading without a restart

A new certificate needs no restart. LCM reloads certificates from `config.json`,
uploaded and obtained certificates when you tell it to:

```sh
sudo lcm tls reload       # reloads and reports what was loaded or rejected
sudo lcm tls status       # shows the state
sudo systemctl reload lcm # reloads only, without a report
```

`systemctl reload lcm` sends the signal `SIGHUP` and does the same as
`lcm tls reload`, just without output. It is the right call for scripts and
hooks. Only the certificates are reloaded, not the other settings in
`config.json`; LCM still reads those only at startup. In the interface the *Certificate & security*
dialog triggers the reload itself on upload and removal.

- **Open connections keep their certificate**, agents stay connected. New
  connections get the new one.
- **A faulty certificate does not replace a good one.** If one fails the check,
  the previous state stays active. `lcm tls reload` names the reason and ends
  with exit code 1.
- The control socket for these commands lives at `/var/lib/lcm/control.sock` and
  is accessible only to the service user and root. Hence `sudo`. The reload
  appears in the audit log (`settings.tls.reload`).

`lcm tls status` looks roughly like this (the program prints it in German):

```text
Zertifikate, in der Reihenfolge, in der sie gewählt werden:

config-0 - config.json
  Namen:        lcm.example.com, agent.example.com, mcp.example.com
  Aussteller:   E7
  Gültig bis:   2027-02-14
  Fingerprint:  3f9a1c0b7d2e5a64…

lcm-ca - LCM-Zertifikat (Rückfall)
  Namen:        localhost, 127.0.0.1, ::1
  Aussteller:   LCM Root CA
  Gültig bis:   2027-01-20
  Fingerprint:  b1c47e90a25d3f08…

Öffentliche Adressen:
  Weboberfläche  https://lcm.example.com
                 für lcm.example.com wird config-0 (config.json) ausgeliefert
  Agent          https://agent.example.com
                 für agent.example.com wird config-0 (config.json) ausgeliefert
  MCP            https://mcp.example.com
                 für mcp.example.com wird config-0 (config.json) ausgeliefert

Gemeinsamer Port :443 (aktiv): Weboberfläche, Agent und MCP nach dem angefragten Namen
  Weboberfläche  lcm.example.com
  Agent          agent.example.com
  MCP            mcp.example.com
  Weboberfläche  auch ohne Namen und für jeden anderen Namen
```

The list shows the certificates in the order in which LCM picks them, then what
each public address serves, then the shared port. If an address says "the LCM
certificate is served" (`ausgeliefert wird das LCM-Zertifikat`), no certificate
of your own fits its name.

## Freeing port 80 and 443

Without root rights a service may open ports below 1024 only with an extra
permission. The `lcm.service` file shipped with LCM grants exactly this one
capability (`CAP_NET_BIND_SERVICE`). You need not set anything, and a drop-in
(`systemctl edit lcm`) is not needed. All other hardening in the service
file stays. LCM needs the capability for two things:

- **Port 443**, if you choose mode C or put the web UI itself on 443
  (`LCM_PORT=443`).
- **Port 80**, only while an HTTP-01 check by Let's Encrypt is running.

The defaults 9310, 9320 and 9330 are above 1023 and need nothing.

In the **firewall** you still release the ports yourself. In mode C that is the
shared port, with HTTP-01 port 80 comes on top
([Public addresses & address mode](/en/guides/adressen/#which-ports-to-open)).

If you built the service file yourself or kept one from an older version, the
capability is missing. LCM then does not open port 80 or the shared port, keeps
running on the other ports and names the reason in the journal. In that case copy
the lines `CapabilityBoundingSet=CAP_NET_BIND_SERVICE` and
`AmbientCapabilities=CAP_NET_BIND_SERVICE` from the current `packaging/lcm.service`
into your file. Or choose high ports (for example `":8443"` and `":8080"`) and
forward 443 and 80 there with a firewall.

In the **Docker container** LCM stays on high ports and Docker publishes them on
80 and 443: `-p 443:8443 -p 80:8080` with `shared_tls_listen: ":8443"` and
`acme_http_listen: ":8080"`. See the image readme (`techeve/lcm` on Docker Hub).

## Agents and certificates

Agents check LCM's certificate on every connection. What counts for them depends
on where the certificate comes from.

- **Public certificate authority (Let's Encrypt and others).** The agent trusts
  the certificate through its system's trusted certificates and checks the name
  of its address. This works with every agent. What matters is that it was set up
  with the name the certificate carries:

  ```sh
  sudo lcm-agent enroll https://agent.example.com <token>
  ```

- **Your own (private) certificate authority.** The server's token carries,
  besides the fingerprint of the certificate, the certificate authority that
  issued it and LCM's own authority. A **new agent** that you set up with such a
  token pins the private authority. After that every certificate from it **that
  carries the name of its address** is valid, even after a renewal with a new
  key. For this the certificate file must contain the chain up to the issuer,
  otherwise LCM cannot put the authority into the token. So set agents up with a
  token that LCM generated **after** the certificate was put in place (*Renew
  agent token*, then run `lcm-agent enroll` again).
- **Older and already enrolled agents** do not know the private authority. They
  follow the certificate's fingerprint, the key they remembered, or the LCM
  certificate. They do not accept a certificate from a private authority with a
  new key. An agent on the new version that stays connected through one of its
  previous anchors learns the authority from the chain itself. So plan the switch
  to a private authority ahead: update the agents, or issue the certificate with
  the same key.
- **Fallback.** If an agent calls LCM by IP address or under a name no
  certificate carries, it gets the LCM certificate. All agents from 2.1 on trust
  it, because they remember LCM's own certificate authority. They also stay
  connected after you remove a certificate of your own.
- **The setup wizard and the download command** take the fingerprint and key from
  the certificate that LCM actually serves for the name of the agent address.
  Enter the address for agents under *Settings → General* if it differs from the
  name of the web UI.

Behind a reverse proxy the notes under
[LCM Remote](/en/guides/remote/#behind-a-reverse-proxy) still apply.

## MCP with TLS

The MCP interface **always speaks HTTPS**, in every mode and on existing
installations too. LCM picks the certificate from the pool by the name under which
the client calls MCP.

- Clients call `https://<name>:9330/mcp`, over the shared port (mode C)
  `https://<name>/mcp`. A client or reverse proxy using `http://` has to switch to
  `https://`.
- MCP binds to the same address as the web UI. There is no field of its own for
  the bind address.
- You set up the certificate for the name as for the other services, at the gear
  of the MCP address. With Let's Encrypt the certificate carries the MCP name as
  soon as MCP is on.

For keys and tools see [MCP interface](/en/guides/mcp/).

## When something does not work

| Observation | Cause and remedy |
|---|---|
| The browser still warns | `lcm tls status` shows which certificate applies to the name of the address. "LCM certificate" means: none of your own names it. |
| "Chain incomplete" | The file contains only the certificate. Use the file with the chain (`fullchain.pem`). |
| "Certificate and key are swapped" or "does not belong to this certificate" | The files do not belong together. |
| "The key cannot be read" | The key is protected with a passphrase. LCM needs it without one. |
| LCM does not open port 80 or 443 | The capability is missing, see [Freeing port 80 and 443](#freeing-port-80-and-443). That affects only self-built or old service files. |
| Saving the public addresses fails | In mode C the web UI needs a name, and names that are set must differ. In every mode the ports must differ. |
| Agent or MCP gets no answer at its own name | The service is off, or in mode C its name is missing. Switch it on under *Settings → General*. |
| An MCP client with `http://` no longer gets through | MCP always speaks HTTPS. Switch the client to `https://`. |
| Let's Encrypt cannot be switched on | No address carries a DNS name. LCM skips IP addresses and `localhost`. Enter addresses with names and save the page. |
| Let's Encrypt fails with HTTP-01 | Does port 80 reach LCM from the internet, and does the name point to this host in DNS? Port 80 is open only during the check. The card names the error of the last run. Behind a proxy, DNS-01 helps. |
| Let's Encrypt fails with DNS-01 | `Test DNS` shows whether provider and zone fit together. For slow providers a longer wait (`propagation_timeout`) helps. |
