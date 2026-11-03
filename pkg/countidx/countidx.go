// Package countidx provides interchangeable count indexes over n buckets:
// a Fenwick tree (the "F" in BFI), a flat count array with linear prefix
// scan, and a two-level sqrt decomposition. They let the study isolate the
// contribution of the Fenwick tree itself (ablation).
package countidx

import "math/bits"

// Index maintains non-negative integer counts c[0..n-1].
type Index interface {
	// Inc adds delta to c[i].
	Inc(i int, delta int32)
	// Prefix returns c[0] + ... + c[i-1]; Prefix(0) = 0, Prefix(n) = total.
	Prefix(i int) int
	// Select returns the smallest i such that Prefix(i+1) > k, i.e. the bucket
	// holding the element of 0-based rank k. Requires 0 <= k < total.
	Select(k int) int
	Bytes() int
	Kind() string
}

// ---------------------------------------------------------------- Fenwick

// Fenwick is a 1-indexed binary indexed tree [Fenwick 1994].
type Fenwick struct {
	t    []int32 // t[1..n]
	n    int
	high int // largest power of two <= n, for Select
}

func NewFenwick(n int) *Fenwick {
	h := 1
	if n > 0 {
		h = 1 << (bits.Len(uint(n)) - 1)
	}
	return &Fenwick{t: make([]int32, n+1), n: n, high: h}
}

func (f *Fenwick) Inc(i int, d int32) {
	for i++; i <= f.n; i += i & -i {
		f.t[i] += d
	}
}

func (f *Fenwick) Prefix(i int) int {
	if i > f.n {
		i = f.n
	}
	s := int32(0)
	for ; i > 0; i -= i & -i {
		s += f.t[i]
	}
	return int(s)
}

// Select uses binary lifting: O(log n), one pass down the implicit tree.
func (f *Fenwick) Select(k int) int {
	pos := 0
	rem := int32(k)
	for step := f.high; step > 0; step >>= 1 {
		nxt := pos + step
		if nxt <= f.n && f.t[nxt] <= rem {
			pos = nxt
			rem -= f.t[nxt]
		}
	}
	return pos // 0-based bucket index
}

func (f *Fenwick) Bytes() int   { return 4 * len(f.t) }
func (f *Fenwick) Kind() string { return "fenwick" }

// ---------------------------------------------------------------- Flat

// Flat keeps raw counts; Prefix and Select are linear scans. Updates are O(1).
type Flat struct{ c []int32 }

func NewFlat(n int) *Flat { return &Flat{c: make([]int32, n)} }

func (f *Flat) Inc(i int, d int32) { f.c[i] += d }

func (f *Flat) Prefix(i int) int {
	if i > len(f.c) {
		i = len(f.c)
	}
	s := int32(0)
	for _, v := range f.c[:i] {
		s += v
	}
	return int(s)
}

func (f *Flat) Select(k int) int {
	rem := int32(k)
	for i, v := range f.c {
		if v > rem {
			return i
		}
		rem -= v
	}
	return len(f.c) - 1
}

func (f *Flat) Bytes() int   { return 4 * len(f.c) }
func (f *Flat) Kind() string { return "flat" }

// ---------------------------------------------------------------- Sqrt

// Sqrt is a two-level decomposition: raw counts plus per-block sums.
// Update O(1), Prefix/Select O(sqrt n).
type Sqrt struct {
	c     []int32
	block []int32
	bs    int
}

func NewSqrt(n int) *Sqrt {
	bs := 1
	for bs*bs < n {
		bs++
	}
	return &Sqrt{c: make([]int32, n), block: make([]int32, (n+bs-1)/bs+1), bs: bs}
}

func (s *Sqrt) Inc(i int, d int32) {
	s.c[i] += d
	s.block[i/s.bs] += d
}

func (s *Sqrt) Prefix(i int) int {
	if i > len(s.c) {
		i = len(s.c)
	}
	b := i / s.bs
	sum := int32(0)
	for _, v := range s.block[:b] {
		sum += v
	}
	for _, v := range s.c[b*s.bs : i] {
		sum += v
	}
	return int(sum)
}

func (s *Sqrt) Select(k int) int {
	rem := int32(k)
	b := 0
	for ; b < len(s.block); b++ {
		if s.block[b] > rem {
			break
		}
		rem -= s.block[b]
	}
	for i := b * s.bs; i < len(s.c); i++ {
		if s.c[i] > rem {
			return i
		}
		rem -= s.c[i]
	}
	return len(s.c) - 1
}

func (s *Sqrt) Bytes() int   { return 4 * (len(s.c) + len(s.block)) }
func (s *Sqrt) Kind() string { return "sqrt" }
