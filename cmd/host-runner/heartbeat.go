package main

import (
	"fmt"
	"log"
	"net/http"
	"time"
)

// heartbeatInterval is how often the runner reports liveness. It must be well
// under the reconciler's "lost" grace so a healthy long-running job is never
// mistaken for a dead one.
const heartbeatInterval = 30 * time.Second

// runHeartbeat periodically tells the control plane this run is alive, which
// updates execution_runs.last_heartbeat_at. Heartbeats flow DURING execution
// (this runs concurrently with the playbook), so the reconciler can distinguish
// a genuinely long-running job from a lost one. It stops when done is signalled.
func runHeartbeat(apiURL, runID string, done <-chan bool) {
	client := &http.Client{Timeout: 5 * time.Second}
	url := fmt.Sprintf("%s/api/v1/runs/%s/heartbeat", apiURL, runID)

	send := func() {
		req, err := http.NewRequest("POST", url, nil)
		if err != nil {
			return
		}
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("Heartbeat failed: %v", err)
			return
		}
		resp.Body.Close()
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
