# CLAUDE.md

Guidance for Claude (and any contributor) working in this repository. `AGENTS.md` points here.

## What this project is

Go API of **Moneta**, a multi-user spending analyzer: users import the CSV/XLSX exports of their bank accounts, Moneta
deduplicates and categorizes the transactions and computes stats, subscriptions, budgets, forecast and insights.
Clients live in the `../moneta-app` monorepo: `apps/web` (React) and `apps/mobile` (Expo); their API client and types
are in `packages/shared`. The full plan is in `docs/PLAN.md`; read it
before changing behavior.

## Stack

Go 1.24+, standard `net/http` mux, pgx v5 + sqlc, goose migrations (embedded), PostgreSQL 17+, `log/slog`.
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
| `psql -U postgres -v pw=… -f scripts/create-db.sql` | One-time: create the `moneta` role + database |
| `docker compose up -d --build`       | Optional full stack in containers (not usable on the dev PC) |

## Architecture

```
cmd/moneta/main.go        entry point: config, HTTP server, graceful shutdown
internal/config/          env → Config (see .env.example)
internal/httpapi/         router (Go 1.22 method patterns), JSON helpers, access log middleware;
                          static.go serves the web build with index.html fallback
scripts/create-db.sql     one-time role + database creation on a native PostgreSQL
.github/workflows/ci.yml  gofmt, vet, build, Docker image build
```

Planned packages (see `docs/PLAN.md` "Code layout"): db, auth, crypto, money, importer, labels, dedup, transfers,
categorize, ml, recurring, budgets, forecast, insights, jobs, export.

## Conventions

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
- Next: M1 (database, migrations, job queue skeleton).
