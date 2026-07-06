package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/praetordev/praetor/pkg/crypto"
	"github.com/praetordev/praetor/pkg/db"
	"github.com/praetordev/praetor/pkg/env"
	"github.com/praetordev/praetor/pkg/metrics"
	"github.com/praetordev/praetor/pkg/objectstore"
	"github.com/praetordev/praetor/pkg/plog"
	natsTransport "github.com/praetordev/praetor/pkg/transport/nats"
	core "github.com/praetordev/praetor/services/scheduler/core"
)

func main() {
	plog.Configure("scheduler")
	log.Println("Starting Scheduler Service...")

	// Fail fast on a missing/invalid encryption key (used to decrypt Galaxy
	// credential tokens when building manifests).
	if err := crypto.ValidateSecrets(false); err != nil {
		log.Fatalf("secrets misconfigured: %v", err)
	}

	// 1. Connect to DB
	database, err := db.Connect(env.String("DATABASE_URL", db.DefaultDSN))
	if err != nil {
		log.Fatalf("Failed to connect to DB: %v", err)
	}

	// 2. Init NATS
	bus, err := natsTransport.NewNatsBus(env.String("NATS_URL", natsTransport.DefaultURL))
	if err != nil {
		log.Fatalf("Failed to connect to NATS: %v", err)
	}
	defer bus.Close()

	// Expose Prometheus /metrics.
	metrics.Serve("")

	// 3. Init Scheduler
	// Poll every 5 seconds
	sched := core.NewScheduler(database, 5*time.Second, bus)
	// Base URL embedded in the manifest for the pushed host-runner to report back.
	sched.APIURL = env.String("API_URL", "")

	// Retention pruning (opt-in). JOB_RETENTION_DAYS=0 (default) keeps everything;
	// a positive value deletes terminal jobs finished longer ago than that, along
	// with their events and log blobs.
	if days := env.Int("JOB_RETENTION_DAYS", 0); days > 0 {
		sched.RetentionDays = days
		if ls, err := objectstore.NewJetStreamLogStore(bus.JS, ""); err == nil {
			sched.Logs = ls
		} else {
			log.Printf("retention: object store unavailable, blobs won't be pruned: %v", err)
		}
		log.Printf("retention: pruning terminal jobs finished > %d day(s) ago", days)
	}

	// 3. Start loop in background; ctx cancellation is the stop signal.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sched.Start(ctx)

	// 4. Wait for SIGINT/SIGTERM
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs

	// 5. Graceful shutdown
	log.Println("Shutting down...")
	cancel()
}
