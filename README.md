# Glucava

Free, self-hosted alternative to Ando. After a Strava activity ends, Glucava adds your Dexcom glucose stats (time in range, min, max, average, sparkline) to its description:

```
🩸 TIR 92% | min 78 | max 164 | avg 112 mg/dL
▁▂▃▃▄▅▄▃▃▂▂▃
```

Strava's API is paywalled, so Glucava edits the description through a headless Chrome session with your own cookies. One Go binary (PocketBase + web UI); Chrome/Chromium must be installed.

## Quick start

```sh
make mock          # fake Strava + Dexcom data, http://127.0.0.1:8090 (make help lists all targets)
```

Real use, with Docker:

```sh
docker build -t glucava .
docker run -d --name glucava -p 127.0.0.1:8090:8090 -v glucava:/data \
  -e GLUCAVA_ADMIN_EMAIL=you@example.com \
  -e GLUCAVA_ADMIN_PASSWORD='long-random-password' \
  -e GLUCAVA_SECRET_KEY="$(openssl rand -base64 32)" glucava
```

Put a TLS reverse proxy in front. Then:

1. Sign in, open **Settings**, enter Dexcom Share credentials (or `glucava dexcom set <user> --region us|ous|jp`). Enable Share in the Dexcom app. "Test connection" checks the login.
2. Open **Strava**. Paste your strava.com cookie export (or `glucava strava cookies import file.json`); this is the supported path. "Sign in automatically" is an **experimental** alternative that fills Strava's login form for you and stores the resulting cookies; it gives up at the first CAPTCHA, verification code or wrong-password message rather than guess, and never stores the password. It has not been verified against the real Strava login page.
3. Run `glucava strava check` to verify the session and edit-page selectors without saving.

## Usage

- **Polling:** Settings → poll interval. New activities from the last 24 h are processed once.
- **Push trigger:** create a token on the **Tokens** page (shown once), then call it from Tasker or Apple Shortcuts when the Strava notification arrives:
  `curl -X POST -H "Authorization: Bearer gst_..." https://host/api/trigger`
  See [docs/triggers.md](docs/triggers.md) for Tasker, HTTP Shortcuts/MacroDroid, and Apple Shortcuts setup.
- **Manual:** open an activity for a preview, then Process or Reprocess. Existing text is kept; only the 🩸 block is replaced.
- **By id:** the **Strava session** page has a "Process a specific activity" field for an activity the poller never saw at all (e.g. it predates glucava, or is older than the polling window). It pages through your Strava training log looking for that id, same as Reprocess otherwise.
- **Notifications:** ntfy, webhook (HMAC-signed) or email on failures and expired sessions. Email is set up under Settings → Email (SMTP host, credentials, recipient); `GLUCAVA_SMTP_*` (below) can seed it on first start.
- **Canary:** once a day, glucava dry-runs the edit page against your most recent activity (no save) to catch a broken selector or an expired session before it silently fails a real one; a failure notifies like any other event.
- Dexcom Share itself only serves ~24 h of history, but a background job stores every reading it sees into glucava's own database as it arrives, so an activity can still be reprocessed later as long as its window is within Settings → **Your data** → retention (default 365 days). An activity missed entirely while it was still fresh (e.g. glucava was not running, or the reading never got ingested) cannot be recovered after the fact — unless you can still get that period as an export file: `make glucose-import FILE=export.csv FORMAT=libre` backfills readings from another app's export directly into the same database, so an older activity can be reprocessed against them. `glucava glucose import --format` currently understands `glooko` (a Glooko export — either the zip download directly, or `cgm_data_*.csv` from its extracted folder; verified against a real export), `libre` (a LibreView CSV export; unverified against a real file — see the code comment) and `nightscout` (an `entries.json` export). A zip is decoded entirely in memory and never extracted to disk: the entry name is only ever compared as a string, never used to build a filesystem path, so a malicious entry name cannot write outside the intended location; decompression is capped to bound a zip bomb. Adding another format is one function; see the extension point below.

## Configuration

| Variable | Meaning |
|---|---|
| `GLUCAVA_ADMIN_EMAIL`, `GLUCAVA_ADMIN_PASSWORD` | first user, created on start |
| `GLUCAVA_DEXCOM_USERNAME`, `GLUCAVA_DEXCOM_PASSWORD`, `GLUCAVA_DEXCOM_REGION` | Dexcom Share login, stored on first start if no credential is stored yet (alternative to `make dexcom`); safe to remove afterwards |
| `GLUCAVA_RETENTION_DAYS` | readings/events retention in days (0 = forever); overrides the UI setting on every start, not just the first |
| `GLUCAVA_SMTP_HOST`, `GLUCAVA_SMTP_SENDER_ADDRESS` | seed the SMTP settings on first start (only while none are saved; afterwards edit them in Settings → Email) |
| `GLUCAVA_SMTP_PORT` (default `587`), `GLUCAVA_SMTP_USERNAME`, `GLUCAVA_SMTP_PASSWORD`, `GLUCAVA_SMTP_TLS=1`, `GLUCAVA_SMTP_SENDER_NAME` (default `glucava`) | rest of the seed; the password goes into the encrypted vault |
| `GLUCAVA_SECRET_KEY` | encryption key for stored secrets (default: `<data dir>/secret.key`) |
| `GLUCAVA_TRUSTED_PROXIES` | comma-separated IPs/CIDRs of your reverse proxy. Only then are `X-Forwarded-For`/`-Proto` believed (per-client rate limits, Secure cookie). Unset = direct peer only |
| `GLUCAVA_ADMIN_UI=1` | expose PocketBase admin UI and API (off by default) |
| `GLUCAVA_NO_SANDBOX=1` | Chrome `--no-sandbox` (set in the Docker image) |
| `CHROME_PATH` | Chrome binary |

## Maintenance

- `glucava user set-password <email>` reads the new password (12+ characters) from stdin and signs out every session. Logout also invalidates the session server-side. Logins last 3 days.
- `glucava secrets rotate-key` re-encrypts stored secrets with a fresh key. Stop the server and back up the data dir first.
- Settings → **Your data**: retention (default 365 days for readings and events; 0 keeps them), CSV export of readings and activities, and delete-all. `glucava data purge --yes` does the same from the shell. Activities are kept by retention because they log what was written to Strava.
- Only one user account can exist.
- Back up the data dir and the encryption key separately; a backup holding both exposes your credentials.

## Development

Run `make hooks` once to enable the git hooks (pre-commit: gofmt, vet, lint; pre-push: tests, govulncheck). `make check` runs everything CI runs. See [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md). See [SECURITY.md](SECURITY.md) for the threat model. Automating Strava's web UI may breach its terms; use at your own risk.

**Adding a glucose source.** Dexcom Share (`internal/glucose/dexcom.go`) is the only built-in source, but the pipeline (background ingestion, activity processing, live dashboard reading) only depends on the small `glucose.Source` interface:

```go
type Source interface {
    Samples(ctx context.Context, from, to time.Time) ([]stats.Sample, error)
}
```

A Libre, Tandem or Medtronic **live API** source is a new type implementing that one method, wired in `cmd/glucava/main.go` next to `dexcomSource`. No changes needed elsewhere.

**Adding a file importer** (a one-shot export, not a live API) is a separate, smaller interface in `internal/glucose/importers`:

```go
type Importer interface {
    Parse(r io.Reader) (samples []stats.Sample, skipped int, err error)
}
```

Register it by name in an `init()` func (see `libre.go`) and it is immediately available to `glucava glucose import --format <name>`; no other wiring needed.

Either kind of contributed source, for hardware or an account type the maintainer doesn't have, is welcome but untested by CI; say so plainly in the code and docs, the same way the experimental Strava auto-login and the Libre importer are marked.

License: AGPL-3.0.
