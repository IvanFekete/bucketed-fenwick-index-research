"""BFI layout diagram: Fenwick array over buckets, bucket sizes, FIFO lists.

Drawn at its printed size (17 cm wide) with all text in Times New Roman 14 pt
(Liberation Serif when Times New Roman is not installed), as EEJET requires.

Usage: python3 -I analysis/fig_layout.py  ->  figures/fig_layout.{svg,png}
"""
import os

import matplotlib

matplotlib.use("Agg")
import matplotlib.pyplot as plt  # noqa: E402
from matplotlib.patches import FancyArrowPatch, Rectangle  # noqa: E402

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
FIG = os.path.join(ROOT, "figures")
INK, MUTED, LINE = "#1f1f1e", "#6b6a64", "#c9c8c1"
BLUE, ORANGE, AQUA = "#2a78d6", "#eb6834", "#1baf7a"
YELLOW, YELLOW_FILL, YELLOW_INK = "#eda100", "#fde7a6", "#8a5d00"
plt.rcParams.update({"font.family": "serif", "font.serif": ["Times New Roman", "Liberation Serif"],
                     "font.size": 14, "svg.fonttype": "none"})

counts = [1, 0, 3, 2, 0, 4, 1, 2]  # players per bucket
B = len(counts)
fen = []
for i in range(1, B + 1):
    lo = i - (i & -i) + 1
    fen.append((lo, i, sum(counts[lo - 1:i])))

CM = 1 / 2.54
fig = plt.figure(figsize=(17 * CM, 13 * CM))
ax = fig.add_axes([0, 0, 1, 1])
ax.set_xlim(-2.9, B + 0.1)
ax.set_ylim(-6.3, 5.1)
ax.axis("off")
W = 0.86

# Rating axis and buckets
for k in range(B):
    ax.add_patch(Rectangle((k + 0.07, 0), W, 0.6, fc="white", ec=MUTED, lw=0.8))
    ax.text(k + 0.5, 0.3, str(counts[k]), ha="center", va="center", color=INK)
    ax.text(k + 0.07, -0.38, f"{1200 + 25 * k}", ha="center", va="center", color=INK)
ax.text(-2.85, 0.3, "bucket sizes\nsize[k]", va="center", color=INK)
ax.text(-2.85, -0.45, "edges, b = 25", va="center", color=INK)

# Fenwick array: node i covers buckets (i - lowbit(i), i]
levels = {1: 1.2, 2: 2.0, 4: 2.8, 8: 3.6}
for lo, hi, s in fen:
    span = hi - lo + 1
    y = levels[span]
    ax.add_patch(Rectangle((lo - 1 + 0.07, y), hi - lo + W, 0.55, fc=YELLOW_FILL if hi in (4, 6) else "#e8f1fc", ec=BLUE, lw=0.9))
    ax.text((lo - 1 + hi) / 2, y + 0.275, f"t[{hi}]={s}", ha="center", va="center", color=INK)
ax.text(-2.85, 2.4, "Fenwick array t\n(counts)", va="center", color=INK)

# Query path for prefix(6) = t[6] + t[4]
for idx in (6, 4):
    lo, hi, s = fen[idx - 1]
    y = levels[hi - lo + 1]
    ax.add_patch(Rectangle((lo - 1 + 0.04, y - 0.03), hi - lo + W + 0.06, 0.61, fc="none", ec=YELLOW, lw=1.6))
t6, t4 = fen[5][2], fen[3][2]
ax.text(B + 0.05, 4.45, f"Rank(1350) = prefix(6) = t[6] + t[4] = {t6} + {t4} = {t6 + t4}", ha="right", color=INK)
assert t6 + t4 == sum(counts[:6])

# FIFO lists under buckets
ids = iter(range(101, 200))
for k, c in enumerate(counts):
    y = -1.0
    for j in range(c):
        y -= 0.62
        ax.add_patch(Rectangle((k + 0.17, y), W - 0.2, 0.46, fc="#e6f6ef", ec=AQUA, lw=0.8))
        ax.text(k + 0.5, y + 0.23, f"p{next(ids)}", ha="center", va="center", color=INK)
        if j:
            ax.add_patch(FancyArrowPatch((k + 0.5, y + 0.62), (k + 0.5, y + 0.46), arrowstyle="-|>",
                                         mutation_scale=6, color=AQUA, lw=0.8))
ax.text(-2.85, -2.6, "per-bucket lists\n(arrival order,\nO(1) removal)", va="center", color=INK)

# Nearest from bucket 3 outward
ax.add_patch(FancyArrowPatch((3.5, -5.0), (2.4, -5.0), arrowstyle="-|>", mutation_scale=12, color=ORANGE, lw=1.6))
ax.add_patch(FancyArrowPatch((3.5, -5.0), (4.6, -5.0), arrowstyle="-|>", mutation_scale=12, color=ORANGE, lw=1.6))
ax.text(3.5, -5.35, "Nearest(r): own bucket first, then outward;\nempty buckets are skipped with Select",
        ha="center", va="top", color=INK)

os.makedirs(FIG, exist_ok=True)
fig.savefig(os.path.join(FIG, "fig_layout.svg"))
fig.savefig(os.path.join(FIG, "fig_layout.png"), dpi=300)
