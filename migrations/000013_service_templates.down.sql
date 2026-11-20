ALTER TABLE bookings DROP COLUMN IF EXISTS series_id;
DROP TABLE IF EXISTS booking_series;
DROP TABLE IF EXISTS service_items;
DROP INDEX IF EXISTS services_status_idx;
ALTER TABLE services
    DROP COLUMN IF EXISTS color,
    DROP COLUMN IF EXISTS status,
    DROP COLUMN IF EXISTS repeat_unit,
    DROP COLUMN IF EXISTS repeat_every,
    DROP COLUMN IF EXISTS repeat_by,
    DROP COLUMN IF EXISTS ends_kind,
    DROP COLUMN IF EXISTS ends_after,
    DROP COLUMN IF EXISTS ends_on,
    DROP COLUMN IF EXISTS set_confirmed;
