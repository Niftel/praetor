package core

import (
	"strings"
	"testing"
	"time"
)

func TestFilterNewEvents(t *testing.T) {
	// events.jsonl with seqs 1..4, a blank line, and one corrupt record.
	raw := strings.Join([]string{
		`{"seq":1,"event_type":"JOB_STARTED"}`,
		`{"seq":2,"event_type":"RUNNER_ON_OK"}`,
		``,
		`{"seq":3,"event_type":"RUNNER_ON_OK"}`,
		`{not json`,
		`{"seq":4,"event_type":"JOB_COMPLETED"}`,
	}, "\n")

	batch, maxSeq := filterNewEvents(raw, 2)
	if maxSeq != 4 {
		t.Fatalf("maxSeq = %d, want 4", maxSeq)
	}
	// Only seq 3 and 4 are new (> persisted 2); the corrupt line is skipped.
	if len(batch) != 2 {
		t.Fatalf("len(batch) = %d, want 2 (%+v)", len(batch), batch)
	}
	if batch[0].Seq != 3 || batch[1].Seq != 4 {
		t.Fatalf("batch seqs = %d,%d, want 3,4", batch[0].Seq, batch[1].Seq)
	}
}

func TestFilterNewEvents_NothingNew(t *testing.T) {
	raw := `{"seq":1}` + "\n" + `{"seq":2}`
	batch, maxSeq := filterNewEvents(raw, 5)
	if len(batch) != 0 {
		t.Fatalf("expected no new events, got %d", len(batch))
	}
	// maxSeq never drops below the persisted floor.
	if maxSeq != 5 {
		t.Fatalf("maxSeq = %d, want 5", maxSeq)
	}
}

func TestSplitChunks(t *testing.T) {
	data := []byte("aaaaabbbbbcc") // 12 bytes, chunk at 5
	chunks := splitChunks(data, 5)
	if len(chunks) != 3 {
		t.Fatalf("len(chunks) = %d, want 3", len(chunks))
	}
	if string(chunks[0]) != "aaaaa" || string(chunks[1]) != "bbbbb" || string(chunks[2]) != "cc" {
		t.Fatalf("unexpected chunk boundaries: %q %q %q", chunks[0], chunks[1], chunks[2])
	}
	// Reassembly is lossless and order-preserving.
	var joined []byte
	for _, c := range chunks {
		joined = append(joined, c...)
	}
	if string(joined) != string(data) {
		t.Fatalf("reassembled %q, want %q", joined, data)
	}
	if len(splitChunks(nil, 5)) != 0 {
		t.Fatalf("empty input should yield no chunks")
	}
}

func TestIsTerminal(t *testing.T) {
	for _, s := range []string{"successful", "failed"} {
		if !isTerminal(s) {
			t.Fatalf("%q should be terminal", s)
		}
	}
	for _, s := range []string{"running", "reconciling", "lost", "pending", ""} {
		if isTerminal(s) {
			t.Fatalf("%q should not be terminal", s)
		}
	}
}

func TestBackoffDelay(t *testing.T) {
	max := 5 * time.Minute
	// Exponential 30s doubling: 60s, 120s, 240s, 480s(capped), ...
	cases := []struct {
		attempts int
		want     time.Duration
	}{
		{1, 60 * time.Second},
		{2, 120 * time.Second},
		{3, 240 * time.Second},
		{4, max}, // 480s capped to 300s
		{10, max},
	}
	for _, c := range cases {
		if got := backoffDelay(c.attempts, max); got != c.want {
			t.Fatalf("backoffDelay(%d) = %s, want %s", c.attempts, got, c.want)
		}
	}
}
