package main

import (
	"flag"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"
)

var (
	jobDir = flag.String("job-dir", "", "Path to the job directory")
	apiURL = flag.String("api-url", "", "URL of the Praetor API")
	runID  = flag.String("run-id", "", "Execution Run ID")
)

func main() {
	flag.Parse()

	if err := run(); err != nil {
		log.Printf("Host Runner exited with error: %v", err)
		os.Exit(1)
	}
}

func run() error {
	if *jobDir == "" {
		log.Fatal("--job-dir is required")
	}

	// Create job directory if it doesn't exist
	if err := os.MkdirAll(*jobDir, 0755); err != nil {
		return err
	}

	// Setup logging
	logFile, err := os.OpenFile(filepath.Join(*jobDir, "runner.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		log.SetOutput(io.MultiWriter(os.Stderr, logFile))
		defer logFile.Close()
	} else {
		log.Printf("Warning: Failed to open runner.log in job dir: %v", err)
	}

	log.Printf("Starting Praetor Host Runner for job dir: %s", *jobDir)

	if *apiURL != "" && *runID != "" {
		syncer := NewSyncer(*jobDir, *apiURL, *runID)
		done := make(chan bool, 1)
		finished := make(chan bool, 1)
		go func() {
			syncer.Start(done)
			finished <- true
		}()
		defer func() {
			log.Println("Waiting for Syncer to finish...")
			// Safety: Give FS a moment to flush any pending writes from runner close
			time.Sleep(100 * time.Millisecond)
			done <- true
			<-finished
			log.Println("Syncer finished.")
		}()
	}

	runner := NewRunner(*jobDir)
	if err := runner.Execute(); err != nil {
		return err
	}

	log.Println("Job completed successfully")
	return nil
}
