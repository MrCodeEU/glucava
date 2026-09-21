# Security policy

Glucava handles glucose data, Strava session cookies and Dexcom credentials. Please report vulnerabilities privately through GitHub's "Report a vulnerability" button (Security tab), not in public issues.

## Model

- Secrets (Strava cookies, Dexcom password, ntfy token, webhook secret) are encrypted with AES-256-GCM. The key comes from `GLUCAVA_SECRET_KEY` or `<data dir>/secret.key` (mode 0600). Keep the key apart from backups of the data dir.
- Sign-up is disabled. The only account is the one created from `GLUCAVA_ADMIN_EMAIL` / `GLUCAVA_ADMIN_PASSWORD`.
- PocketBase's admin UI and REST API are blocked. Set `GLUCAVA_ADMIN_UI=1` to expose them.
- `POST /api/trigger` needs a `gst_` bearer token. Tokens are stored as SHA-256 hashes and failures are rate limited per IP.
- State-changing requests must be same-origin. Pages send a strict CSP.
- Forwarding headers are ignored unless the direct peer is listed in `GLUCAVA_TRUSTED_PROXIES`. Set it to your proxy address, otherwise all clients share the proxy's rate-limit bucket.
- Run behind a TLS reverse proxy. Do not expose the port directly.
- The Strava session is your own logged-in browser session. Automating the web UI may breach Strava's terms; use at your own risk.
- Notification requests never follow redirects. Webhook and ntfy URLs may point at private addresses on purpose (self-hosted ntfy); only the signed-in admin can set them.
- The job queue is in memory. A restart drops queued trigger jobs; the poller finds those activities again.
- The browser profile (which holds session cookies while Chrome runs) is a private temp directory deleted after every run.
- Base images and CI actions are pinned by digest/SHA. `docker-compose.yml` shows a hardened setup (read-only root, no capabilities, no-new-privileges).
- Glucose readings, activities and events are stored unencrypted in SQLite (only credentials are encrypted). Use disk encryption for the data dir. Retention, CSV export and delete-all are in Settings.
- Only one user account can exist.
