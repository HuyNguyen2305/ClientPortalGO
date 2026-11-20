-- Services become templates: repeat rule, color, lifecycle status and invoice line items.
ALTER TABLE services
    ADD COLUMN color         TEXT,
    ADD COLUMN status        TEXT    NOT NULL DEFAULT 'active',
    ADD COLUMN repeat_unit   TEXT    NOT NULL DEFAULT 'off',
    ADD COLUMN repeat_every  INT     NOT NULL DEFAULT 1,
    ADD COLUMN repeat_by     TEXT,
    ADD COLUMN ends_kind     TEXT    NOT NULL DEFAULT 'never',
    ADD COLUMN ends_after    INT,
    ADD COLUMN ends_on       DATE,
    ADD COLUMN set_confirmed BOOLEAN NOT NULL DEFAULT true,
    ADD CONSTRAINT services_color_check CHECK (color IS NULL OR color ~ '^#[0-9A-Fa-f]{6}$'),
    ADD CONSTRAINT services_status_check CHECK (status IN ('active', 'archived', 'deleted')),
    ADD CONSTRAINT services_repeat_unit_check CHECK (repeat_unit IN ('off', 'daily', 'weekly', 'monthly', 'yearly')),
    ADD CONSTRAINT services_repeat_every_check CHECK (repeat_every >= 1),
    ADD CONSTRAINT services_repeat_by_check CHECK (
        repeat_by IS NULL OR (repeat_unit = 'monthly' AND repeat_by IN ('weekday', 'monthday'))),
    ADD CONSTRAINT services_ends_kind_check CHECK (ends_kind IN ('never', 'after', 'on_date')),
    ADD CONSTRAINT services_ends_check CHECK (
        (ends_kind = 'after'   AND ends_after IS NOT NULL AND ends_after >= 1 AND ends_on IS NULL) OR
        (ends_kind = 'on_date' AND ends_on IS NOT NULL AND ends_after IS NULL) OR
        (ends_kind = 'never'   AND ends_after IS NULL AND ends_on IS NULL));

CREATE INDEX services_status_idx ON services (status);

-- Invoice line items a template copies onto each job's estimate.
CREATE TABLE service_items (
    id               BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    service_id       BIGINT NOT NULL REFERENCES services (id) ON DELETE CASCADE,
    position         INT    NOT NULL,
    name             TEXT   NOT NULL,
    description      TEXT,
    quantity         INT    NOT NULL CHECK (quantity > 0),
    unit_price_cents BIGINT NOT NULL CHECK (unit_price_cents >= 0)
);

CREATE INDEX service_items_service_id_idx ON service_items (service_id);

-- One customer's recurring booking of a template; its jobs are bookings with this series_id.
CREATE TABLE booking_series (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    customer_id     BIGINT      NOT NULL REFERENCES customers (id),
    location_id     BIGINT      NOT NULL REFERENCES locations (id),
    service_id      BIGINT      NOT NULL REFERENCES services (id),
    first_starts_at TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX booking_series_customer_id_idx ON booking_series (customer_id);
CREATE INDEX booking_series_location_id_idx ON booking_series (location_id);
CREATE INDEX booking_series_service_id_idx ON booking_series (service_id);

ALTER TABLE bookings ADD COLUMN series_id BIGINT REFERENCES booking_series (id);
CREATE INDEX bookings_series_id_idx ON bookings (series_id);
