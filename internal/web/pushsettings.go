package web

import (
	"fmt"
	"strings"
	"time"

	g "maragu.dev/gomponents"
	. "maragu.dev/gomponents/html"

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

// PushCard is the Settings card for Web Push: support status, enable and
// disable for this device, a test, the switches and the list of devices.
func PushCard(d PushCardData) g.Node {
	bind := func(name string) g.Node { return g.Attr("data-bind", name) }
	return Card(ID("push-card"), g.Attr("data-vapid", d.VAPIDPublic),
		g.Attr("data-signals", `{"pushState":"checking","pushSub":"","pushErr":""}`),
		g.Attr("data-init", pushStateExpr),
		H2(g.Text("Push notifications")),
		P(Class("muted"), g.Text("Get alerts and summaries as a notification on this phone or computer, without an app. "+
			"Messages are sent through your browser's push service (Google, Mozilla or Apple) and contain the notification text only.")),
		pushNotice("insecure", "warning", "Push needs a secure connection. Open glucava over HTTPS (or on localhost) to turn it on."),
		pushNotice("ios-install", "warning", "On iPhone and iPad, add glucava to the Home Screen first (Share, then Add to Home Screen), open it from there, and turn push on in that window. Needs iOS 16.4 or newer."),
		pushNotice("unsupported", "warning", "This browser cannot receive push notifications."),
		pushNotice("denied", "error", "Notifications are blocked for this site. Allow them in the browser's site settings, then reload this page."),
		Div(g.Attr("data-show", "$pushErr !== ''"), Notice("error", Span(g.Attr("data-text", "$pushErr")))),
		g.If(d.VAPIDPublic == "", Notice("error", g.Text("The push key could not be created. Check the server log."))),
		Div(append(comp("actions"),
			Btn("primary", "Enable on this device", g.Attr("data-show", "$pushState==='off'"), g.Attr("data-on:click", pushEnableExpr)),
			Btn("", "Disable on this device", g.Attr("data-show", "$pushState==='on'"), g.Attr("data-on:click", pushDisableExpr)),
		)...),
		H3(g.Text("What to push")),
		Field("pushAlerts", "Failure alerts", "Expired session, failed update, missing glucose data, canary failures.", Input(ID("pushAlerts"), Type("checkbox"), bind("pushAlerts"))),
		Field("pushSummaries", "Summaries", "After each processed activity, the weekly summary and the monthly health report.", Input(ID("pushSummaries"), Type("checkbox"), bind("pushSummaries"))),
		P(Class("muted"), g.Text("Saved with Save settings below. Push messages never contain charts or more than a few lines of text.")),
		PushDevices(d.Devices, d.Loc, d.Now),
	)
}

// PushDevices is the device list with the test button. It has a stable id
// so the push actions patch it in place.
func PushDevices(devs []notify.PushSub, loc *time.Location, now time.Time) g.Node {
	if loc == nil {
		loc = time.Local
	}
	if len(devs) == 0 {
		return Div(ID("push-devices"), H3(g.Text("Devices")), Div(append(comp("empty"), g.Text("No device is subscribed yet."))...))
	}
	rows := make([]g.Node, 0, len(devs))
	for _, sub := range devs {
		last := "never"
		if !sub.LastOK.IsZero() {
			last = fmtWhen(sub.LastOK, loc, now)
		}
		rows = append(rows, Li(g.Attr("style", "display:flex;gap:.75rem;align-items:center;justify-content:space-between;padding:.4rem 0"),
			Div(
				Div(g.Text(deviceName(sub.UserAgent))),
				Div(Class("help"), g.Textf("Added %s · last delivered %s", fmtWhen(sub.Created, loc, now), last)),
				g.If(sub.LastError != "", Div(Class("help"), Badge("error", "last error"), g.Text(" "+sub.LastError))),
			),
			BtnSized("danger", "sm", "Remove", g.Attr("data-on:click", fmt.Sprintf("@post('/actions/push/remove/%s')", jsQuote(sub.ID)))),
		))
	}
	return Div(ID("push-devices"), H3(g.Text("Devices")),
		Ul(g.Attr("style", "list-style:none;margin:0;padding:0"), g.Group(rows)),
		Div(append(comp("actions"), IndicatorBtn("", "Send test push", "/actions/push/test", "pushtest"))...),
	)
}

// deviceName shortens a User-Agent to something recognisable: the browser
// and the system.
func deviceName(ua string) string {
	if ua == "" {
		return "Unknown device"
	}
	browser := "Browser"
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
	return browser + " on " + system
}
