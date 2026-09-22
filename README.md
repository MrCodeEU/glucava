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
- **Manual:** open an activity for a preview, then Process or Reprocess. Existing text is kept; only the 🩸 block is replaced.
- **Notifications:** ntfy or webhook (HMAC-signed) on failures and expired sessions.
- Dexcom Share keeps only 24 h of data, so activities older than that cannot be processed.

## Configuration

| Variable | Meaning |
|---|---|
| `GLUCAVA_ADMIN_EMAIL`, `GLUCAVA_ADMIN_PASSWORD` | first user, created on start |
| `GLUCAVA_DEXCOM_USERNAME`, `GLUCAVA_DEXCOM_PASSWORD`, `GLUCAVA_DEXCOM_REGION` | Dexcom Share login, stored on first start if no credential is stored yet (alternative to `make dexcom`); safe to remove afterwards |
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

License: AGPL-3.0.
