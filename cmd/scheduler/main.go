package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/praetordev/praetor/pkg/crypto"
	"github.com/praetordev/praetor/pkg/db"
	"github.com/praetordev/praetor/pkg/metrics"
	natsTransport "github.com/praetordev/praetor/pkg/transport/nats"
	core "github.com/praetordev/praetor/services/scheduler/core"
)

func main() {
	log.Println("Starting Scheduler Service...")

	// Fail fast on a missing/invalid encryption key (used to decrypt Galaxy
	// credential tokens when building manifests).
	if err := crypto.ValidateSecrets(false); err != nil {
		log.Fatalf("secrets misconfigured: %v", err)
	}

	// 1. Connect to DB
	database, err := db.InitDB()
	if err != nil {
		log.Fatalf("Failed to connect to DB: %v", err)
	}

	// 2. Init NATS
	// Default URL is nats://127.0.0.1:4222
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = "nats://127.0.0.1:4222"
	}
	bus, err := natsTransport.NewNatsBus(natsURL)
	if err != nil {
		log.Fatalf("Failed to connect to NATS: %v", err)
	}
	defer bus.Close()

	// Expose Prometheus /metrics.
	metrics.Serve("")

	// 3. Init Scheduler
	// Poll every 5 seconds
	sched := core.NewScheduler(database, 5*time.Second, bus)

	// 3. Start loop in background
	go sched.Start()

	// 4. Wait for SIGINT/SIGTERM
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs

	// 5. Graceful shutdown
	log.Println("Shutting down...")
	sched.Stop()
}
