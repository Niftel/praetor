package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// heartbeatInterval is how often the runner reports liveness AND checks for a
// cancel request. It must stay well under the reconciler's "lost" grace (minutes)
// so a healthy long-running job is never mistaken for a dead one; it also bounds
// cancel latency, so keep it modest.
const heartbeatInterval = 15 * time.Second

// runHeartbeat periodically tells the control plane this run is alive (updating
// execution_runs.last_heartbeat_at) and reads the response for a cancel request.
// Heartbeats flow DURING execution (concurrently with the playbook), so the
// reconciler can tell a long-running job from a lost one. On the first cancel
// signal it invokes onCancel (which stops the play) exactly once, then keeps
// beating so the run stays live while it winds down. Stops when done is signalled.
func runHeartbeat(apiURL, runID, token string, done <-chan bool, onCancel func()) {
	client := &http.Client{Timeout: 5 * time.Second}
	url := fmt.Sprintf("%s/api/v1/runs/%s/heartbeat", apiURL, runID)
	canceled := false

	send := func() {
		req, err := http.NewRequest("POST", url, nil)
		if err != nil {
			return
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("Heartbeat failed: %v", err)
			return
		}
		defer resp.Body.Close()
		var body struct {
			Cancel bool `json:"cancel"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		if body.Cancel && !canceled && onCancel != nil {
			log.Printf("Cancel requested for run %s — stopping the play", runID)
			canceled = true
			onCancel()
		}
	}

	send() // immediate, so a run is marked alive as soon as it starts
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			send()
		}
	}
}
