package web

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	g "maragu.dev/gomponents"
	. "maragu.dev/gomponents/html"

	"github.com/MrCodeEU/glucava/internal/jobs"
	"github.com/MrCodeEU/glucava/internal/render"
	"github.com/MrCodeEU/glucava/internal/stats"
	"github.com/MrCodeEU/glucava/internal/store"
	"github.com/MrCodeEU/glucava/internal/tokens"
)

// jsQuote escapes s for use inside a single-quoted JavaScript string in a Datastar expression.
var jsQuote = strings.NewReplacer(`\`, `\\`, `'`, `\'`, "\n", `\n`, "\r", `\r`, "<", `\x3c`).Replace

func post(url string) g.Node {
	return g.Attr("data-on:click", fmt.Sprintf("@post('%s')", jsQuote(url)))
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
	glucoseVal, glucoseSub := "-", "no recent reading"
	if d.Latest != nil {
		glucoseVal = render.Value(d.Latest.Value, d.Unit)
		if age := d.Now.Sub(d.Latest.Time); age >= 0 {
			glucoseSub = fmt.Sprintf("%s · %s ago", d.Latest.Time.In(d.Loc).Format("15:04"), fmtDuration(age))
		}
	}

	return Div(ID("live"),
		Grid("",
			Tile("Current glucose", glucoseVal, glucoseSub),
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
		return Div(append(comp("empty"),
			P(g.Text("No activities yet.")),
			P(Class("muted"), g.Text("New Strava activities appear here once the session is set up and polling finds them.")))...)
	}
	rows := make([]g.Node, 0, len(d.Acts))
	for _, a := range d.Acts {
		rows = append(rows, activityRow(a, d))
	}
	return Div(append(comp("tablewrap"),
		Table(append(comp("table"),
			THead(Tr(
				Th(g.Text("When")), Th(g.Text("Activity")), Th(Class("hide-sm"), g.Text("Duration")),
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
		Td(A(Href(href), g.Text(name)), g.If(a.Sport != "", Span(Class("muted"), g.Text(" · "+a.Sport)))),
		Td(Class("hide-sm"), g.Text(fmtDuration(a.Duration))),
		Td(tirCell),
		Td(Class("hide-sm num"), g.Text(minmax)),
		Td(Class("hide-sm num"), g.Text(avg)),
		Td(StatusBadge(a.Status)),
	)
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
	Events  []store.EventRow
	Loc     *time.Location
	Now     time.Time
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
			A(append(comp("button"), Href("/"), g.Text("Back"))...),
			A(append(comp("button"), Href("https://www.strava.com/activities/"+a.StravaID),
				Target("_blank"), Rel("noopener noreferrer"), g.Text("View on Strava ↗"))...),
			g.If(a.Original != nil, Btn("", "Restore original", post("/actions/restore/"+a.StravaID))),
			g.If(d.Cfg.ChartImage, Btn("", "Attach chart again", post("/actions/chart/"+a.StravaID))),
			IndicatorBtn("primary", "Reprocess", "/actions/reprocess/"+a.StravaID, "reprocessing")),
		Div(g.Attr("data-init", "@get('"+jsQuote("/stream/activity/"+a.StravaID)+"')"), ActivityBody(d)),
	)
}

// ActivityBody is the part of the activity page that updates live.
func ActivityBody(d ActivityData) g.Node {
	a := d.Act
	unit := render.Unit(d.Cfg.Unit)

	var tiles g.Node
	if s := a.Summary; s != nil {
		tiles = Grid("",
			Tile("Time in range", fmt.Sprintf("%.0f%%", s.TIR), fmt.Sprintf("%.0f%% below · %.0f%% above", s.Below, s.Above)),
			Tile("Average", render.Value(s.Avg, unit), string(unit)),
			Tile("Min / Max", render.Value(s.Min, unit)+" / "+render.Value(s.Max, unit), string(unit)),
			Tile("Readings", fmt.Sprint(s.Count), fmt.Sprintf("start %s → end %s", render.Value(s.Start, unit), render.Value(s.End, unit))),
		)
	}

	return Div(ID("activity-body"),
		g.If(a.Status == jobs.StatusFailed && a.Error != "",
			Notice("error", Strong(g.Text("This activity failed. ")), g.Text(a.Error))),
		g.If(a.Status == jobs.StatusPending, Notice("", Strong(g.Text("Waiting in the queue. ")), g.Text("Jobs run one at a time; this page updates by itself."))),
		g.If(a.Status == jobs.StatusProcessing && a.Error == "", Notice("", Strong(g.Text("Working on it. ")), g.Text("Starting the browser and writing to Strava usually takes under a minute; this page updates by itself."))),
		g.If(a.Status == jobs.StatusProcessing && a.Error != "", Notice("warning", Strong(g.Text("Not finished yet. ")), g.Text(a.Error))),
		g.If(tiles != nil, tiles),
		g.If(d.Cfg.ChartImage, Card(H2(g.Text("Chart photo for Strava")),
			P(Class("muted"), g.Text("What glucava attaches to the activity, with your current chart settings.")),
			Img(Alt("Glucose chart as attached to Strava"), Src("/chart/"+a.StravaID+".png"),
				g.Attr("style", "max-width:100%;height:auto;border-radius:8px")),
		)),
		Card(H2(g.Text("Glucose")), GlucoseChart(ChartData{
			Samples: d.Samples, Unit: unit, Loc: d.Loc, Start: a.Start, End: a.End(),
			Range: stats.Range{Low: d.Cfg.RangeLow, High: d.Cfg.RangeHigh},
		})),
		Grid("2",
			Card(H2(g.Text("Strava description block")),
				g.If(d.Block != "", Pre(g.Text(d.Block))),
				g.If(d.Block == "", P(Class("muted"), g.Text("Nothing to show yet: no glucose readings are stored for this activity."))),
				P(Class("muted"), g.Text("This block is added to the description on Strava. Your own text there is kept."))),
			Card(H2(g.Text("Processing")),
				Dl(append(comp("dl"),
					Dt(g.Text("Status")), Dd(StatusBadge(a.Status)),
					Dt(g.Text("Attempts")), Dd(g.Textf("%d", a.Attempts)),
					g.If(d.Cfg.ChartImage, Dt(g.Text("Chart photo"))),
					g.If(d.Cfg.ChartImage, Dd(g.Text(map[bool]string{true: "sent once", false: "not sent"}[a.ChartUploaded]))),
					Dt(g.Text("Strava ID")), Dd(Code(g.Text(a.StravaID))),
				)...),
				eventList(d.Events, d.Loc, d.Now),
			),
		),
	)
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
	Cfg                                                                store.Config
	HasDexcomPassword, HasNtfyToken, HasWebhookSecret, HasSMTPPassword bool
	ImportFormats                                                      []string // importers.Names(); empty hides the import card
	ImportOK, ImportErr                                                string   // one-shot flash after /actions/glucose/import redirects back
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
			IndicatorBtn("", "Test connection", "/actions/dexcom/test", "dxtest"))...)),
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
	sig, _ := json.Marshal(map[string]any{
		"unit": c.Unit, "rangeLow": c.RangeLow, "rangeHigh": c.RangeHigh, "preMin": c.PreMin, "postMin": c.PostMin,
		"pollMin": c.PollMin, "dexcomRegion": c.DexcomRegion, "dexcomUsername": c.DexcomUsername,
		"dexcomPassword": "", "ntfyURL": c.NtfyURL, "ntfyToken": "", "webhookURL": c.WebhookURL, "webhookSecret": "",
		"emailTo":  c.EmailTo,
		"smtpHost": c.SMTPHost, "smtpPort": c.SMTPPort, "smtpUsername": c.SMTPUsername, "smtpPassword": "",
		"smtpTLS": c.SMTPTLS, "smtpSender": c.SMTPSender, "smtpSenderName": c.SMTPSenderName, "retentionDays": c.RetentionDays, "purgeConfirm": "",
		"publicURL": c.PublicURL, "mailAlerts": c.MailAlerts, "mailActivity": c.MailActivity, "mailWeekly": c.MailWeekly, "mailHealth": c.MailHealth, "gapAlertHours": c.GapAlertHours, "chartImage": c.ChartImage,
		"chartTheme": c.ChartTheme, "chartSize": c.ChartSize, "chartBand": c.ChartBand, "chartActivity": c.ChartActivity, "chartDots": c.ChartDots, "chartLine": c.ChartLine,
	})
	bind := func(name string) g.Node { return g.Attr("data-bind", name) }

	return Page(pd,
		PageHead("Settings", "Changes apply to the next poll; no restart needed."),
		Div(g.Attr("data-signals", string(sig)),
			Grid("2",
				Card(H2(g.Text("Glucose and timing")),
					Field("unit", "Unit", "", Select(ID("unit"), bind("unit"),
						Option(Value("mg/dL"), g.Text("mg/dL")), Option(Value("mmol/L"), g.Text("mmol/L")))),
					Div(append(comp("fieldrow"),
						Field("rangeLow", "Target low (mg/dL)", "", Input(ID("rangeLow"), Type("number"), Min("40"), Max("200"), bind("rangeLow"))),
						Field("rangeHigh", "Target high (mg/dL)", "", Input(ID("rangeHigh"), Type("number"), Min("80"), Max("400"), bind("rangeHigh"))),
					)...),
					Div(append(comp("fieldrow"),
						Field("preMin", "Minutes before start", "", Input(ID("preMin"), Type("number"), Min("0"), Max("240"), bind("preMin"))),
						Field("postMin", "Minutes after end", "Also how long to wait after an activity before processing it.", Input(ID("postMin"), Type("number"), Min("0"), Max("240"), bind("postMin"))),
					)...),
					Field("pollMin", "Check Strava every (minutes)", "", Input(ID("pollMin"), Type("number"), Min("1"), Max("1440"), bind("pollMin"))),
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
			Card(H2(g.Text("Chart photo")),
				P(Class("muted"), g.Text("Optionally attach the glucose chart to the Strava activity as a photo. The preview below updates as you change the options, using your latest activity (or sample data), before anything is saved.")),
				Field("chartImage", "Attach the chart to each activity", "Once per activity. Experimental: glucava cannot remove photos again, and a first photo can replace the map as the activity's cover.", Input(ID("chartImage"), Type("checkbox"), bind("chartImage"))),
				Grid("2",
					Div(
						Field("chartTheme", "Theme", "", Select(ID("chartTheme"), bind("chartTheme"),
							Option(Value("light"), g.Text("Light")), Option(Value("dark"), g.Text("Dark")))),
						Field("chartSize", "Size", "Large is 1200 px wide, sharper on big screens.", Select(ID("chartSize"), bind("chartSize"),
							Option(Value("standard"), g.Text("Standard (600 px)")), Option(Value("large"), g.Text("Large (1200 px)")))),
						Field("chartLine", "Line thickness (1 to 4)", "", Input(ID("chartLine"), Type("number"), Min("1"), Max("4"), bind("chartLine"))),
						Field("chartBand", "Shade the target range", "", Input(ID("chartBand"), Type("checkbox"), bind("chartBand"))),
						Field("chartActivity", "Shade the activity", "", Input(ID("chartActivity"), Type("checkbox"), bind("chartActivity"))),
						Field("chartDots", "Mark out-of-range readings", "", Input(ID("chartDots"), Type("checkbox"), bind("chartDots"))),
					),
					Div(
						Img(ID("chartPreview"), Alt("Preview of the chart photo"), g.Attr("style", "max-width:100%;height:auto;border:1px solid var(--border, #ccc);border-radius:8px"),
							g.Attr("data-attr:src", chartPreviewExpr)),
					),
				),
			),
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
			Card(H2(g.Text("Your data")),
				P(Class("muted"), g.Text("Glucose readings, activities and events are stored on this server only. Old readings and events are deleted after the number of days below; 0 keeps them forever.")),
				Field("retentionDays", "Keep readings and events for (days)", "", Input(ID("retentionDays"), Type("number"), Min("0"), Max("3650"), bind("retentionDays"))),
				Div(append(comp("actions"),
					A(append(comp("button"), Href("/export/samples.csv"), g.Attr("download", ""), g.Text("Download readings (CSV)"))...),
					A(append(comp("button"), Href("/export/activities.csv"), g.Attr("download", ""), g.Text("Download activities (CSV)"))...),
				)...),
				Field("purgeConfirm", "Delete all data", "Removes every reading, activity and event. Settings, credentials and tokens stay. Type DELETE to enable the button.",
					Input(ID("purgeConfirm"), Type("text"), AutoComplete("off"), bind("purgeConfirm"))),
				Btn("danger", "Delete all data", post("/actions/data/purge"), g.Attr("data-attr:disabled", "$purgeConfirm !== 'DELETE'")),
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

// SecretReveal shows a new token once.
func SecretReveal(name, token string) g.Node {
	return Div(ID("token-reveal"), Div(append(comp("secretbox"),
		P(Strong(g.Textf("Token “%s” created. Copy it now; it is not shown again.", name))),
		Code(g.Text(token)),
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
			P(g.Text("Send a POST request with the token. The call only asks glucava to check Strava now; it carries no other data.")),
			Pre(g.Textf("curl -X POST %s \\\n  -H \"Authorization: Bearer <token>\"", endpoint)),
			H2(g.Text("Android (Tasker)")),
			P(g.Text("Create a profile with the event “Notification” for the Strava app, and let it run an HTTP Request action: method POST, the URL above, header Authorization: Bearer <token>. HTTP Shortcuts and MacroDroid work the same way.")),
			H2(g.Text("iPhone (Shortcuts)")),
			P(g.Text("Build a shortcut with the action “Get Contents of URL” (method POST, the header above) and start it from a Personal Automation. iOS cannot start automations from another app's notification, so use a trigger such as “Workout ends” or “Time of day”. This has not been tested yet.")),
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

// chartPreviewExpr builds the preview image address from the form signals, so
// the image reloads whenever an option changes.
const chartPreviewExpr = "'/chart/latest.png?theme=' + $chartTheme + '&size=' + $chartSize + '&line=' + $chartLine" +
	" + '&band=' + $chartBand + '&activity=' + $chartActivity + '&dots=' + $chartDots" +
	" + '&unit=' + encodeURIComponent($unit) + '&low=' + $rangeLow + '&high=' + $rangeHigh"
