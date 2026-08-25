---
sidebar:
  order: 14
title: Firewall (ufw / firewalld / nftables)
description: Multi-distro firewall with detailed rules and port suggestions.
---

LCM manages each server's host firewall - with the tool that matches the
distribution, and detailed allow rules (port, TCP/UDP, IP version, allowed
sources, note).

## Backend per distribution

| Distribution | Firewall tool |
| --- | --- |
| Ubuntu | **ufw** |
| RHEL, Rocky, AlmaLinux, Fedora, CentOS Stream, openSUSE/SLES | **firewalld** |
| Debian, Arch, Alpine | **nftables** (direct, LCM-owned table) |
| other distributions | ufw (default) |

**Conflict check:** before installing anything, LCM always checks whether a
different firewall tool is already installed. If one exists, **that** one is
used - LCM never installs a second firewall next to an existing one (two
packet filters fight each other). If none exists, LCM installs the designated
tool automatically.

## Rules

Server detail → **Firewall**. Each rule consists of:

- **port** (1-65535),
- **protocol** - TCP or UDP,
- **IP version** - IPv4, IPv6 or both,
- **allowed sources** (optional) - named allowlists and/or your own IPs and
  networks (CIDR); without a selection the rule applies to every sender,
- **note** (optional) - what this rule is for. Where the tool supports
  comments (ufw, nftables) it also ends up in the rule set on the target.

:::note[No bind/destination address any more]
Up to LCM 1.13.0 there also was a bind/destination address: "on which local
address is the port open". Next to the allowed sources ("who may connect")
this caused more confusion than it was worth - both fields sounded alike and
meant different things. The field is gone; the question you actually want to
answer is answered by the sources. A "bind" in an older stored rule set is
ignored when read: the port is then open on all local addresses, while the
sources keep applying as stored.
:::

The **SSH port is always open** (lockout protection); everything else is
blocked when enabled. Applying runs as a job - including installing the tool
when missing, and verifying that the firewall is actually active afterwards.

### SSH rule (not deletable, sources editable)

The SSH allow rule is shown in the editor as the **top, locked row** - with the
server's actual SSH port (even if it differs from 22). It **cannot be deleted**
(that would lock you out); port and protocol are read-only. What you *can* set
are the **allowed sources**: restrict SSH to specific sender addresses or
networks. Empty = reachable from anywhere. The address LCM reaches the server
from is offered as a template - if it is missing from the list, the editor
warns, because that would lock LCM itself out.

### Suggestions from the port scan

The hardware scan inventories the server's **listening services**
(`ss -tulnp`, TCP and UDP, loopback excluded). The firewall dialog shows them
as suggestion chips - one click turns port, protocol, IP version and the
service name (as the note) into a rule.

Ports published by Docker do **not** appear as suggestions, only as a note:
Docker writes its own forwards into the packet filter and bypasses the host
firewall entirely. A rule for such a port changes nothing about reachability -
it would only pretend the port were governed by LCM. To keep it closed from
outside, bind the container publication instead (`127.0.0.1:8080:80` rather
than `8080:80`).

## Per-backend particularities

- **ufw** - declarative rebuild (`ufw --force reset` → rules → enable). A
  v4-only/v6-only rule is expressed via the destination `0.0.0.0/0` or `::/0`
  (a ufw particularity); without one, a rule applies to both families.
- **firewalld** - LCM manages the **default zone declaratively**: all ports
  and rich rules of the zone are re-set (rules with a family or source
  restriction become rich
  rules). Zone *services* (e.g. the preconfigured ssh service) are left
  untouched. A single `--reload` activates everything at once.
- **nftables** - LCM owns its **own table `inet lcm`** and never touches
  foreign tables (Docker, fail2ban, …). The ruleset is replaced atomically
  (one transaction, no rule-less window), syntax-checked with `nft -c` first,
  and persisted via `/etc/nftables.d/lcm.nft` + include (systemd or
  OpenRC/Alpine). Disabling only removes the LCM table.

:::note[Restricted sudo mode]
In restricted mode only **ufw** is manageable (it is on the sudo whitelist).
firewalld and nftables need full root access (service control, files under
`/etc`) - LCM reports that honestly instead of half-configuring.
:::

## Group rule

Under **Groups**, the firewall can be defined as a **policy rule** (enforce) -
with the same rule editor. On every connection LCM checks the actual state
(a cheap hash/set comparison per backend) and only re-applies the ruleset on
drift; a missing tool is installed along the way. Existing rules in the old
format (port list `80,443`) keep working unchanged.

:::note[Docker & Proxmox]
Container ports **published by Docker** bypass the host firewall (Docker
filters ahead of the INPUT chain - with all three backends). LCM shows these
ports honestly on the server but deliberately does not intervene. **Proxmox**
systems are excluded: pve-firewall manages the rules there.
:::

## Source restriction: allowlists and custom IPs

Each rule can restrict its **allowed sources** - in two ways that combine
freely:

- **[IP allowlists](/guides/allowlists)** (shared pool): pick one or more
  named lists via multi-select.
- **Custom IPs/networks**: enter individual IPv4/IPv6 addresses or CIDR
  networks directly on the rule (comma or space separated), e.g.
  `203.0.113.7, 10.0.0.0/24` - no named list required.

With at least one source set, the rule opens the port **only for the union of
those source IPs** - all other sources are blocked. Without sources the rule
applies to all, as before. Rendered as `ufw allow … from …`, firewalld
`source address="…"` or nftables `ip/ip6 saddr { … }`. When list contents
change, the firewall policy rule re-applies the ruleset automatically on its
next run.

:::caution[Empty sources never open]
If a rule's source restriction resolves to **zero IPs** (e.g. because the
referenced allowlist was emptied or deleted), the rule is **skipped** when
applying - the port stays closed. LCM never silently turns a restricted rule
into an open one.
:::

## Rendered rule examples

To make tangible what LCM actually writes onto the server per backend, here is
the same rule intent in all three tools. Each example shows the line(s) produced
from **one** rule in the editor.

### A simple port (HTTP 80/tcp, all addresses, all sources)

Rule: port `80`, TCP, IP version *both*, no source restriction.

| Backend | Rendered rule |
| --- | --- |
| ufw | `ufw allow proto tcp to any port 80 comment 'lcm'` |
| firewalld | `firewall-cmd --permanent --zone=<zone> --add-port=80/tcp` (simple allow, no rich rule) |
| nftables | `tcp dport 80 accept` |

### IPv4-only or IPv6-only (HTTPS 443/tcp)

Rule: port `443`, TCP, IP version *IPv4* (or *IPv6*).

| Backend | IPv4 only | IPv6 only |
| --- | --- | --- |
| ufw | `ufw allow proto tcp to 0.0.0.0/0 port 443 comment 'lcm'` | `ufw allow proto tcp to ::/0 port 443 comment 'lcm'` |
| firewalld | `rule family="ipv4" port port="443" protocol="tcp" accept` | `rule family="ipv6" port port="443" protocol="tcp" accept` |
| nftables | `meta nfproto ipv4 tcp dport 443 accept` | `meta nfproto ipv6 tcp dport 443 accept` |

ufw has no explicit family option and therefore expresses a v4-only/v6-only
allow via the wildcard destination `0.0.0.0/0` or `::/0` - without a destination
the rule applies to both families.

### Source restriction (only specific senders)

Rule: port `443`, TCP, source `203.0.113.7` (from an allowlist or
entered as a custom IP).

| Backend | Rendered rule |
| --- | --- |
| ufw | `ufw allow proto tcp from 203.0.113.7 to any port 443 comment 'lcm'` |
| firewalld | `rule family="ipv4" source address="203.0.113.7" port port="443" protocol="tcp" accept` |
| nftables | `ip saddr { 203.0.113.7 } tcp dport 443 accept` |

Multiple sources produce one line per source in **ufw**, one rich rule per
source (split by family) in **firewalld**, and a combined set in **nftables**
(`ip saddr { a, b, c }`).

### Mixed IPv4 + IPv6 in one rule

Rule: port `443`, TCP, IP version *both*, sources `203.0.113.7` **and**
`2001:db8:acab::1` (e.g. an allowlist with v4 and v6 entries). LCM splits the
sources cleanly by address family - each line matches its family:

| Backend | Rendered rule(s) |
| --- | --- |
| ufw | `... from 203.0.113.7 to any port 443 ...` **and** `... from 2001:db8:acab::1 to any port 443 ...` |
| firewalld | `rule family="ipv4" source address="203.0.113.7" ...` **and** `rule family="ipv6" source address="2001:db8:acab::1" ...` |
| nftables | `ip saddr { 203.0.113.7 } tcp dport 443 accept` **and** `ip6 saddr { 2001:db8:acab::1 } tcp dport 443 accept` |

## Why there is no destination address

A packet filter knows two independent axes, and they are easily mixed up:

- **destination address** (`to …` / `destination address` / `daddr`): on
  **which local address** of the server the port is opened at all.
- **source** (`from …` / `source address` / `saddr`): **who** (which sender IP
  or network) may connect to that port.

LCM only offers the source. The reason is practice: whoever wants a service
"reachable internally only" almost always means "only from the internal
network" - the source. Two similar-sounding fields side by side regularly led
to the wrong one being filled in, leaving a port open to everyone while it
looked restricted.

Only one special case is really lost: the same sender may reach a service on
one local address but not on another. If you need that, bind the service
itself to the intended address (in its own configuration) - the more robust
place for it anyway, because it holds even without a firewall.
