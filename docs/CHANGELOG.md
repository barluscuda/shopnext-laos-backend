# Changelog

## Unreleased

Updated: `2026-10-02 10:57:34` (Asia/Vientiane, UTC+07:00).

Commit range: changes since `8361be8` (`Initial ShopNext Laos backend`).
These changes are currently uncommitted; their ending commit ID and release tag
will be recorded when they are committed and released.

### Changed

- The default Docker Compose stack now starts PostgreSQL, Redis, the migration
  service, the API, and the worker. It includes API readiness checks and worker
  restart and shutdown settings.
- Build, formatting, vet, unit test, and integration test commands now run in
  Docker. Integration tests use PostgreSQL and Redis on an isolated Compose
  network.
- Docker Compose now uses `.env.example` as its default configuration. The
  documentation covers the Docker development workflow.

### Fixed

- First-owner setup now has a dedicated one-off Docker staff command. Terminal
  password input is hidden, passwords can also be supplied through stdin, and
  password validation happens before service connections are opened.
- Worker ticks and hourly cleanup now run with bounded contexts. Worker startup
  and shutdown are logged.
