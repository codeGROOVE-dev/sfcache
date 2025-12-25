# multicache - High-Performance Multi-Tier Cache

<img src="media/logo-small.png" alt="multicache logo" width="256">

[![Go Reference](https://pkg.go.dev/badge/github.com/codeGROOVE-dev/multicache.svg)](https://pkg.go.dev/github.com/codeGROOVE-dev/multicache)
[![Go Report Card](https://goreportcard.com/badge/github.com/codeGROOVE-dev/multicache)](https://goreportcard.com/report/github.com/codeGROOVE-dev/multicache)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

<br clear="right">

multicache is a high-performance cache for Go implementing the **S3-FIFO** algorithm from the SOSP'23 paper ["FIFO queues are all you need for cache eviction"](https://s3fifo.com/). It combines **best-in-class hit rates**, **multi-threaded** scalability, and an optional **multi-tier architecture** for persistence.

**Our philosophy**: Hit rate matters most (cache misses are expensive), then throughput (handle load), then single-threaded latency. We aim to excel at all three.

## Why "multi"?

### Multi-Threaded Performance

Designed for high-concurrency workloads with dynamic sharding (up to 2048 shards) that scales with `GOMAXPROCS`. At 16 threads, multicache delivers **44M+ QPS** for mixed read/write operations—nearly 3× faster than the next best cache.

### Multi-Tier Architecture

Stack fast in-memory caching with durable persistence:

```
┌─────────────────────────────────────┐
│         Your Application            │
└─────────────────┬───────────────────┘
                  │
┌─────────────────▼───────────────────┐
│    Memory Cache (microseconds)      │  ← L1: S3-FIFO eviction
└─────────────────┬───────────────────┘
                  │ async write / sync read
┌─────────────────▼───────────────────┐
│   Persistence Store (milliseconds)  │  ← L2: localfs, Valkey, Datastore
└─────────────────────────────────────┘
```

Persistence backends:
- [`pkg/store/localfs`](pkg/store/localfs) - Local files (JSON, zero dependencies)
- [`pkg/store/valkey`](pkg/store/valkey) - Valkey/Redis
- [`pkg/store/datastore`](pkg/store/datastore) - Google Cloud Datastore
- [`pkg/store/cloudrun`](pkg/store/cloudrun) - Auto-selects Datastore or localfs
- [`pkg/store/null`](pkg/store/null) - No-op for testing

All backends support optional S2 or Zstd compression via [`pkg/store/compress`](pkg/store/compress).

## Features

- **Best-in-class hit rates** - S3-FIFO beats LRU by 5%+ on real traces ([learn more](https://s3fifo.com/))
- **Multi-threaded throughput** - 44M+ QPS at 16 threads (3× faster than competition)
- **Low latency** - 7ns reads, 108M+ QPS single-threaded, zero-allocation hot path
- **Thundering herd prevention** - `GetSet` deduplicates concurrent loads
- **Per-item TTL** - Optional expiration
- **Graceful degradation** - Cache works even if persistence fails

## Usage

As a stupid-fast in-memory cache:

```go
import "github.com/codeGROOVE-dev/multicache"

// strings as keys, ints as values
cache := multicache.New[string, int]()
cache.Set("answer", 42)
val, found := cache.Get("answer")
```

Or as a multi-tier cache with local persistence to survive restarts:

```go
import (
  "github.com/codeGROOVE-dev/multicache"
  "github.com/codeGROOVE-dev/multicache/pkg/store/localfs"
)

p, _ := localfs.New[string, User]("myapp", "")
cache, _ := multicache.NewTiered(p)

cache.SetAsync(ctx, "user:123", user) // Don't wait for the key to persist
cache.Store.Len(ctx)                  // Access persistence layer directly
```

With S2 compression (fast, good ratio):

```go
import "github.com/codeGROOVE-dev/multicache/pkg/store/compress"

p, _ := localfs.New[string, User]("myapp", "", compress.S2())
```

How about a persistent cache suitable for Cloud Run or local development? This uses Cloud DataStore if available, local files if not:

```go
import "github.com/codeGROOVE-dev/multicache/pkg/store/cloudrun"

p, _ := cloudrun.New[string, User](ctx, "myapp")
cache, _ := multicache.NewTiered(p)
```

## Performance against the Competition

multicache prioritizes **hit rate** first, **multi-threaded throughput** second, and **single-threaded latency** third—but aims to excel at all three. We have our own built in `make bench` that asserts cache dominance:

```
>>> TestLatencyNoEviction: Latency - No Evictions (Set cycles within cache size) (go test -run=TestLatencyNoEviction -v)
| Cache         | Get ns/op | Get B/op | Get allocs | Set ns/op | Set B/op | Set allocs |
|---------------|-----------|----------|------------|-----------|----------|------------|
| multicache    |       7.0 |        0 |          0 |      20.0 |        0 |          0 |
| lru           |      22.0 |        0 |          0 |      22.0 |        0 |          0 |
| ristretto     |      32.0 |       14 |          0 |      75.0 |      119 |          3 |
| otter         |      32.0 |        0 |          0 |     138.0 |       51 |          1 |
| freecache     |      70.0 |        8 |          1 |      50.0 |        0 |          0 |
| tinylfu       |      73.0 |        0 |          0 |     109.0 |      168 |          3 |

- 🔥 Get: 214% better than next best (lru)
- 🔥 Set: 10% better than next best (lru)

>>> TestLatencyWithEviction: Latency - With Evictions (Set uses 20x unique keys) (go test -run=TestLatencyWithEviction -v)
| Cache         | Get ns/op | Get B/op | Get allocs | Set ns/op | Set B/op | Set allocs |
|---------------|-----------|----------|------------|-----------|----------|------------|
| multicache    |       7.0 |        0 |          0 |     108.0 |       30 |          0 |
| lru           |      21.0 |        0 |          0 |      81.0 |       80 |          1 |
| ristretto     |      32.0 |       14 |          0 |      73.0 |      118 |          3 |
| otter         |      33.0 |        0 |          0 |     175.0 |       59 |          1 |
| freecache     |      72.0 |        8 |          1 |      99.0 |        1 |          0 |
| tinylfu       |      74.0 |        0 |          0 |     107.0 |      168 |          3 |

- 🔥 Get: 200% better than next best (lru)
- 💧 Set: 48% worse than best (ristretto)

>>> TestZipfThroughput1: Zipf Throughput (1 thread) (go test -run=TestZipfThroughput1 -v)

### Zipf Throughput (alpha=0.99, 75% read / 25% write): 1 thread

| Cache         | QPS        |
|---------------|------------|
| multicache    |  108.91M   |
| lru           |   48.16M   |
| tinylfu       |   18.97M   |
| freecache     |   13.76M   |
| otter         |   13.64M   |
| ristretto     |   10.03M   |

- 🔥 Throughput: 126% faster than next best (lru)

>>> TestZipfThroughput16: Zipf Throughput (16 threads) (go test -run=TestZipfThroughput16 -v)

### Zipf Throughput (alpha=0.99, 75% read / 25% write): 16 threads

| Cache         | QPS        |
|---------------|------------|
| multicache    |   44.16M   |
| freecache     |   14.88M   |
| ristretto     |   13.49M   |
| otter         |   10.22M   |
| lru           |    5.96M   |
| tinylfu       |    4.30M   |

- 🔥 Throughput: 197% faster than next best (freecache)

>>> TestMetaTrace: Meta Trace Hit Rate (10M ops) (go test -run=TestMetaTrace -v)

### Meta Trace Hit Rate (10M ops from Meta KVCache)

| Cache         | 50K cache | 100K cache |
|---------------|-----------|------------|
| multicache    |   71.48%  |   78.39%   |
| otter         |   40.77%  |   55.61%   |
| ristretto     |   40.34%  |   48.97%   |
| tinylfu       |   53.70%  |   54.79%   |
| freecache     |   56.86%  |   65.52%   |
| lru           |   65.21%  |   74.22%   |

- 🔥 Meta trace: 5.6% better than next best (lru)

>>> TestHitRate: Zipf Hit Rate (go test -run=TestHitRate -v)

### Hit Rate (Zipf alpha=0.99, 1M ops, 1M keyspace)

| Cache         | Size=1% | Size=2.5% | Size=5% |
|---------------|---------|-----------|---------|
| multicache    |  63.90% |    68.74% |  71.84% |
| otter         |  62.00% |    67.53% |  71.39% |
| ristretto     |  34.69% |    41.30% |  46.59% |
| tinylfu       |  63.83% |    68.25% |  71.56% |
| freecache     |  56.73% |    57.75% |  63.39% |
| lru           |  57.33% |    64.55% |  69.92% |

- 🔥 Hit rate: 0.41% better than next best (tinylfu)
```

Want even more comprehensive benchmarks? See https://github.com/tstromberg/gocachemark where we win the top score.

## Implementation Notes

multicache implements the S3-FIFO algorithm from the SOSP'23 paper with these optimizations for production use:

1. **Dynamic Sharding** - Up to 2048 shards (capped at 2× GOMAXPROCS) for concurrent workloads
2. **Bloom Filter Ghosts** - Two rotating Bloom filters instead of storing keys, 10-100× less memory
3. **Lazy Ghost Checks** - Only check ghosts at capacity, saving latency during warmup
4. **Intrusive Lists** - Zero-allocation queue operations
5. **Fast-path Hashing** - Specialized `int`/`string` hashing via wyhash
6. **Higher Frequency Cap** - Max freq=7 (vs paper's 3) for better hot/warm discrimination

The core algorithm follows the paper closely: items enter the small queue, get promoted to main after 2+ accesses, and evicted items are tracked in a ghost queue to inform future admissions.

### Divergences from the S3-FIFO Paper

1. **Ghost frequency restoration** - Store peak frequency at eviction; restore 50% on ghost hit. Returning items skip the cold-start problem, reducing re-eviction of proven-popular keys. Only tracked for peakFreq ≥ 2 (lower values yield 0 after integer division).

2. **Main queue ghost tracking** - Ghost includes main queue evictions, not just small queue. Main queue items have demonstrated value (freq ≥ 2 to be promoted); preserving their history improves readmission decisions.

3. **Extended frequency counter** - maxFreq=7 (3 bits) vs paper's maxFreq=3 (2 bits). Finer granularity improves discrimination between warm and hot items during eviction.

These changes yield +0.2-0.5% hit rate on production traces while preserving O(1) operations.

## License

Apache 2.0
