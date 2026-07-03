// Command reconciler is the pull-based recovery service: it SSHes to hosts whose
// runs are parked in 'reconciling' and harvests their WAL, so a job that finished
// on the host (but whose push never reached the control plane) is recovered to
// its true outcome instead of being falsely failed. See host_side_runner_spec.md
// §5 and services/reconciler/core.
package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/praetordev/praetor/pkg/crypto"
	"github.com/praetordev/praetor/pkg/db"
	core "github.com/praetordev/praetor/services/reconciler/core"
)

func main() {
	log.Println("Starting Reconciler Service...")

	// Credentials are decrypted to reconstruct the SSH identity for a past run.
	if err := crypto.ValidateSecrets(false); err != nil {
		log.Fatalf("secrets misconfigured: %v", err)
	}

	database, err := db.InitDB()
	if err != nil {
		log.Fatalf("Failed to connect to DB: %v", err)
	}
	defer database.Close()

	// The run event/log endpoints live on the ingestion service (the same target
	// the host-runner pushes to), not the API.
	apiURL := os.Getenv("INGESTION_URL")
	if apiURL == "" {
		apiURL = os.Getenv("API_URL")
	}
	if apiURL == "" {
		apiURL = "http://ingestion:8081"
	}

	interval := 30 * time.Second
	if v := os.Getenv("RECONCILE_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			interval = d
		}
	}

	rec := core.NewReconciler(database, interval, apiURL)
	go rec.Start()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs

	log.Println("Shutting down...")
	rec.Stop()
}
