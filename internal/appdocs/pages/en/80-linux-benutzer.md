# Set up Linux users

This page describes what a Linux user needs on the servers managed by LCM and
how they set it up themselves. It is written for two people: the administrator
who creates the user in LCM, and the user.

> LCM distributes the user to the servers. The **user sets their password
> themselves**, through an activation link. Without a password they can log in
> with an SSH key, but **not** with a password, for example in the Proxmox web
> interface or at the server's console login.

## When a password is needed

- **SSH with a key**: no password needed.
- **Login with a password**, for example in the Proxmox web interface or at the
  console login, and `sudo` when the permission profile requires a password.

An account without a password is locked. This is intentional: an account
without a password must not stand open.

## Set a password

1. The administrator sends an **activation link** by email or passes it on. The
   link is valid for 48 hours by default.
2. Open the link. You do not need to log in to LCM.
3. Enter the password and repeat it. The password policy applies here too; the
   strength indicator shows when the password is strong enough.
4. Submit. LCM distributes the password **immediately** to all servers where the
   user is set up.

To change your password, ask for a new activation link. A password you set
directly on the server with `passwd` stays in place as long as LCM has **no**
password stored for the user. If LCM has one, the next sync overwrites the
server password with the one from LCM.

## User details

The administrator maintains the details in LCM under **Linux users**. They are
pushed to the servers on every sync:

| Detail | Effect on the server | Where in LCM |
|---|---|---|
| **Username** | Name of the account. Cannot be changed after creation. | Create |
| **Full name** | Comment field of the account (`chfn`), shown in `getent passwd` | Edit |
| **Email** | Only for the activation link. It is **not** written to the server. | Edit |
| **Shell** | Login shell, for example `/bin/bash` | Edit |
| **SSH keys** | Public keys, any number per account | Keys |
| **Permission profile** | `sudo` rights and directory permissions | Assign |

If someone on the server needs an email address, for example for
`git config user.email` or for forwarding, they set it there themselves. LCM
does not write it to the server.

### What a sync overwrites

LCM is the source of truth for these details. If someone changes them directly
on the server, the next sync restores the state from LCM:

- password, if LCM has one stored
- shell
- full name, if LCM has one stored; if none is set, the value on the server
  is left alone
- SSH keys that LCM manages
- lock and expiry of the account

If a change must stay, make it in LCM.

## Lock and remove

In the **Users** tab of a server, **Disable** locks the account on that server;
**Enable** removes the lock again. **Remove** deletes the account together with
its home directory. The lock survives the next sync. For more, see the page
[SSH keys](/#/doku/ssh-schluessel) and the server guide.

## Set up yourself: password and key

On the activation page the user can set both at once: the password and an SSH
key. LCM can also generate the key for them; they then download the private
part once. The guide to your own key is on the page
[Set up SSH keys](/#/doku/ssh-schluessel).
