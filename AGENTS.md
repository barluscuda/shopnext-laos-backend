# ShopNext Laos backend

ShopNext Laos is developed by barluscuda. The first release must reproduce all
existing NeoShop backend features. Preserve account-free shopping and SMS OTP
identity. Public and administrative APIs live under `/api/v1`.

## Working rules

- Whenever the user asks to commit, update `docs/CHANGELOG.md` before committing
  and include it in the commit alongside the requested changes. Record a clear
  summary, an Asia/Vientiane timestamp (UTC+07:00), and the base commit ID.
  Include known commit IDs for previously committed changes; never invent the
  ID of the commit being created. Update other affected documentation in `docs`
  as needed. Commit all requested pending changes unless the user limits scope.
- An explicit `[Plan mode]` instruction permits investigation and planning only.
  Resume implementation only after `[Build mode]`.
- NeoShop at `/home/barluscuda/coding/neoshop/neoshop` is a read-only reference.
- Never read, modify, remove, or commit `.env`. Document configuration in
  `.env.example`. Never log passwords, OTPs, tokens, provider credentials, or
  request bodies containing personal information.
- Domain types and application use cases must not import Gin, GORM, Redis,
  provider clients, or filesystem adapters. Wire dependencies explicitly.
- GORM is for persistence only. Never call `AutoMigrate`. Schema changes require
  versioned SQL migrations; API and worker startup must not migrate implicitly.
- PostgreSQL owns durable business state. Money is whole LAK using int64/BIGINT.
  Historical item snapshots must not change when catalog records change.
- Preserve role permissions, order/payment/refund state machines, verified-phone
  ownership, public pickup-code visibility, and owner-only QR access.
- Critical state updates, events, audit records, and notification enqueueing
  belong in the same transaction. Provider calls happen outside transactions.
- Development payment/SMS adapters and simulation endpoints must be disabled in
  production. Production PhaJay callbacks require authentication.

## Verification

`make check` formats, vets, and runs the test suite. `make integration` starts
isolated test PostgreSQL/Redis, applies SQL migrations, and runs integration
tests. `docker compose build` verifies the Alpine runtime. Integration tests may
only reset the explicitly named `shopnext_test` database, never development or
production data. Update `docs/feature-parity.md` and OpenAPI for API changes.
