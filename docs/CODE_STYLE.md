# Mobius Go Engineering & Code Style Guide

This document defines the AWS-style Go engineering standards for **Mobius**. All contributions across `cmd/`, `pkg/`, `internal/`, and `examples/` must follow these conventions.

---

## 1. Repository Layout & Package Boundaries

* **Public SDK (`pkg/`) vs. Private Runtime & UI (`internal/`)**:
  * `pkg/` exposes the stable, reusable Go SDK (`agent`, `agentctx`, `events`, `llm`, `orchestrator`, `roles`, `sensors`, `session`, `tools`). External Go callers may import `pkg/*`.
  * `internal/` encapsulates private runtime mechanics and terminal UI (`id`, `redact`, `budget`, `artifact`, `guides`, `cli`, `tui`, `tui/theme`, `tui/markdown`). Go's `internal/` compiler boundary prevents external callers from coupling to private application internals.
* **Strict Dependency DAG**: Packages across `pkg/` and `internal/` form a directed acyclic graph.
  * Leaf and foundational packages (`internal/id`, `internal/redact`, `internal/budget`, `internal/artifact`, `internal/guides`, `pkg/events`, `pkg/llm`, `pkg/tools`, `pkg/roles`, `pkg/sensors`, `pkg/agentctx`, `pkg/session`) must never import higher-level coordination (`pkg/orchestrator`) or presentation packages (`internal/cli`, `internal/tui`).
  * Application orchestration (`pkg/orchestrator`) coordinates `pkg/agent`, `pkg/roles`, `pkg/session`, `pkg/tools`, `pkg/llm`, `pkg/events`, `pkg/sensors`, and `internal/guides`, and must never import `internal/cli` or `internal/tui`.
  * Presentation layers (`internal/cli`, `internal/tui`, `internal/tui/theme`, `internal/tui/markdown`) and `cmd/mobius` sit at the top of the dependency tree and wire application components together.
* **No Stray Root `main` Files**: Only `cmd/<binary>/main.go` belongs as a primary executable entrypoint. Runnable demonstrations and scratch previews belong in `examples/<name>/main.go`, never in the repository root.
* **Clean Build Artifacts**: Compiled binaries belong in `bin/` (gitignored), never in the repository root.

---

## 2. Package & Symbol Documentation

* **Package `doc.go`**: Every package under `pkg/` and `internal/` must include a `doc.go` file with a crisp package comment (`// Package <name> ...`) stating:
  1. The package's single responsibility and scope.
  2. Explicit concurrency guarantees (e.g., whether exported types are safe for concurrent use, stateless, or require external synchronization).
* **Godoc Conventions**: Follow Go / AWS Go SDK godoc style on all exported types, constants, variables, functions, and methods:
  * Begin every doc comment with the exact identifier name (`// NewClient creates ...`, `// ErrBudgetExceeded indicates ...`).
  * Document preconditions, ownership/lifetime expectations (e.g., callers must call `Close()`), and error semantics rather than restating obvious syntax.
  * Keep comments free of decorative emojis or informal placeholders.

---

## 3. Cohesive Files & Small Functions

* **Single Responsibility**: Group types and helpers into small, cohesive files aligned by domain concern (e.g., separating transport/client setup, request encoding, stream parsing, and error types).
* **File Size Discipline**: Split files cleanly along logical boundaries when a source file approaches or exceeds ~350–450 lines.
* **Shallow Nesting & Early Returns**: Prefer guard clauses and early returns over deep `if/else` nesting. Extract repeated logic into well-named, single-purpose helper functions.
* **Uniform Variant Handling**: Use table-driven mappings or shared code paths instead of duplicating `if/else` or `switch` branches for parallel variants.

---

## 4. Error Handling & Defensive I/O

* **Explicit Error Wrapping**: Wrap errors with context using `fmt.Errorf("...: %w", err)` so callers can inspect chains via `errors.Is` and `errors.As`. Never silently discard returned errors; if intentionally ignored (such as a best-effort cleanup after an earlier failure), document why.
* **Sentinel Errors**: Define package-level exported sentinel errors (`var Err... = errors.New(...)`) for conditions callers need to branch on programmatically.
* **Bounded I/O**: Cap all untrusted or external reads—including HTTP response bodies, configuration files, YAML/TOML inputs, and tool outputs—with `io.LimitReader` or explicit size constants (`Max...Bytes`) before buffering into memory.
* **Explicit `defer` Cleanup**: Close files, HTTP response bodies, and locks immediately after acquisition checks using `defer` (checking or logging close errors on writable streams where data loss could occur).
* **Defensive Nil Guards**: Validate pointer, interface, and slice inputs at package boundaries (`if cfg == nil`, `if provider == nil`) and return explicit errors rather than panicking.

---

## 5. Network & Timeout Discipline

* **`context.Context` First**: Any function performing I/O, network requests, subprocess execution, or blocking waits must accept `ctx context.Context` as its first parameter and honor cancellation and deadlines.
* **Hardened `http.Transport`**: Configure explicit `http.Transport` timeouts rather than relying on `http.DefaultClient` or `http.DefaultTransport`:
  * Bounded dial timeout (`net.Dialer{Timeout: ...}`)
  * `TLSHandshakeTimeout`
  * `ResponseHeaderTimeout`
  * `IdleConnTimeout` and connection pool limits
* **Streaming / SSE Discipline**: Never set a wall-clock `http.Client.Timeout` on long-lived Server-Sent Events (SSE) or streaming connections, as `Client.Timeout` aborts active streams mid-response. Enforce per-request cancellation via `ctx` and bound connection setup with `Transport` dial, TLS, and response-header timeouts.

---

## 6. Concurrency & Verification

* **Mutex Ownership**: Whenever a struct holds a `sync.Mutex` or `sync.RWMutex`, place the mutex directly above the fields it protects and document ownership in a field comment (e.g., `// mu guards sessions and activeID.`).
* **Never Hold Locks Across I/O**: Copy or snapshot needed state under lock, release the mutex, and perform network calls, disk I/O, or channel/callback invocations without holding locks.
* **Mandatory Verification Gates**: Run the following checks before submitting or finalizing any Go change:
  * `gofmt -w -s <package>` — enforce canonical Go formatting and simplified syntax.
  * `go test -race -count=1 ./...` — detect data races and stale test caches.
  * `go vet ./...` — catch static correctness issues.
  * `golangci-lint run ./...` — verify lint, error-check, and style compliance.
