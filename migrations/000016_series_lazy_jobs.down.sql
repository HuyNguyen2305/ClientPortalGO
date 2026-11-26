DROP INDEX IF EXISTS booking_series_due_idx;
ALTER TABLE booking_series DROP COLUMN IF EXISTS ended_at, DROP COLUMN IF EXISTS generated_through;
