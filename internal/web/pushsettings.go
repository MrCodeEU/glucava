package web

import (
	"fmt"
	"strings"
	"time"

	g "maragu.dev/gomponents"
	. "maragu.dev/gomponents/html"

	"github.com/MrCodeEU/glucava/internal/i18n"
	"github.com/MrCodeEU/glucava/internal/notify"
	"github.com/MrCodeEU/glucava/internal/store"
)

// PushCardData is what the Settings "Push notifications" card needs.
type PushCardData struct {
	VAPIDPublic string // public application server key, for subscribing
	Devices     []notify.PushSub
	Loc         *time.Location
	Now         time.Time
}

// pushSettingSignals are the card's two switches; they are saved together
// with the rest of the form (see settingsSignals).
func pushSettingSignals(c store.Config) map[string]any {
	return map[string]any{"pushAlerts": c.PushAlerts, "pushSummaries": c.PushSummaries}
}

const (
	// pushEnableExpr asks for permission, subscribes, then tells the server.
	// The first statement must run inside the click so the browser accepts the
	// permission prompt. Data-on expressions are not async, hence .then().
	pushEnableExpr = `$pushErr='';gvPush.enable(el.closest('[data-vapid]').dataset.vapid).then(s=>{$pushSub=s;$pushState='on';return @post('/actions/push/subscribe')}).catch(e=>{$pushErr=gvPush.message(e);gvPush.state().then(s=>$pushState=s)})`
	// pushDisableExpr unsubscribes this browser, then tells the server.
	pushDisableExpr = `$pushErr='';gvPush.disable().then(ep=>{$pushSub=ep;$pushState='off';return @post('/actions/push/unsubscribe')}).catch(e=>{$pushErr=gvPush.message(e)})`
	pushStateExpr   = `gvPush.state().then(s=>$pushState=s)`
)

// pushNotice is an inline message shown for one browser state.
func pushNotice(state, variant string, text string) g.Node {
	return Div(g.Attr("data-show", fmt.Sprintf("$pushState==='%s'", state)), Notice(variant, g.Text(text)))
}

// PushCard is PushCardT in English, for callers without a request.
func PushCard(d PushCardData) g.Node { return PushCardT(i18n.English(), d) }

// PushCardT is the Settings card for Web Push: support status, enable and
// disable for this device, a test, the switches and the list of devices. The
// two browser-side messages (static/pwa.js) travel as data-* attributes, so
// the script carries no text of its own.
func PushCardT(tr *i18n.Translator, d PushCardData) g.Node {
	bind := func(name string) g.Node { return g.Attr("data-bind", name) }
	return Card(ID("push-card"), g.Attr("data-vapid", d.VAPIDPublic),
		g.Attr("data-msg-denied", tr.T("push.js.denied")), g.Attr("data-msg-error", tr.T("push.js.error")),
		g.Attr("data-signals", `{"pushState":"checking","pushSub":"","pushErr":""}`),
		g.Attr("data-init", pushStateExpr),
		H2(g.Text(tr.T("push.title"))),
		P(Class("muted"), g.Text(tr.T("push.intro"))),
		pushNotice("insecure", "warning", tr.T("push.notice.insecure")),
		pushNotice("ios-install", "warning", tr.T("push.notice.ios")),
		pushNotice("unsupported", "warning", tr.T("push.notice.unsupported")),
		pushNotice("denied", "error", tr.T("push.notice.denied")),
		Div(g.Attr("data-show", "$pushErr !== ''"), Notice("error", Span(g.Attr("data-text", "$pushErr")))),
		g.If(d.VAPIDPublic == "", Notice("error", g.Text(tr.T("push.notice.nokey")))),
		Div(append(comp("actions"),
			Btn("primary", tr.T("push.enable"), g.Attr("data-show", "$pushState==='off'"), g.Attr("data-on:click", pushEnableExpr)),
			Btn("", tr.T("push.disable"), g.Attr("data-show", "$pushState==='on'"), g.Attr("data-on:click", pushDisableExpr)),
		)...),
		H3(g.Text(tr.T("push.what"))),
		Field("pushAlerts", tr.T("push.alerts.label"), tr.T("push.alerts.help"), Input(ID("pushAlerts"), Type("checkbox"), bind("pushAlerts"))),
		Field("pushSummaries", tr.T("push.summaries.label"), tr.T("push.summaries.help"), Input(ID("pushSummaries"), Type("checkbox"), bind("pushSummaries"))),
		P(Class("muted"), g.Text(tr.T("push.saved"))),
		PushDevicesT(tr, d.Devices, d.Loc, d.Now),
	)
}

// PushDevices is PushDevicesT in English, for callers without a request.
func PushDevices(devs []notify.PushSub, loc *time.Location, now time.Time) g.Node {
	return PushDevicesT(i18n.English(), devs, loc, now)
}

// PushDevicesT is the device list with the test button. It has a stable id
// so the push actions patch it in place.
func PushDevicesT(tr *i18n.Translator, devs []notify.PushSub, loc *time.Location, now time.Time) g.Node {
	if loc == nil {
		loc = time.Local
	}
	if len(devs) == 0 {
		return Div(ID("push-devices"), H3(g.Text(tr.T("push.devices"))), Div(append(comp("empty"), g.Text(tr.T("push.devices.none")))...))
	}
	rows := make([]g.Node, 0, len(devs))
	for _, sub := range devs {
		last := tr.T("push.devices.never")
		if !sub.LastOK.IsZero() {
			last = fmtWhenT(tr, sub.LastOK, loc, now)
		}
		rows = append(rows, Li(g.Attr("style", "display:flex;gap:.75rem;align-items:center;justify-content:space-between;padding:.4rem 0"),
			Div(
				Div(g.Text(deviceName(tr, sub.UserAgent))),
				Div(Class("help"), g.Text(tr.T("push.devices.added", "added", fmtWhenT(tr, sub.Created, loc, now), "last", last))),
				g.If(sub.LastError != "", Div(Class("help"), Badge("error", tr.T("push.devices.lasterror")), g.Text(" "+sub.LastError))),
			),
			BtnSized("danger", "sm", tr.T("push.devices.remove"), g.Attr("data-on:click", fmt.Sprintf("@post('/actions/push/remove/%s')", jsQuote(sub.ID)))),
		))
	}
	return Div(ID("push-devices"), H3(g.Text(tr.T("push.devices"))),
		Ul(g.Attr("style", "list-style:none;margin:0;padding:0"), g.Group(rows)),
		Div(append(comp("actions"), IndicatorBtn("", tr.T("push.devices.test"), "/actions/push/test", "pushtest"))...),
	)
}

// deviceName shortens a User-Agent to something recognisable: the browser
// and the system. Browser and system names are brands and stay as they are.
func deviceName(tr *i18n.Translator, ua string) string {
	if ua == "" {
		return tr.T("push.device.unknown")
	}
	browser := tr.T("push.device.browser")
	for _, b := range []struct{ token, name string }{
		{"Edg/", "Edge"}, {"OPR/", "Opera"}, {"Firefox/", "Firefox"}, {"CriOS/", "Chrome"}, {"FxiOS/", "Firefox"},
		{"Chrome/", "Chrome"}, {"Safari/", "Safari"},
	} {
		if strings.Contains(ua, b.token) {
			browser = b.name
			break
		}
	}
	system := ""
	for _, o := range []struct{ token, name string }{
		{"iPhone", "iPhone"}, {"iPad", "iPad"}, {"Android", "Android"}, {"Windows", "Windows"},
		{"Mac OS X", "macOS"}, {"CrOS", "ChromeOS"}, {"Linux", "Linux"},
	} {
		if strings.Contains(ua, o.token) {
			system = o.name
			break
		}
	}
	if system == "" {
		return browser
	}
	return tr.T("push.device.on", "browser", browser, "system", system)
}
