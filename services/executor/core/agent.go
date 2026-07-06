package core

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/praetordev/praetor/pkg/events"
)

type Agent struct {
	Subscriber EventSubscriber
	Publisher  EventPublisher
	Runner     Runner
	Workers    int
	wg         sync.WaitGroup
}

func NewAgent(sub EventSubscriber, pub EventPublisher, runner Runner, workers int) *Agent {
	if workers <= 0 {
		workers = 2
	}
	return &Agent{
		Subscriber: sub,
		Publisher:  pub,
		Runner:     runner,
		Workers:    workers,
	}
}

func (a *Agent) Start() error {
	reqChan, err := a.Subscriber.SubscribeToExecutionRequests()
	if err != nil {
		return err
	}

	log.Println("Agent started, waiting for jobs...")

	// simple worker pool
	for i := 0; i < a.Workers; i++ {
		a.wg.Add(1)
		go a.worker(i, reqChan)
	}

	a.wg.Wait()
	return nil
}

func (a *Agent) worker(id int, reqChan <-chan events.ExecutionRequest) {
	defer a.wg.Done()
	log.Printf("Worker %d started", id)

	for req := range reqChan {
		log.Printf("Worker %d picked up run %s", id, req.ExecutionRunID)
		a.processRequest(req)
	}
}

func (a *Agent) processRequest(req events.ExecutionRequest) {
	// Channel to receive events from the runner
	// We make it buffered so the runner doesn't block too easily
	eventChan := make(chan events.JobEvent, 100)

	// Start a goroutine to consume events from the runner and publish them
	go func() {
		seq := int64(1)
		for evt := range eventChan {
			evt.Seq = seq // overwrite logical sequence or trust runner?
			// Let's trust runner for now, or enforce monotonic here.
			// Ideally the runner is the source of truth for order, but we can double check.

			if err := a.Publisher.PublishJobEvent(&evt); err != nil {
				log.Printf("Failed to publish event: %v", err)
			}
			seq++
		}
	}()

	// Run the job (blocking for this worker)
	if err := a.Runner.Run(&req, eventChan); err != nil {
		log.Printf("Job run failed: %v", err)
		// Emit JOB_FAILED event since runner didn't succeed
		failMsg := fmt.Sprintf("Runner failed: %v", err)
		eventChan <- events.JobEvent{
			ExecutionRunID: req.ExecutionRunID,
			UnifiedJobID:   req.UnifiedJobID,
			EventType:      "JOB_FAILED",
			Timestamp:      time.Now(),
			StdoutSnippet:  &failMsg,
		}
	}
	close(eventChan)
}
