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
