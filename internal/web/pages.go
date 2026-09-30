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
	"github.com/MrCodeEU/glucava/internal/i18n"
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

func (s SessionInfo) headline(tr *i18n.Translator) (value, sub string) {
	switch {
	case !s.Configured:
		return tr.T("strava.session.none"), tr.T("strava.session.none.sub")
	case !s.CheckedAt.IsZero() && !s.CheckOK:
		return tr.T("strava.session.expired"), tr.T("strava.session.expired.sub")
	case !s.CheckedAt.IsZero():
		return tr.T("strava.session.valid"), tr.T("strava.session.valid.sub", "when", tr.Date(s.CheckedAt, false)+" "+tr.Time(s.CheckedAt))
	}
	return tr.T("strava.session.stored"), tr.Tn("strava.session.stored.sub", len(s.Cookies))
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

// LiveDash is LiveDashT in English, for callers without a request.
func LiveDash(d DashData) g.Node { return LiveDashT(i18n.English(), d) }

// LiveDashT is the part of the dashboard that updates without a reload.
func LiveDashT(tr *i18n.Translator, d DashData) g.Node {
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
	sv, ss := d.Session.headline(tr)
	attention := tr.T("dash.tile.failed.ok")
	if failed > 0 {
		attention = tr.T("dash.tile.failed.fix")
	}

	return Div(ID("live"),
		nowCard(d),
		Grid("",
			Tile(tr.T("dash.tile.annotated.label"), fmt.Sprint(done), tr.T("dash.tile.annotated.sub")),
			Tile(tr.T("dash.tile.tir.label"), tir, tr.T("dash.tile.tir.sub")),
			Tile(tr.T("dash.tile.failed.label"), fmt.Sprint(failed), attention),
			Tile(tr.T("dash.tile.session.label"), sv, ss),
		),
		Card(
			H2(g.Text(tr.T("dash.recent"))),
			activityTableT(tr, d),
		),
	)
}

// activityTable is activityTableT in English, for callers without a request.
func activityTable(d DashData) g.Node { return activityTableT(i18n.English(), d) }

func activityTableT(tr *i18n.Translator, d DashData) g.Node {
	if len(d.Acts) == 0 {
		hint := tr.T("dash.empty.hint")
		var action []g.Node
		if !d.Session.Configured {
			hint = tr.T("dash.empty.nosession")
			action = append(action, A(append(comp("button"), Href("/strava"), g.Text(tr.T("dash.empty.setup")))...))
		}
		return EmptyState("activity", tr.T("dash.empty.title"), hint, action...)
	}
	rows := make([]g.Node, 0, len(d.Acts))
	for _, a := range d.Acts {
		rows = append(rows, activityRow(tr, a, d))
	}
	return Div(append(comp("tablewrap"),
		Table(append(comp("table"),
			THead(Tr(
				Th(g.Text(tr.T("dash.col.when"))), Th(g.Text(tr.T("dash.col.activity"))), Th(Class("hide-sm"), g.Text(tr.T("dash.col.duration"))),
				Th(Class("hide-sm"), g.Text(tr.T("dash.col.distance"))),
				Th(g.Text(tr.T("dash.col.tir"))), Th(Class("hide-sm num"), g.Text(tr.T("dash.col.minmax"))),
				Th(Class("hide-sm num"), g.Text(tr.T("dash.col.avg"))), Th(g.Text(tr.T("dash.col.status"))),
			)),
			TBody(g.Group(rows)),
		)...),
	)...)
}

func activityRow(tr *i18n.Translator, a jobs.Activity, d DashData) g.Node {
	href := "/activity/" + a.StravaID
	name := a.Name
	if name == "" {
		name = tr.T("dash.activity.untitled", "id", a.StravaID)
	}
	tirCell, minmax, avg := g.Node(Span(Class("muted"), g.Text("-"))), "-", "-"
	if a.Summary != nil {
		s := a.Summary
		tirCell = tirBar(tr, *s)
		minmax = render.Value(s.Min, d.Unit) + " / " + render.Value(s.Max, d.Unit)
		avg = render.Value(s.Avg, d.Unit)
	}
	return Tr(g.Attr("data-href", href), g.Attr("data-on:click", fmt.Sprintf("window.location='%s'", jsQuote(href))),
		Td(g.Text(fmtWhenT(tr, a.Start, d.Loc, d.Now))),
		Td(Div(Class("flex items-start gap-2"),
			Span(Class("mt-0.5 text-ink-2"), g.Attr("title", orDash(a.Sport)), icon(sportIcon(a.Sport), "size-4")),
			Div(A(Href(href), g.Text(name)), g.If(a.Sport != "", Span(Class("muted"), g.Text(" · "+a.Sport)))))),
		Td(Class("hide-sm"), g.Text(fmtDuration(a.Duration))),
		Td(Class("hide-sm"), distanceCell(tr, a)),
		Td(tirCell),
		Td(Class("hide-sm num"), g.Text(minmax)),
		Td(Class("hide-sm num"), g.Text(avg)),
		Td(StatusBadgeT(tr, a.Status)),
	)
}

// distanceCell is "12.0 km · 5:30 /km", or a dash when the activity has no distance.
func distanceCell(tr *i18n.Translator, a jobs.Activity) g.Node {
	if a.Distance <= 0 {
		return Span(Class("muted"), g.Text("-"))
	}
	txt := tr.T("dash.distance", "km", tr.Num(a.Distance/1000, 1))
	if pace := render.FormatPace(a.Sport, a.Distance, a.Duration); pace != "" {
		txt += " · " + pace
	}
	return g.Text(txt)
}

func tirBar(tr *i18n.Translator, s stats.Summary) g.Node {
	w := func(v float64) g.Node { return Span(g.Attr("style", fmt.Sprintf("width:%.1f%%", v))) }
	return Div(append(comp("tircell"),
		B(g.Textf("%.0f%%", s.TIR)),
		Div(append(comp("tirbar"), g.Attr("role", "img"),
			g.Attr("aria-label", tr.T("dash.tir.aria", "below", tr.Num(s.Below, 0), "in", tr.Num(s.TIR, 0), "above", tr.Num(s.Above, 0))),
			w(s.Below), w(s.TIR), w(s.Above))...),
	)...)
}

// DashboardPage is the home page.
func DashboardPage(pd PageData, d DashData) g.Node {
	tr := pd.translator()
	return Page(pd,
		PageHead(tr.T("dash.title"), tr.T("dash.sub"),
			IndicatorBtn("primary", tr.T("dash.check"), "/actions/poll", "polling")),
		Div(g.Attr("data-init", "@get('/stream/live')"), LiveDashT(tr, d)),
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
	tr := pd.translator()
	a := d.Act
	title := a.Name
	if title == "" {
		title = tr.T("dash.activity.untitled", "id", a.StravaID)
	}

	return Page(pd,
		PageHead(title, fmt.Sprintf("%s · %s · %s", fmtWhenT(tr, a.Start, d.Loc, d.Now), fmtDuration(a.Duration), orDash(a.Sport)),
			g.Group(activityNav(d)),
			A(append(comp("button"), Href("/"), g.Text(tr.T("activity.head.back")))...),
			A(append(comp("button"), Href("https://www.strava.com/activities/"+a.StravaID),
				Target("_blank"), Rel("noopener noreferrer"), g.Text(tr.T("activity.head.strava")))...),
			g.If(a.Original != nil, Btn("", tr.T("activity.head.restore"), post("/actions/restore/"+a.StravaID))),
			g.If(d.Cfg.ChartImage, Btn("", tr.T("activity.head.chart"), post("/actions/chart/"+a.StravaID))),
			IndicatorBtn("primary", tr.T("activity.head.reprocess"), "/actions/reprocess/"+a.StravaID, "reprocessing"),
			ConfirmDialogT(tr, "delete-dialog", "danger", tr.T("activity.head.delete"), tr.T("activity.head.delete.title"),
				tr.T("activity.head.delete.body"),
				postThenGo("/actions/delete/"+a.StravaID, "/"))),
		Div(g.Attr("data-init", "@get('"+jsQuote("/stream/activity/"+a.StravaID)+"')"), ActivityBody(d)),
	)
}

// processingText is the "Working on it" body: the live step name when it is
// known, or the old generic wording otherwise (progress tracking is off, or
// the step just hasn't arrived yet).
func processingText(step string) string { return processingTextT(i18n.English(), step) }

// processingTextT is processingText in the request's language. The step name
// itself comes from the pipeline and is not translated.
func processingTextT(tr *i18n.Translator, step string) string {
	if step == "" {
		return tr.T("activity.processing.generic")
	}
	return tr.T("activity.processing.step", "step", step)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func eventList(evs []store.EventRow, loc *time.Location, now time.Time) g.Node {
	return eventListT(i18n.English(), evs, loc, now)
}

func eventListT(tr *i18n.Translator, evs []store.EventRow, loc *time.Location, now time.Time) g.Node {
	if len(evs) == 0 {
		return P(Class("muted"), g.Text(tr.T("activity.events.none")))
	}
	items := make([]g.Node, 0, len(evs))
	for _, e := range evs {
		items = append(items, P(SeverityBadgeT(tr, e.Severity), g.Text(" "+fmtWhenT(tr, e.Created, loc, now)+" · "+e.Message)))
	}
	return g.Group(items)
}

// ------------------------------------------------------------------- strava

// StravaStatusCard is StravaStatusCardT in English, for callers without a request.
func StravaStatusCard(s SessionInfo) g.Node { return StravaStatusCardT(i18n.English(), s) }

// StravaStatusCardT is patched after imports and tests.
func StravaStatusCardT(tr *i18n.Translator, s SessionInfo) g.Node {
	value, sub := s.headline(tr)
	list := g.Node(P(Class("muted"), g.Text(tr.T("strava.cookies.none"))))
	if len(s.Cookies) > 0 {
		rows := make([]g.Node, 0, len(s.Cookies))
		for _, c := range s.Cookies {
			exp := tr.T("strava.cookies.browsersession")
			if !c.Expires.IsZero() {
				exp = tr.Date(c.Expires, true)
			}
			rows = append(rows, Tr(Td(Code(g.Text(c.Name))), Td(g.Text(exp))))
		}
		list = Div(append(comp("tablewrap"), Table(append(comp("table"),
			THead(Tr(Th(g.Text(tr.T("strava.cookies.col.name"))), Th(g.Text(tr.T("strava.cookies.col.expires"))))), TBody(g.Group(rows)))...))...)
	}
	return Card(ID("strava-status"),
		H2(g.Text(tr.T("strava.status.title"))),
		P(Strong(g.Text(value+".")), g.Text(" "+sub+".")),
		g.If(s.Configured && !s.HasSession, Notice("warning", g.Text(tr.T("strava.missing")))),
		g.If(s.CheckErr != "", Notice("error", g.Text(s.CheckErr))),
		list,
		Div(append(comp("actions"), IndicatorBtn("", tr.T("strava.test"), "/actions/strava/test", "testing"))...),
	)
}

// StravaPage is the cookie import page.
func StravaPage(pd PageData, s SessionInfo) g.Node {
	tr := pd.translator()
	return Page(pd,
		PageHead(tr.T("strava.title"), tr.T("strava.sub")),
		StravaStatusCardT(tr, s),
		Card(g.Attr("data-signals", `{"loginEmail":"","loginPassword":""}`),
			H2(g.Text(tr.T("strava.login.title"))),
			Notice("warning", g.Text(tr.T("strava.login.warning"))),
			Field("loginEmail", tr.T("strava.login.email.label"), "", Input(ID("loginEmail"), Type("email"), AutoComplete("off"), g.Attr("data-bind", "loginEmail"))),
			Field("loginPassword", tr.T("strava.login.password.label"), tr.T("strava.login.password.help"),
				Input(ID("loginPassword"), Type("password"), AutoComplete("off"), g.Attr("data-bind", "loginPassword"))),
			IndicatorBtn("", tr.T("strava.login.button"), "/actions/strava/login", "signingin"),
		),
		Card(g.Attr("data-signals", `{"cookies":""}`),
			H2(g.Text(tr.T("strava.import.title"))),
			P(g.Text(tr.T("strava.import.intro"))),
			Ul(
				Li(g.Text(tr.T("strava.import.format.json"))),
				Li(g.Text(tr.T("strava.import.format.netscape"))),
				Li(g.Text(tr.T("strava.import.format.header"))),
			),
			Field("cookies", tr.T("strava.import.label"), tr.T("strava.import.help"),
				Textarea(ID("cookies"), g.Attr("data-bind", "cookies"), Placeholder(tr.T("strava.import.placeholder")), g.Attr("spellcheck", "false"), g.Attr("autocomplete", "off"))),
			Btn("primary", tr.T("strava.import.button"), post("/actions/strava/cookies")),
		),
		g.If(s.CanFindActivity, Card(g.Attr("data-signals", `{"processActivityId":""}`),
			H2(g.Text(tr.T("strava.process.title"))),
			P(Class("muted"), g.Text(tr.T("strava.process.intro"))),
			Field("processActivityId", tr.T("strava.process.label"), tr.T("strava.process.help"),
				Input(ID("processActivityId"), Type("text"), g.Attr("inputmode", "numeric"), Placeholder("1234567890"),
					AutoComplete("off"), g.Attr("data-bind", "processActivityId"))),
			IndicatorBtn("primary", tr.T("strava.process.button"), "/actions/process", "processing"),
		)),
		Card(H2(g.Text(tr.T("strava.good.title"))),
			Ul(
				Li(g.Text(tr.T("strava.good.tos"))),
				Li(g.Text(tr.T("strava.good.cookies"))),
				Li(g.Text(tr.T("strava.good.expiry"))),
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

// AccountEmail is AccountEmailT in English, for callers without a request.
func AccountEmail(email string) g.Node { return AccountEmailT(i18n.English(), email) }

// AccountEmailT shows the signed-in address; it is patched after a change.
func AccountEmailT(tr *i18n.Translator, email string) g.Node {
	return P(ID("account-email"), g.Text(tr.T("settings.account.signedin", "email", "")), Strong(g.Text(email)))
}

// DexcomSecretStatus renders the part of the settings page that depends on
// whether the Dexcom password is stored: the help text under the field, plus
// the Test connection button. NtfySecretStatus and WebhookSecretStatus below
// do the same for their own secrets. Each has a stable ID so actionSettings
// can patch it in place after a save, without a full page reload. The plain
// names render English; the T variants take the request's translator.
func DexcomSecretStatus(has bool) g.Node { return DexcomSecretStatusT(i18n.English(), has) }

func DexcomSecretStatusT(tr *i18n.Translator, has bool) g.Node {
	help := tr.T("settings.secret.password.unset")
	if has {
		help = tr.T("settings.secret.password.set")
	}
	return Div(ID("dexcom-secret-status"),
		Div(Class("help"), g.Text(help)),
		g.If(has, Div(append(comp("actions"),
			IndicatorBtn("", tr.T("settings.dexcom.test"), "/actions/dexcom/test", "dxtest"),
			IndicatorBtn("", tr.T("settings.dexcom.resync"), "/actions/glucose/resync", "dxresync"))...)),
	)
}

func NtfySecretStatus(has bool) g.Node { return NtfySecretStatusT(i18n.English(), has) }

func NtfySecretStatusT(tr *i18n.Translator, has bool) g.Node {
	help := tr.T("settings.secret.token.unset")
	if has {
		help = tr.T("settings.secret.token.set")
	}
	return Div(ID("ntfy-secret-status"), Class("help"),
		g.Text(help+" "+tr.T("settings.ntfyToken.help")))
}

func WebhookSecretStatus(has bool) g.Node { return WebhookSecretStatusT(i18n.English(), has) }

func WebhookSecretStatusT(tr *i18n.Translator, has bool) g.Node {
	help := tr.T("settings.secret.secret.unset")
	if has {
		help = tr.T("settings.secret.secret.set")
	}
	return Div(ID("webhook-secret-status"), Class("help"),
		g.Text(help+" "+tr.T("settings.webhookSecret.help")))
}

func SMTPSecretStatus(has bool) g.Node { return SMTPSecretStatusT(i18n.English(), has) }

func SMTPSecretStatusT(tr *i18n.Translator, has bool) g.Node {
	help := tr.T("settings.secret.password.unset")
	if has {
		help = tr.T("settings.secret.password.set")
	}
	return Div(ID("smtp-secret-status"), Class("help"), g.Text(help))
}

// GlucoseImportStatus renders the outcome of a glucose import: exactly one
// of ok/errMsg is non-empty, or both empty for the initial page render. It
// has a stable ID so both the plain-form flash render and the progressive-
// enhancement fetch response (static/glucose-import.js) use the same markup,
// letting the script swap it in without a page reload. The messages arrive
// already translated (or are importer errors), so it has no text of its own.
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
		"settingsTab": "glucose", "language": languageSignal(c.Language),
	}
	for k, v := range overviewSignals(c) {
		sigMap[k] = v
	}
	for k, v := range pushSettingSignals(c) {
		sigMap[k] = v
	}
	sig, _ := json.Marshal(sigMap)
	bind := func(name string) g.Node { return g.Attr("data-bind", name) }
	tr := pd.translator()

	return Page(pd,
		PageHead(tr.T("settings.title"), tr.T("settings.sub")),
		Div(g.Attr("data-signals", string(sig)),
			settingsTabs(tr),
			tabSection("glucose",
				Grid("2",
					Card(H2(g.Text(tr.T("settings.glucose.title"))),
						languageField(pd),
						Field("unit", tr.T("settings.unit.label"), "", Select(ID("unit"), bind("unit"),
							Option(Value("mg/dL"), g.Text("mg/dL")), Option(Value("mmol/L"), g.Text("mmol/L")))),
						Div(append(comp("fieldrow"),
							Field("rangeLow", tr.T("settings.rangeLow.label"), "", Input(ID("rangeLow"), Type("number"), Min("40"), Max("200"), bind("rangeLow"))),
							Field("rangeHigh", tr.T("settings.rangeHigh.label"), "", Input(ID("rangeHigh"), Type("number"), Min("80"), Max("400"), bind("rangeHigh"))),
							Field("veryLow", tr.T("settings.veryLow.label"), tr.T("settings.veryLow.help"), Input(ID("veryLow"), Type("number"), Min("20"), Max("100"), bind("veryLow"))),
							Field("veryHigh", tr.T("settings.veryHigh.label"), tr.T("settings.veryHigh.help"), Input(ID("veryHigh"), Type("number"), Min("150"), Max("600"), bind("veryHigh"))),
						)...),
						Div(append(comp("fieldrow"),
							Field("preMin", tr.T("settings.preMin.label"), "", Input(ID("preMin"), Type("number"), Min("0"), Max("240"), bind("preMin"))),
							Field("postMin", tr.T("settings.postMin.label"), tr.T("settings.postMin.help"), Input(ID("postMin"), Type("number"), Min("0"), Max("240"), bind("postMin"))),
						)...),
						Field("pollMin", tr.T("settings.pollMin.label"), "", Input(ID("pollMin"), Type("number"), Min("1"), Max("1440"), bind("pollMin"))),
						Field("postBuffer", tr.T("settings.postBuffer.label"), tr.T("settings.postBuffer.help"), Input(ID("postBuffer"), Type("number"), Min("0"), Max("180"), bind("postBuffer"))),
					),
					Card(H2(g.Text(tr.T("settings.dexcom.title"))),
						P(Class("muted"), g.Text(tr.T("settings.dexcom.intro"))),
						Field("dexcomRegion", tr.T("settings.dexcom.region.label"), "", Select(ID("dexcomRegion"), bind("dexcomRegion"),
							Option(Value("ous"), g.Text(tr.T("settings.dexcom.region.ous"))), Option(Value("us"), g.Text(tr.T("settings.dexcom.region.us"))), Option(Value("jp"), g.Text(tr.T("settings.dexcom.region.jp"))))),
						Field("dexcomUsername", tr.T("settings.dexcom.username.label"), "", Input(ID("dexcomUsername"), Type("text"), AutoComplete("off"), bind("dexcomUsername"))),
						Field("dexcomPassword", tr.T("settings.dexcom.password.label"), "", Input(ID("dexcomPassword"), Type("password"), AutoComplete("new-password"), bind("dexcomPassword"))),
						DexcomSecretStatusT(tr, d.HasDexcomPassword),
					),
				),
			),
			tabSection("customize",
				Card(H2(g.Text(tr.T("settings.chart.title"))),
					P(Class("muted"), g.Text(tr.T("settings.chart.intro"))),
					Field("chartImage", tr.T("settings.chartImage.label"), tr.T("settings.chartImage.help"), Input(ID("chartImage"), Type("checkbox"), bind("chartImage"))),
					Grid("2",
						Div(
							Field("chartTheme", tr.T("settings.chartTheme.label"), "", Select(ID("chartTheme"), bind("chartTheme"),
								Option(Value("light"), g.Text(tr.T("settings.chartTheme.light"))), Option(Value("dark"), g.Text(tr.T("settings.chartTheme.dark"))))),
							Field("chartSize", tr.T("settings.chartSize.label"), tr.T("settings.chartSize.help"), Select(ID("chartSize"), bind("chartSize"),
								Option(Value("standard"), g.Text(tr.T("settings.chartSize.standard"))), Option(Value("large"), g.Text(tr.T("settings.chartSize.large"))))),
							Field("chartPre", tr.T("settings.chartPre.label"), tr.T("settings.chartPre.help"), Input(ID("chartPre"), Type("number"), Min("0"), Max("240"), bind("chartPre"))),
							Field("chartLine", tr.T("settings.chartLine.label"), "", Input(ID("chartLine"), Type("number"), Min("1"), Max("4"), bind("chartLine"))),
							Field("hrRead", tr.T("settings.hrRead.label"), tr.T("settings.hrRead.help"), Input(ID("hrRead"), Type("checkbox"), bind("hrRead"))),
							Label(g.Text(tr.T("settings.panels.label"))),
							P(Class("muted"), g.Text(tr.T("settings.panels.help"))),
							Div(g.Attr("style", "display:flex;flex-direction:column"),
								chartPanelRow(tr, "chartActivity", "activity", tr.T("settings.panel.activity")),
								chartPanelRow(tr, "chartBand", "band", tr.T("settings.panel.band")),
								chartPanelRow(tr, "chartDots", "dots", tr.T("settings.panel.dots")),
								chartPanelRow(tr, "chartHR", "hr", tr.T("settings.panel.hr")),
								chartPanelRow(tr, "chartElevation", "elevation", tr.T("settings.panel.elevation")),
							),
							Field("chartAvgLine", tr.T("settings.chartAvgLine.label"), tr.T("settings.chartAvgLine.help"), Input(ID("chartAvgLine"), Type("checkbox"), bind("chartAvgLine"))),
							Field("chartRangeLines", tr.T("settings.chartRangeLines.label"), tr.T("settings.chartRangeLines.help"), Input(ID("chartRangeLines"), Type("checkbox"), bind("chartRangeLines"))),
							Field("chartMinMax", tr.T("settings.chartMinMax.label"), tr.T("settings.chartMinMax.help"), Input(ID("chartMinMax"), Type("checkbox"), bind("chartMinMax"))),
							Field("chartHideStats", tr.T("settings.chartHideStats.label"), tr.T("settings.chartHideStats.help"), Input(ID("chartHideStats"), Type("checkbox"), bind("chartHideStats"))),
						),
						Div(
							Img(ID("chartPreview"), Alt(tr.T("settings.chartPreview.alt")), g.Attr("style", "max-width:100%;height:auto;border:1px solid var(--border, #ccc);border-radius:8px"),
								g.Attr("data-attr:src", chartPreviewExpr)),
						),
					),
				),
				overviewSettingsCard(c),
				Card(H2(g.Text(tr.T("settings.desc.title"))),
					P(Class("muted"), g.Text(tr.T("settings.desc.intro"))),
					Field("descPreset", tr.T("settings.descPreset.label"), tr.T("settings.descPreset.help"),
						g.El("select", append([]g.Node{ID("descPreset"), bind("descPreset"), g.Attr("data-on:change", descPresetChangeExpr),
							Option(Value("custom"), g.Text(tr.T("settings.descPreset.custom")))}, presetOptions(tr)...)...)),
					Grid("2",
						Div(
							Field("descTemplate", tr.T("settings.descTemplate.label"), "", Textarea(ID("descTemplate"), Rows("6"), bind("descTemplate"),
								g.Attr("data-on:input__debounce.400ms", descPreviewExpr), g.Attr("spellcheck", "false"))),
						),
						Div(
							Label(g.Text(tr.T("settings.descPreview.label"))),
							Pre(ID("descPreviewBox"), g.Attr("data-text", "$descPreview")),
						),
					),
				),
			),
			tabSection("notify",
				Card(H2(g.Text(tr.T("settings.notify.title"))),
					P(Class("muted"), g.Text(tr.T("settings.notify.intro"))),
					Field("gapAlertHours", tr.T("settings.gapAlertHours.label"), tr.T("settings.gapAlertHours.help"), Input(ID("gapAlertHours"), Type("number"), Min("0"), Max("168"), bind("gapAlertHours"))),
					Grid("2",
						Div(
							Field("ntfyURL", tr.T("settings.ntfyURL.label"), tr.T("settings.ntfyURL.help"), Input(ID("ntfyURL"), Type("url"), bind("ntfyURL"))),
							Field("ntfyToken", tr.T("settings.ntfyToken.label"), "", Input(ID("ntfyToken"), Type("password"), AutoComplete("new-password"), bind("ntfyToken"))),
							NtfySecretStatusT(tr, d.HasNtfyToken),
						),
						Div(
							Field("webhookURL", tr.T("settings.webhookURL.label"), tr.T("settings.webhookURL.help"), Input(ID("webhookURL"), Type("url"), bind("webhookURL"))),
							Field("webhookSecret", tr.T("settings.webhookSecret.label"), "", Input(ID("webhookSecret"), Type("password"), AutoComplete("new-password"), bind("webhookSecret"))),
							WebhookSecretStatusT(tr, d.HasWebhookSecret),
						),
					),
					Card(H2(g.Text(tr.T("settings.email.title"))),
						P(Class("muted"), g.Text(tr.T("settings.email.intro"))),
						Field("emailTo", tr.T("settings.emailTo.label"), "", Input(ID("emailTo"), Type("email"), bind("emailTo"))),
						Div(append(comp("fieldrow"),
							Field("smtpHost", tr.T("settings.smtpHost.label"), "", Input(ID("smtpHost"), Type("text"), AutoComplete("off"), bind("smtpHost"))),
							Field("smtpPort", tr.T("settings.smtpPort.label"), tr.T("settings.smtpPort.help"), Input(ID("smtpPort"), Type("number"), Min("1"), Max("65535"), bind("smtpPort"))),
						)...),
						Div(append(comp("fieldrow"),
							Field("smtpUsername", tr.T("settings.smtpUsername.label"), "", Input(ID("smtpUsername"), Type("text"), AutoComplete("off"), bind("smtpUsername"))),
							Field("smtpPassword", tr.T("settings.smtpPassword.label"), "", Input(ID("smtpPassword"), Type("password"), AutoComplete("new-password"), bind("smtpPassword"))),
						)...),
						SMTPSecretStatusT(tr, d.HasSMTPPassword),
						Div(append(comp("fieldrow"),
							Field("smtpSender", tr.T("settings.smtpSender.label"), "", Input(ID("smtpSender"), Type("email"), bind("smtpSender"))),
							Field("smtpSenderName", tr.T("settings.smtpSenderName.label"), "", Input(ID("smtpSenderName"), Type("text"), bind("smtpSenderName"))),
						)...),
						Field("smtpTLS", tr.T("settings.smtpTLS.label"), tr.T("settings.smtpTLS.help"), Input(ID("smtpTLS"), Type("checkbox"), bind("smtpTLS"))),
						H3(g.Text(tr.T("settings.email.what"))),
						Field("mailAlerts", tr.T("settings.mailAlerts.label"), tr.T("settings.mailAlerts.help"), Input(ID("mailAlerts"), Type("checkbox"), bind("mailAlerts"))),
						Field("mailActivity", tr.T("settings.mailActivity.label"), tr.T("settings.mailActivity.help"), Input(ID("mailActivity"), Type("checkbox"), bind("mailActivity"))),
						Field("mailWeekly", tr.T("settings.mailWeekly.label"), tr.T("settings.mailWeekly.help"), Input(ID("mailWeekly"), Type("checkbox"), bind("mailWeekly"))),
						Field("mailHealth", tr.T("settings.mailHealth.label"), tr.T("settings.mailHealth.help"), Input(ID("mailHealth"), Type("checkbox"), bind("mailHealth"))),
						Field("publicURL", tr.T("settings.publicURL.label"), tr.T("settings.publicURL.help"), Input(ID("publicURL"), Type("url"), bind("publicURL"))),
					),
				),
				PushCardT(tr, d.Push),
			),
			tabSection("data",
				g.If(len(d.ImportFormats) > 0, Card(
					H2(g.Text(tr.T("settings.import.title"))),
					P(Class("muted"), g.Text(tr.T("settings.import.intro"))),
					GlucoseImportStatus(d.ImportOK, d.ImportErr),
					Form(ID("glucose-import-form"), Method("post"), Action("/actions/glucose/import"), g.Attr("enctype", "multipart/form-data"),
						Field("glucoseFormat", tr.T("settings.import.format.label"), "", Select(Name("format"), Required(), importFormatOptions(d.ImportFormats))),
						Field("glucoseSource", tr.T("settings.import.source.label"), tr.T("settings.import.source.help"),
							Input(Type("text"), Name("source"), AutoComplete("off"))),
						Field("glucoseFile", tr.T("settings.import.file.label"), "", Input(Type("file"), Name("file"), g.Attr("accept", ".csv,.json,.txt,.zip"), Required())),
						SubmitBtn("primary", "", tr.T("settings.import.submit")),
					),
					Script(Src("/static/glucose-import.js")),
				)),
				Card(g.Attr("data-signals", `{"accountCurrent":"","accountEmail":"","accountNew":"","accountConfirm":""}`),
					H2(g.Text(tr.T("settings.account.title"))),
					AccountEmailT(tr, d.AccountEmail),
					P(Class("muted"), g.Text(tr.T("settings.account.intro"))),
					Field("accountCurrent", tr.T("settings.account.current.label"), "", Input(ID("accountCurrent"), Type("password"), AutoComplete("current-password"), bind("accountCurrent"))),
					Field("accountEmail", tr.T("settings.account.email.label"), "", Input(ID("accountEmail"), Type("email"), AutoComplete("off"), bind("accountEmail"))),
					Field("accountNew", tr.T("settings.account.new.label"), tr.T("settings.account.new.help"), Input(ID("accountNew"), Type("password"), AutoComplete("new-password"), bind("accountNew"))),
					Field("accountConfirm", tr.T("settings.account.confirm.label"), "", Input(ID("accountConfirm"), Type("password"), AutoComplete("new-password"), bind("accountConfirm"))),
					Btn("", tr.T("settings.account.button"), post("/actions/account")),
				),
				Card(H2(g.Text(tr.T("settings.data.title"))),
					P(Class("muted"), g.Text(tr.T("settings.data.intro"))),
					Field("retentionDays", tr.T("settings.retentionDays.label"), "", Input(ID("retentionDays"), Type("number"), Min("0"), Max("3650"), bind("retentionDays"))),
					Div(append(comp("actions"),
						A(append(comp("button"), Href("/export/samples.csv"), g.Attr("download", ""), g.Text(tr.T("settings.data.samples")))...),
						A(append(comp("button"), Href("/export/activities.csv"), g.Attr("download", ""), g.Text(tr.T("settings.data.activities")))...),
						A(append(comp("button"), Href("/export/report.pdf?range=30d"), g.Attr("download", ""), g.Text(tr.T("settings.data.report")))...),
					)...),
					Field("purgeConfirm", tr.T("settings.purge.label"), tr.T("settings.purge.help"),
						Input(ID("purgeConfirm"), Type("text"), AutoComplete("off"), bind("purgeConfirm"))),
					Btn("danger", tr.T("settings.purge.label"), post("/actions/data/purge"), g.Attr("data-attr:disabled", "$purgeConfirm !== 'DELETE'")),
				),
			),
			Div(append(comp("actions"),
				Btn("primary", tr.T("settings.save"), post("/actions/settings")),
				Btn("", tr.T("settings.test"), post("/actions/notify/test")),
			)...),
		),
	)
}

// ------------------------------------------------------------------- tokens

// TokenList is TokenListT in English, for callers without a request.
func TokenList(list []tokens.Info, loc *time.Location) g.Node {
	return TokenListT(i18n.English(), list, loc)
}

// TokenListT is patched after creating or revoking a token.
func TokenListT(tr *i18n.Translator, list []tokens.Info, loc *time.Location) g.Node {
	if len(list) == 0 {
		return Card(ID("token-list"), H2(g.Text(tr.T("tokens.list.title"))), Div(append(comp("empty"), g.Text(tr.T("tokens.list.empty")))...))
	}
	rows := make([]g.Node, 0, len(list))
	for _, t := range list {
		used := tr.T("tokens.never")
		if !t.LastUsed.IsZero() {
			used = fmtWhenT(tr, t.LastUsed, loc, time.Now())
		}
		state := Badge("ok", tr.T("tokens.state.active"))
		action := g.Node(revokeButton(tr, t.Name))
		if t.Revoked {
			state, action = Badge("", tr.T("tokens.state.revoked")), Span()
		}
		rows = append(rows, Tr(Td(g.Text(t.Name)), Td(state), Td(g.Text(used)), Td(action)))
	}
	return Card(ID("token-list"), H2(g.Text(tr.T("tokens.list.title"))),
		Div(append(comp("tablewrap"), Table(append(comp("table"),
			THead(Tr(Th(g.Text(tr.T("tokens.col.name"))), Th(g.Text(tr.T("tokens.col.state"))), Th(g.Text(tr.T("tokens.col.lastused"))), Th())), TBody(g.Group(rows)))...))...))
}

func revokeButton(tr *i18n.Translator, name string) g.Node {
	return BtnSized("danger", "sm", tr.T("tokens.revoke"), post("/actions/tokens/revoke/"+url.PathEscape(name)))
}

// CopyRow shows a value with a button that copies it.
func CopyRow(tr *i18n.Translator, label, value string) g.Node {
	return Div(Class("copyrow"),
		Span(Class("muted"), g.Text(label)),
		Code(g.Text(value)),
		BtnSized("", "sm", tr.T("tokens.copy"), g.Attr("data-on:click",
			fmt.Sprintf("navigator.clipboard.writeText('%s'); evt.currentTarget.textContent = '%s'", jsQuote(value), jsQuote(tr.T("tokens.copied"))))),
	)
}

// SecretReveal is SecretRevealT in English, for callers without a request.
func SecretReveal(name, token, endpoint, taskerURL, pageURL string) g.Node {
	return SecretRevealT(i18n.English(), name, token, endpoint, taskerURL, pageURL)
}

// SecretRevealT shows a new token once, with the values a phone app needs,
// plus two QR codes while the token is still on screen: one to download a
// ready-to-import Tasker profile directly (for Android), and one that just
// opens this page on whatever device scans it (for HTTP Shortcuts,
// MacroDroid or Apple Shortcuts, which can't be pre-built as a file the
// same way — the target is this page's own per-platform instructions,
// filled in with the real token below instead of a placeholder).
func SecretRevealT(tr *i18n.Translator, name, token, endpoint, taskerURL, pageURL string) g.Node {
	header := "Bearer " + token
	return Div(ID("token-reveal"), Div(append(comp("secretbox"),
		P(Strong(g.Text(tr.T("tokens.reveal.created", "name", name)))),
		CopyRow(tr, tr.T("tokens.reveal.token"), token),
		CopyRow(tr, tr.T("tokens.reveal.url"), endpoint),
		CopyRow(tr, tr.T("tokens.reveal.header"), "Authorization: "+header),
		CopyRow(tr, tr.T("tokens.reveal.test"), fmt.Sprintf("curl -i -X POST -H 'Authorization: %s' %s", header, endpoint)),
		P(Class("muted"), g.Text(tr.T("tokens.reveal.hint"))),
		Grid("2",
			Div(
				H3(g.Text(tr.T("tokens.tasker.title"))),
				P(Class("muted"), g.Text(tr.T("tokens.tasker.intro"))),
				A(append(comp("button"), Href(taskerURL), g.Attr("download", ""), g.Text(tr.T("tokens.tasker.download")))...),
				g.If(qrDataURI(taskerURL) != "", Img(Alt(tr.T("tokens.tasker.qr")), Src(qrDataURI(taskerURL)), g.Attr("style", "width:160px;height:160px;margin-top:.5rem"))),
			),
			Div(
				H3(g.Text(tr.T("tokens.other.title"))),
				P(Class("muted"), g.Text(tr.T("tokens.other.intro"))),
				g.If(qrDataURI(pageURL) != "", Img(Alt(tr.T("tokens.other.qr")), Src(qrDataURI(pageURL)), g.Attr("style", "width:160px;height:160px;margin-top:.5rem"))),
			),
		),
	)...))
}

// TokensPage manages the push-trigger tokens and explains how to use them.
func TokensPage(pd PageData, list []tokens.Info, baseURL string, loc *time.Location) g.Node {
	tr := pd.translator()
	endpoint := strings.TrimRight(baseURL, "/") + "/api/trigger"
	return Page(pd,
		PageHead(tr.T("tokens.title"), tr.T("tokens.sub")),
		Card(g.Attr("data-signals", `{"tokenName":""}`),
			H2(g.Text(tr.T("tokens.new.title"))),
			Div(append(comp("fieldrow"),
				Field("tokenName", tr.T("tokens.name.label"), tr.T("tokens.name.help"), Input(ID("tokenName"), Type("text"), Placeholder(tr.T("tokens.name.placeholder")), g.Attr("data-bind", "tokenName"))),
			)...),
			Btn("primary", tr.T("tokens.create"), post("/actions/tokens/create")),
			Div(ID("token-reveal")),
		),
		TokenListT(tr, list, loc),
		Card(H2(g.Text(tr.T("tokens.how.title"))),
			P(g.Text(tr.T("tokens.how.intro"))),
			Pre(g.Textf("curl -i -X POST %s \\\n  -H \"Authorization: Bearer <token>\"", endpoint)),
			H2(g.Text(tr.T("tokens.how.android.title"))),
			Ol(
				Li(g.Text(tr.T("tokens.how.android.1"))),
				Li(g.Text(tr.T("tokens.how.android.2"))),
				Li(g.Text(tr.T("tokens.how.android.3"))),
				Li(g.Text(tr.T("tokens.how.android.4"))),
			),
			P(Class("muted"), g.Text(tr.T("tokens.how.android.other"))),
			H2(g.Text(tr.T("tokens.how.iphone.title"))),
			P(g.Text(tr.T("tokens.how.iphone.body"))),
			P(Class("muted"), g.Text(tr.T("tokens.how.fallback"))),
		),
	)
}

// ------------------------------------------------------------------- events

// EventsPage lists notifications.
func EventsPage(pd PageData, evs []store.EventRow, loc *time.Location, now time.Time) g.Node {
	tr := pd.translator()
	body := g.Node(Div(append(comp("empty"), t(pd, "events.empty"))...))
	if len(evs) > 0 {
		rows := make([]g.Node, 0, len(evs))
		for _, e := range evs {
			delivered := Badge("", tr.T("events.delivery.waiting"))
			if e.Notified {
				delivered = Badge("ok", tr.T("events.delivery.sent"))
			}
			rows = append(rows, Tr(
				Td(g.Text(fmtWhenT(tr, e.Created, loc, now))), Td(SeverityBadgeT(tr, e.Severity)),
				Td(g.Text(eventTypeLabel(tr, e.Type))),
				Td(g.Text(eventMessage(tr, loc, e)), g.If(e.StravaID != "", A(Href("/activity/"+e.StravaID), g.Text(" "+tr.T("events.link.activity"))))),
				Td(Class("hide-sm"), delivered),
			))
		}
		body = Div(append(comp("tablewrap"), Table(append(comp("table"),
			THead(Tr(Th(t(pd, "events.col.when")), Th(t(pd, "events.col.level")), Th(t(pd, "events.col.type")), Th(t(pd, "events.col.message")), Th(Class("hide-sm"), t(pd, "events.col.delivery")))),
			TBody(g.Group(rows)))...))...)
	}
	return Page(pd,
		PageHead(tr.T("events.title"), tr.T("events.sub")),
		Card(body),
	)
}

// logLevelBadge maps an slog.Level to the existing badge variants, so /logs
// reuses the same color coding as severity elsewhere (Notifications page).
func logLevelBadge(tr *i18n.Translator, level slog.Level) g.Node {
	switch {
	case level >= slog.LevelError:
		return Badge("error", tr.T("logs.level.error"))
	case level >= slog.LevelWarn:
		return Badge("warning", tr.T("logs.level.warn"))
	case level >= slog.LevelInfo:
		return Badge("info", tr.T("logs.level.info"))
	default:
		return Badge("", tr.T("logs.level.debug"))
	}
}

// LogRows is LogRowsT in English, for callers without a request.
func LogRows(entries []logging.Entry, loc *time.Location) g.Node {
	return LogRowsT(i18n.English(), entries, loc)
}

// LogRowsT renders the log table body, newest first. It is also the fragment
// streamLogs patches in place on every bus wake, so it must be re-renderable
// standalone (no page chrome), the same shape as LiveDash for the dashboard.
func LogRowsT(tr *i18n.Translator, entries []logging.Entry, loc *time.Location) g.Node {
	if len(entries) == 0 {
		return Div(append(comp("empty"), ID("log-rows"), g.Text(tr.T("logs.empty")))...)
	}
	rows := make([]g.Node, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, Tr(
			Td(g.Text(e.Time.In(loc).Format("15:04:05"))),
			Td(logLevelBadge(tr, e.Level)),
			Td(g.Text(e.Message)),
			Td(Class("hide-sm"), g.Text(e.Attrs)),
		))
	}
	return Div(append(comp("tablewrap"), ID("log-rows"), Table(append(comp("table"),
		THead(Tr(Th(g.Text(tr.T("logs.col.time"))), Th(g.Text(tr.T("logs.col.level"))), Th(g.Text(tr.T("logs.col.message"))), Th(Class("hide-sm"), g.Text(tr.T("logs.col.details"))))),
		TBody(g.Group(rows)))...))...)
}

// LogsPage is an admin-only diagnostic view of recent structured log entries,
// live-updated via /stream/logs the same way the dashboard streams over
// /stream/live. No Settings toggle, same as the Notifications/Events page.
func LogsPage(pd PageData, entries []logging.Entry, loc *time.Location) g.Node {
	tr := pd.translator()
	return Page(pd,
		PageHead(tr.T("logs.title"), tr.T("logs.sub")),
		Card(
			Div(g.Attr("data-init", "@get('/stream/logs')"), LogRowsT(tr, entries, loc)),
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
func chartPanelRow(tr *i18n.Translator, sig, id, label string) g.Node {
	return Div(g.Attr("style", "display:flex;align-items:center;gap:.5rem;margin:0 0 .5rem"),
		g.Attr("data-style:order", fmt.Sprintf("$chartPanelOrder.split(',').indexOf(%q)", id)),
		Input(Type("checkbox"), g.Attr("data-bind", sig)),
		Span(g.Text(label)),
		Btn("", tr.T("settings.panel.earlier"), g.Attr("data-on:click", panelMoveExpr(id, -1))),
		Btn("", tr.T("settings.panel.later"), g.Attr("data-on:click", panelMoveExpr(id, 1))),
	)
}

// settingsTabIDs lists the settings page's tabs, in display order. Every
// field on the page still lives under one Div per tab (data-signals covers
// the whole page regardless of which is shown), so Save always saves
// everything: switching tabs never loses or hides a change.
var settingsTabIDs = []struct{ id, labelKey string }{
	{"glucose", i18n.Key("settings.tab.glucose")},
	{"customize", i18n.Key("settings.tab.customize")},
	{"notify", i18n.Key("settings.tab.notify")},
	{"data", i18n.Key("settings.tab.data")},
}

// settingsTabs renders the tab bar. Each button sets $settingsTab and is
// shown pressed (the primary variant) exactly when it is the active one.
func settingsTabs(tr *i18n.Translator) g.Node {
	btns := make([]g.Node, len(settingsTabIDs))
	for i, t := range settingsTabIDs {
		active := fmt.Sprintf("$settingsTab === %q", t.id)
		btns[i] = Btn("", tr.T(t.labelKey), // i18n:dynamic (keys registered in settingsTabIDs)
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
func presetOptions(tr *i18n.Translator) []g.Node {
	opts := make([]g.Node, len(render.Presets))
	for i, p := range render.Presets {
		name, desc := render.PresetText(tr, p.ID)
		opts[i] = Option(Value(p.ID), g.Text(name+" — "+desc))
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
