-- Services customers can book; created by admins.
CREATE TABLE services (
    id               BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name             TEXT        NOT NULL,
    duration_minutes INT         NOT NULL,
    price_cents      BIGINT      NOT NULL CHECK (price_cents >= 0),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- A visit always fills whole 15-minute slots.
    CONSTRAINT services_duration_check
        CHECK (duration_minutes BETWEEN 15 AND 480 AND duration_minutes % 15 = 0)
);
