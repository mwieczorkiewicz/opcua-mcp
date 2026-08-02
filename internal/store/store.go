// Package store provides bbolt-backed persistence for cached OPC-UA values,
// node type info, browse results, and subscription intent.
package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"
)

const (
	bucketValues        = "values"
	bucketTypeInfo      = "typeinfo"
	bucketBrowse        = "browse"
	bucketSubscriptions = "subscriptions"
)

// Store is a bbolt-backed key-value store with no expiry/TTL logic of its
// own - it returns whatever was last written, verbatim; freshness decisions
// belong entirely to callers. It relies solely on bbolt's own
// single-writer/MVCC-reader transaction model for concurrency safety - no
// additional locking is layered on top.
type Store struct {
	db *bolt.DB
}

// Open opens (creating if absent) the bbolt file at path, with timeout
// passed as bbolt.Options.Timeout. timeout must be non-zero: bbolt retries
// acquiring its file lock every 50ms forever when Timeout is 0, so a stale
// lock from a prior ungraceful shutdown would hang Open (and hence stdio
// startup) indefinitely.
func Open(path string, timeout time.Duration) (*Store, error) {
	db, err := bolt.Open(path, 0600, &bolt.Options{Timeout: timeout})
	if err != nil {
		return nil, fmt.Errorf("open store at %s: %w (a stale lock from a prior ungraceful shutdown is a common cause; check for another process holding the file)", path, err)
	}

	err = db.Update(func(tx *bolt.Tx) error {
		for _, bucket := range []string{bucketValues, bucketTypeInfo, bucketBrowse, bucketSubscriptions} {
			if _, err := tx.CreateBucketIfNotExists([]byte(bucket)); err != nil {
				return fmt.Errorf("create bucket %s: %w", bucket, err)
			}
		}
		return nil
	})
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	return &Store{db: db}, nil
}

// Close closes the underlying bbolt database. Callers must ensure this is
// called exactly once, after every other component using the Store has
// stopped.
func (s *Store) Close() error {
	return s.db.Close()
}

// get reads a single key from bucket into out (a pointer), returning
// (false, nil) for a missing key - a cache miss is an expected, common
// outcome, not an error.
func (s *Store) get(bucket, key string, out interface{}) (bool, error) {
	if key == "" {
		return false, fmt.Errorf("store: key must not be empty")
	}

	var raw []byte
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket([]byte(bucket)).Get([]byte(key))
		if v != nil {
			raw = append([]byte(nil), v...) // copy: v is only valid within the transaction
		}
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("store: read %s/%s: %w", bucket, key, err)
	}
	if raw == nil {
		return false, nil
	}

	if err := json.Unmarshal(raw, out); err != nil {
		return false, fmt.Errorf("store: decode %s/%s: %w", bucket, key, err)
	}
	return true, nil
}

// put writes entry (marshaled to JSON) into bucket under key.
func (s *Store) put(bucket, key string, entry interface{}) error {
	if key == "" {
		return fmt.Errorf("store: key must not be empty")
	}

	raw, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("store: encode %s/%s: %w", bucket, key, err)
	}

	err = s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte(bucket)).Put([]byte(key), raw)
	})
	if err != nil {
		return fmt.Errorf("store: write %s/%s: %w", bucket, key, err)
	}
	return nil
}

// delete removes key from bucket. Deleting a missing key is a no-op, not an error.
func (s *Store) delete(bucket, key string) error {
	if key == "" {
		return fmt.Errorf("store: key must not be empty")
	}

	err := s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte(bucket)).Delete([]byte(key))
	})
	if err != nil {
		return fmt.Errorf("store: delete %s/%s: %w", bucket, key, err)
	}
	return nil
}

// GetValue returns the cached value for nodeID, or (zero, false, nil) if absent.
func (s *Store) GetValue(ctx context.Context, nodeID string) (ValueEntry, bool, error) {
	if err := ctx.Err(); err != nil {
		return ValueEntry{}, false, err
	}

	var stored storedValueEntry
	ok, err := s.get(bucketValues, nodeID, &stored)
	if err != nil || !ok {
		return ValueEntry{}, ok, err
	}

	value, err := decode(stored.Value)
	if err != nil {
		return ValueEntry{}, false, fmt.Errorf("store: decode value for %s: %w", nodeID, err)
	}

	return ValueEntry{
		Value:           value,
		Status:          stored.Status,
		SourceTimestamp: stored.SourceTimestamp,
		ServerTimestamp: stored.ServerTimestamp,
		ReceivedAt:      stored.ReceivedAt,
		Source:          stored.Source,
	}, true, nil
}

// PutValue stores entry for nodeID. entry.Value's dynamic type must be one
// of the OPC-UA scalar types (or a []interface{} of them) encode supports;
// any other type fails fast rather than silently losing type fidelity.
func (s *Store) PutValue(ctx context.Context, nodeID string, entry ValueEntry) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	encoded, err := encode(entry.Value)
	if err != nil {
		return fmt.Errorf("store: encode value for %s: %w", nodeID, err)
	}

	stored := storedValueEntry{
		Value:           encoded,
		Status:          entry.Status,
		SourceTimestamp: entry.SourceTimestamp,
		ServerTimestamp: entry.ServerTimestamp,
		ReceivedAt:      entry.ReceivedAt,
		Source:          entry.Source,
	}
	return s.put(bucketValues, nodeID, stored)
}

// DeleteValue removes the cached value for nodeID.
func (s *Store) DeleteValue(ctx context.Context, nodeID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.delete(bucketValues, nodeID)
}

// GetTypeInfo returns the cached type info for nodeID, or (zero, false, nil) if absent.
func (s *Store) GetTypeInfo(ctx context.Context, nodeID string) (TypeInfoEntry, bool, error) {
	if err := ctx.Err(); err != nil {
		return TypeInfoEntry{}, false, err
	}
	var entry TypeInfoEntry
	ok, err := s.get(bucketTypeInfo, nodeID, &entry)
	return entry, ok, err
}

// PutTypeInfo stores entry for nodeID.
func (s *Store) PutTypeInfo(ctx context.Context, nodeID string, entry TypeInfoEntry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.put(bucketTypeInfo, nodeID, entry)
}

// GetBrowse returns the cached browse result for parentNodeID, or (zero, false, nil) if absent.
func (s *Store) GetBrowse(ctx context.Context, parentNodeID string) (BrowseEntry, bool, error) {
	if err := ctx.Err(); err != nil {
		return BrowseEntry{}, false, err
	}
	var entry BrowseEntry
	ok, err := s.get(bucketBrowse, parentNodeID, &entry)
	return entry, ok, err
}

// PutBrowse stores entry for parentNodeID.
func (s *Store) PutBrowse(ctx context.Context, parentNodeID string, entry BrowseEntry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.put(bucketBrowse, parentNodeID, entry)
}

// GetSubscription returns the persisted intent for id, or (zero, false, nil) if absent.
func (s *Store) GetSubscription(ctx context.Context, id string) (SubscriptionIntent, bool, error) {
	if err := ctx.Err(); err != nil {
		return SubscriptionIntent{}, false, err
	}
	var intent SubscriptionIntent
	ok, err := s.get(bucketSubscriptions, id, &intent)
	return intent, ok, err
}

// PutSubscription stores intent for id.
func (s *Store) PutSubscription(ctx context.Context, id string, intent SubscriptionIntent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.put(bucketSubscriptions, id, intent)
}

// DeleteSubscription removes the persisted intent for id.
func (s *Store) DeleteSubscription(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.delete(bucketSubscriptions, id)
}

// ListSubscriptions returns every persisted subscription intent.
func (s *Store) ListSubscriptions(ctx context.Context) ([]SubscriptionIntent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var intents []SubscriptionIntent
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte(bucketSubscriptions)).ForEach(func(k, v []byte) error {
			var intent SubscriptionIntent
			if err := json.Unmarshal(v, &intent); err != nil {
				return fmt.Errorf("store: decode subscription %s: %w", k, err)
			}
			intents = append(intents, intent)
			return nil
		})
	})
	if err != nil {
		return nil, fmt.Errorf("store: list subscriptions: %w", err)
	}
	return intents, nil
}
