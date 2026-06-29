// Package registration provides instance self-registration and heartbeat functionality
package registration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

// Config holds registration configuration
type Config struct {
	APIBaseURL        string        // e.g. http://api:8080/api/v1
	Hostname          string        // Instance hostname
	InstanceType      string        // executor, controller, hybrid
	Capacity          int           // Capacity units
	HeartbeatPeriod   time.Duration // How often to send heartbeats
	RegistrationToken string        // Auth token for registration
}

// Instance represents the response from registration
type Instance struct {
	ID            int64   `json:"id"`
	Hostname      string  `json:"hostname"`
	InstanceType  string  `json:"instance_type"`
	Healthy       bool    `json:"healthy"`
	LastHeartbeat *string `json:"last_heartbeat,omitempty"`
}

// Client handles registration and heartbeat
type Client struct {
	config     Config
	httpClient *http.Client
	instanceID int64
	stopCh     chan struct{}
}

// NewClient creates a new registration client
func NewClient(cfg Config) *Client {
	if cfg.Hostname == "" {
		hostname, _ := os.Hostname()
		cfg.Hostname = hostname
	}
	if cfg.Capacity == 0 {
		cfg.Capacity = 100
	}
	if cfg.HeartbeatPeriod == 0 {
		cfg.HeartbeatPeriod = 30 * time.Second
	}

	return &Client{
		config:     cfg,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		stopCh:     make(chan struct{}),
	}
}

// Register sends registration request to API
func (c *Client) Register(ctx context.Context) error {
	payload := map[string]interface{}{
		"hostname":      c.config.Hostname,
		"instance_type": c.config.InstanceType,
		"capacity":      c.config.Capacity,
	}

	body, _ := json.Marshal(payload)
	url := fmt.Sprintf("%s/instances/register", c.config.APIBaseURL)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.config.RegistrationToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.config.RegistrationToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("register request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("registration failed with status %d", resp.StatusCode)
	}

	var instance Instance
	if err := json.NewDecoder(resp.Body).Decode(&instance); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	c.instanceID = instance.ID
	log.Printf("[registration] Registered as instance %d (%s)", instance.ID, instance.Hostname)
	return nil
}

// sendHeartbeat sends a single heartbeat
func (c *Client) sendHeartbeat(ctx context.Context) error {
	if c.instanceID == 0 {
		return fmt.Errorf("not registered")
	}

	url := fmt.Sprintf("%s/instances/%d/heartbeat", c.config.APIBaseURL, c.instanceID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return fmt.Errorf("create heartbeat request: %w", err)
	}
	if c.config.RegistrationToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.config.RegistrationToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("heartbeat request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("heartbeat failed with status %d", resp.StatusCode)
	}

	return nil
}

// StartHeartbeat starts the heartbeat loop in a goroutine
func (c *Client) StartHeartbeat(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(c.config.HeartbeatPeriod)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-c.stopCh:
				return
			case <-ticker.C:
				if err := c.sendHeartbeat(ctx); err != nil {
					log.Printf("[registration] heartbeat failed: %v", err)
					// Try to re-register
					if rerr := c.Register(ctx); rerr != nil {
						log.Printf("[registration] re-registration failed: %v", rerr)
					}
				}
			}
		}
	}()
}

// Stop stops the heartbeat loop
func (c *Client) Stop() {
	close(c.stopCh)
}

// InstanceID returns the registered instance ID
func (c *Client) InstanceID() int64 {
	return c.instanceID
}
