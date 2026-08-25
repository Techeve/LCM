# Setting up two-factor login for SSH

Once **SSH 2FA** is enabled on a server, signing in takes two things: your
**SSH key** and a **one-time code** from an authenticator app. The code changes
every 30 seconds and exists only on your device - so copying your key is not
enough to get in.

You set this second factor up **yourself, on the server**. It belongs to your
account there, not to the machine you connect from.

> Until you have set it up, you still get in with your key alone. So the setup
> cannot lock you out - it switches the second factor on for you.

## What you need

- An **authenticator app** on your phone. Any app that does time-based one-time
  codes (TOTP) works - Google Authenticator, Aegis, 2FAS, Microsoft
  Authenticator, or a password manager with TOTP support.
- Working **SSH access** to the server (see
  [Setting up your SSH key](/#/doku/ssh-schluessel)).

## Step by step

1. **Sign in to the server** as usual:

   ```bash
   ssh user@server
   ```

2. **Start the setup:**

   ```bash
   google-authenticator
   ```

3. The first question asks whether the codes should be **time-based**. Answer
   **y** - that is the usual TOTP scheme every app understands.

4. A **QR code** appears in the terminal. Scan it with your app. If the window
   is too small and the code comes out garbled, use the `secret key` printed
   below it: you can type that into the app by hand.

5. **Save the emergency codes.** Right below are five `emergency scratch
   codes`. Each works exactly once and saves you if the phone is gone. Store
   them somewhere safe - not on the same server, and not in the same app.

6. The remaining four questions are best answered like this:

   | Question (in essence) | Answer | Why |
   |---|:-:|---|
   | Save settings to `~/.google_authenticator`? | **y** | Without saving, all of it was for nothing. |
   | Allow the same code more than once? | **n** | An intercepted code could otherwise be reused. |
   | Widen the time window (for inaccurate clocks)? | **n** | Only needed if codes keep being rejected. |
   | Rate-limit login attempts? | **y** | Slows down automated guessing. |

If you prefer it in one go, the same setup without any questions:

```bash
google-authenticator -t -d -f -r 3 -R 30 -w 3
```

## Verify first, close later

**Do not close your current session yet.** Open a **second** terminal window
and sign in there instead. Only once that works is the setup really sound. If
you did lock yourself out, the still-open first session lets you undo it:

```bash
rm ~/.google_authenticator
```

This is what signing in looks like afterwards:

```
Verification code:
```

Enter the six-digit code from the app there - your key is still checked as well.

## When it does not work

| Problem | Cause and remedy |
|---|---|
| No code is asked for at all | 2FA is not enabled on this server, or you have not set anything up yet. Neither is an error. |
| The code is rejected | Usually the clocks drift apart. Check the time on your phone (turn on automatic time). If that does not help, set up again and answer **y** to the wider time window. |
| Phone lost | Sign in with an **emergency code**, then set up again. Without one, only your administrator can help. |
| "Permission denied" right after the key | The second factor failed. Watch for the `Verification code:` line - if it never appears, something is off in the server configuration. |

> Your one-time code secret lives in `~/.google_authenticator` on the server.
> It applies to this account on this server only - on another server you set
> 2FA up again.
