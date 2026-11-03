"""Paper figures from simulation and Lichess data (no hardware measurements).

Writes SVG and 300-dpi PNG to figures/. No titles inside figures: context is
carried by axis labels and the captions in the manuscript.

EEJET layout: every figure is drawn at its printed size (17 cm wide, 11 cm for
the square model plot) with all text in Times New Roman 14 pt (Liberation
Serif, metric-compatible, when Times New Roman is not installed), regular
weight, axis labels at least 0.5 cm from the tick labels, sub-figures stacked
and labelled "a", "b" under each panel, as in the journal sample.

Usage: python3 -I analysis/figures.py
"""
import json
import math
import os
import sys

import matplotlib

matplotlib.use("Agg")
import matplotlib.pyplot as plt  # noqa: E402
import numpy as np  # noqa: E402

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
sys.path.insert(0, os.path.join(ROOT, "analysis"))
from model_check import TAU, p_bad, worst_gap  # noqa: E402

V2 = os.path.join(ROOT, "analysis", "out", "sim", "v2")
FIG = os.path.join(ROOT, "figures")
os.makedirs(FIG, exist_ok=True)

# Validated categorical slots (dataviz skill palette, light mode), fixed order.
C = ["#2a78d6", "#eb6834", "#1baf7a", "#eda100"]
MARK = ["o", "s", "^", "D"]
LS = ["-", "--", ":", "-."]
INK, MUTED, GRID = "#1f1f1e", "#6b6a64", "#e4e3de"

CM = 1 / 2.54
FULL = 17 * CM
PAD = 15  # points; 0.5 cm = 14.2 pt between axis label and tick labels
plt.rcParams.update({
    "font.family": "serif", "font.serif": ["Times New Roman", "Liberation Serif"], "font.size": 14,
    "axes.labelsize": 14, "xtick.labelsize": 14, "ytick.labelsize": 14, "legend.fontsize": 14,
    "legend.title_fontsize": 14, "font.weight": "normal", "axes.labelweight": "normal",
    "mathtext.fontset": "custom", "mathtext.rm": "Liberation Serif", "mathtext.it": "Liberation Serif",
    "axes.edgecolor": INK, "axes.labelcolor": INK, "xtick.color": INK, "ytick.color": INK,
    "axes.spines.top": False, "axes.spines.right": False, "axes.labelpad": PAD,
    "axes.grid": True, "grid.color": GRID, "grid.linewidth": 0.6, "legend.frameon": False,
    "lines.linewidth": 1.8, "lines.markersize": 6, "svg.fonttype": "none",
})


def save(fig, name):
    # No bbox_inches="tight": the file keeps the printed size, so 14 pt stays 14 pt.
    fig.savefig(os.path.join(FIG, name + ".svg"))
    fig.savefig(os.path.join(FIG, name + ".png"), dpi=300)
    plt.close(fig)


def stacked(height_cm):
    return plt.subplots(2, 1, figsize=(FULL, height_cm * CM), layout="constrained")


def tost(path):
    return json.load(open(path))["results"]


SHARDS = [(1.0, "100% of traffic"), (0.1, "10%"), (0.01, "1%")]

# --------------------------------------------------------------- Fig: rating distribution
hist = np.load(os.path.join(ROOT, "analysis", "out", "blitz_ratings_hist.npy"))
r = np.arange(len(hist))
mu = (r * hist).sum() / hist.sum()
sd = math.sqrt(((r - mu) ** 2 * hist).sum() / hist.sum())
fig, ax = plt.subplots(figsize=(FULL, 8 * CM), layout="constrained")
w = 10
h10 = np.add.reduceat(hist, np.arange(0, len(hist), w)) / hist.sum() / w * 1e3  # per 1000 rating points
ax.bar(np.arange(0, len(hist), w)[: len(h10)] + w / 2, h10, width=w * 0.9, color=C[0], alpha=0.85,
       label="Lichess blitz, Sept 1–3, 2026")
xs = np.linspace(0, 3200, 800)
ax.plot(xs, 1e3 * np.exp(-((xs - mu) ** 2) / (2 * sd * sd)) / (sd * math.sqrt(2 * math.pi)), color=C[1],
        ls="--", lw=1.4, label=f"Normal fit (μ = {mu:.0f}, σ = {sd:.0f})")
ax.set_xlim(400, 3000)
ax.set_ylim(0, h10.max() * 1.55)
ax.set_xlabel("Rating")
ax.set_ylabel(r"Density, ×10$^{\mathregular{-3}}$")
ax.legend(loc="upper left")
save(fig, "fig_ratings")

# --------------------------------------------------------------- Fig: quality cost vs b
fig, axes = stacked(17)
for ax, fname, letter in zip(axes, ["grid_true_tost.json", "grid_bucket_tost.json"], "ab"):
    res = [x for x in tost(os.path.join(V2, fname)) if x["policy"] == "best"]
    ax.axhspan(-1, 1, color=GRID, alpha=0.7, lw=0)
    for i, (s_, name) in enumerate(SHARDS):
        pts = sorted((x["b"], x["mean_diff_pp"], x["ci90_pp"]) for x in res if x["shard"] == s_)
        b_ = [p[0] for p in pts]
        m = [p[1] for p in pts]
        lo = [p[1] - p[2][0] for p in pts]
        hi = [p[2][1] - p[1] for p in pts]
        ax.errorbar(b_, m, yerr=[lo, hi], color=C[i], marker=MARK[i], ls=LS[i], capsize=2, label=name)
    ax.set_xscale("log")
    ax.set_xticks([2, 5, 10, 25, 50, 100, 200])
    ax.get_xaxis().set_major_formatter(matplotlib.ticker.ScalarFormatter())
    ax.set_xlabel(f"Bucket width b, rating points\n{letter}")
    ax.set_ylabel("Δ poor matches, pp")
axes[0].set_ylim(-2, 2)
axes[1].legend(title="Shard size", loc="upper left")
save(fig, "fig_quality_vs_b")

# --------------------------------------------------------------- Fig: model vs simulation
rows = []
for f in ["grid_bucket.jsonl", os.path.join("sens", "bucket_delta0.jsonl")]:
    for line in open(os.path.join(V2, f)):
        x = json.loads(line)
        p = x["params"]
        if p["B"] < 5:
            continue
        mj, bj = x["matches_by_j"], x["bad_by_j"]
        tot = sum(mj)
        pred = sum(m * (p_bad(j, p["B"]) if j < 16 else 1.0) for j, m in enumerate(mj)) / tot
        rows.append((p["Shard"], 100 * sum(bj) / tot, 100 * pred))
fig, ax = plt.subplots(figsize=(11 * CM, 11 * CM), layout="constrained")
ax.plot([0, 100], [0, 100], color=MUTED, lw=0.8)
for i, (s, name) in enumerate(SHARDS):
    o = [r_[1] for r_ in rows if r_[0] == s]
    pr = [r_[2] for r_ in rows if r_[0] == s]
    ax.scatter(pr, o, s=45, marker=MARK[i], color=C[i], edgecolor="white", linewidth=0.5, label=name, zorder=3)
ax.set_xlim(0, 85)
ax.set_ylim(0, 85)
ax.set_xticks([0, 20, 40, 60, 80])
ax.set_yticks([0, 20, 40, 60, 80])
ax.set_xlabel("Model: poor matches, %")
ax.set_ylabel("Simulation: poor matches, %")
ax.legend(title="Shard size", loc="upper left")
err = np.array([abs(a - b_) for _, a, b_ in rows])
ax.text(0.97, 0.03, f"n = {len(rows)} runs\nmean |error| = {err.mean():.2f} pp\nmax = {err.max():.2f} pp",
        transform=ax.transAxes, ha="right", va="bottom", color=INK)
save(fig, "fig_model_vs_sim")

# --------------------------------------------------------------- Fig: window alignment
fig, axes = stacked(18)
res = tost(os.path.join(V2, "sens", "bucket_delta0_tost.json"))
for i, d0 in enumerate([10, 25, 50, 100]):
    pts = sorted((x["b"], x["mean_diff_pp"]) for x in res if x["delta0"] == d0)
    axes[0].plot([p[0] for p in pts], [p[1] for p in pts], color=C[i], marker=MARK[i], ls=LS[i],
                 label=f"δ₀ = {d0}")
axes[0].set_xlabel("Bucket width b\na")
axes[0].set_ylabel("Δ poor matches, pp")
axes[0].legend(loc="upper left")
bs = np.arange(1, 121)
for i, d0 in enumerate([25, 50]):
    axes[1].plot(bs, [worst_gap(d0, b_) for b_ in bs], color=C[i + 1 if i else 1], ls=LS[i + 1],
                 label=f"δ = {d0}")
    axes[1].axhline(d0, color=C[i + 1 if i else 1], lw=0.8, alpha=0.6)
axes[1].axhline(TAU, color=MUTED, lw=0.8, ls=":")
axes[1].text(118, TAU + 3, "τ ≈ 34.9", ha="right", color=INK)
axes[1].set_xlabel("Bucket width b\nb")
axes[1].set_ylabel("Largest accepted gap")
axes[1].legend(loc="upper left")
save(fig, "fig_window_alignment")

print("figures written to", FIG)

# --------------------------------------------------------------- Fig: concurrency (bare metal)
import csv  # noqa: E402

PAR = os.path.join(ROOT, "results", "metal-2026-10-30-full20", "summary_parallel.csv")
if os.path.exists(PAR):
    rows = list(csv.DictReader(open(PAR)))
    fig, axes = stacked(19)
    names = [("locked-skiplist", "skip list, one lock"), ("locked-btree", "B+tree, one lock"),
             ("locked-bfi", "BFI, one lock"), ("striped-bfi-s16", "BFI, 16 stripes"),
             ("striped-bfi-s64", "BFI, 64 stripes")]
    PC = C[:3] + ["#8a4fd1", C[3]]
    PM = MARK[:3] + ["v", MARK[3]]
    PL = LS[:3] + [(0, (5, 1, 1, 1)), LS[3]]
    for ax, w, letter in zip(axes, ["1", "10"], "ab"):
        for i, (key, name) in enumerate(names):
            pts = sorted((int(r["cpu"]), 1e3 / float(r["ns_median"])) for r in rows if r["w"] == w and r["structure"] == key)
            ax.plot([p[0] for p in pts], [p[1] for p in pts], color=PC[i], marker=PM[i], ls=PL[i], label=name)
        ax.set_xscale("log", base=2)
        ax.set_xticks([1, 2, 4, 8, 16])
        ax.get_xaxis().set_major_formatter(matplotlib.ticker.ScalarFormatter())
        ax.set_xlabel(f"Threads\n{letter}")
        ax.set_ylabel("Million operations/s")
        ax.set_ylim(0, 10)
    fig.legend(*axes[0].get_legend_handles_labels(), loc="outside upper center", ncol=2)
    save(fig, "fig_parallel")
