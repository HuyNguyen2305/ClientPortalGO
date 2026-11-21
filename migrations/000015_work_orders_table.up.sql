-- A work order is its own row, linked to the booking it serves and to the service template.
-- customer_id and service_id are copied from the booking (both are fixed for its life) so a customer's list needs no join.
CREATE TABLE work_orders (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    booking_id   BIGINT      NOT NULL REFERENCES bookings (id),
    customer_id  BIGINT      NOT NULL REFERENCES customers (id),
    service_id   BIGINT      NOT NULL REFERENCES services (id),
    status       TEXT        NOT NULL DEFAULT 'scheduled',
    started_at   TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    notes        TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT work_orders_booking_id_key UNIQUE (booking_id),
    CONSTRAINT work_orders_status_check CHECK (status IN ('scheduled', 'in_progress', 'completed'))
);

CREATE INDEX work_orders_customer_id_idx ON work_orders (customer_id, id DESC);
CREATE INDEX work_orders_service_id_idx ON work_orders (service_id);

-- One work order per existing booking, keeping what 000014 stored on the booking.
INSERT INTO work_orders (booking_id, customer_id, service_id, status, started_at, completed_at, notes, created_at)
SELECT id, customer_id, service_id, status, started_at, completed_at, notes, created_at
FROM bookings
ORDER BY id;

ALTER TABLE bookings
    DROP COLUMN notes,
    DROP COLUMN completed_at,
    DROP COLUMN started_at,
    DROP COLUMN status;
