# CLAUDE.md

Guidance for Claude (and any contributor) working in this repository. `AGENTS.md` points here.

## What this project is

Go API of **Moneta**, a multi-user spending analyzer: users import the CSV/XLSX exports of their bank accounts, Moneta
deduplicates and categorizes the transactions and computes stats, subscriptions, budgets, forecast and insights.
Clients live in the `../moneta-app` monorepo: `apps/web` (React) and `apps/mobile` (Expo); their API client and types
are in `packages/shared`. The full plan is in `docs/PLAN.md`; read it
before changing behavior.

## Stack

Go 1.26+, standard `net/http` mux, pgx v5 (hand-written SQL), goose migrations (embedded), PostgreSQL 17+, `log/slog`.
Runs **natively** (no Docker: the dev PC has no hardware virtualization): PostgreSQL 18 Windows service
(`postgresql-x64-18`, `D:\PostgreSQL`, shared with Nutrimo, own `moneta` role + database), `moneta.exe` serving the API
and the built web app (`MONETA_WEB_DIR`). Dockerfile + compose stay as an optional way to run it, built by CI.
No unit tests by default (user's rule); verify with `go vet`, `golangci-lint`, `docs/requests.http` and real imports.

## Commands

| Command                              | What it does                                  |
| ------------------------------------ | --------------------------------------------- |
| `go run ./cmd/moneta`                | Run the API (reads `.env` values from the env) |
| `go build ./... && go vet ./...`     | Compile and vet (run before calling a change done) |
| `gofmt -l .`                         | Must print nothing                            |
| `psql -U postgres -v pw=… -f testdata/                 synthetic bank exports for manual import tests (never real data)
scripts/create-db.sql` | One-time: create the `moneta` role + database |
| `docker compose up -d --build`       | Optional full stack in containers (not usable on the dev PC) |

## Architecture

```
cmd/moneta/main.go        entry point: config → DB open + migrate → job worker + HTTP server, graceful shutdown
cmd/moneta/wire.go        builds services, hooks (import commit → categorize) and job handlers
internal/config/          .env (if present, real env wins) + env → Config (see .env.example)
internal/db/              pgx pool (Open), goose migrations from the embedded migrations/ (Migrate)
internal/jobs/            queue.go: Enqueue (pool or tx); worker.go: claim with SKIP LOCKED + 5 min lease,
                          handlers per kind, retry with exponential backoff (30 s × 2^n, max 6 h)
internal/vault/           envelope encryption: KEK (env) wraps a per-user DEK; UserKeys.Seal/Open (AES-GCM) and Index
                          (HMAC blind index), keys derived with HKDF
internal/auth/            argon2id (password.go), JWT HS256 15 min + opaque refresh tokens (tokens.go), Service:
                          Register/Login/Refresh (rotation, reuse revokes the family)/Logout/Verify (service.go),
                          SQL (store.go), Me + Keys (user.go)
internal/apperr/          domain errors (NotFound, Invalid(field), Conflict) mapped to 404/422/409 by httpapi
internal/db/querier.go    Querier (pool or tx), IsUnique/IsForeignKey, NotFoundIfNoRows, Affected, CleanName
internal/accounts/        banks + accounts (anchor balance/date; balance = anchor + movements after it)
internal/categories/      per-user two-level tree, defaults.go seeded at sign-up (stable keys), CRUD
internal/money/           Parse bank amounts → int64 cents ("," or "." decimals, thousands, (x), trailing -, Excel float noise)
internal/labels/          Normalize (uppercase, no accents/cards/dates/refs) and Merchant (first meaningful words)
internal/importer/        pure: grid.go ReadGrid (xlsx raw values / csv: UTF-8 or Windows-1252, delimiter sniffing),
                          detect.go Detect (header row, column roles by names then content, date format, decimal sep)
                          + Fingerprint, mapping.go Mapping.Validate/Parse, values.go dates (layouts + Excel serials)
internal/dedup/           Classify: exact duplicates per occurrence, near-matches (same amount, ±3 days, inside the
                          file period, not reproduced exactly by the file) → review
internal/imports/         Service: Upload (grid encrypted, profile by fingerprint), SetMapping (stage.go: parse, keys,
                          classify → import_rows; profile upsert), Commit (→ transactions, merchant + blind index),
                          Delete (cancel or undo), Get/List (summary.go)
internal/categorize/      engine.go: user rules (priority, corrections first) → builtin.go French merchant keywords →
                          model (Predictor, later) → uncategorized; service.go: Recategorize (all or one import,
                          skips user/transfer); rules.go: encrypted rules CRUD, Preview, CorrectMerchant (upsert)
internal/transactions/    List (filters, keyset cursor, text search in Go on decrypted labels), Get, Edit (category
                          → source user, optional merchant rule; note)
internal/stats/           Summary (month vs previous + 6-month average, savings rate, by top-level category),
                          Categories (monthly series), Merchants (top by spending); transfers/excluded ignored
internal/httpapi/         router.go: Deps, routes; resources.go: banks/accounts/categories CRUD; imports_handlers.go;
                          transactions_handlers.go (transactions + rules); stats_handlers.go; respond.go: apiError → {error:{code,message,fields}}, respond(w)(v, err), decodeJSON;
                          middleware.go: handle (error-returning handlers), authed (Bearer → userID(r)), limited,
                          body limit, panic recovery, access log; ratelimit.go: token buckets per IP; auth_handlers.go;
                          probes.go: /api/health, /api/ready; static.go: web build with index.html fallback
migrations/               goose SQL files (00001 jobs, 00002 users + refresh_tokens, 00003 banks/accounts/categories, 00004 import_profiles/imports/import_rows/transactions, 00005 rules), embedded by embed.go
scripts/create-db.sql     one-time role + database creation on a native PostgreSQL
.github/workflows/ci.yml  gofmt, vet, build, Docker image build
```

Planned packages (see `docs/PLAN.md` "Code layout"; crypto is named vault): money, importer, labels, dedup, transfers,
categorize, ml, recurring, budgets, forecast, insights, export.

New migration: `migrations/000NN_name.sql` with `-- +goose Up` / `-- +goose Down`; it runs on the next start.
Queries are plain SQL with pgx in the package that owns the table (`Execer`-style interfaces so they also work in a
transaction).

## Conventions

- Handlers are `handlerFunc` returning errors; return `apiError` helpers (errValidation, errNotFound…) for client
  errors, anything else is logged and becomes a 500.
- Domain packages return `apperr` errors; stores take a `db.Querier` and are cheap to build per request.
- Every query filters on `user_id`; never trust an id from the request without that filter.
- Money is `int64` cents, signed (negative = out). Never `float64` for money.
- Sensitive text (labels, merchants, notes, rule patterns) is stored encrypted (`_enc` columns, `internal/crypto`);
  equality lookups use blind indexes (`_key` columns). Amounts, dates and categories stay plain for SQL aggregates.
- Uploaded files are never written to disk.
- Errors: wrap with `fmt.Errorf("doing x: %w", err)`; handlers map them to `{"error": {code, message}}`.
- Idiomatic Go: `gofmt`, small interfaces, `context.Context` first, no globals except constants. Go constants are
  `camelCase` (Go style), grouped at the top of the file so tuning is easy.
- Short files (< ~300 lines), short functions (< ~40 lines), one job each. No duplicated logic. Prefer a lookup
  table over a long switch (e.g. insight rules, built-in merchant rules).
- Every file starts with a comment saying what it is for (the package comment for the main file of a package).
  Comments explain why, not what.
- The API contract lives in `docs/openapi.yaml`; the web app generates its types from it, so update it with every
  endpoint change.
- Every commit updates this file and the README when behavior or structure changes.

## Status

- ✅ M0 scaffold: `GET /api/health`, JSON 404 for unknown `/api/` paths, web app served from `MONETA_WEB_DIR` with
  SPA fallback, Dockerfile + compose (optional), CI, plan.
- Dev PC: Go 1.27 (`C:\Program Files\Go\bin`, may be missing from PATH in old shells). Docker Desktop is installed
  but cannot run (virtualization off in the BIOS).
- ✅ M1: pool, embedded migrations on startup, `.env` loading, `/api/ready`, job queue + worker (checked on the
  real database: migration applied, a job without handler fails and is rescheduled 30 s later, unlocked).
- ✅ M2: register/login/refresh/logout/me, argon2id, JWT + rotating refresh tokens (cookie for web, body for
  mobile with `client: "mobile"`), stolen-token family revocation, rate limits, vault (KEK/DEK). Smoke-tested.
- ✅ M3: banks/accounts/categories CRUD, default categories seeded in the sign-up transaction. Smoke-tested.
- ✅ M4: import pipeline, tested end to end with `testdata/` (synthetic exports in two layouts: overlap duplicates,
  identical same-day rows, re-labelled payment → review, Windows-1252 + title lines + debit/credit, undo).
- ✅ M5: categorization (rules → built-in keywords), categorized at commit, rule changes queue `categorize.user`
  jobs, corrections with "apply to merchant", transactions list/search/pagination. Tested on testdata.
- ✅ M6: stats endpoints; `docs/openapi.yaml` covers every endpoint so far. **MVP API complete.**
- Next: web MVP (W1–W5), then M7 (transfers, recurring).
