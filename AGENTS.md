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

## State as of 2026-09-24

Feature-complete and hardened (see the merged PRs on `main`): pipeline, web
UI with glucose chart, trigger endpoint (docs in `docs/triggers.md`),
ntfy/webhook/email notifications, edit-page canary, glucose importers
(Libre, Nightscout, Glooko), process-by-id, a pre-write safety check,
scripted setup (`glucava config`, `glucava secrets set`), env-tunable timings
and Strava selectors, session lifetime, key rotation, retention/export/purge,
supply-chain pinning.

Writing the description on a real Strava account (edit page, description
textarea, save, reload check) and listing activities via
`/athlete/training_activities` have been exercised by hand and work. Everything
else in the next section is still unverified. A large manual test round is next
(`docs/TESTING.md` is the checklist); feedback comes back in rounds and each
round becomes its own PR.

Not built on purpose: i18n (the language setting was removed), a Prometheus
endpoint. Backlog: LLM selector self-repair, Strava chart-image upload
(render exists in the UI; the Strava upload flow is unverified territory,
decide the approach together with the maintainer first).

## Unverified against the real services — do not assume these are correct

- `internal/strava/writer.go`: `DefaultLoginSelectors` (email/password/submit)
  are guesses (the description textarea and edit URL are confirmed). `Login`'s challenge/wrong-password
  detection (`challengeMarkers` in the same file) is untested against a real
  login page.
- `internal/glucose/dexcom.go`: endpoints and payloads are from memory of
  `pydexcom`, not a live account.
- Email delivery (`internal/notify/email.go`, needs a real SMTP server), the
  canary against a real edit page, and the Tasker/Shortcuts flows in
  `docs/triggers.md` have only been tested against mocks.

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
- **Never bypass the pre-write check.** `Processor.Process` refuses to write
  unless `render.PreservesText(existing, merged)` holds (only glucava's own
  block may change). It exists because a merge bug once duplicated blocks on
  a real activity. Restore is the one deliberate exception.
- **One rule set for settings.** A new setting needs: a migration, a
  `store.Config` field, an entry in `configKeys` and `Validate` in
  `internal/store/config.go` (this also makes it scriptable), and the form.
  Never validate in the web layer only. Secrets go in the vault
  (`internal/secrets/names.go`) and in `scriptableSecrets`, never in `Config`.
- **No hardcoded operational values.** Timings, URLs, selectors and the like
  belong in `cmd/glucava/tuning.go` (env, default = the old value, bad value
  stops the start) or in a setting. A new live glucose source is a
  `sourceBuilders` entry in `cmd/glucava/wire.go`.
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
