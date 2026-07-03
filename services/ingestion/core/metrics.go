package core

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// EventsIngested counts individual events accepted (a request carries a batch),
// so it reflects event throughput rather than request rate.
var EventsIngested = promauto.NewCounter(prometheus.CounterOpts{
	Name: "praetor_ingestion_events_total",
	Help: "Total job events accepted for projection.",
})
