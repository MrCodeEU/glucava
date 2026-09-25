# Manual test checklist

Automated tests only use mocks. Run this against your own real accounts after
changes to the Strava, Dexcom, notification or trigger paths. Use a throwaway
or already-processed activity, and keep the original description in mind:
"Restore original" on the activity page puts it back.

Mark each item, and turn anything surprising into an issue or a PR. If a check
confirms something listed as "unverified" in `AGENTS.md`, delete that line.

## Setup

- [ ] `glucava config list --dev=false --dir <dir>` shows sane values; `glucava secrets status` shows what you expect (values never printed).
- [ ] Fresh data dir from `.env` alone: admin user, Dexcom credential and SMTP are seeded (see the log lines `bootstrap: ...`).
- [ ] `glucava strava check` reports a logged-in session and finds description and save selectors.

## Core flow

- [ ] Dashboard shows the current Dexcom reading.
- [ ] A new activity is picked up by polling within the poll interval and its description gets exactly one block.
- [ ] Text you wrote yourself (and any other app's lines) is byte-for-byte unchanged around the block.
- [ ] Reprocess the same activity: still exactly one block, no duplicates.
- [ ] Restore original puts the pre-glucava text back; reprocess adds the block again.
- [ ] Process a specific activity by id (older than the polling window) with imported or stored readings.
- [ ] The glucose chart on the activity page matches the block's min/max/TIR.

## Triggers

- [ ] `curl -X POST -H "Authorization: Bearer gst_..." https://<host>/api/trigger` returns 202; a wrong token returns 401 and produces a `trigger_rejected` event.
- [ ] Your phone automation (see [triggers.md](triggers.md)) fires and the activity is processed sooner than the poll interval.

## Failure handling

- [ ] Break the Strava session (import a cookie file with a wrong session cookie): a `session_expired` notification arrives once, not on every poll.
- [ ] Canary: point `GLUCAVA_STRAVA_URL` at a page without a textarea (or block Chrome from finding the field) and set `GLUCAVA_CANARY_INTERVAL=1m`; a `canary_failed` notification arrives once, not every minute.
- [ ] Selector override: `GLUCAVA_STRAVA_SELECTOR_DESCRIPTION='["textarea.x"]'` is tried first and the built-ins still work as fallback (`glucava strava check`).
- [ ] Stop Dexcom sharing / use wrong credentials: `glucose_unavailable` notification.
- [ ] Settings → Email: with **Public URL** set, alert mails have an "Open in glucava" button that lands on the right page; without it there is no such button.
- [ ] Turn on **Summary after each activity**, process a new activity: exactly one summary mail. A reprocess from the UI sends none.
- [ ] Turn on **Weekly summary** on a Monday after 08:00 with activities last week: one mail, not a second one on the next check (15 min). Turn the toggle off: nothing.
- [ ] Turn off **Failure alerts**: a failure still shows in the Notifications list and reaches ntfy/webhook, but no mail.
- [ ] Stop the Dexcom share (or block the source) for longer than `gap_alert_hours`: one "No glucose readings" alert, not one per check; when readings return and stop again, a new one.
- [ ] Weekly summary in a week without activities: the short "no activities" note, once.
- [ ] Monthly health report on the 1st after 08:00 (or temporarily set `mail_health_last` in the past via a test data dir): one mail with sensible numbers.
- [ ] Activity summary shows the glucose chart and the weekly summary the bar chart in your real mail client (also on the phone).
- [ ] Settings → Notifications: ntfy, webhook (check the `X-Glucava-Signature`) and email each deliver "Send test notification".

## Data

- [ ] Import a Glooko (or Libre/Nightscout) export with `glucava glucose import`, then reprocess an old activity.
- [ ] Settings → Your data: CSV export opens; retention setting sticks after a restart (and `GLUCAVA_RETENTION_DAYS` overrides it when set).

## Scripted setup

- [ ] `glucava config apply settings.conf` twice: second run prints `unchanged`. A bad value exits non-zero and changes nothing.
- [ ] `printf '%s\n' pw | glucava secrets set smtp_password` then `secrets status` shows `smtp_password=set`.

## Security spot checks

- [ ] `/_/` and `/api/collections` are not reachable (PocketBase blocked).
- [ ] Cookie is `HttpOnly`, and `Secure` behind your TLS proxy; login is rate limited after repeated failures.
- [ ] No secret appears in the container logs (`docker logs`) after all of the above.

## Chart photo (opt-in)

Use a throwaway activity you own. This writes a photo to a real Strava activity and cannot be undone by glucava (delete it in Strava).

- [ ] Settings > Chart photo: changing theme, size, line and the three checkboxes updates the preview at once, without saving; saving keeps the look.
- [ ] With the option on, the activity page shows the chart photo; with it off, it does not.
- [ ] `glucava strava inspect <id>` shows a matching "photo:" line and lists the file input(s).
- [ ] Turn `chart_image` on, process the activity: the chart photo appears once, description text intact.
- [ ] Reprocess: no second photo.
- [ ] Note whether the photo replaced the map as the cover, and whether the upload needs longer than the default 6 s wait.
