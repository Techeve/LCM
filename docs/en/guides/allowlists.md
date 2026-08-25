---
sidebar:
  order: 20
title: IP allowlists
description: Named, reusable lists of source IPs for firewall, fail2ban and CrowdSec.
---

IP allowlists are **named, reusable lists of source IPs/networks** (IPv4/IPv6,
incl. CIDR). They are a **shared pool**: not tied to a single feature, but
selectable in several places. Instead of typing the same office IPs into ten
firewall rules, you maintain them **once** in a list `Office` and select that
everywhere.

## Manage

*Settings → Allowlists*: create, edit, delete lists. Each list has a name and
any number of entries (one IP or CIDR per line, or comma-/space-separated). On
save the entries are validated, **canonicalized**, deduplicated and sorted.

Example - a mixed v4/v6 list `Admins` as typed:

```
203.0.113.7
10.0.0.0/24
2001:db8:1::/48
::1
198.51.100.42/32
```

After saving the entries are in canonical form (e.g. a `/32` or a single address
is normalized consistently, duplicates dropped). Invalid entries (typos, broken
CIDR) are **rejected** - the whole list is then not saved, with a clear error
message.

More useful lists as a template:

| List name | Example entries | Purpose |
| --- | --- | --- |
| `Office` | `203.0.113.0/24`, `2001:db8:office::/48` | fixed site ranges |
| `VPN` | `10.8.0.0/24` | dialed-in administrators |
| `Monitoring` | `198.51.100.10`, `198.51.100.11` | probe/scraper hosts |
| `LCM-Host` | `203.0.113.5` | the LCM instance itself (e.g. for the central CrowdSec LAPI) |

## Use

The same lists can be assigned in three places via **multi-select** - the
selection is the union of their IPs:

- **Firewall rule** (server firewall dialog / group rule): selecting an
  allowlist on a rule opens the port **only for the source IPs of those lists**
  - nobody else. Without an allowlist the rule applies to all sources as
  before. Rendered as `ufw allow … from …`, firewalld `source address="…"` or
  nftables `ip/ip6 saddr { … }`. On the same rule you can additionally enter
  **your own IPs/networks** directly - they are unioned with the allowlist IPs
  (see [Firewall](/en/guides/firewall)).
- **fail2ban**: the allowlist IPs go into `ignoreip` (never banned).
- **CrowdSec**: the allowlist IPs form a parser whitelist (no decisions against
  those sources).

For fail2ban/CrowdSec the LCM source IP is always added automatically
(lockout protection).

## Interplay with own IPs at the firewall

On a firewall rule you can freely combine **named lists** and **rule-local IPs** -
the result is always the union. An example for an SSH rule (port 22/TCP):

- selected allowlists: `Office` (`203.0.113.0/24`) + `VPN` (`10.8.0.0/24`)
- rule-local IPs in the field: `198.51.100.9, 2001:db8:beef::1`

SSH is then effectively open for the sources `203.0.113.0/24`, `10.8.0.0/24`,
`198.51.100.9` and `2001:db8:beef::1` - all other sources are blocked. The IP
version follows each entry automatically (v4 or v6). If the content of one of the
lists changes later (e.g. a new VPN range), the **firewall policy rule** re-applies
the ruleset automatically on its next run - the individual rules need not be
touched.

:::note[Deleted or empty lists]
If a firewall rule only references allowlists that are empty or deleted, LCM
does **not** open the port (it stays closed) - never accidentally "from
anywhere". For fail2ban/CrowdSec, empty lists simply contribute no extra IPs.
:::

## Best practice

- **Few, meaningful lists instead of many IPs on rules.** A list `Office` you
  select everywhere is maintainable in one place; the same IP on ten rules is ten
  places to get it wrong.
- **CIDR instead of single addresses** where the source is a whole network
  (`203.0.113.0/24` instead of 254 single lines).
- **Keep v4 and v6 together.** If a site is dual-stack, both prefixes belong in
  the same list - otherwise the rule applies to only one of the two IP versions.
- **Don't forget the LCM instance.** If you restrict SSH via an allowlist, the IP
  **LCM** reaches the server from must be included - otherwise you lose management
  access. (On the SSH rule LCM does protect the SSH port in principle, but a
  source restriction can lock LCM itself out.)
- **For the central CrowdSec LAPI**, maintain a list with the source IPs of the
  managed servers and open the LAPI port (8080) only for those (see
  [Security tools](/en/guides/security-tools)).
- **Empty = closed.** To temporarily revoke an opening, empty the list - the
  referencing firewall rules then safely close the port instead of opening it.
