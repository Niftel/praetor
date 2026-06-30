package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/praetordev/praetor/pkg/events"
	natsTransport "github.com/praetordev/praetor/pkg/transport/nats"
	"github.com/praetordev/praetor/services/executor/core"
)

func main() {
	log.Println("Starting Executor Agent...")

	// 1. Setup Infrastructure
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = "nats://127.0.0.1:4222"
	}
	bus, err := natsTransport.NewNatsBus(natsURL)
	if err != nil {
		log.Fatalf("Failed to connect to NATS: %v", err)
	}
	defer bus.Close()

	// Determine Publisher (HTTP or NATS)
	var publisher core.EventPublisher = bus
	ingestionURL := os.Getenv("INGESTION_URL")
	if ingestionURL != "" {
		log.Printf("Using HTTP Ingestion Publisher at %s", ingestionURL)
		publisher = core.NewHttpEventPublisher(ingestionURL)
	} else {
		log.Println("Using NATS Event Publisher")
	}

	// 4. Create Runner (Bootstrap Mode)
	runner := core.NewBootstrapRunner()

	// Check for One-Shot Mode
	if os.Getenv("PRAETOR_MODE") == "oneshot" {
		log.Println("Starting in ONE-SHOT mode")
		manifestPath := os.Getenv("PRAETOR_MANIFEST_PATH")
		if manifestPath == "" {
			manifestPath = "/etc/praetor/manifest.json"
		}

		// Read manifest
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			log.Fatalf("Failed to read manifest at %s: %v", manifestPath, err)
		}

		var req events.ExecutionRequest
		if err := json.Unmarshal(data, &req); err != nil {
			log.Fatalf("Failed to unmarshal manifest: %v", err)
		}

		log.Printf("Loaded execution request %s for job %d", req.ExecutionRunID, req.UnifiedJobID)

		// Create event channel and publisher loop
		eventChan := make(chan events.JobEvent, 100)
		doneChan := make(chan bool)

		go func() {
			for evt := range eventChan {
				if err := publisher.PublishJobEvent(&evt); err != nil {
					log.Printf("Failed to publish event: %v", err)
				}
			}
			doneChan <- true
		}()

		// Run job. On failure, emit a JOB_FAILED event (as the daemon path does)
		// so the run is marked failed promptly instead of waiting for the
		// reconciler's heartbeat timeout, then flush before exiting.
		runErr := runner.Run(&req, eventChan)
		if runErr != nil {
			log.Printf("Job execution failed: %v", runErr)
			failMsg := fmt.Sprintf("Runner failed: %v", runErr)
			eventChan <- events.JobEvent{
				ExecutionRunID: req.ExecutionRunID,
				UnifiedJobID:   req.UnifiedJobID,
				EventType:      "JOB_FAILED",
				Timestamp:      time.Now(),
				StdoutSnippet:  &failMsg,
			}
		}

		close(eventChan)
		<-doneChan // Wait for events to flush
		if runErr != nil {
			os.Exit(1)
		}
		log.Println("One-shot execution finished successfully.")
		return
	}

	// 2. Create Agent (Daemon Mode)
	// We use NATS for Subscription (bus), and our selected publisher for Events
	agent := core.NewAgent(bus, publisher, runner)

	// 4. Start
	if err := agent.Start(); err != nil {
		log.Fatalf("Agent failed: %v", err)
	}
}
