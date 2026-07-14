package nats_test

import (
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/praetordev/events"
	natsbus "github.com/praetordev/praetor/pkg/transport/nats"
)

// TestPullConsumerHeavyLoadExactlyOnce simulates heavy launch load against a real
// JetStream server and asserts the pull-consumer delivery contract:
//
//   - every launch is delivered (nothing lost),
//   - each launch is delivered exactly once even with the worker pool saturated
//     by slow "bootstraps" (no redelivery → no double-bootstrap), and
//   - in-flight work stays bounded by the worker count (no 100-deep buffer whose
//     acked-but-unprocessed contents would be lost on a crash).
//
// Set TEST_NATS_URL to an isolated JetStream server (NOT the live stack's NATS —
// subscribing recreates the executor durable and consumes job.requests).
func TestPullConsumerHeavyLoadExactlyOnce(t *testing.T) {
	url := os.Getenv("TEST_NATS_URL")
	if url == "" {
		t.Skip("TEST_NATS_URL not set; skipping NATS load test")
	}

	bus, err := natsbus.NewNatsBus(url)
	if err != nil {
		t.Skipf("cannot reach JetStream NATS: %v", err)
	}
	defer bus.Close()

	const (
		N = 500 // launches
		W = 8   // executor workers
	)

	// Publish N distinct launches as fast as possible (burst).
	for i := 0; i < N; i++ {
		if err := bus.PublishExecutionRequest(&events.ExecutionRequest{
			ExecutionRunID: uuid.New(),
			UnifiedJobID:   int64(i),
		}); err != nil {
			t.Fatalf("publish %d: %v", i, err)
		}
	}

	ch, err := bus.SubscribeToExecutionRequests()
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	var (
		mu          sync.Mutex
		seen        = make(map[uuid.UUID]int)
		received    int64
		inflight    int32
		maxInflight int32
	)
	done := make(chan struct{})
	var once sync.Once
	var wg sync.WaitGroup

	for w := 0; w < W; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case req := <-ch:
					cur := atomic.AddInt32(&inflight, 1)
					for {
						m := atomic.LoadInt32(&maxInflight)
						if cur <= m || atomic.CompareAndSwapInt32(&maxInflight, m, cur) {
							break
						}
					}
					time.Sleep(3 * time.Millisecond) // simulate a slow bootstrap
					atomic.AddInt32(&inflight, -1)

					mu.Lock()
					seen[req.ExecutionRunID]++
					mu.Unlock()

					if atomic.AddInt64(&received, 1) == N {
						once.Do(func() { close(done) })
					}
				case <-done:
					return
				}
			}
		}()
	}

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatalf("timed out: received %d/%d under load", atomic.LoadInt64(&received), N)
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != N {
		t.Fatalf("distinct launches delivered = %d, want %d (some lost)", len(seen), N)
	}
	dups := 0
	for _, c := range seen {
		if c > 1 {
			dups++
		}
	}
	if dups > 0 {
		t.Fatalf("%d launch(es) delivered more than once — would double-bootstrap a host", dups)
	}
	if maxInflight > int32(W+1) {
		t.Fatalf("peak in-flight %d exceeded workers+1 (%d) — unbounded buffering", maxInflight, W+1)
	}
	t.Logf("OK: %d launches delivered exactly once under saturation; peak in-flight %d (workers=%d)", N, maxInflight, W)
}
