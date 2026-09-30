package notify

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"net/mail"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/tools/mailer"

	"github.com/MrCodeEU/glucava/internal/i18n"
)

var (
	trEN = func() *i18n.Translator { return i18n.English() }
	trDE = func() *i18n.Translator { return i18n.Default().For("de") }
)

// noKeyLeak fails when a translation key name ends up in user-visible text.
func noKeyLeak(t *testing.T, what, s string) {
	t.Helper()
	for _, p := range []string{"notify.", "email.", "event.", "report."} {
		if strings.Contains(s, p) {
			t.Errorf("%s leaks a key name (%q): %s", what, p, s)
		}
	}
}

func sendEmail(t *testing.T, tr Translator, m Message) *mailer.Message {
	t.Helper()
	var got *mailer.Message
	e := &Email{
		SendFunc: func(msg *mailer.Message) error { got = msg; return nil },
		From:     mail.Address{Address: "glucava@example.com"}, To: "me@example.com", Tr: tr,
	}
	if err := e.Send(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestEmailFixedTextsFollowTheTranslator(t *testing.T) {
	m := Message{
		Type: "strava_failed", Severity: "error", StravaID: "42", Repaired: true, Time: t0,
		Title: "t", Body: "b", Link: "https://g.example/activity/42", LinkLabel: "l",
	}
	cases := []struct {
		name string
		tr   Translator
		lang string
		want []string
	}{
		{"en", trEN, `lang="en"`, []string{">error<", "Open activity 42 on Strava", "The selector was repaired automatically"}},
		{"nil is english", nil, `lang="en"`, []string{">error<", "Open activity 42 on Strava"}},
		{"de", trDE, `lang="de"`, []string{">Fehler<", "Aktivität 42 auf Strava öffnen", "automatisch repariert"}},
	}
	for _, c := range cases {
		msg := sendEmail(t, c.tr, m)
		for _, w := range append([]string{c.lang}, c.want...) {
			if !strings.Contains(msg.HTML, w) {
				t.Errorf("%s: HTML lacks %q", c.name, w)
			}
		}
		noKeyLeak(t, c.name+" html", msg.HTML)
		noKeyLeak(t, c.name+" text", msg.Text)
	}
	de := sendEmail(t, trDE, m)
	if !strings.Contains(de.Text, "automatisch repariert") || strings.Contains(de.Text, "The selector") {
		t.Errorf("German text part = %q", de.Text)
	}
}

func TestTitleAndLinkLabelsAreTranslated(t *testing.T) {
	de := trDE()
	for typ := range titles {
		got := Title(de, typ)
		noKeyLeak(t, "title "+typ, got)
		if got == Title(i18n.English(), typ) && typ != "test" {
			t.Errorf("title for %s is not translated: %q", typ, got)
		}
	}
	if Title(de, "strava_failed") != "Strava-Update fehlgeschlagen" || Title(de, "brand_new") != "brand_new" {
		t.Errorf("titles: %q / %q", Title(de, "strava_failed"), Title(de, "brand_new"))
	}
	_, label := LinkFor(de, "https://g.example", Message{Type: "session_expired"})
	if label != "Strava-Verbindung prüfen" {
		t.Errorf("label = %q", label)
	}
}

func TestDispatcherUsesInstallationLanguageAndEventKeys(t *testing.T) {
	ch := &fakeChannel{}
	e := ev("1", "glucose_gap", "")
	e.Message = "no glucose reading for 2h 05m (the last one was at Wed 30 Sep, 07:00)"
	e.MsgKey = "event.gap"
	e.MsgArgs = map[string]any{"age_min": float64(125), "last_at": "2026-09-30T05:00:00Z"} // float64: as decoded from JSON
	d, _, _ := newDispatcher([]OutboxEvent{e}, ch)
	lang := "de"
	d.Tr = func() *i18n.Translator { return i18n.Default().Match("", lang) }
	d.Loc = func() *time.Location { return time.UTC }
	if err := d.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := ch.sent[0]
	if got.Title != "Keine Glukosewerte" || got.Body != "Seit 2 h 05 min kein Glukosewert (der letzte war am Mi, 30. Sep, 05:00)." {
		t.Errorf("german message = %q / %q", got.Title, got.Body)
	}
	noKeyLeak(t, "body", got.Body)

	// "auto" (and an unknown tag) is English; an old row without a key keeps its stored text.
	lang = "auto"
	e2 := ev("2", "glucose_gap", "")
	e2.Message = "stored english"
	d2, _, _ := newDispatcher([]OutboxEvent{e, e2}, ch)
	d2.Tr, d2.Loc = d.Tr, d.Loc
	ch.sent = nil
	if err := d2.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ch.sent[0].Title != "No glucose readings" || ch.sent[0].Body != "No glucose reading for 2h 05m (the last one was at Wed 30 Sep, 05:00)." {
		t.Errorf("english message = %q / %q", ch.sent[0].Title, ch.sent[0].Body)
	}
}

func TestDispatcherWithoutTranslatorIsEnglish(t *testing.T) {
	ch := &fakeChannel{}
	d, _, _ := newDispatcher([]OutboxEvent{ev("1", "session_expired", "")}, ch)
	if err := d.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ch.sent[0].Title != "Strava session expired" {
		t.Errorf("title = %q", ch.sent[0].Title)
	}
}

// hardCodedText is what a hard-coded English sentence looks like.
var hardCodedText = regexp.MustCompile(`^[A-Z][a-z]+ [a-z]`)

// textFields are the composite-literal fields that reach a reader.
var textFields = map[string]bool{"Label": true, "Title": true, "Body": true, "ChartAlt": true, "LinkLabel": true, "Subject": true}

// TestNoHardCodedEnglishInConvertedPackages keeps the notification texts in
// the locale files: a Label/Title/Body/ChartAlt set from an English string
// literal in these packages fails the test.
func TestNoHardCodedEnglishInConvertedPackages(t *testing.T) {
	fset := token.NewFileSet()
	for _, dir := range []string{".", "../digest", "../report"} {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range files {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(fset, name, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			ast.Inspect(f, func(n ast.Node) bool {
				kv, ok := n.(*ast.KeyValueExpr)
				if !ok {
					return true
				}
				key, ok := kv.Key.(*ast.Ident)
				lit, isLit := kv.Value.(*ast.BasicLit)
				if !ok || !isLit || lit.Kind != token.STRING || !textFields[key.Name] {
					return true
				}
				if s, err := strconv.Unquote(lit.Value); err == nil && hardCodedText.MatchString(s) {
					t.Errorf("%s: hard-coded text %s; add a key to en.json and use tr.T", fset.Position(lit.Pos()), lit.Value)
				}
				return true
			})
		}
	}
}
