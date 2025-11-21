package bdcache

import (
	"os"
	"time"
)

// Options configures a Cache instance.
type Options struct {
	CacheID        string
	MemorySize     int
	DefaultTTL     time.Duration
	WarmupLimit    int
	UseDatastore   bool
	CleanupEnabled bool
	CleanupMaxAge  time.Duration
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
// - Otherwise: uses local file store.
func WithBestStore(cacheID string) Option {
	return func(o *Options) {
		o.CacheID = cacheID
		o.UseDatastore = os.Getenv("K_SERVICE") != ""
	}
}

// WithWarmup enables cache warmup by loading the N most recently updated entries from persistence on startup.
// By default, warmup is disabled (0). Set to a positive number to load that many entries.
func WithWarmup(n int) Option {
	return func(o *Options) {
		o.WarmupLimit = n
	}
}

// WithCleanup enables background cleanup of expired entries at startup.
// maxAge should be set to your maximum TTL value - entries older than this are deleted.
// This is a safety net for expired data and works alongside native Datastore TTL policies.
// If native TTL is properly configured, this cleanup will be fast (no-op).
func WithCleanup(maxAge time.Duration) Option {
	return func(o *Options) {
		o.CleanupEnabled = true
		o.CleanupMaxAge = maxAge
	}
}

// defaultOptions returns the default configuration (memory-only).
func defaultOptions() *Options {
	return &Options{
		MemorySize: 10000,
	}
}
