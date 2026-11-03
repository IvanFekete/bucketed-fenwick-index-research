"""Per-operation hardware counters from the perf suite (bench/remote/run.sh perf).

For every (operation, structure) there is a setup-only run (-benchtime=1x) and
a measured run. testing.B calibrates b.N with several calls, so the measured
run executes more operations than the final N it prints. The number of
operations is therefore recovered from cycles: ops = (cycles_measured -
cycles_setup) / (ns_per_op * f), with f = 2.4 GHz (turbo disabled, performance
governor; recorded in host.json). Counters per operation = delta / ops.
Values within ±0.01 of zero are noise and reported as 0.

Usage: python3 -I analysis/perf_parse.py results/metal-2026-10-26-perf
"""
import csv
import glob
import os
import re
import sys

GHZ = 2.4


def read(f):
    d = {}
    for line in open(f):
        p = line.strip().split(",")
        if len(p) > 2 and p[0].replace(".", "").isdigit():
            d[p[2]] = float(p[0])
    return d


def main(run_dir):
    out = os.path.join(run_dir, "perf_per_op.csv")
    with open(out, "w", newline="") as fh:
        w = csv.writer(fh)
        w.writerow(["op", "structure", "ns_op", "ipc", "l1d_miss_op", "llc_load_op", "llc_miss_op", "dtlb_miss_op", "ops"])
        for f in sorted(glob.glob(os.path.join(run_dir, "perfx_*_measured.csv"))):
            _, op, st, _ = os.path.basename(f).split("_", 3)
            m, s = read(f), read(f.replace("measured", "setup"))
            mm = re.search(r"\s([\d.]+) ns/op", open(f.replace(".csv", ".txt")).read())
            if not (m and s and mm):
                continue
            ns = float(mm.group(1))
            ops = (m["cycles"] - s["cycles"]) / (ns * GHZ)
            per = {k: (m[k] - s[k]) / ops for k in m}
            z = lambda v: 0.0 if abs(v) < 0.01 else v  # noqa: E731
            w.writerow([op, st, ns, round(per["instructions"] / per["cycles"], 2), round(z(per["L1-dcache-load-misses"]), 2),
                        round(z(per["LLC-loads"]), 2), round(z(per["LLC-load-misses"]), 3), round(z(per["dTLB-load-misses"]), 2),
                        int(ops)])
    print("wrote", out)


if __name__ == "__main__":
    main(sys.argv[1])
