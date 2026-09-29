package metrics

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/MrCodeEU/glucava/internal/stats"
)

// newTestRegistry returns a fresh registry (not the package's shared default
// one) with c registered, so GlucoseCollector tests don't collide with
// metrics other tests in this package have already touched.
func newTestRegistry(t *testing.T, c prometheus.Collector) *prometheus.Registry {
	t.Helper()
	reg := prometheus.NewRegistry()
	if err := reg.Register(c); err != nil {
		t.Fatalf("register collector: %v", err)
	}
	return reg
}

func scrape(t *testing.T, reg *prometheus.Registry) string {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	promhttp.HandlerFor(reg, promhttp.HandlerOpts{}).ServeHTTP(rr, req)
	return rr.Body.String()
}

func TestHandlerServesKnownMetrics(t *testing.T) {
	JobsProcessedTotal.WithLabelValues("success").Inc()
	IngestReadingsStoredTotal.Add(3)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	for _, want := range []string{
		"glucava_jobs_processed_total",
		"glucava_ingest_readings_stored_total",
		"glucava_jobs_queue_depth",
		"glucava_ingest_last_success_timestamp_seconds",
		"glucava_strava_write_duration_seconds",
		"go_goroutines", // confirms the default Go runtime collector is wired in
	} {
		if !strings.Contains(body, want) {
			t.Errorf("response missing metric %q", want)
		}
	}
}

func TestInstrumentHandlerCountsRequests(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	h := InstrumentHandler(inner)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusTeapot)
	}
	// promhttp lower-cases the method label ("get", not "GET").
	if got := testutil.ToFloat64(httpRequestsTotal.WithLabelValues("418", "get")); got < 1 {
		t.Errorf("httpRequestsTotal{code=418,method=get} = %v, want >= 1", got)
	}
}

func TestGlucoseCollectorReportsLatest(t *testing.T) {
	c := &GlucoseCollector{Latest: func(context.Context) (stats.Summary, bool) {
		return stats.Summary{TIR: 91, Avg: 118, Min: 70, Max: 190}, true
	}}

	reg := newTestRegistry(t, c)
	body := scrape(t, reg)
	for _, want := range []string{
		"glucava_glucose_tir_percent 91",
		"glucava_glucose_avg_mgdl 118",
		"glucava_glucose_min_mgdl 70",
		"glucava_glucose_max_mgdl 190",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("response missing %q, got:\n%s", want, body)
		}
	}
}

func TestGlucoseCollectorSkipsWhenNoData(t *testing.T) {
	c := &GlucoseCollector{Latest: func(context.Context) (stats.Summary, bool) {
		return stats.Summary{}, false
	}}

	reg := newTestRegistry(t, c)
	body := scrape(t, reg)
	if strings.Contains(body, "glucava_glucose_tir_percent") {
		t.Errorf("expected no glucose gauges when Latest reports no data, got:\n%s", body)
	}
}

func TestRequireTokenRejectsMissingToken(t *testing.T) {
	called := false
	h := RequireToken(func(string) (bool, error) { return true, nil }, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rr.Code)
	}
	if called {
		t.Error("wrapped handler ran without a token")
	}
}

func TestRequireTokenRejectsInvalidToken(t *testing.T) {
	h := RequireToken(func(token string) (bool, error) { return token == "good", nil }, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("wrapped handler ran with an invalid token")
	}))

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer bad")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rr.Code)
	}
}

func TestRequireTokenAllowsValidToken(t *testing.T) {
	called := false
	h := RequireToken(func(token string) (bool, error) { return token == "good", nil }, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer good")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || !called {
		t.Fatalf("status = %d, called = %v, want 200/true", rr.Code, called)
	}
}

func TestRequireTokenPropagatesVerifyError(t *testing.T) {
	wantErr := errors.New("db down")
	h := RequireToken(func(string) (bool, error) { return false, wantErr }, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("wrapped handler ran despite a Verify error")
	}))

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer whatever")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rr.Code)
	}
}
