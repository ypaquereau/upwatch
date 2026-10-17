# Step 2: data access (`internal/store`)

**Goal:** one package that owns every interaction with PostgreSQL: connection pool, schema migrations, and the queries the API and the worker need.

**Files:** `internal/store/{store,migrate,targets,checks}.go`, their tests, and `internal/store/migrations/001_init.sql`
**Spec:** `docs/SPEC.md` §3.1, §3.2, §3.4

## What we built

| Piece | What it does |
|---|---|
| `New`, `Ping`, `Close` | Open a `pgxpool` pool and fail fast if the database is unreachable |
| `Migrate` | Apply embedded SQL files once each, in one transaction, serialised by an advisory lock |
| `CreateTarget` | Insert, map unique violation to `ErrDuplicate` |
| `GetTarget`, `DeleteTarget` | Map "no row" to `ErrNotFound` |
| `RecordCheck`, `RecentChecks` | Store and read probe results (nullable columns as pointers) |
| `ListTargets` | Each target with its latest check (`LEFT JOIN LATERAL`) |
| `DueTargets` | The worker's polling query |
| `TargetUptime` + `Uptime.Percent` | Count in SQL, compute the percentage in Go |
| `DeleteChecksBefore` | Retention |

## Key concepts

### 1. The store is the only package that knows PostgreSQL
`api` and `checker` never write SQL or import `pgx`. They get Go types and sentinel errors. This keeps the rest testable (each consumer will define the small interface it needs, so a fake store replaces the database in unit tests) and means a database change touches one package.

### 2. Connection pool and fail-fast startup
Opening a connection is expensive, so a service keeps a pool and lends connections to queries. `pgxpool.New` is **lazy**: it succeeds even with a wrong password. `New` therefore pings, and closes the pool if the ping fails. In Kubernetes a pod that cannot reach its database should crash at startup (visible in `kubectl get pods`) instead of looking healthy and failing on the first request.

### 3. Migrations
The database keeps its data between releases, so schema changes are versioned files applied once each. Our runner:
1. takes `pg_advisory_xact_lock`: if two pods start together (rollout), the second waits, then sees the first one's rows and skips them;
2. creates `schema_migrations` if needed;
3. for each embedded file not yet recorded, runs it and records its name, all in **one transaction**. PostgreSQL has transactional DDL, so a failure rolls back everything.

Consequences worth remembering:
- migration files contain **no** `BEGIN`/`COMMIT`: the runner owns the transaction;
- an applied file is never edited, changes go in a new file;
- `//go:embed` compiles the SQL into the binary, so the distroless image needs nothing but the executable.

### 4. Domain errors instead of driver errors
| Database says | Store returns | API will map to |
|---|---|---|
| SQLSTATE `23505` (`*pgconn.PgError`) | `ErrDuplicate` | 409 |
| `pgx.ErrNoRows` (reads) / 0 rows affected (delete) | `ErrNotFound` | 404 |

Errors are wrapped with `%w` (context for logs) and detected with `errors.Is` / `errors.As`. The sentinels are package-level `var`s: immutable error values, the accepted exception to "no globals".

### 5. NULL handling
`status_code`, `latency_ms` and `error` are NULL when they do not apply (network error). In Go they are `*int` / `*string`: `nil` means NULL. Scanning NULL into a plain `int` fails.

### 6. `LEFT JOIN LATERAL` for "latest row per parent"
```sql
FROM targets t
LEFT JOIN LATERAL (
    SELECT ... FROM checks WHERE target_id = t.id ORDER BY checked_at DESC LIMIT 1
) c ON true
```
The subquery runs once per target and can reference `t`. `LEFT` keeps targets with no checks (their `c.*` columns are NULL). The index `(target_id, checked_at DESC)` makes each lookup an index probe. `DueTargets` reuses the same shape.

### 7. Count in SQL, compute in Go
`count(*) FILTER (WHERE success)` gives the numbers; `Uptime.Percent()` is a pure method, so the division and the "no checks" case are unit-tested without a database. Callers can tell "no data" (`Checks == 0`) from "all failed".

### 8. Time and determinism
`RecordCheck` takes `CheckedAt` from the caller instead of using `now()`, and `TargetUptime`/`DeleteChecksBefore` take explicit times. Tests can then build any history they want. (`DueTargets` is the exception: it compares against the database `now()`, which is the clock that matters for the worker.)

### 9. Testing against a real database
SQL can only be validated by PostgreSQL, so store tests run against `TEST_DATABASE_URL` and are skipped when it is unset (`go test ./...` stays green without Docker). Each test drops and recreates the schema, so **never point it at a database you care about**.

```
docker run --rm -d --name upwatch-pg -e POSTGRES_PASSWORD=dev -e POSTGRES_DB=upwatch -p 5432:5432 postgres:17
TEST_DATABASE_URL=postgres://postgres:dev@localhost:5432/upwatch go test -race -count=1 ./internal/store
```
`-count=1` bypasses the test cache, which would otherwise hide changes in external state.

## Gotchas met on the way

- `import { ... }` is wrong, grouped imports use parentheses.
- `default:":8080"` after a field is a struct tag, not a default value.
- `BEGIN`/`COMMIT` inside a migration file breaks the runner's transaction.
- `docker run --rm` deletes the container on `docker stop`; `docker start` then has nothing to start.
- Importing pgx raised the module's Go version to 1.25: the Dockerfile and CI must use Go 1.25 or newer.
- `go mod tidy` is needed after the first import of a new package (missing `go.sum` entries).
- `UnmarshalText`-style methods and `Scan` need addressable destinations: pass `&field`.

## Decisions and trade-offs

| Decision | Why | Trade-off |
|---|---|---|
| `pgx/v5` + `pgxpool`, no ORM, no `database/sql` | Maintained native driver, explicit SQL | Manual scanning |
| Own ~60-line migration runner | No extra dependency, shows how migrations work | No down-migrations, no CLI. `golang-migrate` if needs grow |
| One transaction for all pending migrations | Atomic, simple | Long migrations hold the lock longer |
| Defaults and validation in the API, DB `CHECK` as safety net | One owner for business rules | The store trusts its input |
| Sentinel errors in the store | API stays free of pgx | Two package-level vars |
| `Uptime` returns counts, not a percentage | Distinguishes "no data" from "0%" | One extra method |
| Retention `DELETE` scans the table | Hourly job, 7 days of data | A separate `checked_at` index would fix it if it ever mattered |

## Resources

**pgx and PostgreSQL**
- [pgx v5](https://pkg.go.dev/github.com/jackc/pgx/v5), [pgxpool](https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool), [pgconn.PgError](https://pkg.go.dev/github.com/jackc/pgx/v5/pgconn#PgError)
- [PostgreSQL error codes](https://www.postgresql.org/docs/current/errcodes-appendix.html)
- [INSERT ... RETURNING](https://www.postgresql.org/docs/17/dml-returning.html)
- [SELECT: LATERAL, DISTINCT ON](https://www.postgresql.org/docs/17/sql-select.html)
- [Aggregate FILTER](https://www.postgresql.org/docs/17/sql-expressions.html#SYNTAX-AGGREGATES)
- [Advisory locks](https://www.postgresql.org/docs/17/explicit-locking.html#ADVISORY-LOCKS)
- [Multicolumn indexes](https://www.postgresql.org/docs/17/indexes-multicolumn.html)
- [Transactional DDL](https://wiki.postgresql.org/wiki/Transactional_DDL_in_PostgreSQL:_A_Competitive_Analysis)

**Go**
- [`embed`](https://pkg.go.dev/embed)
- [`errors`](https://pkg.go.dev/errors) and [Working with Errors in Go 1.13](https://go.dev/blog/go1.13-errors)
- [Go blog: Context](https://go.dev/blog/context)
- [Go modules reference](https://go.dev/ref/mod)
- [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments)

**Security**
- [OWASP: SQL injection prevention](https://cheatsheetseries.owasp.org/cheatsheets/SQL_Injection_Prevention_Cheat_Sheet.html)

## Questions to test yourself

1. Why does `New` ping after `pgxpool.New`, and why close the pool if the ping fails?
2. What goes wrong if a migration file contains `COMMIT`?
3. Two pods start at the same time. How does the runner avoid applying a migration twice?
4. Why does the API receive `ErrDuplicate` and not a `*pgconn.PgError`?
5. What does `LEFT JOIN LATERAL` give you here that a plain `LEFT JOIN` could not?
6. Why is `StatusCode` a `*int`?
7. Why is `Uptime.Percent()` separate from the SQL query?
8. Why do store tests pass `CheckedAt` explicitly instead of relying on `now()`?
