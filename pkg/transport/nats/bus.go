package nats

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/praetordev/praetor/pkg/events"
)

// DefaultURL is the local-dev NATS address, used by mains that resolve NATS_URL
// via pkg/env. In-cluster deployments set NATS_URL to the service address.
const DefaultURL = "nats://127.0.0.1:4222"

const (
	SubjectExecutionRequest = "job.requests"
	SubjectJobEvent         = "job.events"
	SubjectLogChunk         = "job.logs"
	QueueGroupExecutor      = "executor-group"
	QueueGroupConsumer      = "consumer-group"

	// StreamEvents is the durable JetStream stream backing the job-event
	// pipeline. Persisting events to disk is what lets a consumer or database
	// outage be tolerated without data loss: the durable consumer resumes from
	// its last acknowledged message once the downstream recovers.
	StreamEvents          = "PRAETOR_EVENTS"
	DurableConsumerEvents = "praetor-event-consumer"
	DurableConsumerLogs   = "praetor-logchunk-consumer"

	// StreamRequests is a durable work-queue stream for job launches. Unlike
	// core NATS (where a request published while no executor is connected is
	// lost), a launch is retained until an executor consumes and acks it. A
	// dedup window makes at-least-once delivery from the scheduler's outbox
	// relay safe: a re-published launch is stored once.
	StreamRequests          = "PRAETOR_REQUESTS"
	DurableConsumerExecutor = "praetor-executor"

	// eventRetention bounds how long undelivered events are retained in the
	// stream. It must comfortably exceed any expected control-plane outage.
	eventRetention = 72 * time.Hour
	// requestDedup is the JetStream duplicate-suppression window keyed on the
	// execution_run_id message id.
	requestDedup = 5 * time.Minute
)

type NatsBus struct {
	Conn *nats.Conn
	JS   nats.JetStreamContext
}

func NewNatsBus(url string) (*NatsBus, error) {
	// Reconnect forever so a NATS restart never permanently severs producers
	// or consumers; buffered messages are flushed on reconnect.
	nc, err := nats.Connect(url,
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("nats connect failed: %w", err)
	}

	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("jetstream init failed: %w", err)
	}

	bus := &NatsBus{Conn: nc, JS: js}
	if err := bus.ensureStreams(); err != nil {
		nc.Close()
		return nil, err
	}
	return bus, nil
}

func (b *NatsBus) Close() {
	b.Conn.Close()
}

// ensureStreams creates (or reconciles) the durable, file-backed streams that
// back the event pipeline and the job-launch queue.
func (b *NatsBus) ensureStreams() error {
	streams := []*nats.StreamConfig{
		{
			Name:      StreamEvents,
			Subjects:  []string{SubjectJobEvent, SubjectLogChunk},
			Storage:   nats.FileStorage,
			Retention: nats.LimitsPolicy,
			MaxAge:    eventRetention,
			Replicas:  1, // raise to 3 on a clustered deployment
		},
		{
			Name:       StreamRequests,
			Subjects:   []string{SubjectExecutionRequest},
			Storage:    nats.FileStorage,
			Retention:  nats.WorkQueuePolicy, // a launch is removed once an executor acks it
			MaxAge:     eventRetention,
			Duplicates: requestDedup,
			Replicas:   1,
		},
	}

	for _, cfg := range streams {
		if _, err := b.JS.StreamInfo(cfg.Name); err != nil {
			if _, err := b.JS.AddStream(cfg); err != nil {
				return fmt.Errorf("create stream %s: %w", cfg.Name, err)
			}
			log.Printf("[nats] created durable JetStream stream %s (subjects=%v)", cfg.Name, cfg.Subjects)
			continue
		}
		if _, err := b.JS.UpdateStream(cfg); err != nil {
			return fmt.Errorf("update stream %s: %w", cfg.Name, err)
		}
	}
	return nil
}

// -- Publisher Implementation --

// PublishExecutionRequest durably enqueues a job launch. It publishes through
// JetStream with the execution_run_id as the dedup message id, so the
// scheduler's outbox relay can republish on retry without ever enqueuing the
// same launch twice (within the dedup window).
func (b *NatsBus) PublishExecutionRequest(req *events.ExecutionRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal execution request: %w", err)
	}
	if _, err := b.JS.Publish(SubjectExecutionRequest, data, nats.MsgId(req.ExecutionRunID.String())); err != nil {
		return fmt.Errorf("jetstream publish execution request: %w", err)
	}
	return nil
}

// PublishJobEvent publishes through JetStream and blocks for the stream's
// persistence ack, so the caller (e.g. the ingestion endpoint, whose 2xx tells
// the host-runner syncer it may advance its cursor) only sees success once the
// event is durably stored.
func (b *NatsBus) PublishJobEvent(event *events.JobEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal job event: %w", err)
	}
	if _, err := b.JS.Publish(SubjectJobEvent, data); err != nil {
		return fmt.Errorf("jetstream publish job event: %w", err)
	}
	return nil
}

func (b *NatsBus) PublishLogChunk(chunk *events.LogChunk) error {
	data, err := json.Marshal(chunk)
	if err != nil {
		return fmt.Errorf("marshal log chunk: %w", err)
	}
	if _, err := b.JS.Publish(SubjectLogChunk, data); err != nil {
		return fmt.Errorf("jetstream publish log chunk: %w", err)
	}
	return nil
}

// -- Subscriber Implementation --

// SubscribeToExecutionRequests binds a durable, manual-ack PULL consumer and
// returns a channel of launches. A pull consumer is deliberate: with the previous
// push queue-subscriber, a delivered message's AckWait ran from delivery and was
// NOT extended by the callback blocking on a full worker pool, so under sustained
// saturation JetStream redelivered launches that were still buffered — double-
// bootstrapping a host. A pull consumer only fetches when a worker is free, so a
// launch is never "in flight but waiting", and there is no 100-deep buffer whose
// acked-but-unprocessed contents are lost on a crash.
//
// A single puller feeds an UNBUFFERED channel that the executor's worker pool
// drains, so it fetches exactly as fast as workers accept. Each message is acked
// on receipt (before hand-off) — a crash mid-bootstrap is recovered by the
// scheduler's stale-run reconciliation, not by redelivery (which would double-
// bootstrap). At most len(workers)+1 launches are in flight, so a crash loses at
// most that many (all recovered by the reconciler), versus up to 100 before.
// Multiple executors sharing the durable pull work natively (no queue group).
func (b *NatsBus) SubscribeToExecutionRequests() (<-chan events.ExecutionRequest, error) {
	// A durable consumer can't switch between push and pull in place, so the OLD
	// push queue-subscriber has to be dropped before PullSubscribe can recreate it
	// as pull. But deleting unconditionally is unsafe with >1 executor: a second
	// executor starting up would delete the shared durable consumer out from under
	// the first one mid-fetch. So only delete when the existing consumer is actually
	// the legacy push type (it has a DeliverSubject); a compatible pull consumer is
	// left in place and simply re-bound below. This makes startup idempotent and
	// safe to run with multiple executors sharing the durable pull consumer.
	if info, ierr := b.JS.ConsumerInfo(StreamRequests, DurableConsumerExecutor); ierr == nil {
		if info.Config.DeliverSubject != "" { // push consumer — incompatible, must recreate
			_ = b.JS.DeleteConsumer(StreamRequests, DurableConsumerExecutor)
		}
	}

	sub, err := b.JS.PullSubscribe(SubjectExecutionRequest, DurableConsumerExecutor,
		nats.ManualAck(),
		nats.AckExplicit(),
		nats.DeliverAll(),
		nats.AckWait(60*time.Second),
	)
	if err != nil {
		return nil, err
	}

	ch := make(chan events.ExecutionRequest) // unbuffered: fetch only when a worker is ready
	go func() {
		for {
			msgs, ferr := sub.Fetch(1, nats.MaxWait(5*time.Second))
			if ferr != nil {
				if ferr == nats.ErrTimeout {
					continue // no work this window; poll again
				}
				log.Printf("[executor] pull fetch error: %v", ferr)
				time.Sleep(time.Second) // back off on transient errors
				continue
			}
			for _, msg := range msgs {
				var req events.ExecutionRequest
				if err := json.Unmarshal(msg.Data, &req); err != nil {
					log.Printf("[executor] terminating undecodable execution request: %v", err)
					_ = msg.Term()
					continue
				}
				_ = msg.Ack() // ack on receipt; recovery is the reconciler's job
				ch <- req     // blocks until a worker takes it — natural backpressure
			}
		}
	}()
	return ch, nil
}

// ConsumeJobEvents binds a durable, manual-ack JetStream consumer and invokes
// handler for each event. The message is acknowledged only when handler returns
// nil; on error it is negatively acknowledged (with a short backoff) and later
// redelivered. This is the mechanism by which a database outage is survived:
// events stay in the stream and are replayed once the handler can commit them.
//
// The call is non-blocking — delivery happens on NATS' background goroutines —
// so callers that want to run forever should block after invoking it.
func (b *NatsBus) ConsumeJobEvents(handler func(events.JobEvent) error) error {
	_, err := b.JS.QueueSubscribe(SubjectJobEvent, QueueGroupConsumer, func(msg *nats.Msg) {
		var evt events.JobEvent
		if err := json.Unmarshal(msg.Data, &evt); err != nil {
			// Undecodable payload will never succeed; terminate so it is not
			// redelivered forever.
			log.Printf("[consumer] terminating undecodable event: %v", err)
			_ = msg.Term()
			return
		}
		if err := handler(evt); err != nil {
			// Keep the event in the stream and retry shortly.
			_ = msg.NakWithDelay(2 * time.Second)
			return
		}
		_ = msg.Ack()
	},
		nats.Durable(DurableConsumerEvents),
		nats.ManualAck(),
		nats.AckExplicit(),
		nats.DeliverAll(),
		nats.AckWait(30*time.Second),
		nats.MaxAckPending(256),
	)
	return err
}

// ConsumeLogChunks binds a durable, manual-ack consumer for log-chunk index
// notifications (the bytes themselves already live in the object store). Same
// ack-after-commit contract as ConsumeJobEvents: the index write into
// job_output_chunks gates acknowledgement, so a DB outage is tolerated.
func (b *NatsBus) ConsumeLogChunks(handler func(events.LogChunk) error) error {
	_, err := b.JS.QueueSubscribe(SubjectLogChunk, QueueGroupConsumer, func(msg *nats.Msg) {
		var chunk events.LogChunk
		if err := json.Unmarshal(msg.Data, &chunk); err != nil {
			log.Printf("[consumer] terminating undecodable log chunk: %v", err)
			_ = msg.Term()
			return
		}
		if err := handler(chunk); err != nil {
			_ = msg.NakWithDelay(2 * time.Second)
			return
		}
		_ = msg.Ack()
	},
		nats.Durable(DurableConsumerLogs),
		nats.ManualAck(),
		nats.AckExplicit(),
		nats.DeliverAll(),
		nats.AckWait(30*time.Second),
		nats.MaxAckPending(256),
	)
	return err
}
