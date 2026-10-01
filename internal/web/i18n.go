package web

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	g "maragu.dev/gomponents"
	. "maragu.dev/gomponents/html"

	"github.com/MrCodeEU/glucava/internal/i18n"
)

type trKey struct{}

// withTranslator is the middleware that picks the request's language once (the
// stored Settings choice, else the browser's Accept-Language, else English)
// and puts the Translator on the context. Every handler, including the SSE
// streams and the action endpoints that patch the page, reads it from there
// through s.tr(r), so live patches render in the same language as the page.
func (s *Server) withTranslator(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tr := i18n.Default().Match(r.Header.Get("Accept-Language"), s.languageSetting())
		ctx := context.WithValue(r.Context(), trKey{}, tr)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// tr returns the request's translator; English when the middleware did not run
// (for example a handler called directly in a test).
func (s *Server) tr(r *http.Request) *i18n.Translator {
	if tr, ok := r.Context().Value(trKey{}).(*i18n.Translator); ok {
		return tr
	}
	return i18n.English()
}

// langCache keeps the stored language for a few seconds so the middleware does
// not hit the database on every request (static files included).
const langCacheTTL = 5 * time.Second

type langCache struct {
	mu   sync.Mutex
	val  string
	at   time.Time
	have bool
}

// languageSetting is the installation-wide language: a locale tag, or "auto".
func (s *Server) languageSetting() string {
	if s.Store == nil {
		return i18n.Auto
	}
	c := &s.langc
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.have && time.Since(c.at) < langCacheTTL {
		return c.val
	}
	cfg, err := s.Store.LoadConfig()
	if err != nil {
		slog.Error("load language setting", "err", err)
		return i18n.Auto
	}
	c.val, c.at, c.have = cfg.Language, time.Now(), true
	return c.val
}

// forgetLanguage drops the cached setting after Settings were saved, so the
// very next request (the page reload that follows a save) already uses it.
func (s *Server) forgetLanguage() {
	s.langc.mu.Lock()
	s.langc.have = false
	s.langc.mu.Unlock()
}

// t is the gomponents helper for a translated text node:
//
//	t(pd, "nav.settings")
//	t(pd, "footer.text", "build", pd.Build)
//
// Keys must be string literals (make i18n-check verifies them).
func t(pd PageData, key string, args ...any) g.Node {
	return g.Text(pd.translator().T(key, args...)) // i18n:dynamic (the helper itself)
}

// tn is t for plural keys; the count is available as {n}. No converted page
// has a plural yet; the conversion of the remaining pages uses it.
//
//nolint:unused
func tn(pd PageData, key string, n int, args ...any) g.Node {
	return g.Text(pd.translator().Tn(key, n, args...)) // i18n:dynamic (the helper itself)
}

// languageSignal is the Settings form's initial value: "auto" when unset.
func languageSignal(stored string) string {
	if stored == "" {
		return i18n.Auto
	}
	return stored
}

// languageField is the Settings "Language" select: Automatic plus every
// installed locale by its own name (auto-discovered, so a new locale file
// appears here without any Go change).
func languageField(pd PageData) g.Node {
	tr := pd.translator()
	opts := []g.Node{Option(Value(i18n.Auto), g.Text(tr.T("language.auto")))}
	for _, l := range i18n.Default().Locales() {
		opts = append(opts, Option(Value(l.Tag), g.Text(l.NativeName)))
	}
	return Field("language", tr.T("settings.language.label"), tr.T("settings.language.help"),
		Select(ID("language"), g.Attr("data-bind", "language"), g.Group(opts)))
}

// translator returns pd's translator, English when unset (PageData built by
// hand in a test or an old call site).
func (pd PageData) translator() *i18n.Translator {
	if pd.T != nil {
		return pd.T
	}
	return i18n.English()
}
