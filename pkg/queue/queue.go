// Package queue defines the contract every matchmaking-queue index in this
// study implements, so that BFI and all baselines run under one harness.
//
// Semantics (exact structures):
//   - Add inserts a player; the caller guarantees the ID is not present.
//   - Remove deletes a present player and returns its rating.
//   - Rank(r) = number of players with rating strictly less than r.
//   - RangeCount(lo, hi) = number of players with rating in [lo, hi].
//   - Nearest(r, k) returns up to k players minimising |rating - r|; among
//     equal distances, players that entered the queue earlier come first
//     when the structure keeps arrival order (FIFO), otherwise any order.
//
// A quantized structure (BFI with bucket width b > 1) answers the same calls
// at bucket granularity: ratings are first mapped to buckets and the
// semantics above apply to bucket indices. The induced error is what the
// paper measures.
package queue

// Queue is the common interface. Single-goroutine; concurrency wrappers live
// in package concurrent.
type Queue interface {
	Add(id uint64, rating int)
	Remove(id uint64) int
	Rank(r int) int
	RangeCount(lo, hi int) int
	// Nearest appends up to k IDs to out and returns it.
	Nearest(r, k int, out []uint64) []uint64
	Len() int
	Name() string
}

// Domain is the bounded rating domain [Min, Max).
type Domain struct {
	Min, Max int
}

// Clamp maps r into the domain.
func (d Domain) Clamp(r int) int {
	if r < d.Min {
		return d.Min
	}
	if r >= d.Max {
		return d.Max - 1
	}
	return r
}

// Width is the number of distinct integer ratings in the domain.
func (d Domain) Width() int { return d.Max - d.Min }
