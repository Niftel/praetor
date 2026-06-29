package main

import (
	"log"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/praetordev/praetor/pkg/db"
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

	// 3. Service & Handler
	svc := core.NewIngestionService(database, bus)
	h := handler.NewIngestionHandler(svc)

	// 4. Router
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Post("/api/v1/runs/{run_id}/events", h.Ingest)

	// 5. Start
	log.Printf("Ingestion listening on port %s", port)
	if err := http.ListenAndServe(":"+port, r); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
