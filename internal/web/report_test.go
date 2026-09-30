package web

import (
	"bytes"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/MrCodeEU/glucava/internal/report"
)

func TestReportNeedsLogin(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	if w := e.get(t, "/export/report.pdf?range=7d", nil); w.Code == http.StatusOK {
		t.Errorf("anonymous report = %d", w.Code)
	}
}

// Not parallel: it sets an environment variable.
func TestReportWithoutTypstIs503(t *testing.T) {
	t.Setenv(report.EnvTypst, "/nonexistent/typst")
	e := newEnv(t)
	c := e.login(t)
	w := e.get(t, "/export/report.pdf?range=7d", c)
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "typst") {
		t.Errorf("code=%d body=%q", w.Code, w.Body.String())
	}
}

var reportName = regexp.MustCompile(`^attachment; filename="glucava-report-\d{4}-\d{2}-\d{2}_\d{4}-\d{2}-\d{2}\.pdf"$`)

func TestReportPDFDownloadAndCache(t *testing.T) {
	if _, err := report.FindTypst(); err != nil {
		t.Skip("typst not available: " + err.Error())
	}
	t.Parallel()
	e := newEnv(t)
	seedOverview(t, e)
	c := e.login(t)

	w := e.get(t, "/export/report.pdf?range=30d&compare=prev", c)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body %q", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/pdf" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cd := w.Header().Get("Content-Disposition"); !reportName.MatchString(cd) {
		t.Errorf("Content-Disposition = %q", cd)
	}
	if !bytes.HasPrefix(w.Body.Bytes(), []byte("%PDF-")) || w.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("not a PDF download: %q", w.Body.Bytes()[:8])
	}
	first := w.Body.Bytes()

	// A second request for the same window and data is answered from the cache.
	again := e.get(t, "/export/report.pdf?range=30d&compare=prev", c)
	if again.Code != http.StatusOK || !bytes.Equal(first, again.Body.Bytes()) {
		t.Errorf("second request differs (code %d)", again.Code)
	}

	// A bad custom range falls back to the default instead of failing.
	if bad := e.get(t, "/export/report.pdf?from=2026-13-01&to=x", c); bad.Code != http.StatusOK {
		t.Errorf("bad custom range = %d", bad.Code)
	}
}

func TestOverviewHasReportButton(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)
	body := e.get(t, "/stats?range=14d&compare=prev", c).Body.String()
	if !strings.Contains(body, `href="/export/report.pdf?compare=prev&amp;range=14d"`) {
		t.Error("Overview has no report link for its range")
	}
}
