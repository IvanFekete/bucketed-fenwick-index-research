"""Analytic model of match quality under bucket-level acceptance (C2).

Geometry. Let b be the bucket width and tau = 400*log10(0.55/0.45) ~ 34.9 the
rating gap beyond which predicted win probability leaves [0.45, 0.55]. If two
players sit in buckets at distance j and, inside their buckets, their ratings
are approximately uniform and independent, the true gap is G = |j*b + D| with
D = X - Y triangular on (-b, b). Then

  j = 0:  P(G > tau) = (1 - tau/b)^2            for b > tau, else 0
  j >= 1: P(G > tau) = P(D > tau - j*b), with c = tau - j*b:
          c >= b: 0;  0 <= c < b: (b - c)^2 / (2 b^2);
          -b < c < 0: 1 - (b + c)^2 / (2 b^2);  c <= -b: 1.

Worst case. With bucket-level acceptance and window delta, the largest bucket
distance accepted is J = floor(delta / b), so the largest true gap accepted is
(J + 1) * b - 1, versus delta under true-rating acceptance. In particular for
b <= delta0 the adjacent bucket is accepted on arrival (J >= 1), which explains
why b = 25 (J = 1 at delta0 = 25) can be worse than b = 35 (J = 0).

Decomposition. The queue dynamics decide only the mix f_j of bucket distances
at which matches form; the geometry gives the bad share for each j. Predicted
bad share = sum_j f_j * P_j(b). This script takes f_j from the simulation and
compares the prediction with the observed bad share, per run and per j.

Usage: python3 -I analysis/model_check.py analysis/out/sim/model_bucketaccept.jsonl
"""
import json
import math
import sys

TAU = 400 * math.log10(0.55 / 0.45)


def p_bad(j, b):
    if j == 0:
        return (1 - TAU / b) ** 2 if b > TAU else 0.0
    c = TAU - j * b
    if c >= b:
        return 0.0
    if c >= 0:
        return (b - c) ** 2 / (2 * b * b)
    if c > -b:
        return 1 - (b + c) ** 2 / (2 * b * b)
    return 1.0


def worst_gap(delta, b):
    return (math.floor(delta / b) + 1) * b - 1


if __name__ == "__main__":
    rows = []
    for path in sys.argv[1:]:
        for line in open(path):
            r = json.loads(line)
            p = r["params"]
            mj, bj = r["matches_by_j"], r["bad_by_j"]
            tot = sum(mj)
            if p["B"] == 1 or tot == 0:
                continue
            # j = 16 is a pooled tail bucket; treat it as P = 1 (gap >= 16*b > tau for b >= 3).
            pred = sum(m * (p_bad(j, p["B"]) if j < 16 else 1.0) for j, m in enumerate(mj)) / tot
            obs = sum(bj) / tot
            per_j = [(j, mj[j], bj[j] / mj[j], p_bad(j, p["B"])) for j in range(4) if mj[j] >= 200]
            rows.append((p["Shard"], p["B"], p.get("Delta0"), obs, pred, per_j))
            print(f"shard={p['Shard']:<5} b={p['B']:<4} d0={p.get('Delta0')}: observed bad={obs:.4f} "
                  f"predicted={pred:.4f} diff={obs - pred:+.4f} | " +
                  " ".join(f"j{j}: n={n} obs={o:.3f} model={m:.3f}" for j, n, o, m in per_j))
    if rows:
        err = [abs(o - pr) for _, _, _, o, pr, _ in rows]
        print(f"\nruns={len(rows)} mean |obs-pred|={sum(err)/len(err):.4f} max={max(err):.4f}")


# ---------------------------------------------------------------------------
# Refined geometry: replace "uniform inside a bucket" with the empirical
# rating density f (Lichess blitz histogram). For buckets A and A+j, the pair
# weight is mass(A)*mass(A+j) and the in-bucket ratings follow f restricted to
# each bucket. Exact computation on the integer grid.
import numpy as np  # noqa: E402


def p_bad_empirical(j, b, f):
    f = np.asarray(f, dtype=np.float64)
    nb = (len(f) + b - 1) // b
    f = np.pad(f, (0, nb * b - len(f)))
    F = f.reshape(nb, b)
    mass = F.sum(axis=1)
    num = den = 0.0
    off = np.arange(b)
    for a in range(nb - j):
        ma, mb = mass[a], mass[a + j]
        if ma == 0 or mb == 0:
            continue
        pa, pb = F[a] / ma, F[a + j] / mb
        # gap = j*b + (y - x), x in bucket a, y in bucket a+j (offsets)
        d = j * b + off[None, :] - off[:, None]
        bad = (np.abs(d) > TAU).astype(np.float64)
        w = ma * mb
        num += w * float(pa @ bad @ pb)
        den += w
        if j == 0:
            pass
    return num / den if den else 0.0


def refined(paths, hist_path):
    f = np.load(hist_path)[:3500]
    cache = {}
    errs_u, errs_e = [], []
    for path in paths:
        for line in open(path):
            r = json.loads(line)
            p = r["params"]
            b = p["B"]
            mj, bj = r["matches_by_j"], r["bad_by_j"]
            tot = sum(mj)
            if b == 1 or tot == 0:
                continue
            pe = []
            for j in range(16):
                if (j, b) not in cache:
                    cache[(j, b)] = p_bad_empirical(j, b, f) if mj[j] else 0.0
                pe.append(cache[(j, b)])
            pred_e = (sum(m * pe[j] for j, m in enumerate(mj[:16])) + mj[16]) / tot
            pred_u = sum(m * (p_bad(j, b) if j < 16 else 1.0) for j, m in enumerate(mj)) / tot
            obs = sum(bj) / tot
            errs_u.append(abs(obs - pred_u))
            errs_e.append(abs(obs - pred_e))
            print(f"shard={p['Shard']:<5} b={b:<4} d0={p['Delta0']:<4}: obs={obs:.4f} uniform={pred_u:.4f} empirical={pred_e:.4f}")
    print(f"\nmean |err| uniform={np.mean(errs_u):.4f} (max {np.max(errs_u):.4f}); "
          f"empirical={np.mean(errs_e):.4f} (max {np.max(errs_e):.4f})")
