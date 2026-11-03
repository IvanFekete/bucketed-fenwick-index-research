// Package ostree is an order-statistic tree keyed by (rating, id) with subtree
// sizes. It is a treap (randomised balanced BST) with array-backed nodes, so
// expected depth is O(log n) and Rank/Select are single root-to-leaf walks.
//
// The paper calls this the "order-statistic tree" baseline: the textbook
// comparison-based structure for dynamic rank queries over n players.
package ostree

import (
	"math/rand"

	"github.com/IvanFekete/bucketed-fenwick-index/pkg/queue"
)

const nilN = int32(0) // slot 0 is the sentinel

type node struct {
	r           int32
	prio        uint32
	id          uint64
	left, right int32
	size        int32
}

type Tree struct {
	n      []node // n[0] is a sentinel with size 0
	free   []int32
	root   int32
	rng    *rand.Rand
	rating map[uint64]int32
}

var _ queue.Queue = (*Tree)(nil)

func New(capHint int) *Tree {
	t := &Tree{n: make([]node, 1, capHint+1), rng: rand.New(rand.NewSource(1)), rating: make(map[uint64]int32, capHint)}
	return t
}

func (t *Tree) Name() string { return "ostree" }
func (t *Tree) Len() int     { return int(t.n[t.root].size) }
func (t *Tree) Bytes() int   { return 32 * cap(t.n) }

func (t *Tree) upd(x int32) {
	nd := &t.n[x]
	nd.size = 1 + t.n[nd.left].size + t.n[nd.right].size
}

func keyLess(ar int32, aid uint64, br int32, bid uint64) bool {
	return ar < br || (ar == br && aid < bid)
}

// split by key: left gets keys < (r,id), right gets keys >= (r,id).
func (t *Tree) split(x int32, r int32, id uint64) (int32, int32) {
	if x == nilN {
		return nilN, nilN
	}
	if keyLess(t.n[x].r, t.n[x].id, r, id) {
		a, b := t.split(t.n[x].right, r, id)
		t.n[x].right = a
		t.upd(x)
		return x, b
	}
	a, b := t.split(t.n[x].left, r, id)
	t.n[x].left = b
	t.upd(x)
	return a, x
}

func (t *Tree) merge(a, b int32) int32 {
	if a == nilN {
		return b
	}
	if b == nilN {
		return a
	}
	if t.n[a].prio > t.n[b].prio {
		t.n[a].right = t.merge(t.n[a].right, b)
		t.upd(a)
		return a
	}
	t.n[b].left = t.merge(a, t.n[b].left)
	t.upd(b)
	return b
}

func (t *Tree) Add(id uint64, r int) {
	t.rating[id] = int32(r)
	var x int32
	if k := len(t.free); k > 0 {
		x = t.free[k-1]
		t.free = t.free[:k-1]
	} else {
		t.n = append(t.n, node{})
		x = int32(len(t.n) - 1)
	}
	t.n[x] = node{r: int32(r), id: id, prio: t.rng.Uint32(), size: 1}
	a, b := t.split(t.root, int32(r), id)
	t.root = t.merge(t.merge(a, x), b)
}

func (t *Tree) Remove(id uint64) int {
	r := t.rating[id]
	delete(t.rating, id)
	a, b := t.split(t.root, r, id)
	// b's minimum is the node to delete; split it off by key (r, id+1).
	mid, c := t.split(b, r, id+1)
	t.free = append(t.free, mid)
	t.root = t.merge(a, c)
	return int(r)
}

func (t *Tree) Rank(r int) int {
	cnt := int32(0)
	for x := t.root; x != nilN; {
		if int(t.n[x].r) < r {
			cnt += t.n[t.n[x].left].size + 1
			x = t.n[x].right
		} else {
			x = t.n[x].left
		}
	}
	return int(cnt)
}

func (t *Tree) RangeCount(lo, hi int) int {
	if lo > hi {
		return 0
	}
	return t.Rank(hi+1) - t.Rank(lo)
}

// at returns the node with 0-based rank k.
func (t *Tree) at(k int32) int32 {
	x := t.root
	for x != nilN {
		ls := t.n[t.n[x].left].size
		if k < ls {
			x = t.n[x].left
		} else if k == ls {
			return x
		} else {
			k -= ls + 1
			x = t.n[x].right
		}
	}
	return nilN
}

// Nearest uses Rank to find the split point and Select to walk outward.
// Each step is O(log n); a production version would use parent pointers or
// an in-order iterator, but this keeps the structure textbook.
func (t *Tree) Nearest(r, k int, out []uint64) []uint64 {
	total := t.Len()
	up := t.Rank(r)
	down := up - 1
	var dn, un int32
	if down >= 0 {
		dn = t.at(int32(down))
	}
	if up < total {
		un = t.at(int32(up))
	}
	for k > 0 && (down >= 0 || up < total) {
		if up >= total || (down >= 0 && r-int(t.n[dn].r) <= int(t.n[un].r)-r) {
			out = append(out, t.n[dn].id)
			down--
			if down >= 0 {
				dn = t.at(int32(down))
			}
		} else {
			out = append(out, t.n[un].id)
			up++
			if up < total {
				un = t.at(int32(up))
			}
		}
		k--
	}
	return out
}
