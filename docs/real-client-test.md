# Testing with a real RustDesk client

The automated tests send the same requests the RustDesk app sends, but nothing beats
trying it with the real app. Work through this list after installing or upgrading.
Each step says what to do in the RustDesk app and what to check in the web console.

## What you need

- `hbbs` and `hbbr` (open-source RustDesk server) and RustDesk Manager running, for
  example with `docker-compose.example.yml`.
- Two computers with the standard RustDesk desktop app, called **A** (the one you
  control from) and **B** (the one being controlled). A phone can stand in for A for
  most steps.
- An admin account in the web console, and a normal user, e.g. `alice`, created under
  **Users**.

Tick each box as you go. If something fails, note the RustDesk app version and look at
the server log (`docker compose logs manager`).

## 1. Connect the apps to your server

On A and B: **Settings → Network → ID/Relay server**.

- [ ] **ID server**: your `hbbs` host. **Relay server**: your `hbbr` host. **Key**: the
      contents of `id_ed25519.pub` from `hbbs`.
- [ ] **API server**: the `PUBLIC_URL` of RustDesk Manager, e.g.
      `https://rustdesk.example.com` (no trailing slash).
- [ ] Within a minute, A and B appear under **Devices**, shown as online, with the
      right hostname, OS, RustDesk version and signed-in user.
- [ ] Open B in **Devices**: CPU, memory and OS details are filled in.

## 2. Sign in from the app

- [ ] On A, click the account icon (or **Login**) and sign in as `alice`. The app shows
      her name. In the console, **Users → alice → Sessions** lists this sign-in.
- [ ] A wrong password is refused with a clear message. After 10 wrong attempts,
      sign-in is blocked for 15 minutes.
- [ ] Turn on 2FA for `alice` (sign in to the console as `alice`, **My account**). Sign
      out on A and back in: the app now asks for the 6-digit code.
- [ ] If you set up single sign-on: the login dialog shows **Continue with …**, and
      it signs `alice` in.
- [ ] **Users → alice → Sessions → Revoke**: the next time A talks to the server (for
      example when you open the address book), it is signed out.

## 3. Owner and accessible devices

- [ ] In the console, set B's owner to `alice` (**Devices → B → Edit**).
- [ ] On A, open the **Accessible devices** tab: B appears, grouped under `alice`.
- [ ] Put B in a device group, and give a user group access to it under **Users →
      User groups**. A member of that user group sees B under **Accessible devices**;
      a user outside it doesn't.

## 4. Address books

- [ ] On A (signed in as `alice`), add B to the **Address book** with a tag and an
      alias. In the console, **Address books → Personal** shows `alice`'s book with
      that entry.
- [ ] Edit the entry in the console. After a refresh on A, the change shows there.
- [ ] Create a shared book, share it with `alice` as read/write, and add B with a
      password. On A, the shared book appears; connecting to B from it doesn't ask
      for the password.
- [ ] Share it read-only instead: A can no longer edit entries in that book.

## 5. Strategies

- [ ] Create a strategy that turns off the clipboard, and assign it to B. Within a
      few seconds, B's **Settings → Security** shows the clipboard turned off, and it
      can't be changed there.
- [ ] Remove the strategy from B: the setting can be changed again.

## 6. Sessions and the audit log

- [ ] From A, connect to B with B's password. In the console, B shows an active
      session; **Audit log → Sessions** has a row with A's ID and name, IP, type
      "Remote control" and the login method.
- [ ] **Devices → B → Disconnect**: the session on A closes within a few seconds.
- [ ] Transfer a file from A to B. **Audit log → File transfers** shows the path,
      direction and size.
- [ ] Type a wrong password many times when connecting to B. **Audit log → Alarms**
      shows the failed attempts.
- [ ] Session note: on A, turn on **Settings → General → Ask for note at end of
      connection** (A must be signed in). Connect to B, close the session, and type a
      note when asked. The note appears on the session row in **Audit log →
      Sessions** and in the CSV export.
- [ ] **Switch sides** (desktop only): during a session, use **Switch sides** in the
      toolbar. It works, and the server log shows no errors about `switch-grant`.

## 7. Install scripts

- [ ] Create a deploy token under **Settings → Deploy tokens**. On a new machine with
      RustDesk installed, run as administrator:
      `rustdesk.exe --assign --token <token> --device_group_name "Office" --user_name alice`.
      The machine appears in **Devices**, in the "Office" group, owned by `alice`.
- [ ] Turn on **Only accept registered devices** under **Settings**. A machine that
      isn't registered yet doesn't appear; one assigned with a token does.

## 8. Web console

- [ ] **My account → Passkeys → Add passkey** with your browser or phone, sign out,
      and sign back in with **Sign in with a passkey** (needs HTTPS at `PUBLIC_URL`).
- [ ] **Audit log → Admin actions** lists what you changed during this test, with your
      username and IP.
- [ ] Create an API token and run
      `curl -H "Authorization: Bearer <token>" <PUBLIC_URL>/admin/api/devices`: you get
      the device list. Revoke the token: the same command returns 401.
- [ ] If `METRICS_TOKEN` is set: `curl -H "Authorization: Bearer <METRICS_TOKEN>"
      <PUBLIC_URL>/metrics` shows `rdm_devices_online` matching the console.

## 9. Sign out

- [ ] Sign out on A. **Users → alice → Sessions** no longer lists A, and the
      **Accessible devices** and shared address books are gone from A.
