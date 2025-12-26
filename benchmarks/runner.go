//go:build ignore

// runner.go runs gocachemark benchmarks and validates results.
//
// Usage:
//
//	go run benchmarks/runner.go                  # solo multicache, validate hitrate
//	go run benchmarks/runner.go -competitive    # gold medalists, track rankings
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// hitrateGoals are the minimum acceptable averages across all cache sizes.
// Keys must match gocachemark JSON output (camelCase).
var hitrateGoals = map[string]float64{
	"cdn":          58.3,
	"meta":         72.0,
	"twitter":      84.5,
	"wikipedia":    30.59,
	"thesiosBlock": 17.85,
	"thesiosFile":  88.03,
	"ibmDocker":    82.95,
	"tencentPhoto": 19.7,
}

// hitRateKeys maps display names to JSON keys for hit rate lookup.
var hitRateKeys = map[string]string{
	"CDN":           "cdn",
	"Meta":          "meta",
	"Zipf":          "zipf",
	"Twitter":       "twitter",
	"Wikipedia":     "wikipedia",
	"Thesios Block": "thesiosBlock",
	"Thesios File":  "thesiosFile",
	"IBM Docker":    "ibmDocker",
	"Tencent Photo": "tencentPhoto",
}

// goldMedalists are the caches to compare in competitive mode.
var goldMedalists = "multicache,otter,clock,theine,sieve,freelru-sync"

const (
	minMulticacheScore = 142
	gocachemarkRepo    = "github.com/tstromberg/gocachemark"
	multicacheModule   = "github.com/codeGROOVE-dev/multicache"
)

func main() {
	competitive := flag.Bool("competitive", false, "Run competitive benchmark with gold medalists")
	flag.Parse()

	// Find multicache root (where we're running from).
	multicacheDir, err := findMulticacheDir()
	if err != nil {
		fatal("finding multicache directory: %v", err)
	}

	// Find or clone gocachemark.
	gocachemarkDir, err := findOrCloneGocachemark(multicacheDir)
	if err != nil {
		fatal("finding gocachemark: %v", err)
	}

	// Update go.mod replace directive.
	cmd := exec.Command("go", "mod", "edit", "-replace", multicacheModule+"="+multicacheDir)
	cmd.Dir = gocachemarkDir
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fatal("updating go.mod replace: %v", err)
	}

	// Prepare output directory for results.
	benchmarksDir := filepath.Join(multicacheDir, "benchmarks")

	// Load previous results for comparison (competitive mode).
	var prevResults *Results
	if *competitive {
		prevResults, _ = loadResults(filepath.Join(benchmarksDir, "gocachemark_results.json"))
	}

	// Build gocachemark arguments.
	args := []string{"run", "."}
	var outdir string
	if *competitive {
		args = append(args, "-caches", goldMedalists)
		outdir = benchmarksDir
	} else {
		args = append(args, "-caches", "multicache")
		outdir, err = os.MkdirTemp("", "gocachemark-")
		if err != nil {
			fatal("creating temp directory: %v", err)
		}
		defer os.RemoveAll(outdir)
	}
	args = append(args, "-outdir", outdir)

	// Run gocachemark with streaming output.
	mode := "multicache"
	if *competitive {
		mode = "competitive"
	}
	fmt.Printf("Running %s benchmarks via gocachemark...\n\n", mode)
	results, err := runGocachemark(gocachemarkDir, args, outdir)
	if err != nil {
		fatal("running gocachemark: %v", err)
	}

	// Validate results.
	fmt.Println()
	if *competitive {
		if err := validateCompetitive(results, prevResults); err != nil {
			fatal("%v", err)
		}
		fmt.Printf("\nResults saved to %s/\n", benchmarksDir)
	} else {
		if err := validateHitrate(results); err != nil {
			fatal("%v", err)
		}
	}
}

func findMulticacheDir() (string, error) {
	// Look for go.mod with multicache module.
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		modPath := filepath.Join(dir, "go.mod")
		if data, err := os.ReadFile(modPath); err == nil {
			if strings.Contains(string(data), multicacheModule) {
				return dir, nil
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", fmt.Errorf("could not find multicache root (no go.mod with %s)", multicacheModule)
}

func findOrCloneGocachemark(multicacheDir string) (string, error) {
	// Check locations in order of preference.
	locations := []string{
		os.Getenv("GOCACHEMARK_DIR"),
		filepath.Join(os.Getenv("HOME"), "src", "gocachemark"),
		filepath.Join(multicacheDir, "out", "gocachemark"),
	}

	for _, loc := range locations {
		if loc == "" {
			continue
		}
		if isGocachemarkDir(loc) {
			return loc, nil
		}
	}

	// Clone to out/gocachemark.
	cloneDir := filepath.Join(multicacheDir, "out", "gocachemark")
	fmt.Printf("Cloning gocachemark to %s...\n", cloneDir)

	if err := os.MkdirAll(filepath.Dir(cloneDir), 0755); err != nil {
		return "", fmt.Errorf("creating out directory: %w", err)
	}

	cmd := exec.Command("git", "clone", "https://"+gocachemarkRepo, cloneDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("cloning gocachemark: %w", err)
	}

	return cloneDir, nil
}

func isGocachemarkDir(dir string) bool {
	mainGo := filepath.Join(dir, "main.go")
	if _, err := os.Stat(mainGo); err != nil {
		return false
	}
	goMod := filepath.Join(dir, "go.mod")
	data, err := os.ReadFile(goMod)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), gocachemarkRepo)
}

func runGocachemark(dir string, args []string, outdir string) (*Results, error) {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Stderr = os.Stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	// Stream output to stdout.
	s := bufio.NewScanner(stdout)
	for s.Scan() {
		fmt.Println(s.Text())
	}
	if err := s.Err(); err != nil {
		return nil, fmt.Errorf("reading output: %w", err)
	}
	if err := cmd.Wait(); err != nil {
		return nil, err
	}

	return loadResults(filepath.Join(outdir, "gocachemark_results.json"))
}

// Results represents gocachemark JSON output.
type Results struct {
	HitRate    map[string]json.RawMessage `json:"hitRate"`
	Rankings   []RankEntry                `json:"rankings"`
	MedalTable MedalTable                 `json:"medalTable"`
}

type MedalTable struct {
	Categories []Category `json:"categories"`
}

type Category struct {
	Name       string      `json:"name"`
	Benchmarks []Benchmark `json:"benchmarks"`
}

type Benchmark struct {
	Name   string `json:"name"`
	Gold   string `json:"gold"`
	Silver string `json:"silver"`
	Bronze string `json:"bronze"`
}

type CacheResult struct {
	Name    string  `json:"name"`
	AvgRate float64 `json:"avgRate"`
}

type RankEntry struct {
	Rank   int    `json:"rank"`
	Name   string `json:"name"`
	Score  int    `json:"score"`
	Gold   int    `json:"gold"`
	Silver int    `json:"silver"`
	Bronze int    `json:"bronze"`
}

type placement struct {
	medal string
	value float64
}

// hitRateResults extracts cache results for a test, skipping non-test fields like "sizes".
func (r *Results) hitRateResults(name string) ([]CacheResult, error) {
	raw, ok := r.HitRate[name]
	if !ok {
		return nil, fmt.Errorf("test %q not found", name)
	}
	var out []CacheResult
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func loadResults(path string) (*Results, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var results Results
	if err := json.Unmarshal(data, &results); err != nil {
		return nil, err
	}
	return &results, nil
}

func validateHitrate(res *Results) error {
	fmt.Println("=== Hitrate Validation ===")

	var fails []string
	for name, goal := range hitrateGoals {
		caches, err := res.hitRateResults(name)
		if err != nil {
			fmt.Printf("? %s: %v\n", name, err)
			continue
		}

		var avg float64
		var found bool
		for _, c := range caches {
			if c.Name == "multicache" {
				avg = c.AvgRate
				found = true
				break
			}
		}
		if !found {
			fmt.Printf("? %s: multicache not found\n", name)
			continue
		}

		// Use small tolerance for floating point comparison (0.01%).
		if avg >= goal-0.01 {
			fmt.Printf("✓ %s: %.2f%% (goal: %.2f%%)\n", name, avg, goal)
		} else {
			fmt.Printf("✗ %s: %.2f%% (goal: %.2f%%)\n", name, avg, goal)
			fails = append(fails, fmt.Sprintf("%s: %.2f%% < %.2f%%", name, avg, goal))
		}
	}

	if len(fails) > 0 {
		return fmt.Errorf("hitrate goals not met:\n  %s", strings.Join(fails, "\n  "))
	}
	fmt.Println("\nAll hitrate goals met!")
	return nil
}

func validateCompetitive(res, prev *Results) error {
	// Find multicache in rankings.
	var mc *RankEntry
	for i := range res.Rankings {
		if res.Rankings[i].Name == "multicache" {
			mc = &res.Rankings[i]
			break
		}
	}
	if mc == nil {
		return fmt.Errorf("multicache not found in rankings")
	}

	if prev != nil {
		fmt.Println("=== Ranking Changes ===")
		reportChanges(prev, res)
	}

	fmt.Println("\n=== Final Validation ===")

	var fails []string
	if mc.Score >= minMulticacheScore {
		fmt.Printf("✓ multicache score: %d (goal: ≥%d)\n", mc.Score, minMulticacheScore)
	} else {
		fmt.Printf("✗ multicache score: %d (goal: ≥%d)\n", mc.Score, minMulticacheScore)
		fails = append(fails, fmt.Sprintf("score %d < %d", mc.Score, minMulticacheScore))
	}

	if prev != nil {
		var prevScore int
		for _, r := range prev.Rankings {
			if r.Name == "multicache" {
				prevScore = r.Score
				break
			}
		}
		if mc.Score >= prevScore {
			fmt.Printf("✓ No point reduction (was %d, now %d)\n", prevScore, mc.Score)
		} else {
			fmt.Printf("✗ Point reduction: %d → %d\n", prevScore, mc.Score)
			fails = append(fails, fmt.Sprintf("points reduced from %d to %d", prevScore, mc.Score))
		}
	}

	if len(fails) > 0 {
		return fmt.Errorf("competitive validation failed:\n  %s", strings.Join(fails, "\n  "))
	}
	return nil
}

func reportChanges(prev, curr *Results) {
	prevP := buildPlacementMap(prev)
	currP := buildPlacementMap(curr)

	prevR := make(map[string]RankEntry)
	for _, r := range prev.Rankings {
		prevR[r.Name] = r
	}

	for _, r := range curr.Rankings {
		p, ok := prevR[r.Name]
		if !ok {
			fmt.Printf("%s: new entry with %d points\n", r.Name, r.Score)
			continue
		}

		delta := r.Score - p.Score
		if delta == 0 {
			continue
		}

		sign := "+"
		if delta < 0 {
			sign = ""
		}
		fmt.Printf("%s: %d → %d (%s%d points)\n", r.Name, p.Score, r.Score, sign, delta)

		// Show which benchmarks changed for this cache.
		for bench, cp := range currP[r.Name] {
			pp := prevP[r.Name][bench]
			if cp.medal == pp.medal {
				continue
			}
			pm, cm := pp.medal, cp.medal
			if pm == "" {
				pm = "none"
			}
			if cm == "" {
				cm = "none"
			}
			if cp.value != 0 && pp.value != 0 {
				fmt.Printf("  %s: %s → %s (%.2f%% → %.2f%%)\n", bench, pm, cm, pp.value, cp.value)
			} else {
				fmt.Printf("  %s: %s → %s\n", bench, pm, cm)
			}
		}
	}
}

func buildPlacementMap(r *Results) map[string]map[string]placement {
	out := make(map[string]map[string]placement)
	for _, rank := range r.Rankings {
		out[rank.Name] = make(map[string]placement)
	}

	for _, cat := range r.MedalTable.Categories {
		for _, b := range cat.Benchmarks {
			name := cat.Name + "/" + b.Name

			// Get hit rate values if applicable.
			var vals map[string]float64
			if cat.Name == "Hit Rate" {
				if key, ok := hitRateKeys[b.Name]; ok {
					if caches, err := r.hitRateResults(key); err == nil {
						vals = make(map[string]float64)
						for _, c := range caches {
							vals[c.Name] = c.AvgRate
						}
					}
				}
			}

			for cache := range out {
				var medal string
				switch cache {
				case b.Gold:
					medal = "gold"
				case b.Silver:
					medal = "silver"
				case b.Bronze:
					medal = "bronze"
				}
				var v float64
				if vals != nil {
					v = vals[cache]
				}
				out[cache][name] = placement{medal, v}
			}
		}
	}
	return out
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
