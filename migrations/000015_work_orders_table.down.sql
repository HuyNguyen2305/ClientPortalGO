ALTER TABLE bookings
    ADD COLUMN status       TEXT NOT NULL DEFAULT 'scheduled',
    ADD COLUMN started_at   TIMESTAMPTZ,
    ADD COLUMN completed_at TIMESTAMPTZ,
    ADD COLUMN notes        TEXT,
    ADD CONSTRAINT bookings_status_check CHECK (status IN ('scheduled', 'in_progress', 'completed'));

UPDATE bookings b
SET status = w.status, started_at = w.started_at, completed_at = w.completed_at, notes = w.notes
FROM work_orders w
WHERE w.booking_id = b.id;

DROP TABLE work_orders;
