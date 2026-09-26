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

Tasker's export files use internal numeric codes that are not safe to
hand-write, so glucava ships no `.prf.xml`. Build it once in the app (about five
minutes), then use Tasker's own sharing to move it to other devices.

1. **Task first.** Tasks tab → **+** → name it `glucava trigger`. Add the action
   **Net → HTTP Request**: Method `POST`, URL `https://your-host/api/trigger`,
   Headers `Authorization: Bearer gst_...`, leave Body empty, Timeout `30`.
   Long-press the task's play button to run it; the Tokens page should show the
   token as used.
2. **Profile.** Profiles tab → **+** → **Event → UI → Notification**. Owner
   Application: **Strava**. Leave Title and Text empty for now. Link the profile
   to the `glucava trigger` task.
3. **Find Strava's wording.** Record an activity (or wait for the next one). When
   Strava posts its "activity saved/uploaded" notification the task fires; if
   it also fires for unrelated Strava notifications (kudos, comments), open the
   profile and add a Title or Text filter (`*upload*`, or whatever your Strava
   version and language actually says). Tasker's Run Log shows the exact text.
4. Android must let Tasker read notifications (Settings → Notification access)
   and must not put Tasker to sleep (battery: unrestricted).

**Sharing it (TaskerNet).** Once it works: long-press the profile (or the
project) → three-dot menu → **Export → As Link**. Tasker uploads it to
TaskerNet and gives you a `https://taskernet.com/shares/?user=...` link that
anyone with Tasker can open and import with one tap. The link can only be
created from the app that holds the profile, which is why one is not
provided here yet. Remove your token from the task before you share it
(replace it with a variable such as `%GLUCAVA_TOKEN` set in a Variable Set
action) and tell importers to fill in their own URL and token.

Untested end to end against a real Strava notification — check that the
profile fires before relying on it. Polling covers you either way.

## HTTP Shortcuts / MacroDroid (Android, no Tasker)

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
