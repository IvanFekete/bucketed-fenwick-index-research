package concurrent

import (
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"testing"

	"github.com/IvanFekete/bucketed-fenwick-index/pkg/baseline/sortedslice"
	"github.com/IvanFekete/bucketed-fenwick-index/pkg/bfi"
	"github.com/IvanFekete/bucketed-fenwick-index/pkg/queue"
)

var dom = queue.Domain{Min: 0, Max: 3500}

// Sequentially, Striped must answer exactly like a single BFI (b = 1).
func TestStripedMatchesBFISequential(t *testing.T) {
	ref := bfi.New(bfi.Config{Domain: dom, Width: 1, Layout: bfi.FIFO, Counts: "fenwick"}, 0)
	st := NewStriped(dom, 1, 8, 0)
	exact := sortedslice.New(0)
	rng := rand.New(rand.NewSource(1))
	var ids []uint64
	rating := map[uint64]int{}
	for i := 0; i < 20000; i++ {
		if rng.Intn(3) > 0 || len(ids) == 0 {
			id := uint64(i + 1)
			r := dom.Clamp(1500 + int(rng.NormFloat64()*400))
			ref.Add(id, r)
			st.Add(id, r)
			rating[id] = r
			exact.Add(id, r)
			ids = append(ids, id)
		} else {
			j := rng.Intn(len(ids))
			ref.Remove(ids[j])
			st.Remove(ids[j])
			exact.Remove(ids[j])
			ids[j] = ids[len(ids)-1]
			ids = ids[:len(ids)-1]
		}
		r := rng.Intn(dom.Max)
		if a, b := st.Rank(r), ref.Rank(r); a != b {
			t.Fatalf("Rank(%d) striped %d bfi %d", r, a, b)
		}
		lo := rng.Intn(dom.Max)
		if a, b := st.RangeCount(lo, lo+200), ref.RangeCount(lo, lo+200); a != b {
			t.Fatalf("RangeCount striped %d bfi %d", a, b)
		}
		// Nearest across stripe boundaries: compare distance multisets with an exact structure.
		got := st.Nearest(r, 10, nil)
		want := exact.Nearest(r, 10, nil)
		if len(got) != len(want) {
			t.Fatalf("Nearest len %d want %d", len(got), len(want))
		}
		dist := func(ids []uint64) []int {
			var d []int
			for _, id := range ids {
				x := rating[id] - r
				if x < 0 {
					x = -x
				}
				d = append(d, x)
			}
			sort.Ints(d)
			return d
		}
		if g, w := dist(got), dist(want); fmt.Sprint(g) != fmt.Sprint(w) {
			t.Fatalf("Nearest(%d) distances %v want %v", r, g, w)
		}
	}
	if st.Len() != ref.Len() {
		t.Fatalf("Len %d vs %d", st.Len(), ref.Len())
	}
}

// Under concurrent churn no update is lost (run with -race).
func TestConcurrentChurn(t *testing.T) {
	for _, q := range []queue.Queue{
		NewStriped(dom, 1, 16, 0),
		NewLocked(bfi.New(bfi.Config{Domain: dom, Width: 1, Layout: bfi.FIFO, Counts: "fenwick"}, 0)),
	} {
		const G, per = 8, 2000
		var wg sync.WaitGroup
		for g := 0; g < G; g++ {
			wg.Add(1)
			go func(g int) {
				defer wg.Done()
				rng := rand.New(rand.NewSource(int64(g)))
				base := uint64(g) << 32
				var mine []uint64
				for i := 0; i < per; i++ {
					id := base + uint64(i) + 1
					q.Add(id, rng.Intn(dom.Max))
					mine = append(mine, id)
					if i%3 == 2 {
						q.Remove(mine[0])
						mine = mine[1:]
					}
					_ = q.Rank(rng.Intn(dom.Max))
					_ = q.Nearest(rng.Intn(dom.Max), 4, nil)
				}
			}(g)
		}
		wg.Wait()
		want := G * (per - per/3)
		if q.Len() != want {
			t.Fatalf("%s: Len %d want %d", q.Name(), q.Len(), want)
		}
		if q.Rank(dom.Max) != want {
			t.Fatalf("%s: Rank(max) %d want %d", q.Name(), q.Rank(dom.Max), want)
		}
	}
}
