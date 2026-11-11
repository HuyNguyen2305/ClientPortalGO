# Client Portal backend, slice 1: invoices and estimates

Date: 2026-11-11. Status: design approved section by section; awaiting review of this written spec.

## Purpose

A Go backend that showcases a complete API for a service-company client portal: what the service
does for a customer, without a login or a frontend yet. Slice 1 covers customers, locations,
invoices and estimates. Work orders, documents, payment options, login, PDF generation and gRPC
are later slices, each with its own spec.

## Decisions

- The whole Postgres schema is designed here as migrations. They run once a database is provided.
- REST only, on Gin. gRPC waits for a slice that needs it.
- No auth yet. The caller names the customer in an `X-Customer-ID` header. This is a known
  shortcut: any caller can read any customer's data, so the service must not be exposed publicly
  until real auth replaces the header.
- Single company; no tenant column.
- Full API: CRUD, status transitions, record payment, convert estimate to invoice, balance.
- Invoices and estimates share one table (`billing_documents`, column `parent_type`) and one code
  stack, routed as `/api/v1/invoices` and `/api/v1/estimates`.
- Money is integer cents (`BIGINT`) in the database and in JSON.
- A dev seed script supplies sample data.
- Stack and layering follow `CLAUDE.md`.

## 1. Database schema

Every table has `created_at` and `updated_at`, both `TIMESTAMPTZ NOT NULL`.

### `customers`

| Column | Type | Notes |
|---|---|---|
| `id` | `BIGINT` identity, PK | |
| `account_number` | `BIGINT NOT NULL`, unique | the account number printed on documents; from a sequence starting at 5000 |
| `name` | `TEXT NOT NULL` | |
| `email`, `phone` | `TEXT` | optional |

### `locations`

A customer's service addresses. They drive the "Filter by Locations" dropdown.

| Column | Type | Notes |
|---|---|---|
| `id` | `BIGINT` identity, PK | |
| `customer_id` | `BIGINT NOT NULL` FK → `customers` | indexed |
| `street`, `city`, `state`, `postal_code` | `TEXT NOT NULL` | |

### `billing_documents`

| Column | Type | Notes |
|---|---|---|
| `id` | `BIGINT` identity, PK | |
| `parent_type` | `TEXT NOT NULL` | `'invoice'` or `'estimate'` (CHECK) |
| `number` | `BIGINT NOT NULL` | display number, unique per `parent_type`. Two sequences: invoices from 85000, estimates from 5000 |
| `customer_id` | `BIGINT NOT NULL` FK → `customers` | |
| `location_id` | `BIGINT NOT NULL` FK → `locations` | indexed |
| `title` | `TEXT NOT NULL` | e.g. "Bi-Monthly Service" |
| `status` | `TEXT NOT NULL` | a CHECK ties the allowed values to `parent_type` |
| `document_date` | `DATE NOT NULL` | |
| `total_cents` | `BIGINT NOT NULL DEFAULT 0` | recomputed from line items in the same transaction that changes them |
| `amount_paid_cents` | `BIGINT NOT NULL DEFAULT 0` | invoices only; updated with each payment in the same transaction |
| `converted_from_id` | `BIGINT` FK → `billing_documents` | set on an invoice created from an estimate |

Balance due is `total_cents - amount_paid_cents`. It is computed, not stored.

### `billing_document_items`

| Column | Type | Notes |
|---|---|---|
| `id` | `BIGINT` identity, PK | |
| `billing_document_id` | `BIGINT NOT NULL` FK, `ON DELETE CASCADE` | indexed |
| `position` | `INT NOT NULL` | display order |
| `name` | `TEXT NOT NULL` | |
| `description` | `TEXT` | |
| `quantity` | `INT NOT NULL`, CHECK > 0 | |
| `unit_price_cents` | `BIGINT NOT NULL`, CHECK >= 0 | |

A line total is `quantity * unit_price_cents`. It is computed, not stored.

### `payments`

| Column | Type | Notes |
|---|---|---|
| `id` | `BIGINT` identity, PK | |
| `billing_document_id` | `BIGINT NOT NULL` FK | indexed; the service ensures it is an invoice |
| `amount_cents` | `BIGINT NOT NULL`, CHECK > 0 | |
| `paid_at` | `TIMESTAMPTZ NOT NULL` | |

### Indexes

- `billing_documents (customer_id, parent_type, document_date DESC, id DESC)` for the list query
  and its keyset pagination.
- `billing_documents (parent_type, number)`, unique.
- `billing_documents (converted_from_id)`, unique where not null, so an estimate converts once.
- A plain index on every other foreign key.

### Migrations

One table per file, each with `up` and `down`: `000001_create_customers`,
`000002_create_locations`, `000003_create_billing_documents`,
`000004_create_billing_document_items`, `000005_create_payments`.

## 2. Statuses

### Invoice

| Status | Meaning |
|---|---|
| `draft` | being prepared; the only state in which it can be edited or deleted |
| `open` | sent to the customer, nothing paid |
| `partially_paid` | some payment recorded, balance remains |
| `paid` | balance is zero |
| `void` | cancelled; excluded from the balance |

Transitions:

- `draft → open` on send.
- `open → partially_paid → paid`, driven only by recorded payments.
- `draft → void` and `open → void`. An invoice with payments cannot be voided.

### Estimate

| Status | Meaning |
|---|---|
| `draft` | being prepared; the only state in which it can be edited or deleted |
| `open` | sent, awaiting the customer's answer |
| `accepted` | the customer accepted |
| `declined` | the customer declined |

Transitions:

- `draft → open` on send.
- `open → accepted` and `open → declined`.

An `accepted` estimate can be converted once into a new `draft` invoice that copies its title,
location and line items, with today's date. The estimate stays `accepted`.

The service layer enforces all transitions. An invalid one answers HTTP 409.

## 3. API

All routes are under `/api/v1` and speak JSON. Every route except `/healthz` and `/customers`
requires `X-Customer-ID` and touches only that customer's rows. A record that belongs to another
customer answers 404, the same as one that does not exist.

### Customers (no header)

| Method and path | Does |
|---|---|
| `POST /customers` | create; `account_number` is assigned |
| `GET /customers` | list, keyset-paginated by `id` |
| `GET /customers/:id` | one customer |
| `PUT /customers/:id` | update name, email, phone |
| `DELETE /customers/:id` | delete; 409 if the customer has locations or documents |

### Locations

| Method and path | Does |
|---|---|
| `POST /locations` | create for the calling customer |
| `GET /locations` | list all; not paginated, because a customer has few |
| `GET /locations/:id` | one location |
| `PUT /locations/:id` | update the address |
| `DELETE /locations/:id` | delete; 409 if documents reference it |

### Invoices and estimates

Shared by `/invoices` and `/estimates`:

| Method and path | Does |
|---|---|
| `GET /` | list, newest first. Query: `location_id`, `status`, `limit` (default 20, max 100), `cursor` |
| `POST /` | create as `draft` with its line items; `number` is assigned |
| `GET /:id` | detail with line items and totals |
| `PUT /:id` | replace title, date, location and line items; `draft` only |
| `DELETE /:id` | delete; `draft` only |
| `POST /:id/send` | `draft → open` |

Invoices only:

| Method and path | Does |
|---|---|
| `POST /invoices/:id/void` | `draft` or `open → void`; 409 if payments exist |
| `POST /invoices/:id/payments` | record a payment on an `open` or `partially_paid` invoice; status becomes `partially_paid` or `paid` |
| `GET /invoices/:id/payments` | list the invoice's payments |

Estimates only:

| Method and path | Does |
|---|---|
| `POST /estimates/:id/accept` | `open → accepted` |
| `POST /estimates/:id/decline` | `open → declined` |
| `POST /estimates/:id/convert` | `accepted` estimate → new `draft` invoice; answers 201 with the invoice; 409 if already converted |

### Balance and health

| Method and path | Does |
|---|---|
| `GET /balance` | `{"balance_cents": n}`: the sum of balance due over `open` and `partially_paid` invoices. Query: `location_id` |
| `GET /healthz` | 200 when the database answers a ping |

### Shapes

Create or update body, for an invoice or an estimate. All fields are required except
`description`; `items` needs at least one entry; `location_id` must belong to the calling customer.

```json
{
  "location_id": 3,
  "title": "Bi-Monthly Service",
  "document_date": "2026-12-10",
  "items": [
    {"name": "Bi-Monthly Service", "description": "General maintenance", "quantity": 1, "unit_price_cents": 6500}
  ]
}
```

Detail response. List rows are the same without `items`.

```json
{
  "id": 12, "number": 85000, "status": "paid", "title": "Bi-Monthly Service",
  "document_date": "2026-12-10",
  "location": {"id": 3, "street": "West Loop 1604 South", "city": "San Antonio", "state": "TX", "postal_code": "12341"},
  "items": [{"id": 40, "name": "Bi-Monthly Service", "description": "General maintenance", "quantity": 1, "unit_price_cents": 6500, "total_cents": 6500}],
  "subtotal_cents": 6500, "total_cents": 6500,
  "amount_paid_cents": 6500, "balance_due_cents": 0
}
```

- Estimates omit `amount_paid_cents` and `balance_due_cents`.
- `subtotal_cents` equals `total_cents` until tax or discounts exist.
- The location is joined in the list query, so a list is one SQL statement.
- Paginated list: `{"data": [...], "next_cursor": "..."}`. `next_cursor` is absent on the last
  page. The cursor is an opaque encoding of the last row's `(document_date, id)`.
- Payment body: `{"amount_cents": 6500}`. It must be positive and no more than the balance due.
- Error body, for every failure: `{"error": {"code": "invalid_transition", "message": "..."}}`.

### Status codes

| Code | When |
|---|---|
| 200 / 201 / 204 | read or action / create / delete |
| 400 | malformed JSON, failed validation, missing or non-numeric `X-Customer-ID`, bad cursor |
| 404 | record missing or owned by another customer; unknown customer in the header |
| 409 | invalid status transition, edit or delete of a non-draft, overpayment, delete blocked by references |
| 500 | anything unexpected; logged, and a generic message is returned |

## 4. Code layout

```
cmd/api/main.go                     wiring, http.Server, signal.NotifyContext shutdown
internal/config/config.go           env: DATABASE_URL, PORT, DB_MAX_CONNS, QUERY_TIMEOUT
internal/model/                     customer.go, location.go, billing_document.go, errors.go
internal/dto/                       customer.go, location.go, billing_document.go, page.go, error.go
internal/repository/db/dbtx.go      DBTX interface (satisfied by pgxpool.Pool and pgx.Tx)
internal/repository/                customer.go, location.go, billing_document.go, payment.go,
                                    lookups.go (location dropdown list), tx.go (WithTx)
internal/service/                   customer.go, location.go, billing_document.go
internal/handler/                   router.go, customer.go, location.go, billing_document.go,
                                    health.go, errors.go
internal/middleware/                customer.go (X-Customer-ID), logging.go (slog)
migrations/                         the five up/down pairs
seeds/dev.sql                       sample customer, locations, invoices, estimates
tests/unit/{service,handler}/       table-driven, mockery mocks
tests/integration/{migrations,customers,locations,billing_documents}/
tools/go.mod                        task, golangci-lint, mockery, migrate as Go tools
Taskfile.yml, .golangci.yml, .mockery.yaml, .env.example, .gitignore
.claude/settings.json               the gofmt + golangci-lint hook that CLAUDE.md describes
```

- `model/billing_document.go` holds `ParentType`, the status constants and one transition table
  (`map[ParentType]map[Status][]Status`). Every status change goes through a single `CanTransition`.
- The service package declares the repository interfaces it needs; mockery generates mocks from them.
- One `BillingDocumentHandler` is constructed twice in `router.go`, once per `ParentType`. The
  invoice-only and estimate-only routes are registered only on their own group.
- Recovery uses Gin's built-in `gin.Recovery()`. CORS is left out until a browser frontend exists.

## 5. Concurrency

Gin runs each request on its own goroutine, and all of them share one `pgxpool`. Beyond that,
concurrency is used where it changes behaviour or latency, and nowhere else.

- **Parallel reads.** The detail endpoint fetches the document and its line items at the same time
  with `errgroup`. Lists are a single JOIN and need none.
- **Safe concurrent writes.** Recording a payment locks the invoice row with `SELECT ... FOR UPDATE`
  inside the transaction, so two simultaneous payments cannot overpay. A double convert is stopped
  by the unique index on `converted_from_id`. Numbers come from sequences, which never collide.
- **Timeouts and cancellation.** `c.Request.Context()` flows through every layer, and the
  repository wraps each query in `context.WithTimeout`. A client that disconnects cancels its query.
- **Shutdown.** `signal.NotifyContext`, then `http.Server.Shutdown`, drains in-flight requests
  before the pool closes.

No goroutines run inside a transaction, because a `pgx.Tx` is not safe for concurrent use.

## 6. Errors

- `model/errors.go` defines `ErrNotFound`, `ErrConflict` and `ErrValidation`. Services wrap them
  with context, for example `fmt.Errorf("record payment: %w", err)`.
- Repositories translate `pgx.ErrNoRows` to `ErrNotFound`, and Postgres foreign-key and unique
  violations (`23503`, `23505`) to `ErrConflict`.
- `handler/errors.go` is the only place that maps errors to HTTP codes and writes the error body.
  Unknown errors are logged with `slog` and answered with a generic 500.

## 7. Tests

- **Unit, `tests/unit/service/`.** The transition table (every allowed and refused pair for both
  types), payment arithmetic and the resulting status, total recomputation, convert, and cursor
  encode/decode, all with mocked repositories.
- **Unit, `tests/unit/handler/`.** Binding and validation failures, the header middleware, and
  error mapping.
- **Integration, tagged `//go:build integration`.** A helper creates a throwaway database from
  `TEST_POSTGRES_URL`, runs the migrations and drops it afterwards. Cases:
  - migrations apply `up` then `down`; constraints reject a wrong status for a type, a duplicate
    number and a second conversion of one estimate;
  - every endpoint through `httptest`;
  - the full path: create estimate → send → accept → convert → send invoice → partial payment →
    final payment → balance returns to zero;
  - each 409 rule;
  - cross-customer access answers 404;
  - pagination across a page boundary;
  - two concurrent payments that together would overpay: exactly one succeeds.
- Done means `task test`, `task test:integration` and `task check` all pass. Integration tests need
  a database; until one is provided, only unit tests and `task check` can run.

## 8. Build order

1. Tooling: `go.mod`, `tools/go.mod`, Taskfile, lint config, hook, `.gitignore`.
2. Migrations and seed.
3. Config, pool, `DBTX`, `WithTx`, health endpoint, `main.go`.
4. Customers, then locations: repository → service → handler, tests alongside.
5. Billing documents: CRUD and lists, then transitions, payments, convert, balance.

## Out of scope

- Tax, discounts and due dates, and therefore an "overdue" status.
- "Available Credits" on the invoice detail, the "Pay" action and payment methods.
- Soft delete: only drafts can be deleted, and they are removed outright.
- Login, PDF generation, gRPC, work orders, documents.
