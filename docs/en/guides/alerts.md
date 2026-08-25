---
sidebar:
  order: 18
title: Alerts & Notifications
description: Rule-based alerts (disk, CVEs, heartbeat …) with severity, cooldown, and delivery via email or webhook (e.g. Microsoft Teams).
---

LCM periodically evaluates monitoring criteria and notifies on
exceedance through configurable channels. The evaluation runs as a
system-global schedule - **fixed at every 30 minutes**; like the other core
functions the interval is not configurable.

## Notification channels

Under *Settings → Notifications* you create channels. A channel is referenced
by alert rules and is protected against deletion as long as a rule uses it.

- **Email (SMTP)** - a dedicated outbox per channel (host, port, sender,
  recipients; password stored encrypted).
- **Webhook** - HTTPS POST to a target URL, either as **generic JSON**
  (for your own automations) or in **Microsoft Teams format** (Adaptive
  Card). For Teams, create a workflow "When a Teams webhook request is
  received" and paste its URL into LCM. The URL is a secret - it is stored
  encrypted and never shown again; HTTPS is mandatory (HTTP only for
  localhost testing).
- **System email (default mailer)** - the managed channel of the
  [default email delivery](#default-email-delivery-system-mailer): no
  configuration of its own, recipients are the admin addresses configured
  there. Toggle it under *Settings → General*.

### Webhook target on your own network

A public service like Teams brings its own certificate - for a receiver on
your internal network you have to provide one yourself. Two points are not
negotiable:

- **HTTPS is mandatory.** `http://` is only accepted for `localhost`,
  `127.0.0.1` and `::1`; a target on the LAN must be `https://`.
- **The certificate is verified.** LCM does not disable the check. A
  self-signed certificate the LCM host does not trust results in failure -
  even if the receiver is reachable.

For your own receiver that means: issue a certificate that carries **the IP
address (or name) as a SAN**, and install the issuing CA on the **LCM host** -
not on the receiver.

```bash
# On the LCM host: mark your own CA as trusted
sudo cp my-ca.crt /usr/local/share/ca-certificates/
sudo update-ca-certificates
sudo systemctl restart lcm
```

The restart is required because the service reads the certificate store at
startup. Verify before creating the channel - if this fails, the webhook will
too:

```bash
curl https://<receiver>:<port>/   # without -k: must complete without certificate errors
```

:::tip[Press "Test" before building rules]
Every channel has a **Test** button that sends a sample message. It reports the
target system's error in plain text - far quicker than waiting for a real alert
and wondering why nothing arrives.
:::

## Default email delivery (system mailer)

Independent of the alert channels, *Settings → General* holds the
**default email delivery**: the system outbox for transactional mail -
**password-reset links**, **invitation links** for new users, and notices to
the admin recipients. A checkbox additionally offers it as a notification
channel (see above); the "Send test message" button verifies the saved
configuration.

For users to use the self-service reset ("Forgot password?" on the login
page), their account needs an **email address**. The reset link is valid for
1 hour and single-use; the endpoint never reveals whether an address exists
(no user enumeration) and is rate-limited per client IP.

## Alert rules

Under *Settings → Alerts*. Each rule binds a criterion to a channel
and optionally to **server groups** (without a selection it applies to all
servers) - so thresholds can be set with different strictness per
infrastructure. Several groups can be selected at once; a server in more than
one selected group is still evaluated only once.

| Type | Trigger |
|---|---|
| **Disk capacity** | usage reaches the percentage threshold |
| **Storage forecast** | the linear extrapolation exceeds the limit within the deadline |
| **Security/CVE** | CVE findings from the configured minimum severity |
| **Overdue updates** | number of open package updates exceeds the threshold |
| **Heartbeat** | last server contact is more than *n* hours ago |
| **Reboot required** | the system itself requests a reboot after an update (e.g. a new kernel) - a plain yes/no criterion without a threshold |
| **APT cache unreachable** | the central [apt-cacher-ng](/en/guides/apt-cache/) does not respond. Applies only to the LCM host running the service, and stays silent while no URL is configured under *Settings → APT cache* |
| **Deep scan** | the last [deep scan](/en/guides/deep-scan/) produced warnings or critical findings (hardening/misconfiguration or a kernel reboot gap) - a plain yes/no criterion without a threshold |
| **CrowdSec LAPI unreachable** | the central CrowdSec LAPI does not respond or rejects the stored machine login. Applies only to the LCM host and stays silent while no LAPI is configured under *Settings → CrowdSec* |
| **CVE database outdated** | the vulnerability database of the CVE scanner is older than 48&nbsp;hours or was never downloaded. This matters because an old database reports no error but returns outdated results - from the outside that looks like “no vulnerabilities”. Applies only to the LCM host |
| **System backup overdue** | automatic backups are enabled, but the newest backup is older than **twice the interval** - or none exists at all. Deliberately measures the outcome instead of individual failed attempts: however the backup went missing, the missing state is what gets reported. Applies only to the LCM host |

## Examples per alert type

All examples are created as a rule under *Settings → Alerts*. Where no
threshold is given, the built-in default applies.

- **Disk capacity** - "Root partition getting tight": threshold **85%**,
  group *Databases*, severity *warning*. Without a threshold the default
  **90%** applies.
- **Storage forecast** - "Filling up soon": deadline **7 days**, all servers,
  severity *warning*. The linear extrapolation from the
  [storage history](/en/guides/monitoring/) warns before the disk actually
  fills up (default deadline: 10 days).
- **Security/CVE** - "Critical gaps immediately": minimum severity
  **critical**, all servers, severity *critical*. A second, looser rule with
  minimum severity **high** just for the group *Production* (default: `high`).
- **Overdue updates** - "Patch backlog": allowed number of open updates
  **0** (any pending update fires), group *Production*. More generous for
  staging, e.g. **20**.
- **Heartbeat** - "Server went quiet": timeout **6 hours**, all servers,
  severity *critical* (default: 24 hours).
- **Reboot required** - "Kernel waiting for reboot": no threshold, group
  *Production*, severity *warning*. Fires as soon as a server itself requests a
  reboot after an update.
- **APT cache unreachable** - no threshold; only meaningful if the LCM host
  runs apt-cacher-ng and a URL is configured under *Settings → APT cache*.
- **Deep scan** - "Report hardening findings": no threshold, severity
  *warning*. Fires as soon as a deep scan yields warnings or critical findings.

## Severity & cooldown

Each rule has a **severity** and a **cooldown**: the most recent alert per
(rule, server) debounces further notifications, so that no alert spam arises.
**`0` does not mean “no cooldown”** - it selects the built-in default of
**360&nbsp;minutes (6&nbsp;h)**; a shorter cooldown must be entered
explicitly (minimum&nbsp;1). Triggered alerts land in the **alert history**
(including delivery status); it is cleaned up after the log retention period.

## Interplay

The alert evaluation uses the same data as the monitoring - among them the
CVE findings from the [CVE scan](/en/guides/security-cve/) and the
[storage history](/en/guides/monitoring/). No duplicate collection is
therefore needed; alerts are the notification layer over the existing inventory.
