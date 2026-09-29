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
event). While an address is blocked (10 failures in one minute) even a valid
token from it gets `429`; this is deliberate so a brute-force guess cannot
succeed during the block. Other methods than POST get `405`.

The activity may not exist on Strava's side yet when your phone's own
notification fires. That's fine: the trigger only kicks the poller a bit
earlier than its own schedule, and the poller retries with backoff, so a
trigger that arrives "too early" just means the next scheduled poll picks it
up as usual — nothing is lost.

## Try it first

Before building anything on the phone, prove the token and URL work:

```sh
curl -i -X POST -H "Authorization: Bearer gst_..." https://your-host/api/trigger
```

Expect `202`. The Tokens page shows the token's "last used" time afterwards.
Everything below just repeats this one request from the phone.

## Tasker (Android)

The Tokens page generates a ready-to-import profile for you: after creating
a token, a "Download Tasker profile" button and a matching QR code appear
next to it (the QR is for scanning from a different screen, e.g. a desktop;
the download button is for opening the Tokens page on the phone itself).
Importing it in Tasker gives you one profile and one task, done.

**What it does, and why.** The profile fires on Tasker's Notification event,
owner app Strava, **deliberately left unfiltered** — no title or text match.
This is confirmed working, not a guess: a real deployment tested filtering
first and dropped it, because Strava's own notifications vary by type (a new
activity, a kudos, a streak reminder all look different) and by locale, so a
title/text filter is unreliable and needs constant retuning. Firing on every
Strava notification instead is harmless: the trigger endpoint only means
"check now", it's idempotent, cheap, and already rate-limited, so an extra
call just means glucava checked a little earlier than the next scheduled
poll would have anyway.

The task is one HTTP Request action: `POST` to `/api/trigger` with your
token in the `Authorization` header, 30s timeout — same request `curl`
above sends. Android must let Tasker read notifications (Settings →
Notification access) and must not put Tasker to sleep (battery:
unrestricted).

**Building it by hand instead.** If you'd rather not import a generated
file, or want to tweak it (e.g. add your own title/text filter after all):
Tasks tab → **+** → name it `glucava trigger`, action **Net → HTTP
Request**: Method `POST`, URL `https://your-host/api/trigger`, Headers
`Authorization: Bearer gst_...`, leave Body empty, Timeout `30`. Then
Profiles tab → **+** → **Event → UI → Notification**, owner Application
**Strava**, leave Title and Text empty (or fill them in if you want
filtering — Tasker's Run Log shows the exact notification text your Strava
version and language actually use). Link the profile to the task.

**Sharing it (TaskerNet).** Long-press the profile (or the project) →
three-dot menu → **Export → As Link**. Tasker uploads it to TaskerNet and
gives you a `https://taskernet.com/shares/?user=...` link that anyone with
Tasker can open and import with one tap — useful for sharing your own tuned
version, since the generated download already has your token baked in and
isn't meant to be shared further as-is.

## HTTP Shortcuts / MacroDroid (Android, no Tasker)

These can't be pre-built as a file the way Tasker's profile is, so the
Tokens page's second QR code (next to the Tasker one, right after creating a
token) just opens the Tokens page itself on whatever device scans it — with
your real token already filled into the examples below instead of a
placeholder.

Both apps can do this without a profile file:

1. New shortcut/macro, trigger: notification posted by the Strava app.
2. Action: HTTP request, `POST` to `https://your-host/api/trigger`, header
   `Authorization: Bearer gst_...`.

No response body handling needed; any 2xx/4xx is fine to ignore.

## Apple Shortcuts (iOS)

iOS cannot start a shortcut from another app's notification, so there is no
"fires the instant Strava posts". The workable flow:

1. Shortcuts → **Automation** → **+** → **Workout** (Apple Watch/Health) →
   **Ends**, or **App → Strava → Is Closed** if you record with Strava itself.
   Set it to **Run Immediately** so it does not ask each time.
2. Actions: **Wait** 30 seconds (Strava needs a moment to save), then
   **Get Contents of URL** → `https://your-host/api/trigger`, Method **POST**,
   Headers `Authorization` = `Bearer gst_...`, no request body.

**Sharing it.** A shortcut exports as an iCloud link from the Shortcuts app
(long-press → Share → Copy iCloud Link). Apple only signs shortcuts on the
device, so a ready-made file cannot be provided here; the link works only if
you create it. Automations themselves cannot be shared, only the shortcut they
run, so a shared link contains the "Wait + Get Contents of URL" part and each
user adds the trigger.

**Marked untested** — the trigger and timing have not been verified against a
real Strava save. A shortcut that fires early or not at all just falls back to
the next scheduled poll.
