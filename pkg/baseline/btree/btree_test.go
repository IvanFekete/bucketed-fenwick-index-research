package btree

import (
	"math/rand"
	"testing"
)

// Depth stays logarithmic under long insert/delete churn at constant size.
func TestDepthUnderChurn(t *testing.T) {
	tr := New(0)
	rng := rand.New(rand.NewSource(1))
	const n = 50000
	ids := make([]uint64, n)
	for i := range ids {
		ids[i] = uint64(i + 1)
		tr.Add(ids[i], 1000+rng.Intn(2000))
	}
	next := uint64(n + 1)
	for i := 0; i < 20*n; i++ {
		j := rng.Intn(n)
		tr.Remove(ids[j])
		ids[j] = next
		tr.Add(next, 1000+rng.Intn(2000))
		next++
	}
	if d := tr.Depth(); d > 6 {
		t.Fatalf("depth %d after churn", d)
	}
	if tr.Len() != n {
		t.Fatalf("len %d", tr.Len())
	}
}
