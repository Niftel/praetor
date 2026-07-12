package core_test

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/praetordev/events"
	"github.com/praetordev/objectstore"
	natsbus "github.com/praetordev/praetor/pkg/transport/nats"
	consumercore "github.com/praetordev/praetor/services/consumer/core"
	"github.com/praetordev/praetor/services/ingestion/core"
)

// TestLogChunkEndToEnd exercises the full log path: ingestion stores the bytes
// in the JetStream Object Store and publishes a LogChunk, which the consumer
// indexes into job_output_chunks. It asserts the bytes are retrievable from the
// object store and the DB holds the matching reference.
//
// Requires TEST_DATABASE_URL (migrated) and a JetStream TEST_NATS_URL.
func TestLogChunkEndToEnd(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	natsURL := os.Getenv("TEST_NATS_URL")
	if dbURL == "" || natsURL == "" {
		t.Skip("TEST_DATABASE_URL and TEST_NATS_URL required; skipping log-chunk integration test")
	}

	db, err := sqlx.Connect("postgres", dbURL)
	if err != nil {
		t.Skipf("cannot reach TEST_DATABASE_URL: %v", err)
	}
	defer db.Close()

	bus, err := natsbus.NewNatsBus(natsURL)
	if err != nil {
		t.Skipf("cannot reach JetStream NATS: %v", err)
	}
	defer bus.Close()

	store, err := objectstore.NewJetStreamLogStore(bus.JS, "")
	if err != nil {
		t.Fatalf("log store: %v", err)
	}
	svc := core.NewIngestionService(db, bus, store)

	// Fixture: a job + run so the job_output_chunks FK is satisfied.
	var jobID int64
	if err := db.QueryRow(`INSERT INTO unified_jobs (name, status) VALUES ('logchunk-test', 'running') RETURNING id`).Scan(&jobID); err != nil {
		t.Fatalf("insert job: %v", err)
	}
	runID := uuid.New()
	if _, err := db.Exec(`INSERT INTO execution_runs (id, unified_job_id, state) VALUES ($1, $2, 'running')`, runID, jobID); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM unified_jobs WHERE id = $1`, jobID) })

	// Stand up the consumer's indexing path.
	writer := consumercore.NewDBWriter(db)
	indexed := make(chan int64, 4)
	if err := bus.ConsumeLogChunks(func(c events.LogChunk) error {
		if err := writer.WriteLogChunk(context.Background(), c); err != nil {
			return err
		}
		indexed <- c.Seq
		return nil
	}); err != nil {
		t.Fatalf("consume log chunks: %v", err)
	}

	// Ingest a chunk.
	payload := []byte("PLAY [all] ***\nTASK [Gathering Facts] ***\nok: [web-01]\n")
	if err := svc.IngestLogChunk(context.Background(), runID, 0, payload); err != nil {
		t.Fatalf("ingest log chunk: %v", err)
	}

	// The bytes are durably in the object store.
	got, err := store.Get(objectstore.ChunkKey(runID.String(), 0))
	if err != nil {
		t.Fatalf("object store get: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("object store round-trip mismatch:\n got %q\nwant %q", got, payload)
	}

	// The consumer indexed the reference.
	select {
	case <-indexed:
	case <-time.After(5 * time.Second):
		t.Fatal("log chunk was not indexed by the consumer")
	}

	var storageKey string
	var byteLen int
	if err := db.QueryRow(
		`SELECT storage_key, byte_length FROM job_output_chunks WHERE execution_run_id = $1 AND seq = 0`,
		runID,
	).Scan(&storageKey, &byteLen); err != nil {
		t.Fatalf("query job_output_chunks: %v", err)
	}
	if storageKey != objectstore.ChunkKey(runID.String(), 0) {
		t.Fatalf("indexed storage_key %q != %q", storageKey, objectstore.ChunkKey(runID.String(), 0))
	}
	if byteLen != len(payload) {
		t.Fatalf("indexed byte_length %d != %d", byteLen, len(payload))
	}

	// Ingest a second chunk and wait for it to be indexed.
	payload2 := []byte("TASK [install nginx] ***\nchanged: [web-01]\nPLAY RECAP ***\n")
	if err := svc.IngestLogChunk(context.Background(), runID, 1, payload2); err != nil {
		t.Fatalf("ingest chunk 1: %v", err)
	}
	select {
	case <-indexed:
	case <-time.After(5 * time.Second):
		t.Fatal("second log chunk was not indexed")
	}

	// Read-back: the full log is the chunks reassembled in order.
	var buf bytes.Buffer
	if err := svc.StreamLogs(context.Background(), runID, -1, &buf); err != nil {
		t.Fatalf("stream logs: %v", err)
	}
	if got, want := buf.String(), string(payload)+string(payload2); got != want {
		t.Fatalf("full log read-back mismatch:\n got %q\nwant %q", got, want)
	}

	// Incremental tail: since=0 must return only the chunk after seq 0.
	buf.Reset()
	if err := svc.StreamLogs(context.Background(), runID, 0, &buf); err != nil {
		t.Fatalf("stream logs since=0: %v", err)
	}
	if got := buf.String(); got != string(payload2) {
		t.Fatalf("tail read-back mismatch:\n got %q\nwant %q", got, payload2)
	}

	if latest, err := svc.LatestLogSeq(context.Background(), runID); err != nil || latest != 1 {
		t.Fatalf("LatestLogSeq = %d, %v; want 1, nil", latest, err)
	}
}
