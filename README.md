# RustDesk Manager

A self-hosted management server for [RustDesk](https://github.com/rustdesk/rustdesk),
for use next to the open-source ID and relay servers (`hbbs`/`hbbr`). It implements
the HTTP API that the standard RustDesk app already calls (normally only offered by
RustDesk Server Pro) and adds a web console to manage it all. It's a single Go binary
with the web console built in, and runs on SQLite or MariaDB/MySQL.

## Features

- **Devices**: every machine that uses your server shows up with its hardware, OS and
  signed-in user, live online status, and active sessions you can disconnect.
- **Audit log**: sessions (who connected, from where, how they logged in, how long,
  and the note left at the end), file transfers, security alarms, and every change
  made in the console. Filter and export as CSV.
- **Users and groups**: accounts for the RustDesk app's Login button, with an
  "Accessible devices" tab showing what each person may see.
- **Address books**: a personal book per user and shared books with read-only,
  read/write or full-control access. Shared books can hold device passwords
  (encrypted), so members connect without typing them.
- **Strategies**: push RustDesk settings (clipboard, file transfer, how sessions are
  accepted, IP whitelist, …) to devices or device groups within seconds.
- **Security**: 2FA with authenticator apps, passkeys for the console, single sign-on with any OpenID Connect
  provider (Google, Microsoft Entra, Keycloak, Authentik, …), login rate limiting, and
  each RustDesk ID bound to the machine that first reported it.
- **Rollout**: deploy tokens for install scripts (`rustdesk --assign`), and an option
  to only accept pre-registered machines.
- **Automation**: API tokens for scripts, bulk device actions, and Prometheus metrics.
- **Housekeeping**: automatic removal of old audit entries and recordings.
- **Web console**: light and dark mode, works on phones.

## Quick start (Docker)

```sh
cp docker-compose.example.yml docker-compose.yml
# edit PUBLIC_URL and ADMIN_INITIAL_PASSWORD in docker-compose.yml
docker compose up -d --build
```

Open `http://your-server:21114` and sign in as `admin` with the password you set.
Without `ADMIN_INITIAL_PASSWORD`, a random password is printed once in the log
(`docker compose logs manager`).

The server runs as a non-root user (uid 65532) and keeps its data in the
`manager-data` volume. To use a host folder instead, `chown 65532:65532` it first.

The example runs `hbbs`, `hbbr` and RustDesk Manager together. If your ID and relay
servers already run elsewhere, keep only the `manager` service.

### Connect RustDesk clients

In RustDesk: **Settings → Network → ID/Relay server**. Keep the ID server, relay
server and key you already use, and set **API server** to `http://your-server:21114`
(or your HTTPS address, see below). If you leave it empty, the client uses
`http://<ID server host>:21114` automatically.

On an installed client you can also set it from an administrator terminal:
`rustdesk.exe --option api-server https://rustdesk.example.com`

The machine appears under **Devices** within a few seconds. The client only talks to
this server when it uses your own ID server, not RustDesk's public one.

## HTTPS with a reverse proxy

Put a reverse proxy in front of port 21114 so logins and passwords travel encrypted.
With [Caddy](https://caddyserver.com), which gets certificates automatically:

```
rustdesk.example.com {
    reverse_proxy manager:21114
}
```

Then set in `docker-compose.yml`:

```yaml
PUBLIC_URL: https://rustdesk.example.com
TRUST_PROXY: "true"      # take client IPs from the proxy's X-Forwarded-For
```

and use `https://rustdesk.example.com` (without a port) as the API server in RustDesk.
The client drops `:21114` from `https://` addresses, so don't serve HTTPS on that port.

Only set `TRUST_PROXY` when the proxy is the only way to reach the server; otherwise
anyone could fake their IP address in the logs and get around the login limit.

## Using it

### Users and the RustDesk app
Create users under **Users**. In RustDesk, the **Login** button (top of the main
window) signs in with those accounts. The **Accessible devices** tab then shows:
- for admins: every device;
- for other users: devices they own (set **Owner** on a device), devices of other
  members of their user group, and devices in the device groups that user group may
  access (set under **Users → User groups**).

Users who aren't admins can also sign in to the web console, where they only see
**My account** (password, 2FA, theme).

### Address books
Every user gets a personal address book in RustDesk after logging in. Admins create
shared books under **Address books** and choose who may use them. Install a device
with `preset-address-book-name` (plus optional `-tag`, `-alias`, `-password`,
`-note`) to add it to a shared book automatically.

### Strategies
A device uses its own strategy, otherwise its device group's, otherwise the
**Default** one. Only ticked settings are managed; unticking one resets it to the
RustDesk default on the device. The device page shows whether the device has applied
its settings. Server addresses, the key, 2FA and passwords can't be pushed: the
RustDesk client refuses them.

### Two-factor authentication
Each user turns it on under **My account**. Signing in to RustDesk and to the console
then asks for a 6-digit code. If someone loses their phone, an admin turns it off
under **Users → Edit**.

### Passkeys
Under **My account → Passkeys**, add a passkey (fingerprint, face, device PIN or a
security key). The console login page then offers **Sign in with a passkey**, with no
password or 2FA code. Passkeys are for the web console only; the RustDesk app has no
passkey login. Browsers only allow them when the console is opened at the address in
`PUBLIC_URL`, over HTTPS (plain `http://localhost` also works for testing).

### Single sign-on
Under **Settings → Single sign-on**, add a provider and register the redirect URL
shown there at the provider (it is built from `PUBLIC_URL`). A "Continue with …"
button then appears in the RustDesk login dialog and on the console login page.
People are matched to existing users by verified email, or created on first sign-in
if you allow it. Microsoft Entra needs **Trust email addresses** turned on, because it
doesn't mark emails as verified; only do that for providers whose users can't choose
any email address.

### Install scripts
Create a token under **Settings → Deploy tokens**, then on each machine (administrator
terminal, RustDesk installed):

```
rustdesk.exe --assign --token <token> --device_group_name "Office" --user_name alice
```

Also available: `--strategy_name`, `--address_book_name` (with `--address_book_tag`,
`--address_book_alias`, `--address_book_password`, `--address_book_note`), `--note`,
`--device_name`, `--device_username`. With **Only accept registered devices** on,
machines must be assigned this way before they appear.

### Replacing a computer
Each RustDesk ID is bound to the first machine that reported it, so another machine
can't take it over. After reinstalling or replacing a computer, use **Reset machine
binding** on its device page.

## Backups and upgrades

Back up `DATA_DIR` (the `manager-data` volume in the Docker example; copy it out with
`docker compose cp manager:/data ./backup`), at least:
- `rustdesk-manager.db` (when using SQLite; with MariaDB/MySQL, back up that database);
- `session.secret`: the key that encrypts saved address book passwords, 2FA secrets
  and SSO client secrets. **Without it those can't be read**;
- `recordings/`, if you use recording uploads.

For a consistent SQLite copy, stop the container first or use
`sqlite3 rustdesk-manager.db ".backup backup.db"`.

To upgrade, pull the new version and run `docker compose up -d --build`. The database
is updated automatically when the server starts.

Forgot the admin password? This works while the server runs:

```sh
docker compose exec manager /rustdesk-manager reset-password admin      # prints a new password
docker compose exec manager /rustdesk-manager reset-password admin 'my new password'
```

## Versions and the Docker image

The current version is in `VERSION`; what changed is in [CHANGELOG.md](CHANGELOG.md).
The running version shows at the bottom of the web console menu, in the start-up
log, and with `docker compose exec manager /rustdesk-manager version`.

Build the image on any machine with Docker:

```sh
docker build --build-arg VERSION=$(cat VERSION) -t rustdesk-manager:$(cat VERSION) -t rustdesk-manager:latest .
```

(PowerShell: `$v = Get-Content VERSION`, then use `$v` instead of `$(cat VERSION)`.)

To move it to a server without a registry, save it to a file, copy that over and
load it there:

```sh
docker save rustdesk-manager:0.1.0 rustdesk-manager:latest | gzip > rustdesk-manager-0.1.0.tar.gz
# on the server:
docker load -i rustdesk-manager-0.1.0.tar.gz
docker compose up -d           # uses image: rustdesk-manager:latest
```

The image is built for the processor of the machine that builds it (usually
`linux/amd64`). For an ARM server such as a Raspberry Pi, add `--platform linux/arm64`.

## Releasing

Day-to-day work happens on the `develop` branch; `main` only receives releases. Note
each change under `## [Unreleased]` in [CHANGELOG.md](CHANGELOG.md) as you go. To
release, run from `develop` (PowerShell 7):

```powershell
./scripts/release.ps1 -Bump patch     # 0.1.0 -> 0.1.1; or -Bump minor / major, or -Version 1.0.0
./scripts/release.ps1 -Bump minor -DryRun   # show every step, change nothing
```

It checks that your tree is clean and up to date, runs the tests, turns the Unreleased
notes into the new version's section (or offers the commit messages if you didn't
write any), updates `VERSION`, then asks before it:
1. commits on `develop`, merges `develop` into `main` and tags `vX.Y.Z`;
2. builds the image and pushes `burnacid/rustdesk-manager:X.Y.Z` and `:latest` to
   Docker Hub;
3. pushes `main`, `develop` and the tag;
4. creates the GitHub release with the notes and the image file attached.

One-time setup: `docker login` (Docker Hub) and `gh auth login` (GitHub CLI). Use
`-Image name/rustdesk-manager` (or `$env:RDM_IMAGE`) for another Docker Hub name, and
`-NoGitHubRelease` to skip the GitHub release. If a step fails after the commit, the
script prints the commands that finish the release by hand.

### Scripts and monitoring
- **API tokens** (**Settings → API tokens**): call the admin API from scripts with
  `Authorization: Bearer <token>`, e.g.
  `curl -H "Authorization: Bearer $TOKEN" https://rustdesk.example.com/admin/api/devices`.
  The endpoints are the ones the console uses (`/admin/api/...`). A token acts as the
  admin who created it and stops working when that admin is disabled, loses admin
  rights or changes their password.
- **Admin actions** (**Audit log → Admin actions**): who changed what, from which IP,
  and whether it was the console or an API token. Request contents are not stored.
- **Bulk actions**: tick devices on the **Devices** page to set their group, owner or
  strategy, disconnect them or delete them at once.
- **Prometheus**: set `METRICS_TOKEN`, then scrape `/metrics` with that token as a
  bearer token (`authorization: {credentials: ...}` in the Prometheus job). It reports
  devices (total and online), users, signed-in sessions, open remote sessions, audit
  record counts and login attempts.

## Configuration

| Variable | Default | Meaning |
|---|---|---|
| `LISTEN` | `:21114` | Listen address |
| `PUBLIC_URL` | `http://localhost:21114` | Address users and devices reach this server on; used for SSO redirects and secure cookies |
| `DB_DRIVER` | `sqlite` | `sqlite` or `mysql` (also for MariaDB) |
| `DB_DSN` | `$DATA_DIR/rustdesk-manager.db` | SQLite file, or a MySQL DSN such as `user:pass@tcp(db:3306)/rustdesk?parseTime=true&charset=utf8mb4` |
| `DATA_DIR` | `./data` | Database, recordings and `session.secret` |
| `ADMIN_USER`, `ADMIN_INITIAL_PASSWORD` | `admin`, random | The first admin; only used when there are no users yet |
| `SESSION_SECRET` | generated into `DATA_DIR/session.secret` | Encryption key for stored secrets; keep it with your backups |
| `TRUST_PROXY` | `false` | `true` behind your own reverse proxy |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error` |
| `METRICS_TOKEN` | empty (off) | Turns on `/metrics`; Prometheus sends it as a bearer token |

Settings that can change at runtime (registered devices only, recording uploads,
how long to keep old data, SSO providers, deploy tokens) are under **Settings** in
the web console.

## Limitations

- **Recordings**: the standard RustDesk app never uploads session recordings; it keeps
  them on the machine. Only custom-built clients upload, so uploads are off by default.
- **`rustdesk --deploy`** and login-required access control depend on RustDesk Server
  Pro's ID server, not just its API, so they aren't available with the open-source
  `hbbs`.
- **Switch sides**: the desktop app registers a "switch grant" with the server before
  swapping sides. Nothing in the open-source `hbbs` uses it, so this server only
  answers in the expected format and stores nothing.
- **The RustDesk name**: RustDesk is a trademark of its owners. This project is not
  affiliated with them; rename it if you publish it.

## Testing with a real RustDesk client

[docs/real-client-test.md](docs/real-client-test.md) is a step-by-step checklist for
trying every feature with the standard RustDesk app.

## Development

```sh
cd web && npm install && npm run build   # the Go binary embeds web/dist, so build it first
go run ./cmd/server                      # API and web console on :21114
cd web && npm run dev                    # optional: live-reloading UI on :5173, proxies to :21114
go test ./...                            # API tests (about 30 s; the 2FA test waits for a fresh code)
```

To run the same tests against MariaDB/MySQL (each test gets its own database):

```sh
docker run -d --name rdm-mariadb -p 127.0.0.1:3307:3306 -e MARIADB_ROOT_PASSWORD=testroot mariadb:11
RDM_TEST_MYSQL='root:testroot@tcp(127.0.0.1:3307)/' go test ./...
```

The client API mirrors the RustDesk client source. To check request and response
shapes, clone it into `reference/` (ignored by git):

```sh
git clone --depth 1 https://github.com/rustdesk/rustdesk reference/rustdesk
git -C reference/rustdesk submodule update --init --depth 1 libs/hbb_common
```

Code layout: `internal/clientapi` (what the RustDesk app calls), `internal/adminapi`
(what the web console calls), `internal/db` (models), `internal/abook`,
`internal/strategy`, `internal/sso` (shared logic), `web/` (React web console).
