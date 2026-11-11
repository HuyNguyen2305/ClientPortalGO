-- Dev sample data: one customer, two locations, six invoices, four estimates.
-- Safe to run more than once; each run adds another sample customer.
DO $$
DECLARE
    cust BIGINT;
    sa   BIGINT;  -- San Antonio location
    ny   BIGINT;  -- New York location
    doc  BIGINT;
    s    TEXT;
    d    DATE;
    i    INT := 0;
BEGIN
    INSERT INTO customers (name, email, phone)
    VALUES ('William Saliba', 'william.saliba@example.com', '+1 232 412 412')
    RETURNING id INTO cust;

    INSERT INTO locations (customer_id, street, city, state, postal_code)
    VALUES (cust, 'West Loop 1604 South', 'San Antonio', 'TX', '12341')
    RETURNING id INTO sa;

    INSERT INTO locations (customer_id, street, city, state, postal_code)
    VALUES (cust, 'Wall Street', 'New York', 'NY', '1232312')
    RETURNING id INTO ny;

    -- Invoices: four paid, one open, one draft, a month apart.
    FOREACH s IN ARRAY ARRAY['paid', 'paid', 'paid', 'paid', 'open', 'draft'] LOOP
        d := DATE '2026-07-10' + make_interval(months => i);
        INSERT INTO billing_documents
            (parent_type, number, customer_id, location_id, title, status, document_date,
             total_cents, amount_paid_cents)
        VALUES ('invoice', nextval('invoice_number_seq'), cust, sa, 'Bi-Monthly Service', s, d,
                6500, CASE WHEN s = 'paid' THEN 6500 ELSE 0 END)
        RETURNING id INTO doc;

        INSERT INTO billing_document_items
            (billing_document_id, position, name, description, quantity, unit_price_cents)
        VALUES (doc, 0, 'Bi-Monthly Service', 'General maintenance every other month.', 1, 6500);

        IF s = 'paid' THEN
            INSERT INTO payments (billing_document_id, amount_cents, paid_at)
            VALUES (doc, 6500, d + TIME '12:00');
        END IF;
        i := i + 1;
    END LOOP;

    -- Estimates: three accepted, one open.
    i := 0;
    FOREACH s IN ARRAY ARRAY['accepted', 'accepted', 'accepted', 'open'] LOOP
        d := DATE '2026-08-02' + make_interval(months => i * 3);
        INSERT INTO billing_documents
            (parent_type, number, customer_id, location_id, title, status, document_date, total_cents)
        VALUES ('estimate', nextval('estimate_number_seq'), cust, ny, 'Bi-Monthly Service', s, d, 6500)
        RETURNING id INTO doc;

        INSERT INTO billing_document_items
            (billing_document_id, position, name, description, quantity, unit_price_cents)
        VALUES (doc, 0, 'Bi-Monthly Service', 'General maintenance every other month.', 1, 6500);
        i := i + 1;
    END LOOP;
END $$;
