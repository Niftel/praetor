// Package objectstore provides durable blob storage for bulk job output (raw
// playbook stdout), keeping it off the control-plane database. Logs are large,
// append-heavy, and unbounded, so the database stores only references
// (job_output_chunks rows) while the bytes live here.
//
// The default implementation is backed by the NATS JetStream Object Store, so a
// Praetor deployment needs no storage dependency beyond the NATS it already
// runs for the event pipeline. The LogStore interface keeps callers decoupled
// from that choice — an S3-backed implementation can be dropped in for hosted
// deployments without touching producers or consumers.
package objectstore

import (
	"fmt"

	"github.com/nats-io/nats.go"
)

// DefaultBucket is the JetStream object-store bucket holding job output chunks.
const DefaultBucket = "PRAETOR_LOGS"

// LogStore is durable blob storage for job output chunks. Keys are content
// paths of the form "<execution_run_id>/<seq>".
type LogStore interface {
	Put(key string, data []byte) error
	Get(key string) ([]byte, error)
}

// JetStreamLogStore implements LogStore over the NATS JetStream Object Store.
type JetStreamLogStore struct {
	os nats.ObjectStore
}

// NewJetStreamLogStore binds (creating if necessary) the file-backed object
// store bucket used for job output.
func NewJetStreamLogStore(js nats.JetStreamContext, bucket string) (*JetStreamLogStore, error) {
	if bucket == "" {
		bucket = DefaultBucket
	}

	store, err := js.ObjectStore(bucket)
	if err != nil {
		// Bucket does not exist yet — create it.
		store, err = js.CreateObjectStore(&nats.ObjectStoreConfig{
			Bucket:      bucket,
			Description: "Praetor job output chunks",
			Storage:     nats.FileStorage,
		})
		if err != nil {
			return nil, fmt.Errorf("bind/create object store bucket %s: %w", bucket, err)
		}
	}
	return &JetStreamLogStore{os: store}, nil
}

func (s *JetStreamLogStore) Put(key string, data []byte) error {
	if _, err := s.os.PutBytes(key, data); err != nil {
		return fmt.Errorf("object store put %s: %w", key, err)
	}
	return nil
}

func (s *JetStreamLogStore) Get(key string) ([]byte, error) {
	data, err := s.os.GetBytes(key)
	if err != nil {
		return nil, fmt.Errorf("object store get %s: %w", key, err)
	}
	return data, nil
}

// ChunkKey builds the object-store key for a given run and chunk sequence. It is
// the same value stored in job_output_chunks.storage_key, so the index and the
// blob always agree.
func ChunkKey(executionRunID string, seq int64) string {
	return fmt.Sprintf("%s/%d", executionRunID, seq)
}
