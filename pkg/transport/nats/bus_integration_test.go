package nats_test

import (
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/praetordev/events"
	natsbus "github.com/praetordev/praetor/pkg/transport/nats"
)

// TestExecutionRequestDurableAndDeduped proves the item-6a guarantees against a
// real JetStream server:
//
//  1. Durability — a launch published while NO executor is connected is retained
//     and delivered to a later subscriber (core NATS would have dropped it).
//  2. Dedup — publishing the same launch twice (as the outbox relay may on
//     retry) results in exactly one delivery.
//
// Set TEST_NATS_URL to a JetStream-enabled server to run it.
func TestExecutionRequestDurableAndDeduped(t *testing.T) {
	url := os.Getenv("TEST_NATS_URL")
	if url == "" {
		t.Skip("TEST_NATS_URL not set; skipping NATS integration test")
	}

	bus, err := natsbus.NewNatsBus(url)
	if err != nil {
		t.Skipf("cannot reach JetStream NATS: %v", err)
	}
	defer bus.Close()

	runID := uuid.New()
	req := &events.ExecutionRequest{ExecutionRunID: runID, UnifiedJobID: 42}

	// Publish twice with no subscriber present.
	if err := bus.PublishExecutionRequest(req); err != nil {
		t.Fatalf("first publish: %v", err)
	}
	if err := bus.PublishExecutionRequest(req); err != nil {
		t.Fatalf("duplicate publish: %v", err)
	}

	// Subscribe only now — the launch must still arrive (durable).
	ch, err := bus.SubscribeToExecutionRequests()
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	select {
	case got := <-ch:
		if got.ExecutionRunID != runID {
			t.Fatalf("got unexpected run %s", got.ExecutionRunID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("durable launch was never delivered to a late subscriber")
	}

	// The duplicate publish must NOT produce a second delivery.
	select {
	case got := <-ch:
		t.Fatalf("dedup failed: launch delivered twice (run %s)", got.ExecutionRunID)
	case <-time.After(1 * time.Second):
		// expected: no second message
	}
}
