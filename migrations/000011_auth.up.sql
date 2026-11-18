ALTER TABLE customers ADD COLUMN password_hash TEXT;

-- Login looks customers up by lower(email); this also keeps emails unique across case.
CREATE UNIQUE INDEX customers_email_lower_key ON customers (lower(email));

CREATE TABLE sessions (
    token_hash  BYTEA       PRIMARY KEY,
    customer_id BIGINT      NOT NULL REFERENCES customers (id) ON DELETE CASCADE,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX sessions_customer_id_idx ON sessions (customer_id);
