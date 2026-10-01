# ShopNext Laos backend

Next-generation shopping for Laos, developed by barluscuda. This is a new
`/api/v1` backend for the complete existing NeoShop shopping and staff feature
set: no customer accounts, SMS OTP, catalog/content, delivery, guest orders,
online payment/COD, refunds, staff roles, reporting, media and notifications.

## Run locally

Requires Go 1.27.1, a C compiler (WebP), Docker and Docker Compose. Configuration
comes from environment variables, or an explicit `CONFIG_FILE`; the binaries
do **not** load `.env`. Defaults match the development Compose services.
See [.env.example](.env.example) for available settings.

```sh
make dev
make migrate
# Password is read from standard input, not a command-line argument.
go run -tags nomsgpack ./cmd/staff --email owner@example.com --name Owner
make api
# In another terminal:
make worker
```

The staff command only creates the first owner. Subsequent staff are managed by
an owner through the API. The development database starts empty: create a
category, product and delivery provider through the administrative endpoints.
Laos provinces/districts are available from `/api/v1/geography`.

For an entirely containerized development environment:

```sh
docker compose --profile app build
docker compose --profile app run --rm migrate
docker compose --profile app run --rm -T api staff --email owner@example.com --name Owner
docker compose --profile app up -d api worker
```

The staff command waits for one password line on stdin. Supply it interactively
or through a secret manager; do not put it in shell history. PostgreSQL and
Redis bind only loopback ports 55432 and 56379. The API binds port 8080.

## API

[OpenAPI](docs/openapi.yaml) is also served at `/api/v1/openapi.yaml`.
JSON uses snake_case and `{ "data": ... }`; errors contain
`error.code`, `error.message` and `request_id`. Lists use `pagination`.
Every browser mutation requires `X-ShopNext-CSRF: 1`, including login, OTP,
uploads and logout. Include credentials so HttpOnly cookies are sent.
Origins must exactly match `ALLOWED_ORIGINS` (space-separated).

Shopping sequence:

1. Browse `/products`, `/categories`, `/content` and `/delivery-options`.
2. POST `/otp/request` with `phone`, then `/otp/verify` with `phone` and `code`.
   Development responses include `dev_code`; production never does.
3. POST `/orders` with the verified phone, recipient name, provider, province,
   city, branch name, `payment_type` (`ONLINE` or `COD_PROVIDER`) and variant
   quantities. Online orders also require a supported `payment_method`.
   Use `Idempotency-Key` for safe retries; it is scoped to the verified phone.
4. Read `/bills/{bill}`. Only the verified owner receives QR/deeplink data.
   Retry QR generation through `/bills/{bill}/payment-attempts`.
5. The authenticated provider callback confirms online payment. With development
   payment mode only, POST `/bills/{bill}/simulate-payment` instead.

Bill-number lookup is intentionally public: pickup code and recipient phone
remain visible as in NeoShop; recipient names are masked for non-owners.
Phone-based lookup requires matching OTP verification. There are no customer
registration/login/profile endpoints. Staff log in through `/admin/auth/login`.

## Verify

```sh
make check
make integration
docker compose build
```

Integration tests only reset the explicitly named `shopnext_test` database and
Redis database 15 on the isolated loopback test service. They never reset the
development database. Test infrastructure uses `compose.test.yaml`; stop it
with `docker compose -f compose.test.yaml down` when finished.

See [architecture](docs/architecture.md), [feature parity](docs/feature-parity.md)
and [operations](docs/operations.md). Real provider credentials, signature
agreement and live merchant acceptance are deployment prerequisites; local
contract tests do not substitute for a live payment/refund/SMS certification.
