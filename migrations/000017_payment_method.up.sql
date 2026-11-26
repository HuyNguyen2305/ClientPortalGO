-- How a payment was made, for reports. Existing payments were taken by an unknown method.
ALTER TABLE payments
    ADD COLUMN method     TEXT NOT NULL DEFAULT 'other',
    ADD COLUMN card_brand TEXT,
    ADD COLUMN card_last4 TEXT,
    ADD CONSTRAINT payments_method_check CHECK (method IN ('cash', 'check', 'card', 'other')),
    ADD CONSTRAINT payments_card_check CHECK (method = 'card' OR (card_brand IS NULL AND card_last4 IS NULL));
