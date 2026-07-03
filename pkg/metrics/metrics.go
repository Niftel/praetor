// Package metrics exposes Prometheus /metrics for every Praetor service. Each
// service defines and increments its own metrics (via promauto, in its own
// package); this package only provides the HTTP surface:
//
//   - Handler() for services that already run an HTTP server (api, ingestion):
//     mount it at /metrics on the existing router.
//   - Serve() for loop-only services (scheduler, consumer, executor, reconciler,
//     packbuilder): starts a tiny background listener whose only route is /metrics.
//
// The default registry also carries Go runtime + process collectors for free.
package metrics

import (
	"log"
	"net/http"
	"os"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Handler returns the Prometheus scrape handler for the default registry.
func Handler() http.Handler { return promhttp.Handler() }

// Serve starts a background HTTP listener exposing /metrics. addr defaults to
// the METRICS_ADDR env var, then ":2112". It never blocks the caller; a listen
// failure is logged (metrics are best-effort and must not take down a service).
func Serve(addr string) {
	if addr == "" {
		addr = os.Getenv("METRICS_ADDR")
	}
	if addr == "" {
		addr = ":2112"
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", Handler())
	go func() {
		log.Printf("metrics: serving /metrics on %s", addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Printf("metrics: listener on %s stopped: %v", addr, err)
		}
	}()
}
