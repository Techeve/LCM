---
sidebar:
  order: 14
title: Time & NTP
description: Time zone, clock comparison and time servers of managed servers.
---

A clock that runs wrong is the least conspicuous fault in operation: the system
keeps running and reports nothing. The consequences show up elsewhere and look
like something else entirely.

:::caution[What a wrong clock breaks]
- **TLS** - certificates appear "not yet valid" or "expired"; HTTPS connections
  fail with misleading messages.
- **Package sources** - signed metadata has a validity window. With the clock
  set too early, `apt` rejects it as "not valid yet".
- **Logs** - events from several servers can no longer be lined up; root cause
  analysis turns into guesswork.
- **One-time passwords (TOTP)** - codes are valid in 30-second windows.
- **Kerberos/AD** - tickets break at roughly 5 minutes of skew.
:::

## What LCM records

On every scan - and on demand via **Check time** - LCM reads, read-only and
without special privileges:

| Value | Source |
|---|---|
| **Time zone** | `timedatectl`, else `/etc/timezone` or the `/etc/localtime` symlink; failing all that, the abbreviation from `date +%Z` |
| **Time service** | chrony, systemd-timesyncd, ntpd or busybox-ntpd - whichever runs |
| **Synchronized?** | what the service itself reports (`chronyc tracking`, `timedatectl`, `ntpq`) |
| **Time servers** | the servers configured on the host |
| **Offset** | comparison of the server clock against LCM's own |

The offset includes the SSH round trip and is accurate to a second or two. For
the question that matters - *is the clock right at all?* - that is plenty, so
it is reported only from **30 seconds** upward.

## Setting it

Server detail → **Settings** (gear) → section *Time & sync*:

- **Set time zone** - LCM writes it and **reads it back**. If the system then
  reports something else, it does not count as set; a written file alone is no
  proof.
- **Configure time servers** - enters the servers, starts the time service and
  **proves synchronization**. If the proof does not arrive within the window,
  LCM says so honestly (HTTP 502); the configuration stays in place, because it
  is not wrong - it just has not taken effect yet. Existing entries in
  `chrony.conf`/`ntp.conf` are replaced line by line, the rest of the file is
  left alone, and a `.lcm-bak` backup is created.

:::note[Containers have no clock of their own]
Inside a container (LXC, Docker, …) the clock **comes from the virtualization
host** and cannot be set there; `systemd-timesyncd` refuses to start for
exactly that reason (`ConditionVirtualization=!container`). LCM therefore
declines the action with a clear note instead of offering something impossible.
The **offset is still reported** - a host running wrong drags all its
containers with it, and the sync then belongs on the host.
:::

## Defaults

Under **Settings → Time & NTP** you can store:

- **Default time servers** (one `Label = host` per line) - they appear as a
  choice when configuring. Empty = built-in list (NTP pool, Cloudflare,
  Google).
- **Default time zone** - pre-fills the form.

Both are purely pre-fills: time is always set deliberately per server, never
silently in the background.

## In the traffic light

- Offset **≥ 30 seconds** → **warning** (in containers with the pointer to the
  host).
- **No time service** on a non-container → note. The clock is right just now,
  but nothing holds it - nothing is broken yet.
- Service running but reporting **not synchronized** → note.
