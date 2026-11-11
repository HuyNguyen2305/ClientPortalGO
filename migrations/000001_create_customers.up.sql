CREATE SEQUENCE customer_account_number_seq START WITH 5000;

CREATE TABLE customers (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_number BIGINT      NOT NULL DEFAULT nextval('customer_account_number_seq'),
    name           TEXT        NOT NULL,
    email          TEXT,
    phone          TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT customers_account_number_key UNIQUE (account_number)
);

ALTER SEQUENCE customer_account_number_seq OWNED BY customers.account_number;
