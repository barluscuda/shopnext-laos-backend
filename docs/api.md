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

## Cookies, CSRF and origins

Customers shop without accounts. OTP verification sets the `shopnext_phone`
cookie; staff login sets `shopnext_staff`. Both are HttpOnly, SameSite=Lax and
Secure in production. Phone verification lasts seven days; staff sessions last
eight hours. Password reset and staff disable revoke staff sessions.

Send `X-ShopNext-CSRF: 1` on every mutation, including OTP requests, login,
logout, DELETE operations and uploads. Webhooks and maintenance use their own
authentication and are exempt from this header. The header is required for
non-browser clients too.

Browser clients must use credentials, for example `fetch(url, {credentials:
"include", ...})`. Any supplied `Origin` must exactly match `ALLOWED_ORIGINS`.
Preflight OPTIONS responses return 204. Choose a frontend deployment compatible
with the cookie SameSite policy. CORS approval does not override SameSite cookie
restrictions. Postman uses its cookie jar; do not substitute bearer authentication
for phone or staff cookies.

## Customer checkout workflow

1. Browse `GET /products`, `/categories`, `/content` and `/delivery-options`.
   Product filters are `category` (top-level slug), `q`, `page`, `limit` and
   `sort` (`newest`, `price-asc`, `price-desc`). Read product details by slug to
   select a variant. `GET /geography` supplies valid province/district pairs;
   `GET /payment-methods` supplies banks and payment hold duration.
2. Send `POST /otp/request` with `{"phone":"<customer phone>"}`. Only the newest
   six-digit OTP works. OTPs expire after five minutes, resend cooldown is 60
   seconds, and five failed verification attempts exhaust the OTP. A development
   SMS response can include `dev_code`; production never returns it.
3. Send `POST /otp/verify` with `phone` and `code`, then retain its cookie.
   `GET /phone-verification` checks the session; DELETE clears it.
4. Send `POST /orders` with the verified phone and checkout body below. Copy the
   provider ID from delivery options and choose a valid province/city pair.
   `city` is the geography district value. Use a branch name of 2–80 characters.
5. Keep the returned `bill_number`, `pickup_code` and `total_kip`. Read
   `GET /bills/{bill}`. Online owners can obtain QR/deeplink data and request a new
   attempt with `POST /bills/{bill}/payment-attempts`, body `{"bank":"BCEL"}`.
6. Payment confirmation comes from the authenticated provider callback. In
   non-production with the development payment adapter, the verified owner can
   use `POST /bills/{bill}/simulate-payment`. That route is absent in production
   and when using a real provider.

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
banks are `BCEL`, `JDB`, `LDB`, `IB`, `STB`, `MMONEYX`. The server calculates totals
from catalog prices and provider shipping fees and stores historical item
snapshots. Checkout accepts 1–30 items, with 1–99 units per variant after merging
duplicates. Recipient names must be 2–80 characters.

For retries, set `Idempotency-Key` to a unique value of at most 128 characters.
Keep the same key and body when retrying the same checkout. Keys are scoped to
the verified phone; changing the checkout under a live key returns
`IDEMPOTENCY_CONFLICT`. Generate a new key for a new purchase.

### Bill visibility

Bill-number lookup is public. Recipient phone and pickup code remain visible;
recipient names are masked for non-owners. QR/deeplink access requires the
matching verified phone. `GET /bills?phone=...` requires that same phone's cookie.
`GET /bills/search?q=...` accepts a bill number or phone; phone search requires
matching verification. Bill reads can lazily release expired payment holds.

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
| Mark paid or delivered | OWNER, FULFILMENT, SUPPORT |
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
and includes all 84 OpenAPI operations grouped by resource.

1. Set `base_url` to the origin, default `http://localhost:8080`, without a trailing
   slash or `/api/v1` suffix. Keep cookie handling enabled and use the same host
   throughout the flow.
2. Supply your test phone and `otp_code`; request and verify OTP individually.
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
