CREATE TABLE locations (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    customer_id BIGINT      NOT NULL REFERENCES customers (id),
    street      TEXT        NOT NULL,
    city        TEXT        NOT NULL,
    state       TEXT        NOT NULL,
    postal_code TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX locations_customer_id_idx ON locations (customer_id);
