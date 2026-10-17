# Upwatch

URL monitoring service in Go, deployed on Kubernetes (k3d) with Terraform, Helm, GitHub Actions and Prometheus/Grafana. A showcase Platform/DevOps project; full spec in `docs/SPEC.md`.

## Working rules
- **Language**: everything we write is in **English** (code, comments, commit messages, docs, README, ADRs), even though the spec (`docs/SPEC.md`) is in French.
- **Learning project**: the author is here to learn, so do **not** do the work for them. Guide them on *what* to do and *why* (next step, files to create, concepts, pitfalls, small hints), explain concepts when needed, and let them write the code. Only write code if they explicitly ask for it; review what they wrote instead. Always include links to the relevant official documentation (pkg.go.dev, go.dev, Kubernetes/Helm/Terraform docs, etc.) as resources.
- Go **step by step** following the plan in the spec (section 12); never generate everything at once.
- After each step run what applies: `go test ./...`, `golangci-lint run`, `helm lint`, `terraform validate`.
- Idiomatic Go: wrap errors (`fmt.Errorf("...: %w", err)`), propagate `context.Context` everywhere, no globals except metrics, interfaces defined on the consumer side.
- Stdlib first: `net/http` routing, `log/slog` JSON, no web framework, no ORM.
- No dependency added without a short justification in the commit message.
- Pin every version (images, charts, providers, GitHub Actions).
- Small commits, Conventional Commits (`feat:`, `fix:`, `chore:`, `docs:`).
- Briefly explain each non-trivial choice so the author can defend it in an interview.

## Layout
- `cmd/upwatch` wiring, config, startup, graceful shutdown
- `internal/{api,checker,store,metrics,config}` application code
- `deploy/{k3d,terraform,helm/upwatch,grafana}` infrastructure
- `docs/` architecture and ADRs
