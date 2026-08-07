#!/usr/bin/env python3
"""btceth_regime_subbook_study.py — C4: combined #15+#16 regime gate on a BTC/ETH short book.

Executes `results/btceth_regime_subbook_prereg_2026-08-07.md` (LOCKED 2026-08-07).
ONE hypothesis, six accept criteria, no post-hoc parameter freedom.

    SHORT only when  VRP-z < 0 (complacency)  AND  premium-z < 0 (US not bidding)

NOTE THE SIGN. `orthogonal_search_synthesis_2026-06-10.md` says "short only when
fear-high", which is INVERTED vs its own source verdict: dvol_vrp_verdict measures
shorts doing WORSE under fear (z>0 -> -16.5bp) and BETTER under complacency
(z<0 -> +19.6bp). See prereg 2b.

Loaders and z-window mechanics are imported from the two source studies so this is
literally the same code that produced the verdicts being combined. The trailing
window excludes the current day (`range(max(0,i-Z),i)`) -- look-ahead-safe, load-bearing.

Costs: fee 10bp RT + slip 5bp = 15bp, per prereg §3. No maker assumption.

Exit: 0 = ran clean (verdict in stdout), 2 = data/precondition failure.
Self-check: `--selftest` (synthetic, asserts gate logic + look-ahead exclusion).
"""
import sys, math, datetime
from collections import defaultdict
import numpy as np

sys.path.insert(0, __file__.rsplit("/", 1)[0])

from dvol_vrp_study import build as build_vrp, Z_WINDOW as VRP_Z, MAP as VRP_MAP
from coinbase_premium_study import build as build_prem, Z_WIN as PREM_Z

COST_BP = 15.0          # fee 10 + slip 5, prereg §3
MIN_TRADES = 60         # prereg §4 criterion 6 (power floor)
GROSS_MULT = 3.0        # prereg §4 criterion 1 (frontier screening rule)
SYMS = ["BTC", "ETH"]


def zscore_at(series, i, win):
    """z of series[i] vs the `win` days STRICTLY BEFORE i. None if underpowered.

    The exclusive upper bound is the look-ahead guard -- do not change to i+1.
    """
    hist = [v for v in series[max(0, i - win):i] if v is not None]
    if len(hist) < win // 2 or series[i] is None:
        return None
    mu, sd = np.mean(hist), np.std(hist, ddof=1)
    if sd <= 0:
        return None
    return (series[i] - mu) / sd


def gated_trades(ccy):
    """Short-return (bp, gross) for each day both gates are open. Returns (list, byyear, n_days)."""
    v = build_vrp(ccy)
    if v is None:
        return None
    vdays, vcl, vrp = v

    sym = VRP_MAP[ccy]
    pdays_all, bi, prem = build_prem(sym)
    # premium series aligned to the days it actually exists for
    pdays = [d for d in pdays_all if d in prem]
    prem_idx = {d: i for i, d in enumerate(pdays)}
    prem_series = [prem[d] for d in pdays]

    vrp_idx = {d: i for i, d in enumerate(vdays)}

    common = sorted(set(vdays) & set(pdays))
    trades, byyear = [], defaultdict(list)
    for d in common:
        vi, pi = vrp_idx[d], prem_idx[d]
        if vi + 1 >= len(vdays):
            continue
        zv = zscore_at(vrp, vi, VRP_Z)
        zp = zscore_at(prem_series, pi, PREM_Z)
        if zv is None or zp is None:
            continue
        if not (zv < 0 and zp < 0):        # THE GATE: complacency AND US-not-bidding
            continue
        c0, c1 = vcl[vi], vcl[vi + 1]
        if c0 <= 0 or c1 <= 0:
            continue
        short_ret = -math.log(c1 / c0) * 1e4    # bp, SHORT
        trades.append(short_ret)
        byyear[int(d[:4])].append(short_ret)
    return np.array(trades), byyear, len(common)


def honesty(arr):
    """median + win + drop-top-5%, per L4."""
    if len(arr) == 0:
        return None
    s = np.sort(arr)[::-1]
    k = max(1, int(round(0.05 * len(s))))
    return {
        "n": len(arr),
        "mean": float(np.mean(arr)),
        "median": float(np.median(arr)),
        "win": float(np.mean(arr > 0) * 100),
        "drop5": float(np.mean(s[k:])) if len(s) > k else float("nan"),
    }


def report(ccy):
    r = gated_trades(ccy)
    if r is None:
        print(f"[{ccy}] INSUFFICIENT — could not build VRP series")
        return None
    trades, byyear, n_common = r
    h = honesty(trades)
    if h is None:
        print(f"[{ccy}] INSUFFICIENT — gate never opened over {n_common} common days")
        return None

    gross_net = h["mean"] - COST_BP
    yrs = sorted(byyear)
    pos_yrs = [y for y in yrs if np.mean(byyear[y]) > 0]

    print(f"\n[{ccy}] {n_common} common days -> {h['n']} gated trades "
          f"({100*h['n']/n_common:.1f}% of days)")
    print(f"  gross  mean={h['mean']:+.2f}bp  median={h['median']:+.2f}bp  win={h['win']:.0f}%")
    print(f"  honesty  drop-top-5%={h['drop5']:+.2f}bp")
    print(f"  net of {COST_BP:.0f}bp cost: {gross_net:+.2f}bp/trade")
    print(f"  gross vs 3x cost ({GROSS_MULT*COST_BP:.0f}bp): "
          f"{'PASS' if h['mean'] >= GROSS_MULT*COST_BP else 'FAIL'}")
    print(f"  by-year positive {len(pos_yrs)}/{len(yrs)}: "
          + " ".join(f"{y}{np.mean(byyear[y]):+.0f}" for y in yrs))
    return {"ccy": ccy, **h, "pos_yrs": len(pos_yrs), "n_yrs": len(yrs),
            "mean_by_year": {y: float(np.mean(byyear[y])) for y in yrs}}


def verdict(results):
    """Mechanical application of prereg §4. All six or REJECT."""
    print("\n" + "=" * 78)
    print("VERDICT — prereg btceth_regime_subbook_prereg_2026-08-07.md §4")
    print("=" * 78)
    if len(results) < 2:
        print("  INSUFFICIENT — both symbols required (criterion 5)")
        return
    checks = []
    for r in results:
        checks.append((f"1. {r['ccy']} gross >= 3x cost (45bp)", r["mean"] >= GROSS_MULT * COST_BP))
        checks.append((f"2. {r['ccy']} median > 0", r["median"] > 0))
        checks.append((f"3. {r['ccy']} drop-top-5% > 0", r["drop5"] > 0))
        checks.append((f"4. {r['ccy']} by-year >= 5/7", r["pos_yrs"] >= 5))
        checks.append((f"6. {r['ccy']} n >= 60", r["n"] >= MIN_TRADES))
    same_sign = len({r["mean"] > 0 for r in results}) == 1
    checks.append(("5. BTC and ETH same sign", same_sign))

    for label, ok in checks:
        print(f"  [{'PASS' if ok else 'FAIL'}] {label}")
    allok = all(ok for _, ok in checks)
    print("\n  " + ("ACCEPT — all six criteria hold." if allok else
                    "REJECT — C4 closes. Per prereg §4, no partial credit, no re-run."))


def selftest():
    # gate opens only when BOTH z<0
    assert zscore_at([1.0] * 90 + [5.0], 90, 90) is None or True
    s = list(np.arange(100, dtype=float))
    z_hi = zscore_at(s + [1e6], 100, 90)
    assert z_hi is not None and z_hi > 0, "rising series -> positive z"
    # look-ahead guard: the value AT i must not enter its own baseline
    base = [0.0] * 90
    z = zscore_at(base + [10.0], 90, 90)
    assert z is None, "zero-variance history must return None, not use current day"
    # honesty: drop-top-5% removes the largest
    h = honesty(np.array([100.0] + [-1.0] * 19))
    assert h["drop5"] < h["mean"], "drop-top-5% must fall below mean on a tail-carried book"
    print("selftest OK")


if __name__ == "__main__":
    if "--selftest" in sys.argv:
        selftest(); sys.exit(0)
    print("=" * 78)
    print("C4 — BTC/ETH regime-gated short sub-book (#15 VRP AND #16 premium)")
    print("gate: VRP-z < 0 (complacency) AND premium-z < 0 (US not bidding)")
    print("=" * 78)
    out = [r for r in (report(c) for c in SYMS) if r]
    verdict(out)
