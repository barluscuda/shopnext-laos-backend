# NeoShop backend feature parity

Reference: `/home/barluscuda/coding/neoshop/neoshop`, read-only. Scope is the
implemented backend behavior, including Next.js server actions and server-rendered
queries, not just the original route handlers. New API formats are intentional.
No customer accounts, coupons or wishlist were introduced.

| Original capability / source area | Replacement | Verification |
| --- | --- | --- |
| `lib/catalog.ts`, product detail, home/category queries | Catalog, bilingual search, top-level category filter, ordered first-available display variant, price sorting, 14-day new flag, related products | Catalog/display/filter integration tests |
| `admin/products`, variants and images actions | Create/update/soft-delete/restore, variant CRUD/stock/override/attributes, image attach/delete/reorder | Catalog editing, historical snapshots, media tests |
| `admin/categories` actions | Parent categories, sibling positions, cycle rejection, deletion restrictions, reorder | Category cycle integration test; API route coverage |
| Hero slides and promo banners actions | WebP 3:1 upload, create/update/active/delete/reorder, shared image cleanup | Media/content integration and resize/validation tests |
| Providers/branches and Laos delivery data | Flat fees, provider management, branch create/delete, deduplicated checkout branch creation, all 18 provinces/districts | Checkout, geography and management tests |
| `lib/otp.ts`, phone verification | OTP request/verify with a secret issuance challenge, cooldown/attempt and IP/device/challenge limits, hashed three-day/five-use phone header key, revocation; order-specific device JWT and User-Agent checking | HTTP challenge isolation, OTP lifecycle/concurrency and limit integration tests |
| `api/orders`, order creation and stock | Guest checkout with OTP header key, server-priced totals, snapshot items, COD/online stock rules, idempotency; online response includes QR/payment data | Concurrent checkout, transaction rollback, COD/snapshot tests |
| Bill lookup/search/phone history | Public basic status/price/time/pickup visibility; protected full details and owner-only QR; verified phone list (latest 50) | Public/owner HTTP access integration tests |
| Payment attempts and PhaJay callbacks | Six banks, retry/supersession, expiry, authentication, canonical dedupe, amount/reference checks, early callback replay, dev simulation | Mock provider contracts, payment/expiry/early callback tests |
| `lib/order-transitions.ts`, staff order actions | Mark paid/delivered/cancel and manually assign pickup code with events, inventory consistency and permissions | Delivery/payment/cancellation/expiry integration tests |
| `lib/refunds.ts`, refund adapters | Full refunds, request/dual approval, unknown-safe submission, polling, manual resolution, 24h deadline | Dual-control/refund integration; mock gateway tests |
| `lib/staff-*`, legacy admin identity | Six roles, eight-hour admin bearer JWTs, PostgreSQL session revocation (including legacy owner), shared legacy-principal guessing quota, password compatibility, bootstrap/create, disable/enable/reset/revoke | Permission/JWT/session/password tests |
| Dashboard/order export/admin lists | Paid revenue, 14-day item/revenue trend, statuses, low stock, recent orders; paginated filters; UTF-8 CSV | HTTP reporting/export and API documentation coverage |
| Audit, SMS history/outbox, Wenova | Transactional audit/notifications, direct OTP SMS, leased outbox, retry/dead-letter/breaker, redacted OTP logs; signed Next.js payment callbacks with durable retries | HTTP outbox and mock Wenova tests |
| Cron reservation release/idempotency cleanup | Continuous worker + secret-protected expiry hook | Expiry, worker and cleanup tests |
| Image processing | WebP magic validation, 5MB input cap, 1600px no-enlargement bound, quality 82, 3:1 content | Media unit/integration tests |

Docker operation: the default stack runs PostgreSQL, Redis, the explicit migration
job, API and continuous worker. First-owner bootstrap has a dedicated one-off
staff service with hidden terminal password entry. Make commands build/run/check
inside containers, and integration tests use their own Compose network and the
`shopnext_test` database. OTP verification requires the issuance challenge;
staff permissions are preserved.

Intentional hardening while preserving shopping flow: callback authentication
fails closed; callback dedupe and state changes commit together; checkout
idempotency commits with stock/order/outbox; catalog writes share lock order;
export supports authorized named staff; consumed OTPs cannot fall back to older
codes; unbound OTP callers cannot spend another issuance's attempts; legacy
login aliases share one quota; panic recovery never dumps request credentials;
CSV cells are protected against spreadsheet formula injection.

Live PhaJay/Wenova merchant acceptance is still an external rollout prerequisite,
especially callback authentication. Backend implementation/test coverage does
not assert successful real-money or real-SMS deployment.

API documentation: the [usage guide](api.md) explains header-key/OTP identity,
checkout, staff permissions, uploads, callbacks and Postman setup. The
[endpoint reference](api-reference.md) and
[Postman collection](shopnext-laos.postman_collection.json) cover all 86 OpenAPI
operations, including root-level media. Regenerate them from OpenAPI with
`python3 tools/generate_api_docs.py` and verify synchronization with `--check`.
OTP clients must retain the latest request challenge and submit it on verification.

Client flow intentionally changed: customer cookies are replaced by header keys;
online checkout includes payment data; pickup codes are entered by staff; public
bill lookup returns a summary and details require verified ownership. Payment
approval atomically queues the signed Next.js webhook and success SMS. Trust
keys are issued on OTP-authorized details requests and expire after 30 days.
