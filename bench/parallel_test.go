package bench

// Multi-threaded throughput (Section 5.4). Run with
//
//	-bench=Parallel -cpu=1,2,4,8,16
//
// testing.B.RunParallel starts GOMAXPROCS goroutines; ns/op is wall time per
// operation across all goroutines, so throughput = 1e9 / (ns/op).
// Each goroutine churns only its own slice of player IDs, so writes never
// race on the same ID and no coordination outside the structure is needed.

import (
	"fmt"
	"math/rand"
	"sync/atomic"
	"testing"

	"github.com/IvanFekete/bucketed-fenwick-index/pkg/baseline/btree"
	"github.com/IvanFekete/bucketed-fenwick-index/pkg/baseline/skiplist"
	"github.com/IvanFekete/bucketed-fenwick-index/pkg/bfi"
	"github.com/IvanFekete/bucketed-fenwick-index/pkg/concurrent"
	"github.com/IvanFekete/bucketed-fenwick-index/pkg/queue"
)

func parallelMakers() []maker {
	return []maker{
		{"locked-skiplist", func(n int) queue.Queue { return concurrent.NewLocked(skiplist.NewSized(n)) }},
		{"locked-btree", func(n int) queue.Queue { return concurrent.NewLocked(btree.New(n)) }},
		{"locked-bfi", func(n int) queue.Queue {
			return concurrent.NewLocked(bfi.New(bfi.Config{Domain: dom, Width: 1, Layout: bfi.FIFO, Counts: "fenwick"}, n))
		}},
		{"striped-bfi-s16", func(n int) queue.Queue { return concurrent.NewStriped(dom, 1, 16, n) }},
		{"striped-bfi-s64", func(n int) queue.Queue { return concurrent.NewStriped(dom, 1, 64, n) }},
	}
}

func BenchmarkParallel(b *testing.B) {
	const n = 1_000_000
	const slots = 64 // per-goroutine ID partitions; supports up to 64 goroutines
	for _, w := range []int{1, 10, 50} {
		for _, m := range parallelMakers() {
			b.Run(fmt.Sprintf("n=%d/w=%d/%s", n, w, m.name), func(b *testing.B) {
				q, ids := built(m, n)
				per := n / slots
				var nextSlot atomic.Int64
				rs := ratings()
				b.ResetTimer()
				b.RunParallel(func(pb *testing.PB) {
					s := int(nextSlot.Add(1) - 1)
					mine := ids[s*per : (s+1)*per]
					r := rand.New(rand.NewSource(int64(1000 + s)))
					out := make([]uint64, 0, 8)
					local := 0
					for pb.Next() {
						if r.Intn(100) < w {
							j := r.Intn(len(mine))
							q.Remove(mine[j])
							id := freshID()
							mine[j] = id
							q.Add(id, rs[r.Intn(len(rs))])
							continue
						}
						c := rs[r.Intn(len(rs))]
						switch x := r.Intn(4); {
						case x < 2:
							out = q.Nearest(c, 8, out[:0])
						case x == 2:
							local += q.RangeCount(c-100, c+100)
						default:
							local += q.Rank(c)
						}
					}
					_ = local
				})
			})
		}
	}
}
