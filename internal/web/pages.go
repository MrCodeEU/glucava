package web

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"
	g "maragu.dev/gomponents"
	. "maragu.dev/gomponents/html"

	"github.com/MrCodeEU/glucava/internal/analytics"
	"github.com/MrCodeEU/glucava/internal/jobs"
	"github.com/MrCodeEU/glucava/internal/logging"
	"github.com/MrCodeEU/glucava/internal/render"
	"github.com/MrCodeEU/glucava/internal/stats"
	"github.com/MrCodeEU/glucava/internal/store"
	"github.com/MrCodeEU/glucava/internal/tokens"
)

// qrDataURI renders content as a PNG QR code, inline as a data: URI so it
// needs no extra route or static file. Returns "" if encoding fails (a
// malformed content string), so a caller can skip the image rather than
// break the page.
func qrDataURI(content string) string {
	png, err := qrcode.Encode(content, qrcode.Medium, 240)
	if err != nil {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
}

// jsQuote escapes s for use inside a single-quoted JavaScript string in a Datastar expression.
var jsQuote = strings.NewReplacer(`\`, `\\`, `'`, `\'`, "\n", `\n`, "\r", `\r`, "<", `\x3c`).Replace

func post(url string) g.Node {
	return g.Attr("data-on:click", fmt.Sprintf("@post('%s')", jsQuote(url)))
}

// postThenGo is post, but navigates to dest once the request finishes. CSP
// here has no 'unsafe-inline', so a server-sent ExecuteScript (an injected
// <script> tag, e.g. datastar.Redirect) is silently blocked by the browser;
// navigating from inside this already-permitted eval'd expression
// ('unsafe-eval' is granted for exactly this) is not. Datastar compiles
// data-on expressions with the plain (non-async) Function constructor, so
// a top-level "await" throws GenerateExpression at click time; chain with
// .then() instead, which works on the plain Promise @post(...) returns.
func postThenGo(url, dest string) g.Node {
	return g.Attr("data-on:click", fmt.Sprintf("@post('%s').then(() => window.location='%s')", jsQuote(url), jsQuote(dest)))
}

// SessionInfo summarises the stored Strava cookies and the last session test.
type SessionInfo struct {
	Configured bool
	HasSession bool // the session cookie is among the stored cookies
	Cookies    []CookieInfo
	CheckedAt  time.Time
	CheckOK    bool
	CheckErr   string

	// CanFindActivity gates the "process a specific activity" card: whether
	// looking an activity id up on Strava is wired up at all (it isn't in demo
	// mode, or in any deployment that hasn't set web.Server.FindActivity).
	CanFindActivity bool
}

// CookieInfo is a cookie name and expiry, never its value.
type CookieInfo struct {
	Name    string
	Expires time.Time // zero for a session cookie
}

func (s SessionInfo) headline() (value, sub string) {
	switch {
	case !s.Configured:
		return "Not set up", "Import your Strava cookies"
	case !s.CheckedAt.IsZero() && !s.CheckOK:
		return "Expired", "Last test failed"
	case !s.CheckedAt.IsZero():
		return "Valid", "Tested " + s.CheckedAt.Format("2 Jan 15:04")
	}
	return "Stored", fmt.Sprintf("%d cookies, not tested yet", len(s.Cookies))
}

// ---------------------------------------------------------------- dashboard

// DashData feeds the dashboard.
type DashData struct {
	Acts    []jobs.Activity
	Unit    render.Unit
	Loc     *time.Location
	Now     time.Time
	Session SessionInfo

	// Latest is the most recent glucose reading, if the source made one
	// available quickly. Nil means unknown, not necessarily unavailable.
	Latest *stats.Sample

	// Day is the last 24 hours of readings, oldest first, and DayTIR their
	// five-band split; Thr says where the bands lie. All optional: without
	// readings the "now" card explains that instead of drawing.
	Day    []stats.Sample
	DayTIR analytics.TIR5
	Thr    analytics.Thresholds
}

// LiveDash is the part of the dashboard that updates without a reload.
func LiveDash(d DashData) g.Node {
	var done, failed, tirN int
	var tirSum float64
	cutoff := d.Now.Add(-14 * 24 * time.Hour)
	for _, a := range d.Acts {
		if a.Status == jobs.StatusFailed {
			failed++
		}
		if a.Status == jobs.StatusDone && a.Start.After(cutoff) {
			done++
			if a.Summary != nil {
				tirSum += a.Summary.TIR
				tirN++
			}
		}
	}
	tir := "-"
	if tirN > 0 {
		tir = fmt.Sprintf("%.0f%%", tirSum/float64(tirN))
	}
	sv, ss := d.Session.headline()
	attention := "Nothing to fix"
	if failed > 0 {
		attention = "Open an activity to reprocess"
	}

	return Div(ID("live"),
		nowCard(d),
		Grid("",
			Tile("Annotated, 14 days", fmt.Sprint(done), "activities with a glucose block"),
			Tile("Average time in range", tir, "across those activities"),
			Tile("Failed", fmt.Sprint(failed), attention),
			Tile("Strava session", sv, ss),
		),
		Card(
			H2(g.Text("Recent activities")),
			activityTable(d),
		),
	)
}

func activityTable(d DashData) g.Node {
	if len(d.Acts) == 0 {
		hint := "New Strava activities appear here once polling finds them. Use “Check Strava now” to look right away."
		var action []g.Node
		if !d.Session.Configured {
			hint = "glucava needs your Strava session before it can find activities."
			action = append(action, A(append(comp("button"), Href("/strava"), g.Text("Set up the Strava session"))...))
		}
		return EmptyState("activity", "No activities yet", hint, action...)
	}
	rows := make([]g.Node, 0, len(d.Acts))
	for _, a := range d.Acts {
		rows = append(rows, activityRow(a, d))
	}
	return Div(append(comp("tablewrap"),
		Table(append(comp("table"),
			THead(Tr(
				Th(g.Text("When")), Th(g.Text("Activity")), Th(Class("hide-sm"), g.Text("Duration")),
				Th(Class("hide-sm"), g.Text("Distance")),
				Th(g.Text("Time in range")), Th(Class("hide-sm num"), g.Text("Min / Max")),
				Th(Class("hide-sm num"), g.Text("Avg")), Th(g.Text("Status")),
			)),
			TBody(g.Group(rows)),
		)...),
	)...)
}

func activityRow(a jobs.Activity, d DashData) g.Node {
	href := "/activity/" + a.StravaID
	name := a.Name
	if name == "" {
		name = "Activity " + a.StravaID
	}
	tirCell, minmax, avg := g.Node(Span(Class("muted"), g.Text("-"))), "-", "-"
	if a.Summary != nil {
		s := a.Summary
		tirCell = tirBar(*s)
		minmax = render.Value(s.Min, d.Unit) + " / " + render.Value(s.Max, d.Unit)
		avg = render.Value(s.Avg, d.Unit)
	}
	return Tr(g.Attr("data-href", href), g.Attr("data-on:click", fmt.Sprintf("window.location='%s'", jsQuote(href))),
		Td(g.Text(fmtWhen(a.Start, d.Loc, d.Now))),
		Td(Div(Class("flex items-start gap-2"),
			Span(Class("mt-0.5 text-ink-2"), g.Attr("title", orDash(a.Sport)), icon(sportIcon(a.Sport), "size-4")),
			Div(A(Href(href), g.Text(name)), g.If(a.Sport != "", Span(Class("muted"), g.Text(" · "+a.Sport)))))),
		Td(Class("hide-sm"), g.Text(fmtDuration(a.Duration))),
		Td(Class("hide-sm"), distanceCell(a)),
		Td(tirCell),
		Td(Class("hide-sm num"), g.Text(minmax)),
		Td(Class("hide-sm num"), g.Text(avg)),
		Td(StatusBadge(a.Status)),
	)
}

// distanceCell is "12.0 km · 5:30 /km", or a dash when the activity has no distance.
func distanceCell(a jobs.Activity) g.Node {
	if a.Distance <= 0 {
		return Span(Class("muted"), g.Text("-"))
	}
	txt := fmt.Sprintf("%.1f km", a.Distance/1000)
	if pace := render.FormatPace(a.Sport, a.Distance, a.Duration); pace != "" {
		txt += " · " + pace
	}
	return g.Text(txt)
}

func tirBar(s stats.Summary) g.Node {
	w := func(v float64) g.Node { return Span(g.Attr("style", fmt.Sprintf("width:%.1f%%", v))) }
	return Div(append(comp("tircell"),
		B(g.Textf("%.0f%%", s.TIR)),
		Div(append(comp("tirbar"), g.Attr("role", "img"),
			g.Attr("aria-label", fmt.Sprintf("%.0f%% below, %.0f%% in range, %.0f%% above", s.Below, s.TIR, s.Above)),
			w(s.Below), w(s.TIR), w(s.Above))...),
	)...)
}

// DashboardPage is the home page.
func DashboardPage(pd PageData, d DashData) g.Node {
	return Page(pd,
		PageHead("Activities", "Strava activities and the glucose data added to them.",
			IndicatorBtn("primary", "Check Strava now", "/actions/poll", "polling")),
		Div(g.Attr("data-init", "@get('/stream/live')"), LiveDash(d)),
	)
}

// ----------------------------------------------------------------- activity

// ActivityData feeds the activity page.
type ActivityData struct {
	Act     jobs.Activity
	Samples []stats.Sample
	Cfg     store.Config
	Block   string // description block that was or would be written
	// Step is the pipeline's current step, while Act.Status is "processing".
	// Empty means unknown (no run in progress, or progress tracking is off).
	Step   string
	HR     *stats.HRSummary
	Events []store.EventRow
	Loc    *time.Location
	Now    time.Time

	Thr     analytics.Thresholds
	Insight *analytics.ActivityInsight // before/during/after numbers; nil without readings
	// Artifacts are suspected sensor artifacts in the activity's glucose window.
	Artifacts []analytics.Artifact
	Rank      *SportRank   // nil until there are enough activities of this sport
	Prev      *ActivityRef // the activity before and after this one, by start time
	Next      *ActivityRef
}

// ActivityPage shows one activity with its glucose chart.
func ActivityPage(pd PageData, d ActivityData) g.Node {
	a := d.Act
	title := a.Name
	if title == "" {
		title = "Activity " + a.StravaID
	}

	return Page(pd,
		PageHead(title, fmt.Sprintf("%s · %s · %s", fmtWhen(a.Start, d.Loc, d.Now), fmtDuration(a.Duration), orDash(a.Sport)),
			g.Group(activityNav(d)),
			A(append(comp("button"), Href("/"), g.Text("Back"))...),
			A(append(comp("button"), Href("https://www.strava.com/activities/"+a.StravaID),
				Target("_blank"), Rel("noopener noreferrer"), g.Text("View on Strava ↗"))...),
			g.If(a.Original != nil, Btn("", "Restore original", post("/actions/restore/"+a.StravaID))),
			g.If(d.Cfg.ChartImage, Btn("", "Attach chart again", post("/actions/chart/"+a.StravaID))),
			IndicatorBtn("primary", "Reprocess", "/actions/reprocess/"+a.StravaID, "reprocessing"),
			ConfirmDialog("delete-dialog", "danger", "Delete", "Delete this activity?",
				"Removes glucava's record of this activity. This does not touch Strava, other than trying to restore the original description first if one was saved.",
				postThenGo("/actions/delete/"+a.StravaID, "/"))),
		Div(g.Attr("data-init", "@get('"+jsQuote("/stream/activity/"+a.StravaID)+"')"), ActivityBody(d)),
	)
}

// processingText is the "Working on it" body: the live step name when it is
// known, or the old generic wording otherwise (progress tracking is off, or
// the step just hasn't arrived yet).
func processingText(step string) string {
	if step == "" {
		return "Starting the browser and writing to Strava usually takes under a minute; this page updates by itself."
	}
	return step + "… this page updates by itself."
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func eventList(evs []store.EventRow, loc *time.Location, now time.Time) g.Node {
	if len(evs) == 0 {
		return P(Class("muted"), g.Text("No notifications for this activity."))
	}
	items := make([]g.Node, 0, len(evs))
	for _, e := range evs {
		items = append(items, P(SeverityBadge(e.Severity), g.Text(" "+fmtWhen(e.Created, loc, now)+" · "+e.Message)))
	}
	return g.Group(items)
}

// ------------------------------------------------------------------- strava

// StravaStatusCard is patched after imports and tests.
func StravaStatusCard(s SessionInfo) g.Node {
	value, sub := s.headline()
	list := g.Node(P(Class("muted"), g.Text("No cookies stored.")))
	if len(s.Cookies) > 0 {
		rows := make([]g.Node, 0, len(s.Cookies))
		for _, c := range s.Cookies {
			exp := "browser session"
			if !c.Expires.IsZero() {
				exp = c.Expires.Format("2 Jan 2006")
			}
			rows = append(rows, Tr(Td(Code(g.Text(c.Name))), Td(g.Text(exp))))
		}
		list = Div(append(comp("tablewrap"), Table(append(comp("table"),
			THead(Tr(Th(g.Text("Cookie")), Th(g.Text("Expires")))), TBody(g.Group(rows)))...))...)
	}
	return Card(ID("strava-status"),
		H2(g.Text("Session status")),
		P(Strong(g.Text(value+". ")), g.Text(sub+".")),
		g.If(s.Configured && !s.HasSession, Notice("warning", g.Text("The session cookie (_strava4_session) is missing, so Strava will probably reject these cookies."))),
		g.If(s.CheckErr != "", Notice("error", g.Text(s.CheckErr))),
		list,
		Div(append(comp("actions"), IndicatorBtn("", "Test session", "/actions/strava/test", "testing"))...),
	)
}

// StravaPage is the cookie import page.
func StravaPage(pd PageData, s SessionInfo) g.Node {
	return Page(pd,
		PageHead("Strava session", "glucava logs in to Strava with your own browser cookies. It does not use the Strava API."),
		StravaStatusCard(s),
		Card(g.Attr("data-signals", `{"loginEmail":"","loginPassword":""}`),
			H2(g.Text("Sign in automatically (experimental)")),
			Notice("warning", g.Text("Unverified against the real Strava login form. It gives up at the first CAPTCHA, verification code or wrong-password message and never guesses past one; nothing is stored unless it reaches your dashboard. If it fails, use cookie import below instead. Strava may notice a new sign-in and email you about it, same as any other browser login.")),
			Field("loginEmail", "Strava email", "", Input(ID("loginEmail"), Type("email"), AutoComplete("off"), g.Attr("data-bind", "loginEmail"))),
			Field("loginPassword", "Strava password", "Never stored; only the resulting session cookies are, exactly like cookie import.",
				Input(ID("loginPassword"), Type("password"), AutoComplete("off"), g.Attr("data-bind", "loginPassword"))),
			IndicatorBtn("", "Try automatic sign-in", "/actions/strava/login", "signingin"),
		),
		Card(g.Attr("data-signals", `{"cookies":""}`),
			H2(g.Text("Import cookies")),
			P(g.Text("Log in to strava.com in your browser, export the cookies for strava.com, and paste them here. Three formats work:")),
			Ul(
				Li(g.Text("a JSON export from a cookie extension (Cookie-Editor and similar)")),
				Li(g.Text("a Netscape cookies.txt file")),
				Li(g.Text("a Cookie header copied from the browser's network tab")),
			),
			Field("cookies", "Cookies", "Stored encrypted. Only the cookie names are ever shown again.",
				Textarea(ID("cookies"), g.Attr("data-bind", "cookies"), Placeholder("Paste cookies here"), g.Attr("spellcheck", "false"), g.Attr("autocomplete", "off"))),
			Btn("primary", "Import cookies", post("/actions/strava/cookies")),
		),
		g.If(s.CanFindActivity, Card(g.Attr("data-signals", `{"processActivityId":""}`),
			H2(g.Text("Process a specific activity")),
			P(Class("muted"), g.Text("Write the glucose description onto an activity that was never auto-detected, for "+
				"example one from before glucava was running, or older than the polling window. It is looked up in your "+
				"Strava training log if it isn't already known here.")),
			Field("processActivityId", "Strava activity id", "The number at the end of the activity's URL on strava.com.",
				Input(ID("processActivityId"), Type("text"), g.Attr("inputmode", "numeric"), Placeholder("1234567890"),
					AutoComplete("off"), g.Attr("data-bind", "processActivityId"))),
			IndicatorBtn("primary", "Process activity", "/actions/process", "processing"),
		)),
		Card(H2(g.Text("Good to know")),
			Ul(
				Li(g.Text("Automating the Strava website goes against Strava's terms of service. glucava uses one browser, one account and a few page loads per activity. Use it at your own risk.")),
				Li(g.Text("Treat these cookies like a password: anyone who has them is logged in as you. Log out of the browser session you exported from only if you want to invalidate them.")),
				Li(g.Text("When Strava ends the session, you get a notification and need to import fresh cookies.")),
			)),
	)
}

// ----------------------------------------------------------------- settings

// SettingsData feeds the settings page.
type SettingsData struct {
	AccountEmail                                                       string
	Cfg                                                                store.Config
	HasDexcomPassword, HasNtfyToken, HasWebhookSecret, HasSMTPPassword bool
	ImportFormats                                                      []string // importers.Names(); empty hides the import card
	ImportOK, ImportErr                                                string   // one-shot flash after /actions/glucose/import redirects back
	DescPreview                                                        string   // rendered preview of Cfg.DescriptionTemplate (or the default), before any edit
	Push                                                               PushCardData
}

// AccountEmail shows the signed-in address; it is patched after a change.
func AccountEmail(email string) g.Node {
	return P(ID("account-email"), g.Text("Signed in as "), Strong(g.Text(email)))
}

// secretHelp describes whether a secret field has a stored value.
func secretHelp(set bool, what string) string {
	if set {
		return what + " is stored. Leave empty to keep it, or type a new one to replace it."
	}
	return what + " is not set."
}

// DexcomSecretStatus renders the part of the settings page that depends on
// whether the Dexcom password is stored: the help text under the field, plus
// the Test connection button. NtfySecretStatus and WebhookSecretStatus below
// do the same for their own secrets. Each has a stable ID so actionSettings
// can patch it in place after a save, without a full page reload.
func DexcomSecretStatus(has bool) g.Node {
	return Div(ID("dexcom-secret-status"),
		Div(Class("help"), g.Text(secretHelp(has, "A password"))),
		g.If(has, Div(append(comp("actions"),
			IndicatorBtn("", "Test connection", "/actions/dexcom/test", "dxtest"),
			IndicatorBtn("", "Resync now", "/actions/glucose/resync", "dxresync"))...)),
	)
}

func NtfySecretStatus(has bool) g.Node {
	return Div(ID("ntfy-secret-status"), Class("help"),
		g.Text(secretHelp(has, "A token")+" Only needed for protected topics."))
}

func WebhookSecretStatus(has bool) g.Node {
	return Div(ID("webhook-secret-status"), Class("help"),
		g.Text(secretHelp(has, "A secret")+" Used for the X-Glucava-Signature header."))
}

func SMTPSecretStatus(has bool) g.Node {
	return Div(ID("smtp-secret-status"), Class("help"), g.Text(secretHelp(has, "A password")))
}

// GlucoseImportStatus renders the outcome of a glucose import: exactly one
// of ok/errMsg is non-empty, or both empty for the initial page render. It
// has a stable ID so both the plain-form flash render and the progressive-
// enhancement fetch response (static/glucose-import.js) use the same markup,
// letting the script swap it in without a page reload.
func GlucoseImportStatus(ok, errMsg string) g.Node {
	return Div(ID("glucose-import-status"),
		g.If(ok != "", Notice("ok", g.Text(ok))),
		g.If(errMsg != "", Notice("error", g.Text(errMsg))),
	)
}

func importFormatOptions(names []string) g.Node {
	opts := make([]g.Node, len(names))
	for i, n := range names {
		opts[i] = Option(Value(n), g.Text(n))
	}
	return g.Group(opts)
}

// SettingsPage is the settings form. Its values live in Datastar signals.
func SettingsPage(pd PageData, d SettingsData) g.Node {
	c := d.Cfg
	sigMap := map[string]any{
		"unit": c.Unit, "rangeLow": c.RangeLow, "rangeHigh": c.RangeHigh, "veryLow": c.VeryLow, "veryHigh": c.VeryHigh, "preMin": c.PreMin, "postMin": c.PostMin,
		"pollMin": c.PollMin, "dexcomRegion": c.DexcomRegion, "dexcomUsername": c.DexcomUsername,
		"dexcomPassword": "", "ntfyURL": c.NtfyURL, "ntfyToken": "", "webhookURL": c.WebhookURL, "webhookSecret": "",
		"emailTo":  c.EmailTo,
		"smtpHost": c.SMTPHost, "smtpPort": c.SMTPPort, "smtpUsername": c.SMTPUsername, "smtpPassword": "",
		"smtpTLS": c.SMTPTLS, "smtpSender": c.SMTPSender, "smtpSenderName": c.SMTPSenderName, "retentionDays": c.RetentionDays, "purgeConfirm": "",
		"publicURL": c.PublicURL, "mailAlerts": c.MailAlerts, "mailActivity": c.MailActivity, "mailWeekly": c.MailWeekly, "mailHealth": c.MailHealth, "gapAlertHours": c.GapAlertHours, "chartImage": c.ChartImage,
		"chartTheme": c.ChartTheme, "chartSize": c.ChartSize, "chartBand": c.ChartBand, "chartActivity": c.ChartActivity, "chartDots": c.ChartDots, "chartLine": c.ChartLine, "chartHR": c.ChartHR, "chartElevation": c.ChartElevation, "chartPre": c.ChartPreMin, "hrRead": c.HRRead, "postBuffer": c.PostBufferMin,
		"chartAvgLine": c.ChartAvgLine, "chartRangeLines": c.ChartRangeLines, "chartMinMax": c.ChartMinMax, "chartHideStats": c.ChartHideStats,
		"chartPanelOrder": defaultStr(c.ChartPanelOrder, "activity,band,dots,hr"),
		"descTemplate":    c.DescriptionTemplate, "descPreset": descPresetIDFor(c.DescriptionTemplate), "descPreview": d.DescPreview,
		"settingsTab": "glucose",
	}
	for k, v := range overviewSignals(c) {
		sigMap[k] = v
	}
	for k, v := range pushSettingSignals(c) {
		sigMap[k] = v
	}
	sig, _ := json.Marshal(sigMap)
	bind := func(name string) g.Node { return g.Attr("data-bind", name) }

	return Page(pd,
		PageHead("Settings", "Changes apply to the next poll; no restart needed."),
		Div(g.Attr("data-signals", string(sig)),
			settingsTabs(),
			tabSection("glucose",
				Grid("2",
					Card(H2(g.Text("Glucose and timing")),
						Field("unit", "Unit", "", Select(ID("unit"), bind("unit"),
							Option(Value("mg/dL"), g.Text("mg/dL")), Option(Value("mmol/L"), g.Text("mmol/L")))),
						Div(append(comp("fieldrow"),
							Field("rangeLow", "Target low (mg/dL)", "", Input(ID("rangeLow"), Type("number"), Min("40"), Max("200"), bind("rangeLow"))),
							Field("rangeHigh", "Target high (mg/dL)", "", Input(ID("rangeHigh"), Type("number"), Min("80"), Max("400"), bind("rangeHigh"))),
							Field("veryLow", "Very low below (mg/dL)", "Level-2 hypoglycemia threshold (consensus: 54). Used by the Overview analytics.", Input(ID("veryLow"), Type("number"), Min("20"), Max("100"), bind("veryLow"))),
							Field("veryHigh", "Very high above (mg/dL)", "Level-2 hyperglycemia threshold (consensus: 250).", Input(ID("veryHigh"), Type("number"), Min("150"), Max("600"), bind("veryHigh"))),
						)...),
						Div(append(comp("fieldrow"),
							Field("preMin", "Minutes before start", "", Input(ID("preMin"), Type("number"), Min("0"), Max("240"), bind("preMin"))),
							Field("postMin", "Minutes after end", "Cooldown glucose shown on the chart photo only; the description text is always activity-only and does not wait for this.", Input(ID("postMin"), Type("number"), Min("0"), Max("240"), bind("postMin"))),
						)...),
						Field("pollMin", "Check Strava every (minutes)", "", Input(ID("pollMin"), Type("number"), Min("1"), Max("1440"), bind("pollMin"))),
						Field("postBuffer", "Reprocess once more after (minutes)", "Catches glucose readings that had not arrived yet the first time, for both the description text and (with the chart photo on) its first upload, which waits for this so it is not attached with an incomplete curve. 0 turns both off.", Input(ID("postBuffer"), Type("number"), Min("0"), Max("180"), bind("postBuffer"))),
					),
					Card(H2(g.Text("Dexcom Share")),
						P(Class("muted"), g.Text("Turn on Dexcom Share in the Dexcom app first. The account is the one that owns the sensor.")),
						Field("dexcomRegion", "Region", "", Select(ID("dexcomRegion"), bind("dexcomRegion"),
							Option(Value("ous"), g.Text("Outside the US")), Option(Value("us"), g.Text("United States")), Option(Value("jp"), g.Text("Japan")))),
						Field("dexcomUsername", "Username, email or phone", "", Input(ID("dexcomUsername"), Type("text"), AutoComplete("off"), bind("dexcomUsername"))),
						Field("dexcomPassword", "Password", "", Input(ID("dexcomPassword"), Type("password"), AutoComplete("new-password"), bind("dexcomPassword"))),
						DexcomSecretStatus(d.HasDexcomPassword),
					),
				),
			),
			tabSection("customize",
				Card(H2(g.Text("Chart photo")),
					P(Class("muted"), g.Text("Optionally attach the glucose chart to the Strava activity as a photo. The preview below updates as you change the options, using your latest activity (or sample data), before anything is saved.")),
					Field("chartImage", "Attach the chart to each activity", "Once per activity. Experimental: glucava cannot remove photos again, and a first photo can replace the map as the activity's cover.", Input(ID("chartImage"), Type("checkbox"), bind("chartImage"))),
					Grid("2",
						Div(
							Field("chartTheme", "Theme", "", Select(ID("chartTheme"), bind("chartTheme"),
								Option(Value("light"), g.Text("Light")), Option(Value("dark"), g.Text("Dark")))),
							Field("chartSize", "Size", "Strava shows the photo as a square. Large is 1620 px, otherwise 1080 px.", Select(ID("chartSize"), bind("chartSize"),
								Option(Value("standard"), g.Text("Standard (1080 px)")), Option(Value("large"), g.Text("Large (1620 px)")))),
							Field("chartPre", "Glucose before the activity (minutes)", "How far back the chart starts. Only the chart uses this; the numbers use the window under Glucose and timing.", Input(ID("chartPre"), Type("number"), Min("0"), Max("240"), bind("chartPre"))),
							Field("chartLine", "Line thickness (1 to 4)", "", Input(ID("chartLine"), Type("number"), Min("1"), Max("4"), bind("chartLine"))),
							Field("hrRead", "Read heart rate from Strava", "Fetched when an activity is processed (one extra page load) and kept, for the numbers on the activity page and in emails, and for the chart.", Input(ID("hrRead"), Type("checkbox"), bind("hrRead"))),
							Label(g.Text("Panels")),
							P(Class("muted"), g.Text("Toggle which layers are drawn, and reorder them: where activity and target-range shading overlap, the one listed lower wins.")),
							Div(g.Attr("style", "display:flex;flex-direction:column"),
								chartPanelRow("chartActivity", "activity", "Shade the activity span"),
								chartPanelRow("chartBand", "band", "Shade the target range"),
								chartPanelRow("chartDots", "dots", "Mark out-of-range readings"),
								chartPanelRow("chartHR", "hr", "Show heart rate (second axis)"),
								chartPanelRow("chartElevation", "elevation", "Show elevation profile (background; fetched from Strava when on)"),
							),
							Field("chartAvgLine", "Average line", "A dashed line at the average glucose value.", Input(ID("chartAvgLine"), Type("checkbox"), bind("chartAvgLine"))),
							Field("chartRangeLines", "Low/high lines", "Dashed lines at the target range low and high.", Input(ID("chartRangeLines"), Type("checkbox"), bind("chartRangeLines"))),
							Field("chartMinMax", "Min/max markers", "Marker dots at the curve's lowest and highest points.", Input(ID("chartMinMax"), Type("checkbox"), bind("chartMinMax"))),
							Field("chartHideStats", "Hide the TIR number and stats bar", "Turns off the big time-in-range number and the below/in-range/above bar at the bottom.", Input(ID("chartHideStats"), Type("checkbox"), bind("chartHideStats"))),
						),
						Div(
							Img(ID("chartPreview"), Alt("Preview of the chart photo"), g.Attr("style", "max-width:100%;height:auto;border:1px solid var(--border, #ccc);border-radius:8px"),
								g.Attr("data-attr:src", chartPreviewExpr)),
						),
					),
				),
				overviewSettingsCard(c),
				Card(H2(g.Text("Description text")),
					P(Class("muted"), g.Text("What gets appended to the Strava activity description. Pick a preset to start from, or write your own "+
						"(Go text/template syntax: {{.TIR}}, {{.TIRWindow}}, {{.TIRBar}}, {{.TIRWindowBar}}, {{.Min}}, {{.Max}}, {{.Avg}}, {{.StdDev}}, {{.CV}}, {{.GMI}}, {{.VeryLow}}, {{.VeryHigh}}, {{.Unit}}, "+
						"{{.Distance}}, {{.Elevation}}, {{.Pace}}, {{.Sparkline}}; {{if .Sparkline}}...{{end}} to only show a line when it's there). {{.TIR}} is the activity window; "+
						"{{.TIRWindow}} is the wider pre/post window the chart draws from below, so the two can differ — add both if you want to show that. {{.Pace}} is a single "+
						"field that's already the right unit for the activity's sport (pace for a run/hike/walk/swim, speed for a ride), empty when there's no meaningful distance "+
						"metric for the sport. The preview below updates as you type, using your latest activity or sample data.")),
					Field("descPreset", "Preset", "Selecting one replaces the template below; keep editing afterwards to customize it further.",
						g.El("select", append([]g.Node{ID("descPreset"), bind("descPreset"), g.Attr("data-on:change", descPresetChangeExpr),
							Option(Value("custom"), g.Text("Custom"))}, presetOptions()...)...)),
					Grid("2",
						Div(
							Field("descTemplate", "Template", "", Textarea(ID("descTemplate"), Rows("6"), bind("descTemplate"),
								g.Attr("data-on:input__debounce.400ms", descPreviewExpr), g.Attr("spellcheck", "false"))),
						),
						Div(
							Label(g.Text("Preview")),
							Pre(ID("descPreviewBox"), g.Attr("data-text", "$descPreview")),
						),
					),
				),
			),
			tabSection("notify",
				Card(H2(g.Text("Notifications")),
					P(Class("muted"), g.Text("Sent when something fails, for example an expired Strava session or missing glucose data.")),
					Field("gapAlertHours", "Alert when no glucose reading arrives for (hours)", "Catches a stopped sensor share or a broken Dexcom login. 0 turns it off.", Input(ID("gapAlertHours"), Type("number"), Min("0"), Max("168"), bind("gapAlertHours"))),
					Grid("2",
						Div(
							Field("ntfyURL", "ntfy topic URL", "For example https://ntfy.sh/my-topic. Leave empty to turn off.", Input(ID("ntfyURL"), Type("url"), bind("ntfyURL"))),
							Field("ntfyToken", "ntfy access token", "", Input(ID("ntfyToken"), Type("password"), AutoComplete("new-password"), bind("ntfyToken"))),
							NtfySecretStatus(d.HasNtfyToken),
						),
						Div(
							Field("webhookURL", "Webhook URL", "Receives each event as JSON. Leave empty to turn off.", Input(ID("webhookURL"), Type("url"), bind("webhookURL"))),
							Field("webhookSecret", "Webhook signing secret", "", Input(ID("webhookSecret"), Type("password"), AutoComplete("new-password"), bind("webhookSecret"))),
							WebhookSecretStatus(d.HasWebhookSecret),
						),
					),
					Card(H2(g.Text("Email")),
						P(Class("muted"), g.Text("Send notifications by email through your own SMTP server. Leave the recipient empty to turn email off.")),
						Field("emailTo", "Send to", "", Input(ID("emailTo"), Type("email"), bind("emailTo"))),
						Div(append(comp("fieldrow"),
							Field("smtpHost", "SMTP host", "", Input(ID("smtpHost"), Type("text"), AutoComplete("off"), bind("smtpHost"))),
							Field("smtpPort", "Port", "Usually 587 (StartTLS) or 465 (TLS).", Input(ID("smtpPort"), Type("number"), Min("1"), Max("65535"), bind("smtpPort"))),
						)...),
						Div(append(comp("fieldrow"),
							Field("smtpUsername", "Username", "", Input(ID("smtpUsername"), Type("text"), AutoComplete("off"), bind("smtpUsername"))),
							Field("smtpPassword", "Password", "", Input(ID("smtpPassword"), Type("password"), AutoComplete("new-password"), bind("smtpPassword"))),
						)...),
						SMTPSecretStatus(d.HasSMTPPassword),
						Div(append(comp("fieldrow"),
							Field("smtpSender", "From address", "", Input(ID("smtpSender"), Type("email"), bind("smtpSender"))),
							Field("smtpSenderName", "From name", "", Input(ID("smtpSenderName"), Type("text"), bind("smtpSenderName"))),
						)...),
						Field("smtpTLS", "Require TLS", "Tick for port 465. Otherwise StartTLS is used when the server offers it.", Input(ID("smtpTLS"), Type("checkbox"), bind("smtpTLS"))),
						H3(g.Text("What to email")),
						Field("mailAlerts", "Failure alerts", "Expired session, failed update, missing glucose data, canary failures.", Input(ID("mailAlerts"), Type("checkbox"), bind("mailAlerts"))),
						Field("mailActivity", "Summary after each activity", "One mail per processed activity with its glucose numbers.", Input(ID("mailActivity"), Type("checkbox"), bind("mailActivity"))),
						Field("mailWeekly", "Weekly summary", "Every Monday morning: last week's activities and glucose numbers, or a short note if there were none.", Input(ID("mailWeekly"), Type("checkbox"), bind("mailWeekly"))),
						Field("mailHealth", "Monthly health report", "On the 1st, 08:00: activities annotated or failed, glucose data coverage, alerts of the month. Doubles as a sign that glucava is still running.", Input(ID("mailHealth"), Type("checkbox"), bind("mailHealth"))),
						Field("publicURL", "Public URL of this web UI", "Used for links in emails, for example https://glucava.example.com. Leave empty for no links.", Input(ID("publicURL"), Type("url"), bind("publicURL"))),
					),
				),
				PushCard(d.Push),
			),
			tabSection("data",
				g.If(len(d.ImportFormats) > 0, Card(
					H2(g.Text("Import glucose readings")),
					P(Class("muted"), g.Text("Backfill readings from an export file, e.g. switching from another app or restoring a period the live source no longer serves. This adds to what is already stored; nothing existing is touched.")),
					GlucoseImportStatus(d.ImportOK, d.ImportErr),
					Form(ID("glucose-import-form"), Method("post"), Action("/actions/glucose/import"), g.Attr("enctype", "multipart/form-data"),
						Field("glucoseFormat", "Format", "", Select(Name("format"), Required(), importFormatOptions(d.ImportFormats))),
						Field("glucoseSource", "Label (optional)", "Distinguishes these readings from the live source; defaults to the format name.",
							Input(Type("text"), Name("source"), AutoComplete("off"))),
						Field("glucoseFile", "Export file", "", Input(Type("file"), Name("file"), g.Attr("accept", ".csv,.json,.txt,.zip"), Required())),
						SubmitBtn("primary", "", "Import"),
					),
					Script(Src("/static/glucose-import.js")),
				)),
				Card(g.Attr("data-signals", `{"accountCurrent":"","accountEmail":"","accountNew":"","accountConfirm":""}`),
					H2(g.Text("Account")),
					AccountEmail(d.AccountEmail),
					P(Class("muted"), g.Text("Change the email or the password you sign in to this page with. Both need your current password, and every other browser is signed out. Leave a field empty to keep it.")),
					Field("accountCurrent", "Current password", "", Input(ID("accountCurrent"), Type("password"), AutoComplete("current-password"), bind("accountCurrent"))),
					Field("accountEmail", "New email", "", Input(ID("accountEmail"), Type("email"), AutoComplete("off"), bind("accountEmail"))),
					Field("accountNew", "New password", "At least 12 characters.", Input(ID("accountNew"), Type("password"), AutoComplete("new-password"), bind("accountNew"))),
					Field("accountConfirm", "Repeat the new password", "", Input(ID("accountConfirm"), Type("password"), AutoComplete("new-password"), bind("accountConfirm"))),
					Btn("", "Change account", post("/actions/account")),
				),
				Card(H2(g.Text("Your data")),
					P(Class("muted"), g.Text("Glucose readings, activities and events are stored on this server only. Old readings and events are deleted after the number of days below; 0 keeps them forever.")),
					Field("retentionDays", "Keep readings and events for (days)", "", Input(ID("retentionDays"), Type("number"), Min("0"), Max("3650"), bind("retentionDays"))),
					Div(append(comp("actions"),
						A(append(comp("button"), Href("/export/samples.csv"), g.Attr("download", ""), g.Text("Download readings (CSV)"))...),
						A(append(comp("button"), Href("/export/activities.csv"), g.Attr("download", ""), g.Text("Download activities (CSV)"))...),
						A(append(comp("button"), Href("/export/report.pdf?range=30d"), g.Attr("download", ""), g.Text("Download report, last 30 days (PDF)"))...),
					)...),
					Field("purgeConfirm", "Delete all data", "Removes every reading, activity and event. Settings, credentials and tokens stay. Type DELETE to enable the button.",
						Input(ID("purgeConfirm"), Type("text"), AutoComplete("off"), bind("purgeConfirm"))),
					Btn("danger", "Delete all data", post("/actions/data/purge"), g.Attr("data-attr:disabled", "$purgeConfirm !== 'DELETE'")),
				),
			),
			Div(append(comp("actions"),
				Btn("primary", "Save settings", post("/actions/settings")),
				Btn("", "Send test notification", post("/actions/notify/test")),
			)...),
		),
	)
}

// ------------------------------------------------------------------- tokens

// TokenList is patched after creating or revoking a token.
func TokenList(list []tokens.Info, loc *time.Location) g.Node {
	if len(list) == 0 {
		return Card(ID("token-list"), H2(g.Text("Trigger tokens")), Div(append(comp("empty"), g.Text("No tokens yet."))...))
	}
	rows := make([]g.Node, 0, len(list))
	for _, t := range list {
		used := "never"
		if !t.LastUsed.IsZero() {
			used = t.LastUsed.In(loc).Format("2 Jan 2006, 15:04")
		}
		state := Badge("ok", "Active")
		action := g.Node(revokeButton(t.Name))
		if t.Revoked {
			state, action = Badge("", "Revoked"), Span()
		}
		rows = append(rows, Tr(Td(g.Text(t.Name)), Td(state), Td(g.Text(used)), Td(action)))
	}
	return Card(ID("token-list"), H2(g.Text("Trigger tokens")),
		Div(append(comp("tablewrap"), Table(append(comp("table"),
			THead(Tr(Th(g.Text("Name")), Th(g.Text("State")), Th(g.Text("Last used")), Th())), TBody(g.Group(rows)))...))...))
}

func revokeButton(name string) g.Node {
	return BtnSized("danger", "sm", "Revoke", post("/actions/tokens/revoke/"+url.PathEscape(name)))
}

// CopyRow shows a value with a button that copies it.
func CopyRow(label, value string) g.Node {
	return Div(Class("copyrow"),
		Span(Class("muted"), g.Text(label)),
		Code(g.Text(value)),
		BtnSized("", "sm", "Copy", g.Attr("data-on:click",
			fmt.Sprintf("navigator.clipboard.writeText('%s'); evt.currentTarget.textContent = 'Copied'", jsQuote(value)))),
	)
}

// SecretReveal shows a new token once, with the values a phone app needs,
// plus two QR codes while the token is still on screen: one to download a
// ready-to-import Tasker profile directly (for Android), and one that just
// opens this page on whatever device scans it (for HTTP Shortcuts,
// MacroDroid or Apple Shortcuts, which can't be pre-built as a file the
// same way — the target is this page's own per-platform instructions,
// filled in with the real token below instead of a placeholder).
func SecretReveal(name, token, endpoint, taskerURL, pageURL string) g.Node {
	header := "Bearer " + token
	return Div(ID("token-reveal"), Div(append(comp("secretbox"),
		P(Strong(g.Textf("Token “%s” created. Copy it now; it is not shown again.", name))),
		CopyRow("Token", token),
		CopyRow("URL (POST)", endpoint),
		CopyRow("Header", "Authorization: "+header),
		CopyRow("Test", fmt.Sprintf("curl -i -X POST -H 'Authorization: %s' %s", header, endpoint)),
		P(Class("muted"), g.Text("A working call answers 202, and “Last used” in the list below changes.")),
		Grid("2",
			Div(
				H3(g.Text("Android (Tasker)")),
				P(Class("muted"), g.Text("A ready-to-import profile: Strava notification → check now, unfiltered (see below for why).")),
				A(append(comp("button"), Href(taskerURL), g.Attr("download", ""), g.Text("Download Tasker profile"))...),
				g.If(qrDataURI(taskerURL) != "", Img(Alt("QR code to download the Tasker profile"), Src(qrDataURI(taskerURL)), g.Attr("style", "width:160px;height:160px;margin-top:.5rem"))),
			),
			Div(
				H3(g.Text("Other platforms")),
				P(Class("muted"), g.Text("Scan to open this page on the phone you'll set the trigger up on — the instructions below fill in this real token instead of a placeholder.")),
				g.If(qrDataURI(pageURL) != "", Img(Alt("QR code to open the Triggers page"), Src(qrDataURI(pageURL)), g.Attr("style", "width:160px;height:160px;margin-top:.5rem"))),
			),
		),
	)...))
}

// TokensPage manages the push-trigger tokens and explains how to use them.
func TokensPage(pd PageData, list []tokens.Info, baseURL string, loc *time.Location) g.Node {
	endpoint := strings.TrimRight(baseURL, "/") + "/api/trigger"
	return Page(pd,
		PageHead("Triggers", "Tell glucava about a new activity right away instead of waiting for the next poll."),
		Card(g.Attr("data-signals", `{"tokenName":""}`),
			H2(g.Text("New token")),
			Div(append(comp("fieldrow"),
				Field("tokenName", "Name", "For example the device it is for.", Input(ID("tokenName"), Type("text"), Placeholder("phone"), g.Attr("data-bind", "tokenName"))),
			)...),
			Btn("primary", "Create token", post("/actions/tokens/create")),
			Div(ID("token-reveal")),
		),
		TokenList(list, loc),
		Card(H2(g.Text("How to call it")),
			P(g.Text("Send a POST request with the token. The call only asks glucava to check Strava now; it carries no other data. “Last used” in the list above shows whether your phone's call arrived.")),
			Pre(g.Textf("curl -i -X POST %s \\\n  -H \"Authorization: Bearer <token>\"", endpoint)),
			H2(g.Text("Android (Tasker)")),
			Ol(
				Li(g.Text("Task: Net → HTTP Request. Method POST, URL and header as above, empty body, timeout 30. Run it once by hand and check “Last used”.")),
				Li(g.Text("Profile: Event → UI → Notification, owner application Strava, no filter yet. Link it to the task.")),
				Li(g.Text("After Strava's next “activity saved” notification, look at Tasker's run log. If the profile also fires for kudos or comments, add a title or text filter with the wording you see there.")),
				Li(g.Text("Tasker needs notification access and unrestricted battery use.")),
			),
			P(Class("muted"), g.Text("HTTP Shortcuts and MacroDroid work the same way: trigger on a Strava notification, then send the request.")),
			H2(g.Text("iPhone (Shortcuts)")),
			P(g.Text("Personal automation → Workout → Ends (or App → Strava → Is Closed), set to Run Immediately. Actions: Wait 30 seconds, then Get Contents of URL with method POST and the header above. iOS cannot react to another app's notification. This has not been tested yet.")),
			P(Class("muted"), g.Text("Without any trigger, polling still picks up new activities on the interval from Settings.")),
		),
	)
}

// ------------------------------------------------------------------- events

// EventsPage lists notifications.
func EventsPage(pd PageData, evs []store.EventRow, loc *time.Location, now time.Time) g.Node {
	body := g.Node(Div(append(comp("empty"), g.Text("Nothing has gone wrong. Notifications appear here when something fails."))...))
	if len(evs) > 0 {
		rows := make([]g.Node, 0, len(evs))
		for _, e := range evs {
			delivered := Badge("", "Waiting")
			if e.Notified {
				delivered = Badge("ok", "Sent")
			}
			rows = append(rows, Tr(
				Td(g.Text(fmtWhen(e.Created, loc, now))), Td(SeverityBadge(e.Severity)),
				Td(g.Text(strings.ReplaceAll(e.Type, "_", " "))),
				Td(g.Text(e.Message), g.If(e.StravaID != "", A(Href("/activity/"+e.StravaID), g.Text(" → activity")))),
				Td(Class("hide-sm"), delivered),
			))
		}
		body = Div(append(comp("tablewrap"), Table(append(comp("table"),
			THead(Tr(Th(g.Text("When")), Th(g.Text("Level")), Th(g.Text("Type")), Th(g.Text("Message")), Th(Class("hide-sm"), g.Text("Delivery")))),
			TBody(g.Group(rows)))...))...)
	}
	return Page(pd,
		PageHead("Notifications", "Everything glucava told you about, or tried to."),
		Card(body),
	)
}

// logLevelBadge maps an slog.Level to the existing badge variants, so /logs
// reuses the same color coding as severity elsewhere (Notifications page).
func logLevelBadge(level slog.Level) g.Node {
	switch {
	case level >= slog.LevelError:
		return Badge("error", "Error")
	case level >= slog.LevelWarn:
		return Badge("warning", "Warn")
	case level >= slog.LevelInfo:
		return Badge("info", "Info")
	default:
		return Badge("", "Debug")
	}
}

// LogRows renders the log table body, newest first. It is also the fragment
// streamLogs patches in place on every bus wake, so it must be re-renderable
// standalone (no page chrome), the same shape as LiveDash for the dashboard.
func LogRows(entries []logging.Entry, loc *time.Location) g.Node {
	if len(entries) == 0 {
		return Div(append(comp("empty"), ID("log-rows"), g.Text("No log entries yet."))...)
	}
	rows := make([]g.Node, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, Tr(
			Td(g.Text(e.Time.In(loc).Format("15:04:05"))),
			Td(logLevelBadge(e.Level)),
			Td(g.Text(e.Message)),
			Td(Class("hide-sm"), g.Text(e.Attrs)),
		))
	}
	return Div(append(comp("tablewrap"), ID("log-rows"), Table(append(comp("table"),
		THead(Tr(Th(g.Text("Time")), Th(g.Text("Level")), Th(g.Text("Message")), Th(Class("hide-sm"), g.Text("Details")))),
		TBody(g.Group(rows)))...))...)
}

// LogsPage is an admin-only diagnostic view of recent structured log entries,
// live-updated via /stream/logs the same way the dashboard streams over
// /stream/live. No Settings toggle, same as the Notifications/Events page.
func LogsPage(pd PageData, entries []logging.Entry, loc *time.Location) g.Node {
	return Page(pd,
		PageHead("Logs", "Recent structured log entries from this process, newest first."),
		Card(
			Div(g.Attr("data-init", "@get('/stream/logs')"), LogRows(entries, loc)),
		),
	)
}

// chartPreviewExpr builds the preview image address from the form signals, so
// the image reloads whenever an option changes.
const chartPreviewExpr = "'/chart/latest.png?theme=' + $chartTheme + '&size=' + $chartSize + '&line=' + $chartLine" +
	" + '&band=' + $chartBand + '&activity=' + $chartActivity + '&dots=' + $chartDots + '&hr=' + $chartHR + '&elevation=' + $chartElevation + '&pre=' + $chartPre" +
	" + '&avgline=' + $chartAvgLine + '&rangelines=' + $chartRangeLines + '&minmax=' + $chartMinMax + '&hidestats=' + $chartHideStats" +
	" + '&unit=' + encodeURIComponent($unit) + '&low=' + $rangeLow + '&high=' + $rangeHigh + '&panelOrder=' + encodeURIComponent($chartPanelOrder)"

// panelMoveExpr swaps panel (one of chartimg's panel names) with its
// neighbour in $chartPanelOrder, one step toward the front (dir=-1) or back
// (dir=+1). The signal is a plain comma-separated string (see
// store.Config.ChartPanelOrder), so this is ordinary array juggling inside
// the already CSP-permitted evaluated data-on expression.
func panelMoveExpr(panel string, dir int) string {
	return fmt.Sprintf(
		"var a=$chartPanelOrder.split(','); var i=a.indexOf(%q); var j=i+(%d); "+
			"if(i>=0 && j>=0 && j<a.length){var t=a[i]; a[i]=a[j]; a[j]=t; $chartPanelOrder=a.join(',')}",
		panel, dir)
}

// chartPanelRow is one row of the chart panel list: a checkbox toggling the
// panel on/off (sig, an existing boolean signal like "chartBand"), plus
// up/down buttons that reorder it within $chartPanelOrder. id is the
// chartimg panel name ("activity", "band", "dots" or "hr"). The row's CSS
// "order" tracks its position in $chartPanelOrder, so the list itself
// visibly reorders as the buttons are clicked, inside a flex-column
// container (see the "Panels" list in the chart card) — without it, the
// buttons changed the draw order but nothing on screen showed it happened.
func chartPanelRow(sig, id, label string) g.Node {
	return Div(g.Attr("style", "display:flex;align-items:center;gap:.5rem;margin:0 0 .5rem"),
		g.Attr("data-style:order", fmt.Sprintf("$chartPanelOrder.split(',').indexOf(%q)", id)),
		Input(Type("checkbox"), g.Attr("data-bind", sig)),
		Span(g.Text(label)),
		Btn("", "↑ Earlier", g.Attr("data-on:click", panelMoveExpr(id, -1))),
		Btn("", "↓ Later", g.Attr("data-on:click", panelMoveExpr(id, 1))),
	)
}

// settingsTabIDs lists the settings page's tabs, in display order. Every
// field on the page still lives under one Div per tab (data-signals covers
// the whole page regardless of which is shown), so Save always saves
// everything: switching tabs never loses or hides a change.
var settingsTabIDs = []struct{ id, label string }{
	{"glucose", "Glucose and timing"},
	{"customize", "Description and chart"},
	{"notify", "Notifications"},
	{"data", "Data and account"},
}

// settingsTabs renders the tab bar. Each button sets $settingsTab and is
// shown pressed (the primary variant) exactly when it is the active one.
func settingsTabs() g.Node {
	btns := make([]g.Node, len(settingsTabIDs))
	for i, t := range settingsTabIDs {
		active := fmt.Sprintf("$settingsTab === %q", t.id)
		btns[i] = Btn("", t.label,
			g.Attr("data-on:click", fmt.Sprintf("$settingsTab = %q", t.id)),
			g.Attr("data-attr:data-variant", active+" ? 'primary' : ''"),
			g.Attr("data-attr:aria-current", active+" ? 'true' : 'false'"),
		)
	}
	return Div(append(comp("actions"), btns...)...)
}

// tabSection wraps children so they only show while $settingsTab equals id.
// Hidden, not removed: their bound signals (and so their values) stay live.
func tabSection(id string, children ...g.Node) g.Node {
	return Div(append([]g.Node{g.Attr("data-show", fmt.Sprintf("$settingsTab === %q", id))}, children...)...)
}

// defaultStr returns s, or fallback if s is empty.
func defaultStr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// descPreviewExpr asks the server to re-render the description preview
// whenever the template text changes (debounced on the textarea's own
// data-on:input, see SettingsPage).
const descPreviewExpr = "@get('/preview/description.txt?tmpl=' + encodeURIComponent($descTemplate) + " +
	"'&unit=' + encodeURIComponent($unit) + '&low=' + $rangeLow + '&high=' + $rangeHigh)"

// descPresetChangeExpr fills the template textarea with the chosen preset's
// text (evalTemplateByID, generated below) and re-runs the preview, unless
// "custom" is picked, which leaves whatever is already there alone.
var descPresetChangeExpr = buildDescPresetChangeExpr()

func buildDescPresetChangeExpr() string {
	var b strings.Builder
	for i, p := range render.Presets {
		if i > 0 {
			b.WriteString(" else ")
		}
		fmt.Fprintf(&b, "if ($descPreset === %q) { $descTemplate = %q; }", p.ID, p.Template)
	}
	b.WriteString("; " + descPreviewExpr)
	return b.String()
}

// presetOptions lists the built-in description templates as <option>s.
func presetOptions() []g.Node {
	opts := make([]g.Node, len(render.Presets))
	for i, p := range render.Presets {
		opts[i] = Option(Value(p.ID), g.Text(p.Name+" — "+p.Description))
	}
	return opts
}

// descPresetIDFor reports which preset (if any) tmpl matches exactly, so the
// dropdown reflects a saved custom template correctly on page load: "custom"
// for anything else, including empty (which renders as DefaultTemplate but
// was never explicitly picked as the "default" preset).
func descPresetIDFor(tmpl string) string {
	for _, p := range render.Presets {
		if p.Template == tmpl {
			return p.ID
		}
	}
	return "custom"
}
