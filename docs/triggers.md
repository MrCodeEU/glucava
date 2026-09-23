# Push trigger setup

The polling loop finds new activities on its own (Settings → poll interval),
usually within a few minutes. A push trigger is only useful to skip that wait:
your phone posts to `/api/trigger` as soon as it sees the "activity saved to
Strava" notification, so processing starts immediately.

The endpoint carries no data — it only means "check now". Create a token first
(Tokens page in the UI, shown once), then:

```sh
curl -X POST -H "Authorization: Bearer gst_..." https://your-host/api/trigger
```

Responses: `202 {"status":"queued"}` (or `"already_pending"` if one is already
in flight), `401` for a missing/invalid token, `429` after repeated failed
attempts from the same address (rate-limited, logged as a `trigger_rejected`
event).

The activity may not exist on Strava's side yet when your phone's own
notification fires. That's fine: the trigger only kicks the poller a bit
earlier than its own schedule, and the poller retries with backoff, so a
trigger that arrives "too early" just means the next scheduled poll picks it
up as usual — nothing is lost.

## Tasker (Android)

No importable `.prf.xml` is shipped — Tasker's export format uses internal
numeric codes per action/event type that aren't safe to hand-write without
testing against the real app, and a wrong one fails silently. Build it in the
UI instead, it's two steps:

1. **New profile → Event → Notification.** App: Strava. Leave the title/text
   filters empty at first, enable the profile, trigger a real Strava save,
   then check Tasker's notification log to see the exact title Strava used
   (it varies by app version/locale) and fill that in as the title filter —
   more reliable than guessing the wording up front.
2. **New task → Net → HTTP Request.** Method `POST`, URL
   `https://your-host/api/trigger`, header
   `Authorization: Bearer gst_...` (the token from the Tokens page). No body.

Untested end to end against a real Strava notification — verify the profile
actually fires before relying on it, and fall back to polling if it doesn't
(the poll interval already covers you within a few minutes either way).

## HTTP Shortcuts / MacroDroid (Android, no Tasker)

Both apps can do this without a profile file:

1. New shortcut/macro, trigger: notification posted by the Strava app.
2. Action: HTTP request, `POST` to `https://your-host/api/trigger`, header
   `Authorization: Bearer gst_...`.

No response body handling needed; any 2xx/4xx is fine to ignore.

## Apple Shortcuts (iOS)

iOS personal automations cannot trigger on an arbitrary app's notification
content the way Tasker can, so there is no equivalent "fires the instant
Strava posts". The documented flow instead is:

1. Automation trigger: **Workout ends** (or app-open on Strava, if you open
   it right after a run), then a short **Wait** (Strava needs a few seconds
   to save the activity).
2. Action: **Get Contents of URL** — `POST` to `https://your-host/api/trigger`,
   header `Authorization: Bearer gst_...`.

**Marked untested** — the Workout-ends trigger and timing have not been
verified against a real Strava save. Polling covers the gap regardless, so a
Shortcut that fires a little early or not at all just falls back to the next
scheduled poll.
