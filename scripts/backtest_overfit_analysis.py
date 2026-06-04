#!/usr/bin/env python3
"""PBO (CSCV) + Deflated Sharpe Ratio for the backtest-overfitting pre-reg.

Pre-reg: results/backtest_overfit_pbo_dsr_decision_rule_2026-05-29.md
Usage:   backtest_overfit_analysis.py <matrix_csv> [--live-col LIVE] [--S 16]
"""
import argparse
import csv
from collections import Counter
from itertools import combinations

import numpy as np
from scipy.stats import norm, skew, kurtosis

EULER_MASCHERONI = 0.5772156649015329


def sharpe(x):
    x = np.asarray(x, dtype=float)
    sd = x.std(ddof=1)
    return float(x.mean() / sd) if sd > 0 else 0.0


def cscv_pbo(M, S=16):
    """Combinatorially-Symmetric CV → PBO, degradation slope, OOS prob-of-loss.

    M: T×N return matrix. Returns (pbo, slope, oos_prob_loss, lambdas, nstar_list).
    nstar_list = the IS-best config index per fold (used to gauge selection
    concentration — when one config wins most folds the degradation slope is a
    regression-to-mean artifact of complementary splits, not an overfit signal).
    """
    M = np.asarray(M, dtype=float)
    T, N = M.shape
    rows_per = T // S
    if rows_per == 0:
        raise ValueError(f"T={T} < S={S}: not enough periods for {S} blocks")
    M = M[: rows_per * S]
    blocks = [np.arange(i * rows_per, (i + 1) * rows_per) for i in range(S)]
    half = S // 2
    lambdas, is_star, oos_star, oos_loss, nstar_list = [], [], [], [], []
    for train_combo in combinations(range(S), half):
        tr = np.concatenate([blocks[b] for b in train_combo])
        te = np.concatenate([blocks[b] for b in range(S) if b not in train_combo])
        is_perf = np.array([sharpe(M[tr, n]) for n in range(N)])
        oos_perf = np.array([sharpe(M[te, n]) for n in range(N)])
        nstar = int(np.argmax(is_perf))
        rank = np.sum(oos_perf <= oos_perf[nstar]) / (N + 1.0)  # (0,1), no 0/1
        rank = min(max(rank, 1e-9), 1 - 1e-9)
        lambdas.append(np.log(rank / (1 - rank)))
        is_star.append(is_perf[nstar])
        oos_star.append(oos_perf[nstar])
        oos_loss.append(1.0 if M[te, nstar].sum() < 0 else 0.0)
        nstar_list.append(nstar)
    lambdas = np.array(lambdas)
    pbo = float(np.mean(lambdas <= 0))
    slope = float(np.polyfit(is_star, oos_star, 1)[0])
    return pbo, slope, float(np.mean(oos_loss)), lambdas, nstar_list


def deflated_sharpe(returns, all_sharpes, N, T=None):
    """Bailey & López de Prado (2014). Per-period inputs. Returns
    (dsr, psr, sr_hat, sr0, g3, g4)."""
    r = np.asarray(returns, dtype=float)
    T = T or len(r)
    sr = sharpe(r)
    g3 = float(skew(r))
    g4 = float(kurtosis(r, fisher=False))  # non-excess kurtosis
    var_sr = float(np.var(np.asarray(all_sharpes, dtype=float), ddof=1))
    sr0 = np.sqrt(var_sr) * (
        (1 - EULER_MASCHERONI) * norm.ppf(1 - 1.0 / N)
        + EULER_MASCHERONI * norm.ppf(1 - 1.0 / (N * np.e))
    )
    denom = np.sqrt(max(1e-12, 1 - g3 * sr + ((g4 - 1) / 4.0) * sr ** 2))
    z = lambda bench: (sr - bench) * np.sqrt(T - 1) / denom
    return (float(norm.cdf(z(sr0))), float(norm.cdf(z(0.0))),
            sr, float(sr0), g3, g4)


def load_matrix(path):
    with open(path) as f:
        rows = list(csv.reader(f))
    header = rows[0]
    labels = header[1:]
    data = np.array([[float(v) for v in r[1:]] for r in rows[1:]], dtype=float)
    return labels, data


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("matrix_csv")
    ap.add_argument("--live-col", default="LIVE")
    ap.add_argument("--S", type=int, default=16)
    args = ap.parse_args()

    labels, M = load_matrix(args.matrix_csv)
    T, Ncfg = M.shape
    live_idx = labels.index(args.live_col)

    pbo, slope, oos_loss, lambdas, nstar_list = cscv_pbo(M, S=args.S)
    all_sr = [sharpe(M[:, j]) for j in range(Ncfg)]
    live_ret = M[:, live_idx]

    counts = Counter(nstar_list)
    modal_idx, modal_cnt = counts.most_common(1)[0]
    concentration = modal_cnt / len(nstar_list)
    live_share = counts.get(live_idx, 0) / len(nstar_list)

    print("=" * 72)
    print("BACKTEST-OVERFITTING ANALYSIS — PBO (CSCV) + Deflated Sharpe")
    print("pre-reg: results/backtest_overfit_pbo_dsr_decision_rule_2026-05-29.md")
    print("=" * 72)
    print(f"matrix: {T} months × {Ncfg} configs   (LIVE col = '{args.live_col}')")
    print(f"LIVE monthly Sharpe = {sharpe(live_ret):.4f}  "
          f"(annualized {sharpe(live_ret) * np.sqrt(12):.3f})")
    print()
    print(f"PBO                         = {pbo:.4f}")
    print(f"degradation slope (OOS~IS)  = {slope:.4f}")
    print(f"OOS prob-of-loss (n*)       = {oos_loss:.4f}")
    print(f"IS-best concentration       = {concentration:.4f}  (modal config '{labels[modal_idx]}')")
    print(f"  LIVE is IS-best in          {live_share:.4f} of folds")
    if concentration > 0.5:
        print("  NOTE: concentration > 0.5 → degradation slope is a regression-to-mean")
        print("        artifact of complementary splits, NOT an overfit signal; rely on PBO + DSR.")
    print()
    print("Deflated Sharpe N-sensitivity:")
    for n in (Ncfg, 2 * Ncfg, 3 * Ncfg):
        dsr, psr, sr, sr0, g3, g4 = deflated_sharpe(live_ret, all_sr, N=n)
        print(f"  N={n:>4}:  DSR={dsr:.4f}  PSR={psr:.4f}  "
              f"SR0={sr0:.4f}  skew={g3:+.3f}  kurt={g4:.3f}")
    print("=" * 72)


if __name__ == "__main__":
    main()
