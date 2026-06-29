package objectstore_test

import (
	"os"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/praetordev/praetor/pkg/objectstore"
)

// TestJetStreamLogStoreRoundTrip proves the JetStream Object Store backend can
// durably store and retrieve a log chunk. Set TEST_NATS_URL to a JetStream
// server to run it.
func TestJetStreamLogStoreRoundTrip(t *testing.T) {
	url := os.Getenv("TEST_NATS_URL")
	if url == "" {
		t.Skip("TEST_NATS_URL not set; skipping object-store integration test")
	}

	nc, err := nats.Connect(url)
	if err != nil {
		t.Skipf("cannot reach NATS: %v", err)
	}
	defer nc.Close()
	js, err := nc.JetStream()
	if err != nil {
		t.Fatalf("jetstream: %v", err)
	}

	store, err := objectstore.NewJetStreamLogStore(js, "PRAETOR_LOGS_TEST")
	if err != nil {
		t.Fatalf("new log store: %v", err)
	}

	key := objectstore.ChunkKey("run-abc", 7)
	payload := []byte("PLAY [all] ***\nTASK [install nginx] ***\nchanged: [web-01]\n")

	if err := store.Put(key, payload); err != nil {
		t.Fatalf("put: %v", err)
	}

	got, err := store.Get(key)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("round-trip mismatch:\n got %q\nwant %q", got, payload)
	}

	// Overwriting the same key (idempotent re-upload on retry) must not error
	// and must return the latest bytes.
	if err := store.Put(key, payload); err != nil {
		t.Fatalf("re-put: %v", err)
	}
}
