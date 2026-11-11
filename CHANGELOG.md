# Changelog

All notable changes to RustDesk Manager are listed here. Versions follow
[semantic versioning](https://semver.org): while the version starts with 0, any
release may change behaviour; from 1.0.0 on, breaking changes raise the first number.

Note changes under "Unreleased" as you work; `scripts/release.ps1` turns them into the
next version's section (see "Releasing" in the README).

## [Unreleased]

### Added
- `scripts/release.ps1`: one command to release a version. It updates the version
  and changelog, runs the tests, merges `develop` into `main`, tags the release,
  publishes the Docker image to Docker Hub and creates the GitHub release.

## [0.1.0] - 2026-11-11

First release. Everything below is covered by the automated tests (on SQLite and
MariaDB), but has not yet been tried against a real RustDesk client.

### Added
- **RustDesk client API**, for use next to the open-source `hbbs`/`hbbr`: the HTTP
  API the standard RustDesk app calls, normally only offered by RustDesk Server Pro.
- **Devices**: registration with hardware and OS details, live online status,
  active sessions with remote disconnect, owner, device groups, custom names,
  install presets (`preset-device-group-name`, `preset-user-name`,
  `preset-strategy-name`, `preset-note`), and each RustDesk ID bound to the first
  machine that reports it (with a reset).
- **Audit log**: sessions, file transfers and security alarms, deduplicated when the
  client retries; filters and CSV export.
- **Users and user groups**: login from the RustDesk app, the "Accessible devices"
  tab with visibility rules, login sessions you can revoke.
- **Address books**: a personal book per user and shared books with read-only,
  read/write or full-control access; device passwords encrypted at rest; devices
  can join a shared book at install time.
- **Strategies**: RustDesk settings pushed to devices or device groups through the
  heartbeat, with settings reset when they stop being managed.
- **Two-factor authentication** with authenticator apps, for the RustDesk app and the
  web console.
- **Single sign-on** with any OpenID Connect provider, for the RustDesk app and the
  web console.
- **Deploy tokens** for `rustdesk --assign`, and an option to only accept
  pre-registered machines.
- **Recording uploads** (only sent by custom-built clients; off by default).
- **Housekeeping**: hourly removal of audit entries and recordings past their
  retention period, and of expired logins.
- **Web console**: dashboard, light and dark mode, phone layout, Fleet logo.
- `reset-password` and `version` commands.
- SQLite or MariaDB/MySQL; a Docker image running as a non-root user.

[Unreleased]: https://github.com/burnacid/rustdesk-manager/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/burnacid/rustdesk-manager/releases/tag/v0.1.0
