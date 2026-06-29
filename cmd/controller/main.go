package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/praetordev/praetor/pkg/registration"
	natsTransport "github.com/praetordev/praetor/pkg/transport/nats"
	"github.com/praetordev/praetor/services/controller/builder"
	"github.com/praetordev/praetor/services/controller/core"
	"github.com/praetordev/praetor/services/controller/launcher"
	"github.com/praetordev/praetor/services/controller/reconciler"
)

func main() {
	// 1. Database Connection
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL is required")
	}
	db, err := sqlx.Connect("postgres", dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// 2. Select Launcher
	var jobLauncher core.Launcher
	orch := os.Getenv("PRAETOR_ORCHESTRATOR")

	if orch == "message_queue" {
		log.Println("Using Message Queue Launcher (NATS)")
		natsURL := os.Getenv("NATS_URL")
		if natsURL == "" {
			natsURL = "nats://127.0.0.1:4222"
		}
		bus, err := natsTransport.NewNatsBus(natsURL)
		if err != nil {
			log.Fatalf("Failed to connect to NATS: %v", err)
		}
		// Note: We don't defer bus.Close() here easily because it runs in background,
		// but main blocks on HTTP server so it should be fine.

		jobLauncher = launcher.NewNatsLauncher(bus, db)
	} else if orch == "k8s" {
		log.Fatal("K8s Launcher not implemented in this refactor yet")
	} else {
		log.Println("Using Docker Launcher")
		dl, err := launcher.NewDockerLauncher()
		if err != nil {
			log.Fatalf("Failed to create Docker launcher: %v", err)
		}
		jobLauncher = dl
	}

	// 3. Setup Dependencies
	projectDir := os.Getenv("PRAETOR_PROJECT_DIR")
	if projectDir == "" {
		projectDir = "."
	}
	log.Printf("Starting Controller with Project Directory: %s", projectDir)

	manifestBuilder := builder.NewSQLManifestBuilder(db, projectDir)

	// 4. Instance Registration
	apiURL := os.Getenv("API_URL")
	if apiURL == "" {
		apiURL = "http://api:8080/api/v1"
	}
	regClient := registration.NewClient(registration.Config{
		APIBaseURL:      apiURL,
		InstanceType:    "controller",
		Capacity:        100,
		HeartbeatPeriod: 30 * time.Second,
	})

	ctx := context.Background()
	if err := regClient.Register(ctx); err != nil {
		log.Printf("[registration] Failed to register (will retry): %v", err)
	}
	regClient.StartHeartbeat(ctx)
	defer regClient.Stop()

	// 5. Start Reconciler
	// 5 minute timeout for pending jobs to prevent executing stale ones on restart
	recon := reconciler.NewReconciler(db, jobLauncher, manifestBuilder, 2*time.Second, 5*time.Minute)

	log.Println("Controller Service Started (Reconciler Mode)")
	go recon.Start()

	// 6. Start HTTP Server (Health/Metrics - Port 8085 to avoid conflict with API)
	log.Fatal(http.ListenAndServe(":8085", nil))
}
