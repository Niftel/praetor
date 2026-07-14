package tests

import (
	"testing"
	"time"

	scheduler "github.com/praetordev/scheduler/core"
)

func TestSchedulerInstantiate(t *testing.T) {
	// Verify it compiles and instantiates
	publisher := &scheduler.NOOPPublisher{}
	sched := scheduler.NewScheduler(nil, 1*time.Second, publisher)
	if sched == nil {
		t.Fatal("NewScheduler returned nil")
	}

	// We can't easily test processPendingJobs with a nil DB unless we mock sqlx,
	// which is complex. For this agent run, verifying structure and compilation
	// is the primary goal if a live DB isn't guaranteed.

	// If we wanted to test logic, we'd need `sqlmock`.
	// For now, simple instantiation proof is enough for skeleton phase.
}
