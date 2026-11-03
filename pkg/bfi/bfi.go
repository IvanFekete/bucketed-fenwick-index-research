// Package bfi implements the Bucketed Fenwick Index: a count index over a
// quantized rating domain plus per-bucket player lists.
//
// A rating r maps to bucket (r - Min) / b. The count index answers Rank,
// RangeCount and "next non-empty bucket" queries; the per-bucket lists hold
// player IDs for enumeration. Two list layouts are provided:
//
//   - FIFO: an intrusive doubly linked list over a slab of nodes. O(1)
//     removal by handle, and players in a bucket stay in arrival order, so
//     Nearest returns the longest-waiting player first among equals.
//   - Swap: a slice per bucket with swap-and-pop removal. O(1) removal, more
//     compact, but arrival order inside a bucket is lost.
//
// The count index is pluggable (Fenwick, flat array, sqrt decomposition) so
// the contribution of the Fenwick tree can be measured in isolation. With
// Counts == nil the structure degenerates to a plain bucketed pool, which is
// the Open Match-style baseline.
package bfi

import (
	"fmt"

	"github.com/IvanFekete/bucketed-fenwick-index/pkg/countidx"
	"github.com/IvanFekete/bucketed-fenwick-index/pkg/queue"
)

// Layout selects the per-bucket list representation.
type Layout int

const (
	FIFO Layout = iota
	Swap
)

// Config fully describes one BFI variant.
type Config struct {
	Domain queue.Domain
	Width  int    // bucket width b >= 1
	Layout Layout // FIFO or Swap
	Counts string // "fenwick", "flat", "sqrt", or "" (no count index: bucketed pool)
}

const nilSlot = int32(-1)

type node struct {
	id         uint64
	prev, next int32
	bucket     int32
}

type swapPos struct {
	bucket, idx int32
}

// Index is one BFI instance. Not safe for concurrent use.
type Index struct {
	cfg    Config
	nb     int
	counts countidx.Index // may be nil
	sizes  []int32        // per-bucket sizes (needed by the pool variant and for O(1) emptiness checks)
	n      int

	// FIFO layout
	nodes      []node
	free       []int32
	head, tail []int32
	slot       map[uint64]int32

	// Swap layout
	lists [][]uint64
	pos   map[uint64]swapPos

	name string
}

var _ queue.Queue = (*Index)(nil)

// New builds an empty index. capHint pre-sizes the ID map and slab.
func New(cfg Config, capHint int) *Index {
	if cfg.Width < 1 {
		panic("bfi: bucket width must be >= 1")
	}
	nb := (cfg.Domain.Width() + cfg.Width - 1) / cfg.Width
	x := &Index{cfg: cfg, nb: nb, sizes: make([]int32, nb)}
	switch cfg.Counts {
	case "fenwick":
		x.counts = countidx.NewFenwick(nb)
	case "flat":
		x.counts = countidx.NewFlat(nb)
	case "sqrt":
		x.counts = countidx.NewSqrt(nb)
	case "":
	default:
		panic("bfi: unknown count index " + cfg.Counts)
	}
	switch cfg.Layout {
	case FIFO:
		x.nodes = make([]node, 0, capHint)
		x.head = make([]int32, nb)
		x.tail = make([]int32, nb)
		for i := range x.head {
			x.head[i], x.tail[i] = nilSlot, nilSlot
		}
		x.slot = make(map[uint64]int32, capHint)
	case Swap:
		x.lists = make([][]uint64, nb)
		x.pos = make(map[uint64]swapPos, capHint)
	}
	counts := cfg.Counts
	if counts == "" {
		counts = "pool"
	}
	layout := "fifo"
	if cfg.Layout == Swap {
		layout = "swap"
	}
	x.name = fmt.Sprintf("bfi-%s-%s-b%d", counts, layout, cfg.Width)
	return x
}

func (x *Index) Name() string { return x.name }
func (x *Index) Len() int     { return x.n }

// Buckets returns the number of buckets B = ceil(domain / b).
func (x *Index) Buckets() int { return x.nb }

// BucketOf maps a rating to its bucket index (ratings are clamped).
func (x *Index) BucketOf(r int) int {
	return (x.cfg.Domain.Clamp(r) - x.cfg.Domain.Min) / x.cfg.Width
}

func (x *Index) Add(id uint64, r int) {
	b := x.BucketOf(r)
	if x.cfg.Layout == FIFO {
		var s int32
		if k := len(x.free); k > 0 {
			s = x.free[k-1]
			x.free = x.free[:k-1]
		} else {
			x.nodes = append(x.nodes, node{})
			s = int32(len(x.nodes) - 1)
		}
		nd := &x.nodes[s]
		nd.id, nd.bucket, nd.next = id, int32(b), nilSlot
		nd.prev = x.tail[b]
		if x.tail[b] != nilSlot {
			x.nodes[x.tail[b]].next = s
		} else {
			x.head[b] = s
		}
		x.tail[b] = s
		x.slot[id] = s
	} else {
		x.pos[id] = swapPos{bucket: int32(b), idx: int32(len(x.lists[b]))}
		x.lists[b] = append(x.lists[b], id)
	}
	x.sizes[b]++
	if x.counts != nil {
		x.counts.Inc(b, 1)
	}
	x.n++
}

// Remove deletes the player and returns the lower edge of its bucket. For
// b = 1 that is exactly its rating.
func (x *Index) Remove(id uint64) int {
	var b int
	if x.cfg.Layout == FIFO {
		s := x.slot[id]
		delete(x.slot, id)
		nd := x.nodes[s]
		b = int(nd.bucket)
		if nd.prev != nilSlot {
			x.nodes[nd.prev].next = nd.next
		} else {
			x.head[b] = nd.next
		}
		if nd.next != nilSlot {
			x.nodes[nd.next].prev = nd.prev
		} else {
			x.tail[b] = nd.prev
		}
		x.free = append(x.free, s)
	} else {
		p := x.pos[id]
		delete(x.pos, id)
		b = int(p.bucket)
		l := x.lists[b]
		last := len(l) - 1
		if int(p.idx) != last {
			moved := l[last]
			l[p.idx] = moved
			x.pos[moved] = swapPos{bucket: p.bucket, idx: p.idx}
		}
		x.lists[b] = l[:last]
	}
	x.sizes[b]--
	if x.counts != nil {
		x.counts.Inc(b, -1)
	}
	x.n--
	return x.cfg.Domain.Min + b*x.cfg.Width
}

// prefix returns the number of players in buckets [0, i).
func (x *Index) prefix(i int) int {
	if i <= 0 {
		return 0
	}
	if i >= x.nb {
		return x.n
	}
	if x.counts != nil {
		return x.counts.Prefix(i)
	}
	s := 0
	for _, v := range x.sizes[:i] {
		s += int(v)
	}
	return s
}

// Rank counts players whose bucket is strictly below bucket(r).
func (x *Index) Rank(r int) int {
	if r <= x.cfg.Domain.Min {
		return 0
	}
	if r >= x.cfg.Domain.Max {
		return x.n
	}
	return x.prefix(x.BucketOf(r))
}

// RangeCount counts players in all buckets that intersect [lo, hi].
func (x *Index) RangeCount(lo, hi int) int {
	if lo > hi || hi < x.cfg.Domain.Min || lo >= x.cfg.Domain.Max {
		return 0
	}
	return x.prefix(x.BucketOf(hi)+1) - x.prefix(x.BucketOf(lo))
}

// nextNonEmptyUp returns the first non-empty bucket >= i, or -1.
func (x *Index) nextNonEmptyUp(i int) int {
	if i >= x.nb {
		return -1
	}
	if x.counts != nil {
		if i < 0 {
			i = 0
		}
		if x.sizes[i] > 0 {
			return i
		}
		p := x.counts.Prefix(i)
		if p >= x.n {
			return -1
		}
		return x.counts.Select(p)
	}
	for ; i < x.nb; i++ {
		if x.sizes[i] > 0 {
			return i
		}
	}
	return -1
}

// nextNonEmptyDown returns the last non-empty bucket <= i, or -1.
func (x *Index) nextNonEmptyDown(i int) int {
	if i < 0 {
		return -1
	}
	if i >= x.nb {
		i = x.nb - 1
	}
	if x.counts != nil {
		if x.sizes[i] > 0 {
			return i
		}
		p := x.counts.Prefix(i + 1)
		if p == 0 {
			return -1
		}
		return x.counts.Select(p - 1)
	}
	for ; i >= 0; i-- {
		if x.sizes[i] > 0 {
			return i
		}
	}
	return -1
}

func (x *Index) appendBucket(b int, out []uint64, want int) []uint64 {
	if x.cfg.Layout == FIFO {
		for s := x.head[b]; s != nilSlot && want > 0; s = x.nodes[s].next {
			out = append(out, x.nodes[s].id)
			want--
		}
		return out
	}
	l := x.lists[b]
	if len(l) > want {
		l = l[:want]
	}
	return append(out, l...)
}

// Nearest returns up to k players in order of increasing bucket distance from
// bucket(r). Ties between the bucket below and the bucket above go to the
// lower bucket; inside a bucket the FIFO layout returns arrival order.
func (x *Index) Nearest(r, k int, out []uint64) []uint64 {
	if k <= 0 || x.n == 0 {
		return out
	}
	c := x.BucketOf(r)
	down := x.nextNonEmptyDown(c)
	up := x.nextNonEmptyUp(c + 1)
	for k > 0 && (down >= 0 || up >= 0) {
		var b int
		if up < 0 || (down >= 0 && c-down <= up-c) {
			b = down
			down = x.nextNonEmptyDown(down - 1)
		} else {
			b = up
			up = x.nextNonEmptyUp(up + 1)
		}
		before := len(out)
		out = x.appendBucket(b, out, k)
		k -= len(out) - before
	}
	return out
}

// Bytes is a structural estimate (arrays only; Go map overhead is measured
// separately from heap statistics).
func (x *Index) Bytes() int {
	total := 4 * len(x.sizes)
	if x.counts != nil {
		total += x.counts.Bytes()
	}
	if x.cfg.Layout == FIFO {
		total += 24*cap(x.nodes) + 4*cap(x.free) + 8*x.nb
	} else {
		total += 24 * x.nb
		for _, l := range x.lists {
			total += 8 * cap(l)
		}
	}
	return total
}

// NearestEdges is Nearest that also reports, for every returned ID, the lower
// rating edge of its bucket (the exact rating when b = 1). Used by the striped
// concurrent variant to merge candidates from several stripes without a
// second lookup.
func (x *Index) NearestEdges(r, k int, out []uint64, edges []int32) ([]uint64, []int32) {
	if k <= 0 || x.n == 0 {
		return out, edges
	}
	c := x.BucketOf(r)
	down := x.nextNonEmptyDown(c)
	up := x.nextNonEmptyUp(c + 1)
	for k > 0 && (down >= 0 || up >= 0) {
		var b int
		if up < 0 || (down >= 0 && c-down <= up-c) {
			b = down
			down = x.nextNonEmptyDown(down - 1)
		} else {
			b = up
			up = x.nextNonEmptyUp(up + 1)
		}
		before := len(out)
		out = x.appendBucket(b, out, k)
		edge := int32(x.cfg.Domain.Min + b*x.cfg.Width)
		for i := before; i < len(out); i++ {
			edges = append(edges, edge)
		}
		k -= len(out) - before
	}
	return out, edges
}
