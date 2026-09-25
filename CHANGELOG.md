# Changelog

All notable changes are listed here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow [Semantic Versioning](https://semver.org/). See [docs/RELEASING.md](docs/RELEASING.md) for how a release is cut.

## [Unreleased]

## [0.1.0] - unreleased

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
- Container image with Chromium, a built-in health check (`glucava healthcheck`) and version information.
