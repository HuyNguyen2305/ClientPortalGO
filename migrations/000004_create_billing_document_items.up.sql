CREATE TABLE billing_document_items (
    id                  BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    billing_document_id BIGINT      NOT NULL REFERENCES billing_documents (id) ON DELETE CASCADE,
    position            INT         NOT NULL,
    name                TEXT        NOT NULL,
    description         TEXT,
    quantity            INT         NOT NULL CHECK (quantity > 0),
    unit_price_cents    BIGINT      NOT NULL CHECK (unit_price_cents >= 0),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX billing_document_items_billing_document_id_idx
    ON billing_document_items (billing_document_id);
