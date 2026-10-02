-- +goose Up
ALTER TABLE staff_sessions ALTER COLUMN user_id DROP NOT NULL;
ALTER TABLE staff_sessions ADD COLUMN legacy boolean NOT NULL DEFAULT false;
ALTER TABLE staff_sessions ADD CONSTRAINT sessions_identity CHECK (
    (legacy AND user_id IS NULL) OR (NOT legacy AND user_id IS NOT NULL)
);
-- Cookie sessions are no longer accepted after the JWT rollout.
UPDATE staff_sessions SET revoked_at = now(), updated_at = now() WHERE revoked_at IS NULL;

-- +goose Down
DELETE FROM staff_sessions WHERE legacy;
ALTER TABLE staff_sessions DROP CONSTRAINT sessions_identity;
ALTER TABLE staff_sessions DROP COLUMN legacy;
ALTER TABLE staff_sessions ALTER COLUMN user_id SET NOT NULL;
