"""oi_gate_study.py — candidate #6: OI-confirmed breakout (OI gate on the LIVE EMA cross).

NOT a new signal — a FILTER on the existing live short trades. For each live short,
look up the OI trend at entry and keep only trades where OI confirms. Tests whether
OI confirmation improves the deployed book. Per `results/strategy_candidates_2026-06-10.md`
(#6) + Phase 3. Spec warning: confluence-filter history is bad (-23/-59% on price
filters); this is the OI version.

INPUT: results/live_journals/<SYM>-*.jsonl (live short trades) + data/metrics/<SYM>.csv
(sum_open_interest, 5min). Only the 20 deployed symbols have metrics.

GATE LOGIC (pre-registered)
  At each short's entry ts, compute OI change over the prior 4h (ΔOI_4h).
  Variant RISING: keep short only if OI rising (new shorts opening = continuation).
  Variant FALLING: keep short only if OI falling (short-squeeze risk avoided).
  Compare kept-subset book Sharpe + net vs the full book (baseline).

ACCEPT: a gate raises Sharpe AND keeps enough trades (>= 40% retained, else it's just
        cherry-picking n down). Spec prior: gates disappoint.
"""
import os, glob, json, math, csv, datetime
from collections import defaultdict
import numpy as np

JDIR = "results/live_journals"
METRICS = "data/metrics"
MS = 1000


def load_oi(sym):
    path = os.path.join(METRICS, f"{sym}.csv")
    out = []
    if not os.path.exists(path):
        return out
    with open(path) as f:
        for row in csv.DictReader(f):
            try:
                t = datetime.datetime.strptime(row["create_time"], "%Y-%m-%d %H:%M:%S")
                ts = t.replace(tzinfo=datetime.timezone.utc)
                oi = float(row["sum_open_interest"])
            except (ValueError, KeyError):
                continue
            out.append((ts, oi))
    out.sort()
    return out


def oi_at(oi_list, when):
    """nearest OI value at or before `when` (binary search)."""
    import bisect
    ts = [x[0] for x in oi_list]
    i = bisect.bisect_right(ts, when) - 1
    return oi_list[i][1] if i >= 0 else None


def load_trades_with_oi():
    trades = []
    oi_cache = {}
    for fp in glob.glob(os.path.join(JDIR, "*.jsonl")):
        opens = {}
        with open(fp) as f:
            lines = [json.loads(l) for l in f if l.strip()]
        for e in lines:
            if e.get("event") == "open":
                opens[e["ts"]] = e
        for e in lines:
            if e.get("event") != "close":
                continue
            sym = e["symbol"]
            # close ts != open ts; match by entry price instead
            entry = e.get("entry", 0); notional = e.get("notional_usd", 0)
            pnl = e.get("pnl_usd", 0)
            if entry <= 0 or notional <= 0:
                continue
            # open ts: find the open event with same entry
            open_ts = None
            for ots, oe in opens.items():
                if abs(oe.get("entry", -1) - entry) < 1e-6:
                    open_ts = ots; break
            if open_ts is None:
                continue
            try:
                ots = datetime.datetime.fromisoformat(open_ts.replace("Z", "+00:00"))
            except Exception:
                continue
            if sym not in oi_cache:
                oi_cache[sym] = load_oi(sym)
            oil = oi_cache[sym]
            if not oil:
                continue
            oi_now = oi_at(oil, ots)
            oi_prev = oi_at(oil, ots - datetime.timedelta(hours=4))
            if oi_now is None or oi_prev is None or oi_prev <= 0:
                continue
            d_oi = (oi_now - oi_prev) / oi_prev
            trades.append(dict(sym=sym, close=datetime.datetime.fromisoformat(e["ts"].replace("Z", "+00:00")),
                               ret=pnl / notional, d_oi=d_oi, pnl=pnl, notional=notional))
    trades.sort(key=lambda t: t["close"])
    return trades


def book_stats(trades):
    if len(trades) < 2:
        return None
    byday = defaultdict(float)
    for t in trades:
        byday[t["close"].date()] += t["ret"]
    s = np.array([byday[d] for d in sorted(byday)])
    sh = s.mean() / s.std(ddof=1) * math.sqrt(252) if s.std(ddof=1) > 0 else 0
    return dict(n=len(trades), sharpe=sh, net=sum(t["pnl"] for t in trades),
                wr=np.mean([t["ret"] > 0 for t in trades]))


def main():
    trades = load_trades_with_oi()
    if not trades:
        print("No trades with OI — need journals + metrics."); return
    base = book_stats(trades)
    print("=" * 74)
    print(f"OI-GATE ON LIVE SHORT (#6) — {base['n']} trades w/ OI, "
          f"{len(set(t['sym'] for t in trades))} symbols")
    print("=" * 74)
    print(f"{'gate':<26}{'n':>6}{'retain':>8}{'Sharpe':>9}{'net$':>11}{'WR':>7}")
    print("-" * 74)
    print(f"{'BASELINE (all)':<26}{base['n']:>6}{'100%':>8}{base['sharpe']:>9.3f}"
          f"{base['net']:>11.0f}{base['wr']:>7.0%}")
    gates = {
        "OI rising (>0)":  lambda t: t["d_oi"] > 0,
        "OI falling (<0)": lambda t: t["d_oi"] < 0,
        "OI rising >2%":   lambda t: t["d_oi"] > 0.02,
        "OI falling >2%":  lambda t: t["d_oi"] < -0.02,
    }
    best = None
    for name, gate in gates.items():
        sub = [t for t in trades if gate(t)]
        st = book_stats(sub)
        if not st:
            continue
        retain = st["n"] / base["n"]
        print(f"{name:<26}{st['n']:>6}{retain:>7.0%}{st['sharpe']:>9.3f}"
              f"{st['net']:>11.0f}{st['wr']:>7.0%}")
        if retain >= 0.40 and (best is None or st["sharpe"] > best[1]):
            best = (name, st["sharpe"], retain, st["net"])

    print()
    print("=" * 74)
    print("VERDICT (candidate #6 OI gate)")
    if best and best[1] >= base["sharpe"] + 0.2:
        print(f"  CANDIDATE: '{best[0]}' Sharpe {best[1]:.3f} vs baseline {base['sharpe']:.3f} "
              f"(+{best[1]-base['sharpe']:.3f}), retains {best[2]:.0%} -> verify + corr.")
    else:
        b = f"'{best[0]}' {best[1]:.3f}" if best else "none retaining >=40%"
        print(f"  NO-GO: best qualifying gate {b} does not beat baseline "
              f"{base['sharpe']:.3f} by >=0.2 at >=40% retention. OI confirmation adds no "
              f"edge to the live cross (confluence-filter history holds: gates disappoint).")
    print("=" * 74)


if __name__ == "__main__":
    main()
