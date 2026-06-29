package nats

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/praetordev/praetor/pkg/events"
)

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

	// eventRetention bounds how long undelivered events are retained in the
	// stream. It must comfortably exceed any expected control-plane outage.
	eventRetention = 72 * time.Hour
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

// ensureStreams creates (or reconciles) the durable, file-backed stream that
// captures every job event and log chunk published to the bus.
func (b *NatsBus) ensureStreams() error {
	cfg := &nats.StreamConfig{
		Name:      StreamEvents,
		Subjects:  []string{SubjectJobEvent, SubjectLogChunk},
		Storage:   nats.FileStorage,
		Retention: nats.LimitsPolicy,
		MaxAge:    eventRetention,
		Replicas:  1, // raise to 3 on a clustered deployment
	}

	if _, err := b.JS.StreamInfo(StreamEvents); err != nil {
		if _, err := b.JS.AddStream(cfg); err != nil {
			return fmt.Errorf("create stream %s: %w", StreamEvents, err)
		}
		log.Printf("[nats] created durable JetStream stream %s (subjects=%v)", StreamEvents, cfg.Subjects)
		return nil
	}
	if _, err := b.JS.UpdateStream(cfg); err != nil {
		return fmt.Errorf("update stream %s: %w", StreamEvents, err)
	}
	return nil
}

// -- Publisher Implementation --

// PublishExecutionRequest dispatches a job to executors. This remains a core
// (non-durable) NATS publish; durable launch delivery is tracked separately as
// a follow-up (outbox pattern) and is out of scope for the event pipeline.
func (b *NatsBus) PublishExecutionRequest(req *events.ExecutionRequest) error {
	return b.Enc.Publish(SubjectExecutionRequest, req)
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

func (b *NatsBus) SubscribeToExecutionRequests() (<-chan events.ExecutionRequest, error) {
	ch := make(chan events.ExecutionRequest, 100)
	// Queue Subscribe ensures load balancing if we run multiple executors.
	_, err := b.Enc.QueueSubscribe(SubjectExecutionRequest, QueueGroupExecutor, func(req *events.ExecutionRequest) {
		ch <- *req
	})
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
