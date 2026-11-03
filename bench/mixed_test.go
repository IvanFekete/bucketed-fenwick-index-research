package bench

// Mixed read/write workloads for the write-ratio crossover (Section 5.4).
// A write is one churn step (remove a random waiting player, add a new one);
// a read is the matchmaker read mix: 1/2 Nearest(8), 1/4 RangeCount, 1/4 Rank.
// The op sequence is pre-generated so every structure sees the same stream.

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/IvanFekete/bucketed-fenwick-index/pkg/baseline/btree"
	"github.com/IvanFekete/bucketed-fenwick-index/pkg/baseline/ostree"
	"github.com/IvanFekete/bucketed-fenwick-index/pkg/baseline/skiplist"
	"github.com/IvanFekete/bucketed-fenwick-index/pkg/bfi"
	"github.com/IvanFekete/bucketed-fenwick-index/pkg/queue"
)

type op struct {
	kind uint8 // 0 churn, 1 nearest, 2 range, 3 rank
	a, b int32
}

func mixedMakers() []maker {
	m := []maker{
		{"skiplist", func(n int) queue.Queue { return skiplist.NewSized(n) }},
		{"ostree", func(n int) queue.Queue { return ostree.New(n) }},
		{"btree", func(n int) queue.Queue { return btree.New(n) }},
	}
	for _, c := range []string{"fenwick", "sqrt", "flat"} {
		c := c
		m = append(m, maker{"bfi-" + c + "-fifo-b1", func(n int) queue.Queue {
			return bfi.New(bfi.Config{Domain: dom, Width: 1, Layout: bfi.FIFO, Counts: c}, n)
		}})
	}
	return m
}

var writePercents = []int{1, 5, 10, 25, 50, 75, 90}

func genOps(n int, writePct int, rng *rand.Rand) []op {
	rs := ratings()
	ops := make([]op, 1<<16)
	for i := range ops {
		if rng.Intn(100) < writePct {
			ops[i] = op{kind: 0, a: int32(rng.Intn(n)), b: int32(rs[rng.Intn(len(rs))])}
			continue
		}
		c := int32(rs[rng.Intn(len(rs))])
		switch x := rng.Intn(4); {
		case x < 2:
			ops[i] = op{kind: 1, a: c}
		case x == 2:
			d := int32(25 + rng.Intn(200))
			ops[i] = op{kind: 2, a: c - d, b: c + d}
		default:
			ops[i] = op{kind: 3, a: c}
		}
	}
	return ops
}

func BenchmarkMixed(b *testing.B) {
	for _, n := range []int{100_000, 1_000_000} {
		for _, w := range writePercents {
			for _, m := range mixedMakers() {
				b.Run(fmt.Sprintf("n=%d/w=%d/%s", n, w, m.name), func(b *testing.B) {
					rng := rand.New(rand.NewSource(int64(w)))
					q, ids := built(m, n)
					ops := genOps(n, w, rng)
					out := make([]uint64, 0, 8)
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						o := ops[i&(len(ops)-1)]
						switch o.kind {
						case 0:
							q.Remove(ids[o.a])
							ids[o.a] = freshID()
							q.Add(ids[o.a], int(o.b))
						case 1:
							out = q.Nearest(int(o.a), 8, out[:0])
						case 2:
							sink += q.RangeCount(int(o.a), int(o.b))
						default:
							sink += q.Rank(int(o.a))
						}
					}
					sink += len(out)
				})
			}
		}
	}
}
