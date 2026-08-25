# Adding a Synology NAS

LCM monitors Synology devices through the **DSM web API** - not over SSH. That
is deliberate: SSH is off by default on a NAS and should stay that way. The API
provides everything needed without opening an additional way in.

> Collected are the DSM version, available updates, storage usage and the state
> of the volumes. Package management, Docker inventory, user sync and CVE
> assessment do not exist here - DSM is not an ordinary Linux.

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

DSM version and whether an update is pending, plus storage usage and volume
state. The status light rests on those two: a pending DSM update or a filling
disk stands out. Applying DSM updates remains your job - LCM points them out.
