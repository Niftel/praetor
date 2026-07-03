package core

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// ReconcileOutcomes counts what happened to a reconciled run, by outcome:
	// recovered_successful, recovered_failed, lost, still_running.
	ReconcileOutcomes = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "praetor_reconciler_outcomes_total",
		Help: "Reconciliation outcomes by type.",
	}, []string{"outcome"})

	// ReconcileEventsProjected counts events pulled from host WALs and projected.
	ReconcileEventsProjected = promauto.NewCounter(prometheus.CounterOpts{
		Name: "praetor_reconciler_events_projected_total",
		Help: "Total events projected from pulled host WALs.",
	})

	// ReconcileChunksProjected counts stdout chunks pulled and uploaded.
	ReconcileChunksProjected = promauto.NewCounter(prometheus.CounterOpts{
		Name: "praetor_reconciler_log_chunks_projected_total",
		Help: "Total stdout log chunks projected from pulled host logs.",
	})

	// ReconcileAttempts counts unproductive attempts (backoff), before give-up.
	ReconcileAttempts = promauto.NewCounter(prometheus.CounterOpts{
		Name: "praetor_reconciler_attempts_total",
		Help: "Total reconcile attempts that made no progress (backed off).",
	})

	// ReconcileTick measures the wall time of one reconciler tick.
	ReconcileTick = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "praetor_reconciler_tick_duration_seconds",
		Help:    "Duration of one reconciler tick.",
		Buckets: prometheus.DefBuckets,
	})
)
