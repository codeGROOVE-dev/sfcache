package multicache

import (
	"math"
	"math/bits"
)

// bloomFilter is a space-efficient probabilistic data structure.
type bloomFilter struct {
	data    []uint64
	mask    uint64 // size - 1 (assuming power of 2 size)
	k       int    // number of hash functions
	entries int    // number of entries added
}

// newBloomFilter creates a new Bloom filter optimized for the given capacity
// and false positive rate.
func newBloomFilter(capacity int, fpRate float64) *bloomFilter {
	// Calculate optimal m (bits) and k (hashes)
	// m = -n * ln(p) / (ln(2)^2)
	// k = m/n * ln(2)

	// Ensure capacity is at least 1
	if capacity < 1 {
		capacity = 1
	}

	ln2 := math.Log(2)
	m := float64(capacity) * -math.Log(fpRate) / (ln2 * ln2)

	// Round m up to nearest power of 2 for fast modulo
	mInt := uint64(1) << bits.Len64(uint64(m)-1)
	mInt = max(mInt, 64)

	// Calculate k
	// k = (m / n) * ln(2)
	// Clamp k between 1 and 16 (cap to avoid too many memory accesses)
	k := min(max(int(float64(mInt)/float64(capacity)*ln2), 1), 16)

	return &bloomFilter{
		data: make([]uint64, mInt/64),
		mask: mInt - 1,
		k:    k,
	}
}

// Add adds a hash to the filter.
func (b *bloomFilter) Add(h uint64) {
	h1 := h
	h2 := h >> 32
	// If the original hash was only 32-bit effective or we want more mixing:
	// Use a simple mixer for h2 if just shifting isn't enough?
	// But wyhash is 64-bit and high quality.
	// For standard double hashing: gi(x) = h1(x) + i*h2(x)

	for i := range b.k {
		//nolint:gosec // G115: i is bounded by b.k which is capped at 16
		idx := (h1 + uint64(i)*h2) & b.mask
		b.data[idx/64] |= 1 << (idx % 64)
	}
	b.entries++
}

// Contains checks if the hash is in the filter.
func (b *bloomFilter) Contains(h uint64) bool {
	h1 := h
	h2 := h >> 32

	for i := range b.k {
		//nolint:gosec // G115: i is bounded by b.k which is capped at 16
		idx := (h1 + uint64(i)*h2) & b.mask
		if b.data[idx/64]&(1<<(idx%64)) == 0 {
			return false
		}
	}
	return true
}

// Reset clears the filter.
func (b *bloomFilter) Reset() {
	for i := range b.data {
		b.data[i] = 0
	}
	b.entries = 0
}

// hashInt64 is a fast hash for int64 keys.
// Uses a reversible mixing function (SplitMix64 variant).
func hashInt64(x int64) uint64 {
	//nolint:gosec // G115: intentional bit reinterpretation for hashing
	z := uint64(x)
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	z ^= z >> 31
	return z
}
