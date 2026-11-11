CREATE TABLE payments (
    id                  BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    billing_document_id BIGINT      NOT NULL REFERENCES billing_documents (id),
    amount_cents        BIGINT      NOT NULL CHECK (amount_cents > 0),
    paid_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX payments_billing_document_id_idx ON payments (billing_document_id);
