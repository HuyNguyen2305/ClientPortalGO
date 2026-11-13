-- A client-chosen key makes a retried payment request record once.
ALTER TABLE payments ADD COLUMN idempotency_key TEXT;

CREATE UNIQUE INDEX payments_idempotency_key_idx
    ON payments (billing_document_id, idempotency_key) WHERE idempotency_key IS NOT NULL;
