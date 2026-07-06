package main

import (
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/praetordev/praetor/pkg/db"
	"github.com/praetordev/praetor/pkg/env"
	"github.com/praetordev/praetor/pkg/metrics"
	"github.com/praetordev/praetor/pkg/objectstore"
	natsTransport "github.com/praetordev/praetor/pkg/transport/nats"
	"github.com/praetordev/praetor/services/ingestion/core"
	"github.com/praetordev/praetor/services/ingestion/handler"
)

func main() {
	port := env.String("INGESTION_PORT", "8081") // Distinct port from API (8080)

	log.Println("Starting Ingestion Service...")

	// 1. DB
	database, err := db.Connect(env.String("DATABASE_URL", db.DefaultDSN))
	if err != nil {
		log.Fatalf("Failed to connect to DB: %v", err)
	}

	// 2. NATS Infrastructure
	bus, err := natsTransport.NewNatsBus(env.String("NATS_URL", natsTransport.DefaultURL))
	if err != nil {
		log.Fatalf("Failed to connect to NATS: %v", err)
	}
	defer bus.Close()

	// 3. Object store for bulk log output (JetStream Object Store)
	logStore, err := objectstore.NewJetStreamLogStore(bus.JS, "")
	if err != nil {
		log.Fatalf("Failed to init log object store: %v", err)
	}

	// 4. Service & Handler
	svc := core.NewIngestionService(database, bus, logStore)
	h := handler.NewIngestionHandler(svc)

	// 4. Router
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(handler.Metrics)

	r.Handle("/metrics", metrics.Handler())

	// Liveness probe for the container healthcheck (compose depends_on:
	// service_healthy). Intentionally cheap — it does not touch Postgres or NATS,
	// so it reports process liveness, not downstream readiness.
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	r.Get("/api/v1/runs/{run_id}/runnable", h.Runnable)
	r.Post("/api/v1/runs/{run_id}/events", h.Ingest)
	r.Post("/api/v1/runs/{run_id}/logs", h.IngestLog)
	r.Get("/api/v1/runs/{run_id}/logs", h.StreamLog)
	r.Post("/api/v1/runs/{run_id}/heartbeat", h.Heartbeat)
	r.Post("/api/v1/runs/{run_id}/facts", h.IngestFacts)
	r.Post("/api/v1/inventories/{id}/sync-data", h.IngestInventorySync)

	// 5. Start
	log.Printf("Ingestion listening on port %s", port)
	if err := http.ListenAndServe(":"+port, r); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
