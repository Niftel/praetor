package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/praetordev/events"
)

// seqCounter is a durable, goroutine-safe monotonic sequence for a run's events.
// It is initialised from the highest seq already present in the WAL so that a
// resumed invocation (e.g. after a host reboot) continues numbering past the
// events the previous run already shipped, rather than restarting at 1 and
// colliding on the (execution_run_id, seq) unique constraint — which would make
// the resume's events silently dropped by the consumer's ON CONFLICT DO NOTHING.
type seqCounter struct{ n int64 }

func newSeqCounter(walPath string) *seqCounter {
	return &seqCounter{n: maxWALSeq(walPath)}
}

func (s *seqCounter) next() int64    { return atomic.AddInt64(&s.n, 1) }
func (s *seqCounter) current() int64 { return atomic.LoadInt64(&s.n) }

// maxWALSeq returns the highest seq recorded in an events WAL, or 0 if the file
// is absent or empty. Corrupt/partial trailing lines are ignored.
func maxWALSeq(path string) int64 {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	var max int64
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var e events.JobEvent
		if json.Unmarshal(sc.Bytes(), &e) == nil && e.Seq > max {
			max = e.Seq
		}
	}
	return max
}

// watchCheckpoints polls the run's checkpoint.json while the play runs and emits
// a CHECKPOINT_SAVED lifecycle event each time the checkpoint advances to a new
// task. This surfaces Praetor's task-level durability on the run timeline: every
// event marks a point the play can be resumed from after an interruption. It
// returns when stop is closed, after one final poll so the last checkpoint is
// never missed.
func watchCheckpoints(jobDir string, req *events.ExecutionRequest, wal *WAL, seq *seqCounter, host string, stop <-chan struct{}) {
	cpPath := filepath.Join(jobDir, "checkpoint.json")
	var last string
	count := 0

	poll := func() {
		data, err := os.ReadFile(cpPath)
		if err != nil {
			return
		}
		var cp struct {
			ResumeAt string `json:"resume_at"`
		}
		if json.Unmarshal(data, &cp) != nil || cp.ResumeAt == "" || cp.ResumeAt == last {
			return
		}
		last = cp.ResumeAt
		count++

		msg := fmt.Sprintf("Checkpoint saved — resumable at %q", cp.ResumeAt)
		ed, _ := json.Marshal(map[string]interface{}{"resume_at": cp.ResumeAt, "count": count, "host": host})
		task := cp.ResumeAt
		if err := wal.Append(&events.JobEvent{
			ExecutionRunID: req.ExecutionRunID,
			UnifiedJobID:   req.UnifiedJobID,
			Seq:            seq.next(),
			EventType:      events.EventCheckpointSaved,
			Timestamp:      time.Now(),
			Host:           &host,
			TaskName:       &task,
			StdoutSnippet:  &msg,
			EventData:      ed,
		}); err != nil {
			log.Printf("Warning: failed to write CHECKPOINT_SAVED event: %v", err)
		}
	}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			poll() // final poll to catch the last checkpoint before we exit
			return
		case <-ticker.C:
			poll()
		}
	}
}
