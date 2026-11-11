# Invoices and Estimates Backend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Gin + Postgres REST API for customers, locations, invoices and estimates, with status
transitions, payments, estimate-to-invoice conversion and an account balance.

**Architecture:** handler → service → repository, dependencies pointing inward. Invoices and
estimates share the `billing_documents` table and one code stack parameterised by `ParentType`.
Services own the rules and run multi-statement work through a repository `WithTx` callback.

**Tech Stack:** Go 1.26+ (1.27.2 installed), Gin, pgx/v5 + pgxpool, golang-migrate, `log/slog`,
`golang.org/x/sync/errgroup`, testify, mockery v3, golangci-lint v2, go-task.

**Spec:** `docs/superpowers/specs/2026-11-11-billing-documents-design.md`. Read it with this plan;
table columns, routes, JSON shapes and status codes are defined there and not repeated here.

## Global Constraints

- Follow `CLAUDE.md`: no SQL in handlers, no Gin in services, parameterised SQL only, errors wrapped
  with `fmt.Errorf("...: %w", err)`, smallest change that solves the task (Ponytail, level full).
- Module path: `clientportal`. `go.mod` says `go 1.26`.
- Money is `int64` cents everywhere. No floats.
- All routes under `/api/v1`; `/healthz` at the root.
- Every repository method starts with `ctx, cancel := context.WithTimeout(ctx, r.timeout); defer cancel()`.
- Every customer-scoped query filters by `customer_id`; another customer's row answers 404.
- Error body is always `{"error": {"code": "...", "message": "..."}}` with codes `invalid_request`
  (400), `not_found` (404), `conflict` (409), `internal` (500).
- Unit tests live in `tests/unit/<package>/`, integration tests in `tests/integration/<feature>/`
  with `//go:build integration`. Mocks are generated into `tests/unit/mocks/`, never hand-written.
- Tools run as `go tool -modfile=tools/go.mod <tool>`. Shorthand below: `task X` means
  `go tool -modfile=tools/go.mod task X`.
- Integration steps need `TEST_POSTGRES_URL` (a Postgres 18 superuser URL). If it is not set, do
  not fake the result: mark the step "not run" in the task report and continue.
- Each task ends with `task check` clean and a commit. Commit messages end with the trailer
  `Co-Authored-By: Claude Fable 5.2 <noreply@anthropic.com>`.

## Deviations from the spec (decided while planning)

- **`internal/app/app.go` is added.** It builds repositories, services and the router from a pool,
  so `main.go` and the integration tests share one wiring path.
- **`cmd/seed/main.go` is added.** `psql` is not installed on the dev machine, so `task seed` runs
  `seeds/dev.sql` through pgx.
- **Error codes** are the four generic ones above; the spec's `invalid_transition` was an example.
- **Unknown customer in the header** is detected by one lookup per request in the middleware,
  marked `shortcut:` (it disappears when real auth replaces the header).
- **`/healthz` answers 503** when the ping fails.

## Review Focus

Inputs the spec implies but does not spell out. Each has a test in the task named.

1. Non-numeric, zero or negative `:id`, `limit`, `location_id` or `X-Customer-ID` → 400, never 500 (Tasks 5, 6, 7).
2. Line items large enough to overflow `int64` totals → 400. Limits: at most 100 items, `quantity` ≤ 1,000,000, `unit_price_cents` ≤ 10,000,000,000 (Task 7).
3. `document_date` that is not a real `YYYY-MM-DD` date, and whitespace-only `title` or `name` → 400 (Tasks 5, 7).
4. `status` filter that is unknown or belongs to the other document type → 400, not an empty list (Task 7).
5. Payment against a `draft`, `paid` or `void` invoice, or an estimate ID on an invoice route → 409 and 404 respectively (Tasks 8, 9).

## File Structure

```
cmd/api/main.go                       load config, open pool, app.New, serve, graceful shutdown
cmd/seed/main.go                      execute seeds/dev.sql against DATABASE_URL
internal/config/config.go             Load() from env
internal/app/app.go                   New(pool, timeout, log) http.Handler
internal/model/errors.go              ErrNotFound, ErrConflict, ErrValidation
internal/model/customer.go            Customer
internal/model/location.go            Location
internal/model/billing_document.go    ParentType, Status, BillingDocument, Item, Payment, DocumentInput,
                                      DocumentFilter, CanTransition, ValidStatus, SumItems, StatusAfterPayment
internal/model/cursor.go              DocumentCursor, Encode, DecodeCursor
internal/dto/{error,page,customer,location,billing_document}.go
internal/repository/db/dbtx.go        DBTX
internal/repository/errors.go         mapErr (pgx.ErrNoRows, 23503, 23505)
internal/repository/customer.go       Customers
internal/repository/location.go       Locations (Create, Get, Update, Delete)
internal/repository/lookups.go        Locations.List
internal/repository/billing_document.go  BillingDocuments (documents and items)
internal/repository/payment.go        BillingDocuments.AddPayment, ListPayments, Balance
internal/repository/tx.go             BillingDocuments.WithTx
internal/service/{customer,location,billing_document}.go   services and the repository interfaces they need
internal/handler/{router,errors,request,health,customer,location,billing_document}.go
internal/middleware/{logging,customer}.go
migrations/00000N_*.{up,down}.sql     five pairs
seeds/dev.sql
tests/unit/{config,model,service,handler,mocks}/
tests/integration/{testdb,testapp,migrations,health,customers,locations,billing_documents}/
tools/go.mod, Taskfile.yml, .golangci.yml, .mockery.yaml, .env.example, .gitignore
.claude/settings.json, .claude/hooks/go-format.sh
```

---

### Task 1: Tooling, config and first green check

**Files:**
- Create: `go.mod`, `tools/go.mod`, `Taskfile.yml`, `.golangci.yml`, `.mockery.yaml`, `.env.example`, `.gitignore`, `.claude/hooks/go-format.sh`, `internal/config/config.go`
- Modify: `.claude/settings.json` (keep the existing `enabledPlugins` block)
- Test: `tests/unit/config/config_test.go`

**Interfaces:**
- Produces: `config.Config{DatabaseURL string; Port string; DBMaxConns int32; QueryTimeout time.Duration}` and `config.Load() (Config, error)`.

- [ ] **Step 1: Create the modules and tools.** `go mod init clientportal`; in `tools/`, `go mod init clientportal/tools` then `go get -tool` for `github.com/go-task/task/v3/cmd/task`, `github.com/golangci/golangci-lint/v2/cmd/golangci-lint`, `github.com/vektra/mockery/v3`, `github.com/golang-migrate/migrate/v4/cmd/migrate`.
  Verify: `go tool -modfile=tools/go.mod task --version` prints a version.

- [ ] **Step 2: Write `Taskfile.yml`** with these tasks:

  | Task | Command |
  |---|---|
  | `test` | `go test ./tests/unit/...` |
  | `test:integration` | `go test -tags integration -p 1 ./tests/integration/...` |
  | `check` | fail if `gofmt -l .` prints anything; `go vet -tags integration ./...`; `go tool -modfile=tools/go.mod golangci-lint run --build-tags integration ./...` |
  | `mocks` | `go tool -modfile=tools/go.mod mockery` |
  | `migrate:up` / `migrate:down` | `GOFLAGS=-tags=postgres,file go tool -modfile=tools/go.mod migrate -path migrations -database "$DATABASE_URL" up` (or `down 1`) |
  | `seed` | `go run ./cmd/seed` |
  | `run` | `go run ./cmd/api` |

  `dotenv: ['.env']` at the top. `.env.example` lists `DATABASE_URL`, `TEST_POSTGRES_URL`, `PORT=8080`, `DB_MAX_CONNS=10`, `QUERY_TIMEOUT=5s`. `.gitignore` has `.env` and `/bin/`.

- [ ] **Step 3: Write `.golangci.yml`** (`version: "2"`, default linters plus `errorlint` and `sqlclosecheck`) and `.mockery.yaml` (package `clientportal/internal/service`, all interfaces, output dir `tests/unit/mocks`, package name `mocks`, expecter style).

- [ ] **Step 4: Write the failing config test**

```go
func TestLoad(t *testing.T) {
    tests := []struct {
        name    string
        env     map[string]string
        want    config.Config
        wantErr bool
    }{
        {"defaults", map[string]string{"DATABASE_URL": "postgres://x"},
            config.Config{DatabaseURL: "postgres://x", Port: "8080", DBMaxConns: 10, QueryTimeout: 5 * time.Second}, false},
        {"overrides", map[string]string{"DATABASE_URL": "postgres://x", "PORT": "9000", "DB_MAX_CONNS": "25", "QUERY_TIMEOUT": "2s"},
            config.Config{DatabaseURL: "postgres://x", Port: "9000", DBMaxConns: 25, QueryTimeout: 2 * time.Second}, false},
        {"missing database url", map[string]string{}, config.Config{}, true},
        {"bad max conns", map[string]string{"DATABASE_URL": "postgres://x", "DB_MAX_CONNS": "many"}, config.Config{}, true},
        {"bad timeout", map[string]string{"DATABASE_URL": "postgres://x", "QUERY_TIMEOUT": "soon"}, config.Config{}, true},
    }
    // each case: t.Setenv every key (unset ones to ""), call config.Load, compare
}
```

- [ ] **Step 5: Run `task test`.** Expected: FAIL, `config` package does not exist.
- [ ] **Step 6: Implement `config.Load`.** An empty env value means "use the default".
- [ ] **Step 7: Run `task test`.** Expected: PASS.

- [ ] **Step 8: Add the formatting hook.** `.claude/hooks/go-format.sh` reads the hook JSON on stdin, extracts `tool_input.file_path` (with `sed`; `jq` is not installed), and exits 0 unless the path ends in `.go`. For a `.go` file it runs `gofmt -w` on it, then `golangci-lint run` on that file's package directory, prints only the lines that mention the edited file to stderr, and exits 2 if there are any. In `.claude/settings.json` add a `PostToolUse` hook with matcher `Edit|Write` running `bash .claude/hooks/go-format.sh`, leaving `enabledPlugins` untouched.
  Verify: `echo '{"tool_input":{"file_path":"internal/config/config.go"}}' | bash .claude/hooks/go-format.sh; echo $?` prints `0`.

- [ ] **Step 9: Run `task check`.** Expected: no output from gofmt, vet or lint.
- [ ] **Step 10: Commit** everything, including the so-far untracked `CLAUDE.md` and `.claude/`: `chore: add tooling, config and formatting hook`.

---

### Task 2: Migrations, test database helper, seed

**Files:**
- Create: `migrations/000001_create_customers.{up,down}.sql` … `000005_create_payments.{up,down}.sql`, `seeds/dev.sql`, `cmd/seed/main.go`, `tests/integration/testdb/testdb.go`
- Test: `tests/integration/migrations/migrations_test.go`

**Interfaces:**
- Consumes: `config.Load`.
- Produces: `testdb.New(t *testing.T) *pgxpool.Pool` (tagged `integration`): `t.Fatal` with a clear message if `TEST_POSTGRES_URL` is unset; creates database `test_<random>`, applies all `up` migrations with the golang-migrate library (file source, pgx/v5 driver), returns a pool on it, and registers a cleanup that closes the pool and drops the database. `testdb.MigrateDown(t, pool)` runs all `down` migrations.
- Produces the schema in spec section 1, with these names fixed: sequences `customer_account_number_seq` (start 5000), `invoice_number_seq` (start 85000), `estimate_number_seq` (start 5000); constraints `billing_documents_parent_type_check`, `billing_documents_status_check`, unique indexes `billing_documents_parent_type_number_key` and `billing_documents_converted_from_id_key`.

- [ ] **Step 1: Write the failing test** `TestMigrations` with subtests, each inserting through `pool.Exec` and asserting on the Postgres error code (`errors.As` to `*pgconn.PgError`):

  | Subtest | Insert | Expect |
  |---|---|---|
  | valid rows | customer, location, one invoice `paid`, one estimate `accepted` | no error |
  | status of the wrong type | invoice with status `accepted`; estimate with status `paid` | code `23514` each |
  | unknown parent type | `parent_type = 'quote'` | `23514` |
  | duplicate number | two invoices with number 90000 | `23505` |
  | same number across types | invoice 90001 and estimate 90001 | no error |
  | second conversion | two invoices with the same `converted_from_id` | `23505` |
  | bad item | `quantity = 0`; `unit_price_cents = -1` | `23514` each |
  | bad payment | `amount_cents = 0` | `23514` |
  | item cascade | delete a document that has items | items gone |
  | down | `testdb.MigrateDown`, then `SELECT to_regclass('customers')` | `NULL` |

- [ ] **Step 2: Run `task test:integration`.** Expected: FAIL (no `testdb`, no migrations).
- [ ] **Step 3: Write `testdb.go` and the five migration pairs.** The status CHECK is one expression: `(parent_type = 'invoice' AND status IN ('draft','open','partially_paid','paid','void')) OR (parent_type = 'estimate' AND status IN ('draft','open','accepted','declined'))`. `created_at`/`updated_at` default to `now()`. Each `down` drops exactly what its `up` created, sequences included.
- [ ] **Step 4: Run `task test:integration`.** Expected: PASS.
- [ ] **Step 5: Write `seeds/dev.sql` and `cmd/seed/main.go`.** The seed inserts one customer ("William Saliba"), two locations (West Loop 1604 South, San Antonio, TX 12341; Wall Street, New York, NY 1232312), six invoices at the San Antonio location titled "Bi-Monthly Service" at 6500 cents (four `paid` with matching `payments` rows, one `open`, one `draft`), and four estimates at the New York location (three `accepted`, one `open`), each with one line item, numbers taken from the sequences. `cmd/seed` loads config, connects, and runs the file in one simple-protocol `Exec`.
  Verify (needs `DATABASE_URL`): `task migrate:up && task seed` exits 0; running `task seed` twice adds a second customer rather than failing.
- [ ] **Step 6: `task check`, then commit:** `feat: add schema migrations, test database helper and seed`.

---

### Task 3: Domain model and rules

**Files:**
- Create: `internal/model/errors.go`, `customer.go`, `location.go`, `billing_document.go`, `cursor.go`
- Test: `tests/unit/model/billing_document_test.go`, `tests/unit/model/cursor_test.go`

**Interfaces (Produces):**

```go
var ErrNotFound, ErrConflict, ErrValidation error   // errors.New("not found"), "conflict", "invalid request"

type Customer struct { ID, AccountNumber int64; Name string; Email, Phone *string; CreatedAt, UpdatedAt time.Time }
type Location struct { ID, CustomerID int64; Street, City, State, PostalCode string; CreatedAt, UpdatedAt time.Time }

type ParentType string  // ParentInvoice = "invoice", ParentEstimate = "estimate"
type Status string      // StatusDraft, StatusOpen, StatusPartiallyPaid, StatusPaid, StatusVoid, StatusAccepted, StatusDeclined

type Item struct { ID, BillingDocumentID int64; Position int; Name string; Description *string; Quantity int; UnitPriceCents int64 }
func (i Item) TotalCents() int64

type BillingDocument struct {
    ID int64; ParentType ParentType; Number, CustomerID int64
    Location Location            // ID always set; address fields set by reads
    Title string; Status Status; DocumentDate time.Time
    TotalCents, AmountPaidCents int64; ConvertedFromID *int64
    Items []Item; CreatedAt, UpdatedAt time.Time
}
func (d BillingDocument) BalanceDueCents() int64

type Payment struct { ID, BillingDocumentID, AmountCents int64; PaidAt time.Time }
type DocumentInput struct { LocationID int64; Title string; DocumentDate time.Time; Items []Item }
type DocumentFilter struct { CustomerID int64; ParentType ParentType; LocationID *int64; Status *Status; After *DocumentCursor; Limit int }

func CanTransition(pt ParentType, from, to Status) bool
func ValidStatus(pt ParentType, s Status) bool
func SumItems(items []Item) int64
func StatusAfterPayment(totalCents, paidCents int64) Status   // paid when paidCents >= totalCents, else partially_paid

type DocumentCursor struct { Date time.Time; ID int64 }
func (c DocumentCursor) Encode() string                 // base64 URL encoding of "2026-12-10|12"
func DecodeCursor(s string) (DocumentCursor, error)     // failure wraps ErrValidation
```

- [ ] **Step 1: Write the failing tests.**
  - `TestCanTransition`: loop over both parent types and every `(from, to)` pair of all seven statuses; the result must be true exactly for this set and false for everything else, including `from == to`:
    - invoice: `draft→open`, `draft→void`, `open→void`, `open→partially_paid`, `open→paid`, `partially_paid→partially_paid`, `partially_paid→paid`
    - estimate: `draft→open`, `open→accepted`, `open→declined`

    (`partially_paid→partially_paid` is the one allowed self-transition: a second partial payment.)
  - `TestValidStatus`: `accepted` is invalid for invoices, `paid` invalid for estimates, `"bogus"` invalid for both, `draft` and `open` valid for both.
  - `TestSumItems`: no items → 0; `{qty 1, 6500}` → 6500; `{2, 6500}` + `{3, 100}` → 13300.
  - `TestStatusAfterPayment`: `(6500, 1000)` → `partially_paid`; `(6500, 6500)` → `paid`.
  - `TestBalanceDueCents`: total 6500, paid 1000 → 5500.
  - `TestCursorRoundTrip`: `{2026-12-10, 12}` encodes and decodes to itself; `DecodeCursor` of `""`, `"!!!"`, base64 of `"nodate|x"` and base64 of `"2026-12-10"` each return an error satisfying `errors.Is(err, model.ErrValidation)`.
- [ ] **Step 2: Run `task test`.** Expected: FAIL to compile.
- [ ] **Step 3: Implement.** One transition table `map[ParentType]map[Status][]Status`; `CanTransition` and `ValidStatus` both read it.
- [ ] **Step 4: Run `task test`.** Expected: PASS.
- [ ] **Step 5: `task check`, then commit:** `feat: add domain model, status transitions and cursor`.

---

### Task 4: HTTP foundation, health endpoint and server

**Files:**
- Create: `internal/repository/db/dbtx.go`, `internal/repository/errors.go`, `internal/dto/error.go`, `internal/handler/errors.go`, `request.go`, `health.go`, `router.go`, `internal/middleware/logging.go`, `internal/app/app.go`, `cmd/api/main.go`, `tests/integration/testapp/testapp.go`
- Test: `tests/unit/handler/errors_test.go`, `tests/unit/handler/request_test.go`, `tests/integration/health/health_test.go`

**Interfaces:**
- Consumes: `model.Err*`, `config.Load`, `testdb.New`.
- Produces:

```go
// repository/db
type DBTX interface {   // satisfied by *pgxpool.Pool and pgx.Tx
    Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
    Query(context.Context, string, ...any) (pgx.Rows, error)
    QueryRow(context.Context, string, ...any) pgx.Row
}
// repository (unexported): mapErr(err error) error
//   pgx.ErrNoRows → model.ErrNotFound; PgError 23503 or 23505 → model.ErrConflict; else err unchanged

// dto
type ErrorBody struct { Code string `json:"code"`; Message string `json:"message"` }
type ErrorResponse struct { Error ErrorBody `json:"error"` }

// handler
func RespondError(c *gin.Context, err error)     // the only place that picks a status code
func PathID(c *gin.Context) (int64, error)       // ":id" as int64 > 0, else wraps ErrValidation
func QueryInt64(c *gin.Context, name string) (*int64, error)   // absent → nil; present must be > 0
type Deps struct { Log *slog.Logger; Ping func(context.Context) error }   // later tasks add service fields
func NewRouter(d Deps) *gin.Engine

// middleware
func Logging(log *slog.Logger) gin.HandlerFunc   // one line per request: method, path, status, duration

// app
func New(pool *pgxpool.Pool, queryTimeout time.Duration, log *slog.Logger) http.Handler

// tests/integration/testapp (tagged integration)
type App struct { Handler http.Handler; Pool *pgxpool.Pool }
func New(t *testing.T) *App                                   // testdb.New + app.New
func (a *App) Do(t *testing.T, method, path string, customerID int64, body any) *httptest.ResponseRecorder
//   customerID 0 means "send no X-Customer-ID header"; body nil means no body
func Decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T
```

- [ ] **Step 1: Write the failing unit tests.**
  - `TestRespondError`: for each row, call `RespondError` on a `gin.CreateTestContext` recorder and assert status and `error.code`:

    | Error | Status | Code | Message |
    |---|---|---|---|
    | `fmt.Errorf("get invoice: %w", model.ErrNotFound)` | 404 | `not_found` | the error's text |
    | `fmt.Errorf("%w: invoice is not a draft", model.ErrConflict)` | 409 | `conflict` | the error's text |
    | `fmt.Errorf("%w: title is required", model.ErrValidation)` | 400 | `invalid_request` | the error's text |
    | `errors.New("connection refused")` | 500 | `internal` | exactly `internal server error` |
  - `TestPathID`: `"12"` → 12; `"0"`, `"-3"`, `"abc"`, `"9999999999999999999999"` → error wrapping `ErrValidation`.
  - `TestQueryInt64`: absent → `nil, nil`; `"7"` → 7; `"0"`, `"x"` → error wrapping `ErrValidation`.
- [ ] **Step 2: Write the failing integration test** `TestHealth`: `GET /healthz` → 200; after `app.Pool.Close()`, `GET /healthz` → 503 with code `internal`.
- [ ] **Step 3: Run `task test`.** Expected: FAIL to compile.
- [ ] **Step 4: Implement** the files above. `NewRouter` uses `gin.New()`, `gin.Recovery()`, `middleware.Logging`, and registers `GET /healthz`. `RespondError` logs 500s with `slog` before answering. Health calls `Deps.Ping` and writes 503 directly on failure.
- [ ] **Step 5: Implement `cmd/api/main.go`:** `config.Load`; `pgxpool.ParseConfig` with `MaxConns = cfg.DBMaxConns`; `signal.NotifyContext` for `os.Interrupt` and `syscall.SIGTERM`; `http.Server{Addr: ":" + cfg.Port, Handler: app.New(...), ReadHeaderTimeout: 10 * time.Second}`; on signal, `Shutdown` with a 15 s context, then `pool.Close()`.
- [ ] **Step 6: Run `task test` and `task test:integration`.** Expected: PASS.
- [ ] **Step 7: `task check`, then commit:** `feat: add HTTP foundation, error mapping and health endpoint`.

---

### Task 5: Customers

**Files:**
- Create: `internal/repository/customer.go`, `internal/service/customer.go`, `internal/dto/customer.go`, `internal/dto/page.go`, `internal/handler/customer.go`
- Modify: `internal/handler/router.go` (add `Deps.Customers`, routes), `internal/app/app.go`
- Test: `tests/unit/service/customer_test.go`, `tests/unit/handler/customer_test.go`, `tests/integration/customers/customers_test.go`

**Interfaces:**
- Consumes: `db.DBTX`, `mapErr`, `RespondError`, `PathID`, `testapp`.
- Produces:

```go
// service
type CustomerRepository interface {
    Create(ctx context.Context, c model.Customer) (model.Customer, error)
    Get(ctx context.Context, id int64) (model.Customer, error)
    List(ctx context.Context, afterID int64, limit int) ([]model.Customer, error)   // ORDER BY id
    Update(ctx context.Context, c model.Customer) (model.Customer, error)
    Delete(ctx context.Context, id int64) error                                      // no row → ErrNotFound
}
type CustomerService struct{ /* repo */ }
func NewCustomerService(repo CustomerRepository) *CustomerService
func (s *CustomerService) Create(ctx, name string, email, phone *string) (model.Customer, error)
func (s *CustomerService) Get(ctx, id int64) (model.Customer, error)
func (s *CustomerService) List(ctx, afterID int64, limit int) (items []model.Customer, nextAfterID int64, err error) // nextAfterID 0 = last page
func (s *CustomerService) Update(ctx, id int64, name string, email, phone *string) (model.Customer, error)
func (s *CustomerService) Delete(ctx, id int64) error

// repository
func NewCustomers(db db.DBTX, timeout time.Duration) *Customers

// dto
type CustomerRequest struct {
    Name  string  `json:"name" binding:"required,max=200"`
    Email *string `json:"email" binding:"omitempty,email,max=320"`
    Phone *string `json:"phone" binding:"omitempty,max=50"`
}
type CustomerResponse struct { ID, AccountNumber int64; Name string; Email, Phone *string }  // snake_case json tags
type Page[T any] struct { Data []T `json:"data"`; NextCursor string `json:"next_cursor,omitempty"` }
func ParseLimit(raw string) (int, error)   // "" → 20; 1..100 accepted; anything else wraps ErrValidation
```

  The customer list cursor is the last row's `id` as a decimal string. `Page.Data` is `[]`, never `null`, when empty.

- [ ] **Step 1: Run `task mocks` after declaring `CustomerRepository`,** then write the failing service tests (`TestCustomerService_*`, table-driven, `mocks.NewMockCustomerRepository(t)`):
  - Create trims the name and passes it to the repo; `"   "` returns `ErrValidation` without calling the repo.
  - List asks the repo for `limit + 1`; with 21 rows back and limit 20 it returns 20 items and `nextAfterID` = the 20th row's ID; with 20 or fewer back, `nextAfterID` is 0.
  - Repo errors come back wrapped and still satisfy `errors.Is`.
- [ ] **Step 2: Write the failing handler tests** (router built with a real `CustomerService` over a mock repo): `POST /api/v1/customers` with `{}` → 400; with `{"name":"A","email":"nope"}` → 400; malformed JSON → 400; `GET /api/v1/customers/abc` → 400; `GET /api/v1/customers?limit=0`, `limit=101`, `limit=x`, `cursor=x` → 400; valid create → 201 and body has `account_number`.
- [ ] **Step 3: Write the failing integration test** `TestCustomers`: create → 201 with `account_number` 5000; a second create gets 5001; get → 200; update → 200 with the new name; get unknown ID → 404; create 3 and list with `limit=2` → 2 rows plus `next_cursor`, following it returns the rest and no `next_cursor`; delete → 204 and then get → 404; delete a customer that has a location (insert the location with `Pool.Exec`) → 409.
- [ ] **Step 4: Run the tests.** Expected: FAIL.
- [ ] **Step 5: Implement repository, service, DTO, handler and routes** (`POST`, `GET`, `GET /:id`, `PUT /:id`, `DELETE /:id` under `/api/v1/customers`). `account_number` comes from `nextval('customer_account_number_seq')` in the INSERT. `Update` sets `updated_at = now()`.
- [ ] **Step 6: Run `task test` and `task test:integration`.** Expected: PASS.
- [ ] **Step 7: `task check`, then commit:** `feat: add customers API`.

---

### Task 6: Locations and the customer header

**Files:**
- Create: `internal/middleware/customer.go`, `internal/repository/location.go`, `internal/repository/lookups.go`, `internal/service/location.go`, `internal/dto/location.go`, `internal/handler/location.go`
- Modify: `internal/handler/router.go`, `internal/app/app.go`
- Test: `tests/unit/handler/customer_header_test.go`, `tests/unit/service/location_test.go`, `tests/integration/locations/locations_test.go`

**Interfaces:**
- Consumes: `CustomerService.Get`, `RespondError`.
- Produces:

```go
// middleware
// shortcut: trusts X-Customer-ID and looks the customer up on every request; replace with real auth.
func RequireCustomer(exists func(ctx context.Context, id int64) error, fail func(*gin.Context, error)) gin.HandlerFunc
func CustomerID(c *gin.Context) int64

// service
type LocationRepository interface {
    Create(ctx context.Context, l model.Location) (model.Location, error)
    Get(ctx context.Context, customerID, id int64) (model.Location, error)
    List(ctx context.Context, customerID int64) ([]model.Location, error)     // ORDER BY id; in lookups.go
    Update(ctx context.Context, l model.Location) (model.Location, error)     // WHERE id AND customer_id
    Delete(ctx context.Context, customerID, id int64) error
}
type LocationService struct{ /* repo */ }
func NewLocationService(repo LocationRepository) *LocationService
// methods mirror the repository, taking customerID first

// repository
func NewLocations(db db.DBTX, timeout time.Duration) *Locations

// dto
type LocationRequest struct { Street, City, State, PostalCode string }  // each `binding:"required,max=200"`, snake_case json
type LocationResponse struct { ID int64; Street, City, State, PostalCode string }
func FromLocation(l model.Location) LocationResponse
```

  `router.go` gains a group `/api/v1` with `RequireCustomer(func(ctx, id) error { _, err := d.Customers.Get(ctx, id); return err }, RespondError)`; locations and every later resource register on it.

- [ ] **Step 1: Write the failing middleware test** `TestRequireCustomer` (a one-route test engine):

  | Header | `exists` returns | Expect |
  |---|---|---|
  | absent | not called | 400 `invalid_request` |
  | `abc`, `0`, `-1` | not called | 400 |
  | `7` | `model.ErrNotFound` | 404 `not_found` |
  | `7` | `nil` | 200, and `CustomerID(c)` is 7 |
- [ ] **Step 2: Write the failing service test:** whitespace-only street → `ErrValidation`; repo errors stay matchable with `errors.Is`.
- [ ] **Step 3: Write the failing integration test** `TestLocations` with customers A and B: A creates a location → 201; A lists → 1 row (a bare JSON array under `data`, no cursor); B lists → 0 rows; B gets, updates and deletes A's location → 404 each; A updates → 200; no header → 400; header for a non-existent customer → 404; A deletes a location referenced by a document (insert with `Pool.Exec`) → 409; A deletes a free location → 204.
- [ ] **Step 4: Run the tests.** Expected: FAIL.
- [ ] **Step 5: Implement.**
- [ ] **Step 6: Run `task mocks`, `task test`, `task test:integration`.** Expected: PASS.
- [ ] **Step 7: `task check`, then commit:** `feat: add locations API and customer header middleware`.

---

### Task 7: Billing documents: create, read, list, update, delete

**Files:**
- Create: `internal/repository/billing_document.go`, `internal/repository/tx.go`, `internal/service/billing_document.go`, `internal/dto/billing_document.go`, `internal/handler/billing_document.go`
- Modify: `internal/handler/router.go`, `internal/app/app.go`
- Test: `tests/unit/service/billing_document_test.go`, `tests/unit/handler/billing_document_test.go`, `tests/integration/billing_documents/crud_test.go`

**Interfaces:**
- Consumes: model types from Task 3, `LocationRepository.Get`, `middleware.CustomerID`, `dto.Page`, `dto.ParseLimit`, `QueryInt64`.
- Produces:

```go
// service
type BillingDocumentRepository interface {
    WithTx(ctx context.Context, fn func(BillingDocumentRepository) error) error
    Create(ctx context.Context, d model.BillingDocument) (id int64, err error)          // document + items; number from the type's sequence
    Get(ctx context.Context, customerID int64, pt model.ParentType, id int64) (model.BillingDocument, error)          // joined location, no items
    GetForUpdate(ctx context.Context, customerID int64, pt model.ParentType, id int64) (model.BillingDocument, error) // same, row-locked
    ListItems(ctx context.Context, documentID int64) ([]model.Item, error)              // ORDER BY position
    List(ctx context.Context, f model.DocumentFilter) ([]model.BillingDocument, error)  // joined location, no items
    Update(ctx context.Context, d model.BillingDocument) error                          // title, date, location, total; replaces items
    Delete(ctx context.Context, id int64) error
}
type BillingDocumentService struct{ /* docs, locations, now */ }
func NewBillingDocumentService(docs BillingDocumentRepository, locs LocationRepository, now func() time.Time) *BillingDocumentService
func (s *BillingDocumentService) Create(ctx, customerID int64, pt model.ParentType, in model.DocumentInput) (model.BillingDocument, error)
func (s *BillingDocumentService) Get(ctx, customerID int64, pt model.ParentType, id int64) (model.BillingDocument, error)   // with Items
func (s *BillingDocumentService) List(ctx, f model.DocumentFilter) ([]model.BillingDocument, *model.DocumentCursor, error)
func (s *BillingDocumentService) Update(ctx, customerID int64, pt model.ParentType, id int64, in model.DocumentInput) (model.BillingDocument, error)
func (s *BillingDocumentService) Delete(ctx, customerID int64, pt model.ParentType, id int64) error

// repository
func NewBillingDocuments(pool *pgxpool.Pool, timeout time.Duration) *BillingDocuments
// WithTx begins a pgx transaction, calls fn with a copy of the repository bound to the tx,
// commits on nil and rolls back on error or panic. Called on a tx-bound copy, it just calls fn.

// dto
type ItemRequest struct {
    Name           string  `json:"name" binding:"required,max=200"`
    Description    *string `json:"description" binding:"omitempty,max=2000"`
    Quantity       int     `json:"quantity" binding:"required,gt=0,lte=1000000"`
    UnitPriceCents *int64  `json:"unit_price_cents" binding:"required,gte=0,lte=10000000000"`
}
type DocumentRequest struct {
    LocationID   int64         `json:"location_id" binding:"required,gt=0"`
    Title        string        `json:"title" binding:"required,max=200"`
    DocumentDate string        `json:"document_date" binding:"required,datetime=2006-01-02"`
    Items        []ItemRequest `json:"items" binding:"required,min=1,max=100,dive"`
}
func (r DocumentRequest) ToInput() model.DocumentInput    // positions are the slice indexes
type DocumentResponse struct { /* the spec's detail shape */ }
//   Items is `json:"items,omitempty"`; AmountPaidCents and BalanceDueCents are *int64 with omitempty, set only for invoices
func FromDocument(d model.BillingDocument, withItems bool) DocumentResponse

// handler
type BillingDocumentHandler struct{ /* svc, parentType */ }
func NewBillingDocumentHandler(svc *service.BillingDocumentService, pt model.ParentType) *BillingDocumentHandler
// methods: List, Create, Get, Update, Delete
```

  Rules the service owns:
  - Create and Update call `locs.Get(ctx, customerID, in.LocationID)` first; a location that is missing or another customer's returns `ErrNotFound`.
  - A whitespace-only title returns `ErrValidation`.
  - Create sets status `draft` and `TotalCents = model.SumItems(items)`, inside `WithTx`.
  - `Get` runs `docs.Get` and `docs.ListItems` concurrently with `errgroup.WithContext`.
  - Update and Delete run in `WithTx`: `GetForUpdate`, then `ErrConflict` unless the status is `draft`.
  - `List` rejects a `Status` for which `model.ValidStatus` is false with `ErrValidation`, asks the repo for `Limit + 1`, and returns a cursor built from the last returned row only when the extra row exists.

  The list SQL orders by `document_date DESC, id DESC` and pages with `(document_date, id) < ($n, $m)`. Optional filters are written as `($n::bigint IS NULL OR location_id = $n)`, never by concatenating SQL.

- [ ] **Step 1: Declare the interface, run `task mocks`, write the failing service tests.** In tests, make the mock's `WithTx` run its callback against the same mock:

```go
repo.EXPECT().WithTx(mock.Anything, mock.Anything).
    RunAndReturn(func(ctx context.Context, fn func(service.BillingDocumentRepository) error) error { return fn(repo) })
```

  Cases:
  - Create: unknown location → `ErrNotFound`, `Create` never called; whitespace title → `ErrValidation`; valid input with items `{2 × 6500}` and `{3 × 100}` → repo `Create` receives status `draft`, `TotalCents` 13300, the right `ParentType` and `CustomerID`.
  - Get: returns the document with the items from `ListItems`; if either call fails the error is returned.
  - Update and Delete: status `open` → `ErrConflict`, and `Update`/`Delete` on the repo are never called; status `draft` → called once.
  - List: status `accepted` with `ParentInvoice` → `ErrValidation`; limit 2 with 3 rows back → 2 rows and a cursor equal to row 2's `(DocumentDate, ID)`; 2 rows back → nil cursor.
- [ ] **Step 2: Write the failing handler tests** (both `/invoices` and `/estimates`, real service over mocks). Each expects 400 and no repository call:
  - `items` missing, empty, or 101 entries;
  - `quantity` 0 or 1000001; `unit_price_cents` missing, -1 or 10000000001 (0 is accepted);
  - `document_date` `"2026-13-40"`, `"12/10/2026"`, missing;
  - `title` missing;
  - `GET /invoices/abc`; `?limit=0`; `?location_id=x`; `?cursor=!!!`; `?status=accepted` on invoices; `?status=paid` on estimates; `?status=bogus`.
- [ ] **Step 3: Write the failing integration test** `TestDocumentCRUD`, run once per parent type:
  - create → 201, status `draft`, `number` 85000 for the first invoice and 5000 for the first estimate, `subtotal_cents == total_cents == 13300` for the two items above;
  - an invoice response has `amount_paid_cents: 0` and `balance_due_cents: 13300`; an estimate response has neither key;
  - get → items in the order sent, each with `total_cents`, and the `location` object filled;
  - update with one item of 6500 → total 6500 and exactly one item;
  - list → rows have `location` and no `items` key;
  - an invoice ID requested under `/estimates/:id` → 404;
  - customer B: get, update, delete → 404, and B's list is empty;
  - create with B's `location_id` as customer A → 404;
  - pagination: 5 documents with dates on 3 distinct days (two pairs sharing a date), `limit=2` → pages of 2, 2, 1, strictly ordered by date then ID descending, no row repeated or skipped, no `next_cursor` on the last page;
  - `?location_id=` returns only that location's documents; `?status=draft` returns only drafts;
  - delete a draft → 204, then get → 404; its items are gone (`Pool` count).
- [ ] **Step 4: Run the tests.** Expected: FAIL.
- [ ] **Step 5: Implement** repository, `WithTx`, service, DTOs, handler. In `router.go`, register the shared routes on both groups from a small loop over `{"/invoices", ParentInvoice}` and `{"/estimates", ParentEstimate}`.
- [ ] **Step 6: Run `task test` and `task test:integration`.** Expected: PASS.
- [ ] **Step 7: `task check`, then commit:** `feat: add invoice and estimate CRUD with keyset pagination`.

---

### Task 8: Status transitions (send, void, accept, decline)

**Files:**
- Modify: `internal/service/billing_document.go`, `internal/repository/billing_document.go`, `internal/handler/billing_document.go`, `internal/handler/router.go`
- Test: `tests/unit/service/billing_document_test.go`, `tests/integration/billing_documents/transitions_test.go`

**Interfaces:**
- Consumes: `model.CanTransition`, `GetForUpdate`, `WithTx`.
- Produces:

```go
// added to BillingDocumentRepository
UpdateStatus(ctx context.Context, id int64, status model.Status) error

func (s *BillingDocumentService) Transition(ctx, customerID int64, pt model.ParentType, id int64, to model.Status) (model.BillingDocument, error)
// WithTx: GetForUpdate → !CanTransition(pt, doc.Status, to) → ErrConflict → UpdateStatus. Returns s.Get afterwards.

func (h *BillingDocumentHandler) Transition(to model.Status) gin.HandlerFunc
```

  Routes: `POST /:id/send` (→ `open`) on both groups; `POST /invoices/:id/void` (→ `void`); `POST /estimates/:id/accept` (→ `accepted`); `POST /estimates/:id/decline` (→ `declined`).

- [ ] **Step 1: Run `task mocks`, write the failing service test** `TestTransition`: invoice `draft → open` calls `UpdateStatus(id, open)`; invoice `paid → void` returns `ErrConflict` and never calls `UpdateStatus`; a `GetForUpdate` `ErrNotFound` passes through.
- [ ] **Step 2: Write the failing integration test** `TestTransitions`:
  - invoice: send → 200 `open`; send again → 409; void an `open` invoice → 200 `void`; void a `draft` → 200; void a `void` → 409; update or delete an `open` invoice → 409.
  - estimate: accept a `draft` → 409; send → `open`; accept → `accepted`; decline an `accepted` → 409; on a second estimate, send then decline → `declined`.
  - routes that must not exist: `POST /estimates/:id/void` and `POST /invoices/:id/accept` → 404.
  - customer B sending A's invoice → 404.
- [ ] **Step 3: Run the tests.** Expected: FAIL.
- [ ] **Step 4: Implement.**
- [ ] **Step 5: Run `task test` and `task test:integration`.** Expected: PASS.
- [ ] **Step 6: `task check`, then commit:** `feat: add invoice and estimate status transitions`.

---

### Task 9: Payments and balance

**Files:**
- Create: `internal/repository/payment.go`
- Modify: `internal/service/billing_document.go`, `internal/dto/billing_document.go`, `internal/handler/billing_document.go`, `internal/handler/router.go`
- Test: `tests/unit/service/billing_document_test.go`, `tests/unit/handler/billing_document_test.go`, `tests/integration/billing_documents/payments_test.go`

**Interfaces:**
- Consumes: `GetForUpdate`, `WithTx`, `model.StatusAfterPayment`, `model.CanTransition`.
- Produces:

```go
// added to BillingDocumentRepository
AddPayment(ctx context.Context, documentID, amountCents int64, newStatus model.Status) error
//   inserts the payment (paid_at = now()) and sets amount_paid_cents = amount_paid_cents + $2, status = $3
ListPayments(ctx context.Context, documentID int64) ([]model.Payment, error)             // ORDER BY paid_at, id
Balance(ctx context.Context, customerID int64, locationID *int64) (int64, error)
//   COALESCE(SUM(total_cents - amount_paid_cents), 0) over invoices in status open or partially_paid

func (s *BillingDocumentService) RecordPayment(ctx, customerID, invoiceID, amountCents int64) (model.BillingDocument, error)
func (s *BillingDocumentService) ListPayments(ctx, customerID, invoiceID int64) ([]model.Payment, error)
func (s *BillingDocumentService) Balance(ctx, customerID int64, locationID *int64) (int64, error)

// dto
type PaymentRequest struct { AmountCents int64 `json:"amount_cents" binding:"required,gt=0"` }
type PaymentResponse struct { ID, AmountCents int64; PaidAt time.Time }   // snake_case json
type BalanceResponse struct { BalanceCents int64 `json:"balance_cents"` }
```

  `RecordPayment`, inside `WithTx`: `GetForUpdate(customerID, ParentInvoice, id)`; `ErrConflict` if `amountCents > doc.BalanceDueCents()`; `newStatus = StatusAfterPayment(total, paid + amount)`; `ErrConflict` unless `CanTransition(ParentInvoice, doc.Status, newStatus)`; `AddPayment`. `ListPayments` calls `docs.Get` first so another customer's invoice is a 404.
  Routes: `POST` and `GET /invoices/:id/payments` (POST answers 201 with the updated invoice, GET answers `{"data": [...]}`), `GET /balance`.

- [ ] **Step 1: Run `task mocks`, write the failing service test** `TestRecordPayment` (invoice total 6500):

  | Status | Paid so far | Amount | Expect |
  |---|---|---|---|
  | `open` | 0 | 1000 | `AddPayment(id, 1000, partially_paid)` |
  | `partially_paid` | 1000 | 2000 | `AddPayment(id, 2000, partially_paid)` |
  | `partially_paid` | 3000 | 3500 | `AddPayment(id, 3500, paid)` |
  | `open` | 0 | 6500 | `AddPayment(id, 6500, paid)` |
  | `open` | 0 | 6501 | `ErrConflict`, no `AddPayment` |
  | `draft` | 0 | 100 | `ErrConflict`, no `AddPayment` |
  | `void` | 0 | 100 | `ErrConflict`, no `AddPayment` |
  | `paid` | 6500 | 1 | `ErrConflict`, no `AddPayment` |
- [ ] **Step 2: Write the failing handler test:** `amount_cents` 0, -5, missing, or `"ten"` → 400.
- [ ] **Step 3: Write the failing integration test** `TestPayments`:
  - an `open` 6500 invoice: pay 1000 → 201, `partially_paid`, `amount_paid_cents` 1000, `balance_due_cents` 5500; pay 5500 → `paid`, balance 0; pay 1 more → 409;
  - void a `partially_paid` invoice → 409;
  - `GET .../payments` → 2 rows, in order, amounts 1000 and 5500;
  - payment on a draft → 409; payment with an estimate's ID → 404; customer B paying or listing A's invoice → 404;
  - balance: customer with invoices `open` 6500 (location L1), `partially_paid` 6500 with 1000 paid (L2), `paid`, `draft`, `void`, and an `open` estimate → `GET /balance` is 12000; `?location_id=L1` is 6500; `?location_id=x` → 400; a customer with no invoices → 0;
  - concurrency: on an `open` 6500 invoice, fire two `POST` payments of 4000 from two goroutines (`sync.WaitGroup`); exactly one answers 201 and one 409, and the invoice ends with `amount_paid_cents` 4000.
- [ ] **Step 4: Run the tests.** Expected: FAIL.
- [ ] **Step 5: Implement.**
- [ ] **Step 6: Run `task test` and `task test:integration`.** Expected: PASS. Run the integration package 5 times (`-count=5`) to confirm the concurrency case is stable.
- [ ] **Step 7: `task check`, then commit:** `feat: add invoice payments and account balance`.

---

### Task 10: Convert an estimate to an invoice, and the full path

**Files:**
- Modify: `internal/service/billing_document.go`, `internal/handler/billing_document.go`, `internal/handler/router.go`
- Test: `tests/unit/service/billing_document_test.go`, `tests/integration/billing_documents/convert_test.go`

**Interfaces:**
- Consumes: `GetForUpdate`, `ListItems`, `Create`, `WithTx`, the `now` function given to `NewBillingDocumentService`.
- Produces:

```go
func (s *BillingDocumentService) Convert(ctx, customerID, estimateID int64) (model.BillingDocument, error)
// WithTx: GetForUpdate(customerID, ParentEstimate, id) → ErrConflict unless status is accepted →
// ListItems → Create an invoice: status draft, same title, location and items (IDs zeroed),
// DocumentDate = now() truncated to the day, ConvertedFromID = &estimate.ID, TotalCents = SumItems.
// A unique violation from Create (already converted) arrives as ErrConflict. Returns s.Get of the invoice.
```

  Route: `POST /estimates/:id/convert` → 201 with the invoice.

- [ ] **Step 1: Write the failing service test** `TestConvert` with `now` fixed at 2026-11-11 15:30 UTC: an `open` estimate → `ErrConflict`, `Create` never called; an `accepted` estimate with two items → `Create` receives `ParentInvoice`, status `draft`, `DocumentDate` 2026-11-11 00:00 UTC, `ConvertedFromID` pointing at the estimate, the same title and location, both items with ID 0, and the summed total; `Create` returning `ErrConflict` passes through.
- [ ] **Step 2: Write the failing integration tests.**
  - `TestConvert`: convert a `draft`, `open` or `declined` estimate → 409; convert an `accepted` one → 201, a `draft` invoice with the estimate's title, location, items and total; the estimate is still `accepted`; convert it again → 409 and the invoice count is unchanged; customer B converting A's estimate → 404; two concurrent converts of one accepted estimate → exactly one 201.
  - `TestFullPath`: create customer → create location → create estimate (6500) → send → accept → convert → send the invoice → `GET /balance` is 6500 → pay 2000 → balance 4500 → pay 4500 → invoice `paid` → balance 0 → `GET /invoices?status=paid` has that one invoice.
- [ ] **Step 3: Run the tests.** Expected: FAIL.
- [ ] **Step 4: Implement.**
- [ ] **Step 5: Run the whole suite:** `task test`, `task test:integration`, `task check`. Expected: all PASS, check clean.
- [ ] **Step 6: Smoke-test the real binary** (needs `DATABASE_URL`): `task migrate:up`, `task seed`, `task run`, then `curl -H "X-Customer-ID: 1" localhost:8080/api/v1/invoices` returns the seeded invoices and `curl localhost:8080/healthz` returns 200. Stop the server with Ctrl+C and confirm it logs a clean shutdown.
- [ ] **Step 7: Commit:** `feat: add estimate to invoice conversion`.
