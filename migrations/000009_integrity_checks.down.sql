ALTER TABLE bookings DROP CONSTRAINT IF EXISTS bookings_estimate_customer_fkey;
ALTER TABLE bookings DROP CONSTRAINT IF EXISTS bookings_location_customer_fkey;
ALTER TABLE billing_documents DROP CONSTRAINT IF EXISTS billing_documents_location_customer_fkey;
ALTER TABLE billing_documents DROP CONSTRAINT IF EXISTS billing_documents_id_customer_id_key;
ALTER TABLE locations DROP CONSTRAINT IF EXISTS locations_id_customer_id_key;
ALTER TABLE billing_documents DROP CONSTRAINT IF EXISTS billing_documents_amounts_check;
