// Property-based correctness gate: every structure is driven by the same long
// random operation sequence and compared, after every query, with a
// brute-force oracle. Quantized BFI variants are compared with the oracle
// evaluated on bucketed ratings, so the test checks the implementation, not
// the quantization error (which is measured separately).
package check

import (
	"math/rand"
	"sort"
	"testing"

	"github.com/IvanFekete/bucketed-fenwick-index/pkg/baseline/btree"
	"github.com/IvanFekete/bucketed-fenwick-index/pkg/baseline/ostree"
	"github.com/IvanFekete/bucketed-fenwick-index/pkg/baseline/skiplist"
	"github.com/IvanFekete/bucketed-fenwick-index/pkg/baseline/sortedslice"
	"github.com/IvanFekete/bucketed-fenwick-index/pkg/bfi"
	"github.com/IvanFekete/bucketed-fenwick-index/pkg/queue"
)

var dom = queue.Domain{Min: 0, Max: 3500}

type player struct {
	r   int
	seq int
}

type oracle struct {
	b   int
	p   map[uint64]player
	seq int
}

func (o *oracle) q(r int) int { return (dom.Clamp(r) - dom.Min) / o.b }

func (o *oracle) rank(r int) int {
	if r <= dom.Min {
		return 0
	}
	if r >= dom.Max {
		return len(o.p)
	}
	c := 0
	for _, pl := range o.p {
		if o.q(pl.r) < o.q(r) {
			c++
		}
	}
	return c
}

func (o *oracle) rangeCount(lo, hi int) int {
	if lo > hi || hi < dom.Min || lo >= dom.Max {
		return 0
	}
	c := 0
	for _, pl := range o.p {
		if o.q(pl.r) >= o.q(lo) && o.q(pl.r) <= o.q(hi) {
			c++
		}
	}
	return c
}

type cand struct {
	id  uint64
	d   int
	up  bool
	seq int
}

// nearest returns the oracle order: bucket distance, lower side first, then
// arrival order.
func (o *oracle) nearest(r, k int) []cand {
	c := o.q(r)
	all := make([]cand, 0, len(o.p))
	for id, pl := range o.p {
		qb := o.q(pl.r)
		d := qb - c
		up := d > 0
		if d < 0 {
			d = -d
		}
		all = append(all, cand{id, d, up, pl.seq})
	}
	sort.Slice(all, func(i, j int) bool {
		a, b := all[i], all[j]
		if a.d != b.d {
			return a.d < b.d
		}
		if a.up != b.up {
			return !a.up
		}
		return a.seq < b.seq
	})
	if len(all) > k {
		all = all[:k]
	}
	return all
}

type subject struct {
	q        queue.Queue
	b        int
	fifo     bool // strict order check (bucket distance, lower first, arrival)
	quantize bool // compare at bucket granularity
}

func subjects(capHint int) []subject {
	var s []subject
	for _, b := range []int{1, 7, 25} {
		for _, counts := range []string{"fenwick", "flat", "sqrt", ""} {
			for _, lay := range []bfi.Layout{bfi.FIFO, bfi.Swap} {
				s = append(s, subject{
					q:        bfi.New(bfi.Config{Domain: dom, Width: b, Layout: lay, Counts: counts}, capHint),
					b:        b,
					fifo:     lay == bfi.FIFO,
					quantize: true,
				})
			}
		}
	}
	s = append(s,
		subject{q: sortedslice.New(capHint), b: 1},
		subject{q: skiplist.New(), b: 1},
		subject{q: skiplist.NewPooled(), b: 1},
		subject{q: ostree.New(capHint), b: 1},
		subject{q: btree.New(capHint), b: 1},
	)
	return s
}

func TestAgainstOracle(t *testing.T) {
	const ops = 6000
	for _, seed := range []int64{1, 2, 3} {
		for _, sub := range subjects(1024) {
			rng := rand.New(rand.NewSource(seed))
			o := &oracle{b: sub.b, p: map[uint64]player{}}
			var ids []uint64
			next := uint64(1)
			for i := 0; i < ops; i++ {
				switch op := rng.Intn(10); {
				case op < 4 || len(ids) == 0: // add (clustered ratings to create ties)
					r := 1500 + int(rng.NormFloat64()*350)
					if rng.Intn(20) == 0 {
						r = rng.Intn(dom.Max)
					}
					r = dom.Clamp(r)
					sub.q.Add(next, r)
					o.seq++
					o.p[next] = player{r, o.seq}
					ids = append(ids, next)
					next++
				case op < 6: // remove
					j := rng.Intn(len(ids))
					id := ids[j]
					ids[j] = ids[len(ids)-1]
					ids = ids[:len(ids)-1]
					got := sub.q.Remove(id)
					want := o.p[id].r
					if sub.quantize {
						want = dom.Min + o.q(want)*sub.b
					}
					if got != want {
						t.Fatalf("%s seed %d op %d: Remove(%d)=%d want %d", sub.q.Name(), seed, i, id, got, want)
					}
					delete(o.p, id)
				case op < 7:
					r := rng.Intn(dom.Max+200) - 100
					if got, want := sub.q.Rank(r), o.rank(r); got != want {
						t.Fatalf("%s seed %d op %d: Rank(%d)=%d want %d", sub.q.Name(), seed, i, r, got, want)
					}
				case op < 8:
					lo := rng.Intn(dom.Max+200) - 100
					hi := lo + rng.Intn(600) - 50
					if got, want := sub.q.RangeCount(lo, hi), o.rangeCount(lo, hi); got != want {
						t.Fatalf("%s seed %d op %d: RangeCount(%d,%d)=%d want %d", sub.q.Name(), seed, i, lo, hi, got, want)
					}
				default:
					r := rng.Intn(dom.Max)
					k := 1 + rng.Intn(12)
					got := sub.q.Nearest(r, k, nil)
					want := o.nearest(r, k)
					if len(got) != len(want) {
						t.Fatalf("%s seed %d op %d: Nearest(%d,%d) len %d want %d", sub.q.Name(), seed, i, r, k, len(got), len(want))
					}
					if sub.fifo {
						for j := range got {
							if got[j] != want[j].id {
								t.Fatalf("%s seed %d op %d: Nearest(%d,%d)[%d]=%d want %d", sub.q.Name(), seed, i, r, k, j, got[j], want[j].id)
							}
						}
						continue
					}
					// Order-free check: same multiset of distances, all IDs present.
					gd := make([]int, len(got))
					for j, id := range got {
						pl, ok := o.p[id]
						if !ok {
							t.Fatalf("%s: Nearest returned absent id %d", sub.q.Name(), id)
						}
						d := o.q(pl.r) - o.q(r)
						if d < 0 {
							d = -d
						}
						gd[j] = d
					}
					sort.Ints(gd)
					for j := range gd {
						if gd[j] != want[j].d {
							t.Fatalf("%s seed %d op %d: Nearest(%d,%d) distances %v want %v", sub.q.Name(), seed, i, r, k, gd, want)
						}
					}
				}
				if sub.q.Len() != len(o.p) {
					t.Fatalf("%s: Len=%d want %d", sub.q.Name(), sub.q.Len(), len(o.p))
				}
			}
		}
	}
}
