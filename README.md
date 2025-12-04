# sfcache - Stupid Fast Cache

<img src="media/logo-small.png" alt="sfcache logo" width="256">

[![Go Reference](https://pkg.go.dev/badge/github.com/codeGROOVE-dev/sfcache.svg)](https://pkg.go.dev/github.com/codeGROOVE-dev/sfcache)
[![Go Report Card](https://goreportcard.com/badge/github.com/codeGROOVE-dev/sfcache)](https://goreportcard.com/report/github.com/codeGROOVE-dev/sfcache)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

<br clear="right">

Stupid fast in-memory Go cache with optional L2 persistence layer.

Designed for persistently caching API requests in an unreliable environment, this cache has something for everyone.

## Features

- **Faster than a bat out of hell** - Best-in-class latency and throughput
- **S3-FIFO eviction** - Better hit-rates than LRU ([learn more](https://s3fifo.com/))
- **L2 Persistence (optional)** - Bring your own database or use built-in backends:
  - [`pkg/persist/localfs`](pkg/persist/localfs) - Local files (gob encoding, zero dependencies)
  - [`pkg/persist/datastore`](pkg/persist/datastore) - Google Cloud Datastore
  - [`pkg/persist/valkey`](pkg/persist/valkey) - Valkey/Redis
  - [`pkg/persist/cloudrun`](pkg/persist/cloudrun) - Auto-detect Cloud Run
- **Per-item TTL** - Optional expiration
- **Graceful degradation** - Cache works even if persistence fails
- **Zero allocation reads** - minimal GC thrashing
- **Type safe** - Go generics

## Usage

As a stupid-fast in-memory cache:

```go
import "github.com/codeGROOVE-dev/sfcache"

// strings as keys, ints as values
cache := sfcache.Memory[string, int]()
cache.Set("answer", 42)
val, found := cache.Get("answer")
```

or with local file persistence to survive restarts:

```go
import (
  "github.com/codeGROOVE-dev/sfcache"
  "github.com/codeGROOVE-dev/sfcache/pkg/persist/localfs"
)

p, _ := localfs.New[string, User]("myapp", "")
cache, _ := sfcache.Persistent[string, User](ctx, p)

cache.SetAsync(ctx, "user:123", user) // Don't wait for the key to persist
cache.Store.Len(ctx)                  // Access persistence layer directly
```

A persistent cache suitable for Cloud Run or local development; uses Cloud Datastore if available

```go
import "github.com/codeGROOVE-dev/sfcache/pkg/persist/cloudrun"

p, _ := cloudrun.New[string, User](ctx, "myapp")
cache, _ := sfcache.Persistent[string, User](ctx, p)
```

## Performance against the Competition

sfcache prioritizes high hit-rates and low read latency, but it performs quite well all around.

Here's the results from an M4 MacBook Pro - run `make bench` to see the results for yourself:

```
>>> TestLatency: Single-Threaded Latency (go test -run=TestLatency -v)

### Single-Threaded Latency (sorted by Get)

| Cache         | Get ns/op | Get B/op | Get allocs | Set ns/op | Set B/op | Set allocs |
|---------------|-----------|----------|------------|-----------|----------|------------|
| sfcache       |       8.0 |        0 |          0 |      21.0 |        0 |          0 |
| lru           |      23.0 |        0 |          0 |      22.0 |        0 |          0 |
| ristretto     |      32.0 |       14 |          0 |      65.0 |      118 |          3 |
| otter         |      35.0 |        0 |          0 |     131.0 |       48 |          1 |
| freecache     |      62.0 |        8 |          1 |      49.0 |        0 |          0 |
| tinylfu       |      75.0 |        0 |          0 |      97.0 |      168 |          3 |

- 🔥 Get: 188% better than next best (lru)
- 🔥 Set: 4.8% better than next best (lru)

>>> TestZipfThroughput1: Zipf Throughput (1 thread) (go test -run=TestZipfThroughput1 -v)

### Zipf Throughput (alpha=0.99, 75% read / 25% write): 1 threads

| Cache         | QPS        |
|---------------|------------|
| sfcache       |   96.94M   |
| lru           |   46.24M   |
| tinylfu       |   19.21M   |
| freecache     |   15.02M   |
| otter         |   12.95M   |
| ristretto     |   11.34M   |

- 🔥 Throughput: 110% faster than next best (lru)

>>> TestZipfThroughput16: Zipf Throughput (16 threads) (go test -run=TestZipfThroughput16 -v)

### Zipf Throughput (alpha=0.99, 75% read / 25% write): 16 threads

| Cache         | QPS        |
|---------------|------------|
| sfcache       |   43.27M   |
| freecache     |   15.08M   |
| ristretto     |   14.20M   |
| otter         |   10.85M   |
| lru           |    5.64M   |
| tinylfu       |    4.25M   |

- 🔥 Throughput: 187% faster than next best (freecache)

>>> TestMetaTrace: Meta Trace Hit Rate (10M ops) (go test -run=TestMetaTrace -v)

### Meta Trace Hit Rate (10M ops from Meta KVCache)

| Cache         | 50K cache | 100K cache |
|---------------|-----------|------------|
| sfcache       |   68.19%  |   76.03%   |
| otter         |   41.31%  |   55.41%   |
| ristretto     |   40.33%  |   48.91%   |
| tinylfu       |   53.70%  |   54.79%   |
| freecache     |   56.86%  |   65.52%   |
| lru           |   65.21%  |   74.22%   |

- 🔥 Meta trace: 2.4% better than next best (lru)

>>> TestHitRate: Zipf Hit Rate (go test -run=TestHitRate -v)

### Hit Rate (Zipf alpha=0.99, 1M ops, 1M keyspace)

| Cache         | Size=1% | Size=2.5% | Size=5% |
|---------------|---------|-----------|---------|
| sfcache       |  64.19% |    69.23% |  72.50% |
| otter         |  61.64% |    67.94% |  71.38% |
| ristretto     |  34.88% |    41.25% |  46.62% |
| tinylfu       |  63.83% |    68.25% |  71.56% |
| freecache     |  56.65% |    57.75% |  63.39% |
| lru           |  57.33% |    64.55% |  69.92% |

- 🔥 Hit rate: 1.1% better than next best (tinylfu)
```

Cache performance is a game of balancing trade-offs. There will be workloads where other cache implementations are better, but nobody blends speed and persistence like we do.

## License

Apache 2.0
