# Glucava

Free, self-hosted alternative to Ando. After a Strava activity ends, Glucava adds your Dexcom glucose stats (time in range, min, max, average, sparkline) to its description:

```
🩸 TIR 92% | min 78 | max 164 | avg 112 mg/dL
▁▂▃▃▄▅▄▃▃▂▂▃
```

Strava's API is paywalled, so Glucava edits the description through a headless Chrome session with your own cookies. One Go binary (PocketBase + web UI); Chrome/Chromium must be installed.

## Quick start

```sh
make demo          # fake Strava + Dexcom data, http://127.0.0.1:8090
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

1. Sign in, open **Settings**, enter Dexcom Share credentials (or `glucava dexcom set <user> --region us|ous|jp`). Enable Share in the Dexcom app.
2. Open **Strava**, paste your strava.com cookie export (or `glucava strava cookies import file.json`).
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
| `GLUCAVA_SECRET_KEY` | encryption key for stored secrets (default: `<data dir>/secret.key`) |
| `GLUCAVA_ADMIN_UI=1` | expose PocketBase admin UI and API (off by default) |
| `GLUCAVA_NO_SANDBOX=1` | Chrome `--no-sandbox` (set in the Docker image) |
| `CHROME_PATH` | Chrome binary |

## Development

`make check` runs vet, tests, lint and govulncheck. See [SECURITY.md](SECURITY.md) for the threat model. Automating Strava's web UI may breach its terms; use at your own risk.

License: AGPL-3.0.
