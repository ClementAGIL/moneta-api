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

- Go 1.24+ (`winget install GoLang.Go`)
- Docker Desktop for the full stack (`winget install Docker.DockerDesktop`), or a local PostgreSQL 17

## Run

```sh
cp .env.example .env          # then fill in the secrets
go run ./cmd/moneta           # API on :8080
curl http://localhost:8080/api/health
```

Full stack (database, API, web app served by Caddy), with `../moneta-app` checked out next to this repo:

```sh
docker compose up -d --build   # then open http://<server>:8000
```

## Security model

Labels, merchant names and notes are encrypted with a per-user key, itself wrapped by a master key from the
environment. This protects a leaked database dump or backup; it does not protect against someone who controls the
running server. Deleting an account destroys its key first.
