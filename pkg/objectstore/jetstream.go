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
	"time"

	"github.com/nats-io/nats.go"
	"github.com/praetordev/env"
)

// DefaultBucket is the JetStream object-store bucket holding job output chunks.
const DefaultBucket = "PRAETOR_LOGS"

// Log output is large and append-heavy, so the bucket is bounded by default to
// keep JetStream's file store from growing without limit (issue #17). Both bounds
// are env-tunable; 0 disables that bound.
//   - PRAETOR_LOG_STORE_MAX_MB       : hard size cap (MiB). Default 5 GiB.
//   - PRAETOR_LOG_STORE_MAX_AGE_DAYS : age cap. Default 0 (age is handled by the
//     scheduler's retention prune, which also removes the DB rows).
const (
	defaultLogStoreMaxMB   = 5120 // 5 GiB
	defaultLogStoreMaxDays = 0
)

// logStoreBounds resolves the configured (maxBytes, ttl) for the log bucket.
func logStoreBounds() (int64, time.Duration) {
	maxBytes := int64(env.Int("PRAETOR_LOG_STORE_MAX_MB", defaultLogStoreMaxMB)) * 1024 * 1024
	ttl := time.Duration(env.Int("PRAETOR_LOG_STORE_MAX_AGE_DAYS", defaultLogStoreMaxDays)) * 24 * time.Hour
	return maxBytes, ttl
}

// LogStore is durable blob storage for job output chunks. Keys are content
// paths of the form "<execution_run_id>/<seq>".
type LogStore interface {
	Put(key string, data []byte) error
	Get(key string) ([]byte, error)
	// Delete removes a stored blob. Deleting a missing key is not an error, so
	// retention pruning is idempotent.
	Delete(key string) error
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

	maxBytes, ttl := logStoreBounds()

	store, err := js.ObjectStore(bucket)
	if err != nil {
		// Bucket does not exist yet — create it with the configured bounds.
		store, err = js.CreateObjectStore(&nats.ObjectStoreConfig{
			Bucket:      bucket,
			Description: "Praetor job output chunks",
			Storage:     nats.FileStorage,
			MaxBytes:    maxBytes,
			TTL:         ttl,
		})
		if err != nil {
			return nil, fmt.Errorf("bind/create object store bucket %s: %w", bucket, err)
		}
	} else {
		// Bucket already exists — reconcile the bounds so an older, unbounded
		// deployment picks up the cap. The object store is backed by the stream
		// OBJ_<bucket>; update it in place. Best-effort: a failure here must not
		// stop the service from serving logs.
		reconcileBucketBounds(js, bucket, maxBytes, ttl)
	}
	return &JetStreamLogStore{os: store}, nil
}

// reconcileBucketBounds sets MaxBytes/MaxAge on the stream backing an existing
// object-store bucket, so a bucket created before the cap existed becomes bounded
// without data loss (shrinking MaxBytes drops the oldest chunks, which is the
// intended retention behaviour).
func reconcileBucketBounds(js nats.JetStreamContext, bucket string, maxBytes int64, ttl time.Duration) {
	stream := "OBJ_" + bucket
	si, err := js.StreamInfo(stream)
	if err != nil {
		return
	}
	cfg := si.Config
	changed := false
	if cfg.MaxBytes != maxBytes {
		cfg.MaxBytes = maxBytes
		changed = true
	}
	if cfg.MaxAge != ttl {
		cfg.MaxAge = ttl
		changed = true
	}
	if changed {
		_, _ = js.UpdateStream(&cfg)
	}
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

func (s *JetStreamLogStore) Delete(key string) error {
	if err := s.os.Delete(key); err != nil && err != nats.ErrObjectNotFound {
		return fmt.Errorf("object store delete %s: %w", key, err)
	}
	return nil
}

// ChunkKey builds the object-store key for a given run and chunk sequence. It is
// the same value stored in job_output_chunks.storage_key, so the index and the
// blob always agree.
func ChunkKey(executionRunID string, seq int64) string {
	return fmt.Sprintf("%s/%d", executionRunID, seq)
}
