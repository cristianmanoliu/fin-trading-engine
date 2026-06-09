#!/usr/bin/env python3
"""candidate_correlation.py — measure pairwise monthly-return correlation across
all configs in an overfit returns matrix.

The core question behind "broad combination search": are the configs that beat
baseline INDEPENDENT bets, or correlated copies of the same bet? If the top
performers are all >0.7 correlated, a 5,000-cell grid yields ~few effective
independent strategies, and the deflated-Sharpe haircut dominates. If they are
low-correlation, broad search is justified.

Usage: candidate_correlation.py <matrix_csv> [--live-col LIVE]
Outputs: full correlation of the LIVE col vs each config, the top-performing
configs' cross-correlation, and the effective number of independent bets
(1 / sum(w_i^2) on normalized eigenvalues of the correlation matrix).
"""
import sys, csv, argparse, math

def load(path):
    rows = list(csv.reader(open(path)))
    hdr = rows[0]
    data = rows[1:]
    cols = {}
    for j, name in enumerate(hdr):
        if j == 0:
            continue  # month index
        vals = []
        for r in data:
            try:
                vals.append(float(r[j]))
            except (ValueError, IndexError):
                vals.append(0.0)
        cols[name] = vals
    return cols

def pearson(a, b):
    n = len(a)
    if n == 0:
        return 0.0
    ma = sum(a) / n
    mb = sum(b) / n
    num = sum((a[i] - ma) * (b[i] - mb) for i in range(n))
    da = math.sqrt(sum((x - ma) ** 2 for x in a))
    db = math.sqrt(sum((x - mb) ** 2 for x in b))
    if da == 0 or db == 0:
        return 0.0
    return num / (da * db)

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("matrix_csv")
    ap.add_argument("--live-col", default="LIVE")
    args = ap.parse_args()

    cols = load(args.matrix_csv)
    names = list(cols.keys())
    live = args.live_col

    # totals (5y NET per config) to identify top performers
    totals = {n: sum(v) for n, v in cols.items()}
    ranked = sorted(names, key=lambda n: -totals[n])

    print("=" * 72)
    print("CANDIDATE CORRELATION ANALYSIS")
    print(f"matrix: {len(next(iter(cols.values())))} months × {len(names)} configs")
    print("=" * 72)

    print(f"\nTop 12 configs by 5y total NET:")
    for n in ranked[:12]:
        c = pearson(cols[n], cols[live]) if n != live else 1.0
        print(f"  {n:18s} total=${totals[n]:>14,.0f}   corr_vs_{live}={c:+.3f}")

    # Correlation of every config vs LIVE
    print(f"\nCorrelation vs {live} (all configs, sorted):")
    corr_live = sorted(((pearson(cols[n], cols[live]), n) for n in names if n != live), reverse=True)
    for c, n in corr_live:
        flag = "  <-- HIGH (same bet)" if c > 0.7 else ("  <-- low (diversifier?)" if c < 0.3 else "")
        print(f"  {n:18s} {c:+.3f}{flag}")

    # Cross-correlation among the TOP 6 performers (are the "winners" the same bet?)
    top = ranked[:6]
    print(f"\nCross-correlation among top-6 performers:")
    print("            " + "".join(f"{n[:8]:>9s}" for n in top))
    pair_corrs = []
    for a in top:
        line = f"  {a[:10]:10s}"
        for b in top:
            c = pearson(cols[a], cols[b])
            line += f"{c:>9.2f}"
            if a < b:
                pair_corrs.append(c)
        print(line)
    if pair_corrs:
        print(f"\n  mean pairwise corr (top-6, off-diagonal): {sum(pair_corrs)/len(pair_corrs):+.3f}")
        print(f"  max: {max(pair_corrs):+.3f}  min: {min(pair_corrs):+.3f}")

    # Effective number of independent bets via participation ratio of the
    # correlation matrix eigenvalues (power-iteration-free proxy: 1/sum(rho^2)
    # over the mean |corr| is overkill; use a simple normalized-variance proxy).
    # Proper PR = (sum lambda)^2 / sum(lambda^2). Compute the corr matrix once.
    allnames = ranked  # use all
    M = [[pearson(cols[a], cols[b]) for b in allnames] for a in allnames]
    # eigenvalues of a symmetric PSD-ish matrix via power iteration for the top,
    # but PR needs all. Approx PR using trace identities: trace=N, sum lambda^2 =
    # sum_ij M_ij^2 (Frobenius^2) since eigenvalues^2 sum = ||M||_F^2 for symmetric.
    N = len(allnames)
    fro2 = sum(M[i][j] ** 2 for i in range(N) for j in range(N))
    pr = (N ** 2) / fro2 if fro2 > 0 else 0  # (sum lambda)^2 / sum lambda^2 = N^2/||M||_F^2
    print(f"\nEffective independent bets (participation ratio of {N}×{N} corr matrix):")
    print(f"  PR ≈ {pr:.1f}  out of {N} configs")
    print(f"  → {N} configs collapse to ~{pr:.0f} independent return streams.")
    if pr < N * 0.4:
        print(f"  VERDICT: low effective diversity — broad search yields correlated copies,")
        print(f"           NOT independent bets. Deflated-Sharpe haircut dominates.")
    else:
        print(f"  VERDICT: meaningful diversity — broad search may find independent edge.")
    print("=" * 72)

if __name__ == "__main__":
    main()
