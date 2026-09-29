// Package taskerprofile generates a Tasker .prf.xml that POSTs to glucava's
// push trigger endpoint. The shape is copied byte-for-byte (apart from the
// substituted host and token) from a profile a real user built and tested by
// hand: a Notification event (Tasker event code 461) on the Strava app,
// deliberately left unfiltered (no title/text match), triggering one task
// with an HTTP Request action (code 339) that POSTs to /api/trigger with the
// bearer token.
//
// Left unfiltered on purpose: Strava's own notifications vary by type
// (a new activity, a kudos, a streak reminder all look different) and by
// locale, so a title/text filter is unreliable. The trigger endpoint is
// idempotent, cheap and already rate-limited, so firing on every Strava
// notification is harmless — it only ever means "check now a bit earlier
// than the next scheduled poll would have anyway".
package taskerprofile

import (
	"fmt"
	"strings"
)

// xmlEscape escapes the handful of characters XML text/attribute content
// cannot contain literally. baseURL and token are both effectively trusted
// (the admin's own configured public URL, and glucava's own generated
// token, which is base64url so none of these ever appear in it), but
// escaping is cheap and this keeps the generator correct regardless.
func xmlEscape(s string) string {
	r := strings.NewReplacer(`&`, "&amp;", `<`, "&lt;", `>`, "&gt;", `"`, "&quot;", `'`, "&apos;")
	return r.Replace(s)
}

// Build renders a Tasker profile+task import file that POSTs to
// baseURL+"/api/trigger" with the given bearer token whenever Tasker sees a
// notification from the Strava app.
func Build(baseURL, token string) []byte {
	url := xmlEscape(strings.TrimRight(baseURL, "/") + "/api/trigger")
	auth := xmlEscape("Authorization:Bearer " + token)
	return []byte(fmt.Sprintf(taskerXML, url, auth))
}

const taskerXML = `<TaskerData sr="" dvi="1" tv="6.6.20">
	<Profile sr="prof1" ve="2">
		<id>1</id>
		<mid0>2</mid0>
		<nme>Glucava</nme>
		<Share sr="Share">
			<b>false</b>
			<d>Checks glucava now when Strava posts a notification. Deliberately unfiltered: notification wording varies by type and locale, and an extra check is harmless.</d>
			<g>Notifications</g>
			<p>false</p>
			<t></t>
		</Share>
		<Event sr="con0" ve="2">
			<code>461</code>
			<pri>0</pri>
			<App sr="arg0">
				<appClass>com.strava.SplashActivity</appClass>
				<appPkg>com.strava</appPkg>
				<label>Strava</label>
			</App>
			<Str sr="arg1" ve="3"/>
			<Str sr="arg2" ve="3"/>
			<Str sr="arg3" ve="3"/>
			<Str sr="arg4" ve="3"/>
			<Str sr="arg5" ve="3"/>
			<Str sr="arg6" ve="3"/>
			<Int sr="arg7" val="0"/>
		</Event>
	</Profile>
	<Task sr="task2">
		<id>2</id>
		<nme>Glucava Trigger</nme>
		<pri>6</pri>
		<Action sr="act0" ve="7">
			<code>339</code>
			<Int sr="arg1" val="1"/>
			<Int sr="arg10" val="0"/>
			<Int sr="arg11" val="0"/>
			<Int sr="arg12" val="1"/>
			<Str sr="arg2" ve="3">%s</Str>
			<Str sr="arg3" ve="3">%s</Str>
			<Str sr="arg4" ve="3"/>
			<Str sr="arg5" ve="3"/>
			<Str sr="arg6" ve="3"/>
			<Str sr="arg7" ve="3"/>
			<Int sr="arg8" val="30"/>
			<Int sr="arg9" val="0"/>
		</Action>
	</Task>
</TaskerData>
`
