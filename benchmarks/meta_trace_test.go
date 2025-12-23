//nolint:errcheck // benchmark code - errors not critical for performance measurement
package benchmarks

// Meta trace benchmark using real Meta KVCache trace data.
// The trace is embedded as a zstd-compressed file and decompressed once on first use.

import (
	"bufio"
	_ "embed"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/codeGROOVE-dev/multicache"
	"github.com/coocood/freecache"
	"github.com/dgraph-io/ristretto"
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/klauspost/compress/zstd"
	"github.com/maypok86/otter/v2"
	"github.com/vmihailenco/go-tinylfu"
)

//go:embed testdata/meta_trace_10m.csv.zst
var metaTraceCompressed []byte

// Cached trace operations - decompressed once on first use
var (
	metaTraceOps  []traceOp
	metaTraceOnce sync.Once
	errMetaTrace  error
)

// loadMetaTraceOnce decompresses the embedded trace data exactly once.
func loadMetaTraceOnce() ([]traceOp, error) {
	metaTraceOnce.Do(func() {
		decoder, err := zstd.NewReader(nil)
		if err != nil {
			errMetaTrace = fmt.Errorf("create zstd decoder: %w", err)
			return
		}
		defer decoder.Close()

		decompressed, err := decoder.DecodeAll(metaTraceCompressed, nil)
		if err != nil {
			errMetaTrace = fmt.Errorf("decompress trace: %w", err)
			return
		}

		// Parse CSV lines (format: key,op)
		scanner := bufio.NewScanner(strings.NewReader(string(decompressed)))
		ops := make([]traceOp, 0, 10_000_000)

		for scanner.Scan() {
			line := scanner.Text()
			parts := strings.SplitN(line, ",", 2)
			if len(parts) < 2 {
				continue
			}
			ops = append(ops, traceOp{
				key: parts[0],
				op:  parts[1],
			})
		}

		if err := scanner.Err(); err != nil {
			errMetaTrace = fmt.Errorf("scan trace: %w", err)
			return
		}

		metaTraceOps = ops
	})

	return metaTraceOps, errMetaTrace
}

type traceOp struct {
	key string
	op  string // "GET" or "SET"
}

type metaTraceResult struct {
	name  string
	rates []float64
}

// runMetaTraceHitRate runs the Meta trace benchmark and prints results.
// Used by TestBenchmarkSuite.
func runMetaTraceHitRate() {
	ops, err := loadMetaTraceOnce()
	if err != nil {
		fmt.Printf("Error loading trace: %v\n", err)
		return
	}

	fmt.Println()
	fmt.Printf("### Meta Trace Hit Rate (10M ops from Meta KVCache)\n")
	fmt.Println()
	fmt.Println("| Cache         | 50K cache | 100K cache |")
	fmt.Println("|---------------|-----------|------------|")

	caches := []struct {
		name string
		fn   func([]traceOp, int) float64
	}{
		{"multicache", runMetaTraceMulticache},
		{"otter", runMetaTraceOtter},
		{"ristretto", runMetaTraceRistretto},
		{"tinylfu", runMetaTraceTinyLFU},
		{"freecache", runMetaTraceFreecache},
		{"lru", runMetaTraceLRU},
	}

	results := make([]metaTraceResult, len(caches))

	for i, c := range caches {
		rate50k := c.fn(ops, 50_000)
		rate100k := c.fn(ops, 100_000)
		results[i] = metaTraceResult{name: c.name, rates: []float64{rate50k, rate100k}}
		fmt.Printf("| %s |   %5.2f%%  |   %5.2f%%   |\n",
			formatCacheName(c.name), rate50k*100, rate100k*100)
	}

	fmt.Println()
	printMetaTraceSummary(results)
}

func printMetaTraceSummary(results []metaTraceResult) {
	// Find multicache and best performer at 100K
	var multicacheRate, bestRate float64
	var bestName string
	multicacheIdx := -1

	for i, r := range results {
		rate := r.rates[1] // 100K cache size
		if r.name == "multicache" {
			multicacheRate = rate
			multicacheIdx = i
		}
		if rate > bestRate {
			bestRate = rate
			bestName = r.name
		}
	}

	if multicacheIdx < 0 {
		return
	}

	if multicacheRate >= bestRate {
		// Find second best
		var secondBest float64
		var secondName string
		for _, r := range results {
			rate := r.rates[1]
			if r.name != "multicache" && rate > secondBest {
				secondBest = rate
				secondName = r.name
			}
		}
		pct := (multicacheRate - secondBest) / secondBest * 100
		fmt.Printf("- 🔥 Meta trace: %s better than next best (%s)\n\n", formatPercent(pct), secondName)
	} else {
		pct := (bestRate - multicacheRate) / multicacheRate * 100
		fmt.Printf("- 💧 Meta trace: %s worse than best (%s)\n\n", formatPercent(pct), bestName)
	}
}

// TestMetaTrace benchmarks cache implementations against the Meta KVCache trace.
// Run with: go test -run=TestMetaTrace -v
func TestMetaTrace(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping meta trace benchmark in short mode")
	}

	ops, err := loadMetaTraceOnce()
	if err != nil {
		t.Fatalf("failed to load trace: %v", err)
	}
	t.Logf("Loaded %d operations from Meta trace", len(ops))

	// Count unique keys
	uniqueKeys := make(map[string]struct{})
	for _, op := range ops {
		uniqueKeys[op.key] = struct{}{}
	}
	t.Logf("Unique keys: %d", len(uniqueKeys))

	// Benchmark at 50K and 100K cache sizes
	for _, cacheSize := range []int{50_000, 100_000} {
		fmt.Printf("\n### Cache Size: %d\n\n", cacheSize)
		fmt.Println("| Cache      | Hit Ratio |")
		fmt.Println("|------------|-----------|")

		results := []struct {
			name    string
			hitRate float64
		}{
			{"multicache", runMetaTraceMulticache(ops, cacheSize)},
			{"otter", runMetaTraceOtter(ops, cacheSize)},
			{"ristretto", runMetaTraceRistretto(ops, cacheSize)},
			{"tinylfu", runMetaTraceTinyLFU(ops, cacheSize)},
			{"freecache", runMetaTraceFreecache(ops, cacheSize)},
			{"lru", runMetaTraceLRU(ops, cacheSize)},
		}

		for _, r := range results {
			fmt.Printf("| %-10s | %7.2f%% |\n", r.name, r.hitRate*100)
		}
	}
}

func runMetaTraceMulticache(ops []traceOp, cacheSize int) float64 {
	cache := multicache.New[string, string](multicache.Size(cacheSize))
	defer cache.Close()

	var hits, misses int64
	for _, op := range ops {
		switch op.op {
		case "GET":
			if _, ok := cache.Get(op.key); ok {
				hits++
			} else {
				misses++
				cache.Set(op.key, op.key)
			}
		case "SET":
			cache.Set(op.key, op.key)
		}
	}
	return float64(hits) / float64(hits+misses)
}

func runMetaTraceOtter(ops []traceOp, cacheSize int) float64 {
	cache := otter.Must(&otter.Options[string, string]{MaximumSize: cacheSize})

	var hits, misses int64
	for _, op := range ops {
		switch op.op {
		case "GET":
			if _, ok := cache.GetIfPresent(op.key); ok {
				hits++
			} else {
				misses++
				cache.Set(op.key, op.key)
			}
		case "SET":
			cache.Set(op.key, op.key)
		}
	}
	return float64(hits) / float64(hits+misses)
}

func runMetaTraceLRU(ops []traceOp, cacheSize int) float64 {
	cache, _ := lru.New[string, string](cacheSize)

	var hits, misses int64
	for _, op := range ops {
		switch op.op {
		case "GET":
			if _, ok := cache.Get(op.key); ok {
				hits++
			} else {
				misses++
				cache.Add(op.key, op.key)
			}
		case "SET":
			cache.Add(op.key, op.key)
		}
	}
	return float64(hits) / float64(hits+misses)
}

func runMetaTraceRistretto(ops []traceOp, cacheSize int) float64 {
	cache, _ := ristretto.NewCache(&ristretto.Config{
		NumCounters:        int64(cacheSize) * 10,
		MaxCost:            int64(cacheSize),
		BufferItems:        64,
		IgnoreInternalCost: true,
	})
	defer cache.Close()

	var hits, misses int64
	for _, op := range ops {
		switch op.op {
		case "GET":
			if _, ok := cache.Get(op.key); ok {
				hits++
			} else {
				misses++
				cache.Set(op.key, op.key, 1)
			}
		case "SET":
			cache.Set(op.key, op.key, 1)
		}
	}
	return float64(hits) / float64(hits+misses)
}

func runMetaTraceTinyLFU(ops []traceOp, cacheSize int) float64 {
	cache := tinylfu.New(cacheSize, cacheSize*10)

	var hits, misses int64
	for _, op := range ops {
		switch op.op {
		case "GET":
			if _, ok := cache.Get(op.key); ok {
				hits++
			} else {
				misses++
				cache.Set(&tinylfu.Item{Key: op.key, Value: op.key})
			}
		case "SET":
			cache.Set(&tinylfu.Item{Key: op.key, Value: op.key})
		}
	}
	return float64(hits) / float64(hits+misses)
}

func runMetaTraceFreecache(ops []traceOp, cacheSize int) float64 {
	// freecache uses bytes, estimate ~32 bytes per entry (key + value + overhead)
	cacheBytes := max(cacheSize*32, 512*1024) // minimum 512KB
	cache := freecache.NewCache(cacheBytes)

	var hits, misses int64
	for _, op := range ops {
		keyBytes := []byte(op.key)
		switch op.op {
		case "GET":
			if _, err := cache.Get(keyBytes); err == nil {
				hits++
			} else {
				misses++
				cache.Set(keyBytes, keyBytes, 0)
			}
		case "SET":
			cache.Set(keyBytes, keyBytes, 0)
		}
	}
	return float64(hits) / float64(hits+misses)
}
