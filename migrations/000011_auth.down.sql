DROP TABLE sessions;
DROP INDEX customers_email_lower_key;
ALTER TABLE customers DROP COLUMN password_hash;
