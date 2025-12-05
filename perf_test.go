//go:build !race

package sfcache

import (
	"testing"
	"time"
)

func TestMemoryCache_ReadPerformance(t *testing.T) {
	cache := Memory[int, int]()
	defer cache.Close()

	// Populate cache
	for i := range 10000 {
		cache.Set(i, i, 0)
	}

	// Warm up
	for i := range 1000 {
		cache.Get(i % 10000)
	}

	// Measure read performance
	const iterations = 100000
	start := time.Now()
	for i := range iterations {
		cache.Get(i % 10000)
	}
	elapsed := time.Since(start)
	nsPerOp := float64(elapsed.Nanoseconds()) / float64(iterations)

	const maxNsPerOp = 20.0
	if nsPerOp > maxNsPerOp {
		t.Errorf("single-threaded read performance: %.2f ns/op exceeds %.0f ns/op threshold", nsPerOp, maxNsPerOp)
	}
	t.Logf("single-threaded read performance: %.2f ns/op", nsPerOp)
}
