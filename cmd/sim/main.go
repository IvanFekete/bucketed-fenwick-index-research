// Command sim replays real Lichess arrivals through a skill-window matchmaker
// built on BFI and reports match quality and waiting time as a function of
// the bucket width b and the shard size (fraction of traffic kept).
//
// Model (documented in the paper, Section 4):
//   - Each rated game in the dump contributes two arrivals at its UTC start
//     time with the two players' ratings. Real queue-entry times are not in
//     the dump; game start is the closest observable proxy.
//   - A shard keeps each game independently with probability s (seeded), so
//     shard size scales arrival rate while preserving the empirical rating
//     distribution and the diurnal pattern.
//   - Window: delta(w) = min(DeltaMax, Delta0 + Growth*w) for a player who has
//     waited w seconds. A pair is acceptable when |r1 - r2| <= min of the two
//     windows (mutual acceptance).
//   - On arrival the player tries to match immediately; every second the
//     matchmaker sweeps all waiting players, oldest first. A player who waits
//     longer than Abandon seconds leaves.
//   - Candidate generation is the only thing the index decides: Nearest(r, k)
//     returns up to k players by bucket distance (FIFO inside a bucket).
//     Policy "first" takes the first acceptable candidate in that order;
//     policy "best" takes the acceptable candidate with the smallest true
//     rating gap. With b = 1 both reduce to exact nearest-rating matching.
//
// Output: one JSON object per run on stdout, with per-hour blocks so that
// confidence intervals and equivalence tests can be computed downstream.
package main

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/IvanFekete/bucketed-fenwick-index/pkg/bfi"
	"github.com/IvanFekete/bucketed-fenwick-index/pkg/queue"
)

type arrival struct {
	t int64
	r int32
}

type params struct {
	B        int
	Shard    float64
	Policy   string
	K        int
	Delta0   float64
	Growth   float64
	DeltaMax float64
	Abandon  int64
	Seed     int64
	Accept   string // "true": window check on true ratings; "bucket": on bucket indices only
}

type block struct {
	Hour      int64   `json:"hour"`
	Arrivals  int     `json:"arrivals"`
	Matches   int     `json:"matches"`
	Abandoned int     `json:"abandoned"`
	SumGap    float64 `json:"sum_gap"`
	Bad       int     `json:"bad"` // |gap| > EloTol
	Waits     []int32 `json:"-"`
	WaitP50   float64 `json:"wait_p50"`
	WaitP95   float64 `json:"wait_p95"`
	SumWait   float64 `json:"sum_wait"`
}

// EloTol is the rating gap at which predicted win probability leaves
// [0.45, 0.55] under the logistic Elo model: 400*log10(0.55/0.45).
var EloTol = 400 * math.Log10(0.55/0.45)

var dom = queue.Domain{Min: 0, Max: 3500}

func load(path, speed string) []arrival {
	f, err := os.Open(path)
	must(err)
	defer f.Close()
	gz, err := gzip.NewReader(f)
	must(err)
	sc := bufio.NewScanner(bufio.NewReaderSize(gz, 1<<20))
	var out []arrival
	first := true
	for sc.Scan() {
		if first {
			first = false
			continue
		}
		p := strings.Split(sc.Text(), ",")
		if len(p) < 6 || p[1] != speed {
			continue
		}
		t, _ := strconv.ParseInt(p[0], 10, 64)
		w, _ := strconv.Atoi(p[4])
		b, _ := strconv.Atoi(p[5])
		out = append(out, arrival{t, int32(w)}, arrival{t, int32(b)})
	}
	must(sc.Err())
	sort.SliceStable(out, func(i, j int) bool { return out[i].t < out[j].t })
	return out
}

// decorrelate breaks the pairing that Lichess itself performed: the two
// arrivals of one game have strongly correlated ratings, so replaying them
// as-is lets the simulated matchmaker rediscover Lichess's own pairs. Ratings
// are permuted among all arrivals of the same hour, which keeps the arrival
// process and the hourly rating distribution and removes the pair structure.
func decorrelate(a []arrival, seed int64) {
	rng := rand.New(rand.NewSource(seed))
	for i := 0; i < len(a); {
		j := i
		h := a[i].t / 3600
		for j < len(a) && a[j].t/3600 == h {
			j++
		}
		rng.Shuffle(j-i, func(x, y int) { a[i+x].r, a[i+y].r = a[i+y].r, a[i+x].r })
		i = j
	}
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type waiting struct {
	r     int32
	since int64
}

func run(all []arrival, p params) map[string]any {
	rng := rand.New(rand.NewSource(p.Seed))
	// Shard sampling at game granularity: arrivals come in pairs.
	var arr []arrival
	for i := 0; i+1 < len(all); i += 2 {
		if rng.Float64() < p.Shard {
			arr = append(arr, all[i], all[i+1])
		}
	}
	idx := bfi.New(bfi.Config{Domain: dom, Width: p.B, Layout: bfi.FIFO, Counts: "fenwick"}, 1<<16)
	pool := map[uint64]waiting{}
	var order []uint64 // arrival order, lazily compacted
	blocks := map[int64]*block{}
	t0 := arr[0].t
	blk := func(t int64) *block {
		h := (t - t0) / 3600
		b := blocks[h]
		if b == nil {
			b = &block{Hour: h}
			blocks[h] = b
		}
		return b
	}
	window := func(w int64) float64 { return math.Min(p.DeltaMax, p.Delta0+p.Growth*float64(w)) }
	cand := make([]uint64, 0, p.K+1)
	nearestCalls := 0
	// Matches by bucket distance j between the pair (j >= 16 pooled), with
	// counts of bad matches, for validating the analytic model.
	var byJ, badByJ [17]int

	// try matches player id (rating r, waiting since s) at time t; id may or
	// may not be in the index (self is skipped).
	try := func(id uint64, r int32, s, t int64, indexed bool) bool {
		cand = idx.Nearest(int(r), p.K+1, cand[:0])
		nearestCalls++
		dp := window(t - s)
		best := uint64(0)
		bestGap := math.MaxFloat64
		bestAcc := math.MaxFloat64
		for _, c := range cand {
			if c == id {
				continue
			}
			q := pool[c]
			gap := math.Abs(float64(q.r - r))
			acc := gap
			if p.Accept == "bucket" {
				acc = math.Abs(float64(idx.BucketOf(int(q.r))-idx.BucketOf(int(r)))) * float64(p.B)
			}
			if acc > dp || acc > window(t-q.since) {
				continue
			}
			if p.Policy == "first" {
				best, bestGap = c, gap
				break
			}
			if acc < bestAcc {
				best, bestGap, bestAcc = c, gap, acc
			}
		}
		if best == 0 {
			return false
		}
		q := pool[best]
		idx.Remove(best)
		delete(pool, best)
		if indexed {
			idx.Remove(id)
		}
		delete(pool, id)
		b := blk(t)
		b.Matches++
		b.SumGap += bestGap
		j := idx.BucketOf(int(q.r)) - idx.BucketOf(int(r))
		if j < 0 {
			j = -j
		}
		if j > 16 {
			j = 16
		}
		byJ[j]++
		if bestGap > EloTol {
			badByJ[j]++
		}
		if bestGap > EloTol {
			b.Bad++
		}
		for _, w := range []int64{t - s, t - q.since} {
			b.Waits = append(b.Waits, int32(w))
			b.SumWait += float64(w)
		}
		return true
	}

	next := uint64(1)
	i := 0
	for now := t0; i < len(arr) || len(pool) > 0; now++ {
		if i >= len(arr) && now > arr[len(arr)-1].t+p.Abandon+1 {
			break
		}
		// Sweep waiting players oldest first (windows have widened).
		live := order[:0]
		for _, id := range order {
			w, ok := pool[id]
			if !ok {
				continue
			}
			if now-w.since > p.Abandon {
				idx.Remove(id)
				delete(pool, id)
				blk(now).Abandoned++
				continue
			}
			if try(id, w.r, w.since, now, true) {
				continue
			}
			live = append(live, id)
		}
		order = live
		// Arrivals in this second.
		for ; i < len(arr) && arr[i].t == now; i++ {
			a := arr[i]
			blk(now).Arrivals++
			id := next
			next++
			if try(id, a.r, now, now, false) {
				continue
			}
			pool[id] = waiting{a.r, now}
			idx.Add(id, int(a.r))
			order = append(order, id)
		}
		// Jump over idle gaps.
		if len(pool) == 0 && i < len(arr) && arr[i].t > now+1 {
			now = arr[i].t - 1
		}
	}

	hours := make([]int64, 0, len(blocks))
	for h := range blocks {
		hours = append(hours, h)
	}
	sort.Slice(hours, func(a, b int) bool { return hours[a] < hours[b] })
	var out []*block
	var allW []int32
	var tot block
	for _, h := range hours {
		b := blocks[h]
		sort.Slice(b.Waits, func(x, y int) bool { return b.Waits[x] < b.Waits[y] })
		if n := len(b.Waits); n > 0 {
			b.WaitP50 = float64(b.Waits[n/2])
			b.WaitP95 = float64(b.Waits[(n*95)/100])
		}
		allW = append(allW, b.Waits...)
		tot.Arrivals += b.Arrivals
		tot.Matches += b.Matches
		tot.Abandoned += b.Abandoned
		tot.SumGap += b.SumGap
		tot.Bad += b.Bad
		tot.SumWait += b.SumWait
		out = append(out, b)
	}
	sort.Slice(allW, func(x, y int) bool { return allW[x] < allW[y] })
	res := map[string]any{
		"params": p, "elo_tol": EloTol, "buckets": idx.Buckets(), "nearest_calls": nearestCalls,
		"arrivals": tot.Arrivals, "matches": tot.Matches, "abandoned": tot.Abandoned,
		"mean_gap":     tot.SumGap / math.Max(1, float64(tot.Matches)),
		"bad_share":    float64(tot.Bad) / math.Max(1, float64(tot.Matches)),
		"mean_wait":    tot.SumWait / math.Max(1, float64(len(allW))),
		"abandon_rate": float64(tot.Abandoned) / math.Max(1, float64(tot.Arrivals)),
		"blocks":       out,
		"matches_by_j": byJ, "bad_by_j": badByJ,
	}
	if n := len(allW); n > 0 {
		res["wait_p50"] = allW[n/2]
		res["wait_p95"] = allW[(n*95)/100]
		res["wait_p99"] = allW[(n*99)/100]
	}
	return res
}

func main() {
	in := flag.String("in", "data/derived/lichess_2026-09_d1-3.csv.gz", "extracted CSV")
	speed := flag.String("speed", "blitz", "speed class")
	bs := flag.String("b", "1,2,5,10,15,20,25,30,35,40,50,75,100,150,200", "bucket widths")
	shards := flag.String("shard", "1,0.1,0.01", "fractions of traffic")
	policies := flag.String("policy", "first,best", "candidate policies")
	k := flag.Int("k", 16, "candidates per Nearest call")
	d0 := flag.Float64("delta0", 25, "initial window")
	g := flag.Float64("growth", 5, "window growth per second")
	dmax := flag.Float64("deltamax", 400, "maximum window")
	ab := flag.Int64("abandon", 300, "seconds before a player leaves")
	seed := flag.Int64("seed", 1, "shard sampling seed")
	accept := flag.String("accept", "true", "acceptance check: true ratings or bucket indices")
	decor := flag.Bool("decorrelate", true, "permute ratings within each hour to remove Lichess's own pairing")
	flag.Parse()

	all := load(*in, *speed)
	if *decor {
		decorrelate(all, *seed+1000)
	}
	fmt.Fprintf(os.Stderr, "%d %s arrivals\n", len(all), *speed)
	enc := json.NewEncoder(os.Stdout)
	for _, sh := range strings.Split(*shards, ",") {
		s, _ := strconv.ParseFloat(sh, 64)
		for _, pol := range strings.Split(*policies, ",") {
			for _, bstr := range strings.Split(*bs, ",") {
				b, _ := strconv.Atoi(bstr)
				res := run(all, params{B: b, Shard: s, Policy: pol, K: *k, Delta0: *d0, Growth: *g,
					DeltaMax: *dmax, Abandon: *ab, Seed: *seed, Accept: *accept})
				must(enc.Encode(res))
				fmt.Fprintf(os.Stderr, "shard=%-5s %-5s b=%-3d matches=%d gap=%.1f bad=%.4f wait50=%v wait95=%v abandon=%.4f\n",
					sh, pol, b, res["matches"], res["mean_gap"], res["bad_share"], res["wait_p50"], res["wait_p95"], res["abandon_rate"])
			}
		}
	}
}
