-- +goose Up
ALTER TABLE phone_tokens ADD COLUMN uses integer NOT NULL DEFAULT 0 CHECK (uses BETWEEN 0 AND 5);
-- Previously issued cookie tokens must not bypass the new header-key lifetime.
UPDATE phone_tokens SET expires_at = LEAST(expires_at, created_at + interval '3 days');
ALTER TABLE orders DROP CONSTRAINT orders_pickup_code_key;
ALTER TABLE orders ALTER COLUMN pickup_code SET DEFAULT '';
CREATE UNIQUE INDEX orders_pickup_code_assigned ON orders(pickup_code) WHERE pickup_code <> '';
CREATE TABLE trusted_devices (
    id text PRIMARY KEY,
    order_id text NOT NULL REFERENCES orders(bill_number) ON DELETE CASCADE,
    token_hash text UNIQUE NOT NULL,
    user_agent text NOT NULL,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX trusted_devices_order ON trusted_devices(order_id);

CREATE TABLE client_notifications (
 id text PRIMARY KEY,
 order_id text UNIQUE NOT NULL REFERENCES orders(bill_number),
 payload jsonb NOT NULL,
 status text NOT NULL CHECK(status IN ('PENDING','SENDING','SENT','FAILED','DEAD')),
 attempts integer NOT NULL DEFAULT 0,
 available_at timestamptz NOT NULL,
 lease_until timestamptz,
 sent_at timestamptz,
 last_error text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX client_notifications_due ON client_notifications(status, available_at);

-- +goose Down
DROP TABLE client_notifications;
DROP TABLE trusted_devices;
DROP INDEX orders_pickup_code_assigned;
-- Empty codes represent orders awaiting staff assignment; rollback requires
-- assigning unique codes to these orders before restoring the old constraint.
ALTER TABLE orders ADD CONSTRAINT orders_pickup_code_key UNIQUE(pickup_code);
ALTER TABLE orders ALTER COLUMN pickup_code DROP DEFAULT;
ALTER TABLE phone_tokens DROP COLUMN uses;
