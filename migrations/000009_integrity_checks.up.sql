-- Database-side guards for rules the service layer already enforces.

-- Money never goes negative and a document is never paid beyond its total.
ALTER TABLE billing_documents
    ADD CONSTRAINT billing_documents_amounts_check
    CHECK (total_cents >= 0 AND amount_paid_cents >= 0 AND amount_paid_cents <= total_cents);

-- A document or booking can only point at a location, and a booking at an estimate, of the same customer.
ALTER TABLE locations
    ADD CONSTRAINT locations_id_customer_id_key UNIQUE (id, customer_id);
ALTER TABLE billing_documents
    ADD CONSTRAINT billing_documents_id_customer_id_key UNIQUE (id, customer_id);

ALTER TABLE billing_documents
    ADD CONSTRAINT billing_documents_location_customer_fkey
    FOREIGN KEY (location_id, customer_id) REFERENCES locations (id, customer_id);
ALTER TABLE bookings
    ADD CONSTRAINT bookings_location_customer_fkey
    FOREIGN KEY (location_id, customer_id) REFERENCES locations (id, customer_id);
ALTER TABLE bookings
    ADD CONSTRAINT bookings_estimate_customer_fkey
    FOREIGN KEY (estimate_id, customer_id) REFERENCES billing_documents (id, customer_id);
