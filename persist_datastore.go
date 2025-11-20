package bdcache

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/codeGROOVE-dev/ds9/pkg/datastore"
)

const (
	datastoreKind     = "CacheEntry"
	maxDatastoreKeyLen = 1500 // Datastore has stricter key length limits
)

// datastorePersist implements PersistenceLayer using Google Cloud Datastore.
type datastorePersist[K comparable, V any] struct {
	client *datastore.Client
	kind   string
}

// ValidateKey checks if a key is valid for Datastore persistence.
// Datastore has stricter key length limits than files.
func (d *datastorePersist[K, V]) ValidateKey(key K) error {
	keyStr := fmt.Sprintf("%v", key)
	if len(keyStr) > maxDatastoreKeyLen {
		return fmt.Errorf("key too long: %d bytes (max %d for datastore)", len(keyStr), maxDatastoreKeyLen)
	}
	if len(keyStr) == 0 {
		return fmt.Errorf("key cannot be empty")
	}
	return nil
}

// datastoreEntry represents a cache entry in Datastore.
// We use base64-encoded string for Value to avoid datastore []byte limitations.
// The key is stored in the Datastore entity key itself.
type datastoreEntry struct {
	Value     string    `datastore:"value,noindex"` // base64-encoded JSON value
	Expiry    time.Time `datastore:"expiry,omitempty,noindex"`
	UpdatedAt time.Time `datastore:"updated_at"`
}

// newDatastorePersist creates a new Datastore-based persistence layer.
// An empty projectID will auto-detect the project.
func newDatastorePersist[K comparable, V any](ctx context.Context, cacheID string) (*datastorePersist[K, V], error) {
	// Empty project ID lets ds9 auto-detect
	client, err := datastore.NewClientWithDatabase(ctx, "", cacheID)
	if err != nil {
		return nil, fmt.Errorf("create datastore client: %w", err)
	}

	slog.Debug("initialized datastore persistence", "database", cacheID, "kind", datastoreKind)

	return &datastorePersist[K, V]{
		client: client,
		kind:   datastoreKind,
	}, nil
}

// makeKey creates a Datastore key from a cache key.
// We use the string representation directly as the key name.
func (d *datastorePersist[K, V]) makeKey(key K) *datastore.Key {
	keyStr := fmt.Sprintf("%v", key)
	return datastore.NameKey(d.kind, keyStr, nil)
}

// Load retrieves a value from Datastore.
func (d *datastorePersist[K, V]) Load(ctx context.Context, key K) (V, time.Time, bool, error) {
	var zero V
	dsKey := d.makeKey(key)

	var entry datastoreEntry
	if err := d.client.Get(ctx, dsKey, &entry); err != nil {
		if err == datastore.ErrNoSuchEntity {
			return zero, time.Time{}, false, nil
		}
		return zero, time.Time{}, false, fmt.Errorf("datastore get: %w", err)
	}

	// Check expiration
	if !entry.Expiry.IsZero() && time.Now().After(entry.Expiry) {
		// Delete expired entry asynchronously with timeout
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			d.client.Delete(ctx, dsKey)
		}()
		return zero, time.Time{}, false, nil
	}

	// Decode from base64
	valueBytes, err := base64.StdEncoding.DecodeString(entry.Value)
	if err != nil {
		return zero, time.Time{}, false, fmt.Errorf("decode base64: %w", err)
	}

	// Decode value from JSON
	var value V
	if err := json.Unmarshal(valueBytes, &value); err != nil {
		return zero, time.Time{}, false, fmt.Errorf("unmarshal value: %w", err)
	}

	return value, entry.Expiry, true, nil
}

// Store saves a value to Datastore.
func (d *datastorePersist[K, V]) Store(ctx context.Context, key K, value V, expiry time.Time) error {
	dsKey := d.makeKey(key)

	// Encode value as JSON then base64
	valueBytes, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal value: %w", err)
	}
	valueStr := base64.StdEncoding.EncodeToString(valueBytes)

	entry := datastoreEntry{
		Value:     valueStr,
		Expiry:    expiry,
		UpdatedAt: time.Now(),
	}

	if _, err := d.client.Put(ctx, dsKey, &entry); err != nil {
		return fmt.Errorf("datastore put: %w", err)
	}

	return nil
}

// Delete removes a value from Datastore.
func (d *datastorePersist[K, V]) Delete(ctx context.Context, key K) error {
	dsKey := d.makeKey(key)

	if err := d.client.Delete(ctx, dsKey); err != nil {
		return fmt.Errorf("datastore delete: %w", err)
	}

	return nil
}

// LoadRecent streams entries from Datastore, returning up to 'limit' most recently updated entries.
func (d *datastorePersist[K, V]) LoadRecent(ctx context.Context, limit int) (<-chan Entry[K, V], <-chan error) {
	entryCh := make(chan Entry[K, V], 100)
	errCh := make(chan error, 1)

	go func() {
		defer close(entryCh)
		defer close(errCh)

		// Query ordered by UpdatedAt descending, limited
		query := datastore.NewQuery(d.kind).Order("-updated_at")
		if limit > 0 {
			query = query.Limit(limit)
		}

		iter := d.client.Run(ctx, query)

		now := time.Now()
		loaded := 0
		expired := 0

		for {
			var entry datastoreEntry
			dsKey, err := iter.Next(&entry)
			if err == datastore.Done {
				break
			}
			if err != nil {
				errCh <- fmt.Errorf("query next: %w", err)
				return
			}

			// Check context cancellation
			select {
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			default:
			}

			// Clean up expired entries asynchronously with timeout
			if !entry.Expiry.IsZero() && now.After(entry.Expiry) {
				go func(key *datastore.Key) {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					d.client.Delete(ctx, key)
				}(dsKey)
				expired++
				continue
			}

			// Extract key from Datastore entity key name
			// We need to parse the key string back to type K
			// For now, we'll use fmt.Sscanf for simple types
			var key K
			keyStr := dsKey.Name
			if _, err := fmt.Sscanf(keyStr, "%v", &key); err != nil {
				// If Sscanf fails, try direct type assertion for string keys
				if strKey, ok := any(keyStr).(K); ok {
					key = strKey
				} else {
					slog.Warn("failed to parse key from datastore", "key", keyStr, "error", err)
					continue
				}
			}

			// Decode value from base64
			valueBytes, err := base64.StdEncoding.DecodeString(entry.Value)
			if err != nil {
				slog.Warn("failed to decode value from datastore", "error", err)
				continue
			}

			var value V
			if err := json.Unmarshal(valueBytes, &value); err != nil {
				slog.Warn("failed to unmarshal value from datastore", "error", err)
				continue
			}

			entryCh <- Entry[K, V]{
				Key:       key,
				Value:     value,
				Expiry:    entry.Expiry,
				UpdatedAt: entry.UpdatedAt,
			}
			loaded++
		}

		slog.Info("loaded cache entries from datastore", "loaded", loaded, "expired", expired)
	}()

	return entryCh, errCh
}

// LoadAll streams all entries from Datastore (no limit).
func (d *datastorePersist[K, V]) LoadAll(ctx context.Context) (<-chan Entry[K, V], <-chan error) {
	return d.LoadRecent(ctx, 0)
}

// Close releases Datastore client resources.
func (d *datastorePersist[K, V]) Close() error {
	return d.client.Close()
}
