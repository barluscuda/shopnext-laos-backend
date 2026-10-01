# Minimal hexagonal architecture

`internal/domain` holds framework-independent records, integer money, identity
normalization and role permissions. `internal/application` holds use cases and
ports. Driving adapters are Gin and the worker/CLI commands; driven adapters
are PostgreSQL/GORM, Redis, PhaJay, Wenova and local WebP storage. Bootstrap
constructs and owns resources explicitly. No DI container or hidden init hooks.

```text
Gin API / worker / CLI → application use cases → domain rules
                              ↓ ports
                    PostgreSQL / Redis / providers / media
```

The persistence port exposes allowlisted entities and structured predicates,
transactions and locking. It is intentionally small rather than a repository
per table. GORM mapping and PostgreSQL-specific catalog queries remain in the
adapter; domain records have no ORM tags. HTTP query inputs cannot supply table
names or arbitrary SQL identifiers.

PostgreSQL is authoritative. SQL migrations are embedded and explicitly run
by `cmd/migrate`; startup never migrates. Redis only owns distributed rate
windows, not stock, tokens or orders. All LAK amounts use int64/BIGINT with
overflow checks. Item names/prices are immutable historical snapshots.

## Transactions and lifecycle

Checkout writes inventory, order/items, events, SMS outbox and idempotency
response in one transaction. Advisory locks serialize idempotency keys and
branch/identifier allocation. Product locks precede sorted variant locks to
match administrative catalog mutations. Provider calls never hold those locks.

Online stock is reserved for 15 minutes by default. Payment confirmation
atomically converts the reservation; expiry and pending cancellation release
it. COD immediately consumes on-hand stock and is confirmed without marking
payment PAID, matching NeoShop. Delivery changes no stock; confirmed cancellation
returns stock. Cancellation never claims a refund succeeded.

Callbacks commit dedupe and business changes together. Unmatched early callbacks
are retained and replayed. Invalid amount/reference and late/superseded payments
are classified for operator reconciliation rather than changing stock.

Refund approval records intent, then durably claims submission before contacting
the gateway. Unknown outcomes are not automatically resubmitted. Only confirmed
provider success changes payment to REFUNDED. Named staff cannot approve their
own request; legacy shared-admin mode retains the original exception.

SMS uses a durable, leased outbox, at-least-once delivery, bounded retries and
a local circuit breaker. A crash after gateway acceptance but before committing
SENT can produce a duplicate message; gateway idempotency is not assumed.

Processes use signal cancellation and bounded shutdown. Constructors return
errors, resources are closed by bootstrap, and external HTTP clients have
timeouts and reject redirects. Production disables fake-provider flows.
