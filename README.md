# ShopNext Laos backend

Next-generation shopping for Laos, developed by barluscuda. This is a new
`/api/v1` backend for the complete existing NeoShop shopping and staff feature
set: no customer accounts, SMS OTP, catalog/content, delivery, guest orders,
online payment/COD, refunds, staff roles, reporting, media and notifications.

## Run with Docker

Requires Docker and Docker Compose; Make provides convenient commands. Go and
the WebP C compiler run inside the build containers. Configuration comes from
exported environment variables. Make explicitly disables Compose's automatic
`.env` loading; the binaries also do **not** load `.env`. Make uses the checked-in
`.env.example` and falls back to `/dev/null` if it is missing.

```sh
make dev
make staff EMAIL=owner@example.com NAME="Owner"
make logs
```

`make dev` builds and starts PostgreSQL, Redis, the migration job, API and worker.
The migration job must succeed before API/worker start. The worker runs in the
background and restarts automatically if it exits. API readiness is checked
before startup completes. No host Go installation is needed.

The staff command prompts for a password without echoing it; use at least 10
characters. It only creates the first owner. Subsequent staff are managed by
an owner through the API. The development database starts empty: create a
category, product and delivery provider through the administrative endpoints.
Laos provinces/districts are available from `/api/v1/geography`.

The equivalent commands without Make are:

```sh
docker compose --env-file .env.example up --build -d --wait
docker compose --env-file .env.example run --rm staff --email owner@example.com --name Owner
docker compose --env-file .env.example logs --follow api worker
```

For a password supplied by a secret manager over stdin, add `-T` to the staff
`run` command. Do not put passwords in command arguments or shell history.
PostgreSQL and Redis bind only loopback ports 55432 and 56379. The API binds
port 8080. Containers connect to `postgres:5432` and `redis:6379`; exported
`DATABASE_URL`/`REDIS_URL` must use addresses reachable inside Docker.

Use `make worker` or `make api` to rebuild/start a service, `make migrate` for an
explicit migration run, and `make stop` to stop the stack while preserving data.

## API

Start with the [human-readable API guide](docs/api.md) and
[full endpoint reference](docs/api-reference.md). Import the
[Postman collection](docs/shopnext-laos.postman_collection.json) to explore all
86 operations. [OpenAPI](docs/openapi.yaml) is also served at
`/api/v1/openapi.yaml`. Regenerate the reference and collection with
`python3 tools/generate_api_docs.py`; use `--check` to detect documentation drift.
JSON uses snake_case and `{ "data": ... }`; errors contain
`error.code`, `error.message` and `request_id`. Lists use `pagination`.
Every mutation requires `X-ShopNext-CSRF: 1`, including login, OTP,
uploads and logout. Customers use header keys; staff use revocable bearer JWTs.
Origins must exactly match `ALLOWED_ORIGINS` (space-separated).

Shopping sequence:

1. Browse `/products`, `/categories`, `/content` and `/delivery-options`.
2. POST `/otp/request` with `phone`, then `/otp/verify` with `phone` and `code`.
   Development OTP request responses include `dev_code`; production never does.
   Verification returns `verification_key` (three days, five protected uses).
   Forward it in `X-Phone-Verification-Key`; no customer cookie is used.
3. POST `/orders` with the verified phone, recipient name, provider, province,
   city, branch name, `payment_type` (`ONLINE` or `COD_PROVIDER`) and variant
   quantities. Online orders also require a supported `payment_method`.
   Use `Idempotency-Key` for safe retries; it is scoped to the verified phone.
4. Online checkout returns QR/deeplink data in `payment`. Read the public basic
   status at `/bills/{bill}`; full details use `/bills/{bill}/details` with a matching
   phone key or order-scoped trust key and device User-Agent. OTP detail access
   issues a trusted-device JWT. Retry QR at `/bills/{bill}/payment-attempts`.
5. The authenticated provider callback confirms online payment. With development
   payment mode only, POST `/bills/{bill}/simulate-payment` instead.
   Successful approval queues a signed Next.js webhook and success SMS.
6. Staff enters the pickup code via PUT `/admin/orders/{bill}/pickup-code`.

Bill-number lookup is public for status, total, time and assigned pickup code.
Recipient, delivery, item and QR details require verified ownership.
Phone-based lookup requires matching OTP verification. There are no customer
registration/login/profile endpoints. Staff log in through `/admin/auth/login`.

## Verify

```sh
make check
make integration
make build
```

Integration tests only reset the explicitly named `shopnext_test` database and
Redis database 15 on the isolated test service. They never reset the development
database. Formatting, vetting, tests and integration tests all run in Docker.
Test infrastructure uses a separate Compose project in `compose.test.yaml`;
stop it with `docker compose --env-file .env.example -f compose.test.yaml down`
when finished.

See [architecture](docs/architecture.md), [feature parity](docs/feature-parity.md)
and [operations](docs/operations.md). Real provider credentials, signature
agreement and live merchant acceptance are deployment prerequisites; local
contract tests do not substitute for a live payment/refund/SMS certification.

Customer API access uses three-day/five-use OTP header keys and order-specific
trusted-device JWTs. Online checkout returns payment QR data; staff assigns the
pickup code later. Set `TRUST_DEVICE_SECRET`, `CLIENT_PAYMENT_WEBHOOK_URL` and
`CLIENT_PAYMENT_WEBHOOK_SECRET` for production. See [the API guide](docs/api.md)
for Next.js callback signature validation and the revised checkout flow.
