# RustDesk Manager

Self-hosted API server and admin web UI for RustDesk, for use with the open-source
`hbbs`/`hbbr`. It implements the HTTP API the stock RustDesk client already calls
(normally provided only by RustDesk Server Pro): device inventory, audit logs,
users, address books, strategies, SSO, 2FA and session recordings.

Status: milestone 6 of 7. Working: device inventory, online status, audit logs
(sessions, file transfers, alarms), remote disconnect, user accounts, the
"Accessible devices" tab, personal and shared address books, strategies
(settings pushed to devices), 2FA, single sign-on (OpenID Connect), deploy
tokens for install scripts, and recording uploads. Next: polish (CSV export,
cleanup of old audit data).

## Run with Docker
```sh
cp docker-compose.example.yml docker-compose.yml   # edit PUBLIC_URL and password
docker compose up -d --build
```
Open `http://your-server:21114` and log in as `admin`.

Forgot the admin password? Reset it (works while the server runs):
```sh
docker compose exec manager /rustdesk-manager reset-password admin   # Docker
go run ./cmd/server reset-password admin                              # from source
```
It prints a new random password, or pass one: `reset-password admin 'new-password'`.

## Point RustDesk clients at it
In the client: Settings → Network → ID/Relay server → **API server** =
`http://your-server:21114` (or your HTTPS reverse-proxy URL). If left empty, the
client already defaults to `http://<ID server host>:21114`.

## Testing with a real client
1. Start the server and sign in at `http://<server>:21114` as `admin`.
2. On a test machine, set RustDesk → Settings → Network → **API server** to
   `http://<server>:21114` (same ID server and key as before).
3. Within a few seconds the machine appears under **Devices** as online.
4. Connect to it from a second RustDesk client. The session shows on the
   Dashboard and under **Audit log**, and the device page lists it under
   **Active sessions**, where **Disconnect** ends it within a few seconds.
5. Copy a file in the session; it appears under Audit log → File transfers.
6. Enter a wrong password a few times; an alarm appears under Audit log → Alarms.

### Logging in from the RustDesk app
1. Under **Users**, create a user (or use `admin`).
2. In RustDesk, click **Login** (top of the main window, or Settings → Account)
   and sign in with that username and password.
3. The **Accessible devices** tab lists what the user may see:
   - admins: every device;
   - other users: devices they own (set **Owner** on a device page), devices
     of the other members of their user group, and devices in the device
     groups their user group may access (set on **Users → User groups**).

### Address books
- Every user gets a **personal** address book in RustDesk after logging in.
- Admins create **shared** books under **Address books** and share them with
  users or user groups as read only, read/write or full control. They show up
  in RustDesk as extra address books.
- A shared book entry can hold the device password, so members connect
  without typing it. The web console never shows these passwords.
- Install a device with `preset-address-book-name` (plus optional `-tag`,
  `-alias`, `-password`, `-note`) to add it to a shared book automatically.
- Address book passwords are encrypted with a key derived from
  `DATA_DIR/session.secret` (or `SESSION_SECRET`). Back that file up with the
  database: without it the saved passwords can't be read.

Disabling a user or changing their password signs them out of RustDesk.

### Strategies
A strategy is a set of RustDesk settings (clipboard, file transfer, how sessions
are accepted, IP whitelist, ...) that the server pushes to devices with their
heartbeat, so changes arrive within seconds.
- A device uses its own strategy, otherwise its device group's, otherwise the
  **default** strategy. Install with `preset-strategy-name` to assign one.
- Only ticked settings are managed; the rest stay as set on the device.
  Unticking a setting resets it to the RustDesk default on the device.
- The device page shows whether the device has **applied** its settings.
- Server addresses, the key, 2FA and passwords can't be pushed: the RustDesk
  client refuses them for security reasons.

### Two-factor authentication
Every user can turn on 2FA under **My account** in the web console (users who
aren't admins only see that page). After that, signing in to RustDesk and to
the console asks for a 6-digit code from an authenticator app. If someone loses
their phone, an admin can turn it off under Users → Edit.

### Single sign-on (OpenID Connect)
Under **Settings → Single sign-on**, add a provider (Google, Microsoft Entra,
Keycloak, Authentik, Okta, ...). Register the redirect URL shown there at the
provider; it is built from `PUBLIC_URL`, so set that to the address users reach
this server on. The RustDesk login dialog and the console login page then show a
"Continue with ..." button. People are matched to existing users by verified
email, or created on first login if you allow it. 2FA is left to the provider.

### Install scripts (deploy tokens)
Create a token under **Settings → Deploy tokens**, then on each machine (as
administrator, with RustDesk installed):
```
rustdesk.exe --assign --token <token> --device_group_name "Office" --user_name alice
```
Also: `--strategy_name`, `--address_book_name` (+ `--address_book_tag`,
`--address_book_alias`, `--address_book_password`), `--note`, `--device_name`,
`--device_username`. Turn on **Only accept registered devices** to refuse
machines that weren't assigned this way. (`rustdesk --deploy` needs RustDesk
Server Pro's ID server and answers "not enabled" here.)

### Recordings
The upload endpoint and the Recordings page exist, but the standard RustDesk
app never uploads recordings (it keeps them on the machine); only custom
builds do. Uploads are off by default (Settings).

Each RustDesk ID is bound to the first machine (uuid) that reports it. If you
reinstall or replace the machine, use **Reset machine binding** on its page.

## Configuration (environment variables)
| Variable | Default | Meaning |
|---|---|---|
| `LISTEN` | `:21114` | Listen address |
| `DB_DRIVER` | `sqlite` | `sqlite` or `mysql` (MariaDB/MySQL) |
| `DB_DSN` | `$DATA_DIR/rustdesk-manager.db` | SQLite path or MySQL DSN |
| `DATA_DIR` | `./data` | Database, recordings, session secret |
| `PUBLIC_URL` | `http://localhost:21114` | External URL (SSO redirects) |
| `ADMIN_USER` / `ADMIN_INITIAL_PASSWORD` | `admin` / random (logged) | First admin, created once |
| `SESSION_SECRET` | generated in `DATA_DIR` | Signs admin sessions |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `TRUST_PROXY` | `false` | `true` only behind your own reverse proxy: takes client IPs from `X-Forwarded-For` |

## Development
```sh
cd web && npm install && npm run build   # build UI (needed before go build)
go run ./cmd/server                      # API + UI on :21114
cd web && npm run dev                    # optional: hot-reload UI, proxies to :21114
```
`reference/` holds clones of the RustDesk client and server source used to match
the API contract (not part of the build).
