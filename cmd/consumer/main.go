package main

import (
	"log"

	"github.com/praetordev/crypto"
	"github.com/praetordev/db"
	"github.com/praetordev/env"
	"github.com/praetordev/metrics"
	"github.com/praetordev/plog"
	natsTransport "github.com/praetordev/praetor/pkg/transport/nats"
	"github.com/praetordev/praetor/services/consumer/core"
)

func main() {
	plog.Configure("consumer")
	log.Println("Starting Event Consumer Service...")

	// Fail fast on a missing/invalid encryption key (used to decrypt
	// notification target URLs before dispatch).
	if err := crypto.ValidateSecrets(false); err != nil {
		log.Fatalf("secrets misconfigured: %v", err)
	}

	// 1. Connect to DB
	database, err := db.Connect(env.String("DATABASE_URL", db.DefaultDSN))
	if err != nil {
		log.Fatalf("Failed to connect to DB: %v", err)
	}

	// 2. Setup Infrastructure
	bus, err := natsTransport.NewNatsBus(env.String("NATS_URL", natsTransport.DefaultURL))
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
