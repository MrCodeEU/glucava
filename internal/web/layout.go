package web

import (
	"fmt"

	g "maragu.dev/gomponents"
	. "maragu.dev/gomponents/html"
)

// PageData is what the layout needs for every page.
type PageData struct {
	Title  string
	Active string // nav key: dashboard, strava, settings, tokens, events
	User   string
	Build  string
	Demo   bool
	Alerts int // recent error events, shown as a badge on Notifications
}

var navItems = []struct{ key, href, label string }{
	{"dashboard", "/", "Activities"},
	{"strava", "/strava", "Strava session"},
	{"settings", "/settings", "Settings"},
	{"tokens", "/tokens", "Triggers"},
	{"events", "/events", "Notifications"},
}

// themeToggle is a Datastar expression; the initial mode is applied by /static/theme.js.
const themeToggle = `const d=document.documentElement,c=d.dataset.mode||(matchMedia('(prefers-color-scheme: dark)').matches?'dark':'light'),m=c==='dark'?'light':'dark';d.dataset.mode=m;try{localStorage.setItem('gv-mode',m)}catch(e){}`

func head(title, build string) g.Node {
	v := "?v=" + build
	return Head(
		Meta(Charset("utf-8")),
		Meta(Name("viewport"), Content("width=device-width, initial-scale=1")),
		Meta(Name("robots"), Content("noindex")),
		TitleEl(g.Text(title+" · glucava")),
		Link(Rel("icon"), Type("image/svg+xml"), Href("/static/favicon.svg")),
		Link(Rel("stylesheet"), Href("/static/app.css"+v)),
		Script(Src("/static/theme.js")),
		Script(Type("module"), Src("/static/datastar.js"+v)),
	)
}

// Page wraps body in the app shell with navigation.
func Page(pd PageData, body ...g.Node) g.Node {
	links := make([]g.Node, 0, len(navItems))
	for _, it := range navItems {
		label := []g.Node{g.Text(it.label)}
		if it.key == "events" && pd.Alerts > 0 {
			label = append(label, g.Text(" "), Badge("error", fmt.Sprint(pd.Alerts)))
		}
		links = append(links, A(append(comp("navlink"),
			Href(it.href), g.Group(label),
			g.If(it.key == pd.Active, g.Attr("aria-current", "page")))...))
	}

	return g.Group([]g.Node{
		g.Raw("<!doctype html>"),
		HTML(Lang("en"),
			head(pd.Title, pd.Build),
			Body(Div(append(comp("shell"),
				g.If(pd.Demo, Div(append(comp("banner"), g.Text("Demo mode: all data is made up and nothing is sent to Strava."))...)),
				Nav(append(comp("nav"), Div(
					A(append(comp("brand"), Href("/"),
						Img(Src("/static/favicon.svg"), Alt("")), g.Text("glucava"))...),
					g.Group(links),
					Span(comp("navspacer")...),
					BtnSized("", "sm", "Theme", g.Attr("data-on:click", themeToggle), g.Attr("aria-label", "Toggle dark mode")),
					Form(Method("post"), Action("/logout"),
						SubmitBtn("", "sm", "Sign out ("+pd.User+")")),
				))...),
				Main(g.Group(body)),
				Div(append(comp("footer"), g.Text("glucava "+pd.Build+" · self-hosted, open source"))...),
				Div(ID("toast"), g.Attr("aria-live", "polite")),
			)...)),
		),
	})
}

// LoginPage is the sign-in form. It is a plain HTML form, so it works without JavaScript.
// email is redisplayed after a failed attempt so a mistyped password does not
// also cost the address; the password field is always left blank.
func LoginPage(build, errMsg, email string) g.Node {
	return g.Group([]g.Node{
		g.Raw("<!doctype html>"),
		HTML(Lang("en"),
			head("Sign in", build),
			Body(Div(append(comp("loginwrap"),
				Card(
					H1(g.Text("glucava")),
					P(Class("muted"), g.Text("Sign in to continue.")),
					g.If(errMsg != "", Notice("error", g.Text(errMsg))),
					Form(Method("post"), Action("/login"),
						Field("email", "Email", "", Input(ID("email"), Name("email"), Type("email"), Value(email), Required(), AutoComplete("username"), AutoFocus())),
						Field("password", "Password", "", Input(ID("password"), Name("password"), Type("password"), Required(), AutoComplete("current-password"))),
						SubmitBtn("primary", "", "Sign in"),
					),
				),
			)...)),
		),
	})
}
