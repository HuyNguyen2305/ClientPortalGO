ALTER TABLE payments
    DROP CONSTRAINT IF EXISTS payments_card_check,
    DROP CONSTRAINT IF EXISTS payments_method_check,
    DROP COLUMN IF EXISTS card_last4,
    DROP COLUMN IF EXISTS card_brand,
    DROP COLUMN IF EXISTS method;
