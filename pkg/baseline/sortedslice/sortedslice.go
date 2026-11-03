// Package sortedslice is the simplest exact baseline: a slice of (rating, id)
// kept sorted. Queries are binary searches; updates shift memory, O(n).
package sortedslice

import (
	"sort"

	"github.com/IvanFekete/bucketed-fenwick-index/pkg/queue"
)

type entry struct {
	r  int32
	id uint64
}

type Slice struct {
	a      []entry
	rating map[uint64]int32
}

var _ queue.Queue = (*Slice)(nil)

func New(capHint int) *Slice {
	return &Slice{a: make([]entry, 0, capHint), rating: make(map[uint64]int32, capHint)}
}

func (s *Slice) Name() string { return "sortedslice" }
func (s *Slice) Len() int     { return len(s.a) }
func (s *Slice) Bytes() int   { return 16 * cap(s.a) }

func less(e entry, r int32, id uint64) bool {
	return e.r < r || (e.r == r && e.id < id)
}

func (s *Slice) find(r int32, id uint64) int {
	return sort.Search(len(s.a), func(i int) bool { return !less(s.a[i], r, id) })
}

func (s *Slice) Add(id uint64, r int) {
	s.rating[id] = int32(r)
	i := s.find(int32(r), id)
	s.a = append(s.a, entry{})
	copy(s.a[i+1:], s.a[i:])
	s.a[i] = entry{int32(r), id}
}

func (s *Slice) Remove(id uint64) int {
	r := s.rating[id]
	delete(s.rating, id)
	i := s.find(r, id)
	copy(s.a[i:], s.a[i+1:])
	s.a = s.a[:len(s.a)-1]
	return int(r)
}

func (s *Slice) Rank(r int) int {
	return sort.Search(len(s.a), func(i int) bool { return int(s.a[i].r) >= r })
}

func (s *Slice) RangeCount(lo, hi int) int {
	if lo > hi {
		return 0
	}
	return s.Rank(hi+1) - s.Rank(lo)
}

func (s *Slice) Nearest(r, k int, out []uint64) []uint64 {
	up := s.Rank(r)
	down := up - 1
	for k > 0 && (down >= 0 || up < len(s.a)) {
		if up >= len(s.a) || (down >= 0 && r-int(s.a[down].r) <= int(s.a[up].r)-r) {
			out = append(out, s.a[down].id)
			down--
		} else {
			out = append(out, s.a[up].id)
			up++
		}
		k--
	}
	return out
}
