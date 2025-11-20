package bdcache

import (
	"os"
	"time"
)

// Options configures a Cache instance.
type Options struct {
	MemorySize   int           // Maximum number of items in memory cache (default: 10000)
	DefaultTTL   time.Duration // Default TTL for items with no explicit TTL (default: 0 = no expiration)
	CacheID      string        // Identifier for cache (used for local dir name and datastore database ID)
	UseDatastore bool          // If true, use Cloud Datastore instead of local files
	WarmupLimit  int           // Max number of entries to load during warmup (0 = disabled, default: 0)
}

// Option is a functional option for configuring a Cache.
type Option func(*Options)

// WithMemorySize sets the maximum number of items in the memory cache.
func WithMemorySize(n int) Option {
	return func(o *Options) {
		o.MemorySize = n
	}
}

// WithDefaultTTL sets the default TTL for cache items.
func WithDefaultTTL(d time.Duration) Option {
	return func(o *Options) {
		o.DefaultTTL = d
	}
}

// WithLocalStore enables local file persistence using the given cache ID as subdirectory name.
// Files are stored in os.UserCacheDir()/cacheID.
func WithLocalStore(cacheID string) Option {
	return func(o *Options) {
		o.CacheID = cacheID
		o.UseDatastore = false
	}
}

// WithCloudDatastore enables Cloud Datastore persistence using the given cache ID as database ID.
// An empty project ID will auto-detect the correct project.
func WithCloudDatastore(cacheID string) Option {
	return func(o *Options) {
		o.CacheID = cacheID
		o.UseDatastore = true
	}
}

// WithBestStore automatically selects the best persistence option:
// - If K_SERVICE environment variable is set (Google Cloud Run/Knative): uses Cloud Datastore
// - Otherwise: uses local file store
func WithBestStore(cacheID string) Option {
	return func(o *Options) {
		o.CacheID = cacheID
		if os.Getenv("K_SERVICE") != "" {
			o.UseDatastore = true
		} else {
			o.UseDatastore = false
		}
	}
}

// WithWarmup enables cache warmup by loading the N most recently updated entries from persistence on startup.
// By default, warmup is disabled (0). Set to a positive number to load that many entries.
func WithWarmup(n int) Option {
	return func(o *Options) {
		o.WarmupLimit = n
	}
}

// defaultOptions returns the default configuration (memory-only).
func defaultOptions() *Options {
	return &Options{
		MemorySize:   10000,
		DefaultTTL:   0,
		CacheID:      "",
		UseDatastore: false,
		WarmupLimit:  0,
	}
}
