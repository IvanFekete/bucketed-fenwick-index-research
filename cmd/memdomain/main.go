// Command memdomain measures (1) steady-state heap bytes per waiting player for
// every structure, and (2) how BFI behaves as the rating domain widens, which
// is the "unbounded domain" limit of applicability (Section 5.4).
//
// Wide domains are produced by scaling the empirical ratings by a factor s
// (fixed-point ratings, as used by systems that store MMR with decimals), so
// the shape of the distribution is unchanged while the number of buckets
// grows to D = 3500*s at b = 1.
//
// Output: JSON lines on stdout.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"strconv"
	"testing"

	"github.com/IvanFekete/bucketed-fenwick-index/pkg/baseline/btree"
	"github.com/IvanFekete/bucketed-fenwick-index/pkg/baseline/ostree"
	"github.com/IvanFekete/bucketed-fenwick-index/pkg/baseline/skiplist"
	"github.com/IvanFekete/bucketed-fenwick-index/pkg/baseline/sortedslice"
	"github.com/IvanFekete/bucketed-fenwick-index/pkg/bfi"
	"github.com/IvanFekete/bucketed-fenwick-index/pkg/queue"
)

func loadRatings(path string) []int {
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	var rs []int
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		v, _ := strconv.Atoi(sc.Text())
		rs = append(rs, v)
	}
	return rs
}

func heap() uint64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

type mk func(dom queue.Domain, n int) queue.Queue

var structures = []struct {
	name string
	mk   mk
}{
	{"sortedslice", func(d queue.Domain, n int) queue.Queue { return sortedslice.New(n) }},
	{"skiplist", func(d queue.Domain, n int) queue.Queue { return skiplist.NewSized(n) }},
	{"ostree", func(d queue.Domain, n int) queue.Queue { return ostree.New(n) }},
	{"btree", func(d queue.Domain, n int) queue.Queue { return btree.New(n) }},
	{"bfi-fenwick-fifo-b1", func(d queue.Domain, n int) queue.Queue {
		return bfi.New(bfi.Config{Domain: d, Width: 1, Layout: bfi.FIFO, Counts: "fenwick"}, n)
	}},
	{"bfi-fenwick-swap-b1", func(d queue.Domain, n int) queue.Queue {
		return bfi.New(bfi.Config{Domain: d, Width: 1, Layout: bfi.Swap, Counts: "fenwick"}, n)
	}},
	{"bfi-fenwick-fifo-b25", func(d queue.Domain, n int) queue.Queue {
		return bfi.New(bfi.Config{Domain: d, Width: 25, Layout: bfi.FIFO, Counts: "fenwick"}, n)
	}},
	{"bfi-sqrt-fifo-b1", func(d queue.Domain, n int) queue.Queue {
		return bfi.New(bfi.Config{Domain: d, Width: 1, Layout: bfi.FIFO, Counts: "sqrt"}, n)
	}},
}

var keep queue.Queue

func memory(enc *json.Encoder, rs []int) {
	dom := queue.Domain{Min: 0, Max: 3500}
	for _, n := range []int{10_000, 100_000, 1_000_000} {
		for _, s := range structures {
			if s.name == "sortedslice" && n > 100_000 {
				continue // O(n) per update: churning 1e6 players takes hours
			}
			rng := rand.New(rand.NewSource(1))
			base := heap()
			q := s.mk(dom, n)
			for i := 0; i < n; i++ {
				q.Add(uint64(i+1), rs[rng.Intn(len(rs))])
			}
			// Churn to steady state: n removals and n additions.
			next := uint64(n + 1)
			ids := make([]uint64, n)
			for i := range ids {
				ids[i] = uint64(i + 1)
			}
			for i := 0; i < n; i++ {
				j := rng.Intn(n)
				q.Remove(ids[j])
				ids[j] = next
				q.Add(next, rs[rng.Intn(len(rs))])
				next++
			}
			idsBytes := uint64(8 * n)
			after := heap()
			keep = q
			per := float64(after-base-idsBytes) / float64(n)
			must(enc.Encode(map[string]any{"exp": "memory", "structure": s.name, "n": n, "bytes_per_player": per}))
			keep = nil
		}
	}
}

func timeOp(f func(b *testing.B)) float64 {
	r := testing.Benchmark(f)
	return float64(r.T.Nanoseconds()) / float64(r.N)
}

func domains(enc *json.Encoder, rs []int, n int) {
	for _, scale := range []int{1, 10, 100, 1000} {
		dom := queue.Domain{Min: 0, Max: 3500 * scale}
		scaled := make([]int, len(rs))
		for i, v := range rs {
			scaled[i] = v * scale
		}
		for _, s := range structures {
			if s.name == "sortedslice" || s.name == "bfi-fenwick-swap-b1" {
				continue
			}
			rng := rand.New(rand.NewSource(2))
			base := heap()
			q := s.mk(dom, n)
			ids := make([]uint64, n)
			for i := 0; i < n; i++ {
				ids[i] = uint64(i + 1)
				q.Add(ids[i], scaled[rng.Intn(len(scaled))])
			}
			mem := float64(heap()-base) / float64(n)
			qs := make([]int, 4096)
			for i := range qs {
				qs[i] = scaled[rng.Intn(len(scaled))]
			}
			out := make([]uint64, 0, 8)
			rank := timeOp(func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					sinkI += q.Rank(qs[i&4095])
				}
			})
			near := timeOp(func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					out = q.Nearest(qs[i&4095], 8, out[:0])
				}
			})
			next := uint64(n + 1)
			churn := timeOp(func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					j := rng.Intn(n)
					q.Remove(ids[j])
					ids[j] = next
					q.Add(next, scaled[rng.Intn(len(scaled))])
					next++
				}
			})
			must(enc.Encode(map[string]any{"exp": "domain", "structure": s.name, "n": n, "domain": dom.Max,
				"bytes_per_player": mem, "rank_ns": rank, "nearest8_ns": near, "churn_ns": churn}))
			keep = q
			keep = nil
		}
	}
}

var sinkI int

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func main() {
	path := flag.String("ratings", "data/derived/ratings_sample.txt", "rating sample")
	exp := flag.String("exp", "memory,domain", "experiments")
	n := flag.Int("n", 100_000, "players for the domain experiment")
	flag.Parse()
	rs := loadRatings(*path)
	enc := json.NewEncoder(os.Stdout)
	for _, e := range []string{"memory", "domain"} {
		if !contains(*exp, e) {
			continue
		}
		if e == "memory" {
			memory(enc, rs)
		} else {
			domains(enc, rs, *n)
		}
	}
}

func contains(list, x string) bool {
	for i := 0; i+len(x) <= len(list); i++ {
		if list[i:i+len(x)] == x && (i+len(x) == len(list) || list[i+len(x)] == ',') && (i == 0 || list[i-1] == ',') {
			return true
		}
	}
	return false
}
