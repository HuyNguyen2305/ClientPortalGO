-- A booking is also the visit's work order: staff move it from scheduled to in progress to completed.
ALTER TABLE bookings
    ADD COLUMN status       TEXT NOT NULL DEFAULT 'scheduled',
    ADD COLUMN started_at   TIMESTAMPTZ,
    ADD COLUMN completed_at TIMESTAMPTZ,
    ADD COLUMN notes        TEXT,
    ADD CONSTRAINT bookings_status_check CHECK (status IN ('scheduled', 'in_progress', 'completed'));
