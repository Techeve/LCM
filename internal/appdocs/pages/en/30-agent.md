# Installing the LCM agent

Normally LCM connects to a server over **SSH**. For machines that cannot be
reached from outside - behind NAT, in someone else's network, in a home office -
there is the **agent**: a small service that runs on the server and opens the
connection **outbound, by itself**. No port needs to be opened on the server.

> Which route you pick depends on reachability, not on features: scans, package
> updates and actions are available either way.

## Before you start

The agent connects to a **dedicated port** on the LCM server - not the one the
web interface uses. That separation is deliberate: the interface port offers no
agent endpoint and vice versa. LCM shows you the address in the next step; it
looks like this:

```
https://lcm.example.com:9320
```

So it is the **LCM server** that needs to be reachable, not the server you want
to manage. If the LCM server sits behind a firewall, that one port has to be
forwarded to it.

## Step 1 - Create the server in LCM

In LCM click **"+ Add server"**, choose the **Agent** mode at the top and give
it a **name**. That is all it takes.

LCM creates the server as **offline** for now and generates an **enrollment
token**. The next page shows ready-made commands for the target server, with
the address and token already filled in.

> The token is shown **exactly once**. It cannot be retrieved afterwards
> because LCM only stores its hash. Copy it right away; if it gets lost, just
> create a new one via **"Regenerate token"**.

## Step 2 - Install the agent on the server

Sign in to the server you want to manage. There are two routes - follow the
commands LCM shows you; here they are for context:

**With the package repository (recommended)** - the agent then gets updates
like any other package:

```bash
# set up the TechEve repository once (skip if already present)
curl -fsSL https://repo.techeve.de/setup.sh | sudo sh
sudo apt install lcm-agent
```

**Without the repository** - download the binary straight from the LCM server.
Handy for a quick test, but you then have to apply updates yourself.

## Step 3 - Enroll the agent

```bash
sudo lcm-agent enroll https://lcm.example.com:9320 <token>
```

This needs **root**, because the agent is installed as a system service. The
command tests the connection first and only then writes the configuration - so
a wrong token or an unreachable address shows up immediately, not later in the
service log.

After that the agent connects on its own, the server goes **online** in LCM,
and the first system scan starts automatically.

## Checking that it works

On the server:

```bash
systemctl status lcm-agent
```

In LCM: the server shows as **online** and, after the first scan, reports its
operating system, packages and hardware.

## When it does not work

| Problem | Cause and remedy |
|---|---|
| `enroll` reports the connection failed | Address or port are wrong - it is the **agent port**, not the interface one. Check from that server whether the LCM server is reachable. |
| `enroll requires root` | Run it with `sudo`. |
| Server stays offline | `systemctl status lcm-agent` and `journalctl -u lcm-agent -n 50` show the cause. Often: the token was typed instead of pasted. |
| Token lost | Use **"Regenerate token"** on the server in LCM. The old one becomes invalid immediately and the agent has to be enrolled again. |

## What is different on an agent server

The agent runs as a root service - there is no service user as on the SSH
route. Accordingly, the SSH-specific functions (SSH hardening, key rotation,
reconnect) are hidden for these servers. Everything else - scans, updates,
actions, schedules - works as usual.

To remove it: delete the server in LCM and, on the target system, run

```bash
sudo lcm-agent uninstall
```
