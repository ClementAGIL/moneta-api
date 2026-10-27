# moneta-api

Backend of **Moneta**, a self-hosted spending analyzer. Import the CSV or Excel exports of all your bank accounts,
and Moneta merges them, removes duplicates, categorizes every transaction and shows where your money goes:
spending by category, subscriptions, budgets, end-of-month forecast and advice to save more.

Clients: [moneta-app](../moneta-app), a monorepo with the React web app and the Expo (React Native) mobile app.

> Status: early scaffold. See [docs/PLAN.md](docs/PLAN.md) for the design and roadmap.

## Highlights (planned)

- Import any bank's CSV/XLSX through a column-mapping wizard; the format is remembered.
- Duplicate detection across overlapping exports (exact + near matches to review).
- Categorization by rules, then by a Naive Bayes classifier written from scratch in Go, trained on your corrections.
- Recurring payment and subscription detection, budgets, cash-flow forecast, rule-based insights.
- Per-user envelope encryption of transaction labels; data export and account deletion.
- PostgreSQL job queue (`SKIP LOCKED`) for background work.

## Requirements

- Go 1.26+ (`winget install GoLang.Go`)
- PostgreSQL 17+ (Windows installer from postgresql.org, or any Linux package)

## Run

```sh
cp .env.example .env                                     # then fill in the secrets
psql -U postgres -v pw=<POSTGRES_PASSWORD> -f scripts/create-db.sql   # once: moneta role + database
go run ./cmd/moneta                                      # API on :8080
curl http://localhost:8080/api/health
```

To serve the web app from the same process (production), build [moneta-app](../moneta-app)'s web app and point
`MONETA_WEB_DIR` at its `apps/web/dist`: the API then serves the app on `/` and itself on `/api`.

### Optional: Docker

`docker compose up -d --build` starts PostgreSQL, the API and the web app (Caddy) on port 8000, with `../moneta-app`
checked out next to this repo. Handy to try the project without installing Go or PostgreSQL; CI builds both images.

## API

JSON under `/api/v1`. Done so far: `POST /auth/register`, `/auth/login`, `/auth/refresh`, `/auth/logout`,
`GET /me`, CRUD on `/banks`, `/accounts`, `/categories`; imports (`POST /imports` multipart
`file` → preview + suggested mapping, `PUT /imports/{id}/mapping`, `POST /imports/{id}/commit`, `DELETE` to cancel or
undo, `GET /imports`), `/import-profiles`; `GET /transactions` (filters, `q` search, cursor) and `PATCH /transactions/{id}`; `/rules` CRUD
and `POST /rules/preview`; `/stats/summary`, `/stats/categories`, `/stats/merchants`; `/recurring` and transfer pairing; `/budgets`, `/insights`; `/model` and `/transactions/{id}/explain`. Full contract:
[docs/openapi.yaml](docs/openapi.yaml); probes `GET /api/health` and `/api/ready`. Errors look like
`{"error": {"code": "validation", "message": "...", "fields": {...}}}`.

Try it with the synthetic exports in `testdata/`.

## Security model

Labels, merchant names and notes are encrypted with a per-user key, itself wrapped by a master key from the
environment. This protects a leaked database dump or backup; it does not protect against someone who controls the
running server. Deleting an account destroys its key first.
