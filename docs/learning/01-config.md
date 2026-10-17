# Step 1: configuration (`internal/config`)

**Goal:** turn environment variables into one typed, validated `Config` struct, loaded once at startup.

**Files:** `internal/config/config.go`, `internal/config/config_test.go`
**Spec:** `docs/SPEC.md` §3.6

## What we built

- A `Config` struct with 8 fields, each stored in the type the rest of the app wants (`time.Duration`, `int`, `bool`, `slog.Level`) instead of the raw string.
- `Load(getenv func(string) string) (Config, error)`: starts from defaults, overrides each field from the environment only when the variable is non-empty, then validates.
- A table-driven test (`TestLoad`) that runs `Load` against a fake environment.

## Key concepts

### 1. Parse once, store the right type
Environment variables are always strings. If the struct kept `"5s"` or `"info"`, every consumer would parse and validate it again. Converting once in `Load` means a bad value fails at startup, with a clear message, instead of deep inside the worker later.

### 2. Defaults live in code, not in struct tags
`default:":8080"` after a field is not a feature of Go. A struct tag is just a string that a library may read through reflection. We use no such library (stdlib first), so defaults are plain values in `Load`:

```go
cfg := Config{HTTPAddr: ":8080", WorkerEnabled: true, ...}
if v := getenv("HTTP_ADDR"); v != "" {
    cfg.HTTPAddr = v
}
```

An empty string means "unset", so the default stays. This matters for booleans: `os.Getenv("X") == "true"` would turn an unset `WORKER_ENABLED` into `false`, while the spec says the default is `true`.

### 3. Dependency injection through a function parameter
`Load` receives `getenv` instead of calling `os.Getenv`. Production passes `os.Getenv`, tests pass a map-backed fake. Benefits: no `os.Setenv` in tests, no shared global state, tests can run in parallel. It is the same idea as "interfaces defined on the consumer side", applied to a single function.

### 4. Error wrapping with `%w`
Every error names the variable and wraps the cause:

```go
return Config{}, fmt.Errorf("parsing WORKER_CONCURRENCY: %w", err)
```

`%w` keeps the original error reachable through `errors.Is` / `errors.As`, while the message tells whoever reads the pod log which variable is wrong.

### 5. Multi-value returns do not fit in a struct literal
`strconv.Atoi` and `time.ParseDuration` return `(value, error)`. Struct literals take one value per field, so parsing needs separate statements where `err` can be checked.

### 6. Table-driven tests
One slice of cases (name, input, expected result or expected error), one loop with `t.Run`. Adding a case costs a few lines, and a failure names the case. For error cases we assert that the message *contains* the variable name, not the full text, so rewording a message does not break the tests.

## Small Go details worth remembering

- **Naming:** exported fields are CamelCase and initialisms stay capitalized (`HTTPAddr`, `DatabaseURL`). `HTTP_ADDR` is the env var name, never a Go identifier.
- **Grouped declarations use parentheses**: `import ( ... )`. Curly braces are for blocks, bodies and struct definitions.
- **`UnmarshalText` has a pointer receiver:** `cfg.LogLevel.UnmarshalText(...)` modifies the field in place because `cfg` is addressable and Go takes `&cfg.LogLevel` for you.
- **`if x, err = f(); err != nil`** (with `=`, not `:=`) assigns into an existing variable or field without shadowing it.
- **Package comments:** `revive` requires `// Package config ...` above `package config` (and `// Command upwatch ...` for a `main` package).
- **`got != tt.want`** only compiles while `Config` has comparable fields. A slice or map field would require `reflect.DeepEqual` or `go-cmp`.

## Decisions we made (and how to defend them)

| Decision | Why | Trade-off |
|---|---|---|
| Stdlib only, no `envconfig`/`viper` | 8 variables do not justify a dependency; the code stays readable | More lines, but explicit |
| Return the first error | Simplest to read and test | A user fixing several variables needs several restarts. `errors.Join` is a small upgrade if it hurts |
| `Config` returned by value | Small struct, no nil check for callers | Copied on each pass; irrelevant at this size |
| Required `DATABASE_URL` has no default | A silent default DSN would hide a misconfiguration | The app refuses to start without it, which is the point |

## Checks run

```
gofmt -l .
go vet ./...
go test -race -cover ./...      # 97.4% on internal/config
golangci-lint run               # 0 issues
```

## Resources

**Go**
- [A Tour of Go](https://go.dev/tour/)
- [Effective Go](https://go.dev/doc/effective_go)
- [Go Code Review Comments: initialisms](https://go.dev/wiki/CodeReviewComments#initialisms)
- [Go Doc Comments](https://go.dev/doc/comment)
- [Go spec: import declarations](https://go.dev/ref/spec#Import_declarations)
- [`time.ParseDuration`](https://pkg.go.dev/time#ParseDuration)
- [`strconv`](https://pkg.go.dev/strconv)
- [`log/slog`](https://pkg.go.dev/log/slog) and [`Level.UnmarshalText`](https://pkg.go.dev/log/slog#Level.UnmarshalText)
- [`errors`](https://pkg.go.dev/errors)

**Errors**
- [Working with Errors in Go 1.13](https://go.dev/blog/go1.13-errors)

**Testing**
- [`testing`](https://pkg.go.dev/testing)
- [Go wiki: TableDrivenTests](https://go.dev/wiki/TableDrivenTests)

**Design**
- [The Twelve-Factor App: Config](https://12factor.net/config)
- [Go Proverbs](https://go-proverbs.github.io/)

**Tooling**
- [golangci-lint linters](https://golangci-lint.run/usage/linters/)

## Questions to test yourself

1. Why does `Load` take `getenv` instead of calling `os.Getenv`?
2. Why is `LogLevel` a `slog.Level` and not a `string`?
3. What would go wrong with `WorkerEnabled: os.Getenv("WORKER_ENABLED") == "true"`?
4. What does `%w` give you that `%v` does not?
5. When would you switch from "first error" to `errors.Join`?
