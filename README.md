# bdcache - Big Dumb Cache

<img src="media/logo-small.png" alt="bdcache logo" width="256">

[![Go Reference](https://pkg.go.dev/badge/github.com/codeGROOVE-dev/bdcache.svg)](https://pkg.go.dev/github.com/codeGROOVE-dev/bdcache)
[![Go Report Card](https://goreportcard.com/badge/github.com/codeGROOVE-dev/bdcache)](https://goreportcard.com/report/github.com/codeGROOVE-dev/bdcache)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

<br clear="right">

Stupid fast in-memory Go cache with optional L2 persistence layer.

Designed originally for persistently caching HTTP fetches in unreliable environments like Google Cloud Run, this cache has something for everyone.

## Features

- **Faster than a bat out of hell** - Best-in-class latency and throughput
- **S3-FIFO eviction** - Better hit-rates than LRU ([learn more](https://s3fifo.com/))
- **Pluggable persistence** - Bring your own database or use built-in backends:
  - [`persist/localfs`](persist/localfs) - Local files (gob encoding, zero dependencies)
  - [`persist/datastore`](persist/datastore) - Google Cloud Datastore
  - [`persist/valkey`](persist/valkey) - Valkey/Redis
  - [`persist/cloudrun`](persist/cloudrun) - Auto-detect Cloud Run
- **Per-item TTL** - Optional expiration
- **Graceful degradation** - Cache works even if persistence fails
- **Zero allocation reads** - minimal GC thrashing
- **Type safe** - Go generics

## Usage

As a stupid-fast in-memory cache:

```go
import "github.com/codeGROOVE-dev/bdcache"

// strings as keys, ints as values
cache, _ := bdcache.New[string, int](ctx)
cache.Set(ctx, "answer", 42, 0)
val, found, err := cache.Get(ctx, "answer")
```

With local file persistence to survive restarts:

```go
import (
  "github.com/codeGROOVE-dev/bdcache"
  "github.com/codeGROOVE-dev/bdcache/persist/localfs"
)

p, err := localfs.New[string, User]("myapp", "")
cache, _ := bdcache.New[string, User](ctx, bdcache.WithPersistence(p))

cache.SetAsync(ctx, "answer", 42, 0) // Don't wait for the key to persist
```

A persistent cache suitable for Cloud Run or local development; uses Cloud Datastore if available

```go
p, _ := cloudrun.New[string, User](ctx, "myapp")
cache, _ := bdcache.New[string, User](ctx, bdcache.WithPersistence(p))
```




## Performance against the Competition

bdcache prioritizes high hit-rates and low read latency, but it performs quite well all around.

Here's the results from an M4 MacBook Pro - run `make bench` to see the results for yourself:

### Hit Rate (Zipf α=0.99, 1M ops, 1M keyspace)

| Cache         | Size=1% | Size=2.5% | Size=5% |
|---------------|---------|-----------|---------|
| bdcache 🟡    |  94.46% |    94.89% |  95.09% |
| otter 🦦      |  94.27% |    94.68% |  95.09% |
| ristretto ☕  |  91.63% |    92.44% |  93.02% |
| tinylfu 🔬    |  94.31% |    94.87% |  95.09% |
| freecache 🆓  |  94.03% |    94.15% |  94.75% |
| lru 📚        |  94.10% |    94.84% |  95.09% |

🏆 Hit rate: +0.1% better than 2nd best (tinylfu)

### Single-Threaded Latency (sorted by Get)

| Cache         | Get ns/op | Get B/op | Get allocs | Set ns/op | Set B/op | Set allocs |
|---------------|-----------|----------|------------|-----------|----------|------------|
| bdcache 🟡    |       9.0 |        0 |          0 |      20.0 |        0 |          0 |
| lru 📚        |      22.0 |        0 |          0 |      22.0 |        0 |          0 |
| ristretto ☕  |      31.0 |       14 |          0 |      68.0 |      120 |          3 |
| otter 🦦      |      34.0 |        0 |          0 |     138.0 |       51 |          1 |
| freecache 🆓  |      71.0 |       15 |          1 |      56.0 |        4 |          0 |
| tinylfu 🔬    |      84.0 |        3 |          0 |     105.0 |      175 |          3 |

🏆 Get latency: +144% faster than 2nd best (lru)
🏆 Set latency: +10% faster than 2nd best (lru)

### Single-Threaded Throughput (mixed read/write)

| Cache         | Get QPS    | Set QPS    |
|---------------|------------|------------|
| bdcache 🟡    |   79.25M   |   43.15M   |
| lru 📚        |   36.39M   |   36.88M   |
| ristretto ☕  |   28.22M   |   13.46M   |
| otter 🦦      |   25.46M   |    7.16M   |
| freecache 🆓  |   13.30M   |   16.32M   |
| tinylfu 🔬    |   11.32M   |    9.34M   |

🏆 Get throughput: +118% faster than 2nd best (lru)
🏆 Set throughput: +17% faster than 2nd best (lru)

### Concurrent Throughput (mixed read/write): 4 threads

| Cache         | Get QPS    | Set QPS    |
|---------------|------------|------------|
| bdcache 🟡    |   29.62M   |   29.92M   |
| ristretto ☕  |   25.98M   |   13.12M   |
| freecache 🆓  |   25.36M   |   21.84M   |
| otter 🦦      |   23.14M   |    3.99M   |
| lru 📚        |    9.39M   |    9.64M   |
| tinylfu 🔬    |    5.75M   |    4.91M   |

🏆 Get throughput: +14% faster than 2nd best (ristretto)
🏆 Set throughput: +37% faster than 2nd best (freecache)

### Concurrent Throughput (mixed read/write): 8 threads

| Cache         | Get QPS    | Set QPS    |
|---------------|------------|------------|
| bdcache 🟡    |   22.19M   |   18.68M   |
| otter 🦦      |   19.74M   |    3.03M   |
| ristretto ☕  |   18.82M   |   11.39M   |
| freecache 🆓  |   16.83M   |   16.30M   |
| lru 📚        |    7.55M   |    7.68M   |
| tinylfu 🔬    |    4.95M   |    4.15M   |

🏆 Get throughput: +12% faster than 2nd best (otter)
🏆 Set throughput: +15% faster than 2nd best (freecache)

### Concurrent Throughput (mixed read/write): 12 threads

| Cache         | Get QPS    | Set QPS    |
|---------------|------------|------------|
| bdcache 🟡    |   24.49M   |   24.03M   |
| ristretto ☕  |   22.85M   |   11.48M   |
| otter 🦦      |   21.77M   |    2.92M   |
| freecache 🆓  |   17.45M   |   16.70M   |
| lru 📚        |    7.42M   |    7.62M   |
| tinylfu 🔬    |    4.55M   |    3.70M   |

🏆 Get throughput: +7.2% faster than 2nd best (ristretto)
🏆 Set throughput: +44% faster than 2nd best (freecache)

### Concurrent Throughput (mixed read/write): 16 threads

| Cache         | Get QPS    | Set QPS    |
|---------------|------------|------------|
| bdcache 🟡    |   15.96M   |   15.55M   |
| otter 🦦      |   15.64M   |    2.84M   |
| ristretto ☕  |   15.59M   |   12.31M   |
| freecache 🆓  |   15.24M   |   14.72M   |
| lru 📚        |    7.47M   |    7.42M   |
| tinylfu 🔬    |    4.71M   |    3.43M   |

🏆 Get throughput: +2.0% faster than 2nd best (otter)
🏆 Set throughput: +5.6% faster than 2nd best (freecache)

### Concurrent Throughput (mixed read/write): 24 threads

| Cache         | Get QPS    | Set QPS    |
|---------------|------------|------------|
| bdcache 🟡    |   15.93M   |   15.41M   |
| otter 🦦      |   15.81M   |    2.88M   |
| ristretto ☕  |   15.57M   |   13.20M   |
| freecache 🆓  |   14.58M   |   14.10M   |
| lru 📚        |    7.59M   |    7.80M   |
| tinylfu 🔬    |    4.96M   |    3.73M   |

🏆 Get throughput: +0.7% faster than 2nd best (otter)
🏆 Set throughput: +9.2% faster than 2nd best (freecache)

### Concurrent Throughput (mixed read/write): 32 threads

| Cache         | Get QPS    | Set QPS    |
|---------------|------------|------------|
| bdcache 🟡    |   16.68M   |   15.38M   |
| otter 🦦      |   15.87M   |    2.87M   |
| ristretto ☕  |   15.55M   |   13.50M   |
| freecache 🆓  |   14.64M   |   13.84M   |
| lru 📚        |    7.87M   |    8.01M   |
| tinylfu 🔬    |    5.12M   |    3.01M   |

🏆 Get throughput: +5.1% faster than 2nd best (otter)
🏆 Set throughput: +11% faster than 2nd best (freecache)

NOTE: Performance characteristics often have trade-offs. There are almost certainly workloads where other cache implementations are faster, but nobody blends speed and persistence the way that bdcache does.

## License

Apache 2.0
