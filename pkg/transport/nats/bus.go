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
	Enc  *nats.EncodedConn
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

	ec, err := nats.NewEncodedConn(nc, nats.JSON_ENCODER)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("nats encoded conn failed: %w", err)
	}

	js, err := nc.JetStream()
	if err != nil {
		ec.Close()
		nc.Close()
		return nil, fmt.Errorf("jetstream init failed: %w", err)
	}

	bus := &NatsBus{Conn: nc, Enc: ec, JS: js}
	if err := bus.ensureStreams(); err != nil {
		ec.Close()
		nc.Close()
		return nil, err
	}
	return bus, nil
}

func (b *NatsBus) Close() {
	b.Enc.Close()
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

// SubscribeToExecutionRequests binds a durable, manual-ack work-queue consumer
// and returns a channel of launches. The interface is unchanged for callers, but
// delivery is now durable: a launch survives the executor being offline and is
// redelivered until acknowledged. The message is acked once it has been handed
// to a local worker (ack-on-receipt) — a crash before completion is recovered by
// the scheduler's stale-run reconciliation rather than by redelivery, which
// avoids double-bootstrapping a host. The queue group still load-balances across
// executors.
func (b *NatsBus) SubscribeToExecutionRequests() (<-chan events.ExecutionRequest, error) {
	ch := make(chan events.ExecutionRequest, 100)
	_, err := b.JS.QueueSubscribe(SubjectExecutionRequest, QueueGroupExecutor, func(msg *nats.Msg) {
		var req events.ExecutionRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			log.Printf("[executor] terminating undecodable execution request: %v", err)
			_ = msg.Term()
			return
		}
		ch <- req // backpressure: blocks (extending ack via AckWait) if workers are saturated
		_ = msg.Ack()
	},
		nats.Durable(DurableConsumerExecutor),
		nats.ManualAck(),
		nats.AckExplicit(),
		nats.DeliverAll(),
		nats.AckWait(60*time.Second),
	)
	if err != nil {
		return nil, err
	}
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
