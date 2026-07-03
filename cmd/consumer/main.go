package main

import (
	"log"
	"os"

	"github.com/praetordev/praetor/pkg/crypto"
	"github.com/praetordev/praetor/pkg/db"
	"github.com/praetordev/praetor/pkg/metrics"
	natsTransport "github.com/praetordev/praetor/pkg/transport/nats"
	"github.com/praetordev/praetor/services/consumer/core"
)

func main() {
	log.Println("Starting Event Consumer Service...")

	// Fail fast on a missing/invalid encryption key (used to decrypt
	// notification target URLs before dispatch).
	if err := crypto.ValidateSecrets(false); err != nil {
		log.Fatalf("secrets misconfigured: %v", err)
	}

	// 1. Connect to DB
	database, err := db.InitDB()
	if err != nil {
		log.Fatalf("Failed to connect to DB: %v", err)
	}

	// 2. Setup Infrastructure
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = "nats://127.0.0.1:4222"
	}
	bus, err := natsTransport.NewNatsBus(natsURL)
	if err != nil {
		log.Fatalf("Failed to connect to NATS: %v", err)
	}
	defer bus.Close()

	metrics.Serve("")

	// 3. Create Consumer (with notification dispatch on lifecycle events)
	writer := core.NewDBWriter(database)
	writer.Notifier = core.NewNotifier(database)
	consumer := core.NewConsumer(bus, writer)

	// 4. Start
	if err := consumer.Start(); err != nil {
		log.Fatalf("Consumer failed: %v", err)
	}
}
