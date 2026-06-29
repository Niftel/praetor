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
		// Two independent syncers: structured lifecycle events (events.jsonl) and
		// bulk stdout (stdout.log -> object store). Each owns its own cursor.
		syncer := NewSyncer(*jobDir, *apiURL, *runID)
		logSyncer := NewLogSyncer(*jobDir, *apiURL, *runID)
		done := make(chan bool, 1)
		logDone := make(chan bool, 1)
		finished := make(chan bool, 1)
		logFinished := make(chan bool, 1)
		go func() {
			syncer.Start(done)
			finished <- true
		}()
		go func() {
			logSyncer.Start(logDone)
			logFinished <- true
		}()
		defer func() {
			log.Println("Waiting for syncers to finish...")
			// Safety: give the FS a moment to flush any pending writes.
			time.Sleep(100 * time.Millisecond)
			done <- true
			logDone <- true
			<-finished
			<-logFinished
			log.Println("Syncers finished.")
		}()
	}

	runner := NewRunner(*jobDir)
	if err := runner.Execute(); err != nil {
		return err
	}

	log.Println("Job completed successfully")
	return nil
}
