<p align="center">
  <img src="media/logo-small.png" alt="bdcache" width="200"/>
</p>

# bdcache - Big Dumb Cache

Simple, fast, reliable Go cache built for Google Cloud Run and local development.

## Why?

- **Actually Simple** - Not 47 configuration options. Just cache stuff.
- **Actually Fast** - ~19ns per operation. Zero allocations.
- **Actually Reliable** - Gracefully degrades when things break (and they will).
- **Smart Persistence** - Local files for dev/persistent processes. Cloud Datastore for stateless Cloud Run.

## Install

```bash
go get github.com/tstromberg/bdcache
```

## Use It

```go
// Memory only
cache, _ := bdcache.New[string, int](ctx)
cache.Set(ctx, "answer", 42, 0)
val, found, _ := cache.Get(ctx, "answer")

// Smart default (recommended)
// - Uses Cloud Datastore on Cloud Run (stateless, shared cache)
// - Uses local files elsewhere (dev, persistent processes)
cache, _ := bdcache.New[string, User](ctx, bdcache.WithBestStore("myapp"))

// Explicit local files (for dev/persistent processes)
cache, _ := bdcache.New[string, Data](ctx, bdcache.WithLocalStore("myapp"))

// Explicit Cloud Datastore (for Cloud Run/stateless)
cache, _ := bdcache.New[string, Thing](ctx, bdcache.WithCloudDatastore("myapp"))
```

## Features

- **S3-FIFO eviction** - Better hit rates than your LRU
- **Type safe** - Go generics, not `interface{}`
- **Dual persistence** - Local files (gob) for dev. Cloud Datastore for Cloud Run.
- **Graceful** - Persistence broken? Fine. You get memory-only.
- **On-demand loading** - Loads from persistence as needed. Optional warmup available.
- **Per-item TTL** - Or don't. Whatever.

## Persistence

**Local files** (gob-encoded) for local development and persistent processes:
- Fast, simple, no external dependencies
- Survives restarts on VMs or local machines
- Stored in OS-appropriate cache directory with squid-style subdirectories (first 2 chars of key)
- Keys limited to 127 characters: alphanumeric, dash, underscore, period, colon (a-z A-Z 0-9 - _ . :)

**Cloud Datastore** (JSON-encoded) for Google Cloud Run:
- Shared cache across stateless instances
- Survives cold starts and redeployments
- Auto-detects project ID, uses database namespacing
- Keys limited to 1500 characters (Datastore constraint)

## Performance

```
BenchmarkCache_Get_Hit-16      66M ops/sec    18.5 ns/op    0 allocs
BenchmarkCache_Set-16          61M ops/sec    19.3 ns/op    0 allocs
```

## Options

- `WithMemorySize(n)` - Max items in RAM (default: 10k)
- `WithDefaultTTL(d)` - Default expiration (default: never)
- `WithLocalStore(id)` - Enable file persistence
- `WithCloudDatastore(id)` - Enable Datastore persistence
- `WithBestStore(id)` - Auto-select based on environment
- `WithWarmup(n)` - Pre-load N most recent items on startup (default: disabled)

## API

### Core Methods

- `Get(ctx, key) (V, bool, error)` - Retrieve value, returns (value, found, error)
- `Set(ctx, key, value, ttl) error` - Store value with optional TTL. ttl=0 uses DefaultTTL if configured
  - **Reliability guarantee**: Value is ALWAYS stored in memory, even if persistence fails
  - Returns error if key violates persistence constraints or if persistence fails
  - Even when error is returned, value is cached in memory
- `Delete(ctx, key)` - Remove value (void)
- `Cleanup() int` - Remove expired entries, returns count
- `Len() int` - Get memory cache size
- `Close() error` - Release resources

### Key Constraints

**File persistence**:
- Maximum 127 characters
- Only alphanumeric, dash, underscore, period, colon (a-z A-Z 0-9 - _ . :)
- Keys violating these constraints return an error from Set()

**Datastore persistence**:
- Maximum 1500 characters
- Any non-empty string allowed
- Keys violating these constraints return an error from Set()

**Memory-only** (no persistence):
- No key constraints
- Any comparable type works

## License

MIT
