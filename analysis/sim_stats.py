"""Equivalence tests for the queue simulation (C2).

For every (shard, policy, b) run, compares the hourly share of "bad" matches
(|gap| > Elo tolerance, i.e. predicted win probability outside [0.45, 0.55])
with the b = 1 run of the same shard and policy, paired by hour.

Equivalence margin: 1 percentage point, fixed in the paper plan (section 5,
phase E) before any simulation was run. TOST at alpha = 0.05 is equivalent to
the 90% confidence interval of the mean paired difference lying inside
(-margin, +margin). The CI uses a moving-block bootstrap over hours (block of
6 hours) because consecutive hours are autocorrelated.

Usage: python3 -I analysis/sim_stats.py analysis/out/sim/grid_default.jsonl [...]
"""
import json
import sys

import numpy as np

MARGIN = 0.01
BLOCK = 6
B_BOOT = 4000
rng = np.random.default_rng(20261023)


def hourly(run):
    m = {b["hour"]: b for b in run["blocks"]}
    return m


def block_bootstrap_mean(x):
    n = len(x)
    nb = int(np.ceil(n / BLOCK))
    starts = rng.integers(0, n - BLOCK + 1, size=(B_BOOT, nb))
    idx = (starts[:, :, None] + np.arange(BLOCK)[None, None, :]).reshape(B_BOOT, -1)[:, :n]
    return x[idx].mean(axis=1)


for path in sys.argv[1:]:
    runs = [json.loads(l) for l in open(path)]
    accept = runs[0]["params"].get("Accept", "true") or "true"
    base = {}
    for r in runs:
        p = r["params"]
        if p["B"] == 1:
            base[(p["Shard"], p["Policy"], p["Delta0"], p["Growth"], p["K"])] = hourly(r)
    out = []
    for r in runs:
        p = r["params"]
        if p["B"] == 1:
            continue
        b1 = base[(p["Shard"], p["Policy"], p["Delta0"], p["Growth"], p["K"])]
        h = hourly(r)
        hours = sorted(set(b1) & set(h))
        d, w = [], []
        for k in hours:
            x, y = h[k], b1[k]
            if x["matches"] < 50 or y["matches"] < 50:
                continue
            d.append(x["bad"] / x["matches"] - y["bad"] / y["matches"])
            w.append(x["sum_gap"] / x["matches"] - y["sum_gap"] / y["matches"])
        d = np.array(d)
        boot = block_bootstrap_mean(d)
        lo, hi = np.percentile(boot, [5, 95])
        out.append({"accept": accept, "shard": p["Shard"], "policy": p["Policy"], "b": p["B"], "delta0": p["Delta0"], "growth": p["Growth"], "k": p["K"], "hours": int(len(d)),
                    "bad_share": r["bad_share"], "bad_share_b1": None, "mean_diff_pp": 100 * float(d.mean()),
                    "ci90_pp": [100 * float(lo), 100 * float(hi)], "equivalent": bool(lo > -MARGIN and hi < MARGIN),
                    "mean_gap_diff": float(np.mean(w))})
    for o in out:
        print(f"{o['accept']:6s} shard={o['shard']:<5} {o['policy']:5s} d0={o['delta0']:<4} g={o['growth']:<3} k={o['k']:<3} b={o['b']:<4d} "
              f"Δbad={o['mean_diff_pp']:+6.2f}pp CI90=[{o['ci90_pp'][0]:+6.2f},{o['ci90_pp'][1]:+6.2f}] "
              f"Δgap={o['mean_gap_diff']:+6.2f} {'EQUIV' if o['equivalent'] else 'not-equiv'}")
    with open(path.replace(".jsonl", "_tost.json"), "w") as f:
        json.dump({"margin_pp": 100 * MARGIN, "block_hours": BLOCK, "boot": B_BOOT, "results": out}, f, indent=1)
