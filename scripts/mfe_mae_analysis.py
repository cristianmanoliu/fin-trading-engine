#!/usr/bin/env python3
"""
mfe_mae_analysis.py — exit-policy decomposition for the deployed-16 candidate.

For each historical trade, the in-engine instrumentation captures:
  MFE_R = peak favorable excursion in stop-distance R-multiples
  MAE_R = peak adverse excursion in stop-distance R-multiples

Under the deployed 6:1 fixed-RR exit, every TARGET trade has MFE_R ≈ 6 and
every STOP trade has MAE_R ≈ 1. The interesting numbers are:
  - MFE_R for STOP losses — how far did losers go in our favor before
    reversing? Distribution determines whether a partial-TP / stop-to-BE
    framework could rescue value the fixed exit leaves on the table.
  - MAE_R for TARGET wins — how close did winners get to stopping out
    before recovering? Tells us whether a tighter stop would have killed
    real winners.
  - MFE_R for time_stop (max-hold force-close) — how much was on the
    table when the position was force-closed?

The script:
  1. Histograms MFE_R / MAE_R per outcome bucket
  2. Computes P(MFE_R ≥ T | STOP) for T ∈ {0.5, 1.0, 1.5, 2.0, 2.5, 3.0, 4.0, 5.0}
  3. Computes a BEST-CASE counterfactual NET improvement under the policy
     "partial 50% at +T·R with stop-to-BE on remainder", assuming the
     remainder always hits target on wins (upper bound — true gain
     requires path-level simulation).

Inputs:
  results/hod_journals/<DATE>/*.jsonl  (with mfe_r/mae_r fields — re-run
                                        scripts/hod_journals.sh after
                                        bumping pkg/execution/stub.go)

Usage:
  python3 scripts/mfe_mae_analysis.py [results/hod_journals/2026-05-07-mfe]
"""
from __future__ import annotations

import json
import sys
from collections import defaultdict
from dataclasses import dataclass
from pathlib import Path


@dataclass
class Trade:
    symbol: str
    side: str
    pnl_usd: float
    outcome: str
    mfe_r: float
    mae_r: float


def load(journal_dir: Path) -> list[Trade]:
    trades: list[Trade] = []
    for jf in sorted(journal_dir.glob("*-*.jsonl")):
        symbol = jf.stem.split("-")[0]
        with jf.open() as f:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                ev = json.loads(line)
                if ev["event"] != "close":
                    continue
                outcome = ev.get("outcome", "?")
                if outcome == "PARTIAL":
                    continue  # partials only fire under multi-level-tp; ignore here
                trades.append(Trade(
                    symbol=symbol,
                    side=ev.get("side", "?"),
                    pnl_usd=ev.get("pnl_usd", 0.0),
                    outcome=outcome,
                    mfe_r=ev.get("mfe_r", 0.0),
                    mae_r=ev.get("mae_r", 0.0),
                ))
    return trades


def percentiles(xs: list[float], qs: list[float]) -> list[float]:
    if not xs:
        return [0.0] * len(qs)
    s = sorted(xs)
    out = []
    for q in qs:
        pos = q * (len(s) - 1)
        lo = int(pos)
        hi = min(lo + 1, len(s) - 1)
        frac = pos - lo
        out.append(s[lo] * (1 - frac) + s[hi] * frac)
    return out


def main() -> int:
    if len(sys.argv) > 1:
        journal_dir = Path(sys.argv[1])
    else:
        root = Path(__file__).parent.parent / "results" / "hod_journals"
        candidates = sorted(p for p in root.iterdir() if p.is_dir() and "mfe" in p.name)
        if not candidates:
            print("no MFE journal dirs — re-run scripts/hod_journals.sh after bumping stub.go")
            return 1
        journal_dir = candidates[-1]

    trades = load(journal_dir)
    if not trades:
        print(f"no trades in {journal_dir}")
        return 1

    n = len(trades)
    by_outcome: dict[str, list[Trade]] = defaultdict(list)
    for t in trades:
        by_outcome[t.outcome].append(t)

    print("MFE/MAE DECOMPOSITION — exit-policy diagnostic for deployed-16 candidate")
    print("=" * 80)
    print(f"journal dir: {journal_dir}")
    print(f"trades:      {n:,}")
    print("outcomes:    " + ", ".join(f"{k}={len(v)}" for k, v in sorted(by_outcome.items())))
    total_net = sum(t.pnl_usd for t in trades)
    n_win = sum(1 for t in trades if t.pnl_usd > 0)
    print(f"total NET:   ${total_net:+,.0f}  ({n_win}/{n} wins, WR={n_win/n*100:.1f}%)")
    print()

    # ── MFE / MAE percentile distributions per outcome ──────────────────────
    qs = [0.10, 0.25, 0.50, 0.75, 0.90, 0.95, 0.99]
    print("PERCENTILES of MFE_R / MAE_R per outcome bucket:")
    print(f"  {'outcome':<12}  {'n':>5}  {'metric':<6}  " + "  ".join(f"p{int(q*100):>02}" for q in qs))
    print(f"  {'-'*12}  {'-'*5}  {'-'*6}  " + "  ".join("-" * 4 for _ in qs))
    for outcome in sorted(by_outcome.keys()):
        bucket = by_outcome[outcome]
        for metric in ("MFE_R", "MAE_R"):
            xs = [t.mfe_r if metric == "MFE_R" else t.mae_r for t in bucket]
            ps = percentiles(xs, qs)
            print(f"  {outcome:<12}  {len(bucket):>5}  {metric:<6}  " +
                  "  ".join(f"{v:>4.2f}" for v in ps))
    print()

    # ── P(MFE_R ≥ T | STOP) — the smoking gun for rescuable losses ───────────
    stops = by_outcome.get("STOP", [])
    print(f"P(MFE_R ≥ T | STOP) — fraction of {len(stops)} losses that reached T·R favorable")
    print("before reversing. Each such trade is a candidate for partial-TP rescue at T:")
    print()
    print(f"  {'T (R)':>6}  {'count':>6}  {'fraction':>9}  {'best-case ΔNET per loss':>28}")
    print(f"  {'-'*6}  {'-'*6}  {'-'*9}  {'-'*28}")
    thresholds = [0.5, 1.0, 1.5, 2.0, 2.5, 3.0, 4.0, 5.0]
    p_above_T = {}
    for T in thresholds:
        cnt = sum(1 for t in stops if t.mfe_r >= T)
        p = cnt / len(stops) if stops else 0
        p_above_T[T] = p
        # Best-case: remainder closes at BE → net per such loss = +T/2 R instead of -1 R.
        # Improvement = T/2 + 1 R per rescued loss.
        delta_per_loss = (T / 2 + 1.0)
        print(f"  {T:>5.1f}   {cnt:>6}  {p:>8.1%}   ${delta_per_loss:>+5.2f}R per rescued loss")
    print()

    # ── Counterfactual: total-portfolio NET under partial-TP at T ────────────
    print("COUNTERFACTUAL — partial 50% at +T·R, stop-to-BE on remainder:")
    print("  Per-trade expected ΔR = -WR×(3 - T/2)  +  (1-WR)×P(MFE≥T|STOP)×(T/2 + 1)")
    print(f"  WR = {n_win/n:.3f}  (deployed-16, 5y)")
    print()

    # Need average win and loss in $ to convert R changes to $ changes.
    # Use mean abs(loss) as 1R proxy: at fixed RR=6, mean win ≈ 6× mean loss.
    losses_usd = [-t.pnl_usd for t in trades if t.pnl_usd < 0]
    wins_usd = [t.pnl_usd for t in trades if t.pnl_usd > 0]
    mean_loss = sum(losses_usd) / len(losses_usd) if losses_usd else 0
    sum(wins_usd) / len(wins_usd) if wins_usd else 0
    # 1R in $ ≈ mean loss (after fees + slip)
    one_r_usd = mean_loss
    print(f"  1R ≈ ${one_r_usd:,.0f}  (mean abs loss after fees/slip)")
    print(f"  baseline NET: ${total_net:+,.0f}  ({total_net / one_r_usd:.1f} R aggregate)")
    print()

    wr = n_win / n
    print(f"  {'T':>4}  {'P(MFE≥T|STOP)':>14}  {'best-case ΔNET':>15}  {'best-case new NET':>18}  {'lift %':>7}")
    print(f"  {'-'*4}  {'-'*14}  {'-'*15}  {'-'*18}  {'-'*7}")
    best_T = None
    best_lift = -1
    for T in thresholds:
        p = p_above_T[T]
        # Per-trade ΔR best case (winners' remainder always hits target):
        delta_r_per_trade = -wr * (3 - T / 2) + (1 - wr) * p * (T / 2 + 1.0)
        delta_net = delta_r_per_trade * n * one_r_usd
        new_net = total_net + delta_net
        lift_pct = delta_net / total_net * 100 if total_net != 0 else 0
        marker = ""
        if delta_net > best_lift:
            best_lift = delta_net
            best_T = T
            marker = "  *"
        print(f"  {T:>3.1f}   {p:>13.1%}   ${delta_net:>+13,.0f}   ${new_net:>+16,.0f}   {lift_pct:>+6.1f}%{marker}")
    print()
    print("  * = highest best-case lift")
    print()

    # ── MAE_R for TARGET — were winners almost killed before recovering? ────
    targets = by_outcome.get("TARGET", [])
    if targets:
        mae_above = [(T, sum(1 for t in targets if t.mae_r >= T) / len(targets))
                     for T in [0.3, 0.5, 0.7, 0.8, 0.9]]
        print(f"MAE_R distribution for TARGET wins ({len(targets)} trades) — how close")
        print("did winners come to stopping out before turning around?")
        for T, frac in mae_above:
            print(f"  P(MAE_R ≥ {T:.1f} | TARGET) = {frac:>5.1%}")
        print(f"  → Tighter stop at K·R would kill {mae_above[-1][1]*100:.1f}% of these wins (at K=0.9).")
        print()

    # ── time_stop force-close — how much was on the table? ──────────────────
    time_stops = by_outcome.get("time_stop", [])
    if time_stops:
        mfe_quantiles = percentiles([t.mfe_r for t in time_stops], [0.10, 0.50, 0.90])
        print(f"MFE_R distribution for time_stop ({len(time_stops)} force-closed @ 504h):")
        print(f"  p10/p50/p90 of MFE_R: {mfe_quantiles[0]:.2f} / {mfe_quantiles[1]:.2f} / {mfe_quantiles[2]:.2f}")
        print()

    # ── Verdict ──────────────────────────────────────────────────────────────
    print("=" * 80)
    print("VERDICT")
    print("=" * 80)
    p_2 = p_above_T[2.0]
    if p_2 >= 0.286:
        print(f"⚠ {p_2*100:.1f}% of STOP losses have MFE_R ≥ 2.0 — exceeds the 28.6%")
        print("  best-case breakeven for partial-TP at 2R + stop-to-BE.")
        print("  → A position-management redesign IS likely +EV. Pre-register the")
        print("    counterfactual with path-level walk-forward validation across 6 windows.")
        print(f"    Best lift in the upper-bound table at T=${best_T} R.")
    else:
        print(f"✓ Only {p_2*100:.1f}% of STOP losses reach MFE_R ≥ 2.0 — below the 28.6%")
        print("  best-case breakeven for partial-TP at 2R.")
        print("  → 6:1 fixed-RR is empirically near-optimal for this strategy.")
        print("  → Position-management is NOT the holy grail; explore HG2 (funding asymmetry)")
        print("    or HG3 (winner-profile classifier) instead.")

    return 0


if __name__ == "__main__":
    sys.exit(main())
