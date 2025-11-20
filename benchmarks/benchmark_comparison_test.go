package benchmarks

import (
	"context"
	"testing"

	"github.com/codeGROOVE-dev/bdcache"
	"github.com/dgraph-io/ristretto"
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/maypok86/otter/v2"
)

// Benchmark comparison against popular Go cache libraries.
// Tests realistic workload patterns to demonstrate S3-FIFO advantages.

const benchSize = 10000

// Workload that exposes S3-FIFO advantages over LRU
// Based on the S3-FIFO paper: mix of looping references and one-hit wonders
//
// Pattern:
// 1. Small working set (5K keys) accessed in a loop - fits in cache
// 2. Interspersed one-hit wonders (unique keys) that pollute LRU but not S3-FIFO
//
// S3-FIFO wins because one-hit wonders stay in Small queue and get evicted quickly,
// while LRU promotes them to the front, evicting useful loop items.
func generateWorkload(n int) []string {
	const loopSize = 5000       // Working set that fits in cache
	const oneHitWonders = 50000 // Large number of unique one-time accesses

	keys := make([]string, n)
	oneHitIndex := 0

	for i := 0; i < n; i++ {
		if i%4 == 0 {
			// 25% one-hit wonders (cache pollution)
			idx := oneHitIndex % oneHitWonders
			keys[i] = string(rune('W' + idx))
			oneHitIndex++
		} else {
			// 75% loop through working set
			keys[i] = string(rune('L' + (i % loopSize)))
		}
	}
	return keys
}

// BenchmarkHitRate_bdcache measures hit rate for bdcache with S3-FIFO
func BenchmarkHitRate_bdcache(b *testing.B) {
	ctx := context.Background()
	cache, err := bdcache.New[string, int](ctx, bdcache.WithMemorySize(benchSize))
	if err != nil {
		b.Fatal(err)
	}

	// Generate full workload including warmup
	totalOps := 50000 + b.N
	workload := generateWorkload(totalOps)

	// Warmup phase - not measured
	for i := 0; i < 50000; i++ {
		key := workload[i]
		if _, found, _ := cache.Get(ctx, key); !found {
			_ = cache.Set(ctx, key, i, 0)
		}
	}

	// Measurement phase
	hits := 0
	misses := 0

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := workload[50000+i]

		if _, found, _ := cache.Get(ctx, key); found {
			hits++
		} else {
			misses++
			_ = cache.Set(ctx, key, i, 0)
		}
	}
	b.StopTimer()

	hitRate := float64(hits) / float64(hits+misses) * 100
	b.ReportMetric(hitRate, "hit%")
}

// BenchmarkHitRate_LRU measures hit rate for hashicorp/golang-lru (standard LRU)
func BenchmarkHitRate_LRU(b *testing.B) {
	cache, err := lru.New[string, int](benchSize)
	if err != nil {
		b.Fatal(err)
	}

	// Generate full workload including warmup
	totalOps := 50000 + b.N
	workload := generateWorkload(totalOps)

	// Warmup phase - not measured
	for i := 0; i < 50000; i++ {
		key := workload[i]
		if _, found := cache.Get(key); !found {
			cache.Add(key, i)
		}
	}

	// Measurement phase
	hits := 0
	misses := 0

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := workload[50000+i]

		if _, found := cache.Get(key); found {
			hits++
		} else {
			misses++
			cache.Add(key, i)
		}
	}
	b.StopTimer()

	hitRate := float64(hits) / float64(hits+misses) * 100
	b.ReportMetric(hitRate, "hit%")
}

// BenchmarkHitRate_ristretto measures hit rate for Ristretto (TinyLFU)
func BenchmarkHitRate_ristretto(b *testing.B) {
	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: benchSize * 10,
		MaxCost:     benchSize,
		BufferItems: 64,
	})
	if err != nil {
		b.Fatal(err)
	}
	defer cache.Close()

	// Generate full workload including warmup
	totalOps := 50000 + b.N
	workload := generateWorkload(totalOps)

	// Warmup phase - not measured
	for i := 0; i < 50000; i++ {
		key := workload[i]
		if _, found := cache.Get(key); !found {
			cache.Set(key, i, 1)
		}
	}
	cache.Wait() // Ristretto uses async writes

	// Measurement phase
	hits := 0
	misses := 0

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := workload[50000+i]

		if _, found := cache.Get(key); found {
			hits++
		} else {
			misses++
			cache.Set(key, i, 1)
		}
	}
	b.StopTimer()

	hitRate := float64(hits) / float64(hits+misses) * 100
	b.ReportMetric(hitRate, "hit%")
}

// BenchmarkSpeed_bdcache measures raw Get operation speed for bdcache
func BenchmarkSpeed_bdcache(b *testing.B) {
	ctx := context.Background()
	cache, err := bdcache.New[int, int](ctx, bdcache.WithMemorySize(benchSize))
	if err != nil {
		b.Fatal(err)
	}

	// Pre-populate
	for i := 0; i < 1000; i++ {
		_ = cache.Set(ctx, i, i, 0)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = cache.Get(ctx, i%1000)
	}
}

// BenchmarkSpeed_LRU measures raw Get operation speed for golang-lru
func BenchmarkSpeed_LRU(b *testing.B) {
	cache, err := lru.New[int, int](benchSize)
	if err != nil {
		b.Fatal(err)
	}

	// Pre-populate
	for i := 0; i < 1000; i++ {
		cache.Add(i, i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = cache.Get(i % 1000)
	}
}

// BenchmarkSpeed_ristretto measures raw Get operation speed for Ristretto
func BenchmarkSpeed_ristretto(b *testing.B) {
	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: benchSize * 10,
		MaxCost:     benchSize,
		BufferItems: 64,
	})
	if err != nil {
		b.Fatal(err)
	}
	defer cache.Close()

	// Pre-populate
	for i := 0; i < 1000; i++ {
		cache.Set(i, i, 1)
	}
	cache.Wait() // Ristretto sets are async

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = cache.Get(i % 1000)
	}
}

// BenchmarkSpeed_otter measures raw Get operation speed for Otter
func BenchmarkSpeed_otter(b *testing.B) {
	cache := otter.Must(&otter.Options[int, int]{
		MaximumSize: benchSize,
	})

	// Pre-populate
	for i := 0; i < 1000; i++ {
		cache.Set(i, i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = cache.GetIfPresent(i % 1000)
	}
}
