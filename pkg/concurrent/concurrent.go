// Package concurrent provides the thread-safe variants compared in the
// concurrency experiment (Section 5.4):
//
//   - Locked wraps any queue.Queue in a single sync.RWMutex: reads (Rank,
//     RangeCount, Nearest) share the lock, writes are exclusive. This is the
//     straightforward way to share a comparison-based ordered set in Go, which
//     has no standard concurrent ordered set with rank support.
//   - Striped partitions the bucket range of a BFI into S contiguous stripes,
//     each a BFI with its own RWMutex, plus an atomic player count per stripe.
//     Rank(r) adds the atomic counts of the stripes fully below r to a locked
//     rank inside r's stripe. Writes to different stripes do not contend.
//
// Consistency: every single operation is linearizable within one stripe.
// Rank and RangeCount across stripes read the atomic counters of other
// stripes without locking them, so under concurrent writes they return a
// value that was correct at some instant during the call for each stripe,
// not necessarily the same instant for all stripes. For a matchmaking queue,
// whose counts are advisory (they size search windows, they do not admit
// players), this is acceptable and is stated in the paper.
package concurrent

import (
	"sync"
	"sync/atomic"

	"github.com/IvanFekete/bucketed-fenwick-index/pkg/bfi"
	"github.com/IvanFekete/bucketed-fenwick-index/pkg/queue"
)

// Locked serialises writers and lets readers run in parallel.
type Locked struct {
	mu sync.RWMutex
	q  queue.Queue
}

var _ queue.Queue = (*Locked)(nil)

func NewLocked(q queue.Queue) *Locked { return &Locked{q: q} }

func (l *Locked) Name() string { return "locked-" + l.q.Name() }

func (l *Locked) Add(id uint64, r int) {
	l.mu.Lock()
	l.q.Add(id, r)
	l.mu.Unlock()
}

func (l *Locked) Remove(id uint64) int {
	l.mu.Lock()
	r := l.q.Remove(id)
	l.mu.Unlock()
	return r
}

func (l *Locked) Rank(r int) int {
	l.mu.RLock()
	v := l.q.Rank(r)
	l.mu.RUnlock()
	return v
}

func (l *Locked) RangeCount(lo, hi int) int {
	l.mu.RLock()
	v := l.q.RangeCount(lo, hi)
	l.mu.RUnlock()
	return v
}

func (l *Locked) Nearest(r, k int, out []uint64) []uint64 {
	l.mu.RLock()
	out = l.q.Nearest(r, k, out)
	l.mu.RUnlock()
	return out
}

func (l *Locked) Len() int {
	l.mu.RLock()
	v := l.q.Len()
	l.mu.RUnlock()
	return v
}

// ---------------------------------------------------------------- Striped

type stripe struct {
	mu  sync.RWMutex
	idx *bfi.Index
	n   atomic.Int64
	_   [40]byte // keep stripes on separate cache lines
}

type ownerShard struct {
	mu sync.Mutex
	m  map[uint64]int32
	_  [40]byte
}

func (s *Striped) shardOf(id uint64) *ownerShard { return &s.owner[(id*0x9E3779B97F4A7C15)>>56] }

// Striped is a BFI split into contiguous rating ranges.
type Striped struct {
	dom     queue.Domain
	width   int // ratings per stripe
	stripes []*stripe
	owner   [256]ownerShard // id -> stripe, lock-striped by id
	name    string
}

var _ queue.Queue = (*Striped)(nil)

// NewStriped builds S stripes over the domain; each stripe is a BFI with the
// given bucket width, FIFO layout and Fenwick counts.
func NewStriped(dom queue.Domain, bucketWidth, s, capHint int) *Striped {
	w := (dom.Width() + s - 1) / s
	// Stripe boundaries on bucket boundaries.
	w = ((w + bucketWidth - 1) / bucketWidth) * bucketWidth
	st := &Striped{dom: dom, width: w}
	for lo := dom.Min; lo < dom.Max; lo += w {
		hi := lo + w
		if hi > dom.Max {
			hi = dom.Max
		}
		st.stripes = append(st.stripes, &stripe{idx: bfi.New(bfi.Config{
			Domain: queue.Domain{Min: lo, Max: hi}, Width: bucketWidth, Layout: bfi.FIFO, Counts: "fenwick",
		}, capHint/s+1)})
	}
	for i := range st.owner {
		st.owner[i].m = make(map[uint64]int32, capHint/256+1)
	}
	st.name = "striped-bfi"
	return st
}

func (s *Striped) Name() string { return s.name }

func (s *Striped) stripeOf(r int) int {
	return (s.dom.Clamp(r) - s.dom.Min) / s.width
}

func (s *Striped) Add(id uint64, r int) {
	i := s.stripeOf(r)
	st := s.stripes[i]
	st.mu.Lock()
	st.idx.Add(id, r)
	st.mu.Unlock()
	st.n.Add(1)
	o := s.shardOf(id)
	o.mu.Lock()
	o.m[id] = int32(i)
	o.mu.Unlock()
}

func (s *Striped) Remove(id uint64) int {
	o := s.shardOf(id)
	o.mu.Lock()
	i := o.m[id]
	delete(o.m, id)
	o.mu.Unlock()
	st := s.stripes[i]
	st.mu.Lock()
	r := st.idx.Remove(id)
	st.mu.Unlock()
	st.n.Add(-1)
	return r
}

func (s *Striped) Rank(r int) int {
	if r <= s.dom.Min {
		return 0
	}
	if r >= s.dom.Max {
		return s.Len()
	}
	i := s.stripeOf(r)
	total := 0
	for _, st := range s.stripes[:i] {
		total += int(st.n.Load())
	}
	st := s.stripes[i]
	st.mu.RLock()
	total += st.idx.Rank(r)
	st.mu.RUnlock()
	return total
}

func (s *Striped) RangeCount(lo, hi int) int {
	if lo > hi {
		return 0
	}
	return s.Rank(hi+1) - s.Rank(lo)
}

// Nearest gathers up to k candidates from r's stripe, then visits
// neighbouring stripes in order of the distance from r to their boundary and
// stops as soon as that distance exceeds the k-th best candidate found so far.
// Candidates are merged by rating distance (lower side first on ties), so for
// b = 1 the result has the same distances as an exact structure. Stripes are
// locked one at a time.
func (s *Striped) Nearest(r, k int, out []uint64) []uint64 {
	if k <= 0 {
		return out
	}
	type cand struct {
		id uint64
		d  int
		up bool
	}
	var buf [64]uint64
	var ebuf [64]int32
	best := make([]cand, 0, 2*k)
	add := func(j int) {
		st := s.stripes[j]
		st.mu.RLock()
		ids, edges := st.idx.NearestEdges(r, k, buf[:0], ebuf[:0])
		st.mu.RUnlock()
		for t, id := range ids {
			d := int(edges[t]) - r
			up := d > 0
			if d < 0 {
				d = -d
			}
			best = append(best, cand{id, d, up})
		}
		// insertion sort: lists are short (<= 2k)
		for a := 1; a < len(best); a++ {
			for b := a; b > 0; b-- {
				x, y := best[b-1], best[b]
				if y.d < x.d || (y.d == x.d && !y.up && x.up) {
					best[b-1], best[b] = y, x
				} else {
					break
				}
			}
		}
		if len(best) > k {
			best = best[:k]
		}
	}
	i := s.stripeOf(r)
	add(i)
	lo, hi := i-1, i+1
	for lo >= 0 || hi < len(s.stripes) {
		dl, dh := 1<<62, 1<<62
		if lo >= 0 {
			dl = r - (s.dom.Min + (lo+1)*s.width - 1)
		}
		if hi < len(s.stripes) {
			dh = (s.dom.Min + hi*s.width) - r
		}
		next := dl
		if dh < dl {
			next = dh
		}
		if len(best) == k && best[k-1].d < next {
			break
		}
		if dl <= dh {
			add(lo)
			lo--
		} else {
			add(hi)
			hi++
		}
	}
	for _, c := range best {
		out = append(out, c.id)
	}
	return out
}

func (s *Striped) Len() int {
	t := 0
	for _, st := range s.stripes {
		t += int(st.n.Load())
	}
	return t
}
