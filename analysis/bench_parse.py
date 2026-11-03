"""Parse Go benchmark output from a bench run into tidy CSV with medians and
95% bootstrap confidence intervals across repetitions.

Usage: python3 -I analysis/bench_parse.py results/<run>/  ->  <run>/summary_*.csv

Inputs (as written by bench/remote/run.sh): bench_raw.txt, mixed_raw.txt,
parallel_raw.txt. Each line looks like
  BenchmarkRank/n=100000/skiplist-1   1000000   1234 ns/op   0 B/op   0 allocs/op
"""
import csv
import os
import re
import sys
from collections import defaultdict

import numpy as np

LINE = re.compile(r"^Benchmark(?P<name>\S+?)(?:-(?P<cpu>\d+))?\s+(?P<iters>\d+)\s+(?P<ns>[\d.]+) ns/op"
                  r"(?:\s+(?P<bytes>[\d.]+) B/op)?(?:\s+(?P<allocs>[\d.]+) allocs/op)?")
rng = np.random.default_rng(1)


def ci(x):
    x = np.asarray(x)
    if len(x) < 3:
        return float(np.median(x)), float("nan"), float("nan")
    boots = np.median(rng.choice(x, size=(4000, len(x)), replace=True), axis=1)
    return float(np.median(x)), float(np.percentile(boots, 2.5)), float(np.percentile(boots, 97.5))


def parse(path):
    rows = defaultdict(list)
    meta = {}
    for line in open(path):
        m = LINE.match(line.strip())
        if not m:
            continue
        parts = m["name"].split("/")
        key = {"bench": parts[0], "cpu": int(m["cpu"] or 1)}
        for p in parts[1:]:
            if "=" in p:
                k, v = p.split("=", 1)
                key[k] = v
            else:
                key["structure"] = p
        k = tuple(sorted(key.items()))
        rows[k].append(float(m["ns"]))
        meta[k] = (m["bytes"], m["allocs"])
    return rows, meta


def main(run_dir):
    for name in ["bench_raw.txt", "mixed_raw.txt", "parallel_raw.txt"]:
        path = os.path.join(run_dir, name)
        if not os.path.exists(path):
            continue
        rows, meta = parse(path)
        out = os.path.join(run_dir, "summary_" + name.replace("_raw.txt", ".csv"))
        keys = sorted({k for key in rows for k, _ in key})
        with open(out, "w", newline="") as f:
            w = csv.writer(f)
            w.writerow(keys + ["reps", "ns_median", "ns_ci_lo", "ns_ci_hi", "bytes_op", "allocs_op"])
            for key, xs in sorted(rows.items()):
                d = dict(key)
                med, lo, hi = ci(xs)
                b, a = meta[key]
                w.writerow([d.get(k, "") for k in keys] + [len(xs), med, lo, hi, b or "", a or ""])
        print("wrote", out, len(rows), "series")


if __name__ == "__main__":
    main(sys.argv[1])
