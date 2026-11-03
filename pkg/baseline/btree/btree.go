// Package btree is an order-statistic B+tree keyed by (rating, id): every
// internal node stores the number of players under each child, so Rank is a
// single root-to-leaf walk that sums counts of the children to the left.
// Leaves are linked in both directions for Nearest.
//
// Deletion is relaxed: underfull nodes are not merged, empty nodes are
// unlinked and the root collapses when it has one child. This keeps the code
// short; with insert/delete churn at a constant population the tree depth
// stays logarithmic (checked by the property test via Depth).
package btree

import (
	"sort"

	"github.com/IvanFekete/bucketed-fenwick-index/pkg/queue"
)

const fanout = 32

type key struct {
	r  int32
	id uint64
}

func less(a, b key) bool { return a.r < b.r || (a.r == b.r && a.id < b.id) }

type node struct {
	leaf       bool
	keys       []key   // leaf: entries; internal: max key of each child
	kids       []*node // internal only
	cnt        []int32 // internal only: players under each child
	parent     *node
	prev, next *node // leaf links
}

type Tree struct {
	root   *node
	n      int
	rating map[uint64]int32
}

var _ queue.Queue = (*Tree)(nil)

func New(capHint int) *Tree {
	return &Tree{root: &node{leaf: true}, rating: make(map[uint64]int32, capHint)}
}

func (t *Tree) Name() string { return "btree" }
func (t *Tree) Len() int     { return t.n }

func (t *Tree) Bytes() int {
	var walk func(x *node) int
	walk = func(x *node) int {
		b := 64 + 16*cap(x.keys)
		if !x.leaf {
			b += 12 * cap(x.kids)
			for _, k := range x.kids {
				b += walk(k)
			}
		}
		return b
	}
	return walk(t.root)
}

// Depth is used by tests.
func (t *Tree) Depth() int {
	d := 1
	for x := t.root; !x.leaf; x = x.kids[0] {
		d++
	}
	return d
}

// childFor returns the index of the child whose key range should contain k.
func childFor(x *node, k key) int {
	i := sort.Search(len(x.keys), func(i int) bool { return !less(x.keys[i], k) })
	if i == len(x.keys) {
		i = len(x.keys) - 1
	}
	return i
}

func (t *Tree) findLeaf(k key) (*node, []int) {
	x := t.root
	var path []int
	for !x.leaf {
		i := childFor(x, k)
		path = append(path, i)
		x = x.kids[i]
	}
	return x, path
}

func (t *Tree) Add(id uint64, r int) {
	t.rating[id] = int32(r)
	k := key{int32(r), id}
	leaf, _ := t.findLeaf(k)
	i := sort.Search(len(leaf.keys), func(i int) bool { return less(k, leaf.keys[i]) })
	leaf.keys = append(leaf.keys, key{})
	copy(leaf.keys[i+1:], leaf.keys[i:])
	leaf.keys[i] = k
	t.n++
	// Update counts and max keys up the path.
	for c, p := leaf, leaf.parent; p != nil; c, p = p, p.parent {
		j := indexOf(p, c)
		p.cnt[j]++
		p.keys[j] = c.keys[len(c.keys)-1]
	}
	if len(leaf.keys) > fanout {
		t.split(leaf)
	}
}

func indexOf(p, c *node) int {
	for j, k := range p.kids {
		if k == c {
			return j
		}
	}
	panic("btree: child not found")
}

func count(x *node) int32 {
	if x.leaf {
		return int32(len(x.keys))
	}
	s := int32(0)
	for _, c := range x.cnt {
		s += c
	}
	return s
}

func (t *Tree) split(x *node) {
	mid := len(x.keys) / 2
	y := &node{leaf: x.leaf, parent: x.parent}
	y.keys = append([]key(nil), x.keys[mid:]...)
	x.keys = x.keys[:mid:mid]
	if x.leaf {
		y.next, y.prev = x.next, x
		if x.next != nil {
			x.next.prev = y
		}
		x.next = y
	} else {
		y.kids = append([]*node(nil), x.kids[mid:]...)
		y.cnt = append([]int32(nil), x.cnt[mid:]...)
		x.kids = x.kids[:mid:mid]
		x.cnt = x.cnt[:mid:mid]
		for _, c := range y.kids {
			c.parent = y
		}
	}
	p := x.parent
	if p == nil {
		p = &node{}
		p.keys = []key{x.keys[len(x.keys)-1], y.keys[len(y.keys)-1]}
		p.kids = []*node{x, y}
		p.cnt = []int32{count(x), count(y)}
		x.parent, y.parent = p, p
		t.root = p
		return
	}
	j := indexOf(p, x)
	p.keys = append(p.keys, key{})
	p.kids = append(p.kids, nil)
	p.cnt = append(p.cnt, 0)
	copy(p.keys[j+2:], p.keys[j+1:])
	copy(p.kids[j+2:], p.kids[j+1:])
	copy(p.cnt[j+2:], p.cnt[j+1:])
	p.keys[j], p.keys[j+1] = x.keys[len(x.keys)-1], y.keys[len(y.keys)-1]
	p.kids[j+1] = y
	p.cnt[j], p.cnt[j+1] = count(x), count(y)
	if len(p.kids) > fanout {
		t.split(p)
	}
}

func (t *Tree) Remove(id uint64) int {
	r := t.rating[id]
	delete(t.rating, id)
	k := key{r, id}
	leaf, _ := t.findLeaf(k)
	i := sort.Search(len(leaf.keys), func(i int) bool { return !less(leaf.keys[i], k) })
	copy(leaf.keys[i:], leaf.keys[i+1:])
	leaf.keys = leaf.keys[:len(leaf.keys)-1]
	t.n--
	// Walk up: fix counts and max keys, unlink empty nodes.
	c := leaf
	for p := c.parent; p != nil; c, p = p, p.parent {
		j := indexOf(p, c)
		p.cnt[j]--
		if (c.leaf && len(c.keys) == 0) || (!c.leaf && len(c.kids) == 0) {
			if c.leaf {
				if c.prev != nil {
					c.prev.next = c.next
				}
				if c.next != nil {
					c.next.prev = c.prev
				}
			}
			p.keys = append(p.keys[:j], p.keys[j+1:]...)
			p.kids = append(p.kids[:j], p.kids[j+1:]...)
			p.cnt = append(p.cnt[:j], p.cnt[j+1:]...)
		} else {
			p.keys[j] = c.keys[len(c.keys)-1]
		}
	}
	for !t.root.leaf && len(t.root.kids) == 1 {
		t.root = t.root.kids[0]
		t.root.parent = nil
	}
	if !t.root.leaf && len(t.root.kids) == 0 {
		t.root = &node{leaf: true}
	}
	return int(r)
}

// Rank counts players with rating < r.
func (t *Tree) Rank(r int) int {
	k := key{int32(r), 0} // smallest key with rating r
	x := t.root
	total := 0
	for !x.leaf {
		i := sort.Search(len(x.keys), func(i int) bool { return !less(x.keys[i], k) })
		for j := 0; j < i; j++ {
			total += int(x.cnt[j])
		}
		if i == len(x.keys) {
			return total
		}
		x = x.kids[i]
	}
	return total + sort.Search(len(x.keys), func(i int) bool { return !less(x.keys[i], k) })
}

func (t *Tree) RangeCount(lo, hi int) int {
	if lo > hi {
		return 0
	}
	return t.Rank(hi+1) - t.Rank(lo)
}

// Nearest locates the first entry with rating >= r and walks leaves outward.
func (t *Tree) Nearest(r, k int, out []uint64) []uint64 {
	if t.n == 0 || k <= 0 {
		return out
	}
	key0 := key{int32(r), 0}
	leaf, _ := t.findLeaf(key0)
	i := sort.Search(len(leaf.keys), func(i int) bool { return !less(leaf.keys[i], key0) })
	upL, upI := leaf, i
	if upI == len(upL.keys) {
		upL, upI = upL.next, 0
	}
	dnL, dnI := leaf, i-1
	if dnI < 0 {
		dnL = leaf.prev
		if dnL != nil {
			dnI = len(dnL.keys) - 1
		}
	}
	for k > 0 && (upL != nil || dnL != nil) {
		if upL == nil || (dnL != nil && r-int(dnL.keys[dnI].r) <= int(upL.keys[upI].r)-r) {
			out = append(out, dnL.keys[dnI].id)
			dnI--
			if dnI < 0 {
				dnL = dnL.prev
				if dnL != nil {
					dnI = len(dnL.keys) - 1
				}
			}
		} else {
			out = append(out, upL.keys[upI].id)
			upI++
			if upI == len(upL.keys) {
				upL, upI = upL.next, 0
			}
		}
		k--
	}
	return out
}
