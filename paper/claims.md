# Claims register

Every number in paper/manuscript.md, with its source file and the script that produced it.
Re-check this list before each submission draft.

| Section | Claim | Source | Produced by |
|---|---|---|---|
| 4.1 | τ = 400·log10(0.55/0.45) ≈ 34.9 | analytic | `analysis/model_check.py` (TAU), `cmd/sim` (EloTol) |
| 4.1 | Observed rating range 400–3197 | `analysis/out/phase_c.json` → speeds.blitz.ratings.min/max | `analysis/phase_c.py` |
| 4.2 | 8,954,768 games; 4,127,086 blitz; 8,254,172 arrivals | `phase_c.json` → games, by_speed.blitz, speeds.blitz.ratings.n; `data/derived/lichess_extract.log` | `tools/lichess`, `analysis/phase_c.py` |
| 4.4 | δ0 = 25, g = 5, cap 400, abandon 300 s, k = 16 | `cmd/sim` defaults; recorded in every result's `params` | `cmd/sim` |
| 4.5 | 72 hourly blocks, 1 pp margin, 6-h blocks, 4,000 resamples | `analysis/out/sim/v2/*_tost.json` (margin_pp, block_hours, boot, results[].hours) | `analysis/sim_stats.py` |
| 5.3 | mean 1612, sd 367, skew −0.08, ex. kurtosis −0.19, KS 0.021 | `phase_c.json` → speeds.blitz.ratings | `analysis/phase_c.py` |
| 5.3 | 0.18 % beyond 3σ vs 0.27 % normal | `phase_c.json` → tail_beyond_3sd | `analysis/phase_c.py` |
| 5.3 | spike at 1500: 0.27 % of ratings, ≈3× neighbourhood | `analysis/out/blitz_ratings_hist.npy` (h[1500]/sum = 0.00274; mean of ±10 = 0.00092) | ad-hoc check on the histogram file |
| 5.3 | 425–1,827 games/min; peak/trough 2.65; dispersion median 1.58, p90 3.80 | `phase_c.json` → speeds.blitz.arrivals | `analysis/phase_c.py` |
| 5.3 | median gap in played games 30; 54 % within τ | `phase_c.json` → speeds.blitz.pair_gap | `analysis/phase_c.py` |
| 5.3 | b=1 poor-match share 1.6 / 17.1 / 53.6 %; median wait 0 / 1 / 9 s | `analysis/out/sim/v2/grid_true.log` (b=1 lines) | `cmd/sim` via `analysis/run_all_sims.sh` |
| 5.3 | exact acceptance: 84/84 equivalent, max 0.13 pp | `analysis/out/sim/v2/grid_true_tost.json` | `analysis/sim_stats.py` |
| 5.3 | sensitivity: 48/48 equivalent, max 0.05 pp | `analysis/out/sim/v2/sens/true_*_tost.json` | `analysis/sim_stats.py` |
| 5.3 | waits and abandonment unchanged across b | `analysis/out/sim/v2/grid_true.log` | `cmd/sim` |
| 5.3 | bucket acceptance equivalent for b ≤ 5 (shards 1, 0.01), b ≤ 2 (0.1) | `analysis/out/sim/v2/grid_bucket_tost.json` | `analysis/sim_stats.py` |
| 5.3 | b=100: +41.0 / +25.8 / +6.4 pp; b=200: +65.9 / +49.5 / +15.2 pp | `grid_bucket_tost.json` (policy best; first identical) | `analysis/sim_stats.py` |
| 5.3 | b=25 +11.0 pp vs b=35 +5.4 pp (shard 1, δ0 = 25) | `grid_bucket_tost.json` | `analysis/sim_stats.py` |
| 5.3 | formula (1) G_max = (⌊δ/b⌋+1)·b − 1 | analytic | `analysis/model_check.py` (worst_gap) |
| 5.3 | formulas (2), (3) | analytic | `analysis/model_check.py` (p_bad) |
| 5.3 | 102 runs, mean |err| 0.33 pp, max 0.56 pp (uniform) | `analysis/out/sim/v2/model_check.txt` (excluding b = 2) | `analysis/model_check.py` |
| 5.3 | empirical density: mean 0.07 pp, max 0.33 pp | recomputed with `model_check.refined` on v2 grid + δ0 sweep, b ≥ 5 | `analysis/model_check.py` |
| 5.3 | dense limit (1 − τ/b)² = 0.42 at b = 100 | analytic | — |
| 5.1 | Table 1 bounds and space | code in `pkg/` (node and array layouts) | — |
| 6 | "up to 66 pp" | = b = 200, shard 1 above | — |

Not yet registered (PENDING sections): all latency, memory, cache and concurrency numbers.

## Bare-metal numbers (Sections 4.5, 5.2, 5.4, 6, 7, abstract)

| Claim | Source | Produced by |
|---|---|---|
| Host: c7i.metal-24xl, Xeon Platinum 8488C, 48 cores, 1 socket, 105 MiB L3, SMT off, performance governor, no_turbo=1, THP madvise, Go 1.24.7, Linux 7.0 | `results/metal-2026-10-26/host.json` | `bench/remote/prepare_host.sh` |
| 20 repetitions × 500 ms (isolated, mixed, parallel); memory and hardware counters from separate runs | `bench/remote/run.sh` (suite full20), `results/metal-2026-10-30-full20/summary_*.csv` reps column | — |
| Table 2 (all latencies with CIs); Rank 26.2 across n; skiplist 274→1,152; B+tree 142→317; update 373 vs 305–323 (diff 50–68); sorted array 149 μs at 1e5 | `results/metal-2026-10-30-full20/summary_bench.csv` | `analysis/bench_parse.py` |
| ratios 2.8, 2.5, 12, 11, 44, 41 | computed from Table 2 medians | — |
| swap 20.8 vs FIFO 46.2 Nearest; update 366 vs 303–326 | `summary_bench.csv` | — |
| heap/player at 1e6: 53.9, 40.6, 61.8, 99.2, 121.4 | `results/metal-2026-10-26/memdomain.jsonl` (exp memory) | `cmd/memdomain` |
| per-bucket arrays ≈ 56 kB at B = 3500; Fenwick array 14 kB | analytic from `pkg/bfi` layout | — |
| Table 3 hardware events, IPC, ~8,000 instructions per flat Rank | `results/metal-2026-10-26-perf/perf_per_op.csv` | `analysis/perf_parse.py` |
| Table 4 mixed workload; crossover between 50 % and 75 %; sqrt 8–9 % faster at 90 %; B+tree 5.8–9.3× slower at w ≤ 10 %; CI half-widths ≤ 0.9 % | `results/metal-2026-10-30-full20/summary_mixed.csv` | `analysis/bench_parse.py` |
| Table 5 wide domain (n = 1e5) | `results/metal-2026-10-26/memdomain.jsonl` (exp domain) | `cmd/memdomain` |
| Table 6 / Fig. 6 throughput; 9.03 → 4.09 Mops; 64 stripes 3.56 → 6.92; B+tree 2.08, skip list 1.25 at 16 threads, 1 % | `results/metal-2026-10-30-full20/summary_parallel.csv` | `analysis/bench_parse.py`, `analysis/figures.py` |
| 5.4 / Table 6: 16 stripes 2.99–4.32 Mops at 1 % updates; above one lock only at 16 threads (4.27 vs 4.09) | `results/metal-2026-10-30-full20/summary_parallel.csv` (striped-bfi-s16; Mops = 1000 / ns_median) | `analysis/bench_parse.py` |
| 4.3: striped BFI design (stripes on bucket boundaries, per-stripe RWMutex + atomic count, 256-way ID map, Nearest merge exact distances for b = 1, per-stripe linearizability) | `pkg/concurrent/concurrent.go` (package comment, `NewStriped`, `Nearest`) | — |
| rank 20 % faster at b = 25 (26.2 → 20.8) | `summary_bench.csv` | — |
| abstract: 1821 characters with keywords | computed | — |
| 6: prior work [30] — 35× vs skip list at n = 10^5, 1 vCPU, no domain-based baselines | HackerNoon article (Fekete, 11.06.2026), read 29.10.2026 | — |
| 6: hypothesis summary — BFI (fenwick, FIFO, b = 1) faster than sortedslice/skiplist/ostree/btree in Rank, RangeCount, Churn, Nearest8 at n ∈ {1e4, 1e5, 1e6}; faster than all comparison-based structures at every w in the mixed workload | `results/metal-2026-10-30-full20/summary_bench.csv`, `summary_mixed.csv` | `analysis/bench_parse.py` (checked 30.10.2026) |
| 1 | 955 blitz games/min on average; ≈1,900 arrivals/min (2 per game) | `phase_c.json` → speeds.blitz.arrivals.games_per_min_mean = 955.5 | `analysis/phase_c.py` |
| 6 / Table 7 | capabilities of related work | [2], [3], [4], [8] (DISC 2024 abstract: wait-free trie over {0..N−1}, lock-free BST, select and range queries, no experiments described), [20]–[23] as described in Section 2 | — |
| 6 worked example: round = 1e5 × Nearest8 + 1,000 × Churn at n = 1e5: BFI b1 47.4 + 328.9 → 5.07 ms; b25 28.3 + 315.1 → 3.14 ms; B+tree 350.6 + 1244.5 → 36.3 ms; skip list 634.0 + 2372.5 → 65.8 ms; b limits from Section 5.3 | `results/metal-2026-10-30-full20/summary_bench.csv`; `analysis/out/sim/v2/grid_*_tost.json` | arithmetic in this row |
