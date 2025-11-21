package benchmarks

import (
	"context"
	"fmt"
	"testing"

	"github.com/codeGROOVE-dev/bdcache"

	lru "github.com/hashicorp/golang-lru/v2"
)

// This file contains hit rate comparison benchmarks designed to expose
// the differences between S3-FIFO and LRU eviction algorithms.

const cacheSize = 10000

// Workload 1: One-hit wonders mixed with hot items
// S3-FIFO should win because one-hit wonders stay in Small queue and don't evict hot items.
// LRU treats one-hit wonders the same as hot items, causing unnecessary evictions.
func generateOneHitWonderWorkload(n int) []int {
	keys := make([]int, n)
	hotSetSize := 5000 // Fits in cache
	oneHitWonderID := 100000

	for i := range n {
		if i%3 == 0 {
			// 33% one-hit wonders - each unique, accessed once
			keys[i] = oneHitWonderID
			oneHitWonderID++
		} else {
			// 67% hot set - repeatedly accessed
			keys[i] = i % hotSetSize
		}
	}
	return keys
}

// Workload 2: Scan pattern
// Periodic scan through a large dataset that shouldn't evict working set.
// S3-FIFO resists scans better than LRU.
func generateScanWorkload(n int) []int {
	keys := make([]int, n)
	workingSet := 8000 // Fits comfortably in cache
	scanSize := 50000  // Large scan that would evict everything in LRU

	scanCounter := 0
	for i := range n {
		if i%100 < 90 {
			// 90% working set access
			keys[i] = i % workingSet
		} else {
			// 10% scan through cold data
			keys[i] = 100000 + (scanCounter % scanSize)
			scanCounter++
		}
	}
	return keys
}

// Workload 3: Loop pattern with pollution
// Access pattern: A, B, C, D, ..., then pollute with one-time items, repeat loop
// S3-FIFO should keep the loop items even when polluted.
func generateLoopWorkload(n int) []int {
	keys := make([]int, n)
	loopSize := 6000 // Fits in cache

	pollutionID := 200000
	for i := range n {
		if i%10 < 8 {
			// 80% loop through working set
			keys[i] = i % loopSize
		} else {
			// 20% pollution - unique items accessed once
			keys[i] = pollutionID
			pollutionID++
		}
	}
	return keys
}

// runCacheWorkload executes a workload and returns hit rate
func runCacheWorkload(b *testing.B, workload []int, cacheName string) float64 {
	b.Helper()

	ctx := context.Background()
	var hits, misses int

	switch cacheName {
	case "bdcache":
		cache, err := bdcache.New[int, int](ctx, bdcache.WithMemorySize(cacheSize))
		if err != nil {
			b.Fatal(err)
		}

		for _, key := range workload {
			if _, found, err := cache.Get(ctx, key); err == nil && found {
				hits++
			} else {
				misses++
				if err := cache.Set(ctx, key, key, 0); err != nil {
					b.Fatalf("Set failed: %v", err)
				}
			}
		}

	case "golang-lru":
		cache, err := lru.New[int, int](cacheSize)
		if err != nil {
			b.Fatal(err)
		}

		for _, key := range workload {
			if _, found := cache.Get(key); found {
				hits++
			} else {
				misses++
				cache.Add(key, key)
			}
		}
	}

	return float64(hits) / float64(hits+misses) * 100
}

// Benchmark: One-hit wonders
func BenchmarkHitRate_OneHitWonders_bdcache(b *testing.B) {
	workload := generateOneHitWonderWorkload(100000)
	b.ResetTimer()
	hitRate := runCacheWorkload(b, workload, "bdcache")
	b.ReportMetric(hitRate, "hit%")
}

func BenchmarkHitRate_OneHitWonders_LRU(b *testing.B) {
	workload := generateOneHitWonderWorkload(100000)
	b.ResetTimer()
	hitRate := runCacheWorkload(b, workload, "golang-lru")
	b.ReportMetric(hitRate, "hit%")
}

// Benchmark: Scan resistance
func BenchmarkHitRate_Scan_bdcache(b *testing.B) {
	workload := generateScanWorkload(100000)
	b.ResetTimer()
	hitRate := runCacheWorkload(b, workload, "bdcache")
	b.ReportMetric(hitRate, "hit%")
}

func BenchmarkHitRate_Scan_LRU(b *testing.B) {
	workload := generateScanWorkload(100000)
	b.ResetTimer()
	hitRate := runCacheWorkload(b, workload, "golang-lru")
	b.ReportMetric(hitRate, "hit%")
}

// Benchmark: Loop with pollution
func BenchmarkHitRate_Loop_bdcache(b *testing.B) {
	workload := generateLoopWorkload(100000)
	b.ResetTimer()
	hitRate := runCacheWorkload(b, workload, "bdcache")
	b.ReportMetric(hitRate, "hit%")
}

func BenchmarkHitRate_Loop_LRU(b *testing.B) {
	workload := generateLoopWorkload(100000)
	b.ResetTimer()
	hitRate := runCacheWorkload(b, workload, "golang-lru")
	b.ReportMetric(hitRate, "hit%")
}

// Comparison test that runs all workloads and prints results
func TestHitRateComparison(t *testing.T) {
	workloads := map[string][]int{
		"One-hit wonders": generateOneHitWonderWorkload(100000),
		"Scan resistance": generateScanWorkload(100000),
		"Loop pollution":  generateLoopWorkload(100000),
	}

	fmt.Println("\nHit Rate Comparison: bdcache (S3-FIFO) vs golang-lru (LRU)")
	fmt.Println("Cache size: 10,000 items | Workload size: 100,000 operations")
	fmt.Println("================================================================================")

	for name, workload := range workloads {
		bdcacheRate := runCacheWorkload(&testing.B{}, workload, "bdcache")
		lruRate := runCacheWorkload(&testing.B{}, workload, "golang-lru")
		diff := bdcacheRate - lruRate

		fmt.Printf("\n%s:\n", name)
		fmt.Printf("  bdcache (S3-FIFO): %.2f%%\n", bdcacheRate)
		fmt.Printf("  golang-lru (LRU): %.2f%%\n", lruRate)
		switch {
		case diff > 0:
			fmt.Printf("  ✅ bdcache wins by %.2f percentage points\n", diff)
		case diff < 0:
			fmt.Printf("  ❌ LRU wins by %.2f percentage points\n", -diff)
		default:
			fmt.Printf("  🤝 Tie\n")
		}
	}
	fmt.Println()
}
