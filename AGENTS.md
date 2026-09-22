# AGENTS.md

Guidance for coding agents working in this repository. For setup, layout,
data flow and conventions see [`docs/DEVELOPMENT.md`](docs/DEVELOPMENT.md)
first — this file covers what that doc doesn't: hard rules, the current
state of the project, and things that will bite you if skipped.

## What this repo is

Glucava adds Dexcom glucose stats (time in range, min, max, avg, sparkline)
to Strava activity descriptions. Strava's API needs a paid subscription, so
the write path is a headless Chrome session driven with the user's own
browser cookies, not the API. Single Go binary, PocketBase for DB/auth/UI
backend, gomponents + Datastar for the server-rendered UI. AGPL-3.0,
self-hosted, one account per instance.

## State as of 2026-09-22

MVP is feature-complete and hardened (see the merged PRs on `main`): the
pipeline, web UI, trigger endpoint, notifications, session lifetime,
key rotation, data retention/export/purge, supply-chain pinning. **Nothing
has been verified against the real Strava or Dexcom services yet** — see
the next section. Manual testing against real accounts is in progress and
feedback comes back in rounds; each round becomes its own PR.

## Unverified against the real services — do not assume these are correct

- `internal/strava/writer.go`: `DefaultSelectors` (description textarea,
  save button), `DefaultLoginSelectors` (email/password/submit) and the
  `/activities/{id}/edit` URL are guesses. `Login`'s challenge/wrong-password
  detection (`challengeMarkers` in the same file) is untested against a real
  login page.
- `internal/strava/list.go`: the `/athlete/training_activities` JSON field
  names.
- `internal/glucose/dexcom.go`: endpoints and payloads are from memory of
  `pydexcom`, not a live account.

When a manual test reveals the real shape, fix the code AND delete the
corresponding line above — an item staying here after it's confirmed is
worse than not having the list.

## Hard rules

- **Never edit an applied migration** in `internal/migrations/`. Add a new
  numbered file instead (`004_...go` style), even for a one-field fix.
- **Never log or return a secret.** Strava cookies, Dexcom password, ntfy
  token, webhook secret, the encryption key: none of these may reach a log
  line, an `Event.Message`, or an HTTP error body. `internal/notify/ntfy.go`
  strips the URL from `url.Error` for this reason — follow that pattern for
  any new outbound client.
- **`internal/strava.Writer.Login` must never persist a password.** Only
  the resulting session cookies are saved, exactly like cookie import. If
  you add credential storage for it, that's a deliberate product change,
  not a refactor — ask first.
- **Activity ids are untrusted input** wherever they reach a URL, a SQL
  filter, or inline JS. Match `^\d+$` first (see `digits` in
  `internal/web/handlers.go`) and route inline JS string values through
  `jsQuote` (`internal/web/pages.go`).
- **PocketBase's own admin UI and REST API stay blocked** (`blockPocketBase`
  in `cmd/glucava/main.go`). Don't add a route under `/_/` or `/api/` to the
  Glucava UI; it would collide with that block. `GLUCAVA_ADMIN_UI=1` is the
  documented escape hatch, not a code change.
- **Single user only.** `bootstrap.EnforceSingleUser` rejects a second
  `users` record on purpose. Don't route around it to "fix" a signup flow.
- **`main` requires a PR.** The ruleset has no admin bypass; the `check`
  status (vet+test+lint+vuln, `.github/workflows/ci.yml`) must pass. Branch
  first, never push to `main` directly.

## Commit and PR style

Conventional commits (`feat:`, `fix:`, `chore:`, `docs:`, `test:`). PR
descriptions are normal prose, not caveman/terse notes, since other people
read them. Attribution footers as instructed in the session (Claude commit
trailer / PR footer) — don't invent your own.

## Before you're done

`make check` (vet, test, lint, govulncheck) must pass, and `gofmt -l .`
must be empty. Prefer `make hooks` once per clone so this is automatic.
If you touched `internal/web`, run the real-browser flow check
(`make mock` in one terminal, `go run ./tools/shot -url http://127.0.0.1:8090 -flow`
in another, with `CHROME_PATH` set) — the Go test suite mocks Datastar/SSE
enough that a real rendering bug can still slip through it.
