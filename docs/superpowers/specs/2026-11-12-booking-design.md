# Client Portal backend, slice 2: online booking

Date: 2026-11-12. Status: design approved section by section; awaiting review of this written spec.
Builds on slice 1: `docs/superpowers/specs/2026-11-11-billing-documents-design.md`.

## Purpose

A customer books a visit in three steps: pick a service, pick a day (seven at a time from
tomorrow, paged with Before and After), pick a time (15-minute slots). Admins define the
services. Confirming a booking creates an open estimate, so the slice 1 flow takes over from
there: accept, convert to invoice, pay.

## Decisions

- Confirming a booking creates an **open** estimate for the service.
- **One visit at a time:** a slot is offered only if it overlaps no other live booking,
  counting the service's duration.
- **Opening hours come from config:** the same every day, in one company timezone.
- A service has a **name, a duration and a price**, and admins create it.
- Clients send and receive **business-local** dates and times (`"2026-11-12"`, `"07:00"`); the
  server does all timezone conversion.
- Admin endpoints have no authentication yet, like the `X-Customer-ID` header; both are known
  shortcuts to replace with real auth before going public.

## 1. Data model

### `services` (migration 000006)

| Column | Type | Notes |
|---|---|---|
| `id` | `BIGINT` identity, PK | |
| `name` | `TEXT NOT NULL` | |
| `duration_minutes` | `INT NOT NULL` | CHECK between 15 and 480 and a multiple of 15 |
| `price_cents` | `BIGINT NOT NULL` | CHECK >= 0 |
| `created_at`, `updated_at` | `TIMESTAMPTZ NOT NULL` | default `now()` |

### `bookings` (migration 000007)

| Column | Type | Notes |
|---|---|---|
| `id` | `BIGINT` identity, PK | |
| `customer_id` | `BIGINT NOT NULL` FK → `customers` | indexed |
| `location_id` | `BIGINT NOT NULL` FK → `locations` | indexed |
| `service_id` | `BIGINT NOT NULL` FK → `services` | indexed; deleting a service with bookings is 409 |
| `estimate_id` | `BIGINT NOT NULL` FK → `billing_documents`, unique | the estimate this booking created |
| `starts_at`, `ends_at` | `TIMESTAMPTZ NOT NULL` | CHECK `ends_at > starts_at`; the end is fixed at booking time, so editing a service never moves existing visits |
| `cancelled_at` | `TIMESTAMPTZ` | set when cancelled; frees the slot |
| `created_at`, `updated_at` | `TIMESTAMPTZ NOT NULL` | default `now()` |

Constraint `bookings_no_overlap`:
`EXCLUDE USING gist (tstzrange(starts_at, ends_at) WITH &&) WHERE (cancelled_at IS NULL)`.
The ranges are half-open, so touching visits (09:00–09:30 and 09:30–10:00) are allowed. Two
concurrent overlapping bookings: the database accepts exactly one. No extension is needed.

## 2. Booking rules

Config (all optional, with defaults):

| Variable | Default | Meaning |
|---|---|---|
| `BUSINESS_TZ` | `America/Chicago` | the company timezone (a guess from the Texas sample data) |
| `BOOKING_OPEN` | `07:00` | first start time of the day |
| `BOOKING_CLOSE` | `17:00` | every visit must end by this time |
| `BOOKING_STEP_MINUTES` | `15` | slot grid |
| `BOOKING_HORIZON_DAYS` | `90` | the last bookable date is today + this many days |

- **Bookable dates:** from tomorrow through today + horizon, with "today" taken in
  `BUSINESS_TZ`.
- **Slots** for a service on a bookable date are the local times `open, open+step, …` where
  `start + duration <= close`, and `[start, start+duration)` overlaps no live booking. Local
  times stay local on a daylight-saving changeover day.
- **Confirming** (one transaction):
  1. Validate the date and time: a real date and an `HH:MM` time, on the grid, the whole visit
     inside opening hours, the date bookable.
  2. Check that the service exists and that the location belongs to the customer.
  3. Create an **open** estimate for that location, dated the visit day, with one line: the
     service name, quantity 1, the service price.
  4. Insert the booking. An overlap fails `bookings_no_overlap`; the transaction rolls back and
     the answer is 409.
- **Cancelling:** declining the estimate (`POST /estimates/:id/decline`) sets the booking's
  `cancelled_at` in the same transaction. Accepting or converting leaves the booking alone.
  There is no separate cancel endpoint and no rescheduling (decline and book again).

## 3. API

All routes under `/api/v1`, with slice 1's conventions for JSON, error bodies, `{"data": [...]}`
lists and status codes.

### Services (no `X-Customer-ID`)

| Method and path | Does |
|---|---|
| `GET /services` | all services, by name; not paginated |
| `GET /services/:id` | one service |
| `POST /services` | admin: create |
| `PUT /services/:id` | admin: replace name, duration, price |
| `DELETE /services/:id` | admin: delete; 409 if bookings use it |

Body: `{"name": "Bi-Monthly Service", "duration_minutes": 30, "price_cents": 6500}`. The name is
required (trimmed, at most 200 characters); the duration is a multiple of 15 between 15 and 480;
the price is from 0 to 10,000,000,000. Response: the same fields plus `id`.

### Day and time (`X-Customer-ID` required)

| Method and path | Does |
|---|---|
| `GET /booking/days?from=YYYY-MM-DD` | up to seven bookable dates starting at `from` (default: tomorrow), clipped to the bookable range |
| `GET /services/:id/slots?date=YYYY-MM-DD` | free start times for that service on that date |

```json
{"days": [{"date": "2026-11-13", "weekday": "Friday"}], "previous": "2026-11-06", "next": "2026-11-20"}
{"date": "2026-11-13", "slots": ["07:00", "07:15"]}
```

- `previous` is `from - 7 days`, clipped to tomorrow, and absent when `from` is tomorrow.
- `next` is `from + 7 days`, absent when that is past the horizon.
- A `from` or `date` outside the bookable range is 400. A full day gives `"slots": []`.

### Bookings (`X-Customer-ID` required)

| Method and path | Does |
|---|---|
| `POST /bookings` | confirm; 201 with the booking |
| `GET /bookings` | the customer's live bookings that have not ended, soonest first; not paginated |
| `GET /bookings/:id` | one of the customer's bookings, cancelled or not |

Request: `{"service_id": 1, "location_id": 3, "date": "2026-11-13", "time": "07:00"}`.

Response:

```json
{
  "id": 8,
  "service": {"id": 1, "name": "Bi-Monthly Service", "duration_minutes": 30, "price_cents": 6500},
  "location": {"id": 3, "street": "West Loop 1604 South", "city": "San Antonio", "state": "TX", "postal_code": "12341"},
  "date": "2026-11-13", "time": "07:00",
  "starts_at": "2026-11-13T07:00:00-06:00", "ends_at": "2026-11-13T07:30:00-06:00",
  "cancelled": false,
  "estimate": {"id": 41, "number": 5004, "status": "open", "total_cents": 6500}
}
```

The `service` object shows the service as it is now; `starts_at` and `ends_at` show what was
booked.

| Code | When |
|---|---|
| 400 | bad date or time format; off the grid; outside opening hours or past closing; a date outside the bookable range |
| 404 | the service is missing; the location is missing or another customer's; the booking is another customer's |
| 409 | the slot was taken meanwhile |

Existing endpoint change: `POST /estimates/:id/decline` also cancels the linked booking.

## 4. Code layout

```
migrations/000006_create_services.{up,down}.sql
migrations/000007_create_bookings.{up,down}.sql
internal/config/config.go             + the five BOOKING/BUSINESS settings, as model.Hours
internal/model/service.go             Service
internal/model/booking.go             Booking, Hours, Interval; pure rules: BookableRange,
                                      Days, ParseSlot, FreeSlots
internal/repository/service.go        Services: CRUD
internal/repository/booking.go        booking SQL as methods on BillingDocuments (shares its WithTx)
internal/repository/errors.go         + 23P01 (exclusion violation) → ErrConflict
internal/service/catalog.go           Catalog: service CRUD
internal/service/booking.go           BookingService: Days, Slots, Book, List, Get
internal/service/billing_document.go  Transition: declining an estimate cancels its booking
internal/dto/service.go, internal/dto/booking.go
internal/handler/service.go, internal/handler/booking.go, router.go
internal/app/app.go                   app.New(pool, cfg, log)
```

- Bookings are written in the same transaction as their estimate, so their SQL is on the
  existing `BillingDocuments` repository, reached through its `WithTx`.
- The slot arithmetic is pure functions with no clock or database, so every case is a
  table test.
- The service-layer type for admin services is `service.Catalog`, to avoid
  `service.ServiceService`.
- The clock is injected as `now func() time.Time`.

## 5. Tests

- **Unit, model:** the bookable range around midnight in the business timezone; day paging and
  its clipping; date and time parsing (formats, grid, hours, past closing); free slots (empty
  day, a 45-minute booking blocking three starts, a booking blocking a start only through
  overlap, touching visits, the last slot that fits); a daylight-saving changeover day.
- **Unit, service:** Book with mocked repositories (unknown service or location is 404; the
  estimate is open with the service name and price; a conflict passes through); declining an
  estimate cancels its booking.
- **Unit, handler:** binding and validation failures for services and bookings.
- **Integration:**
  - the exclusion constraint;
  - service CRUD and delete blocked by a booking;
  - the full path (service, days, slots, book, estimate open, accept, convert, pay);
  - booked and overlapping slots disappear;
  - concurrent booking of one slot: one 201, one 409;
  - decline frees the slot;
  - 400 cases;
  - another customer's location is 404;
  - day paging edges.
- Done means `task test`, `task test:integration` and `task check` pass.

## Out of scope

Rescheduling; several crews; per-weekday hours and holidays; admin authentication; technicians,
travel time, reminders; work orders.
