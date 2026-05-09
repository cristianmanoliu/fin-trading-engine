#!/usr/bin/env python3
"""
cat_f2_cocross_analyze.py — Cat F2 co-cross confluence filter.

Pre-registered 2026-05-07 — see
results/cat_f2_cocross_confluence_decision_rule_2026-05-07.md.

Two-pass post-hoc filter:
  Pass 1 (already done by scripts/hod_journals.sh): universe-57 EMA short
    journals captured at results/hod_journals/2026-05-07-univ57/
  Pass 2 (this script): for each deployed-16 trade, count co-cross events
    on the OTHER 56 universe symbols at the same 4H boundary (±60s),
    filter to retain only trades with n_cocross ≥ 5, compute filtered vs
    unfiltered NET / WR / NET-per-trade aggregate + per-window.

Decision rule (locked):
  DEPLOY-CANDIDATE: filtered NET ≥ unfiltered NET AND ≥5/6 windows
                    filtered ≥ unfiltered AND filtered NET/trade > unfiltered
  SHADOW DEPLOY: filtered NET/trade ≥ 1.3× unfiltered AND filtered total
                 ≥ 60% of unfiltered AND ≥4/6 windows filtered NET/trade
                 beats unfiltered
  WALK-FORWARD CANDIDATE: filtered WR > unfiltered by ≥2pp aggregate AND
                          positive NET in ≥3/6 windows
  REJECT: otherwise
"""
from __future__ import annotations

import json
import sys
from collections import defaultdict
from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
from pathlib import Path

# ── Locked parameters from pre-registration ──────────────────────────────────
K_THRESHOLD = 5        # n_cocross required ≥ 5
TIME_TOLERANCE_S = 60  # ±60 seconds at the 4H boundary
WINDOWS = [
    ("W-2", "2020-05-01", "2021-05-01"),
    ("W-1", "2021-05-01", "2022-05-01"),
    ("W0",  "2022-05-01", "2023-05-01"),
    ("W1",  "2023-05-01", "2024-05-01"),
    ("W2",  "2024-05-01", "2025-05-01"),
    ("W3",  "2025-05-01", "2026-05-01"),
]


@dataclass
class Trade:
    symbol: str
    side: str
    pnl_usd: float
    entry_ts: datetime
    outcome: str


def parse_iso(s: str) -> datetime:
    return datetime.fromisoformat(s.replace("Z", "+00:00")).astimezone(timezone.utc)


def boundary_4h(ts: datetime) -> datetime:
    """Snap a timestamp to its 4H boundary (00, 04, 08, 12, 16, 20 UTC)."""
    return ts.replace(hour=(ts.hour // 4) * 4, minute=0, second=0, microsecond=0)


def load_trades(journal_dir: Path) -> list[Trade]:
    """Load all close events with their open-event entry timestamps."""
    trades: list[Trade] = []
    for jf in sorted(journal_dir.glob("*-*.jsonl")):
        symbol = jf.stem.split("-")[0]
        open_ev = None
        for line in jf.read_text().splitlines():
            if not line:
                continue
            ev = json.loads(line)
            if ev["event"] == "open":
                open_ev = ev
            elif ev["event"] == "close" and open_ev is not None:
                if ev.get("outcome") == "PARTIAL":
                    open_ev = None
                    continue
                trades.append(Trade(
                    symbol=symbol,
                    side=open_ev["side"],
                    pnl_usd=ev.get("pnl_usd", 0.0),
                    entry_ts=parse_iso(open_ev["ts"]),
                    outcome=ev.get("outcome", "?"),
                ))
                open_ev = None
    return trades


def load_cross_index(journal_dir: Path) -> dict[datetime, set[str]]:
    """Build {boundary_4h_ts → set of symbols that crossed at that boundary}.

    Uses ALL 'open' events from the universe-57 journals. Each open event's
    `ts` IS the 4H boundary (since signal_tf=4H), so no extra rounding needed.
    """
    idx: dict[datetime, set[str]] = defaultdict(set)
    for jf in sorted(journal_dir.glob("*-*.jsonl")):
        symbol = jf.stem.split("-")[0]
        for line in jf.read_text().splitlines():
            if not line:
                continue
            ev = json.loads(line)
            if ev["event"] != "open":
                continue
            ts = parse_iso(ev["ts"])
            idx[ts].add(symbol)
    return idx


def count_cocross(t: Trade, cross_idx: dict[datetime, set[str]]) -> int:
    """How many symbols (excluding t.symbol) crossed within ±60s of t.entry_ts?"""
    boundary = t.entry_ts  # signal_tf=4H means entry_ts is already at boundary
    syms_at_boundary: set[str] = set()
    # Match exact boundary plus ±N×4h-floor adjustments are unnecessary —
    # all 4H crosses use the same boundary timestamp. The ±60s tolerance
    # accounts for tick lag, but in our data the ts is already exactly at
    # 4H boundary (e.g. "12:00:00Z"), so most matches are exact.
    for delta in (timedelta(seconds=-TIME_TOLERANCE_S),
                  timedelta(0),
                  timedelta(seconds=TIME_TOLERANCE_S)):
        syms_at_boundary |= cross_idx.get(boundary + delta, set())
    syms_at_boundary.discard(t.symbol)
    return len(syms_at_boundary)


def aggregate(trades: list[Trade]) -> dict:
    n = len(trades)
    if n == 0:
        return {"n": 0, "wins": 0, "wr": 0.0, "net": 0.0, "net_per_trade": 0.0}
    wins = sum(1 for t in trades if t.pnl_usd > 0)
    net = sum(t.pnl_usd for t in trades)
    return {
        "n": n,
        "wins": wins,
        "wr": wins / n * 100,
        "net": net,
        "net_per_trade": net / n,
    }


def in_window(t: Trade, start_iso: str, end_iso: str) -> bool:
    s = parse_iso(start_iso + "T00:00:00+00:00")
    e = parse_iso(end_iso + "T00:00:00+00:00")
    return s <= t.entry_ts < e


def main() -> int:
    deployed_dir = Path("results/hod_journals/2026-05-07-mfe")
    universe_dir = Path("results/hod_journals/2026-05-07-univ57")
    if not deployed_dir.is_dir() or not universe_dir.is_dir():
        print(f"missing {deployed_dir} or {universe_dir}")
        return 1

    print(f"loading deployed-16 trades from {deployed_dir} ...")
    deployed_trades = load_trades(deployed_dir)
    print(f"  → {len(deployed_trades)} trades")

    print(f"loading universe-57 cross index from {universe_dir} ...")
    cross_idx = load_cross_index(universe_dir)
    print(f"  → {sum(len(v) for v in cross_idx.values())} cross events at "
          f"{len(cross_idx)} unique 4H boundaries")

    # Annotate each deployed trade with n_cocross.
    print("computing n_cocross per deployed trade ...")
    for t in deployed_trades:
        t.n_cocross = count_cocross(t, cross_idx)  # type: ignore[attr-defined]

    # Distribution of n_cocross.
    cocross_dist = defaultdict(int)
    for t in deployed_trades:
        cocross_dist[t.n_cocross] += 1
    print(f"\nn_cocross distribution across {len(deployed_trades)} deployed trades:")
    for k in sorted(cocross_dist):
        bar = "█" * (cocross_dist[k] // 10) if cocross_dist[k] >= 10 else ""
        print(f"  n_cocross={k:>2}  count={cocross_dist[k]:>4}  {bar}")
    above_K = sum(c for k, c in cocross_dist.items() if k >= K_THRESHOLD)
    print(f"\n  n_cocross ≥ {K_THRESHOLD}:  {above_K}/{len(deployed_trades)} ({above_K/len(deployed_trades)*100:.1f}%)")

    # Aggregate filtered vs unfiltered.
    unfiltered = deployed_trades
    filtered = [t for t in deployed_trades if t.n_cocross >= K_THRESHOLD]
    excluded = [t for t in deployed_trades if t.n_cocross < K_THRESHOLD]

    print()
    print("=" * 80)
    print("AGGREGATE — 5y full sample")
    print("=" * 80)
    u, f, e = aggregate(unfiltered), aggregate(filtered), aggregate(excluded)
    SPAN = 5.28
    fmt = lambda d: (f"n={d['n']:>4}  wins={d['wins']:>4}  WR={d['wr']:>5.1f}%  "
                     f"NET=${d['net']:>+12,.0f}  NET/trade=${d['net_per_trade']:>+8,.0f}  "
                     f"annual=${d['net']/SPAN:>+11,.0f}/yr")
    print(f"  unfiltered:  {fmt(u)}")
    print(f"  filtered:    {fmt(f)}")
    print(f"  excluded:    {fmt(e)}")
    print()
    if u['net'] != 0:
        print(f"  filtered NET as % of unfiltered:        {f['net']/u['net']*100:.1f}%")
    if u['net_per_trade'] != 0:
        print(f"  filtered NET/trade as × of unfiltered:  {f['net_per_trade']/u['net_per_trade']:.2f}×")
    print(f"  WR delta (filtered − unfiltered):       {f['wr']-u['wr']:+.2f}pp")

    # Per-window aggregate.
    print()
    print("=" * 80)
    print("PER-WINDOW (walk-forward)")
    print("=" * 80)
    print(f"  {'win':<5}  {'range':<24}  {'unf NET $':>13}  {'flt NET $':>13}  "
          f"{'unf $/tr':>10}  {'flt $/tr':>10}  {'WR Δ pp':>9}")
    print(f"  {'-'*5}  {'-'*24}  {'-'*13}  {'-'*13}  {'-'*10}  {'-'*10}  {'-'*9}")
    per_window = []
    for label, s_iso, e_iso in WINDOWS:
        u_w = [t for t in unfiltered if in_window(t, s_iso, e_iso)]
        f_w = [t for t in filtered if in_window(t, s_iso, e_iso)]
        ua, fa = aggregate(u_w), aggregate(f_w)
        wr_delta = fa['wr'] - ua['wr']
        per_window.append({
            "label": label, "range": f"{s_iso[:7]} → {e_iso[:7]}",
            "u": ua, "f": fa, "wr_delta": wr_delta,
        })
        print(f"  {label:<5}  {s_iso[:7]} → {e_iso[:7]}    "
              f"${ua['net']:>+12,.0f}  ${fa['net']:>+12,.0f}  "
              f"${ua['net_per_trade']:>+9,.0f}  ${fa['net_per_trade']:>+9,.0f}  "
              f"{wr_delta:>+8.1f}")

    # Decision rule.
    print()
    print("=" * 80)
    print("DECISION RULE (locked, applied mechanically)")
    print("=" * 80)
    n_filtered_beats = sum(1 for w in per_window if w['f']['net'] >= w['u']['net'])
    n_filtered_per_trade_beats = sum(1 for w in per_window if w['f']['net_per_trade'] > w['u']['net_per_trade'])
    n_filtered_pos = sum(1 for w in per_window if w['f']['net'] > 0)
    wr_delta_agg = f['wr'] - u['wr']

    deploy_cond_a = f['net'] >= u['net']
    deploy_cond_b = n_filtered_beats >= 5
    deploy_cond_c = f['net_per_trade'] > u['net_per_trade'] if u['net_per_trade'] != 0 else False

    shadow_cond_a = (f['net_per_trade'] >= 1.3 * u['net_per_trade']) if u['net_per_trade'] > 0 else False
    shadow_cond_b = (f['net'] >= 0.6 * u['net']) if u['net'] > 0 else False
    shadow_cond_c = n_filtered_per_trade_beats >= 4

    candidate_cond_a = wr_delta_agg >= 2.0
    candidate_cond_b = n_filtered_pos >= 3

    print("  DEPLOY-CANDIDATE conditions:")
    print(f"    (a) filtered NET ≥ unfiltered:           {'✓' if deploy_cond_a else '✗'}  "
          f"(${f['net']:+,.0f} vs ${u['net']:+,.0f})")
    print(f"    (b) ≥5/6 windows filtered ≥ unfiltered:  {'✓' if deploy_cond_b else '✗'}  "
          f"({n_filtered_beats}/6)")
    print(f"    (c) filtered NET/trade > unfiltered:     {'✓' if deploy_cond_c else '✗'}  "
          f"(${f['net_per_trade']:+,.0f} vs ${u['net_per_trade']:+,.0f})")
    print("  SHADOW DEPLOY conditions:")
    print(f"    (a) filtered NET/trade ≥ 1.3× unfiltered:  {'✓' if shadow_cond_a else '✗'}  "
          f"(ratio = {f['net_per_trade']/u['net_per_trade']:.2f}×)" if u['net_per_trade'] != 0 else "    (a) N/A")
    print(f"    (b) filtered total ≥ 60% of unfiltered:    {'✓' if shadow_cond_b else '✗'}  "
          f"({f['net']/u['net']*100:.1f}%)" if u['net'] != 0 else "    (b) N/A")
    print(f"    (c) ≥4/6 windows filtered $/tr beats unf:  {'✓' if shadow_cond_c else '✗'}  "
          f"({n_filtered_per_trade_beats}/6)")
    print("  WALK-FORWARD CANDIDATE conditions:")
    print(f"    (a) WR delta ≥ +2pp aggregate:           {'✓' if candidate_cond_a else '✗'}  "
          f"({wr_delta_agg:+.2f}pp)")
    print(f"    (b) positive NET in ≥3/6 windows:        {'✓' if candidate_cond_b else '✗'}  "
          f"({n_filtered_pos}/6)")

    # Apply tier in order.
    if deploy_cond_a and deploy_cond_b and deploy_cond_c:
        verdict = "DEPLOY-CANDIDATE"
    elif shadow_cond_a and shadow_cond_b and shadow_cond_c:
        verdict = "SHADOW DEPLOY"
    elif candidate_cond_a and candidate_cond_b:
        verdict = "WALK-FORWARD CANDIDATE"
    else:
        verdict = "REJECT"

    print()
    print(f"  → VERDICT: {verdict}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
