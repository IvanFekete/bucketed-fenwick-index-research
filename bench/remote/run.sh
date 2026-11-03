#!/usr/bin/env bash
# Benchmark suites. "smoke" checks the whole pipeline in minutes on a cheap
# instance; "full" produces the numbers used in the paper.
set -euo pipefail
SUITE=$1
OUT=$2
mkdir -p "$OUT"
cd "$(dirname "$0")/../.."

# Pin to physical cores of NUMA node 0, leaving core 0 to the OS.
ALL=$(lscpu -p=CPU,CORE,NODE | grep -v '^#' | awk -F, '$3==0 {print $1}' | sort -n | uniq)
if [ "$(echo "$ALL" | wc -l)" -ge 2 ]; then
  CORES=$(echo "$ALL" | tail -n +2 | paste -sd, -)   # leave the first core to the OS
else
  CORES=$(echo "$ALL" | paste -sd, -)                # tiny smoke instance: one core only
fi
PIN="numactl --physcpubind=${CORES%%,*} --membind=0"
PIN_MANY="numactl --physcpubind=$CORES --membind=0"
echo "pin single: $PIN" | tee "$OUT/pinning.txt"
echo "pin many:   $PIN_MANY" | tee -a "$OUT/pinning.txt"

export BFI_RATINGS="$PWD/data/derived/ratings_sample.txt"
phase() { echo "$(date -u +%FT%TZ) $*" | tee -a "$OUT/progress.txt"; }
phase start "$SUITE"
go test ./pkg/... 2>&1 | tee "$OUT/tests.txt"
go test -c -o /tmp/bench.test ./bench

case "$SUITE" in
  smoke)
    COUNT=2; COUNT_SLOW=1; BENCHTIME=200ms
    FILTER='^Benchmark(Rank|Churn)$/^n=100000$/(skiplist|ostree|bfi-fenwick-fifo-b1|bfi-sqrt-fifo-b1)$'
    MIXED='^BenchmarkMixed$/^n=100000$/^w=(1|50)$/(skiplist|bfi-fenwick-fifo-b1)$'
    PAR='^BenchmarkParallel$/.*/^w=10$/(locked-bfi|striped-bfi-s16)$'; CPUS=1,2
    MEM_ARGS="-exp domain -n 20000"
    ;;
  perf)
    COUNT=0; COUNT_SLOW=0; BENCHTIME=3s
    ;;
  full)
    COUNT=5; COUNT_SLOW=3; BENCHTIME=500ms
    FILTER='^Benchmark(Rank|RangeCount|Nearest8|Churn)$'
    MIXED='^BenchmarkMixed$'
    PAR='^BenchmarkParallel$'; CPUS=1,2,4,8,16
    MEM_ARGS="-exp memory,domain -n 100000"
    ;;
  full20)
    # Same benchmarks as "full" with 20 repetitions of every suite; memory and
    # hardware counters are taken from the "full" and "perf" runs.
    COUNT=20; COUNT_SLOW=20; BENCHTIME=500ms
    FILTER='^Benchmark(Rank|RangeCount|Nearest8|Churn)$'
    MIXED='^BenchmarkMixed$'
    PAR='^BenchmarkParallel$'; CPUS=1,2,4,8,16
    MEM_ARGS=""
    ;;
  *) echo "unknown suite $SUITE"; exit 2 ;;
esac

phase isolated
# Isolated-operation latencies (single core). Randomised benchmark order per
# repetition is provided by running each -count pass as a separate process
# with -shuffle.
for i in $(seq 1 "$COUNT"); do
  [ "$COUNT" -gt 0 ] || break
  $PIN /tmp/bench.test -test.run='^$' -test.bench="$FILTER" -test.benchmem -test.benchtime="$BENCHTIME" \
       -test.count=1 -test.cpu=1 -test.shuffle=$i >> "$OUT/bench_raw.txt" 2>> "$OUT/bench_stderr.txt"
done

phase mixed
# Mixed read/write workloads (write-ratio crossover), single core.
for i in $(seq 1 "$COUNT_SLOW"); do
  $PIN /tmp/bench.test -test.run='^$' -test.bench="$MIXED" -test.benchtime="$BENCHTIME" \
       -test.count=1 -test.cpu=1 -test.shuffle=$i >> "$OUT/mixed_raw.txt" 2>> "$OUT/bench_stderr.txt"
done

phase parallel
# Multi-threaded throughput on physical cores of one NUMA node.
for i in $(seq 1 "$COUNT_SLOW"); do
  $PIN_MANY /tmp/bench.test -test.run='^$' -test.bench="$PAR" -test.benchtime="$BENCHTIME" \
       -test.count=1 -test.cpu="$CPUS" -test.shuffle=$i >> "$OUT/parallel_raw.txt" 2>> "$OUT/bench_stderr.txt"
done

phase memdomain
# Heap per player and wide-domain behaviour.
if [ -n "${MEM_ARGS:-}" ]; then
  go build -o /tmp/memdomain ./cmd/memdomain
  $PIN /tmp/memdomain -ratings "$BFI_RATINGS" $MEM_ARGS > "$OUT/memdomain.jsonl" 2>> "$OUT/bench_stderr.txt"
fi

phase perf
# Hardware counters, one process per (operation, structure), so counters are
# attributable. Each pair is measured twice: a setup-only run (-benchtime=1x)
# and a measured run; per-operation counters are (measured - setup) / N.
if { [ "$SUITE" = full ] || [ "$SUITE" = perf ]; } && perf stat -e LLC-loads -- true >/dev/null 2>&1; then
  EVENTS=cycles,instructions,L1-dcache-load-misses,LLC-loads,LLC-load-misses,dTLB-load-misses
  for op in Rank RangeCount Nearest8 Churn; do
    for st in sortedslice skiplist ostree btree bfi-flat-fifo-b1 bfi-sqrt-fifo-b1 bfi-fenwick-fifo-b1 bfi-fenwick-fifo-b25; do
      [ "$op" = Churn ] && [ "$st" = sortedslice ] && continue
      for mode in setup measured; do
        bt=$BENCHTIME; [ $mode = setup ] && bt=1x
        $PIN perf stat -x, -e $EVENTS -o "$OUT/perfx_${op}_${st}_${mode}.csv" /tmp/bench.test -test.run='^$' \
          -test.bench="^Benchmark${op}\$/^n=1000000\$/^${st}\$" -test.benchtime="$bt" -test.count=1 -test.cpu=1 \
          > "$OUT/perfx_${op}_${st}_${mode}.txt" 2>&1 || true
      done
    done
  done
fi

phase done
echo "suite $SUITE finished $(date -u +%FT%TZ)" | tee "$OUT/FINISHED"
