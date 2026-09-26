# Changelog

All notable changes are listed here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow [Semantic Versioning](https://semver.org/). See [docs/RELEASING.md](docs/RELEASING.md) for how a release is cut.

## [Unreleased]

## [0.1.2] - 2026-09-26

### Fixed
- The container image now runs glucava under `tini`. Chrome leaves child processes behind after every Strava check; as PID 1 glucava never reaped them, so they piled up as zombies (about eight per check) until the container hit its process limit and every check failed with "chrome failed to start: Cannot fork". If you run the binary as PID 1 yourself, start the container with `--init`.

### Added
- A startup warning when glucava runs as PID 1 without an init, and a plain hint in the failed-check message when Chrome cannot start because the container is out of processes.

## [0.1.1] - 2026-09-26

### Added
- Settings → Account: change the sign-in email and password from the web UI. It asks for the current password (a wrong one counts as a failed login), signs out every other browser and keeps you signed in.
- `glucava user show` and `glucava user set-email`; `glucava user set-password` no longer needs the email while there is exactly one user, so a mistyped seed email can be fixed.

### Fixed
- The cookie that ends a session is now `Secure` behind an HTTPS proxy, like the one that starts it (CodeQL).
- The Dexcom source cache compares credentials directly instead of hashing them (CodeQL).

## [0.1.0] - 2026-09-25

First public release.

### Added
- Adds time in range, lowest, highest, average and a sparkline from Dexcom Share to the description of Strava activities, through a chromedp session with your own cookies. Only glucava's own block is written; a pre-write check refuses any merge that would change your text, and the original description is kept so it can be restored.
- Polling and a token-protected `POST /api/trigger` endpoint for phones (Tasker, Apple Shortcuts); manual process and reprocess in the web UI.
- Server-rendered web UI (gomponents, Datastar) with dashboard, per-activity glucose chart, Strava session page, triggers, notifications, settings; light and dark.
- Notifications through ntfy, HMAC-signed webhooks and email. Emails are HTML with a plain-text fallback, icons, inline charts and links to the web UI (public URL setting). Optional summaries: after each activity, weekly, monthly health report. Glucose gap alert.
- Daily canary that dry-runs the Strava edit page and reports drift; selector overrides through the environment.
- File importers for Glooko, LibreView and Nightscout exports; continuous local storage of readings; retention, CSV export, delete-all.
- Scripted setup: every setting is available in the web UI and through `glucava config`, `glucava secrets`, and seed-once environment variables. Backup and restore with key rotation.
- Security: encrypted secrets, single user, PocketBase admin API blocked, CSP and same-origin checks, per-IP rate limits, hardened container example, AGPL source link on every page.
- Optional chart photo (experimental, off by default): a square glucose card (time in range, coloured curve with target band and activity span, in-range bar, lowest/average/highest) attached to the Strava activity once, checked against the edit page's own media list before it counts as sent. Theme, size, shading, dots and line thickness are settings with a live preview; a lead-in before the activity (`chart_pre_minutes`) shows where glucose came from.
- Heart rate: read from Strava's web session when an activity is processed, kept per activity, and used on the activity page, in the summary email, in the activities CSV and as a second curve on the chart photo.
- Jobs say why they wait: each failed attempt is logged and shown on the activity page with the next retry time; errors a retry cannot fix fail at once; a finish message appears when a run you are watching ends.
- Probes that never save: `glucava strava check <id> --photo` and `--hr` (also `make strava-photo`, `make strava-hr`) to see what Strava's pages do.
- Container image with Chromium, a built-in health check (`glucava healthcheck`) and version information.
