// Isolated-operation micro-benchmarks. Every structure is populated with the
// same N players drawn from the rating sampler, then one operation type is
// timed. Run on the benchmark host with:
//
//	go test ./bench -run=^$ -bench=. -benchmem -count=20 -cpu=1 | tee raw.txt
//
// Ratings come from an empirical sample (data/derived/ratings_sample.txt, one
// integer per line) when present, otherwise from a Gaussian stand-in; the
// source is printed so results files are self-describing.
package bench

import (
	"bufio"
	"fmt"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/IvanFekete/bucketed-fenwick-index/pkg/baseline/btree"
	"github.com/IvanFekete/bucketed-fenwick-index/pkg/baseline/ostree"
	"github.com/IvanFekete/bucketed-fenwick-index/pkg/baseline/skiplist"
	"github.com/IvanFekete/bucketed-fenwick-index/pkg/baseline/sortedslice"
	"github.com/IvanFekete/bucketed-fenwick-index/pkg/bfi"
	"github.com/IvanFekete/bucketed-fenwick-index/pkg/queue"
)

var dom = queue.Domain{Min: 0, Max: 3500}

var (
	sampleOnce sync.Once
	sample     []int
)

func ratings() []int {
	sampleOnce.Do(func() {
		path := os.Getenv("BFI_RATINGS")
		if path == "" {
			path = "../data/derived/ratings_sample.txt"
		}
		if f, err := os.Open(path); err == nil {
			defer f.Close()
			sc := bufio.NewScanner(f)
			for sc.Scan() {
				if v, err := strconv.Atoi(sc.Text()); err == nil {
					sample = append(sample, dom.Clamp(v))
				}
			}
			fmt.Fprintf(os.Stderr, "ratings: empirical sample %s (%d values)\n", path, len(sample))
		}
		if len(sample) == 0 {
			rng := rand.New(rand.NewSource(7))
			for i := 0; i < 1<<20; i++ {
				sample = append(sample, dom.Clamp(int(1500+rng.NormFloat64()*350)))
			}
			fmt.Fprintln(os.Stderr, "ratings: Gaussian stand-in N(1500, 350)")
		}
	})
	return sample
}

type maker struct {
	name string
	mk   func(n int) queue.Queue
}

func makers() []maker {
	m := []maker{
		{"sortedslice", func(n int) queue.Queue { return sortedslice.New(n) }},
		{"skiplist", func(n int) queue.Queue { return skiplist.NewSized(n) }},
		{"ostree", func(n int) queue.Queue { return ostree.New(n) }},
		{"btree", func(n int) queue.Queue { return btree.New(n) }},
	}
	add := func(b int, c string, lay bfi.Layout) {
		name := bfi.New(bfi.Config{Domain: dom, Width: b, Layout: lay, Counts: c}, 0).Name()
		m = append(m, maker{name, func(n int) queue.Queue {
			return bfi.New(bfi.Config{Domain: dom, Width: b, Layout: lay, Counts: c}, n)
		}})
	}
	// b = 1: every count index and both list layouts (ablation).
	for _, c := range []string{"fenwick", "flat", "sqrt", ""} {
		for _, lay := range []bfi.Layout{bfi.FIFO, bfi.Swap} {
			add(1, c, lay)
		}
	}
	// Bucket-width sweep for the main variant only.
	for _, b := range []int{5, 10, 25, 50, 100} {
		add(b, "fenwick", bfi.FIFO)
	}
	return m
}

func populate(q queue.Queue, n int, rng *rand.Rand) []uint64 {
	rs := ratings()
	ids := make([]uint64, n)
	vals := make([]int, n)
	for i := range vals {
		vals[i] = rs[rng.Intn(len(rs))]
	}
	if q.Name() == "sortedslice" {
		// Insert in rating order: O(1) appends instead of O(n) shifts. The
		// resulting array is identical to random-order insertion.
		sort.Ints(vals)
	}
	for i := range ids {
		ids[i] = uint64(i + 1)
		q.Add(ids[i], vals[i])
	}
	return ids
}

// built returns a populated structure, building it once per process.
// testing.B calls a benchmark function several times while it calibrates
// b.N; rebuilding a million-player structure on every call dominated the run
// time of the first bare-metal attempt. All benchmarks of one (structure, n)
// share the instance; churn keeps the population size constant.
type builtQ struct {
	q   queue.Queue
	ids []uint64
}

// freshID hands out player IDs that were never used in this process, so that
// churn on a cached structure never re-adds an ID that is still present.
var idSeq atomic.Uint64

func init() { idSeq.Store(1 << 40) }

func freshID() uint64 { return idSeq.Add(1) }

var (
	builtMu    sync.Mutex
	builtCache = map[string]*builtQ{}
)

func built(m maker, n int) (queue.Queue, []uint64) {
	builtMu.Lock()
	defer builtMu.Unlock()
	key := fmt.Sprintf("%s/%d", m.name, n)
	if b, ok := builtCache[key]; ok {
		return b.q, b.ids
	}
	q := m.mk(n)
	ids := populate(q, n, rand.New(rand.NewSource(12345)))
	builtCache[key] = &builtQ{q, ids}
	return q, ids
}

var sizes = []int{10_000, 100_000, 1_000_000}

var sink int

func BenchmarkRank(b *testing.B) {
	for _, n := range sizes {
		for _, m := range makers() {
			b.Run(fmt.Sprintf("n=%d/%s", n, m.name), func(b *testing.B) {
				rng := rand.New(rand.NewSource(1))
				q, _ := built(m, n)
				rs := ratings()
				qs := make([]int, 4096)
				for i := range qs {
					qs[i] = rs[rng.Intn(len(rs))]
				}
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					sink += q.Rank(qs[i&4095])
				}
			})
		}
	}
}

func BenchmarkRangeCount(b *testing.B) {
	for _, n := range sizes {
		for _, m := range makers() {
			b.Run(fmt.Sprintf("n=%d/%s", n, m.name), func(b *testing.B) {
				rng := rand.New(rand.NewSource(2))
				q, _ := built(m, n)
				rs := ratings()
				qs := make([][2]int, 4096)
				for i := range qs {
					c := rs[rng.Intn(len(rs))]
					d := 25 + rng.Intn(200)
					qs[i] = [2]int{c - d, c + d}
				}
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					w := qs[i&4095]
					sink += q.RangeCount(w[0], w[1])
				}
			})
		}
	}
}

func BenchmarkNearest8(b *testing.B) {
	for _, n := range sizes {
		for _, m := range makers() {
			b.Run(fmt.Sprintf("n=%d/%s", n, m.name), func(b *testing.B) {
				rng := rand.New(rand.NewSource(3))
				q, _ := built(m, n)
				rs := ratings()
				qs := make([]int, 4096)
				for i := range qs {
					qs[i] = rs[rng.Intn(len(rs))]
				}
				out := make([]uint64, 0, 8)
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					out = q.Nearest(qs[i&4095], 8, out[:0])
				}
				sink += len(out)
			})
		}
	}
}

// BenchmarkChurn times one remove + one add (a player leaving and a new one
// arriving), keeping the population constant.
func BenchmarkChurn(b *testing.B) {
	for _, n := range sizes {
		for _, m := range makers() {
			if m.name == "sortedslice" && n > 100_000 {
				continue
			}
			b.Run(fmt.Sprintf("n=%d/%s", n, m.name), func(b *testing.B) {
				rng := rand.New(rand.NewSource(4))
				q, ids := built(m, n)
				rs := ratings()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					j := rng.Intn(len(ids))
					q.Remove(ids[j])
					ids[j] = freshID()
					q.Add(ids[j], rs[rng.Intn(len(rs))])
				}
			})
		}
	}
}
