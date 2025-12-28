# multicache

multicache is an in-memory #golang cache library. 

It's been optimized over hundreds of experiments to be the highest performing cache available - both in terms of hit rates and throughput - and also features an optional multi-tier persistent cache option.

## Install

```
go get github.com/codeGROOVE-dev/multicache
```

## Use

```go
cache := multicache.New[string, int](multicache.Size(10000))
cache.Set("answer", 42)
val, ok := cache.Get("answer")
```

With persistence:

```go
store, _ := localfs.New[string, User]("myapp", "")
cache, _ := multicache.NewTiered(store)

cache.Set(ctx, "user:123", user)           // sync write
cache.SetAsync(ctx, "user:456", user)      // async write
```

GetSet deduplicates concurrent loads to prevent thundering herd situations:

```go
user, err := cache.GetSet("user:123", func() (User, error) {
    return db.LoadUser("123")
})
```

## Options

```go
multicache.Size(n)           // max entries (default 16384)
multicache.TTL(time.Hour)    // default expiration
```

## Persistence

Memory cache backed by durable storage. Reads check memory first; writes go to both.

| Backend | Import |
|---------|--------|
| Local filesystem | `pkg/store/localfs` |
| Valkey/Redis | `pkg/store/valkey` |
| Google Cloud Datastore | `pkg/store/datastore` |
| Auto-detect (Cloud Run) | `pkg/store/cloudrun` |

For maximum efficiency, all backends support S2 or Zstd compression via `pkg/store/compress`.

## Performance

multicache has been exhaustively tested for performance using [gocachemark](https://github.com/tstromberg/gocachemark). As of Dec 2025, it's the highest performing cache implementation for Go.

Where multicache wins:

- **Throughput**: 1 billion ints/second at 16 threads or higher. (2-3X faster than otter)
- **Hit rate**: Highest average across datasets (1.6% higher than sieve, 4.4% higher than otter)
- **Latency**: 9-11ns Get, zero allocations (3-4X lower latency than otter)

Where others win:

- **Memory**: freelru and otter use less memory per entry
- **Some traces**: CLOCK/LRU marginally better on purely temporal workloads (IBM Docker, Thesios)

Much of the credit for high throughput goes to [puzpuzpuz/xsync](https://github.com/puzpuzpuz/xsync). While highly sharded maps and flightGroups performed well, you can't beat xsync's lock-free data structures.

Run `make competive-bench` for full results.

## Algorithm

multicache uses [S3-FIFO](https://s3fifo.com/), which features three queues: small (new entries), main (promoted entries), and ghost (recently evicted keys). New items enter small; items accessed twice move to main. The ghost queue tracks evicted keys in a bloom filter to fast-track their return.

multicache has been hyper-tuned for high performance, and deviates from the original paper in a handful of ways:

- **Dynamic sharding** - scales to 16×GOMAXPROCS shards; at 32 threads: 21x Get throughput, 6x Set throughput vs single shard
- **Tuned small queue** - 24.7% vs paper's 10%, chosen via sweep in 0.1% increments to maximize wins across 9 production traces
- **Full ghost frequency restoration** - returning keys restore 100% of their previous access count; +0.37% zipf, +0.05% meta, +0.04% tencentPhoto, +0.03% wikipedia
- **Extended frequency cap** - max freq=7 vs paper's 3; +0.9% meta, +0.8% zipf
- **Hot item demotion** - items that were once hot (freq≥4) get demoted to small queue instead of evicted; +0.24% zipf
- **Death row buffer** - 8-entry buffer per shard holds recently evicted items for instant resurrection; +0.04% meta/tencentPhoto, +0.03% wikipedia, +8% set throughput
- **Ghost frequency ring buffer** - fixed-size 256-entry ring replaces map allocations; -5.1% string latency, -44.5% memory

## License

Apache 2.0
