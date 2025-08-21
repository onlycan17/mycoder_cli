# Repository Guidelines

## Project Structure & Module Organization
- `cmd/mycoder`: CLI entrypoint (`main.go`) and subcommands.
- `internal/`: application packages (server, llm, indexer, store, models, version).
- `docs/`: product, API, and architecture docs.
- `scripts/`: developer scripts (e.g., pre-commit hook).
- `bin/`: build artifacts (ignored by VCS).
- Tests live alongside packages as `*_test.go` under `internal/`.

## Build, Test, and Development Commands
- `make fmt`: format Go code (`gofmt -s -w .`).
- `make fmt-check`: verify formatting without writing.
- `make lint`: static checks via `go vet ./...`.
- `make test`: run all tests `go test ./...`.
- `make build`: build binary to `bin/mycoder`.
- `make run`: build then start server on `:8089`.
- `make hook-install`: install `.git/hooks/pre-commit`.

## Coding Style & Naming Conventions
- Go 1.21+. Format with `gofmt -s`; no manual style deviations.
- Packages: lowercase, short, meaningful (e.g., `server`, `store`).
- Exported identifiers: `CamelCase`; unexported: `camelCase`.
- Errors: wrap with context `fmt.Errorf("context: %w", err)`; early returns to reduce nesting.
- Keep functions focused and small; prefer composition over large types.

## Testing Guidelines
- Framework: standard `testing` with table-driven tests for logic.
- Location: `internal/<pkg>/*_test.go`.
- Naming: `TestXxx`, helpers unexported. Use subtests for cases.
- Run: `make test` (optionally `go test -cover ./...`).

## Commit & Pull Request Guidelines
- Commits: imperative mood, concise scope-first subject (e.g., "server: add SSE progress").
- Squash noisy WIP commits before merging.
- PRs: include purpose, key changes, screenshots/logs for UX/CLI, and linked issues.
- Requirements: CI green (`make fmt-check && make lint && make test`), no flaky tests, docs updated (`docs/*`) when behavior changes.

## Security & Configuration Tips
- Sensitive config via env vars: `MYCODER_OPENAI_API_KEY`, `MYCODER_OPENAI_BASE_URL`, `MYCODER_SERVER_URL`, policy regexes (`MYCODER_SHELL_DENY_REGEX`, `MYCODER_FS_ALLOW_REGEX`), storage `MYCODER_SQLITE_PATH`.
- Never hardcode secrets; prefer `.env`-style local files ignored by Git.
- Validate and sanitize user inputs for CLI and server endpoints.
