# Adding a Synology NAS

LCM monitors Synology devices through the **DSM web API** - not over SSH. That
is deliberate: SSH is off by default on a NAS and should stay that way. The API
provides everything needed without opening an additional way in.

> Collected are the DSM version, available updates, installed packages, storage
> pools, volumes and drives, the firewall and the security settings. Package
> management, Docker inventory, user sync and CVE assessment do not exist here -
> DSM is not an ordinary Linux.

## Step 1 - Create a dedicated account in DSM

In DSM, under *Control Panel → User & Group*, create a **dedicated account for
LCM** and make it a member of the **administrators** group. Sharing an existing
admin account is a bad idea - with its own account you can tell from the DSM
log what was LCM and what was a person.

> **Important:** two-factor authentication must **not be enforced** for this
> account. A scan runs unattended and cannot enter a one-time code.
>
> To keep the account well protected anyway, restrict it to the LCM server's
> address instead: *Control Panel → Security → Account*. The password is then
> useless from anywhere else.

## Step 2 - Add the device in LCM

In LCM click **"+ Add server"** and pick the **Synology DSM** mode at the top.
Enter:

- a **name** for the overview
- **host/IP** of the NAS
- the **DSM port** - `5001` by default (HTTPS)
- **account** and **password** of the user you just created

## Step 3 - Confirm the certificate

LCM shows the **fingerprint of your NAS's TLS certificate**. Compare it in DSM
under *Control Panel → Security → Certificate* and confirm.

This step is not a formality: Synology ships a self-signed certificate by
default, which cannot be checked against any official authority. LCM therefore
remembers exactly this fingerprint and **refuses to connect if it changes** -
the same safeguard as with the SSH host key.

LCM then collects the state right away and the device appears online.

## When it does not work

| Problem | Cause and remedy |
|---|---|
| Login fails although the credentials are right | 2FA is enforced for the account. Lift the enforcement for this one account and secure it with the IP restriction instead. |
| No connection to the port | The DSM port differs (`5001` for HTTPS, `5000` unencrypted). If it was changed, enter the actual one. |
| Connection aborts with a certificate error | The NAS certificate changed - for instance because a Let's Encrypt certificate was renewed. Add the device again and confirm the new fingerprint. |
| "Access denied" on queries | The account is not in the **administrators** group. DSM's state queries require it. |

## What LCM shows here

LCM reads the values and compares them with the targets. Every finding names
the place in the DSM interface where you fix it. LCM can also set the security
settings to their targets itself, but only when you click.

| Area | Source | What it becomes |
|---|---|---|
| System | `SYNO.Core.System` | model, DSM version, CPU, memory, time zone and time service |
| Updates | `SYNO.Core.Upgrade.Server` | a finding when a newer DSM version is available |
| Packages | `SYNO.Core.Package` | the **Packages** tab; DSM has no separate Apps tab |
| Storage | `SYNO.Storage.CGI.Storage` | the **Storage** tab: every volume on its own, storage pools and drives with their state |
| Security Advisor | `SYNO.Core.SecurityScan.Status` | findings of the risk and danger levels |
| Firewall and security | `SYNO.Core.Security.*` and others | compared with the targets below |

### Storage

Volumes are listed under **Storage** by their path (`/volume1`). Each one can
be monitored on its own with its own warning threshold - like a drive of a
Linux server. The sum of all volumes feeds the storage history.

Storage pools, volumes and drives are always assessed:

| State in DSM | Status light |
|---|---|
| drive failed, SMART reports failure, pool or volume crashed, volume read-only | red |
| pool without full redundancy (degraded), repair running, SMART warning, too many bad sectors, remaining life below the threshold | yellow |

A virtualised DSM (VirtualDSM) reports neither SMART nor temperature. What is
missing is not a finding. Its CPU is shown as "virtual", with the number of
vCPUs.

### Security settings

| Setting | Target | In DSM |
|---|---|---|
| Firewall | on, and the active profile denies what no rule allows: a final "deny all" rule or "If no rules are matched: deny access" | Control Panel → Security → Firewall |
| Enhance browser compatibility by skipping IP checking | off | Control Panel → Security → Security |
| Cross-site request forgery (CSRF) protection | on | Control Panel → Security → Security |
| HTTP Content Security Policy | on | Control Panel → Security → Security |
| Do not allow DSM to be embedded with iFrame | on | Control Panel → Security → Security |
| Clear all saved login sessions upon restart | on | Control Panel → Security → Security |
| Auto block | on, at most 10 attempts within 5 minutes | Control Panel → Security → Protection |
| DoS protection | on if the device is reachable from the internet | Control Panel → Security → Protection |
| TLS/SSL profile level | modern compatibility | Control Panel → Security → Advanced |
| Spectre and Meltdown protection | on | Control Panel → Security → Advanced |

Every deviation is a yellow finding. Only DoS protection is a hint without a
light: LCM cannot tell whether your NAS is reachable from the internet. If it
is, turn it on.

DSM 7.3 reports the TLS/SSL profile level only as a number. Read on a device
are level 1 for "modern compatibility" and level 3 for "old compatibility".
Level 2 counts as "intermediate compatibility", because DSM has only three
levels. LCM shows any other number but does not assess it.

Synology does not document these interfaces, and the field names change with
the DSM version. If LCM cannot interpret a value, it counts as **unknown** and
does not colour the status light. The job log of the collection run (**Jobs**
tab) then shows the reason and DSM's answer, without passwords or session
data. With that answer, the spelling of your DSM version can be added to
LCM.

### Setting targets

On the **Security** tab every deviation has a **Set to target** button. **Fix
all deviations** takes them all at once. A dialog first shows what will change.
Setting runs as a job, afterwards LCM reads the device again. It needs the
`servers:write` permission.

LCM reads the setting, changes only the target value and sends everything it
read back to DSM. All other values stay as they are. The job log shows for
each setting the state before, what was sent and the state after.

For three settings the dialog warns first:

| Setting | Hint |
|---|---|
| DoS protection | Only useful if the device is reachable from the internet. |
| TLS/SSL profile level | Older devices and apps may lose the connection. |
| Spectre and Meltdown protection | Takes effect only after the device restarts. |

LCM does not switch the **firewall**. Turned on with rules that do not fit, it
locks out LCM and you. The rules stay with DSM. Check them under *Control
Panel → Security → Firewall* and turn it on there.

The **Security** tab lists the rules of the active profile, per interface with
action, protocol, ports and source. Below them is what applies when no rule
matches. Disabled rules are marked "off" and do not count. The rules for all
interfaces are what matters. Rules on a single adapter do not protect the
others.

### DSM interface

The **Open DSM** button in the device's header bar opens the DSM sign-in page
in a new tab, at the address and port stored in LCM.

### Updates

Applying DSM updates remains your job - LCM points them out.
