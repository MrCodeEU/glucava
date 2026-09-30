package web

import (
	"fmt"
	"strings"

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

// navItem is one top-level destination. Primary ones sit in the bar (bottom
// tab bar on phones); the rest go in the "System" menu, which keeps the bar
// short enough to fit five tabs on a 390 px screen.
type navItem struct{ key, href, label, icon string }

var (
	navPrimary = []navItem{
		{"dashboard", "/", "Activities", "activity"},
		{"stats", "/stats", "Overview", "chart"},
		{"events", "/events", "Notifications", "bell"},
		{"settings", "/settings", "Settings", "sliders"},
	}
	navSystem = []navItem{
		{"strava", "/strava", "Strava session", "link"},
		{"tokens", "/tokens", "Triggers", "zap"},
		{"logs", "/logs", "Logs", "scroll"},
	}
)

// Whole-literal classes for the shell (see the note in components.go).
const (
	navLinkClass = "relative flex items-center gap-2 rounded-lg px-3 py-2 text-sm font-medium text-ink-2 no-underline hover:bg-surface-2 hover:text-ink hover:no-underline " +
		"aria-[current=page]:bg-surface-2 aria-[current=page]:text-ink " +
		"max-md:flex-col max-md:justify-center max-md:gap-0.5 max-md:rounded-none max-md:px-1 max-md:py-2 max-md:text-[0.6875rem] " +
		"max-md:aria-[current=page]:bg-transparent max-md:aria-[current=page]:text-accent"
	// navInlineClass hides a System link until the bar is wide enough for it.
	navInlineClass = "max-xl:hidden"
	menuPanelClass = "absolute right-0 top-full z-40 mt-2 w-60 rounded-xl border border-line bg-surface p-1.5 text-left shadow-xl " +
		"max-md:bottom-full max-md:top-auto max-md:mb-2"
	userPanelClass = "absolute right-0 top-full z-40 mt-2 w-64 rounded-xl border border-line bg-surface p-1.5 shadow-xl"
)

// closeOutside closes an open <details> menu when the page is clicked elsewhere.
const closeOutside = `el.removeAttribute('open')`

// navAlertsBadge is the Notifications nav link's error-count badge. It
// always renders the same wrapper element, present or empty, so an action
// handler can patch it in place after something that might change the count
// (see actionPoll) without needing a full page reload.
func navAlertsBadge(count int) g.Node {
	return Span(ID("nav-alerts"), g.If(count > 0, Badge("error", fmt.Sprint(count))))
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
		// The chart library and its <gv-chart> element are deferred: pages
		// without charts never wait on them, and the elements upgrade in place
		// when a Datastar patch inserts a chart later.
		Script(Defer(), Src("/static/echarts.min.js"+v)),
		Script(Defer(), Src("/static/charts.js"+v)),
	)
}

// initial is the avatar letter for the user menu.
func initial(email string) string {
	for _, r := range email {
		return strings.ToUpper(string(r))
	}
	return "?"
}

func navLink(it navItem, active string, extra ...g.Node) g.Node {
	return navLinkWith(it, active, navLinkClass, extra...)
}

func navLinkWith(it navItem, active, class string, extra ...g.Node) g.Node {
	return A(Href(it.href), Class(class),
		icon(it.icon, "size-4 max-md:size-5"), Span(g.Text(it.label)), g.Group(extra),
		g.If(it.key == active, g.Attr("aria-current", "page")))
}

// systemMenu is the "More" entry: a <details> dropdown with the less-used
// destinations. On phones it opens upward from the bottom bar.
func systemMenu(active string) g.Node {
	items := make([]g.Node, 0, len(navSystem))
	inSystem := false
	for _, it := range navSystem {
		if it.key == active {
			inSystem = true
		}
		items = append(items, A(append(comp("menuitem"), Href(it.href),
			icon(it.icon, "size-4 text-ink-2"), g.Text(it.label),
			g.If(it.key == active, g.Attr("aria-current", "page")))...))
	}
	return g.El("details", append(comp("menu"), Class("relative max-md:contents xl:hidden"),
		g.Attr("data-on:click__outside", closeOutside),
		g.El("summary", Class(navLinkClass), icon("more", "size-4 max-md:size-5"), Span(g.Text("System")),
			g.If(inSystem, g.Attr("aria-current", "page"))),
		Div(Class(menuPanelClass), g.Attr("role", "menu"), g.Group(items)),
	)...)
}

// userMenu holds the account actions: theme toggle and sign out. It is a
// compact <details>, so a long email can never wrap the nav.
func userMenu(user string) g.Node {
	return g.El("details", append(comp("menu"), Class("relative"),
		g.Attr("data-on:click__outside", closeOutside),
		g.El("summary", Class("flex items-center gap-2 rounded-full border border-line bg-surface py-1 pl-1 pr-2.5 text-sm hover:bg-surface-2 md:pr-2"),
			g.Attr("aria-label", "Account menu"),
			Span(Class("grid size-7 place-items-center rounded-full bg-accent text-xs font-bold text-accent-ink"), g.Text(initial(user))),
			icon("chevron", "size-4 text-ink-2 transition-transform"),
		),
		Div(Class(userPanelClass),
			Div(Class("px-2.5 pb-2 pt-1.5"),
				Div(Class("text-xs text-ink-2"), g.Text("Signed in as")),
				Div(Class("truncate text-sm font-semibold"), g.Attr("title", user), g.Text(user)),
			),
			Div(Class("my-1 h-px bg-line")),
			g.El("button", append(comp("menuitem"), Type("button"), g.Attr("data-on:click", themeToggle),
				Span(Class("icon-sun"), icon("sun", "size-4 text-ink-2")),
				Span(Class("icon-moon"), icon("moon", "size-4 text-ink-2")),
				g.Text("Toggle theme"))...),
			Form(Method("post"), Action("/logout"),
				g.El("button", append(comp("menuitem"), Type("submit"),
					icon("logout", "size-4 text-ink-2"), g.Text("Sign out"))...)),
		),
	)...)
}

// Page wraps body in the app shell with navigation.
func Page(pd PageData, body ...g.Node) g.Node {
	links := make([]g.Node, 0, len(navPrimary)+1)
	for _, it := range navPrimary {
		var extra []g.Node
		if it.key == "events" {
			extra = append(extra, navAlertsBadge(pd.Alerts))
		}
		links = append(links, navLink(it, pd.Active, extra...))
	}
	// Wide screens show the system pages inline; the dropdown only exists
	// where the bar has no room for them (tablet width and the phone tab bar).
	for _, it := range navSystem {
		links = append(links, navLinkWith(it, pd.Active, navLinkClass+" "+navInlineClass))
	}
	links = append(links, systemMenu(pd.Active))

	return g.Group([]g.Node{
		g.Raw("<!doctype html>"),
		HTML(Lang("en"),
			head(pd.Title, pd.Build),
			Body(Div(append(comp("shell"),
				g.If(pd.Demo, Div(append(comp("banner"), g.Text("Demo mode: all data is made up and nothing is sent to Strava."))...)),
				Nav(append(comp("nav"), Class("sticky top-0 z-30 border-b border-line bg-surface print:hidden"),
					Div(Class("mx-auto flex h-14 max-w-6xl items-center gap-2 px-4"),
						A(Href("/"), Class("mr-2 flex items-center gap-2 text-base font-bold tracking-tight text-ink no-underline hover:no-underline"),
							Img(Src("/static/favicon.svg"), Alt(""), Class("size-6")), g.Text("glucava")),
						Div(Class("flex items-center gap-1 max-md:fixed max-md:inset-x-0 max-md:bottom-0 max-md:z-30 max-md:grid max-md:grid-cols-5 max-md:gap-0 max-md:border-t max-md:border-line max-md:bg-surface max-md:pb-[env(safe-area-inset-bottom)]"),
							g.Attr("aria-label", "Main"), g.Group(links)),
						Span(Class("flex-1")),
						userMenu(pd.User),
					))...),
				Main(Class("mx-auto w-full max-w-6xl flex-1 px-4 pb-6 pt-6 max-md:pt-4"), g.Group(body)),
				sourceFooter(pd.Build),
				Div(ID("toast"), g.Attr("aria-live", "polite"),
					Class("pointer-events-none fixed bottom-4 right-4 z-50 flex max-w-[min(26rem,calc(100vw-2rem))] flex-col gap-2 max-md:bottom-20")),
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
			Body(g.Group([]g.Node{Div(append(comp("loginwrap"),
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
			)...), sourceFooter(build)})),
		),
	})
}

// SourceURL is where the running version's source code is offered. The AGPL
// (section 13) requires this for everyone who uses the software over a
// network, so it is on every page, including the sign-in page. A fork changes
// it to its own repository.
var SourceURL = "https://github.com/MrCodeEU/glucava"

func sourceFooter(build string) g.Node {
	return Div(append(comp("footer"),
		g.Text("glucava "+build+" · self-hosted, open source (AGPL-3.0) · "),
		A(Href(SourceURL), Rel("noopener"), g.Text("source code")),
	)...)
}
