# bdcache - Big Dumb Cache

<img src="media/logo-small.png" alt="bdcache logo" width="256" align="right">

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
    panic(err)
}
if err := cache.Set(ctx, "answer", 42, 0); err != nil {
    panic(err)
}
val, found, err := cache.Get(ctx, "answer")

// With smart persistence (local files for dev, Google Cloud Datastore for Cloud Run)
cache, err := bdcache.New[string, User](ctx, bdcache.WithBestStore("myapp"))
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
| **bdcache** | S3-FIFO | **12.95** | **0 allocs** | ✅ Auto (Local files + GCP Datastore) |
| golang-lru | LRU | 13.63 | 0 allocs | ❌ None |
| otter | S3-FIFO | 15.73 | 0 allocs | ⚠️ Manual (Save/Load entire cache) |
| ristretto | TinyLFU | 30.43 | 0 allocs | ❌ None |

> ⚠️ **Benchmark Disclaimer**: These benchmarks are highly cherrypicked to show S3-FIFO's advantages. Different cache implementations excel at different workloads - LRU may outperform S3-FIFO in some scenarios, while TinyLFU shines in others. Performance varies based on access patterns, working set size, and hardware.
>
> **The real differentiator** is bdcache's automatic per-item persistence designed for unreliable environments like Cloud Run and Kubernetes, where shutdowns are unpredictable. See [benchmarks/](benchmarks/) for methodology.

**Key advantage:**
- **Automatic persistence for unreliable environments** - per-item writes to local files or Google Cloud Datastore survive unexpected shutdowns (Cloud Run, Kubernetes), container restarts, and crashes without manual save/load choreography

**Also competitive on:**
- Speed - comparable to or faster than alternatives on typical workloads
- Hit rates - S3-FIFO protects hot data from scans in specific scenarios
- Zero allocations - efficient for high-frequency operations

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

## License

Apache 2.0
