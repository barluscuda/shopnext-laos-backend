-- +goose Up
ALTER TABLE otp_codes ADD COLUMN challenge_hash text NOT NULL DEFAULT '';
-- Existing issuances have no challenge and must be requested again.
UPDATE otp_codes SET expires_at = LEAST(expires_at, now()), updated_at = now()
WHERE consumed_at IS NULL;

-- +goose Down
ALTER TABLE otp_codes DROP COLUMN challenge_hash;
