"""Crash overlay as a hedge for a long SPY book.
Implements results/crash_overlay_hedge_prereg_2026-11-15.md (locked).

Usage (needs polars for the SPY parquet, e.g. the fin-equity-lab venv):
  python scripts/crash_overlay_hedge_test.py <journal_dir> <SPY.parquet>
<journal_dir>/j/<SYM>/*.jsonl are the backtest journals of the $500 run.
"""
import collections, glob, json, math, sys

BOOK = 200_000
L = 3  # Newey-West lags


def extra(t, entry_bp, stop_bp):  # same rule as scripts/crash_overlay_reprice.py
    market = t["outcome"] == "STOP" or t.get("slip_usd", 0) > 0
    return t["notional_usd"] * (entry_bp + (stop_bp - 5 if market else 0)) / 1e4


def ols_nw(y, x, lags):
    """y = a + b x + e. Returns a, b, Newey-West t(a), t(b)."""
    n = len(y); mx, my = sum(x) / n, sum(y) / n
    b = sum((xi - mx) * (yi - my) for xi, yi in zip(x, y)) / sum((xi - mx) ** 2 for xi in x)
    a = my - b * mx
    e = [yi - a - b * xi for xi, yi in zip(x, y)]
    X = [(1.0, xi) for xi in x]
    S = [[0.0, 0.0], [0.0, 0.0]]
    for lag in range(lags + 1):
        w = 1.0 if lag == 0 else 2 * (1 - lag / (lags + 1))  # x2: both off-diagonal terms
        for t in range(lag, n):
            for i in range(2):
                for j in range(2):
                    S[i][j] += w * e[t] * e[t - lag] * (X[t][i] * X[t - lag][j] if lag == 0
                                                        else 0.5 * (X[t][i] * X[t - lag][j] + X[t - lag][i] * X[t][j]))
    sxx = [[n, sum(x)], [sum(x), sum(xi * xi for xi in x)]]
    det = sxx[0][0] * sxx[1][1] - sxx[0][1] ** 2
    inv = [[sxx[1][1] / det, -sxx[0][1] / det], [-sxx[1][0] / det, sxx[0][0] / det]]
    m = lambda A, B: [[sum(A[i][k] * B[k][j] for k in range(2)) for j in range(2)] for i in range(2)]
    V = m(m(inv, S), inv)
    return a, b, a / math.sqrt(V[0][0]), b / math.sqrt(V[1][1])


def max_dd(series, start=BOOK):
    v = peak = start; dd = 0.0
    for p in series:
        v += p; peak = max(peak, v); dd = max(dd, peak - v)
    return dd


def sd(v):
    m = sum(v) / len(v); return math.sqrt(sum((x - m) ** 2 for x in v) / (len(v) - 1))


def selfcheck():
    xs = [0.01 * i for i in range(-5, 6)]
    a, b, _, _ = ols_nw([3 + 2 * x + (0.001 if i % 2 else -0.001) for i, x in enumerate(xs)], xs, 2)
    assert abs(a - 3) < 1e-3 and abs(b - 2) < 0.1
    assert max_dd([100, -300, 50, 400, -100]) == 300
    assert extra({"notional_usd": 1e4, "outcome": "STOP"}, 8, 8) == 11.0


def overlay_months(trades, k, bp):
    by = collections.defaultdict(float)
    for t in trades:
        by[t["ts"][:7]] += k * t["pnl_usd"] - k * extra(t, bp, bp)
    return by


def run(label, X, r, months):
    x = [r[m] for m in months]; S = [BOOK * v for v in x]; Xs = [X.get(m, 0.0) for m in months]
    C = [s + o for s, o in zip(S, Xs)]
    a, b, ta, tb = ols_nw(Xs, x, L)
    h1 = a > 0 and ta >= 2.0
    sdC, sdS = sd(C), sd(S)
    if sdC > sdS:
        h2, w, B = False, None, None
    else:
        w = sdC / sdS; B = [w * s for s in S]
        h2 = sum(C) > sum(B) and max_dd(C) <= max_dd(B)
    mx, my = sum(x) / len(x), sum(Xs) / len(Xs)
    corr = sum((p - mx) * (q - my) for p, q in zip(x, Xs)) / ((len(x) - 1) * sd(x) * sd(Xs))
    print(f"\n## {label}  (n={len(months)} months)")
    print(f"overlay mean ${my:,.0f}/month (${12*my:,.0f}/yr), sd ${sd(Xs):,.0f}/month")
    print(f"H1: alpha ${a:,.0f}/month (${12*a:,.0f}/yr), NW t={ta:.2f} -> {'PASS' if h1 else 'FAIL'}")
    print(f"    beta ${b/100:,.0f} per 1% SPY move (t={tb:.2f}), corr {corr:.3f}")
    print(f"H2: sd(S) ${sdS:,.0f}  sd(C) ${sdC:,.0f}", end="")
    if w is None:
        print("  -> overlay ADDS risk -> FAIL")
    else:
        print(f"  w={w:.3f}\n    total: C ${sum(C):,.0f}  B ${sum(B):,.0f}  S ${sum(S):,.0f}")
        print(f"    maxDD: C ${max_dd(C):,.0f}  B ${max_dd(B):,.0f}  S ${max_dd(S):,.0f} -> {'PASS' if h2 else 'FAIL'}")
    print(f"VERDICT: {'PASS' if h1 and h2 else 'FAIL'}" if len(months) >= 72 else "VERDICT: NO VERDICT (<72 months)")
    return Xs, x


def main(jdir, spy_path):
    import polars as pl
    selfcheck()
    trades = [json.loads(l) for f in glob.glob(f"{jdir}/j/*/*.jsonl") for l in open(f)]
    trades = [t for t in trades if t["event"] == "close"]
    spy = pl.read_parquet(spy_path).sort("date")
    last = {}
    for d, c in zip(spy["date"].to_list(), spy["adjusted_close"].to_list()):
        last[str(d)[:7]] = c  # sorted, so the last write is the month-end close
    keys = sorted(last)
    r = {m: last[m] / last[p] - 1 for p, m in zip(keys, keys[1:])}
    months = [m for m in sorted(r) if "2020-01" <= m <= "2026-07"]
    print(f"trades {len(trades)}, months {months[0]}..{months[-1]}")
    X, x = run("PRIMARY: $250 stake, 8bp", overlay_months(trades, 0.5, 8), r, months)
    worst = sorted(range(len(months)), key=lambda i: x[i])[:10]
    print("\n10 worst SPY months (report only):")
    for i in worst:
        print(f"  {months[i]}  SPY {100*x[i]:+5.1f}%  book ${BOOK*x[i]:+9,.0f}  overlay ${X[i]:+8,.0f}")
    print(f"  sum: book ${sum(BOOK*x[i] for i in worst):+,.0f}  overlay ${sum(X[i] for i in worst):+,.0f}")
    # quarterly, report only
    q = lambda m: f"{m[:4]}Q{(int(m[5:])-1)//3+1}"
    Xq, rq = collections.defaultdict(float), collections.defaultdict(lambda: 1.0)
    for m, xm, rm in zip(months, X, x):
        Xq[q(m)] += xm; rq[q(m)] *= 1 + rm
    qs = sorted(Xq)
    a, b, ta, tb = ols_nw([Xq[k] for k in qs], [rq[k] - 1 for k in qs], 1)
    print(f"\nQuarterly (report only, n={len(qs)}): alpha ${a:,.0f}/q, NW t={ta:.2f}; beta ${b/100:,.0f} per 1% (t={tb:.2f})")
    run("REPORT ONLY: $500 stake, 9.5bp", overlay_months(trades, 1.0, 9.5), r, months)


if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2])
