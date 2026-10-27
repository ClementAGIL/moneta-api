# CLAUDE.md

Guidance for Claude (and any contributor) working in this repository. `AGENTS.md` points here.

## What this project is

Go API of **Moneta**, a multi-user spending analyzer: users import the CSV/XLSX exports of their bank accounts, Moneta
deduplicates and categorizes the transactions and computes stats, subscriptions, budgets, forecast and insights.
Clients live in the `../moneta-app` monorepo: `apps/web` (React) and `apps/mobile` (Expo); their API client and types
are in `packages/shared`. The full plan is in `docs/PLAN.md`; read it
before changing behavior.

## Stack

Go 1.24+, standard `net/http` mux, pgx v5 + sqlc, goose migrations (embedded), PostgreSQL 17, `log/slog`.
Runs with Docker Compose on the user's home server. No unit tests by default (user's rule); verify with
`go vet`, `golangci-lint`, `docs/requests.http` and real imports.

## Commands

| Command                              | What it does                                  |
| ------------------------------------ | --------------------------------------------- |
| `go run ./cmd/moneta`                | Run the API (reads `.env` values from the env) |
| `go build ./... && go vet ./...`     | Compile and vet (run before calling a change done) |
| `gofmt -l .`                         | Must print nothing                            |
| `docker compose up -d --build`       | Full stack: db, api, web on :8000 (needs `../moneta-app`) |

## Architecture

```
cmd/moneta/main.go        entry point: config, HTTP server, graceful shutdown
internal/config/          env → Config (see .env.example)
internal/httpapi/         router (Go 1.22 method patterns), JSON helpers, access log middleware
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

- ✅ M0 scaffold: `GET /api/health`, Dockerfile, docker-compose (db + api + web), plan.
- Next: M1 (database, migrations, job queue skeleton). Go and Docker are not installed on the dev PC yet.
