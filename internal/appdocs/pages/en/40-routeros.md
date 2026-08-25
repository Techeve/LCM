# Adding a MikroTik router

LCM can monitor MikroTik devices running **RouterOS**: model, RouterOS version
and whether a newer release is available. Access is **read-only** - LCM changes
nothing on a router.

> RouterOS is not a Linux with a package manager. Packages, Docker, user sync,
> hardening and CVE assessment therefore do not exist here - they are hidden
> for these devices rather than running into nothing.

## Step 1 - Create a user for LCM

LCM only needs **read rights** on the router. Create a dedicated user in the
built-in `read` group instead of sharing an admin account:

```
/user add name=lcm group=read password=<strong-password>
```

That lets LCM query values only - configuration changes are denied to this user
by the router itself.

## Step 2 - Add the device in LCM

In LCM click **"+ Add server"** and pick the **MikroTik RouterOS** mode at the
top. You need:

- a **name** for the overview
- **host/IP** and **SSH port** (usually 22)
- the **user** you just created

LCM then shows the **SSH host key fingerprint**. Compare it against the router
and confirm - LCM remembers this value and refuses to connect if it ever
changes. That protects you from someone slipping in between unnoticed.

## Step 3 - Choose how to authenticate

**With a password:** LCM connects right away, reads version and device data and
adds the router as **online**. The password is stored encrypted.

**With a key (recommended):** LCM generates a key pair and shows you the public
part. The device stays **offline** until you install that key on the router:

1. Upload the public key shown to the router as a file - via *Files* in WinBox
   or with `scp`, named `lcm.pub`.
2. Import it on the router:

   ```
   /user ssh-keys import public-key-file=lcm.pub user=lcm
   ```

On the next refresh LCM connects and the device goes online.

## When it does not work

| Problem | Cause and remedy |
|---|---|
| "No RouterOS detected" | LCM verifies during onboarding that RouterOS really answers. Usually the host points at a different device, or the SSH service on the router is off (*IP → Services → ssh*). |
| Device stays offline after importing the key | The import must name the **same user** LCM signs in as (`user=` in the import command). |
| Login is refused | Check that the user is enabled on the router and that the SSH service is not restricted to certain addresses (*IP → Services → ssh*, field *Available From*). |

## What LCM shows here

Model, RouterOS version, available update and reachability. The status light
follows suit: a device with a pending RouterOS update or without contact stands
out. Updating the router remains your job - LCM only tells you it is due.
