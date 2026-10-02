# ShopNext Laos API guide

Use this guide to connect a storefront, staff client or API testing tool. The
[endpoint reference](api-reference.md) covers every operation, request field and
response schema. [OpenAPI](openapi.yaml) is the machine-readable contract and is
served at `GET /api/v1/openapi.yaml`.

## Connection and conventions

The local API origin is `http://localhost:8080`. Public and administrative routes
use `/api/v1`; uploaded files use `/media/{filename}` directly on the origin.
Production clients use the deployment's HTTPS origin.

Send JSON with `Content-Type: application/json`. Fields use snake_case, except
PhaJay callback fields, which preserve provider casing. Unknown fields and extra
JSON values are rejected. Money fields ending in `_kip` are whole LAK integers;
never send decimal currency values. Use IDs returned by the API for catalog and
delivery resources; order URLs use the `bill_number`, not a product/resource ID.

Most successful JSON responses have a `data` envelope:

```json
{"data":{"verified":true}}
```

Paginated administrative lists and public product browsing also return:

```json
{"data":[],"pagination":{"page":1,"limit":30,"total":0}}
```

`page` defaults to 1 (maximum 1,000,000), and `limit` defaults to 30 (maximum 100).
Some nested collections and public lookup endpoints are not paginated; see their
response schemas. Liveness returns `{"status":"ok"}` without an envelope.
CSV export, media downloads and the OpenAPI download return their own formats.
Order creation returns HTTP 201; other successful JSON operations return 200.

## Header keys, staff cookies, CSRF and origins

The customer client is a Next.js server. The backend returns customer keys in
JSON and authenticates them from headers; it does not set or accept customer
cookies or manage a customer session. Next.js owns storage and browser delivery
of these keys. OTP keys are opaque, stored hashed in PostgreSQL, valid for three
days and at most five successful protected operations. Trusted-device keys are
HS256 JWTs containing an order ID, random token ID and expiry. Only a hash of the
random token ID is stored, together with the device User-Agent. A trust key lasts
30 days and authorizes only its order.

Send `X-Phone-Verification-Key: <verification_key>` for checkout or phone history.
For order details and payment actions, send that header or
`X-Trust-Device-Key: <trust_device_key>`. Next.js must forward the actual end-user
`User-Agent` consistently: using its own server User-Agent would bind every
customer key to the server instead of the customer's device. Each trust-key use
checks both the signature and the stored token, order, User-Agent and expiry.
A trust key cannot create an order or query another order or phone history.

Staff login continues to use `shopnext_staff`, HttpOnly, SameSite=Lax and Secure
in production, with an eight-hour lifetime. Staff password reset and disable
revoke staff sessions. Staff authentication is separate from customer keys.

Send `X-ShopNext-CSRF: 1` on mutations, including server requests, OTP requests,
login, logout, DELETE operations and uploads. Provider webhooks and maintenance
use their own authentication and are exempt. Any supplied `Origin` must exactly
match `ALLOWED_ORIGINS`. Server requests can omit Origin. OPTIONS returns 204.

## Customer checkout workflow

1. Browse `GET /products`, `/categories`, `/content` and `/delivery-options`.
   Product filters are `category` (top-level slug), `q`, `page`, `limit` and
   `sort` (`newest`, `price-asc`, `price-desc`). Select a variant from product
   details. `GET /geography` supplies valid province/district pairs;
   `GET /payment-methods` supplies banks and payment hold duration.
2. Request OTP through `POST /otp/request`, body `{"phone":"<customer phone>"}`.
   Only the newest six-digit code works. It expires after five minutes; resend
   cooldown is 60 seconds; five failed verification attempts exhaust the OTP.
   Development can return `dev_code`; production never returns it.
3. Verify through `POST /otp/verify`, body `{"phone":"<customer phone>","code":"<code>"}`.
   Response data contains `verified`, `verification_key`, `expires_at` and
   `max_uses: 5`. No Set-Cookie is emitted. Store the key in Next.js and forward
   it in `X-Phone-Verification-Key`. `GET /phone-verification` checks it without
   consuming a use; DELETE revokes it.
4. Send `POST /orders` with that header and the checkout body below. The phone
   must match the verified key. Use provider IDs and geography values returned
   by the API; `city` means district. A successful new order consumes one use
   atomically with its order and stock writes. Failed validation/state operations
   roll back that use. Missing, invalid, expired or exhausted credentials return
   HTTP 403 with `phone_verification_required: true` and OTP endpoint URLs.
5. The response (201) includes `order_id` (same as `bill_number`), `pickup_code`,
   `order_status`, `payment_status`, `created_at`, `reservation_expires_at`,
   `total_kip` and `payment`. The pickup code is initially empty. Online payment
   data contains the selected bank, `qr_code`, `deeplink`, amount and expiry.
   COD returns `payment: null`. If QR generation fails, the order still exists:
   response includes `payment_error: "PAYMENT_QR_UNAVAILABLE"`. Retry payment on
   that order using `POST /bills/{bill}/payment-attempts`, body `{"bank":"BCEL"}`;
   do not create another order just to obtain its QR.
6. PhaJay calls the authenticated backend webhook. Payment approval confirms the
   order and atomically queues a Next.js success webhook and payment-success
   SMS. The worker delivers them outside the transaction. A development owner
   can use `POST /bills/{bill}/simulate-payment`; this route is absent in
   production and with a real payment adapter.
7. Staff manually assigns the pickup code using
   `PUT /admin/orders/{bill}/pickup-code`, body `{"pickup_code":"<code>"}`.
   The order must be CONFIRMED; permitted roles are OWNER, FULFILMENT and SUPPORT.
   Codes are unique when assigned, 1–80 characters, and may be alphanumeric.
   Assignment also queues a pickup-ready SMS. Repeating the current code is a
   successful no-op. Payment SMS never claims a pickup code is ready.

Example checkout (replace placeholders before sending):

```json
{
  "recipient_name": "Test customer",
  "recipient_phone": "<verified phone>",
  "provider_id": "<provider ID>",
  "province": "<province from geography>",
  "city": "<district from geography>",
  "branch_name": "<delivery branch name>",
  "payment_type": "ONLINE",
  "payment_method": "BCEL",
  "items": [{"variant_id": "<variant ID>", "quantity": 1}]
}
```

Use `COD_PROVIDER` for cash on delivery; omit `payment_method` for COD. Online
banks are `BCEL`, `JDB`, `LDB`, `IB`, `STB`, `MMONEYX`. The server calculates whole
LAK totals from catalog and shipping prices and stores item snapshots. Checkout
accepts 1–30 rows, merging duplicates to 1–99 units per variant. Recipient and
branch names must be 2–80 characters. Online stock is reserved until payment or
expiry; COD stock is deducted at checkout.

Use `Idempotency-Key` with a unique value of at most 128 characters for checkout.
Keep the same key and body for retries. It is phone-scoped for 24 hours. Exact
replays return the same order plus current payment data without another OTP-key
use, even after all five uses have been spent, provided the phone key has not
expired or been revoked. A changed body returns `IDEMPOTENCY_CONFLICT`.

## Bill search and protected details

`GET /bills/{bill}` and bill-number `GET /bills/search?q=...` are public. They
return `order_id`, `bill_number`, `payment_status`, `payment_successful`,
`order_status`, `pickup_code`, `pickup_code_ready`, `created_at`, `total_kip` and
`reservation_expires_at`. Recipient identity, item/delivery details and QR data
require the protected details endpoint.

`GET /bills/{bill}/details` requires a matching OTP verification key or an
order-scoped trust key plus the matching User-Agent. OTP access consumes one use
and returns full bill data plus `trust_device_key` and
`trust_device_expires_at`. Save that key in Next.js for the customer's device.
On subsequent requests with a valid trust key, the server returns full details
without issuing another key or consuming an OTP use. An invalid, expired,
revoked, wrong-order or changed-User-Agent trust key returns 403; omit it and
verify the recipient phone again to obtain a replacement.

`GET /bills?phone=...` and phone `GET /bills/search?q=...` require the matching
OTP key and consume one use per successful request; latest 50 orders only.
Explicit payment retries and development simulation also consume one use when
using an OTP key. Trust-authorized order access does not consume OTP uses. Public
summary lookup and verification-status checks do not consume uses. Bill reads
can lazily release expired payment holds. Legacy customer cookies are ignored.

## Next.js payment success webhook

Configure `CLIENT_PAYMENT_WEBHOOK_URL` and a 32+ character
`CLIENT_PAYMENT_WEBHOOK_SECRET`; production requires an HTTPS URL. Configure a
32+ character random `TRUST_DEVICE_SECRET` for JWT signing in production.
Development can leave the callback unconfigured; then no client webhook is
queued. The destination is server configuration, never a checkout-supplied URL.

The worker POSTs to Next.js with this body:

```json
{
  "event_id": "<stable event ID>",
  "type": "order.payment_succeeded",
  "order_id": "<bill number>",
  "payment_status": "PAID",
  "order_status": "CONFIRMED",
  "paid_at": "<RFC3339 timestamp>",
  "total_kip": 70000
}
```

Headers: `X-ShopNext-Event-ID`, `X-ShopNext-Timestamp` (Unix seconds) and
`X-ShopNext-Signature` (hex HMAC-SHA256 of `timestamp + "." + rawBody` using the
shared secret). Verify the signature with a timing-safe comparison and reject
timestamps outside five minutes. Verify the signature before parsing JSON.
Deduplicate the stable body `event_id`; retries get a fresh timestamp/signature.
Return 2xx after durably accepting the event, then update the browser through
Next.js. The backend does not contact a browser directly.

Delivery is at least once with row leases and up to eight attempts, then DEAD.
The worker checks every five seconds and backs off failed deliveries. SMS and
client webhook retries are independent; payment success does not wait for either
external service to respond. Monitor `client_notifications` for FAILED/DEAD
rows. Public bill status remains available if notification delivery is delayed.

## Staff workflow and permissions

Create the first owner with `make staff EMAIL=owner@example.com NAME="Owner"`.
The development database starts empty. Log in with `POST /admin/auth/login`,
body `{"email":"<staff email>","password":"<staff password>"}`, plus the CSRF
header. Retain the cookie, check `/admin/auth/session`, and log out through
`POST /admin/auth/logout`.

| Capability | Roles |
| --- | --- |
| View orders, attempts, payment events and refunds; export CSV; inspect session | All staff roles |
| Manage products, variants and images | OWNER, CATALOG |
| Manage categories, providers and branches | OWNER, CATALOG |
| Manage hero slides and promo banners | OWNER, CATALOG |
| Mark paid, delivered or assign pickup code | OWNER, FULFILMENT, SUPPORT |
| Cancel orders | OWNER, SUPPORT |
| Request refund | OWNER, FINANCE, SUPPORT |
| Approve or resolve refund | OWNER, FINANCE |
| View SMS logs and outbox | OWNER, SUPPORT, AUDITOR |
| Manage staff and view audit logs | OWNER |
| View dashboard | OWNER, CATALOG, FULFILMENT, FINANCE, AUDITOR |

Permissions do not bypass order/payment/refund state checks. Refund approval
requires a different named actor from the requester. Resolve an uncertain refund
only after independent provider verification; see [operations](operations.md).

For an empty development catalog: create a category, provider and product,
then add branch/content records as needed. Product creation can include variants
and images; product updates use the dedicated child endpoints for those fields.
PUT operations supply the resource input; they are not PATCH operations.
Product deletion is soft deletion; the restore endpoint reactivates visibility.

Staff list endpoints accept `page`, `limit`, `q`, `sort`, `order` (`asc`/`desc`).
Sort fields depend on the resource; invalid sort/filter values return 400.
Additional filters include product `status`, `deleted` (`exclude`, `only`,
`include`) and `category_id`; order `status` and `payment`; branch `provider_id`;
staff `role`; attempt/refund `order_id` (bill number). The endpoint reference
lists each operation's supported parameters. CSV export uses order search/status/
payment filters and returns a downloadable UTF-8 CSV with BOM.

### Uploads and media

Send `POST /admin/uploads?kind=product` or `?kind=content` with multipart field
`file`, a WebP file of at most 5 MiB, and the CSRF header. Let the HTTP client set
the multipart Content-Type boundary. Upload permission follows the selected
kind. Images retain their aspect ratio and are resized to a maximum dimension of
1600 pixels without enlargement. Content inputs must have a 3:1 ratio.
Use the returned `data.url` when creating image/content records. Files are
served from `/media/{filename}` and receive immutable cache headers.

## Provider callbacks and maintenance

`POST /webhooks/phajay` accepts provider fields `transactionId`, `billNumber`,
`txnAmount`, `status`, `refNo`. A completed payment uses `PAYMENT_COMPLETED`.
Use the actual provider transaction reference and amount for the order.

For real providers or production, supply hex HMAC-SHA256 of the exact raw body
in the configured `PHAJAY_WEBHOOK_SIGNATURE_HEADER` (default
`x-phajay-signature`). Any whitespace/body changes require a new signature.
The merchant authentication agreement is a rollout prerequisite; see
[operations](operations.md). Unsigned callbacks are accepted only with the
development payment adapter outside production. Callback success includes an
`outcome`; HTTP 200 alone does not mean a payment was applied.

`POST /maintenance/release-expired` uses `Authorization: Bearer <CRON_SECRET>`
and returns `data.released`. The worker handles other scheduled jobs too.
Liveness checks the process; readiness checks PostgreSQL and Redis.

## Errors and troubleshooting

```json
{
  "error": {"code": "CSRF_HEADER_REQUIRED", "message": "request failed"},
  "request_id": "<request identifier>"
}
```

| Status | Meaning / action |
| --- | --- |
| 400 | Invalid JSON, fields, pagination, sort or business input; correct the request. |
| 401 | Missing/invalid staff session, maintenance secret or callback signature. |
| 403 | CSRF/origin rejection, insufficient permission or phone ownership mismatch. |
| 404 | Resource unavailable, unknown route or disabled development endpoint. |
| 409 | State conflict, inventory conflict or changed idempotent checkout. |
| 429 | Rate limit; honor `Retry-After` when present. |
| 500 | Internal failure; retain `request_id` / `X-Request-ID` for investigation. |
| 502 | Provider failure. |
| 503 | Dependency unavailable, including readiness failure. |

Inspect `error.code` to distinguish failures sharing a status. Never log
passwords, OTPs, cookies, provider credentials or personal request bodies.

## Postman collection

Import [ShopNext Laos.postman_collection.json](shopnext-laos.postman_collection.json).
It uses the [Postman Collection v2.1 schema](https://schema.postman.com/json/collection/v2.1.0/collection.json)
and includes all 86 OpenAPI operations grouped by resource.

1. Set `base_url` to the origin, default `http://localhost:8080`, without a trailing
   slash or `/api/v1` suffix. Keep cookie handling enabled and use the same host
   throughout the flow.
2. Supply your test phone and `otp_code`; request and verify OTP individually. Copy the returned key into a private `phone_verification_key` variable; for device access set `trust_device_key` and `user_agent`. Enable its header and disable the OTP header when testing trust access.
   Development `dev_code` is not automatically captured. Use local/private
   variables for credentials and OTPs; exported defaults are blank.
3. Set delivery/catalog variables from responses: `product_slug`, `product_id`,
   `variant_id`, `category_id`, `provider_id`, `province`, `city`, `branch_name`.
   Set an `idempotency_key` for checkout. Successful checkout captures only
   `bill_number` into the collection variables. An environment variable with the
   same name overrides it; update or remove that override when needed.
4. For staff requests set `staff_email` and `staff_password`, then send login.
   Staff creation/reset uses `new_staff_password`; creation also uses
   `new_staff_email` and `new_staff_name`.
5. Optional query parameters start disabled. Enable and edit the ones you need.
   Set the target resource variables before updates, deletes or reorder actions.
   Reorder examples contain a single ID; replace `ids` with the intended complete
   ordering. Uploads require manually selecting a WebP file in Body → form-data.
6. Set `maintenance_secret` for cron requests. For signed callbacks compute the
   signature of the final resolved body and set `webhook_signature` and
   `webhook_signature_header`. No signature is computed automatically.

Send requests individually in the workflow you intend. A full collection run
includes creates, deletes, staff changes, payment actions and refunds; it is not
an automated scenario. Placeholder examples need existing IDs and valid
resource-specific values. The collection does not save passwords, OTPs or
cookies into variables or print them in scripts. Keep Postman's request history
and cookie storage private.

## Keeping documentation current

`docs/openapi.yaml` contains JSON, which is valid YAML. The generator uses the
Python standard library and reads that checked-in contract:

```sh
python3 tools/generate_api_docs.py
python3 tools/generate_api_docs.py --check
```

Update OpenAPI alongside API changes, regenerate the endpoint reference and
collection, and update this workflow guide and [feature parity](feature-parity.md)
when behavior changes. Generated files are committed so readers can use them
without installing a documentation toolchain.
