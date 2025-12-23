package multicache

import (
	"context"
	"time"
)

// Store defines the interface for cache persistence backends.
// Uses only standard library types, so implementations can satisfy it
// without importing this package.
type Store[K comparable, V any] interface {
	// ValidateKey checks if a key is valid for this persistence store.
	ValidateKey(key K) error

	// Get retrieves a value from persistent storage.
	Get(ctx context.Context, key K) (V, time.Time, bool, error)

	// Set saves a value to persistent storage with an expiry time.
	Set(ctx context.Context, key K, value V, expiry time.Time) error

	// Delete removes a value from persistent storage.
	Delete(ctx context.Context, key K) error

	// Cleanup removes expired entries older than maxAge.
	Cleanup(ctx context.Context, maxAge time.Duration) (int, error)

	// Location returns the storage location for a given key.
	Location(key K) string

	// Flush removes all entries from persistent storage.
	Flush(ctx context.Context) (int, error)

	// Len returns the number of entries in persistent storage.
	Len(ctx context.Context) (int, error)

	// Close releases any resources held by the persistence store.
	Close() error
}
