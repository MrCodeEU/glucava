package web

import (
	"regexp"
	"strings"
	"testing"

	g "maragu.dev/gomponents"
	. "maragu.dev/gomponents/html"

	"github.com/MrCodeEU/glucava/internal/i18n"
)

var (
	tagRe   = regexp.MustCompile(`<[a-zA-Z][^<>]*>`)
	classRe = regexp.MustCompile(`\sclass=`)
)

// TestNoDuplicateClass: an element with both comp() classes and Class(...)
// renders two class attributes and the browser drops the second.
func TestNoDuplicateClass(t *testing.T) {
	t.Parallel()
	pd := PageData{Title: "t", Active: "stats", User: "a@example.test", Build: "x", Alerts: 2}
	nodes := map[string]g.Node{
		"page": Page(pd,
			PageHead("T", "sub", Btn("primary", "Go")),
			Grid("2", StatTile("A", "1", "s", &Delta{Text: "+1", Dir: "up", Tone: "good"}), Tile("B", "2", "")),
			Segmented("Range", []NavItem{{"a", "A", "/a"}, {"b", "B", "/b"}}, "a"),
			Tabs("View", []NavItem{{"a", "A", "/a"}}, "a"),
			EmptyState("inbox", "Nothing", "hint", Btn("", "Do")),
			ErrorState("Broke", "detail"), Skeleton("h-4 w-10"), HelpTip("tip"),
			ConfirmDialog("d", "danger", "Delete", "Sure?", "body"),
			Badge("done", "ok"), Notice("error", g.Text("x")), Toast("ok", "hi"),
		),
		"login": LoginPage(i18n.English(), "x", "bad", "a@example.test"),
	}
	for name, n := range nodes {
		html := renderString(n)
		for _, tag := range tagRe.FindAllString(html, -1) {
			if len(classRe.FindAllString(tag, -1)) > 1 {
				t.Errorf("%s: two class attributes on %s", name, tag)
			}
		}
		if !strings.Contains(html, "data-component=") {
			t.Errorf("%s: no components rendered", name)
		}
	}
}

// TestShellKeepsHooks: handlers patch these by id and the tests rely on the variants.
func TestShellKeepsHooks(t *testing.T) {
	t.Parallel()
	html := renderString(Page(PageData{Title: "t", User: "a@example.test", Build: "x", Alerts: 3}, P(g.Text("x"))))
	for _, want := range []string{`id="nav-alerts"`, `id="toast"`, `data-variant="error"`, `/static/echarts.min.js?v=x`, `/static/charts.js?v=x`, `action="/logout"`, "Sign out", "a@example.test"} {
		if !strings.Contains(html, want) {
			t.Errorf("page lacks %s", want)
		}
	}
}
