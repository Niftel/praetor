package core

import (
	"context"
	"log"

	"github.com/praetordev/praetor/pkg/events"
)

// EventConsumer delivers job events to a handler and acknowledges each one
// only when the handler returns nil. A non-nil return keeps the event in the
// durable stream for redelivery, so the database is the gate on acknowledgement.
type EventConsumer interface {
	ConsumeJobEvents(handler func(events.JobEvent) error) error
}

type Consumer struct {
	Subscriber EventConsumer
	Writer     *DBWriter
}

func NewConsumer(sub EventConsumer, writer *DBWriter) *Consumer {
	return &Consumer{
		Subscriber: sub,
		Writer:     writer,
	}
}

func (c *Consumer) Start() error {
	log.Println("Consumer started, waiting for events...")

	// The handler's error is the ack signal: returning nil acks the message,
	// returning an error (e.g. the DB is unavailable) leaves it in the stream
	// for redelivery once we recover.
	err := c.Subscriber.ConsumeJobEvents(func(evt events.JobEvent) error {
		if err := c.processEvent(evt); err != nil {
			log.Printf("Error processing event %d for run %s (will retry): %v", evt.Seq, evt.ExecutionRunID, err)
			return err
		}
		log.Printf("Processed event %s (Seq: %d) for Job %d", evt.EventType, evt.Seq, evt.UnifiedJobID)
		return nil
	})
	if err != nil {
		return err
	}

	// Delivery runs on background goroutines; block forever.
	select {}
}

func (c *Consumer) processEvent(evt events.JobEvent) error {
	// Delegate to DBWriter
	ctx := context.Background()
	return c.Writer.WriteEvent(ctx, evt)
}
