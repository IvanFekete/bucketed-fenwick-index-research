# Bucketed Fenwick index for skill-based matchmaking queues

Code, raw measurements, simulation outputs and analysis scripts for the paper

> I. Fekete. *A bucketed Fenwick index for skill-based matchmaking queues: operating envelope and the cost of rating quantization.* Submitted to the Eastern-European Journal of Enterprise Technologies.

The paper text is in [`paper/manuscript.md`](paper/manuscript.md). Every number in it is mapped to the data file and script that produced it in [`paper/claims.md`](paper/claims.md).

## What is here

A matchmaking queue answers three kinds of queries: how many waiting players have a rating below `r` (rank), how many lie in `[lo, hi]` (range count), and which `k` players are closest to `r` (k-nearest). The bucketed Fenwick index (BFI) keeps a Fenwick tree of player counts over rating buckets of width `b`, plus a list of players per bucket, so every operation costs `O(log B)` in the number of buckets `B` rather than in the number of players.

The repository contains:

- the index and the structures it is compared with, all behind one interface;
- property-based tests against a reference implementation;
- single-threaded, mixed-workload and multi-threaded benchmarks, with the raw results from a bare-metal server;
- a matchmaking simulator that replays Lichess arrivals to measure how rating quantization affects match quality, plus the closed-form model that predicts it;
- the scripts that turn raw results into the tables and figures of the paper.

## Layout

| Path | Contents |
|---|---|
| `pkg/queue` | Common queue interface (insert, remove, rank, range count, k-nearest) |
| `pkg/bfi` | Bucketed Fenwick index: bucket width `b`, FIFO or swap lists, pluggable count index |
| `pkg/countidx` | Count indexes over buckets: Fenwick tree, flat array, square-root decomposition |
| `pkg/baseline` | Comparison-based baselines: sorted array, skip list with spans, order-statistic treap, order-statistic B+tree |
| `pkg/concurrent` | Single reader–writer lock wrapper and striped BFI for the concurrency experiment |
| `pkg/check` | Property-based tests of every structure against a reference |
| `bench` | Go benchmarks: isolated operations, mixed workload, parallel throughput |
| `bench/remote` | Host preparation and benchmark suites used on the bare-metal server |
| `cmd/sim` | Matchmaking simulator (widening window, exact or bucket-level acceptance) |
| `cmd/memdomain` | Heap per player and wide-rating-domain experiment |
| `tools/lichess` | Streams a Lichess PGN dump and extracts start time, time control and ratings |
| `tools/build_docx.py` | Builds the journal-formatted `.docx` from the manuscript |
| `data/derived` | Rating sample used by the benchmarks, extraction log |
| `results/` | Raw benchmark output and summaries from the bare-metal runs |
| `analysis/` | Analysis scripts; `analysis/out` holds their outputs and the simulation results |
| `figures/` | Figures of the paper (PNG, 300 dpi, and SVG) |
| `paper/` | Manuscript and the claims-to-data map |

## Requirements

- Go 1.24.7 or newer
- Python 3.11+ with `numpy`, `scipy`, `pandas`, `matplotlib` (analysis and figures), `python-docx` and `pandoc` (only for `tools/build_docx.py`)
- Linux with `perf` and `numactl` for the hardware-counter and pinned runs

## Reproducing the results

### Tests

```bash
go test ./pkg/...
```

### Benchmarks

The benchmarks draw ratings from the empirical blitz distribution in `data/derived/ratings_sample.txt`.

```bash
export BFI_RATINGS=$PWD/data/derived/ratings_sample.txt
go test -run '^$' -bench . -benchtime 500ms -cpu 1 ./bench
```

The paper's numbers come from `bench/remote/run.sh`, which pins each run to one core, repeats every benchmark 20 times as a separate process and collects hardware counters with `perf`:

```bash
sudo bench/remote/prepare_host.sh results/my-run   # SMT and turbo off, fixed frequency, records host.json
bench/remote/run.sh full20 results/my-run
bench/remote/run.sh perf   results/my-run-perf
python3 -I analysis/bench_parse.py results/my-run
python3 -I analysis/perf_parse.py  results/my-run-perf
```

The runs reported in the paper were made on an Amazon EC2 `c7i.metal-24xl` (Intel Xeon Platinum 8488C, Ubuntu 24.04, Linux 7.0, Go 1.24.7); their raw output and summaries are in `results/metal-2026-10-30-full20` (latency, mixed workload, parallel throughput), `results/metal-2026-10-26` (memory, wide domains) and `results/metal-2026-10-26-perf` (hardware counters). `host.json` in each directory records the host configuration.

### Lichess data

The simulation uses rated games played on 1–3 September 2026 from the [Lichess open database](https://database.lichess.org) (CC0). The extracted CSV is not stored in the repository; to recreate it:

```bash
curl -s https://database.lichess.org/standard/lichess_db_standard_rated_2026-09.pgn.zst \
  | go run ./tools/lichess -until 2026-09-04T00:00:00Z \
  | gzip > data/derived/lichess_2026-09_d1-3.csv.gz
python3 -I analysis/phase_c.py data/derived/lichess_2026-09_d1-3.csv.gz   # rating distribution, arrivals
```

### Simulation and model

```bash
go build -o sim ./cmd/sim
./analysis/run_all_sims.sh                                   # writes analysis/out/sim/v2
python3 -I analysis/sim_stats.py analysis/out/sim/v2/grid_true.jsonl
python3 -I analysis/sim_stats.py analysis/out/sim/v2/grid_bucket.jsonl
python3 -I analysis/model_check.py analysis/out/sim/v2/grid_bucket.jsonl
```

The simulation results used in the paper are already in `analysis/out/sim/v2`.

### Figures and manuscript

```bash
python3 -I analysis/fig_layout.py
python3 -I analysis/figures.py
python3 tools/build_docx.py paper/manuscript_eejet.docx
```

## License

MIT, see [LICENSE](LICENSE). The Lichess data is in the public domain (CC0).
