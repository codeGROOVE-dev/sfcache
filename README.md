# bdcache - Big Dumb Cache

<img src="media/logo-small.png" alt="bdcache logo" width="256">

[![Go Reference](https://pkg.go.dev/badge/github.com/codeGROOVE-dev/bdcache.svg)](https://pkg.go.dev/github.com/codeGROOVE-dev/bdcache)
[![Go Report Card](https://goreportcard.com/badge/github.com/codeGROOVE-dev/bdcache)](https://goreportcard.com/report/github.com/codeGROOVE-dev/bdcache)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

<br clear="right">

Fast, persistent Go cache with S3-FIFO eviction - better hit rates than LRU, survives restarts with local files or Google Cloud Datastore, zero allocations.

## Install

```bash
go get github.com/codeGROOVE-dev/bdcache
```

## Use

```go
// Memory only
cache, err := bdcache.New[string, int](ctx)
if err != nil {
    return err
}
if err := cache.Set(ctx, "answer", 42, 0); err != nil {
    return err
}
val, found, err := cache.Get(ctx, "answer")

// With smart persistence (local files for dev, Google Cloud Datastore for Cloud Run)
cache, err := bdcache.New[string, User](ctx, bdcache.WithBestStore("myapp"))

// With Cloud Datastore persistence and automatic cleanup
cache, err := bdcache.New[string, User](ctx,
    bdcache.WithCloudDatastore("myapp"),
    bdcache.WithCleanup(24*time.Hour), // Cleanup entries older than 24h
)
```

## Features

- **S3-FIFO eviction** - Better than LRU ([learn more](https://s3fifo.com/))
- **Type safe** - Go generics
- **Persistence** - Local files (gob) or Google Cloud Datastore (JSON)
- **Graceful degradation** - Cache works even if persistence fails
- **Per-item TTL** - Optional expiration

## Performance

### vs Popular Go Caches

Benchmarks on MacBook Pro M4 Max comparing memory-only Get operations:

| Library | Algorithm | ns/op | Allocations | Persistence |
|---------|-----------|-------|-------------|-------------|
| **bdcache** | S3-FIFO | **8.61** | **0 allocs** | ✅ Auto (Local files + GCP Datastore) |
| golang-lru | LRU | 13.02 | 0 allocs | ❌ None |
| otter | S3-FIFO | 14.58 | 0 allocs | ⚠️ Manual (Save/Load entire cache) |
| ristretto | TinyLFU | 30.53 | 0 allocs | ❌ None |

> ⚠️ **Benchmark Disclaimer**: These benchmarks are highly cherrypicked to show S3-FIFO's advantages. Different cache implementations excel at different workloads - LRU may outperform S3-FIFO in some scenarios, while TinyLFU shines in others. Performance varies based on access patterns, working set size, and hardware.
>
> **The real differentiator** is bdcache's automatic per-item persistence designed for unreliable environments like Cloud Run and Kubernetes, where shutdowns are unpredictable. See [benchmarks/](benchmarks/) for methodology.

**Key advantage:**
- **Automatic persistence for unreliable environments** - per-item writes to local files or Google Cloud Datastore survive unexpected shutdowns (Cloud Run, Kubernetes), container restarts, and crashes without manual save/load choreography

**Also competitive on:**
- Speed - comparable to or faster than alternatives on typical workloads
- Hit rates - S3-FIFO protects hot data from scans in specific scenarios
- Zero allocations - efficient for high-frequency operations

### Competitive Analysis

Independent benchmark using [scalalang2/go-cache-benchmark](https://github.com/scalalang2/go-cache-benchmark) (500K items, Zipfian distribution):

**Hit Rate Leadership:**
- **0.1% cache size**: bdcache **48.12%** vs SIEVE 47.42%, TinyLFU 47.37%, S3-FIFO 47.16%
- **1% cache size**: bdcache **64.45%** vs TinyLFU 63.94%, Otter 63.60%, S3-FIFO 63.59%, SIEVE 63.33%
- **10% cache size**: bdcache **80.39%** vs TinyLFU 80.43%, Otter 79.86%, S3-FIFO 79.84%

Consistently ranks top 1-2 for hit rate across all cache sizes while maintaining competitive throughput (5-12M QPS). The S3-FIFO implementation prioritizes cache efficiency over raw speed, making bdcache ideal when hit rate matters.

### Detailed Benchmarks

Memory-only operations:
```
BenchmarkCache_Get_Hit-16      56M ops/sec    17.8 ns/op       0 B/op     0 allocs
BenchmarkCache_Set-16          56M ops/sec    17.8 ns/op       0 B/op     0 allocs
```

With file persistence enabled:
```
BenchmarkCache_Get_PersistMemoryHit-16    85M ops/sec    11.8 ns/op       0 B/op     0 allocs
BenchmarkCache_Get_PersistDiskRead-16     73K ops/sec    13.8 µs/op    7921 B/op   178 allocs
BenchmarkCache_Set_WithPersistence-16      9K ops/sec   112.3 µs/op    2383 B/op    36 allocs
```

## Cloud Datastore TTL Setup

When using Google Cloud Datastore persistence, configure native TTL policies for automatic expiration:

### One-time Setup (per database)

```bash
# Enable TTL on the 'expiry' field for CacheEntry kind
gcloud firestore fields ttls update expiry \
  --collection-group=CacheEntry \
  --enable-ttl \
  --database=YOUR_CACHE_ID
```

**Important:**
- Replace `YOUR_CACHE_ID` with your cache ID (passed to `WithCloudDatastore()`)
- This is a one-time setup per database
- Datastore automatically deletes expired entries within 24 hours
- No indexing needed on the expiry field (prevents hotspots)

### Best Practices

1. **Use Native TTL**: Let Datastore handle expiration automatically
2. **Add Cleanup Fallback**: Use `WithCleanup()` as a safety net:
   ```go
   cache, err := bdcache.New[string, User](ctx,
       bdcache.WithCloudDatastore("myapp"),
       bdcache.WithCleanup(24*time.Hour), // Safety net for orphaned data
   )
   ```
3. **Set Cleanup MaxAge**: Should match your longest TTL value
4. **Monitor Costs**: TTL deletions count toward entity delete operations

If native TTL is properly configured, `WithCleanup()` will find no entries (fast no-op).

## License

Apache 2.0
