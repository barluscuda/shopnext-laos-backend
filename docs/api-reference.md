# API endpoint reference

Generated from [OpenAPI](openapi.yaml). Start with the [API guide](api.md) for authentication, workflows and Postman setup.

Regenerate with `python3 tools/generate_api_docs.py`; verify with `python3 tools/generate_api_docs.py --check`.

Paths use `/api/v1` unless stated otherwise. Required response fields describe presence, not whether their values can be null. Shared error responses are described in the guide.

## GET /api/v1/health/live

Process liveness

**Authentication:** Public.

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | [Live](#live) |

## GET /api/v1/health/ready

PostgreSQL and Redis readiness

**Authentication:** Public.

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [Ready](#ready) |

## GET /api/v1/openapi.yaml

Download this API specification

**Authentication:** Public.

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/yaml` | string |

## GET /api/v1/products

Browse active catalog

**Authentication:** Public.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `page` | query | No | integer | default: `1`; minimum: `1` |
| `limit` | query | No | integer | default: `30`; minimum: `1`; maximum: `100` |
| `category` | query | No | string | Top-level category slug; child categories are not expanded. |
| `q` | query | No | string |  |
| `sort` | query | No | string | enum: `["newest", "price-asc", "price-desc"]`; default: `"newest"` |

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` array of [ProductView](#productview), `pagination` [Pagination](#pagination) |

## GET /api/v1/products/{slug}

Product details

**Authentication:** Public.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `slug` | path | Yes | string |  |

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [ProductView](#productview) |

## GET /api/v1/products/{slug}/related

Up to four related products

**Authentication:** Public.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `slug` | path | Yes | string |  |

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` array of [ProductView](#productview) |

## GET /api/v1/categories

All categories in sibling order

**Authentication:** Public.

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` array of [Category](#category) |

## GET /api/v1/content

Active hero slides and promotions

**Authentication:** Public.

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [ContentCollection](#contentcollection) |

## GET /api/v1/delivery-options

Delivery providers and branches

**Authentication:** Public.

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` array of [DeliveryProvider](#deliveryprovider) |

## GET /api/v1/geography

Laos provinces and districts

**Authentication:** Public.

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` array of [Province](#province) |

## GET /api/v1/payment-methods

Enabled payment banks and hold duration

**Authentication:** Public.

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [PaymentMethods](#paymentmethods) |

## POST /api/v1/otp/request

Send six-digit SMS OTP

**Authentication:** Public.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |

**Request body:** `application/json` (required).

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `phone` | string | Yes |  |

Example (replace `{{variables}}` with real values):

```json
{
  "phone": "{{phone}}"
}
```

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [OTPRequested](#otprequested) |

## POST /api/v1/otp/verify

Consume OTP and return three-day, five-use phone verification key

**Authentication:** Public.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |

**Request body:** `application/json` (required).

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `phone` | string | Yes |  |
| `code` | string | Yes | pattern: `"^[0-9]{6}$"` |

Example (replace `{{variables}}` with real values):

```json
{
  "phone": "{{phone}}",
  "code": "{{otp_code}}"
}
```

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [PhoneVerificationKeyResult](#phoneverificationkeyresult) |

## GET /api/v1/phone-verification

Check phone verification header key without consuming a use

**Authentication:** Phone verification header key or public (owner data requires verified phone).

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `X-Phone-Verification-Key` | header | Yes | string | OTP verification key, valid for three days and five protected operations. Exact checkout replays do not consume another use. |

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [PhoneVerification](#phoneverification) |

## DELETE /api/v1/phone-verification

Revoke phone verification header key

**Authentication:** Phone verification header key or public (owner data requires verified phone).

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |
| `X-Phone-Verification-Key` | header | Yes | string | OTP verification key, valid for three days and five protected operations. Exact checkout replays do not consume another use. |

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [PhoneVerification](#phoneverification) |

## POST /api/v1/orders

Create verified guest order and return online payment QR data

**Authentication:** Phone verification header key.

Checks X-Phone-Verification-Key against recipient_phone. Consumes one use atomically with order creation. An idempotent replay returns current order and payment state without consuming another use. Pickup code is initially empty until manually assigned by staff. Online payment data includes QR/deeplink and expiry; QR failure still returns the created order (201) with payment_error so clients can retry the same order.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |
| `Idempotency-Key` | header | No | string | 24h phone-scoped replay; reuse with different normalized body returns 409. maxLength: `128` |
| `X-Phone-Verification-Key` | header | Yes | string | OTP verification key, valid for three days and five protected operations. Exact checkout replays do not consume another use. |

**Request body:** `application/json` (required).

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `recipient_name` | string | Yes |  |
| `recipient_phone` | string | Yes |  |
| `provider_id` | string | Yes |  |
| `province` | string | Yes |  |
| `city` | string | Yes |  |
| `branch_name` | string | Yes |  |
| `payment_type` | string | Yes | enum: `["ONLINE", "COD_PROVIDER"]` |
| `payment_method` | string | No |  |
| `items` | array of [CheckoutItem](#checkoutitem) | Yes | minItems: `1`; maxItems: `30` |

Example (replace `{{variables}}` with real values):

```json
{
  "recipient_name": "{{recipient_name}}",
  "recipient_phone": "{{phone}}",
  "provider_id": "{{provider_id}}",
  "province": "{{province}}",
  "city": "{{city}}",
  "branch_name": "{{branch_name}}",
  "payment_type": "ONLINE",
  "payment_method": "{{payment_method}}",
  "items": [
    {
      "variant_id": "{{variant_id}}",
      "quantity": 1
    }
  ]
}
```

| Success status | Content type | Response |
| --- | --- | --- |
| 201 | `application/json` | object: `data` [CheckoutResult](#checkoutresult) |

## GET /api/v1/bills

Latest 50 orders for verified phone

**Authentication:** Phone verification header key.

Matching verification key required. Consumes one use; latest 50 orders only. A trusted-device key cannot authorize phone history.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `phone` | query | Yes | string |  |
| `X-Phone-Verification-Key` | header | Yes | string | OTP verification key, valid for three days and five protected operations. Exact checkout replays do not consume another use. |

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` array of [Order](#order) |

## GET /api/v1/bills/search

Search public order summary by bill number, or protected phone history

**Authentication:** Phone verification header key or public (owner data requires verified phone).

Bill-number search returns a public BillSummary. Phone search requires a matching verification key and consumes one use.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `q` | query | Yes | string |  |
| `X-Phone-Verification-Key` | header | No | string | OTP verification key, valid for three days and five protected operations. Exact checkout replays do not consume another use. |

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [BillSearch](#billsearch) |

## GET /api/v1/bills/{bill}

Public basic order status and pickup readiness

**Authentication:** Public.

No credentials required. Returns statuses, creation time, total and publicly visible pickup code; excludes recipient identity, item and delivery details, and payment QR.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `bill` | path | Yes | string |  |

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [BillSummary](#billsummary) |

## POST /api/v1/bills/{bill}/payment-attempts

Create replacement bank QR

**Authentication:** Phone verification header key or Order trusted-device header key.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `bill` | path | Yes | string |  |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |
| `X-Phone-Verification-Key` | header | No | string | OTP verification key, valid for three days and five protected operations. Exact checkout replays do not consume another use. |
| `X-Trust-Device-Key` | header | No | string | Order-scoped trusted-device JWT. User-Agent must match the stored device. Valid for 30 days; it does not authorize another order. |
| `User-Agent` | header | No | string | Required when using a trust key; must match the stored device. |

**Request body:** `application/json` (required).

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `bank` | string | Yes | enum: `["BCEL", "JDB", "LDB", "IB", "STB", "MMONEYX"]` |

Example (replace `{{variables}}` with real values):

```json
{
  "bank": "{{payment_method}}"
}
```

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [Attempt](#attempt) |

## POST /api/v1/bills/{bill}/simulate-payment

Development-only payment simulation

**Authentication:** Phone verification header key or Order trusted-device header key.

Not registered in production or when a real payment provider is configured.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `bill` | path | Yes | string |  |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |
| `X-Phone-Verification-Key` | header | No | string | OTP verification key, valid for three days and five protected operations. Exact checkout replays do not consume another use. |
| `X-Trust-Device-Key` | header | No | string | Order-scoped trusted-device JWT. User-Agent must match the stored device. Valid for 30 days; it does not authorize another order. |
| `User-Agent` | header | No | string | Required when using a trust key; must match the stored device. |

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [ActionResult](#actionresult) |

## POST /api/v1/webhooks/phajay

Authenticated, deduplicated payment callback

**Authentication:** Webhook HMAC signature.

Hex HMAC-SHA256 of raw body. Merchant callback authentication scheme must be agreed before rollout. Unsigned acceptance only with development provider outside production.

**Request body:** `application/json` (required).

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `transactionId` | string or integer | Yes |  |
| `billNumber` | string | No |  |
| `txnAmount` | integer or string | No |  |
| `status` | string | No |  |
| `refNo` | integer or string | No |  |

Example (replace `{{variables}}` with real values):

```json
{
  "transactionId": "{{provider_transaction_id}}",
  "billNumber": "{{bill_number}}",
  "txnAmount": 1,
  "status": "PAYMENT_COMPLETED",
  "refNo": 1
}
```

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [WebhookResult](#webhookresult) |

## POST /api/v1/maintenance/release-expired

Release up to 100 expired reservations

**Authentication:** Maintenance bearer secret.

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` object: `released` integer (int64) |

## POST /api/v1/admin/auth/login

Log in named staff or optional legacy owner

**Authentication:** Public.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |

**Request body:** `application/json` (required).

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `email` | string | Yes |  |
| `password` | string (password) | Yes |  |

Example (replace `{{variables}}` with real values):

```json
{
  "email": "{{staff_email}}",
  "password": "{{staff_password}}"
}
```

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [Actor](#actor) |

## GET /api/v1/admin/auth/session

Read staff identity

**Authentication:** Staff cookie.

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [Actor](#actor) |

## POST /api/v1/admin/auth/logout

Revoke staff cookie

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [ActionResult](#actionresult) |

## GET /api/v1/admin/dashboard

Revenue, trends, statuses and low stock

**Authentication:** Staff cookie.

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [Dashboard](#dashboard) |

## GET /api/v1/admin/products

List products (role permission enforced)

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `page` | query | No | integer | default: `1`; minimum: `1` |
| `limit` | query | No | integer | default: `30`; minimum: `1`; maximum: `100` |
| `q` | query | No | string | maxLength: `200` |
| `sort` | query | No | string | Allowlisted record field. Default created_at; fields differ by resource. |
| `order` | query | No | string | enum: `["asc", "desc"]`; default: `"desc"` |
| `status` | query | No | string |  |
| `deleted` | query | No | string | enum: `["exclude", "include", "only"]`; default: `"exclude"` |
| `category_id` | query | No | string |  |

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` array of [Product](#product), `pagination` [Pagination](#pagination) |

## POST /api/v1/admin/products

Create product with initial variants/images

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |

**Request body:** `application/json` (required).

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `category_id` | string | Yes |  |
| `sku` | string | No | Optional on creation; defaults to P- plus uppercase slug. Must be 2–40 characters after defaulting.; maxLength: `40` |
| `name` | string | Yes | minLength: `2`; maxLength: `120` |
| `name_en` | string | Yes | minLength: `2`; maxLength: `120` |
| `slug` | string | Yes |  |
| `description` | string | No | maxLength: `2000` |
| `base_price_kip` | integer (int64) | Yes | minimum: `0`; maximum: `2000000000` |
| `status` | string | No |  |
| `initial_stock` | integer (int64) | No | minimum: `0`; maximum: `1000000000` |
| `variants` | array of [VariantInput](#variantinput) | No | Creation only; omit for a default variant. Updates use child endpoints.; maxItems: `50` |
| `images` | array of [ImageInput](#imageinput) | No | Creation only; updates use child endpoints.; maxItems: `30` |

Example (replace `{{variables}}` with real values):

```json
{
  "category_id": "{{category_id}}",
  "sku": "SAMPLE-001",
  "name": "Sample item",
  "name_en": "Sample item",
  "slug": "sample-item",
  "description": "sample",
  "base_price_kip": 1,
  "status": "ACTIVE",
  "initial_stock": 1,
  "variants": [
    {
      "sku": "SAMPLE-001",
      "attributes": {},
      "price_override_kip": 1,
      "stock_on_hand": 1,
      "position": 1
    }
  ],
  "images": [
    {
      "url": "{{media_url}}",
      "alt_text": "sample",
      "position": 1
    }
  ]
}
```

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [Product](#product) |

## GET /api/v1/admin/categories

List categories (role permission enforced)

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `page` | query | No | integer | default: `1`; minimum: `1` |
| `limit` | query | No | integer | default: `30`; minimum: `1`; maximum: `100` |
| `q` | query | No | string | maxLength: `200` |
| `sort` | query | No | string | Allowlisted record field. Default created_at; fields differ by resource. |
| `order` | query | No | string | enum: `["asc", "desc"]`; default: `"desc"` |

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` array of [Category](#category), `pagination` [Pagination](#pagination) |

## POST /api/v1/admin/categories

Create categories

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |

**Request body:** `application/json` (required).

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `name` | string | Yes | minLength: `2`; maxLength: `60` |
| `name_en` | string | Yes | minLength: `2`; maxLength: `60` |
| `slug` | string | Yes |  |
| `parent_id` | string or null | No |  |

Example (replace `{{variables}}` with real values):

```json
{
  "name": "Sample item",
  "name_en": "Sample item",
  "slug": "sample-item",
  "parent_id": null
}
```

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [Category](#category) |

## GET /api/v1/admin/providers

List providers (role permission enforced)

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `page` | query | No | integer | default: `1`; minimum: `1` |
| `limit` | query | No | integer | default: `30`; minimum: `1`; maximum: `100` |
| `q` | query | No | string | maxLength: `200` |
| `sort` | query | No | string | Allowlisted record field. Default created_at; fields differ by resource. |
| `order` | query | No | string | enum: `["asc", "desc"]`; default: `"desc"` |

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` array of [Provider](#provider), `pagination` [Pagination](#pagination) |

## POST /api/v1/admin/providers

Create providers

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |

**Request body:** `application/json` (required).

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `name` | string | Yes | minLength: `2`; maxLength: `60` |
| `shipping_fee_kip` | integer (int64) | Yes | minimum: `0`; maximum: `1000000` |

Example (replace `{{variables}}` with real values):

```json
{
  "name": "Sample item",
  "shipping_fee_kip": 1
}
```

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [Provider](#provider) |

## GET /api/v1/admin/branches

List branches (role permission enforced)

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `page` | query | No | integer | default: `1`; minimum: `1` |
| `limit` | query | No | integer | default: `30`; minimum: `1`; maximum: `100` |
| `q` | query | No | string | maxLength: `200` |
| `sort` | query | No | string | Allowlisted record field. Default created_at; fields differ by resource. |
| `order` | query | No | string | enum: `["asc", "desc"]`; default: `"desc"` |
| `provider_id` | query | No | string |  |

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` array of [Branch](#branch), `pagination` [Pagination](#pagination) |

## POST /api/v1/admin/branches

Create delivery branch

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |

**Request body:** `application/json` (required).

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `provider_id` | string | Yes |  |
| `province` | string | Yes |  |
| `city` | string | Yes |  |
| `name` | string | Yes | minLength: `2`; maxLength: `80` |

Example (replace `{{variables}}` with real values):

```json
{
  "provider_id": "{{provider_id}}",
  "province": "{{province}}",
  "city": "{{city}}",
  "name": "Sample item"
}
```

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [Branch](#branch) |

## GET /api/v1/admin/hero-slides

List hero-slides (role permission enforced)

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `page` | query | No | integer | default: `1`; minimum: `1` |
| `limit` | query | No | integer | default: `30`; minimum: `1`; maximum: `100` |
| `q` | query | No | string | maxLength: `200` |
| `sort` | query | No | string | Allowlisted record field. Default created_at; fields differ by resource. |
| `order` | query | No | string | enum: `["asc", "desc"]`; default: `"desc"` |

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` array of [Content](#content), `pagination` [Pagination](#pagination) |

## POST /api/v1/admin/hero-slides

Create hero-slides

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |

**Request body:** `application/json` (required).

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `image_url` | string | Yes |  |
| `alt_text` | string | No |  |
| `heading` | string | No |  |
| `subheading` | string | No |  |
| `cta_label` | string | No |  |
| `cta_href` | string | No |  |
| `position` | integer | No | minimum: `0` |
| `active` | boolean | No | default: `true` |

Example (replace `{{variables}}` with real values):

```json
{
  "image_url": "{{media_url}}",
  "alt_text": "sample",
  "heading": "sample",
  "subheading": "sample",
  "cta_label": "sample",
  "cta_href": "/products",
  "position": 1,
  "active": true
}
```

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [Content](#content) |

## GET /api/v1/admin/promo-banners

List promo-banners (role permission enforced)

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `page` | query | No | integer | default: `1`; minimum: `1` |
| `limit` | query | No | integer | default: `30`; minimum: `1`; maximum: `100` |
| `q` | query | No | string | maxLength: `200` |
| `sort` | query | No | string | Allowlisted record field. Default created_at; fields differ by resource. |
| `order` | query | No | string | enum: `["asc", "desc"]`; default: `"desc"` |

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` array of [Content](#content), `pagination` [Pagination](#pagination) |

## POST /api/v1/admin/promo-banners

Create promo-banners

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |

**Request body:** `application/json` (required).

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `image_url` | string | Yes |  |
| `alt_text` | string | No |  |
| `heading` | string | No |  |
| `subheading` | string | No |  |
| `cta_label` | string | No |  |
| `cta_href` | string | No |  |
| `position` | integer | No | minimum: `0` |
| `active` | boolean | No | default: `true` |

Example (replace `{{variables}}` with real values):

```json
{
  "image_url": "{{media_url}}",
  "alt_text": "sample",
  "heading": "sample",
  "subheading": "sample",
  "cta_label": "sample",
  "cta_href": "/products",
  "position": 1,
  "active": true
}
```

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [Content](#content) |

## GET /api/v1/admin/orders

List orders (role permission enforced)

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `page` | query | No | integer | default: `1`; minimum: `1` |
| `limit` | query | No | integer | default: `30`; minimum: `1`; maximum: `100` |
| `q` | query | No | string | maxLength: `200` |
| `sort` | query | No | string | Allowlisted record field. Default created_at; fields differ by resource. |
| `order` | query | No | string | enum: `["asc", "desc"]`; default: `"desc"` |
| `status` | query | No | string |  |
| `payment` | query | No | string |  |

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` array of [Order](#order), `pagination` [Pagination](#pagination) |

## GET /api/v1/admin/payment-attempts

List payment-attempts (role permission enforced)

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `page` | query | No | integer | default: `1`; minimum: `1` |
| `limit` | query | No | integer | default: `30`; minimum: `1`; maximum: `100` |
| `q` | query | No | string | maxLength: `200` |
| `sort` | query | No | string | Allowlisted record field. Default created_at; fields differ by resource. |
| `order` | query | No | string | enum: `["asc", "desc"]`; default: `"desc"` |
| `status` | query | No | string |  |
| `order_id` | query | No | string |  |

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` array of [Attempt](#attempt), `pagination` [Pagination](#pagination) |

## GET /api/v1/admin/payment-events

List payment-events (role permission enforced)

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `page` | query | No | integer | default: `1`; minimum: `1` |
| `limit` | query | No | integer | default: `30`; minimum: `1`; maximum: `100` |
| `q` | query | No | string | maxLength: `200` |
| `sort` | query | No | string | Allowlisted record field. Default created_at; fields differ by resource. |
| `order` | query | No | string | enum: `["asc", "desc"]`; default: `"desc"` |
| `status` | query | No | string |  |

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` array of [PaymentEvent](#paymentevent), `pagination` [Pagination](#pagination) |

## GET /api/v1/admin/refunds

List refunds (role permission enforced)

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `page` | query | No | integer | default: `1`; minimum: `1` |
| `limit` | query | No | integer | default: `30`; minimum: `1`; maximum: `100` |
| `q` | query | No | string | maxLength: `200` |
| `sort` | query | No | string | Allowlisted record field. Default created_at; fields differ by resource. |
| `order` | query | No | string | enum: `["asc", "desc"]`; default: `"desc"` |
| `status` | query | No | string |  |
| `order_id` | query | No | string |  |

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` array of [Refund](#refund), `pagination` [Pagination](#pagination) |

## GET /api/v1/admin/staff

List staff (role permission enforced)

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `page` | query | No | integer | default: `1`; minimum: `1` |
| `limit` | query | No | integer | default: `30`; minimum: `1`; maximum: `100` |
| `q` | query | No | string | maxLength: `200` |
| `sort` | query | No | string | Allowlisted record field. Default created_at; fields differ by resource. |
| `order` | query | No | string | enum: `["asc", "desc"]`; default: `"desc"` |
| `role` | query | No | string | enum: `["OWNER", "CATALOG", "FULFILMENT", "FINANCE", "SUPPORT", "AUDITOR"]` |

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` array of [Staff](#staff), `pagination` [Pagination](#pagination) |

## POST /api/v1/admin/staff

Create named staff

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |

**Request body:** `application/json` (required).

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `email` | string | Yes |  |
| `name` | string | Yes |  |
| `role` | string | Yes | enum: `["OWNER", "CATALOG", "FULFILMENT", "FINANCE", "SUPPORT", "AUDITOR"]` |
| `password` | string (password) | Yes | minLength: `10`; maxLength: `1024` |

Example (replace `{{variables}}` with real values):

```json
{
  "email": "{{new_staff_email}}",
  "name": "{{new_staff_name}}",
  "role": "OWNER",
  "password": "{{new_staff_password}}"
}
```

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [Staff](#staff) |

## GET /api/v1/admin/sms-logs

List sms-logs (role permission enforced)

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `page` | query | No | integer | default: `1`; minimum: `1` |
| `limit` | query | No | integer | default: `30`; minimum: `1`; maximum: `100` |
| `q` | query | No | string | maxLength: `200` |
| `sort` | query | No | string | Allowlisted record field. Default created_at; fields differ by resource. |
| `order` | query | No | string | enum: `["asc", "desc"]`; default: `"desc"` |

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` array of [SMSLog](#smslog), `pagination` [Pagination](#pagination) |

## GET /api/v1/admin/outbox

List outbox (role permission enforced)

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `page` | query | No | integer | default: `1`; minimum: `1` |
| `limit` | query | No | integer | default: `30`; minimum: `1`; maximum: `100` |
| `q` | query | No | string | maxLength: `200` |
| `sort` | query | No | string | Allowlisted record field. Default created_at; fields differ by resource. |
| `order` | query | No | string | enum: `["asc", "desc"]`; default: `"desc"` |
| `status` | query | No | string |  |

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` array of [Outbox](#outbox), `pagination` [Pagination](#pagination) |

## GET /api/v1/admin/audit-logs

List audit-logs (role permission enforced)

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `page` | query | No | integer | default: `1`; minimum: `1` |
| `limit` | query | No | integer | default: `30`; minimum: `1`; maximum: `100` |
| `q` | query | No | string | maxLength: `200` |
| `sort` | query | No | string | Allowlisted record field. Default created_at; fields differ by resource. |
| `order` | query | No | string | enum: `["asc", "desc"]`; default: `"desc"` |

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` array of [Audit](#audit), `pagination` [Pagination](#pagination) |

## GET /api/v1/admin/orders/export

Export filtered orders as UTF-8 CSV

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `q` | query | No | string |  |
| `status` | query | No | string |  |
| `payment` | query | No | string |  |

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `text/csv` | string |

## GET /api/v1/admin/orders/{bill}

Full staff bill, events and refunds

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `bill` | path | Yes | string |  |

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [Bill](#bill) |

## POST /api/v1/admin/orders/{bill}/mark-paid

Order mark-paid with audit/event

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `bill` | path | Yes | string |  |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |

**Request body:** `application/json` (required).

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `note` | string | No | maxLength: `2000` |

Example (replace `{{variables}}` with real values):

```json
{
  "note": "Staff action"
}
```

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [ActionResult](#actionresult) |

## POST /api/v1/admin/orders/{bill}/mark-delivered

Order mark-delivered with audit/event

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `bill` | path | Yes | string |  |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |

**Request body:** `application/json` (required).

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `note` | string | No | maxLength: `2000` |

Example (replace `{{variables}}` with real values):

```json
{
  "note": "Staff action"
}
```

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [ActionResult](#actionresult) |

## POST /api/v1/admin/orders/{bill}/cancel

Order cancel with audit/event

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `bill` | path | Yes | string |  |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |

**Request body:** `application/json` (required).

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `note` | string | No | maxLength: `2000` |

Example (replace `{{variables}}` with real values):

```json
{
  "note": "Staff action"
}
```

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [ActionResult](#actionresult) |

## POST /api/v1/admin/orders/{bill}/refunds

Request full online refund

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `bill` | path | Yes | string |  |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |

**Request body:** `application/json` (required).

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `reason` | string | Yes | minLength: `1`; maxLength: `2000` |

Example (replace `{{variables}}` with real values):

```json
{
  "reason": "Customer requested refund"
}
```

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [Refund](#refund) |

## POST /api/v1/admin/refunds/{id}/approve

Approve with named-staff dual control

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `id` | path | Yes | string |  |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [ActionResult](#actionresult) |

## POST /api/v1/admin/refunds/{id}/resolve

Resolve an independently verified unknown/manual refund

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `id` | path | Yes | string |  |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |

**Request body:** `application/json` (required).

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `provider_refund_bill_id` | string | Yes |  |
| `terminal_success` | boolean | Yes |  |

Example (replace `{{variables}}` with real values):

```json
{
  "provider_refund_bill_id": "{{provider_refund_bill_id}}",
  "terminal_success": true
}
```

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [ActionResult](#actionresult) |

## GET /api/v1/admin/products/{id}

Full product for editing

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `id` | path | Yes | string |  |

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [ProductView](#productview) |

## PUT /api/v1/admin/products/{id}

Replace product fields; use child endpoints for variants/images

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `id` | path | Yes | string |  |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |

**Request body:** `application/json` (required).

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `category_id` | string | Yes |  |
| `sku` | string | No | Optional on creation; defaults to P- plus uppercase slug. Must be 2–40 characters after defaulting.; maxLength: `40` |
| `name` | string | Yes | minLength: `2`; maxLength: `120` |
| `name_en` | string | Yes | minLength: `2`; maxLength: `120` |
| `slug` | string | Yes |  |
| `description` | string | No | maxLength: `2000` |
| `base_price_kip` | integer (int64) | Yes | minimum: `0`; maximum: `2000000000` |
| `status` | string | No |  |
| `initial_stock` | integer (int64) | No | minimum: `0`; maximum: `1000000000` |
| `variants` | array of [VariantInput](#variantinput) | No | Creation only; omit for a default variant. Updates use child endpoints.; maxItems: `50` |
| `images` | array of [ImageInput](#imageinput) | No | Creation only; updates use child endpoints.; maxItems: `30` |

Example (replace `{{variables}}` with real values):

```json
{
  "category_id": "{{category_id}}",
  "sku": "SAMPLE-001",
  "name": "Sample item",
  "name_en": "Sample item",
  "slug": "sample-item",
  "description": "sample",
  "base_price_kip": 1,
  "status": "ACTIVE",
  "initial_stock": 1,
  "variants": [
    {
      "sku": "SAMPLE-001",
      "attributes": {},
      "price_override_kip": 1,
      "stock_on_hand": 1,
      "position": 1
    }
  ],
  "images": [
    {
      "url": "{{media_url}}",
      "alt_text": "sample",
      "position": 1
    }
  ]
}
```

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [Product](#product) |

## DELETE /api/v1/admin/products/{id}

Soft-delete product

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `id` | path | Yes | string |  |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [ActionResult](#actionresult) |

## POST /api/v1/admin/products/{id}/restore

Restore product visibility

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `id` | path | Yes | string |  |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [ActionResult](#actionresult) |

## GET /api/v1/admin/products/{id}/variants

List up to 100 product variants

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `id` | path | Yes | string |  |

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` object: `variants` array of [Variant](#variant), `total` integer (int64) |

## POST /api/v1/admin/products/{id}/variants

Add variant

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `id` | path | Yes | string |  |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |

**Request body:** `application/json` (required).

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `sku` | string | Yes | minLength: `2`; maxLength: `40` |
| `attributes` | object | No |  |
| `price_override_kip` | integer or null (int64) | No | minimum: `0`; maximum: `2000000000` |
| `stock_on_hand` | integer (int64) | Yes | minimum: `0`; maximum: `1000000000` |
| `position` | integer (int64) | No |  |

Example (replace `{{variables}}` with real values):

```json
{
  "sku": "SAMPLE-001",
  "attributes": {},
  "price_override_kip": 1,
  "stock_on_hand": 1,
  "position": 1
}
```

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [Variant](#variant) |

## PUT /api/v1/admin/products/{id}/variants/{variant}

Replace variant fields; cannot reduce below reserved stock

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `id` | path | Yes | string |  |
| `variant` | path | Yes | string |  |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |

**Request body:** `application/json` (required).

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `sku` | string | Yes | minLength: `2`; maxLength: `40` |
| `attributes` | object | No |  |
| `price_override_kip` | integer or null (int64) | No | minimum: `0`; maximum: `2000000000` |
| `stock_on_hand` | integer (int64) | Yes | minimum: `0`; maximum: `1000000000` |
| `position` | integer (int64) | No |  |

Example (replace `{{variables}}` with real values):

```json
{
  "sku": "SAMPLE-001",
  "attributes": {},
  "price_override_kip": 1,
  "stock_on_hand": 1,
  "position": 1
}
```

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [Variant](#variant) |

## DELETE /api/v1/admin/products/{id}/variants/{variant}

Delete unreferenced non-last variant

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `id` | path | Yes | string |  |
| `variant` | path | Yes | string |  |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [ActionResult](#actionresult) |

## POST /api/v1/admin/products/{id}/images

Attach uploaded product image

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `id` | path | Yes | string |  |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |

**Request body:** `application/json` (required).

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `url` | string | Yes |  |
| `alt_text` | string | No |  |
| `position` | integer (int64) | No |  |

Example (replace `{{variables}}` with real values):

```json
{
  "url": "{{media_url}}",
  "alt_text": "sample",
  "position": 1
}
```

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [Image](#image) |

## DELETE /api/v1/admin/products/{id}/images/{image}

Detach image and remove unreferenced file

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `id` | path | Yes | string |  |
| `image` | path | Yes | string |  |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [ActionResult](#actionresult) |

## PUT /api/v1/admin/categories/{id}

Replace categories fields

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `id` | path | Yes | string |  |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |

**Request body:** `application/json` (required).

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `name` | string | Yes | minLength: `2`; maxLength: `60` |
| `name_en` | string | Yes | minLength: `2`; maxLength: `60` |
| `slug` | string | Yes |  |
| `parent_id` | string or null | No |  |

Example (replace `{{variables}}` with real values):

```json
{
  "name": "Sample item",
  "name_en": "Sample item",
  "slug": "sample-item",
  "parent_id": null
}
```

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [Category](#category) |

## DELETE /api/v1/admin/categories/{id}

Delete categories subject to references

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `id` | path | Yes | string |  |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [ActionResult](#actionresult) |

## PUT /api/v1/admin/providers/{id}

Replace providers fields

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `id` | path | Yes | string |  |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |

**Request body:** `application/json` (required).

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `name` | string | Yes | minLength: `2`; maxLength: `60` |
| `shipping_fee_kip` | integer (int64) | Yes | minimum: `0`; maximum: `1000000` |

Example (replace `{{variables}}` with real values):

```json
{
  "name": "Sample item",
  "shipping_fee_kip": 1
}
```

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [Provider](#provider) |

## PUT /api/v1/admin/hero-slides/{id}

Replace hero-slides fields

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `id` | path | Yes | string |  |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |

**Request body:** `application/json` (required).

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `image_url` | string | Yes |  |
| `alt_text` | string | No |  |
| `heading` | string | No |  |
| `subheading` | string | No |  |
| `cta_label` | string | No |  |
| `cta_href` | string | No |  |
| `position` | integer | No | minimum: `0` |
| `active` | boolean | No | default: `true` |

Example (replace `{{variables}}` with real values):

```json
{
  "image_url": "{{media_url}}",
  "alt_text": "sample",
  "heading": "sample",
  "subheading": "sample",
  "cta_label": "sample",
  "cta_href": "/products",
  "position": 1,
  "active": true
}
```

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [Content](#content) |

## DELETE /api/v1/admin/hero-slides/{id}

Delete hero-slides subject to references

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `id` | path | Yes | string |  |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [ActionResult](#actionresult) |

## PUT /api/v1/admin/promo-banners/{id}

Replace promo-banners fields

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `id` | path | Yes | string |  |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |

**Request body:** `application/json` (required).

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `image_url` | string | Yes |  |
| `alt_text` | string | No |  |
| `heading` | string | No |  |
| `subheading` | string | No |  |
| `cta_label` | string | No |  |
| `cta_href` | string | No |  |
| `position` | integer | No | minimum: `0` |
| `active` | boolean | No | default: `true` |

Example (replace `{{variables}}` with real values):

```json
{
  "image_url": "{{media_url}}",
  "alt_text": "sample",
  "heading": "sample",
  "subheading": "sample",
  "cta_label": "sample",
  "cta_href": "/products",
  "position": 1,
  "active": true
}
```

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [Content](#content) |

## DELETE /api/v1/admin/promo-banners/{id}

Delete promo-banners subject to references

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `id` | path | Yes | string |  |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [ActionResult](#actionresult) |

## DELETE /api/v1/admin/branches/{id}

Delete branches subject to references

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `id` | path | Yes | string |  |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [ActionResult](#actionresult) |

## POST /api/v1/admin/categories/reorder

Reorder distinct members within group

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |

**Request body:** `application/json` (required).

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `ids` | array of string | Yes | minItems: `1`; maxItems: `500`; uniqueItems: `true` |
| `parent_id` | string or null | No |  |

Example (replace `{{variables}}` with real values):

```json
{
  "ids": [
    "{{category_id}}"
  ],
  "parent_id": null
}
```

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [ActionResult](#actionresult) |

## POST /api/v1/admin/hero-slides/reorder

Reorder distinct members within group

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |

**Request body:** `application/json` (required).

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `ids` | array of string | Yes | minItems: `1`; maxItems: `500`; uniqueItems: `true` |
| `parent_id` | string or null | No |  |

Example (replace `{{variables}}` with real values):

```json
{
  "ids": [
    "{{hero_slide_id}}"
  ],
  "parent_id": null
}
```

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [ActionResult](#actionresult) |

## POST /api/v1/admin/promo-banners/reorder

Reorder distinct members within group

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |

**Request body:** `application/json` (required).

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `ids` | array of string | Yes | minItems: `1`; maxItems: `500`; uniqueItems: `true` |
| `parent_id` | string or null | No |  |

Example (replace `{{variables}}` with real values):

```json
{
  "ids": [
    "{{promo_banner_id}}"
  ],
  "parent_id": null
}
```

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [ActionResult](#actionresult) |

## POST /api/v1/admin/products/{id}/variants/reorder

Reorder distinct members within group

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `id` | path | Yes | string |  |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |

**Request body:** `application/json` (required).

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `ids` | array of string | Yes | minItems: `1`; maxItems: `500`; uniqueItems: `true` |
| `parent_id` | string or null | No |  |

Example (replace `{{variables}}` with real values):

```json
{
  "ids": [
    "{{variant_id}}"
  ],
  "parent_id": null
}
```

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [ActionResult](#actionresult) |

## POST /api/v1/admin/products/{id}/images/reorder

Reorder distinct members within group

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `id` | path | Yes | string |  |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |

**Request body:** `application/json` (required).

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `ids` | array of string | Yes | minItems: `1`; maxItems: `500`; uniqueItems: `true` |
| `parent_id` | string or null | No |  |

Example (replace `{{variables}}` with real values):

```json
{
  "ids": [
    "{{image_id}}"
  ],
  "parent_id": null
}
```

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [ActionResult](#actionresult) |

## POST /api/v1/admin/staff/{id}/disable

Staff disable with audit and session rules

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `id` | path | Yes | string |  |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [ActionResult](#actionresult) |

## POST /api/v1/admin/staff/{id}/enable

Staff enable with audit and session rules

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `id` | path | Yes | string |  |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [ActionResult](#actionresult) |

## POST /api/v1/admin/staff/{id}/reset-password

Staff reset-password with audit and session rules

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `id` | path | Yes | string |  |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |

**Request body:** `application/json` (required).

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `password` | string (password) | Yes | minLength: `10`; maxLength: `1024` |

Example (replace `{{variables}}` with real values):

```json
{
  "password": "{{new_staff_password}}"
}
```

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [ActionResult](#actionresult) |

## POST /api/v1/admin/uploads

Upload validated WebP; content kind requires 3:1

**Authentication:** Staff cookie.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |
| `kind` | query | No | string | enum: `["product", "content"]`; default: `"product"` |

**Request body:** `multipart/form-data` (required).

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `file` | string (binary) | Yes | WebP only, maximum 5 MiB. |

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [UploadResult](#uploadresult) |

## GET /media/{filename}

Read persisted WebP

**Authentication:** Public.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `filename` | path | Yes | string |  |

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `image/webp` | string |

## GET /api/v1/bills/{bill}/details

Protected full order details and trusted-device key

**Authentication:** Phone verification header key or Order trusted-device header key.

Requires an OTP key matching the recipient phone or a valid order-scoped trust JWT. OTP authorization consumes one use and issues a trust key. Trust authorization checks the stored User-Agent every time and does not consume an OTP use. No customer cookies are accepted. Next.js must forward the actual device User-Agent.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `bill` | path | Yes | string |  |
| `X-Phone-Verification-Key` | header | No | string | OTP verification key, valid for three days and five protected operations. Exact checkout replays do not consume another use. |
| `X-Trust-Device-Key` | header | No | string | Order-scoped trusted-device JWT. User-Agent must match the stored device. Valid for 30 days; it does not authorize another order. |
| `User-Agent` | header | Yes | string | Actual end-user device User-Agent forwarded by Next.js. |

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [ClientBill](#clientbill) |

## PUT /api/v1/admin/orders/{bill}/pickup-code

Manually assign pickup code to a confirmed order

**Authentication:** Staff cookie.

OWNER, FULFILMENT or SUPPORT. Accepts a unique 1–80 character code. Order must be CONFIRMED. Writes assignment, order event, audit and pickup SMS enqueueing in one transaction. Repeating the current code succeeds without duplicate notifications.

| Parameter | Location | Required | Type | Notes |
| --- | --- | --- | --- | --- |
| `bill` | path | Yes | string |  |
| `X-ShopNext-CSRF` | header | Yes | string | Required on all browser mutations. const: `"1"` |

**Request body:** `application/json` (required).

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `pickup_code` | string | Yes | minLength: `1`; maxLength: `80` |

Example (replace `{{variables}}` with real values):

```json
{
  "pickup_code": "{{pickup_code}}"
}
```

| Success status | Content type | Response |
| --- | --- | --- |
| 200 | `application/json` | object: `data` [ActionResult](#actionresult) |

## Data schemas

Response objects below use snake_case; provider webhook fields retain their provider casing.

### Base

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `id` | string | Yes |  |
| `created_at` | string (date-time) | Yes |  |
| `updated_at` | string (date-time) | Yes |  |

### Category

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `id` | string | Yes |  |
| `created_at` | string (date-time) | Yes |  |
| `updated_at` | string (date-time) | Yes |  |
| `parent_id` | string or null | Yes |  |
| `name` | string | Yes |  |
| `name_en` | string | Yes |  |
| `slug` | string | Yes |  |
| `sort_order` | integer (int64) | Yes |  |

### Product

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `id` | string | Yes |  |
| `created_at` | string (date-time) | Yes |  |
| `updated_at` | string (date-time) | Yes |  |
| `category_id` | string | Yes |  |
| `sku` | string | Yes |  |
| `name` | string | Yes |  |
| `name_en` | string | Yes |  |
| `slug` | string | Yes |  |
| `description` | string | Yes |  |
| `base_price_kip` | integer (int64) | Yes |  |
| `status` | string | Yes |  |
| `deleted_at` | string (date-time) or null | Yes |  |

### Variant

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `id` | string | Yes |  |
| `created_at` | string (date-time) | Yes |  |
| `updated_at` | string (date-time) | Yes |  |
| `product_id` | string | Yes |  |
| `sku` | string | Yes |  |
| `attributes` | object | Yes |  |
| `price_override_kip` | integer (int64) or null | Yes |  |
| `stock_on_hand` | integer (int64) | Yes |  |
| `stock_reserved` | integer (int64) | Yes |  |
| `position` | integer (int64) | Yes |  |

### Image

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `id` | string | Yes |  |
| `created_at` | string (date-time) | Yes |  |
| `updated_at` | string (date-time) | Yes |  |
| `product_id` | string | Yes |  |
| `url` | string | Yes |  |
| `alt_text` | string | Yes |  |
| `position` | integer (int64) | Yes |  |

### Content

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `id` | string | Yes |  |
| `created_at` | string (date-time) | Yes |  |
| `updated_at` | string (date-time) | Yes |  |
| `image_url` | string | Yes |  |
| `alt_text` | string | Yes |  |
| `heading` | string | Yes |  |
| `subheading` | string | Yes |  |
| `cta_label` | string | Yes |  |
| `cta_href` | string | Yes |  |
| `position` | integer (int64) | Yes |  |
| `active` | boolean | Yes |  |

### Provider

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `id` | string | Yes |  |
| `created_at` | string (date-time) | Yes |  |
| `updated_at` | string (date-time) | Yes |  |
| `name` | string | Yes |  |
| `shipping_fee_kip` | integer (int64) | Yes |  |

### Branch

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `id` | string | Yes |  |
| `created_at` | string (date-time) | Yes |  |
| `updated_at` | string (date-time) | Yes |  |
| `provider_id` | string | Yes |  |
| `province` | string | Yes |  |
| `city` | string | Yes |  |
| `name` | string | Yes |  |

### Order

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `bill_number` | string | Yes |  |
| `pickup_code` | string | Yes |  |
| `recipient_phone` | string | Yes |  |
| `recipient_name` | string | Yes |  |
| `express_provider_id` | string | Yes |  |
| `branch_id` | string | Yes |  |
| `payment_type` | string | Yes |  |
| `payment_method` | string | Yes |  |
| `payment_status` | string | Yes |  |
| `order_status` | string | Yes |  |
| `subtotal_kip` | integer (int64) | Yes |  |
| `shipping_fee_kip` | integer (int64) | Yes |  |
| `total_kip` | integer (int64) | Yes |  |
| `reservation_expires_at` | string (date-time) or null | Yes |  |
| `verified_at` | string (date-time) | Yes |  |
| `created_at` | string (date-time) | Yes |  |

### Item

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `id` | string | Yes |  |
| `created_at` | string (date-time) | Yes |  |
| `updated_at` | string (date-time) | Yes |  |
| `order_id` | string | Yes |  |
| `product_variant_id` | string | Yes |  |
| `product_name_snapshot` | string | Yes |  |
| `unit_price_kip_snapshot` | integer (int64) | Yes |  |
| `quantity` | integer (int64) | Yes |  |
| `line_total_kip` | integer (int64) | Yes |  |

### OrderEvent

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `id` | string | Yes |  |
| `created_at` | string (date-time) | Yes |  |
| `updated_at` | string (date-time) | Yes |  |
| `order_id` | string | Yes |  |
| `action` | string | Yes |  |
| `note` | string | Yes |  |

### Attempt

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `id` | string | Yes |  |
| `created_at` | string (date-time) | Yes |  |
| `updated_at` | string (date-time) | Yes |  |
| `order_id` | string | Yes |  |
| `provider` | string | Yes |  |
| `bank` | string | Yes |  |
| `provider_transaction_id` | string or null | Yes |  |
| `amount_kip` | integer (int64) | Yes |  |
| `status` | string | Yes |  |
| `qr_code` | string | Yes |  |
| `deeplink` | string | Yes |  |
| `expires_at` | string (date-time) | Yes |  |
| `last_error` | string | Yes |  |

### PaymentEvent

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `id` | string | Yes |  |
| `created_at` | string (date-time) | Yes |  |
| `updated_at` | string (date-time) | Yes |  |
| `provider` | string | Yes |  |
| `provider_transaction_id` | string | Yes |  |
| `dedupe_key` | string | Yes |  |
| `bill_number` | string | Yes |  |
| `normalized_status` | string | Yes |  |
| `amount_kip` | integer (int64) or null | Yes |  |
| `raw_body` | string | Yes |  |
| `outcome` | string | Yes |  |
| `processed_at` | string (date-time) or null | Yes |  |

### Refund

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `id` | string | Yes |  |
| `created_at` | string (date-time) | Yes |  |
| `updated_at` | string (date-time) | Yes |  |
| `payment_attempt_id` | string | Yes |  |
| `order_id` | string | Yes |  |
| `amount_kip` | integer (int64) | Yes |  |
| `reason` | string | Yes |  |
| `status` | string | Yes |  |
| `provider_refund_bill_id` | string or null | Yes |  |
| `provider_status` | string | Yes |  |
| `requested_by` | string | Yes |  |
| `approved_by` | string | Yes |  |
| `last_error` | string | Yes |  |
| `submitted_at` | string (date-time) or null | Yes |  |
| `completed_at` | string (date-time) or null | Yes |  |

### OTP

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `id` | string | Yes |  |
| `created_at` | string (date-time) | Yes |  |
| `updated_at` | string (date-time) | Yes |  |
| `phone` | string | Yes |  |
| `expires_at` | string (date-time) | Yes |  |
| `attempts` | integer (int64) | Yes |  |
| `consumed_at` | string (date-time) or null | Yes |  |

### PhoneToken

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `id` | string | Yes |  |
| `created_at` | string (date-time) | Yes |  |
| `updated_at` | string (date-time) | Yes |  |
| `phone` | string | Yes |  |
| `expires_at` | string (date-time) | Yes |  |

### Staff

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `id` | string | Yes |  |
| `created_at` | string (date-time) | Yes |  |
| `updated_at` | string (date-time) | Yes |  |
| `email` | string | Yes |  |
| `name` | string | Yes |  |
| `role` | string | Yes |  |
| `disabled_at` | string (date-time) or null | Yes |  |

### Session

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `id` | string | Yes |  |
| `created_at` | string (date-time) | Yes |  |
| `updated_at` | string (date-time) | Yes |  |
| `user_id` | string | Yes |  |
| `expires_at` | string (date-time) | Yes |  |
| `revoked_at` | string (date-time) or null | Yes |  |

### Actor

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `id` | string | Yes |  |
| `email` | string | Yes |  |
| `name` | string | Yes |  |
| `role` | string | Yes |  |
| `legacy` | boolean | Yes |  |

### Audit

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `id` | string | Yes |  |
| `created_at` | string (date-time) | Yes |  |
| `updated_at` | string (date-time) | Yes |  |
| `actor_email` | string | Yes |  |
| `actor_role` | string | Yes |  |
| `actor_legacy` | boolean | Yes |  |
| `action` | string | Yes |  |
| `target_type` | string | Yes |  |
| `target_id` | string | Yes |  |
| `summary` | string | Yes |  |
| `ip` | string | Yes |  |

### Outbox

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `id` | string | Yes |  |
| `created_at` | string (date-time) | Yes |  |
| `updated_at` | string (date-time) | Yes |  |
| `phone` | string | Yes |  |
| `dedupe_key` | string | Yes |  |
| `status` | string | Yes |  |
| `attempts` | integer (int64) | Yes |  |
| `last_error` | string | Yes |  |
| `available_at` | string (date-time) | Yes |  |
| `lease_until` | string (date-time) or null | Yes |  |
| `sent_at` | string (date-time) or null | Yes |  |

### SMSLog

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `id` | string | Yes |  |
| `created_at` | string (date-time) | Yes |  |
| `updated_at` | string (date-time) | Yes |  |
| `phone` | string | Yes |  |
| `message` | string | Yes |  |
| `accepted` | boolean | Yes |  |

### Idempotency

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `id` | string | Yes |  |
| `created_at` | string (date-time) | Yes |  |
| `updated_at` | string (date-time) | Yes |  |
| `scope` | string | Yes |  |
| `key` | string | Yes |  |
| `request_hash` | string | Yes |  |
| `response` | object | Yes |  |
| `expires_at` | string (date-time) | Yes |  |

### ProductView

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `id` | string | Yes |  |
| `created_at` | string (date-time) | Yes |  |
| `updated_at` | string (date-time) | Yes |  |
| `category_id` | string | Yes |  |
| `sku` | string | Yes |  |
| `name` | string | Yes |  |
| `name_en` | string | Yes |  |
| `slug` | string | Yes |  |
| `description` | string | Yes |  |
| `base_price_kip` | integer (int64) | Yes |  |
| `status` | string | Yes |  |
| `deleted_at` | string (date-time) or null | Yes |  |
| `variants` | array or null | Yes |  |
| `images` | array or null | Yes |  |
| `category` | [Category](#category) | Yes |  |
| `in_stock` | integer (int64) | Yes |  |
| `display_variant_id` | string | Yes |  |
| `display_price_kip` | integer (int64) | Yes |  |
| `is_new` | boolean | Yes |  |

### DeliveryProvider

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `id` | string | Yes |  |
| `created_at` | string (date-time) | Yes |  |
| `updated_at` | string (date-time) | Yes |  |
| `name` | string | Yes |  |
| `shipping_fee_kip` | integer (int64) | Yes |  |
| `branches` | array or null | Yes |  |

### VariantInput

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `sku` | string | Yes | minLength: `2`; maxLength: `40` |
| `attributes` | object | No |  |
| `price_override_kip` | integer or null (int64) | No | minimum: `0`; maximum: `2000000000` |
| `stock_on_hand` | integer (int64) | Yes | minimum: `0`; maximum: `1000000000` |
| `position` | integer (int64) | No |  |

### ProductInput

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `category_id` | string | Yes |  |
| `sku` | string | No | Optional on creation; defaults to P- plus uppercase slug. Must be 2–40 characters after defaulting.; maxLength: `40` |
| `name` | string | Yes | minLength: `2`; maxLength: `120` |
| `name_en` | string | Yes | minLength: `2`; maxLength: `120` |
| `slug` | string | Yes |  |
| `description` | string | No | maxLength: `2000` |
| `base_price_kip` | integer (int64) | Yes | minimum: `0`; maximum: `2000000000` |
| `status` | string | No |  |
| `initial_stock` | integer (int64) | No | minimum: `0`; maximum: `1000000000` |
| `variants` | array of [VariantInput](#variantinput) | No | Creation only; omit for a default variant. Updates use child endpoints.; maxItems: `50` |
| `images` | array of [ImageInput](#imageinput) | No | Creation only; updates use child endpoints.; maxItems: `30` |

### ImageInput

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `url` | string | Yes |  |
| `alt_text` | string | No |  |
| `position` | integer (int64) | No |  |

### CategoryInput

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `name` | string | Yes | minLength: `2`; maxLength: `60` |
| `name_en` | string | Yes | minLength: `2`; maxLength: `60` |
| `slug` | string | Yes |  |
| `parent_id` | string or null | No |  |

### ReorderInput

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `ids` | array of string | Yes | minItems: `1`; maxItems: `500`; uniqueItems: `true` |
| `parent_id` | string or null | No |  |

### CheckoutItem

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `variant_id` | string | Yes |  |
| `quantity` | integer (int64) | Yes | minimum: `1`; maximum: `99` |

### Checkout

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `recipient_name` | string | Yes |  |
| `recipient_phone` | string | Yes |  |
| `provider_id` | string | Yes |  |
| `province` | string | Yes |  |
| `city` | string | Yes |  |
| `branch_name` | string | Yes |  |
| `payment_type` | string | Yes | enum: `["ONLINE", "COD_PROVIDER"]` |
| `payment_method` | string | No |  |
| `items` | array of [CheckoutItem](#checkoutitem) | Yes | minItems: `1`; maxItems: `30` |

### CheckoutResult

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `bill_number` | string | Yes |  |
| `pickup_code` | string | Yes | Empty until assigned manually by staff. |
| `total_kip` | integer (int64) | Yes |  |
| `order_id` | string | Yes | Same identifier as bill_number. |
| `order_status` | string | Yes |  |
| `payment_status` | string | Yes |  |
| `created_at` | string (date-time) | Yes |  |
| `reservation_expires_at` | string (date-time) or null | Yes |  |
| `payment` | [Attempt](#attempt) or null | Yes |  |
| `payment_error` | string | No | PAYMENT_QR_UNAVAILABLE when order creation succeeded but a QR could not be generated. |

### Bill

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `bill_number` | string | Yes |  |
| `pickup_code` | string | Yes |  |
| `recipient_phone` | string | Yes |  |
| `recipient_name` | string | Yes |  |
| `express_provider_id` | string | Yes |  |
| `branch_id` | string | Yes |  |
| `payment_type` | string | Yes |  |
| `payment_method` | string | Yes |  |
| `payment_status` | string | Yes |  |
| `order_status` | string | Yes |  |
| `subtotal_kip` | integer (int64) | Yes |  |
| `shipping_fee_kip` | integer (int64) | Yes |  |
| `total_kip` | integer (int64) | Yes |  |
| `reservation_expires_at` | string (date-time) or null | Yes |  |
| `verified_at` | string (date-time) | Yes |  |
| `created_at` | string (date-time) | Yes |  |
| `recipient_name_masked` | boolean | Yes |  |
| `items` | array or null | Yes |  |
| `provider` | [Provider](#provider) | Yes |  |
| `branch` | [Branch](#branch) | Yes |  |
| `payment_attempt` | [Attempt](#attempt) or null | Yes |  |
| `events` | array or null | No |  |
| `refunds` | array or null | No |  |

### StaffInput

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `email` | string | Yes |  |
| `name` | string | Yes |  |
| `role` | string | Yes | enum: `["OWNER", "CATALOG", "FULFILMENT", "FINANCE", "SUPPORT", "AUDITOR"]` |
| `password` | string (password) | Yes | minLength: `10`; maxLength: `1024` |

### LowStock

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `id` | string | Yes |  |
| `created_at` | string (date-time) | Yes |  |
| `updated_at` | string (date-time) | Yes |  |
| `product_id` | string | Yes |  |
| `sku` | string | Yes |  |
| `attributes` | object | Yes |  |
| `price_override_kip` | integer (int64) or null | Yes |  |
| `stock_on_hand` | integer (int64) | Yes |  |
| `stock_reserved` | integer (int64) | Yes |  |
| `position` | integer (int64) | Yes |  |
| `product_name` | string | Yes |  |
| `product_slug` | string | Yes |  |

### SalesPoint

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `date` | string | Yes |  |
| `revenue_kip` | integer (int64) | Yes |  |
| `items` | integer (int64) | Yes |  |

### Dashboard

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `status_counts` | object | Yes |  |
| `expiring_count` | integer (int64) | Yes |  |
| `today_revenue_kip` | integer (int64) | Yes |  |
| `sales` | array or null | Yes |  |
| `low_stock` | array or null | Yes |  |
| `low_stock_count` | integer (int64) | Yes |  |
| `recent_orders` | array or null | Yes |  |

### Error

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `error` | object: `code` string, `message` string, `phone_verification_required` boolean, `otp_request_url` string, `otp_verify_url` string | Yes |  |
| `request_id` | string | Yes |  |

### Pagination

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `page` | integer | Yes | minimum: `1` |
| `limit` | integer | Yes | minimum: `1`; maximum: `100` |
| `total` | integer (int64) | Yes |  |

### PhoneRequest

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `phone` | string | Yes |  |

### OTPVerify

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `phone` | string | Yes |  |
| `code` | string | Yes | pattern: `"^[0-9]{6}$"` |

### Login

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `email` | string | Yes |  |
| `password` | string (password) | Yes |  |

### OTPRequested

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `requested` | boolean | Yes |  |
| `dev_code` | string | No | Only non-production; never available in production. |

### PhoneVerification

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `verified` | boolean | Yes |  |
| `phone` | string | No |  |

### ActionResult

Type: object

### BankRequest

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `bank` | string | Yes | enum: `["BCEL", "JDB", "LDB", "IB", "STB", "MMONEYX"]` |

### NoteRequest

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `note` | string | No | maxLength: `2000` |

### RefundRequest

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `reason` | string | Yes | minLength: `1`; maxLength: `2000` |

### RefundResolve

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `provider_refund_bill_id` | string | Yes |  |
| `terminal_success` | boolean | Yes |  |

### ProviderInput

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `name` | string | Yes | minLength: `2`; maxLength: `60` |
| `shipping_fee_kip` | integer (int64) | Yes | minimum: `0`; maximum: `1000000` |

### BranchInput

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `provider_id` | string | Yes |  |
| `province` | string | Yes |  |
| `city` | string | Yes |  |
| `name` | string | Yes | minLength: `2`; maxLength: `80` |

### ContentInput

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `image_url` | string | Yes |  |
| `alt_text` | string | No |  |
| `heading` | string | No |  |
| `subheading` | string | No |  |
| `cta_label` | string | No |  |
| `cta_href` | string | No |  |
| `position` | integer | No | minimum: `0` |
| `active` | boolean | No | default: `true` |

### PasswordReset

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `password` | string (password) | Yes | minLength: `10`; maxLength: `1024` |

### Province

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `name` | string | Yes |  |
| `type` | string | Yes |  |
| `districts` | array of string | Yes |  |

### PaymentMethods

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `provider` | string | Yes |  |
| `banks` | array of string | Yes |  |
| `hold_minutes` | integer | Yes |  |

### ContentCollection

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `hero_slides` | array of [Content](#content) | Yes |  |
| `promo_banners` | array of [Content](#content) | Yes |  |

### UploadResult

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `url` | string | Yes |  |

### Webhook

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `transactionId` | string or integer | Yes |  |
| `billNumber` | string | No |  |
| `txnAmount` | integer or string | No |  |
| `status` | string | No |  |
| `refNo` | integer or string | No |  |

### WebhookResult

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `outcome` | string | Yes | enum: `["APPLIED", "DEDUPED", "ALREADY_PAID", "AMOUNT_MISMATCH", "REFERENCE_MISMATCH", "RECONCILIATION_REQUIRED", "UNMATCHED", "IGNORED"]` |

### BillSearch

Type: object: `type` object, `bills` array of [Order](#order) or object: `type` object, `bill` [BillSummary](#billsummary)

```json
{
  "oneOf": [
    {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "type": {
          "const": "phone"
        },
        "bills": {
          "type": "array",
          "items": {
            "$ref": "#/components/schemas/Order"
          }
        }
      },
      "required": [
        "type",
        "bills"
      ]
    },
    {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "type": {
          "const": "bill_number"
        },
        "bill": {
          "$ref": "#/components/schemas/BillSummary"
        }
      },
      "required": [
        "type",
        "bill"
      ]
    }
  ]
}
```

### Ready

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `status` | object | Yes | const: `"ready"` |

### Live

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `status` | object | Yes | const: `"ok"` |

### PhoneVerificationKeyResult

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `verified` | boolean | Yes | const: `true` |
| `verification_key` | string | Yes |  |
| `expires_at` | string (date-time) | Yes |  |
| `max_uses` | integer | Yes | const: `5` |

### BillSummary

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `bill_number` | string | Yes |  |
| `payment_status` | string | Yes |  |
| `order_status` | string | Yes |  |
| `pickup_code` | string | Yes |  |
| `created_at` | string (date-time) | Yes |  |
| `total_kip` | integer (int64) | Yes |  |
| `reservation_expires_at` | string (date-time) or null | Yes |  |
| `order_id` | string | Yes |  |
| `payment_successful` | boolean | Yes |  |
| `pickup_code_ready` | boolean | Yes |  |

### ClientBill

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `bill_number` | string | Yes |  |
| `pickup_code` | string | Yes |  |
| `recipient_phone` | string | Yes |  |
| `recipient_name` | string | Yes |  |
| `express_provider_id` | string | Yes |  |
| `branch_id` | string | Yes |  |
| `payment_type` | string | Yes |  |
| `payment_method` | string | Yes |  |
| `payment_status` | string | Yes |  |
| `order_status` | string | Yes |  |
| `subtotal_kip` | integer (int64) | Yes |  |
| `shipping_fee_kip` | integer (int64) | Yes |  |
| `total_kip` | integer (int64) | Yes |  |
| `reservation_expires_at` | string (date-time) or null | Yes |  |
| `verified_at` | string (date-time) | Yes |  |
| `created_at` | string (date-time) | Yes |  |
| `recipient_name_masked` | boolean | Yes |  |
| `items` | array or null | Yes |  |
| `provider` | [Provider](#provider) | Yes |  |
| `branch` | [Branch](#branch) | Yes |  |
| `payment_attempt` | [Attempt](#attempt) or null | Yes |  |
| `events` | array or null | No |  |
| `refunds` | array or null | No |  |
| `trust_device_key` | string | No | Issued when OTP authorization is used and no trust key is supplied. |
| `trust_device_expires_at` | string (date-time) | No |  |

### PickupCodeInput

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `pickup_code` | string | Yes | minLength: `1`; maxLength: `80` |

### ClientPaymentEvent

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `event_id` | string | Yes |  |
| `type` | string | Yes | const: `"order.payment_succeeded"` |
| `order_id` | string | Yes |  |
| `payment_status` | string | Yes | const: `"PAID"` |
| `order_status` | string | Yes | const: `"CONFIRMED"` |
| `paid_at` | string (date-time) | Yes |  |
| `total_kip` | integer (int64) | Yes |  |
