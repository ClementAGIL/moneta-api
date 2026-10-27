# Moneta — API plan (Go)

## Context

Moneta is a multi-user spending analyzer. Users import the CSV / Excel exports of all their bank accounts; Moneta
merges them, removes duplicates, categorizes every transaction, and shows where the money goes: spending by category,
subscriptions, budgets, end-of-month forecast, and rule-based advice to save more. Savings and investments are out of
scope (the user tracks them in MyFinanceApp).

Two repos, local git for now:

| Repo                  | Role                                                                                       |
| --------------------- | ------------------------------------------------------------------------------------------ |
| `E:\Repos\moneta-api` | this repo: Go API, PostgreSQL, background jobs; serves the built web app in production    |
| `E:\Repos\moneta-app` | npm-workspaces monorepo: `apps/web` (Vite + React), `apps/mobile` (Expo, Android first),   |
|                       | `packages/shared` (design tokens, API client + types, hooks, formatting); see its PLAN.md |

Decisions taken with the user (2026-10-27):

- **Public multi-user** (sign-up open), hosted on the **home server, natively**: PostgreSQL as a Windows service and
  one `moneta.exe` serving the API on `/api` and the built web app on `/` (same origin, no proxy). Docker was the
  first plan but the PC has hardware virtualization disabled; the Dockerfile and compose file stay as an optional,
  CI-built way to run the project.
- Banks export **CSV and XLSX**. New formats are handled by a **column-mapping wizard**; the mapping is saved as an
  import profile and recognized automatically next time.
- Import from **web upload** and the **Android share sheet**. Original files are **never stored**.
- Duplicates: exact matches skipped automatically, **near-matches reviewed** by the user.
- **Bank → accounts** hierarchy; transfers between the user's own accounts are detected and excluded from spending.
- Categorization: **merchant rules** first; after ~200 user-confirmed transactions, **our own text classifier
  written in Go** (no ML library, no LLM).
- Analyses: spending by category/month, **recurring & subscriptions**, **budgets & alerts**, **cash-flow forecast**.
- Advice: **rule-based insights** only; no data leaves the server.
- Auth: **email + password**, argon2id, JWT access token + rotating refresh token.
- Security: **encrypted sensitive columns** (labels, notes), uploaded files deleted after parsing, **export & delete
  account**.
- MVP = web: auth, banks/accounts, import wizard, dedup, rule categories, monthly dashboard.
- Clients: **React + TypeScript web** and **React Native / Expo mobile** (Android first) in one monorepo, sharing
  design tokens, the API client and data hooks (changed from native Kotlin the same day, to share the design system).

Environment: JDK 17, Node 22.14, Android SDK, Go 1.27, PostgreSQL 18 (`D:\PostgreSQL`, service
`postgresql-x64-18`, shared with Nutrimo; Moneta has its own role and database). Docker Desktop cannot run here.

---

## Stack

| Concern        | Choice                                                     | Why                                               |
| -------------- | ---------------------------------------------------------- | ------------------------------------------------- |
| Language       | Go 1.26+ (required by pgx)                                 | new backend stack for the portfolio               |
| HTTP           | standard `net/http` (1.22 method patterns), own middleware | no framework needed                               |
| DB driver      | `jackc/pgx/v5` (pool)                                      | the reference PostgreSQL driver                   |
| Queries        | hand-written SQL with pgx, one repository file per table   | no ORM; sqlc dropped: needs cgo on Windows        |
| Migrations     | `pressly/goose/v3`, SQL files embedded in the binary       | applied on startup                                |
| Passwords      | `golang.org/x/crypto/argon2` (argon2id)                    | current recommendation                            |
| Tokens         | `golang-jwt/jwt/v5` (HS256) + opaque refresh tokens        |                                                   |
| XLSX           | `xuri/excelize/v2`                                         | reads .xlsx; old .xls is not supported (see below) |
| Encodings      | `golang.org/x/text/encoding/charmap`                       | French banks export Windows-1252                  |
| Logs           | `log/slog` JSON                                            |                                                   |
| Lint / format  | `gofmt`, `go vet`, `golangci-lint`                         |                                                   |
| Database       | PostgreSQL 17+ (18 on the dev PC)                          | features used exist since 13; CI tests on 17      |

Old binary `.xls` (Excel 97) has no maintained pure-Go reader: the wizard asks the user to re-save as .xlsx or CSV.
If one of the user's banks only gives .xls, add a converter step then (LibreOffice headless in the container).

---

## Code layout

```
cmd/moneta/main.go          config → DB open + migrate → job worker goroutine + HTTP server, graceful shutdown
internal/config/            env → Config
internal/db/                pgx pool, goose migrations (embedded from migrations/)
internal/httpapi/           router, middleware (logging, recover, auth, CORS, rate limit, body limit), errors
internal/httpapi/handlers/  one file per resource (auth, accounts, imports, transactions, stats, ...)
internal/auth/              argon2id hashing, JWT issue/verify, refresh-token rotation
internal/vault/             envelope encryption (KEK → per-user DEK), AES-GCM seal/open, blind index (HMAC)
internal/money/             int64 cents, parse "1 234,56" / "-12.30" / "12,30 €", format
internal/importer/          detect (type, encoding, delimiter, header row), grid (CSV/XLSX → [][]string),
                            mapping (profile → rows), values (dates, amounts), fingerprint (header hash)
internal/labels/            normalize labels, extract merchant ("CB CARREFOUR 12/03 PARIS" → "CARREFOUR")
internal/dedup/             exact keys with occurrence index, near-match finder
internal/transfers/         pair opposite movements between the user's own accounts
internal/categorize/        pipeline: user rules → built-in merchant rules → model → uncategorized
internal/ml/                tokenizer, multinomial Naive Bayes, evaluation (holdout accuracy), explain
internal/recurring/         series detection (weekly/monthly/quarterly/yearly), price changes, next date
internal/budgets/           progress per category/month, alert thresholds
internal/forecast/          end-of-month balance projection
internal/insights/          table of insight rules → Insight{kind, severity, params}
internal/jobs/              PostgreSQL job queue (SKIP LOCKED), worker, retries, handlers registry
internal/export/            user data export (ZIP of JSON + CSV)
migrations/                 goose SQL files
docs/openapi.yaml           API contract (the web app generates its TS types from it)
```

---

## Data model (PostgreSQL)

All ids are `uuid` (v7, generated in Go so they sort by time). Amounts are `bigint` cents, signed (negative = money
out). Dates are `date` (booking date as given by the bank). Every user table has `user_id` and every query filters
on it. `created_at` / `updated_at` everywhere.

Encrypted columns are `bytea` (AES-256-GCM, nonce prepended), suffixed `_enc`. Columns ending in `_key` are blind
indexes: `HMAC-SHA256(user index key, normalized value)`, so equality lookups work without decrypting.

```
users            id, email (citext unique), password_hash, dek_wrapped bytea, locale, created_at
refresh_tokens   id, user_id, family_id, token_hash, expires_at, used_at, revoked_at, user_agent
banks            id, user_id, name
accounts         id, user_id, bank_id, name, kind (current|card|other), currency char(3) = 'EUR',
                 anchor_balance_cents, anchor_date          -- user-entered "balance on date", base of all balances
import_profiles  id, user_id, bank_id, name, file_kind (csv|xlsx), encoding, delimiter, sheet, header_row,
                 date_col, date_format, amount_mode (single|debit_credit), amount_col, debit_col, credit_col,
                 decimal_sep, label_cols int[], fingerprint     -- sha256 of the normalized header row
imports          id, user_id, account_id, profile_id, status (uploaded|mapped|committed|cancelled|failed),
                 grid_enc bytea (raw cells, deleted on commit/cancel, expires after 24 h),
                 rows_total, rows_new, rows_duplicate, rows_review, period_start, period_end, error
import_rows      id, import_id, row_index, booked_on, amount_cents, label_enc, dedup_key, occurrence,
                 status (new|duplicate|review|skipped|invalid), match_txn_id, error
transactions     id, user_id, account_id, import_id, booked_on, amount_cents, label_enc, merchant_enc,
                 merchant_key, dedup_key, occurrence, category_id, category_source (rule|builtin|model|user|transfer),
                 confidence real, transfer_id, recurring_id, note_enc
                 index (user_id, booked_on), (account_id, dedup_key), (user_id, merchant_key)
categories       id, user_id (null = default set), parent_id, name, kind (expense|income|transfer), icon, color,
                 excluded_from_stats bool
rules            id, user_id, pattern_enc, match (merchant|contains), sign (any|in|out), category_id, priority,
                 hits, origin (manual|correction)
recurring        id, user_id, merchant_key, display_enc, sign, cadence (weekly|monthly|quarterly|yearly),
                 amount_cents (latest), avg_cents, last_date, next_date, status (active|ended|ignored),
                 is_subscription bool, price_changed_at
budgets          id, user_id, category_id, amount_cents          -- monthly, same every month
insights         id, user_id, kind, period (date), severity (info|warn|alert), params jsonb, dismissed_at
                 unique (user_id, kind, period, params->>'ref')  -- regenerating never duplicates
models           user_id pk, version, trained_at, samples, accuracy, vocab_size, state bytea (gob, encrypted)
jobs             id, kind, user_id, payload jsonb, run_at, attempts, max_attempts, locked_until, last_error, done_at
```

Default categories (seeded, French names, editable per user by copy-on-write): Alimentation (Courses,
Restaurants), Logement (Loyer, Énergie, Internet/Téléphone, Assurance habitation), Transport (Carburant, Transports
en commun, Train/Avion, Péage/Parking), Abonnements (Streaming, Logiciels, Sport), Santé, Shopping, Loisirs, Impôts &
taxes, Frais bancaires, Revenus (Salaire, Remboursements, Autres revenus), Virements internes, Retraits espèces,
Non catégorisé.

---

## Security

- **Passwords:** argon2id (m = 64 MiB, t = 3, p = 2, 16-byte salt), encoded PHC string. Constant-time compare.
- **Access token:** JWT HS256, 15 min, claims `sub`, `exp`, `iat`. Sent as `Authorization: Bearer`.
- **Refresh token:** 32 random bytes, stored as SHA-256, 30 days, **rotated on every use**. Reusing an already-used
  token revokes its whole family (stolen-token detection). Web: `HttpOnly; Secure; SameSite=Strict` cookie scoped to
  `/api/v1/auth`. Mobile: response body, stored in `expo-secure-store` (Android Keystore).
- **Encryption at rest:** `MONETA_MASTER_KEY` (KEK, env) wraps a random per-user DEK (AES-GCM). From the DEK, HKDF
  derives an encryption key and a blind-index key. Encrypted: labels, merchant names, notes, rule patterns, import
  grids, model state. Plain: amounts, dates, categories (needed by SQL aggregates). Deleting a user deletes the DEK
  first: any leftover row or backup becomes unreadable (crypto-shredding).
  Threat model, stated in the README: this protects against a leaked database dump or backup, not against an
  attacker who controls the running server (it holds the KEK).
- **Uploads:** 5 MB max, read in memory, never written to disk; the parsed grid is stored encrypted until commit.
- **Rate limits** (in memory, token bucket): login 5/min per IP + email, register 3/h per IP, import 20/h per user.
- **CORS:** only `MONETA_WEB_ORIGIN`. In production the API serves the web app itself, so they share one origin.
- **GDPR:** `GET /me/export` (ZIP: JSON of everything + one CSV of transactions), `DELETE /me` (password confirm).
- Email verification / password reset need SMTP: later, behind an optional `SMTP_*` config.

---

## Import pipeline

```
upload ──► detect ──► grid (stored encrypted) ──► mapping (profile) ──► parse rows ──► dedup ──► review ──► commit
                                     ▲ fingerprint match → profile pre-filled                         │
                                                                              jobs: transfers, categorize,
                                                                              recurring, insights ◄──┘
```

1. **Detect** (`importer/detect.go`): XLSX by ZIP magic bytes, else CSV. Encoding: UTF-8 (with/without BOM) if
   valid, else Windows-1252. Delimiter: the one of `; , \t |` giving the most consistent column count over the first
   50 lines. Header row: first row whose cells are mostly non-numeric and followed by rows with dates (banks often
   put account info lines above the table).
2. **Profile suggestion:** SHA-256 of the normalized header row. A matching profile pre-fills the mapping and the
   account (profile → bank → last account used).
3. **Mapping** (wizard on the web): date column + format (auto-guessed among `02/01/2006`, `2006-01-02`,
   `02-01-2006`, `02/01/06`, Excel serial numbers), amount (single signed column, or debit + credit columns),
   decimal separator, label = one or several columns joined.
4. **Parse:** each row → `{booked_on, amount_cents, label}` or a per-row error (shown, not fatal). Lines without a
   date (sub-totals, "Solde au …") are skipped as `invalid`.
5. **Dedup** (`dedup/`):
   - `dedup_key = HMAC(account_id | date | amount | normalized label)`. Identical rows are legitimate (two coffees
     the same day), so each row also gets an **occurrence index** within its file. Row `(key, n)` is a duplicate if
     the account already has ≥ n transactions with that key.
   - **Near-match:** same account and amount, date within ±3 days, and either a different label or the exact key
     missing → `review`, linked to the candidate. (Banks change the date between "pending" and "booked" exports and
     sometimes shorten labels.)
6. **Review:** user keeps or skips each `review` row (web and Android).
7. **Commit:** one SQL transaction inserts the new rows, sets the import counters, deletes the grid; then queues the
   `import.committed` job.
8. **Undo:** `DELETE /imports/{id}` deletes the transactions of a committed import (with confirmation).

Card accounts with deferred debit: the current account shows one monthly "RELEVE CARTE" line that duplicates the card
account's detail. Detected like a transfer (amount = sum of the card account over the period), excluded from stats.

---

## Labels and merchants

`labels.Normalize`: uppercase, strip accents, collapse spaces, drop French bank prefixes (`CB`, `CARTE X1234`,
`PAIEMENT PAR CARTE`, `PRLV SEPA`, `VIR SEPA RECU`, `VIR INST`, `RETRAIT DAB`, `FACTURE CARTE DU 120324`...), dates
(`12/03`, `120324`), card numbers and long references (≥ 6 digits, mixed id tokens).
`labels.Merchant`: first significant 1–3 tokens of the normalized label, minus a city-name list and legal suffixes
(`SAS`, `SARL`, `FR`). These two functions drive dedup, rules, recurring detection and the classifier, so they are
the most important code to get right; tune them on the user's real files.

---

## Categorization

Pipeline (`categorize.Categorize(tx) → (category, source, confidence)`), first hit wins:

1. **User rules** by priority (`merchant` equals, or label `contains`, optional sign).
2. **Built-in rules**: Go table of ~300 common French merchants and keywords (`CARREFOUR`→Courses, `SNCF`→Train,
   `NETFLIX`→Streaming, `URSSAF`, `DGFIP`→Impôts, `EDF`/`ENGIE`→Énergie, `FRAIS`/`COTISATION`→Frais bancaires...).
3. **Model** (once active, see below) if confidence ≥ 0.6.
4. **Non catégorisé**.

Transactions categorized by the user (`source = user`) are never overwritten. A **correction** in the UI asks "apply
to all <merchant>?": yes → creates a `correction` rule and re-categorizes the merchant's non-user transactions.
Recategorization after rule changes runs as a job.

### Classifier (`internal/ml`, own implementation)

- **Features:** tokens of the normalized label (words, word bigrams, char 3-grams of the merchant) + sign + amount
  bucket (log scale) + weekday/month-day bucket (rent on the 1st, salary at month end).
- **Model:** multinomial Naive Bayes with Laplace smoothing, per user. Counts update **incrementally** on each
  correction (cheap), full retrain nightly as a job. Stored as encrypted gob in `models`.
- **Activation:** after ≥ 200 user-confirmed transactions (user-set or accepted rule results), and only if its
  holdout accuracy (last 20 % by date) beats the rules-only accuracy on the same holdout. `GET /model` exposes
  samples, accuracy of both, and state.
- **Explain:** top 3 features by log-likelihood ratio, returned with the category ("because: CARREFOUR, card,
  < 50 €") and shown in the UI.
- Later, as a portfolio comparison: logistic regression (SGD) in the same package, chosen by holdout score.

---

## Analyses

- **Stats** (SQL aggregates): spending/income per category per month, excluding `transfer` and
  `excluded_from_stats`; top merchants (group by `merchant_key`, decrypt display names in Go); savings rate
  `(income − spending) / income`.
- **Transfers** (`transfers/`): between two different accounts of the user, amounts opposite, dates within 3 days,
  one candidate only → paired, both set to "Virements internes". Ambiguous cases are left alone.
- **Recurring** (`recurring/`): group by `(merchant_key, sign)`, sort by date, intervals classified as weekly
  (7 ± 2 d), monthly (30 ± 4), quarterly (91 ± 10), yearly (365 ± 15); ≥ 3 occurrences (2 for yearly) and amount
  within ±15 % of the median. Next date = last + cadence. Price change = last amount differs > 3 % from the previous.
  Ended = next date passed by more than half a cadence. Subscription = recurring expense in Abonnements, or a
  monthly fixed amount < 100 € the user confirms.
- **Budgets:** monthly amount per category; progress = month-to-date spending; alerts at 80 % and 100 % become
  insights.
- **Forecast** (`forecast/`): per account and total. Current balance = anchor balance ± transactions since the
  anchor date. End of month = current + remaining expected recurring items this month + median daily variable
  spending (last 90 days, recurring excluded) × days left. Returns a daily series with a p25–p75 band.
- **Insights** (`insights/`), a lookup table `kind → func(ctx, user, period) []Insight`:
  - category spending > 6-month average by +30 % and +30 € (and the reverse, as praise)
  - new subscription, subscription price increase, several subscriptions in the same subcategory
  - budget at 80 % / exceeded
  - bank fees this year
  - largest expenses of the month
  - savings rate vs. previous 6 months, suggested amount to put aside
  - forecast below zero before the end of the month
  Insights carry a `kind` and `params`; the clients turn them into sentences (localized there).

---

## Background jobs

`jobs` table, worker goroutines in the server process: `SELECT … FOR UPDATE SKIP LOCKED` on due jobs, exponential
backoff (`30s × 2^attempts`), `max_attempts = 5`, `last_error` kept. Kinds:
`import.committed` (transfers → categorize → recurring → insights for that user), `categorize.user` (after rule
changes), `model.retrain` (nightly per active user), `insights.monthly` (1st of the month), `user.delete`,
`imports.expire` (cancel uncommitted imports after 24 h). A tiny scheduler inserts the periodic jobs.

---

## HTTP API (`/api/v1`, JSON, documented in `docs/openapi.yaml`)

Errors: `{"error": {"code": "validation", "message": "...", "fields": {...}}}`. Lists use cursor pagination
(`?cursor=&limit=`, cursor = base64 of `(booked_on, id)`).

| Area         | Endpoints                                                                                           |
| ------------ | --------------------------------------------------------------------------------------------------- |
| Health       | `GET /api/health`, `GET /api/ready` (DB ping), outside `/v1` so probes never change             |
| Auth         | `POST /auth/register`, `POST /auth/login`, `POST /auth/refresh`, `POST /auth/logout`                |
| Me           | `GET /me`, `PATCH /me` (locale, password), `GET /me/export`, `DELETE /me`                           |
| Banks        | `GET/POST /banks`, `PATCH/DELETE /banks/{id}`                                                       |
| Accounts     | `GET/POST /accounts`, `PATCH/DELETE /accounts/{id}` (incl. anchor balance)                          |
| Imports      | `POST /imports` (multipart file → detection + preview + suggested profile/account),                 |
|              | `PUT /imports/{id}/mapping`, `GET /imports/{id}` (summary + review rows),                           |
|              | `POST /imports/{id}/commit` (review decisions), `DELETE /imports/{id}` (cancel or undo), `GET /imports` |
| Profiles     | `GET /import-profiles`, `PATCH/DELETE /import-profiles/{id}`                                        |
| Transactions | `GET /transactions?account=&from=&to=&category=&uncategorized=&q=`, `PATCH /transactions/{id}`      |
|              | (category, note, `applyToMerchant`), `POST /transactions/{a}/transfer/{b}`, `DELETE …/transfer`     |
| Categories   | `GET/POST /categories`, `PATCH/DELETE /categories/{id}`                                             |
| Rules        | `GET/POST /rules`, `PATCH/DELETE /rules/{id}`, `POST /rules/preview` (matching transactions)        |
| Stats        | `GET /stats/summary?month=`, `GET /stats/categories?from=&to=`, `GET /stats/merchants?from=&to=`    |
| Recurring    | `GET /recurring`, `PATCH /recurring/{id}` (ignore, mark subscription)                               |
| Budgets      | `GET/PUT /budgets`, `GET /budgets/progress?month=`                                                  |
| Forecast     | `GET /forecast?account=`                                                                            |
| Insights     | `GET /insights?unread=`, `POST /insights/{id}/dismiss`                                              |
| Model        | `GET /model`, `POST /model/retrain`                                                                 |

Free-text search `q` runs in Go over the user's decrypted labels in the date range (a few thousand rows a year).

---

## Milestones (one or more commits each, CLAUDE.md + README updated each time)

| #   | Milestone                                                                                     | With web     |
| --- | --------------------------------------------------------------------------------------------- | ------------ |
| M0  | ✅ Scaffold: `/api/health`, web app serving, Dockerfile, compose, CI, docs                     | W0           |
| M1  | ✅ DB: pgx pool, goose embedded migrations, `.env` loading, `/api/ready`, job queue + worker   |              |
| M2  | ✅ Auth: users, argon2id, JWT, refresh rotation, auth middleware, rate limits, crypto (KEK/DEK)  | W1           |
| M3  | ✅ Banks, accounts, default categories                                                           | W2           |
| M4  | ✅ Import: detect, grid, profiles, mapping, parse, dedup, review, commit, undo                   | W3           |
| M5  | ✅ Labels/merchants, built-in + user rules, corrections, transactions list/search                | W4           |
| M6  | Stats endpoints → **MVP complete**                                                            | W5           |
| M7  | Transfers + recurring + subscriptions                                                         | W6           |
| M8  | Budgets + insights + scheduler                                                                | W7           |
| M9  | Classifier (Naive Bayes, holdout, explain, activation)                                        | W8           |
| M10 | Forecast                                                                                      | W9           |
| M11 | Export / delete account, OpenAPI complete, hardening (limits, timeouts, headers)              | W10          |

The mobile milestones (A1–A6 in `moneta-app/docs/PLAN.md`) only consume existing endpoints and can start after M6.

Before M4: get one anonymized sample file per bank of the user (a few rows, labels altered) into `testdata/` to
design detection and label normalization against real formats.

## Verification

No unit tests by default (user's rule). Verify with `go vet`, `golangci-lint`, `curl`/HTTPie scripts in
`docs/requests.http`, and real imports of the user's files. Possible exception to discuss with the user: table tests
for `importer`, `money`, `labels`, `dedup` and `ml`, which are pure functions and the most error-prone.
