-- Invoices and estimates share this table; parent_type tells them apart.
CREATE SEQUENCE invoice_number_seq START WITH 85000;
CREATE SEQUENCE estimate_number_seq START WITH 5000;

CREATE TABLE billing_documents (
    id                BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    parent_type       TEXT        NOT NULL,
    number            BIGINT      NOT NULL,
    customer_id       BIGINT      NOT NULL REFERENCES customers (id),
    location_id       BIGINT      NOT NULL REFERENCES locations (id),
    title             TEXT        NOT NULL,
    status            TEXT        NOT NULL,
    document_date     DATE        NOT NULL,
    total_cents       BIGINT      NOT NULL DEFAULT 0,
    amount_paid_cents BIGINT      NOT NULL DEFAULT 0,
    converted_from_id BIGINT      REFERENCES billing_documents (id),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT billing_documents_parent_type_check
        CHECK (parent_type IN ('invoice', 'estimate')),
    CONSTRAINT billing_documents_status_check CHECK (
        (parent_type = 'invoice'  AND status IN ('draft', 'open', 'partially_paid', 'paid', 'void'))
        OR
        (parent_type = 'estimate' AND status IN ('draft', 'open', 'accepted', 'declined'))
    )
);

CREATE UNIQUE INDEX billing_documents_parent_type_number_key
    ON billing_documents (parent_type, number);
CREATE UNIQUE INDEX billing_documents_converted_from_id_key
    ON billing_documents (converted_from_id) WHERE converted_from_id IS NOT NULL;
CREATE INDEX billing_documents_list_idx
    ON billing_documents (customer_id, parent_type, document_date DESC, id DESC);
CREATE INDEX billing_documents_location_id_idx
    ON billing_documents (location_id);
