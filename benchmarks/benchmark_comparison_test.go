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

	for i := range n {
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
	for i := range 50000 {
		key := workload[i]
		if _, found, err := cache.Get(ctx, key); err == nil && !found {
			if err := cache.Set(ctx, key, i, 0); err != nil {
				b.Fatalf("Set failed: %v", err)
			}
		}
	}

	// Measurement phase
	hits := 0
	misses := 0

	b.ResetTimer()
	//nolint:intrange // b.N is dynamic and cannot use range
	for i := 0; i < b.N; i++ {
		key := workload[50000+i]

		if _, found, err := cache.Get(ctx, key); err == nil && found {
			hits++
		} else {
			misses++
			if err := cache.Set(ctx, key, i, 0); err != nil {
				b.Fatalf("Set failed: %v", err)
			}
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
	for i := range 50000 {
		key := workload[i]
		if _, found := cache.Get(key); !found {
			cache.Add(key, i)
		}
	}

	// Measurement phase
	hits := 0
	misses := 0

	b.ResetTimer()
	//nolint:intrange // b.N is dynamic and cannot use range
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
	for i := range 50000 {
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
	//nolint:intrange // b.N is dynamic and cannot use range
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
	for i := range 1000 {
		if err := cache.Set(ctx, i, i, 0); err != nil {
			b.Fatalf("Set failed: %v", err)
		}
	}

	b.ResetTimer()
	//nolint:intrange // b.N is dynamic and cannot use range
	for i := 0; i < b.N; i++ {
		if _, _, err := cache.Get(ctx, i%1000); err != nil {
			b.Fatalf("Get failed: %v", err)
		}
	}
}

// BenchmarkSpeed_LRU measures raw Get operation speed for golang-lru
func BenchmarkSpeed_LRU(b *testing.B) {
	cache, err := lru.New[int, int](benchSize)
	if err != nil {
		b.Fatal(err)
	}

	// Pre-populate
	for i := range 1000 {
		cache.Add(i, i)
	}

	b.ResetTimer()
	//nolint:intrange // b.N is dynamic and cannot use range
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
	for i := range 1000 {
		cache.Set(i, i, 1)
	}
	cache.Wait() // Ristretto sets are async

	b.ResetTimer()
	//nolint:intrange // b.N is dynamic and cannot use range
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
	for i := range 1000 {
		cache.Set(i, i)
	}

	b.ResetTimer()
	//nolint:intrange // b.N is dynamic and cannot use range
	for i := 0; i < b.N; i++ {
		_, _ = cache.GetIfPresent(i % 1000)
	}
}
