-- A series now places each job when its time comes. generated_through is the last date whose job was
-- already placed (or skipped); ended_at is set when the customer cancels the series or its rule runs out.
ALTER TABLE booking_series
    ADD COLUMN generated_through DATE,
    ADD COLUMN ended_at          TIMESTAMPTZ;

-- Existing series already hold their jobs: continue after the last of them.
UPDATE booking_series s
SET generated_through = COALESCE(
    (SELECT max((b.starts_at AT TIME ZONE 'UTC')::date) FROM bookings b WHERE b.series_id = s.id),
    (s.first_starts_at AT TIME ZONE 'UTC')::date - 1);

ALTER TABLE booking_series ALTER COLUMN generated_through SET NOT NULL;

CREATE INDEX booking_series_due_idx ON booking_series (generated_through) WHERE ended_at IS NULL;
