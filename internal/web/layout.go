package web

import (
	"fmt"
	"strings"

	g "maragu.dev/gomponents"
	. "maragu.dev/gomponents/html"

	"github.com/MrCodeEU/glucava/internal/i18n"
)

// PageData is what the layout needs for every page.
type PageData struct {
	Title  string
	Active string // nav key: dashboard, strava, settings, tokens, events
	User   string
	Build  string
	Demo   bool
	Alerts int // recent error events, shown as a badge on Notifications

	// T translates for this request; Lang is its locale tag, used for
	// <html lang>. Both are filled by Server.page. A zero PageData renders
	// English. Use the t/tn helpers (i18n.go) rather than T directly in pages.
	T    *i18n.Translator
	Lang string
}

// navItem is one top-level destination. Primary ones sit in the bar (bottom
// tab bar on phones); the rest go in the "System" menu, which keeps the bar
// short enough to fit five tabs on a 390 px screen. label is a translation key.
type navItem struct{ key, href, label, icon string }

var (
	navPrimary = []navItem{
		{"dashboard", "/", i18n.Key("nav.activities"), "activity"},
		{"stats", "/stats", i18n.Key("nav.overview"), "chart"},
		{"events", "/events", i18n.Key("nav.notifications"), "bell"},
		{"settings", "/settings", i18n.Key("nav.settings"), "sliders"},
	}
	navSystem = []navItem{
		{"strava", "/strava", i18n.Key("nav.strava"), "link"},
		{"tokens", "/tokens", i18n.Key("nav.triggers"), "zap"},
		{"logs", "/logs", i18n.Key("nav.logs"), "scroll"},
	}
)

// Whole-literal classes for the shell (see the note in components.go).
const (
	navLinkClass = "relative flex items-center gap-2 rounded-lg px-3 py-2 text-sm font-medium text-ink-2 no-underline hover:bg-surface-2 hover:text-ink hover:no-underline " +
		"aria-[current=page]:bg-surface-2 aria-[current=page]:text-ink " +
		"max-md:flex-col max-md:justify-center max-md:gap-0.5 max-md:rounded-none max-md:px-1 max-md:py-2 max-md:text-[0.6875rem] " +
		"max-md:aria-[current=page]:bg-transparent max-md:aria-[current=page]:text-accent max-md:min-w-0"
	// navInlineClass hides a System link until the bar is wide enough for it.
	navInlineClass = "max-[1180px]:hidden"
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
	// title is already translated by the caller; " · glucava" is the brand.
	v := "?v=" + build
	return Head(
		Meta(Charset("utf-8")),
		Meta(Name("viewport"), Content("width=device-width, initial-scale=1")),
		Meta(Name("robots"), Content("noindex")),
		TitleEl(g.Text(title+" · glucava")),
		Link(Rel("icon"), Type("image/svg+xml"), Href("/static/favicon.svg")),
		Link(Rel("stylesheet"), Href("/static/app.css"+v)),
		pwaHead(v),
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

func navLink(tr *i18n.Translator, it navItem, active string, extra ...g.Node) g.Node {
	return navLinkWith(tr, it, active, navLinkClass, extra...)
}

func navLinkWith(tr *i18n.Translator, it navItem, active, class string, extra ...g.Node) g.Node {
	return A(Href(it.href), Class(class),
		icon(it.icon, "size-4 max-md:size-5"), Span(Class("max-md:max-w-full max-md:truncate"), g.Text(tr.T(it.label))), g.Group(extra), // i18n:dynamic (nav tables use i18n.Key)
		g.If(it.key == active, g.Attr("aria-current", "page")))
}

// systemMenu is the "More" entry: a <details> dropdown with the less-used
// destinations. On phones it opens upward from the bottom bar.
func systemMenu(tr *i18n.Translator, active string) g.Node {
	items := make([]g.Node, 0, len(navSystem))
	inSystem := false
	for _, it := range navSystem {
		if it.key == active {
			inSystem = true
		}
		items = append(items, A(append(comp("menuitem"), Href(it.href),
			icon(it.icon, "size-4 text-ink-2"), g.Text(tr.T(it.label)), // i18n:dynamic (nav tables use i18n.Key)
			g.If(it.key == active, g.Attr("aria-current", "page")))...))
	}
	return g.El("details", append(comp("menu"), Class("relative max-md:contents min-[1180px]:hidden"),
		g.Attr("data-on:click__outside", closeOutside),
		g.El("summary", Class(navLinkClass), icon("more", "size-4 max-md:size-5"), Span(g.Text(tr.T("nav.system"))),
			g.If(inSystem, g.Attr("aria-current", "page"))),
		Div(Class(menuPanelClass), g.Attr("role", "menu"), g.Group(items)),
	)...)
}

// userMenu holds the account actions: theme toggle and sign out. It is a
// compact <details>, so a long email can never wrap the nav.
func userMenu(tr *i18n.Translator, user string) g.Node {
	return g.El("details", append(comp("menu"), Class("relative"),
		g.Attr("data-on:click__outside", closeOutside),
		g.El("summary", Class("flex items-center gap-2 rounded-full border border-line bg-surface py-1 pl-1 pr-2.5 text-sm hover:bg-surface-2 md:pr-2"),
			g.Attr("aria-label", tr.T("nav.account")),
			Span(Class("grid size-7 place-items-center rounded-full bg-accent text-xs font-bold text-accent-ink"), g.Text(initial(user))),
			icon("chevron", "size-4 text-ink-2 transition-transform"),
		),
		Div(Class(userPanelClass),
			Div(Class("px-2.5 pb-2 pt-1.5"),
				Div(Class("text-xs text-ink-2"), g.Text(tr.T("menu.signed_in_as"))),
				Div(Class("truncate text-sm font-semibold"), g.Attr("title", user), g.Text(user)),
			),
			Div(Class("my-1 h-px bg-line")),
			g.El("button", append(comp("menuitem"), Type("button"), g.Attr("data-on:click", themeToggle),
				Span(Class("icon-sun"), icon("sun", "size-4 text-ink-2")),
				Span(Class("icon-moon"), icon("moon", "size-4 text-ink-2")),
				g.Text(tr.T("menu.toggle_theme")))...),
			Form(Method("post"), Action("/logout"),
				g.El("button", append(comp("menuitem"), Type("submit"),
					icon("logout", "size-4 text-ink-2"), g.Text(tr.T("menu.sign_out")))...)),
		),
	)...)
}

// Page wraps body in the app shell with navigation.
func Page(pd PageData, body ...g.Node) g.Node {
	tr := pd.translator()
	links := make([]g.Node, 0, len(navPrimary)+1)
	for _, it := range navPrimary {
		var extra []g.Node
		if it.key == "events" {
			extra = append(extra, navAlertsBadge(pd.Alerts))
		}
		links = append(links, navLink(tr, it, pd.Active, extra...))
	}
	// Wide screens show the system pages inline; the dropdown only exists
	// where the bar has no room for them (tablet width and the phone tab bar).
	for _, it := range navSystem {
		links = append(links, navLinkWith(tr, it, pd.Active, navLinkClass+" "+navInlineClass))
	}
	links = append(links, systemMenu(tr, pd.Active))

	return g.Group([]g.Node{
		g.Raw("<!doctype html>"),
		HTML(Lang(tr.Lang()),
			head(pd.Title, pd.Build),
			Body(Div(append(comp("shell"),
				g.If(pd.Demo, Div(append(comp("banner"), t(pd, "banner.demo"))...)),
				Nav(append(comp("nav"), Class("sticky top-0 z-30 border-b border-line bg-surface print:hidden"),
					Div(Class("mx-auto flex h-14 max-w-6xl items-center gap-2 px-4"),
						A(Href("/"), Class("mr-2 flex items-center gap-2 text-base font-bold tracking-tight text-ink no-underline hover:no-underline"),
							Img(Src("/static/favicon.svg"), Alt(""), Class("size-6")), g.Text("glucava")),
						Div(Class("flex items-center gap-1 max-md:fixed max-md:inset-x-0 max-md:bottom-0 max-md:z-30 max-md:grid max-md:grid-cols-5 max-md:gap-0 max-md:border-t max-md:border-line max-md:bg-surface max-md:pb-[env(safe-area-inset-bottom)]"),
							g.Attr("aria-label", tr.T("nav.main")), g.Group(links)),
						Span(Class("flex-1")),
						userMenu(tr, pd.User),
					))...),
				Main(Class("mx-auto w-full max-w-6xl flex-1 px-4 pb-6 pt-6 max-md:pt-4"), g.Group(body)),
				sourceFooter(tr, pd.Build),
				Div(ID("toast"), g.Attr("aria-live", "polite"),
					Class("pointer-events-none fixed bottom-4 right-4 z-50 flex max-w-[min(26rem,calc(100vw-2rem))] flex-col gap-2 max-md:bottom-20")),
			)...)),
		),
	})
}

// LoginPage is the sign-in form. It is a plain HTML form, so it works without JavaScript.
// email is redisplayed after a failed attempt so a mistyped password does not
// also cost the address; the password field is always left blank.
// errMsg is already translated by the caller.
func LoginPage(tr *i18n.Translator, build, errMsg, email string) g.Node {
	return g.Group([]g.Node{
		g.Raw("<!doctype html>"),
		HTML(Lang(tr.Lang()),
			head(tr.T("login.title"), build),
			Body(g.Group([]g.Node{Div(append(comp("loginwrap"),
				Card(
					H1(g.Text("glucava")),
					P(Class("muted"), g.Text(tr.T("login.lead"))),
					g.If(errMsg != "", Notice("error", g.Text(errMsg))),
					Form(Method("post"), Action("/login"),
						Field("email", tr.T("login.email"), "", Input(ID("email"), Name("email"), Type("email"), Value(email), Required(), AutoComplete("username"), AutoFocus())),
						Field("password", tr.T("login.password"), "", Input(ID("password"), Name("password"), Type("password"), Required(), AutoComplete("current-password"))),
						SubmitBtn("primary", "", tr.T("login.submit")),
					),
				),
			)...), sourceFooter(tr, build)})),
		),
	})
}

// SourceURL is where the running version's source code is offered. The AGPL
// (section 13) requires this for everyone who uses the software over a
// network, so it is on every page, including the sign-in page. A fork changes
// it to its own repository.
var SourceURL = "https://github.com/MrCodeEU/glucava"

func sourceFooter(tr *i18n.Translator, build string) g.Node {
	return Div(append(comp("footer"),
		g.Text(tr.T("footer.text", "build", build)+" · "),
		A(Href(SourceURL), Rel("noopener"), g.Text(tr.T("footer.source"))),
	)...)
}
