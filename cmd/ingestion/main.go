package main

import (
	"log"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/praetordev/praetor/pkg/db"
	"github.com/praetordev/praetor/pkg/objectstore"
	natsTransport "github.com/praetordev/praetor/pkg/transport/nats"
	"github.com/praetordev/praetor/services/ingestion/core"
	"github.com/praetordev/praetor/services/ingestion/handler"
)

func main() {
	port := os.Getenv("INGESTION_PORT")
	if port == "" {
		port = "8081" // Distinct port from API (8080)
	}

	log.Println("Starting Ingestion Service...")

	// 1. DB
	database, err := db.InitDB()
	if err != nil {
		log.Fatalf("Failed to connect to DB: %v", err)
	}

	// 2. NATS Infrastructure
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = "nats://127.0.0.1:4222"
	}
	bus, err := natsTransport.NewNatsBus(natsURL)
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
