# Development

## Setup

Requires Go (version in `go.mod`), and optionally Chrome/Chromium plus `golangci-lint`.

```sh
make hooks     # pre-commit: gofmt, vet, lint. pre-push: race tests, govulncheck
make mock      # fake Strava and Dexcom data on http://127.0.0.1:8090 (login: demo@example.test / demo-password-123)
make dev       # REAL services, go run, data in .data (settings from .env, see .env.example)
make run       # same with the built binary
make help      # all targets (cli, token, dexcom, strava-cookies, strava-check, strava-list, reset-*)
make check     # everything CI runs
```

Set `CHROME_PATH` to run the browser tests; without a Chrome they are skipped. On CI the Chrome-launching tests can rarely fail with `websocket url timeout reached`; that is a runner flake, re-run the job. `make shot` (needs `CHROME_PATH` and a running demo) writes screenshots, `make mailshot` renders sample emails to `docs/img`, `make site` previews the project site in `docs/site` (published by `.github/workflows/pages.yml` once GitHub Pages is set to "GitHub Actions"), and `go run ./tools/shot -flow` clicks through the main UI paths.

## Layout

| Path | Purpose |
|---|---|
| `cmd/glucava` | wiring, CLI commands (`token`, `strava`, `dexcom`, `user`, `secrets`, `config`, `glucose`, `data`), env tuning (`tuning.go`), source registry (`wire.go`) |
| `internal/jobs` | pipeline (`Processor`), single-worker `Queue` with retry, interfaces (`Store`, `Writer`) |
| `internal/glucose` | `Source` interface, Dexcom Share client, file importers (`importers/`) |
| `internal/canary` | daily dry run of the edit page |
| `internal/ingest` | stores live readings continuously |
| `internal/stats`, `internal/render` | TIR/min/max/avg, sparkline, description block and merge/strip |
| `internal/strava` | chromedp writer, cookie handling, activity listing, dry run |
| `internal/poll` | finds new activities from the web session |
| `internal/trigger`, `internal/tokens` | `POST /api/trigger` and `gst_` bearer tokens |
| `internal/digest` | per-activity, weekly and monthly health messages and their schedulers |
| `internal/gap` | alerts once when no glucose reading has arrived for too long |
| `internal/chartimg` | PNG charts for emails, drawn in Go |
| `internal/notify` | ntfy/webhook/email channels and the outbox dispatcher |
| `internal/secrets` | AES-256-GCM vault, key loading and rotation |
| `internal/store` | PocketBase implementations of the interfaces |
| `internal/migrations` | PocketBase collections and schema changes |
| `internal/web` | gomponents pages, Datastar SSE handlers, auth, CSP |
| `internal/clientip` | trusted-proxy aware client address |
| `internal/demo` | fake sources for demo mode |

## Data flow

1. The poller or `POST /api/trigger` enqueues an activity (deduped by Strava id).
2. `Processor` loads glucose for the activity window (stored samples are the fallback when the source has aged out), computes stats, and renders a block.
3. The Strava writer opens the edit page with the stored cookies. The merge callback first saves the original description, then replaces only the `🩸` block. The writer verifies the saved text by reloading the page.
Before step 3 saves anything, `Processor` checks that only glucava's own block changed (`render.PreservesText`); if not, it fails with `ErrUnsafeMerge` and writes nothing.
4. Failures become `events`; the dispatcher sends them to ntfy/webhook/email (outbox pattern, cooldown, max age) and adds a web UI link from the public URL (`notify.LinkFor`). Summaries bypass the outbox: `Queue.OnDone` (first successful run only) and `digest.Weekly` build messages in `internal/digest` and `sendSummary` (cmd/glucava/main.go) mails them if the matching setting is on. They are email-only. The canary records a `canary_failed` event when the edit page no longer looks right.

## Conventions

- Interfaces live next to their consumer (`jobs.Store`, `jobs.Writer`), so tests use small fakes.
- The UI is server-rendered. Interactivity is Datastar (`data-on:click="@post(...)"`), with SSE responses. Anything put inside an inline JS string goes through `jsQuote`. Activity ids must match `^\d+$` before use.
- Styling is hand-written CSS in `internal/web/static/app.css` using `data-component` / `data-variant` attributes. No inline scripts (the CSP forbids them).
- PocketBase collections have nil API rules; only server code touches them.
- Settings: one rule set in `store.Config` (`Validate`, `configKeys`), shared by the web form and `glucava config`. Secrets live in the vault, not in `Config`. Operational values (timings, selectors) are env vars parsed in `cmd/glucava/tuning.go`.
- Schema changes are new numbered files in `internal/migrations`. Never edit an applied migration.
- Tests that need a PocketBase app import `github.com/pocketbase/pocketbase/migrations` and call `app.RunAllMigrations()` (see `internal/store/pb_test.go`).
- Commits: conventional style (`feat:`, `fix:`, `chore:`). `main` is protected: open a PR; the `check` job must pass.

## Unverified against the real services

The Strava login selectors and the Dexcom endpoints were written from memory and tested only against mocks (the description write and activity listing are confirmed; see AGENTS.md). Verify them first with a real session: `glucava strava check`, `strava list --raw`, `dexcom set`. Selector candidates live in `internal/strava/writer.go` (`DefaultSelectors`).

## Manual testing

See [TESTING.md](TESTING.md) for the checklist to run against real Strava, Dexcom and SMTP accounts.

## Releasing

Not automated yet. Build with `make build` or `docker build .`. Base images and GitHub Actions are pinned by digest/SHA; Dependabot proposes updates.

## Dependency pins

`modernc.org/sqlite`, `modernc.org/libc` and `modernc.org/memory` are pinned to the versions PocketBase's own `go.mod` lists (see `modernc_versions_check.go` in the PocketBase module), and Dependabot ignores them. When upgrading PocketBase, copy those three versions from its `go.mod`, or the server prints a "differs from the expected and tested" warning on every start and CLI call.
