// Package metrics exposes glucava's job, ingest and glucose state as
// Prometheus metrics, for scraping into the same VictoriaMetrics/Grafana
// setup the rest of the homelab already reports to. All counters and
// gauges here are package-level, registered against the default Prometheus
// registerer, and served by promhttp.Handler() at /metrics.
package metrics

import (
	"context"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/MrCodeEU/glucava/internal/stats"
)

// The default registerer already carries a Go runtime collector and a
// process collector (prometheus.init() registers both), so /metrics gets
// GC, goroutine and memory stats, plus process CPU/RSS/fd counts, for free.

var (
	// JobsProcessedTotal counts finished activity jobs by outcome:
	// "success" or "failed" (retries exhausted). A restore or a job still
	// in progress is not counted.
	JobsProcessedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "glucava_jobs_processed_total",
		Help: "Activity jobs finished, by outcome.",
	}, []string{"result"})

	// JobsQueueDepth is how many activities are currently queued or being
	// processed by the single job worker.
	JobsQueueDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "glucava_jobs_queue_depth",
		Help: "Activities currently queued or being processed.",
	})

	// IngestReadingsStoredTotal counts glucose readings the background
	// ingest loop has stored (see internal/ingest). It does not count
	// readings fetched directly while processing one activity.
	IngestReadingsStoredTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "glucava_ingest_readings_stored_total",
		Help: "Glucose readings stored by the background ingest loop.",
	})

	// IngestErrorsTotal counts failed background ingest fetches.
	IngestErrorsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "glucava_ingest_errors_total",
		Help: "Background ingest fetches that failed.",
	})

	// IngestLastSuccessTimestamp is the Unix time of the last successful
	// background ingest fetch (0 before the first one). An alert on its
	// age catches a source that has silently stopped answering, the same
	// gap the in-app gap alert (internal/gap) already watches for.
	IngestLastSuccessTimestamp = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "glucava_ingest_last_success_timestamp_seconds",
		Help: "Unix time of the last successful background ingest fetch.",
	})

	// StravaWriteDuration times one description edit: load the activity's
	// edit page, merge in the block, save.
	StravaWriteDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "glucava_strava_write_duration_seconds",
		Help:    "Time to load, merge and save one Strava activity description.",
		Buckets: prometheus.DefBuckets,
	})

	httpRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "glucava_http_requests_total",
		Help: "HTTP requests served, by method and status code.",
	}, []string{"code", "method"})

	httpRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "glucava_http_request_duration_seconds",
		Help:    "HTTP request latency.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method"})

	httpInFlight = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "glucava_http_requests_in_flight",
		Help: "HTTP requests currently being served.",
	})
)

// InstrumentHandler wraps h with request count, latency and in-flight
// metrics. Requests are labelled by method and status code only, never by
// path: several routes carry an id in the path (e.g. /activity/{id}), and a
// path label would give every activity its own time series forever.
func InstrumentHandler(h http.Handler) http.Handler {
	return promhttp.InstrumentHandlerInFlight(httpInFlight,
		promhttp.InstrumentHandlerDuration(httpRequestDuration,
			promhttp.InstrumentHandlerCounter(httpRequestsTotal, h)))
}

// Handler serves the Prometheus text exposition format.
func Handler() http.Handler {
	return promhttp.Handler()
}

var (
	glucoseTIRDesc = prometheus.NewDesc("glucava_glucose_tir_percent", "Time in range over glucava's recent window.", nil, nil)
	glucoseAvgDesc = prometheus.NewDesc("glucava_glucose_avg_mgdl", "Average glucose over glucava's recent window, mg/dL.", nil, nil)
	glucoseMinDesc = prometheus.NewDesc("glucava_glucose_min_mgdl", "Minimum glucose over glucava's recent window, mg/dL.", nil, nil)
	glucoseMaxDesc = prometheus.NewDesc("glucava_glucose_max_mgdl", "Maximum glucose over glucava's recent window, mg/dL.", nil, nil)
)

// GlucoseCollector reports glucava's own recent-window glucose summary as
// gauges. It computes Latest fresh on every scrape rather than caching a
// periodically-updated value, so the numbers are never staler than the
// scrape interval itself.
type GlucoseCollector struct {
	// Latest returns a summary for glucava's own recent window and
	// whether one could be computed at all (no data yet, or the source
	// is down). Required.
	Latest func(ctx context.Context) (stats.Summary, bool)
}

func (c *GlucoseCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- glucoseTIRDesc
	ch <- glucoseAvgDesc
	ch <- glucoseMinDesc
	ch <- glucoseMaxDesc
}

func (c *GlucoseCollector) Collect(ch chan<- prometheus.Metric) {
	sum, ok := c.Latest(context.Background())
	if !ok {
		return
	}
	ch <- prometheus.MustNewConstMetric(glucoseTIRDesc, prometheus.GaugeValue, sum.TIR)
	ch <- prometheus.MustNewConstMetric(glucoseAvgDesc, prometheus.GaugeValue, sum.Avg)
	ch <- prometheus.MustNewConstMetric(glucoseMinDesc, prometheus.GaugeValue, sum.Min)
	ch <- prometheus.MustNewConstMetric(glucoseMaxDesc, prometheus.GaugeValue, sum.Max)
}

// RegisterGlucoseCollector registers a GlucoseCollector using latest. Callers
// outside this package register through here instead of importing
// prometheus themselves just to call MustRegister.
func RegisterGlucoseCollector(latest func(ctx context.Context) (stats.Summary, bool)) {
	prometheus.MustRegister(&GlucoseCollector{Latest: latest})
}
