"""Phase C: empirical rating distribution and arrival process from Lichess.

Usage: python3 -I analysis/phase_c.py data/derived/lichess_2026-09_d1-3.csv.gz
Writes analysis/out/phase_c.json, data/derived/ratings_sample.txt and draft figures.
"""
import json
import os
import sys

import numpy as np
import pandas as pd
from scipy import stats

src = sys.argv[1]
root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
out_dir = os.path.join(root, "analysis", "out")
os.makedirs(out_dir, exist_ok=True)

df = pd.read_csv(src, dtype={"ts_unix": "int64", "speed": "category", "white_elo": "int32", "black_elo": "int32"},
                 usecols=["ts_unix", "speed", "white_elo", "black_elo"])
res = {"source": os.path.basename(src), "games": int(len(df)),
       "t_first": int(df.ts_unix.min()), "t_last": int(df.ts_unix.max()),
       "by_speed": {k: int(v) for k, v in df.speed.value_counts().items()}}

ELO_TOL = 400 * np.log10(0.55 / 0.45)  # |dr| giving |p-0.5| <= 0.05, ~34.9


def describe(r):
    r = np.asarray(r, dtype=np.float64)
    q = np.percentile(r, [0.1, 1, 5, 25, 50, 75, 95, 99, 99.9])
    # distance from a fitted normal: KS statistic (effect size, not a test at this n)
    mu, sd = r.mean(), r.std()
    ks = stats.kstest((r - mu) / sd, "norm").statistic
    # tail mass beyond 3 sd relative to a normal (0.27%)
    tail3 = float(np.mean(np.abs(r - mu) > 3 * sd))
    return {"n": int(r.size), "mean": float(mu), "sd": float(sd), "skew": float(stats.skew(r)),
            "excess_kurtosis": float(stats.kurtosis(r)), "ks_vs_normal": float(ks),
            "tail_beyond_3sd": tail3, "normal_tail_beyond_3sd": 0.0027,
            "percentiles": dict(zip(["p0.1", "p1", "p5", "p25", "p50", "p75", "p95", "p99", "p99.9"], map(float, q))),
            "min": int(r.min()), "max": int(r.max())}


def bucket_stats(r, widths):
    out = {}
    lo = 0
    for b in widths:
        idx = (r - lo) // b
        counts = np.bincount(idx)
        nz = counts[counts > 0]
        p = nz / nz.sum()
        out[str(b)] = {"buckets_total": int(counts.size), "buckets_nonempty": int(nz.size),
                       "max_share": float(p.max()), "entropy_bits": float(-(p * np.log2(p)).sum()),
                       "share_top10pct_buckets": float(np.sort(p)[::-1][: max(1, nz.size // 10)].sum())}
    return out


res["speeds"] = {}
rng = np.random.default_rng(42)
for speed in ["bullet", "blitz", "rapid", "classical"]:
    d = df[df.speed == speed]
    if len(d) < 1000:
        continue
    r = np.concatenate([d.white_elo.values, d.black_elo.values])
    dr = np.abs(d.white_elo.values.astype(np.int64) - d.black_elo.values)
    ts = d.ts_unix.values
    t0 = ts.min()
    per_min = np.bincount((ts - t0) // 60)
    per_min = per_min[1:-1]  # drop partial edge minutes
    # dispersion within each hour (removes the diurnal trend): var/mean of per-minute counts
    hours = per_min[: (per_min.size // 60) * 60].reshape(-1, 60)
    disp = hours.var(axis=1, ddof=1) / hours.mean(axis=1)
    hourly = np.bincount(((ts - t0) // 3600) % 24 if False else (pd.to_datetime(ts, unit="s").hour), minlength=24)
    res["speeds"][speed] = {
        "games": int(len(d)),
        "ratings": describe(r),
        "pair_gap": {"mean_abs": float(dr.mean()), "median_abs": float(np.median(dr)),
                     "p90_abs": float(np.percentile(dr, 90)), "p99_abs": float(np.percentile(dr, 99)),
                     "share_within_elo_tol": float(np.mean(dr <= ELO_TOL)), "elo_tol": float(ELO_TOL)},
        "arrivals": {"games_per_min_mean": float(per_min.mean()), "games_per_min_min": int(per_min.min()),
                     "games_per_min_max": int(per_min.max()),
                     "dispersion_index_within_hour_median": float(np.median(disp)),
                     "dispersion_index_within_hour_p90": float(np.percentile(disp, 90)),
                     "hourly_profile_utc": [int(x) for x in hourly],
                     "peak_to_trough": float(hourly.max() / max(1, hourly.min()))},
        "buckets": bucket_stats(r.astype(np.int64), [1, 5, 10, 25, 35, 50, 100, 200]),
    }
    if speed == "blitz":
        smp = rng.choice(r, size=min(1_000_000, r.size), replace=False)
        np.savetxt(os.path.join(root, "data", "derived", "ratings_sample.txt"), smp, fmt="%d")
        np.save(os.path.join(out_dir, "blitz_ratings_hist.npy"), np.bincount(r.astype(np.int64)))
        np.save(os.path.join(out_dir, "blitz_per_min.npy"), per_min)

with open(os.path.join(out_dir, "phase_c.json"), "w") as f:
    json.dump(res, f, indent=1)
print(json.dumps({k: res[k] for k in ["games", "by_speed"]}, indent=1))
for s, v in res["speeds"].items():
    rr = v["ratings"]
    print(f"{s:9s} n={v['games']:>9d} mean={rr['mean']:.0f} sd={rr['sd']:.0f} skew={rr['skew']:+.2f} "
          f"exkurt={rr['excess_kurtosis']:+.2f} KS={rr['ks_vs_normal']:.3f} tail3sd={rr['tail_beyond_3sd']:.4f} "
          f"|dr| med={v['pair_gap']['median_abs']:.0f} within35={v['pair_gap']['share_within_elo_tol']:.2f} "
          f"gpm={v['arrivals']['games_per_min_mean']:.0f} disp={v['arrivals']['dispersion_index_within_hour_median']:.2f} "
          f"b25 maxshare={v['buckets']['25']['max_share']:.4f}")
