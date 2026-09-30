# Changelog

All notable changes are listed here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow [Semantic Versioning](https://semver.org/). See [docs/RELEASING.md](docs/RELEASING.md) for how a release is cut.

## [Unreleased]

## [0.7.3] - 2026-09-30

### Changed
- Overview: the time-of-day and lows-and-highs cards use the full row, and the lows-and-highs summary merges longest and total time into one column, so their tables no longer scroll sideways on desktop.

### Added
- A printable PDF report: the Overview's "Download report" button (and a link in Settings → Your data) produces an A4 report for the selected range, with the previous-period comparison, in mg/dL or mmol/L. It is typeset by Typst from glucava's own charts with an embedded font, so it looks the same on every host. It has key numbers, time in range against the consensus targets, the daily profile (AGP), a day-by-day chart, time of day, lows and highs, per-activity-type numbers, recent activities and glucose sources. The Docker image includes typst; other setups need it on the `PATH` or `GLUCAVA_TYPST`, otherwise the download answers 503.
- Suspected sensor artifacts: compression lows and sudden sensor dips are detected and flagged on the Overview (badge with reasons, and a note such as "time below range is 1.6% as recorded and 1.0% without the suspected artifacts"). Each low episode has "Not real" / "It was real" buttons that override the detector. Settings → Overview page → "Suspected sensor artifacts" can exclude them from the statistics and from the PDF report. The Activity page shows a notice when one overlaps the activity.

## [0.7.2] - 2026-09-30

### Added
- Activity page: one interactive chart with glucose, heart rate and elevation, the activity and before/after windows shaded, and zoom. New tiles for variability (CV, SD), GMI, very-low/very-high share, drop rate, duration, distance, pace, elevation gain and rank among activities of the same sport, plus a before/during/after card that flags a low within three hours afterwards. Older/Newer buttons move between activities.
- Dashboard "now" card: current value with trend arrow, the last three hours with the target band, and a 24-hour time-in-range donut. Activity rows show a sport icon and distance/pace.

### Changed
- Overview: the weekday-by-hour heatmap now colours by time in range on a red-to-green gradient by default (average glucose stays available as a card option), and the calendar uses the same gradient. The by-activity-type chart shows the full five-band split (very low to very high) so a bar no longer looks like 100% in range when it is not. Heatmap, calendar, time of day and lows/highs sit two to a row on wide screens, which shortens the page. "Lowest time in range" no longer comes up empty when only a few activities exist. Short bar charts show every axis label. The demo data now includes a continuous last-day trace and elevation profiles.

## [0.7.1] - 2026-09-30

### Added
- Overview rewrite: key numbers (TIR, GMI, average, CV, glucose risk index, coverage), five-band TIR donuts for all readings and for activities only, AGP (median and percentile bands by time of day), daily trends with zoom, weekday-by-hour and calendar heatmaps, time-of-day breakdown, lows and highs episodes with the activity they happened around, by-activity-type table (CV, start-to-end change, drop rate, lows afterwards, average heart rate, distance, climb, pace), activity insights (start glucose against change, best and worst activities), and a sources and coverage strip. Charts use Apache ECharts.
- Range presets 7/14/30/90 days and all time, a custom from–to range, and comparison with the previous period. Default range is a setting.
- `{{.TIRBar}}` and `{{.TIRWindowBar}}` description-template fields: the below/in-range/above split drawn as ten emoji blocks (🟥 low, 🟩 in range, 🟨 high). New "Bar + window" preset shows the bar and, when it differs, a second one for the wider before/after window. The Default preset is unchanged, so existing descriptions do not change.

### Changed
- Overview cards are configured with one layout setting (show, hide, reorder, per-card options) instead of five toggles; existing choices carry over. The `overview_show_*` CLI settings are replaced by `overview_layout` and `overview_default_range`.
- Overview data is cached per range and data version.
- The System pages (Strava session, Triggers, Logs) are links in the top bar from 1180 px up; the System dropdown only appears where the bar is too narrow.

## [0.7.0] - 2026-09-30

### Added
- Configurable very-low and very-high glucose thresholds (Settings → target range; defaults 54 and 250 mg/dL), used by the very-low/very-high shares in every summary.

### Changed
- The Overview loads glucose readings and activities with lean raw queries (about 4x faster on a year of data), and the database gets an index on reading time.
- The web UI is restyled on Tailwind CSS v4 (standalone CLI, compiled `app.css` committed, `make css` / `make css-check`). New app shell: a responsive top bar that becomes a bottom tab bar on phones, a "System" menu for the less-used pages, and a compact account menu with the theme toggle and sign out (fixes the sign-out button wrapping on narrow screens). New component kit: stat tiles with trend, segmented control (used for the Overview range picker), tabs, empty/error/skeleton states, help tooltips, ghost/danger buttons, improved forms, tables with a sticky header, and print styles. Light and dark palettes were retuned for contrast.
- `tools/shot` covers every page, `-matrix` runs 1280 and 390 px in light and dark, and it now fails on browser console errors and CSP violations.

## [0.6.0] - 2026-09-29

### Added
- Distance, elevation gain and a single sport-aware pace/speed field, pulled from Strava's own activity listing (free — no extra request): `{{.Distance}}`, `{{.Elevation}}` and `{{.Pace}}` in the description template, tiles on the activity page, and `distance_km`/`elevation_gain_m`/`pace` columns in the activities CSV export. `{{.Pace}}` is already in the right unit and label for the activity's sport (pace for a run/hike/walk/swim, speed for a ride), empty for a sport with no meaningful distance metric or a manual/trainer entry.
- An elevation profile panel on the chart photo (Settings → Description and chart → Panels), drawn as a faint terrain silhouette behind the glucose curve. Fetched the same way heart rate already is (one extra page load, only when the panel is on).

## [0.5.1] - 2026-09-29

### Fixed
- The Overview page's General glucose card showed trend charts with no indication of what date range they covered, unlike the activity-based Trends card right above it.

## [0.5.0] - 2026-09-29

### Added
- Two more cards on the Overview page (each independently toggleable, like the existing three): a whole-range glucose summary, independent of activities, and a per-source health list showing each glucose source's reading count and newest reading — the signal that a live connection is still working or a backfill import actually landed something, without waiting for an activity to show up.
- The Tokens page generates a ready-to-import Tasker `.prf.xml` (download button + QR code, next to a freshly created token): a Notification event on the Strava app, deliberately left unfiltered rather than trying to match specific wording, wired to an HTTP Request task that calls `/api/trigger`. A second QR code opens the Tokens page itself, for HTTP Shortcuts/MacroDroid/Apple Shortcuts, whose per-platform instructions on that page fill in the real token instead of a placeholder.
- A new `/logs` page shows the last 1000 log entries, live-updating, without needing `docker logs` on the host. Logging switched from `log.Printf` to structured `log/slog` throughout, so entries carry real fields (activity id, error, counts) instead of pre-formatted strings.
- A new `{{.TIRWindow}}` description-template field: time in range over the wider pre/post buffer window the chart photo draws from, alongside the existing `{{.TIR}}` (activity window only). The two can legitimately show different percentages for the same activity — this makes that visible/explainable in the text instead of silently differing from the chart. Corrected on the delayed reprocess pass once late readings land, same as the chart.

### Fixed
- The nav bar's sign-out button could wrap onto its own line separately from the theme toggle when the signed-in email was long enough, instead of the two wrapping together as a unit. The sign-out button's text is also now truncated instead of stretching the button for a very long email.

## [0.4.0] - 2026-09-29

### Added
- `GET /metrics`: Prometheus metrics for job outcomes and queue depth, background ingest health (including a "last successful fetch" gauge to catch a silently stopped source), Strava write latency, a recent-window glucose summary, and HTTP request counts/latency/in-flight, alongside the standard Go runtime/process collectors. Needs a bearer token, the same kind and the same tokens as `/api/trigger`.
- **Overview page** (`/stats`): trends over time (time in range and average glucose, one point per day), a breakdown by activity type, and the raw activity table, over a selectable date range (7/30/90 days or all time). Each card can be turned off independently under Settings → Description and chart → Overview page.

### Fixed
- The chart photo's font cache reused a `font.Face` across concurrent chart renders (e.g. two browser tabs, or the settings page's live preview refetching while another view also draws a chart), which is not safe: `font.Face` keeps private state that drawing mutates. Two charts rendered at close enough to the same time could corrupt each other's text. Each render now gets its own `Face` instead of a shared, cached one.
- The Strava description merge could disagree with itself on already-merged text when the input mixed CRLF, lone CR, or leading whitespace with a Glucava block in specific ways: `Strip`/`Merge` did not always give the same answer for their own output as for the original text (found by a new fuzz test, `FuzzMergePreservesText`/`FuzzStripIdempotent`; not something Merge's normal callers were ever seen to trigger).
- The four chart overlay toggles added in 0.3.0 (average line, target range low/high lines, min/max markers, hide stats) never actually worked: the settings collection was missing their database columns entirely, so a checkbox on the Settings page looked like it saved but the value was silently dropped, and the real chart photo attached to a Strava activity never picked it up either. If you turned any of these on since 0.3.0, re-save Settings once after upgrading.

## [0.3.0] - 2026-09-29

### Added
- Glucose ingest self-heals after a gap: if the newest stored reading is older than the usual rolling window (phone off, flight mode, a dead Dexcom login), the next fetch widens automatically to catch up on everything the source still has, up to its retention limit. A "Resync now" button on the settings page forces the same catch-up on demand instead of waiting for the next tick.
- Four new chart photo overlays, each its own on/off toggle on the settings page: an average-glucose line, dashed lines at the target range low/high, marker dots at the curve's minimum and maximum, and hiding the TIR number and stats bar for a plainer chart.

### Fixed
- Deleting an activity didn't update any OTHER already-open dashboard tab (the one that clicked delete navigated home correctly; a second tab kept showing the deleted row until a manual reload). Delete now publishes to the same live-update bus every other action already uses.

## [0.2.1] - 2026-09-29

### Fixed
- Delete stopped working again in 0.2.0: the confirm button's expression used `await`, which Datastar compiles with a plain, non-async `Function` constructor and rejects at click time. Chains with `.then()` instead, like the fix in 0.1.5.
- Chart panel reorder buttons (Earlier/Later) worked but gave no visual feedback, so they looked broken. Rows now reflect the current order via CSS `order`.
- The description template preview never updated and never showed template errors: `/preview/description.txt` was missing from `web.Routes`, so PocketBase's own router 404'd the request before it ever reached this app's handler.

## [0.2.0] - 2026-09-28

### Added
- Custom description text: the Strava description block can now be written as a Go `text/template`, with the same numbers (TIR, min/max/avg, StdDev, CV, GMI, level 2 hypo/hyperglycemia, sparkline) available as fields. Settings page has 5 built-in presets (Default, Minimal, Clinical, Emoji, Numbers only) plus a raw template editor, with a live preview against your latest activity or sample data. Scriptable as `description_template` (CLI/`.env`), like every other setting.
- Clinical glucose metrics: GMI (estimated A1C), coefficient of variation, and level 2 (severe) hypo-/hyperglycemia percentages, independent of your own target range.
- Chart panel order: the activity-span and target-range shading (and, for parity, the dots and heart rate toggles) are now an ordered, reorderable list on the settings page, not just on/off switches — where activity and target-range shading overlap, whichever is listed later wins. Scriptable as `chart_panel_order` (comma-separated: `activity,band,dots,hr`).

### Changed
- Settings page is now split into 4 tabs (Glucose and timing, Description and chart, Notifications, Data and account) instead of one long scroll. Save and test buttons stay visible on every tab.
- Activity page: while a run is in progress, "Working on it" now names the actual step (fetching glucose readings, writing the description, reading heart rate, drawing and uploading the chart) instead of a generic message, pushed live over the existing SSE connection.

## [0.1.5] - 2026-09-27

### Changed
- Delete's confirmation is now a styled dialog matching the rest of the UI, instead of the browser's plain `confirm()` popup.

### Fixed
- Delete did not return to the dashboard afterwards: it tried to navigate through a server-sent script, which this app's CSP (no `unsafe-inline`) silently blocks. It now navigates from the client side instead, inside the already-permitted evaluated expression.

## [0.1.4] - 2026-09-27

### Added
- Activity page: a "Delete" button (with a confirmation dialog) removes glucava's own record of an activity. It never touches Strava beyond trying, best effort, to restore the original description first if one was saved; a failure there (e.g. the activity is already gone from Strava) does not block the local delete.

### Fixed
- The description text used to wait for the full "Minutes after end" (`post_minutes`) window before its very first write, because that window's cooldown glucose was baked into the stats. The text is now activity-only (start minus "Minutes before start" through end) and is written the moment an activity finishes; `post_minutes` now only widens the chart photo's curve with cooldown glucose, and no longer delays anything by itself. The delayed-reprocess buffer (`post_buffer_minutes`) still refreshes the text afterwards if any last-minute Dexcom readings were late, and still gates the chart's first upload.
- An activity that was deleted on Strava, if glucava still had a record of it (e.g. it was resurrected by the restart recovery in 0.1.3), used to retry forever and could even trip the daily canary, both misreporting "no description element" as if Strava's page had changed. It is now recognised distinctly (the edit page redirects elsewhere instead of showing the activity) and is not retried.

## [0.1.3] - 2026-09-27

### Added
- Tokens page: a new token comes with copy buttons for the URL, the header and a ready `curl` test with the token filled in, and the phone setup steps are shorter and concrete (Tasker task, profile, notification filter; iOS automation).
- Delayed reprocess (`post_buffer_minutes`, default 5): an activity is automatically reprocessed once more a few minutes after its glucose window first closes, to pick up Dexcom readings that had not arrived yet the first time. With the chart photo on, its first upload waits for that same deadline instead of firing early with an incomplete curve, since a photo cannot be replaced once attached; the description text is not delayed. 0 turns both off.

### Fixed
- An activity interrupted mid-run by a restart (host reboot, crash) used to sit forever: the poller skips any activity it already has a record for, and nothing ever finished that record. Startup now resets and re-queues anything left at "processing". Root cause of a real incident: a scheduled host reboot interrupted a Strava write, and the poller silently never saw that activity again.
- The poller now always logs its outcome, including "nothing new", instead of only when it queued something, so a stuck poll is visible in the logs.
- The block written to a Strava description now carries two invisible Unicode code points as a fingerprint at its start, and two more at its end, so finding it no longer depends on the 🩸 emoji or the wording "TIR " alone (either of which another app, or a coincidence, could share) or on guessing its end from shape (a line that merely looks like a sparkline). A block written by 0.1.2 or earlier is still recognised by its old text and bound, and gets the fingerprint on its next reprocess, so no existing activity needs any action.

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
