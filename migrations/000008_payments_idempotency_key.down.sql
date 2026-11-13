DROP INDEX IF EXISTS payments_idempotency_key_idx;
ALTER TABLE payments DROP COLUMN IF EXISTS idempotency_key;
