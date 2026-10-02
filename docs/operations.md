# Operations and rollout

## Continuous integration

The GitHub Actions workflow `.github/workflows/go-tests.yml` runs on pushes,
pull requests, and manual dispatch. It uses the Docker toolchain from the
repository to run `make check`, rejects Go formatting changes, then runs
`make integration` with the isolated `shopnext_test` PostgreSQL database and
Redis database 15. Test services are stopped even when a step fails. Compose
uses `.env.example`; CI requires no production credentials.

## Required production configuration

Set `APP_ENV=production`, explicit HTTPS `ALLOWED_ORIGINS`, production
`DATABASE_URL`/`REDIS_URL`, `PAYMENT_PROVIDER=phajay`, `SMS_PROVIDER=wenova`,
merchant `PHAJAY_SECRET_KEY`, `PHAJAY_WEBHOOK_SECRET`, `WENOVA_TOKEN`, and a
32-character-or-longer `CRON_SECRET` and independent `ADMIN_JWT_SECRET`. Set provider base URLs and sender ID to
merchant-approved values. Terminate TLS at a reverse proxy. Set `TRUSTED_PROXIES`
only to actual proxy addresses/CIDRs; otherwise client-supplied forwarding
headers are ignored. Never expose PostgreSQL or Redis publicly.

Before real payment traffic, agree the callback authentication contract with
PhaJay. This implementation expects lowercase/uppercase hex HMAC-SHA256 over
the exact raw request body in `PHAJAY_WEBHOOK_SIGNATURE_HEADER` (default
`x-phajay-signature`). It rejects missing/incorrect signatures. This is a
fail-closed deployment contract, not a claim that PhaJay currently issues that
header. If the merchant deployment uses another verified scheme, adapt and
test the authentication adapter before enabling production callbacks.

Perform live merchant acceptance for all enabled bank QR endpoints, deeplinks,
callback retries, wrong/late payments, full refund submission/polling and Wenova
SMS delivery. Local tests verify the contracts recovered from NeoShop, not
availability or authorization of external merchant services.

## Migrations and release

For local development, `make dev` starts the complete Docker stack, including a
separate migration job before API and worker. `make staff EMAIL=... NAME=...`
starts the one-off first-owner command with a hidden password prompt; named
staff administration after bootstrap remains in the API. `make logs` follows
API and worker logs. Make targets use `.env.example` by default; override
`COMPOSE_ENV_FILE` or export environment variables for local settings. Use Docker
service names for local database/Redis URLs. Shared Compose settings apply to
migrations, API, staff and worker, including production provider credentials
and safety validation.

Back up PostgreSQL before schema changes. Run `migrate up` once per release,
then deploy API and worker. API/worker never run AutoMigrate. `migrate status`
is read-only. Down migrations require `ALLOW_MIGRATION_DOWN=1`; the initial
down migration destroys all application tables and is not a production rollback
strategy. Prefer forward migrations for deployed systems.

Keep the uploads volume persistent and backed up. Runtime containers are
non-root; mounted storage must be writable by UID 10001. Local media storage
assumes API replicas share the same mounted files. Detached uploads can remain
after failed catalog/content writes; remove them only after checking all image,
hero and promo references. Media deletion and database transactions cannot be
made atomically consistent across a local filesystem.

## Scheduled work and alerts

Run the worker continuously. Every five seconds it drains client callbacks and
SMS notifications. Every minute it releases expired holds, expires
payment attempts, replays early callbacks, polls refunds and drains 20 SMS
messages; idempotency cleanup runs hourly. Jobs recheck state under row locks
and allow multiple worker processes; an individual Worker object is used by
one loop, not concurrently. Sustained load may require a shorter cadence or
larger audited batch limits. SMS retries stop after eight claims; leases last
five minutes; backoff starts at 30 seconds and caps at one hour. A five-batch
failure streak opens the local breaker for five minutes.

The default Compose stack starts the worker after migrations and Redis readiness,
restarts API/worker on failure and provides a 60-second worker shutdown grace
period. The worker logs startup/shutdown and failures; a quiet running process
between scheduled ticks is normal. Each tick and hourly cleanup has a 50-second
deadline. `make worker` starts/rebuilds it independently when needed.

`POST /api/v1/maintenance/release-expired` is an optional expiry-only cron hook,
protected by `Authorization: Bearer <CRON_SECRET>`. It is not a replacement for
the worker. Health live checks the process; health ready checks PostgreSQL and
Redis. Alerts should cover readiness failures, worker errors, DEAD outbox rows,
stale pending callbacks, `RECONCILIATION_REQUIRED` events and MANUAL_REVIEW
refunds. Unmatched callbacks remain available for investigation.

A refund with an unknown provider outcome must be checked in the merchant
portal, never blindly resubmitted. Missing provider IDs become MANUAL_REVIEW;
nonterminal results/outages beyond 24 hours do as well. Resolve only after
independent verification through the permission-protected refund resolve API.

## Identity, privacy and legacy behavior

OTP: six digits, SHA-256 stored, five-minute TTL, 60-second resend cooldown and
five failed verification attempts. Only the newest OTP can be used. Phone
verification header keys are opaque and hashed in PostgreSQL, with a three-day
TTL and five-use limit. Customer access does not use backend cookies. Order-only
trusted-device JWTs expire after 30 days and check stored token hashes and the
User-Agent on every use; configure TRUST_DEVICE_SECRET in production. Admin bearer JWTs have an eight-hour TTL;
password reset and disabling revoke sessions. Passwords use NeoShop-compatible
scrypt. Login, mutation and audit history never stores plaintext credentials.

Optional `ADMIN_PASSWORD_HASH` (raw/base64 NeoShop scrypt format) plus
`ADMIN_SESSION_SECRET` enables legacy shared-owner login, which now issues a
revocable JWT. Its signing key binds ADMIN_JWT_SECRET and ADMIN_SESSION_SECRET;
rotating either invalidates legacy tokens. Prefer named staff for attribution
and dual-control separation. Migration 00003 revokes old cookie sessions and
allows legacy JWT sessions in PostgreSQL. Deploy it before the API, then have
staff log in again. Admin clients must read data.access_token and send it in
Authorization: Bearer headers. Rotating ADMIN_JWT_SECRET invalidates all admin
JWTs; tokens cannot authorize customer operations.

Public bill-number lookup returns basic status, time, total and pickup readiness,
including the pickup code when staff has assigned it. Full recipient, delivery,
item and QR details require an OTP header key or order-specific trust key.
Phone history requires the matching OTP header key. Configure the signed Next.js
payment webhook using CLIENT_PAYMENT_WEBHOOK_URL and CLIENT_PAYMENT_WEBHOOK_SECRET
(both required in production). The worker retries delivery from the durable
client_notifications table; monitor FAILED/DEAD records. SMS retries are independent.
Retention/archival of orders, audit logs, provider raw callbacks, OTP/token rows,
SMS history and files must be defined operationally before real customer data;
only idempotency records currently have automated deletion.

Pickup codes start empty and are entered by authorized staff. Assigned codes
are globally unique and may contain 1–80 characters. Existing assigned codes are
preserved by the migration. The unique index excludes empty codes.
