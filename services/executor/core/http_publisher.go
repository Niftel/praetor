package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/praetordev/praetor/pkg/events"
)

type HttpEventPublisher struct {
	IngestionURL string
	Client       *http.Client
}

func NewHttpEventPublisher(ingestionURL string) *HttpEventPublisher {
	return &HttpEventPublisher{
		IngestionURL: ingestionURL,
		Client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (p *HttpEventPublisher) PublishJobEvent(event *events.JobEvent) error {
	// API endpoint: POST /api/v1/runs/{run_id}/events
	url := fmt.Sprintf("%s/api/v1/runs/%s/events", p.IngestionURL, event.ExecutionRunID.String())

	// Wrap in array as the API expects a batch
	payload := []events.JobEvent{*event}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.Client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send event to %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("ingestion service returned error: %d", resp.StatusCode)
	}

	return nil
}

func (p *HttpEventPublisher) PublishLogChunk(chunk *events.LogChunk) error {
	// Not implemented yet for Ingestion Service HTTP API?
	// Or maybe we use the same endpoint if it supports it, or a differnet one.
	// For now, logging to stdout/stderr which the executor infrastructure might capture is default.
	return nil
}
