# Changelog

## Unreleased

Updated: `2026-10-02 11:02:13` (Asia/Vientiane, UTC+07:00).

Base commit: `cc8c085`.

### Changed

- Agent rules now require a changelog update with a summary, local timestamp,
  and base commit reference whenever the user asks to commit. The changelog must
  be included with the requested changes, and affected documentation updated.

## Docker workflow and worker lifecycle — `cc8c085`

Recorded: `2026-10-02 10:57:34` (Asia/Vientiane, UTC+07:00).

Commit range: `8361be8..cc8c085`.
Changelog introduced in `ff3e803`; implementation committed in `cc8c085`.

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
