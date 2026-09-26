package web

import (
	"context"
	"embed"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
	g "maragu.dev/gomponents"

	"github.com/MrCodeEU/glucava/internal/bus"
	"github.com/MrCodeEU/glucava/internal/clientip"
	"github.com/MrCodeEU/glucava/internal/jobs"
	"github.com/MrCodeEU/glucava/internal/secrets"
	"github.com/MrCodeEU/glucava/internal/stats"
	"github.com/MrCodeEU/glucava/internal/store"
	"github.com/MrCodeEU/glucava/internal/tokens"
	"github.com/MrCodeEU/glucava/internal/trigger"
)

//go:embed static
var staticFS embed.FS

// Enqueuer accepts jobs. *jobs.Queue satisfies it.
type Enqueuer interface {
	Enqueue(j jobs.Job) (bool, error)
}

// SessionChecker tests whether Strava accepts the stored cookies.
type SessionChecker interface {
	CheckSession(ctx context.Context) error
}

const authCookie = "gv_auth"

// Server is the web UI.
type Server struct {
	App    core.App
	Store  *store.PB
	Vault  *secrets.Vault
	Tokens *tokens.Manager
	Jobs   Enqueuer
	Signal *trigger.Signal
	Bus    *bus.Bus

	// Proxies names the reverse proxies whose X-Forwarded-* headers are believed.
	// Nil trusts none, so limits and cookies use the direct peer.
	Proxies *clientip.Resolver

	Session       SessionChecker                                                     // optional
	StravaLogin   func(ctx context.Context, email, password string) error            // optional; experimental
	GlucoseTest   func(ctx context.Context) error                                    // optional
	Poll          func(ctx context.Context) (int, error)                             // runs a Strava check inline; optional
	LatestGlucose func(ctx context.Context) (*stats.Sample, error)                   // optional
	FindActivity  func(ctx context.Context, stravaID string) (*jobs.Activity, error) // looks up an activity the poller never queued; optional
	SendTest      func(ctx context.Context) error                                    // sends a test notification; optional
	SourceName    string                                                             // key of stored glucose samples, e.g. "dexcom"
	Build         string
	Demo          bool
	Loc           *time.Location // display zone; default time.Local
	Now           func() time.Time

	mu        sync.Mutex
	lastCheck struct {
		at  time.Time
		ok  bool
		err string
	}
	failures map[string]*loginBucket
}

func (s *Server) loc() *time.Location {
	if s.Loc != nil {
		return s.Loc
	}
	return time.Local
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Server) page(r *http.Request, title, active string) PageData {
	email, _ := s.user(r)
	n, err := s.Store.CountRecentErrors(r.Context(), s.now().Add(-24*time.Hour))
	if err != nil {
		log.Printf("web: count recent errors: %v", err)
	}
	return PageData{Title: title, Active: active, User: email, Build: s.Build, Demo: s.Demo, Alerts: n}
}

// Handler returns the UI routes. Mount it on the paths in Paths.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	sub, _ := fs.Sub(staticFS, "static")
	files := http.FileServerFS(sub)
	mux.Handle("GET /static/", http.StripPrefix("/static/", s.cache(files)))

	mux.HandleFunc("GET /login", s.loginPage)
	mux.HandleFunc("POST /login", s.login)
	mux.HandleFunc("POST /logout", s.logout)

	page := func(pattern string, h http.HandlerFunc) { mux.Handle(pattern, s.auth(h)) }
	page("GET /{$}", s.dashboard)
	page("GET /activity/{id}", s.activity)
	page("GET /chart/{name}", s.chartImage)
	page("GET /strava", s.stravaPage)
	page("GET /settings", s.settingsPage)
	page("GET /tokens", s.tokensPage)
	page("GET /events", s.eventsPage)
	page("GET /stream/live", s.streamLive)
	page("GET /stream/activity/{id}", s.streamActivity)

	page("POST /actions/poll", s.actionPoll)
	page("POST /actions/reprocess/{id}", s.actionReprocess)
	page("POST /actions/chart/{id}", s.actionChartAgain)
	page("POST /actions/process", s.actionProcessActivity)
	page("POST /actions/restore/{id}", s.actionRestore)
	page("POST /actions/settings", s.actionSettings)
	page("POST /actions/account", s.actionAccount)
	page("POST /actions/notify/test", s.actionNotifyTest)
	page("POST /actions/strava/cookies", s.actionStravaCookies)
	page("POST /actions/strava/test", s.actionStravaTest)
	page("POST /actions/strava/login", s.actionStravaLogin)
	page("POST /actions/dexcom/test", s.actionDexcomTest)
	page("POST /actions/tokens/create", s.actionTokenCreate)
	page("POST /actions/tokens/revoke/{name}", s.actionTokenRevoke)
	page("POST /actions/data/purge", s.actionPurge)
	page("POST "+importRoute, s.actionGlucoseImport)
	page("GET /export/samples.csv", s.exportSamples)
	page("GET /export/activities.csv", s.exportActivities)

	return secure(mux)
}

// Routes lists the PocketBase route patterns that forward to Handler. PocketBase
// keeps /api, /_ and /health for itself, so the UI claims only its own paths.
var Routes = []string{
	"/{$}", "/login", "/logout", "/activity/{id}", "/strava", "/settings", "/tokens", "/events",
	"/chart/{path...}", "/stream/{path...}", "/actions/{path...}", "/export/{path...}", "/static/{path...}",
}

// maxBody caps most request bodies. The largest legitimate one otherwise is a
// pasted cookie export.
const maxBody = 1 << 20

// maxImportBody is the cap for the glucose import route only: a CGM export
// covering months or years of readings is legitimately much larger than
// anything else this UI accepts.
const maxImportBody = 32 << 20

const importRoute = "/actions/glucose/import"

// csp restricts what pages may load. Datastar evaluates expressions with the
// Function constructor, which needs 'unsafe-eval'; styles need 'unsafe-inline'
// for the width of the time-in-range bars. Nothing else is allowed inline.
const csp = "default-src 'self'; script-src 'self' 'unsafe-eval'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"

// secure adds hardening headers and limits request bodies.
func secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Content-Security-Policy", csp)
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if r.Body != nil {
			limit := int64(maxBody)
			if r.URL.Path == importRoute {
				limit = maxImportBody
			}
			r.Body = http.MaxBytesReader(w, r.Body, limit)
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) cache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Build == "dev" {
			w.Header().Set("Cache-Control", "no-cache")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=86400")
		}
		next.ServeHTTP(w, r)
	})
}

// auth requires a signed-in user and, for POST, a same-origin request.
func (s *Server) auth(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.user(r); !ok {
			if r.Header.Get("Datastar-Request") != "" {
				http.Error(w, "signed out", http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		if r.Method == http.MethodPost && !sameOrigin(r) {
			http.Error(w, "cross-site request refused", http.StatusForbidden)
			return
		}
		next(w, r)
	})
}

// sameOrigin rejects cross-site POSTs. Browsers send Origin on POST, and
// Sec-Fetch-Site on all modern versions; a request without either is not a
// browser form post and cannot ride on the user's cookie.
func sameOrigin(r *http.Request) bool {
	if o := r.Header.Get("Origin"); o != "" {
		u, err := url.Parse(o)
		return err == nil && u.Host == r.Host
	}
	switch r.Header.Get("Sec-Fetch-Site") {
	case "", "same-origin", "none":
		return true
	}
	return false
}

// baseURL is the address the browser used, for the trigger examples.
func (s *Server) baseURL(r *http.Request) string {
	scheme := "http"
	if s.Proxies.Secure(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func renderString(n g.Node) string {
	var b strings.Builder
	_ = n.Render(&b)
	return b.String()
}
