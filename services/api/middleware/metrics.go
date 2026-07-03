package middleware

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	httpRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "praetor_api_http_requests_total",
		Help: "API HTTP requests by matched route, method and status.",
	}, []string{"route", "method", "status"})

	httpLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "praetor_api_http_request_duration_seconds",
		Help:    "API HTTP request latency by matched route and method.",
		Buckets: prometheus.DefBuckets,
	}, []string{"route", "method"})
)

// Metrics records request count + latency labelled by the chi route PATTERN (not
// the raw path) so per-id URLs don't explode label cardinality.
func Metrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)

		route := chi.RouteContext(r.Context()).RoutePattern()
		if route == "" {
			route = "other" // unmatched routes (404s) collapse to one series
		}
		httpRequests.WithLabelValues(route, r.Method, strconv.Itoa(ww.Status())).Inc()
		httpLatency.WithLabelValues(route, r.Method).Observe(time.Since(start).Seconds())
	})
}
