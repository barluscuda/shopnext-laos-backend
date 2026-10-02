# Changelog

## Unreleased

Updated: `2026-10-03 02:18:33` (Asia/Vientiane, UTC+07:00).

Base commit: `46e194fe687e86a785cf568475673e9fb3b0ff87`.
Previously committed admin bearer JWT changes: `46e194fe687e86a785cf568475673e9fb3b0ff87`.

### Added

- GitHub Actions Go testing workflow for pushes, pull requests, and manual
  runs, using the existing Docker-based `make check` and `make integration`
  commands, enforcing Go formatting and cleaning up isolated test services.
- CI usage documentation in the README and operations guide.

### Verification

- `git diff --check` passed. The workflow has not run on GitHub yet; tests
  were not run locally for this workflow-only change.

Updated: `2026-10-03 02:03:23` (Asia/Vientiane, UTC+07:00).

Base commit: `aadca9ce822809630f8fa9a8d389d9d5586bc571`.

### Changed

- Replaced staff cookies with eight-hour admin bearer JWTs backed by revocable
  PostgreSQL sessions, including legacy-owner sessions; added the required
  signing secret configuration and migration `00003_admin_jwt.sql`.
- Updated the API contract, generated reference, Postman collection, feature
  parity, operations guide and README for bearer authentication.

### Verification

- `git diff --check` passed. Tests were not run for this commit.

Updated: `2026-10-02 18:28:19` (Asia/Vientiane, UTC+07:00).

Base commit: `41206353dff2660a3bd1e766ada521b5b915641e`.
Related committed API documentation: `c97c185`.

### Changed

- Replaced customer cookie authentication with three-day, five-use OTP header
  keys and order-scoped trusted-device JWTs checked against the User-Agent.
- Online checkout now returns payment QR data, and staff manually assigns
  pickup codes after confirming an order.
- Public bill lookup now returns basic status and pickup readiness; protected
  details require a phone key or trusted-device key.
- Successful PhaJay payments now queue signed Next.js webhook notifications
  and payment SMS in the payment transaction, with durable delivery retries.
- Added versioned migration `00002_client_order_access.sql` and updated
  OpenAPI, API guide, feature parity, Postman collection and runtime config.

### Verification

- `make check`, `make integration`, `make build`, OpenAPI generation check and
  `git diff --check` passed.

Updated: `2026-10-02 16:56:49` (Asia/Vientiane, UTC+07:00).

Base commit: `fd842a9d4c358cbe7a2dbc2108e4ea6a04e16d18`.

### Added

- Human-readable API guide covering OTP and cookie identity, checkout, staff
  permissions, uploads, callbacks, error handling, and Postman setup.
- Generated endpoint reference and Postman v2.1 collection covering all 84
  OpenAPI operations, with request examples, variables, and CSRF headers.
- Standard-library Python generator with a `--check` mode to detect drift
  between OpenAPI and the generated documentation.

### Changed

- README and feature parity documentation now link the API documentation and
  describe how to regenerate and verify it.

### Verification

- Postman collection validated against the official v2.1 JSON schema.
- Checked all 84 operations, variables, CSRF headers, request fields, media
  paths, and documentation links; regeneration and `git diff --check` passed.

## Commit documentation rules — `fd842a9`

Recorded: `2026-10-02 11:02:13` (Asia/Vientiane, UTC+07:00).

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
