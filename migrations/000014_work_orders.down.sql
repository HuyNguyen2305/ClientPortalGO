ALTER TABLE bookings
    DROP COLUMN IF EXISTS notes,
    DROP COLUMN IF EXISTS completed_at,
    DROP COLUMN IF EXISTS started_at,
    DROP COLUMN IF EXISTS status;
