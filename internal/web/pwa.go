package web

import (
	"encoding/json"
	"html"
	"io/fs"
	"net/http"
	"strings"

	g "maragu.dev/gomponents"
	. "maragu.dev/gomponents/html"
)

// Installable-app support: the web manifest, the service worker and the
// few <head> tags that point at them. The service worker caches only static
// files and delivers push messages; no page or data is ever stored on the
// device (see static/sw.js).

// Brand colours for the manifest and the browser chrome (theme-color); they
// match --bg in static/input.css.
const (
	themeLight = "#f5f6f8"
	themeDark  = "#0d1014"
)

// precacheAssets is the static app shell the service worker caches. The
// versioned URLs are exactly those layout.go's head() links, so a cached
// copy answers the page's own requests.
func precacheAssets(build string) []string {
	v := "?v=" + build
	return []string{
		"/static/app.css" + v, "/static/datastar.js" + v, "/static/echarts.min.js" + v, "/static/charts.js" + v, "/static/pwa.js" + v,
		"/static/theme.js", "/static/favicon.svg", "/static/icon-192.png", "/static/badge-96.png",
	}
}

// pwaRoutes adds the public PWA endpoints: neither the manifest nor the
// service worker script is fetched with the session cookie by every browser,
// and neither contains anything private.
func (s *Server) pwaRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /manifest.webmanifest", s.manifest)
	mux.HandleFunc("GET /sw.js", s.serviceWorker)
}

func (s *Server) manifest(w http.ResponseWriter, r *http.Request) {
	tr := s.tr(r)
	m := map[string]any{
		"name": "glucava", "short_name": "glucava", "lang": tr.Lang(),
		"description": tr.T("pwa.description"),
		"id":          "/", "start_url": "/", "scope": "/",
		"display": "standalone", "orientation": "any",
		"background_color": themeLight, "theme_color": themeLight,
		"icons": []map[string]string{
			{"src": "/static/icon-192.png", "sizes": "192x192", "type": "image/png", "purpose": "any"},
			{"src": "/static/icon-512.png", "sizes": "512x512", "type": "image/png", "purpose": "any"},
			{"src": "/static/icon-maskable-512.png", "sizes": "512x512", "type": "image/png", "purpose": "maskable"},
		},
	}
	b, _ := json.Marshal(m)
	w.Header().Set("Content-Type", "application/manifest+json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Add("Vary", "Accept-Language")
	_, _ = w.Write(b)
}

// serviceWorker serves static/sw.js at the root scope, with the build and the
// precache list and the offline page's text (in the request's language)
// filled in. It is never cached by the browser's HTTP cache, so an updated
// worker is picked up on the next visit.
func (s *Server) serviceWorker(w http.ResponseWriter, r *http.Request) {
	tr := s.tr(r)
	src, err := fs.ReadFile(staticFS, "static/sw.js")
	if err != nil {
		http.Error(w, "service worker missing", http.StatusInternalServerError)
		return
	}
	assets, _ := json.Marshal(precacheAssets(s.Build))
	build, _ := json.Marshal(s.Build)
	// The offline page is HTML inside a JS string: escape for HTML first, then
	// quote for JS (json.Marshal also escapes < and > again).
	quote := func(s string) string { b, _ := json.Marshal(html.EscapeString(s)); return string(b) }
	js := strings.NewReplacer("__BUILD__", string(build), "__ASSETS__", string(assets),
		"__LANG__", quote(tr.Lang()),
		"__OFFLINE_TITLE__", quote(tr.T("pwa.offline.title")), "__OFFLINE_BODY__", quote(tr.T("pwa.offline.body"))).Replace(string(src))
	w.Header().Add("Vary", "Accept-Language")
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Service-Worker-Allowed", "/")
	_, _ = w.Write([]byte(js))
}

// pwaHead is the <head> part of the installable app: manifest, theme colour,
// home-screen icon, and the script that registers the service worker.
func pwaHead(v string) g.Node {
	return g.Group([]g.Node{
		Link(Rel("manifest"), Href("/manifest.webmanifest")),
		Meta(Name("theme-color"), Content(themeLight), g.Attr("media", "(prefers-color-scheme: light)")),
		Meta(Name("theme-color"), Content(themeDark), g.Attr("media", "(prefers-color-scheme: dark)")),
		Link(Rel("apple-touch-icon"), Href("/static/apple-touch-icon.png")),
		Meta(Name("apple-mobile-web-app-title"), Content("glucava")),
		Script(Defer(), Src("/static/pwa.js"+v)),
	})
}
