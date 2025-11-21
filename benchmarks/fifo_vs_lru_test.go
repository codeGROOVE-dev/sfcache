package benchmarks

import (
	"context"
	"fmt"
	"testing"

	"github.com/codeGROOVE-dev/bdcache"
	lru "github.com/hashicorp/golang-lru/v2"
)

// This test creates a workload where S3-FIFO SHOULD beat LRU:
// 1. Working set of 8K items (fits in 10K cache)
// 2. One-time scan through 10K items (should not evict working set in S3-FIFO)
// 3. Re-access working set (should hit in S3-FIFO, miss in LRU)
func TestFIFOvsLRU_ScanResistance(t *testing.T) {
	const cacheSize = 10000
	const workingSetSize = 8000
	const scanSize = 10000

	fmt.Println("\n=== S3-FIFO vs LRU: Scan Resistance Test ===")
	fmt.Printf("Cache size: %d | Working set: %d | Scan size: %d\n\n", cacheSize, workingSetSize, scanSize)

	// Test S3-FIFO
	ctx := context.Background()
	s3Cache, err := bdcache.New[int, int](ctx, bdcache.WithMemorySize(cacheSize))
	if err != nil {
		fmt.Printf("Failed to create cache: %v\n", err)
		return
	}

	// Phase 1: Build working set
	fmt.Println("Phase 1: Build working set (both caches)")
	for i := range workingSetSize {
		if err := s3Cache.Set(ctx, i, i, 0); err != nil {
			fmt.Printf("Set error: %v\n", err)
		}
	}

	// Phase 2: Access working set once (marks as hot in S3-FIFO)
	fmt.Println("Phase 2: Access working set (marks as hot)")
	for i := range workingSetSize {
		if _, _, err := s3Cache.Get(ctx, i); err != nil {
			fmt.Printf("Get error: %v\n", err)
		}
	}

	fmt.Printf("  S3-FIFO after warmup: items=%d\n", s3Cache.Len())

	// Phase 3: One-time scan through large dataset
	fmt.Println("Phase 3: One-time scan through cold data")
	for i := 100000; i < 100000+scanSize; i++ {
		if err := s3Cache.Set(ctx, i, i, 0); err != nil {
			fmt.Printf("Set error: %v\n", err)
		}
	}
	fmt.Printf("  S3-FIFO after scan: items=%d\n", s3Cache.Len())

	// Phase 4: Re-access working set
	fmt.Println("Phase 4: Re-access working set")
	s3Hits := 0
	for i := range workingSetSize {
		if _, found, err := s3Cache.Get(ctx, i); err != nil {
			fmt.Printf("Get error: %v\n", err)
		} else if found {
			s3Hits++
		}
	}

	fmt.Printf("  S3-FIFO hits: %d/%d (%.1f%%)\n", s3Hits, workingSetSize, float64(s3Hits)/float64(workingSetSize)*100)

	// Now test LRU with same workload
	lruCache, err := lru.New[int, int](cacheSize)
	if err != nil {
		fmt.Printf("Failed to create LRU cache: %v\n", err)
		return
	}

	// Phase 1: Build working set
	for i := range workingSetSize {
		lruCache.Add(i, i)
	}

	// Phase 2: Access working set once
	for i := range workingSetSize {
		_, _ = lruCache.Get(i)
	}

	// Phase 3: One-time scan
	for i := 100000; i < 100000+scanSize; i++ {
		lruCache.Add(i, i)
	}

	// Phase 4: Re-access working set
	lruHits := 0
	for i := range workingSetSize {
		if _, found := lruCache.Get(i); found {
			lruHits++
		}
	}

	fmt.Printf("  LRU hits: %d/%d (%.1f%%)\n", lruHits, workingSetSize, float64(lruHits)/float64(workingSetSize)*100)

	fmt.Printf("\n✨ S3-FIFO advantage: +%.1f percentage points\n",
		float64(s3Hits-lruHits)/float64(workingSetSize)*100)

	// S3-FIFO should do better
	if s3Hits <= lruHits {
		t.Errorf("S3-FIFO should beat LRU on scan resistance: S3=%d, LRU=%d", s3Hits, lruHits)
	}
}
