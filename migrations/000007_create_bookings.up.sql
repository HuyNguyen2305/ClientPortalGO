-- A booked visit. Each booking created one estimate; declining that estimate cancels the booking.
CREATE TABLE bookings (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    customer_id  BIGINT      NOT NULL REFERENCES customers (id),
    location_id  BIGINT      NOT NULL REFERENCES locations (id),
    service_id   BIGINT      NOT NULL REFERENCES services (id),
    estimate_id  BIGINT      NOT NULL REFERENCES billing_documents (id),
    starts_at    TIMESTAMPTZ NOT NULL,
    ends_at      TIMESTAMPTZ NOT NULL,
    cancelled_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT bookings_estimate_id_key UNIQUE (estimate_id),
    CONSTRAINT bookings_ends_after_starts CHECK (ends_at > starts_at),
    -- One visit at a time: live bookings never overlap. Ranges are half-open, so touching visits are fine.
    CONSTRAINT bookings_no_overlap
        EXCLUDE USING gist (tstzrange(starts_at, ends_at) WITH &&) WHERE (cancelled_at IS NULL)
);

CREATE INDEX bookings_customer_id_idx ON bookings (customer_id, starts_at);
CREATE INDEX bookings_location_id_idx ON bookings (location_id);
CREATE INDEX bookings_service_id_idx ON bookings (service_id);
