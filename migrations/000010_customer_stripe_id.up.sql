ALTER TABLE customers ADD COLUMN stripe_customer_id TEXT;
ALTER TABLE customers ADD CONSTRAINT customers_stripe_customer_id_key UNIQUE (stripe_customer_id);
