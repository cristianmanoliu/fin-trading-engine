#!/usr/bin/env python3
"""cat_g_f1xf2_composite.py — apply the locked decision rule from
`results/cat_g_f1xf2_composite_decision_rule_2026-05-07.md` to the deployed-16
trade pool.

Composite design D: filter deployed signals to keep only those where BOTH
hold at the candle close:
  F1 condition: 8h funding × 3 × 10000 > +30 bp/day (overcrowded longs)
  F2 condition: ≥5 OTHER deployed-16 symbols had a same-direction cross
                within ±60 seconds

Output: walk-forward 6-window per-window NET (filtered vs unfiltered),
aggregate counts, and mechanical verdict per locked decision tiers.
"""

from __future__ import annotations

import csv
import json
import sys
from datetime import datetime, timedelta, timezone
from pathlib import Path

# ── Locked parameters from the pre-registration ──────────────────────────────
F1_THRESHOLD_BPS_PER_DAY = 30.0   # locked from cat_f1
F2_K_THRESHOLD = 5                # locked from cat_f2
F2_TIME_TOLERANCE_S = 60          # locked from cat_f2
DEPLOYED_NOTIONAL = "deployed-16" # locked

ROOT = Path(__file__).resolve().parent.parent
JOURNAL_DIR = ROOT / "results" / "hod_journals" / "2026-05-07"
FUNDING_DIR = ROOT / "data" / "funding"

DEPLOYED_16 = [
    "ROSEUSDT", "BCHUSDT", "GRTUSDT", "1INCHUSDT", "ADAUSDT", "KAVAUSDT",
    "1000SHIBUSDT", "ENSUSDT", "XLMUSDT", "IMXUSDT", "ETCUSDT", "RUNEUSDT",
    "AVAXUSDT", "APTUSDT", "DOTUSDT", "FILUSDT",
]

# Walk-forward windows (locked from cat_f1/cat_f2)
WINDOWS = [
    ("W-2", datetime(2020, 5, 1, tzinfo=timezone.utc),  datetime(2021, 4, 30, tzinfo=timezone.utc)),
    ("W-1", datetime(2021, 5, 1, tzinfo=timezone.utc),  datetime(2022, 4, 30, tzinfo=timezone.utc)),
    ("W0",  datetime(2022, 5, 1, tzinfo=timezone.utc),  datetime(2023, 4, 30, tzinfo=timezone.utc)),
    ("W1",  datetime(2023, 5, 1, tzinfo=timezone.utc),  datetime(2024, 4, 30, tzinfo=timezone.utc)),
    ("W2",  datetime(2024, 5, 1, tzinfo=timezone.utc),  datetime(2025, 4, 30, tzinfo=timezone.utc)),
    ("W3",  datetime(2025, 5, 1, tzinfo=timezone.utc),  datetime(2026, 4, 30, tzinfo=timezone.utc)),
]


def load_trades(journal_dir: Path) -> list[dict]:
    """Pair open/close events; return list of trades with full attributes."""
    out: list[dict] = []
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
                # Skip PARTIAL — Cat G is on terminal closes only (matches F1/F2/A1).
                if ev.get("outcome") == "PARTIAL":
                    open_ev = None
                    continue
                ts = datetime.fromisoformat(open_ev["ts"].replace("Z", "+00:00"))
                out.append({
                    "ts": ts.astimezone(timezone.utc),
                    "symbol": symbol,
                    "side": open_ev["side"],
                    "entry": open_ev["entry"],
                    "pnl_usd": ev.get("pnl_usd", 0.0),
                    "outcome": ev.get("outcome", "?"),
                })
                open_ev = None
    out.sort(key=lambda t: t["ts"])
    return out


def load_funding_history(symbol: str) -> list[tuple[datetime, float]]:
    """Load funding history as sorted (ts, rate) list. Rate is in decimal
    (e.g., 0.0001 = 1bp per 8h interval)."""
    path = FUNDING_DIR / f"{symbol}.csv"
    if not path.exists():
        return []
    out: list[tuple[datetime, float]] = []
    with path.open() as f:
        reader = csv.DictReader(f)
        for row in reader:
            ts_ms = int(row["funding_time_ms"])
            rate = float(row["funding_rate"].strip('"'))
            ts = datetime.fromtimestamp(ts_ms / 1000, timezone.utc)
            out.append((ts, rate))
    out.sort(key=lambda x: x[0])
    return out


def funding_at(history: list[tuple[datetime, float]], ts: datetime) -> float | None:
    """Return funding rate from the most recent funding interval at or before ts.
    Binance: 8h intervals at 00/08/16 UTC. We just look up the latest <= ts."""
    if not history:
        return None
    # Binary search.
    lo, hi = 0, len(history) - 1
    if ts < history[0][0]:
        return None
    while lo < hi:
        mid = (lo + hi + 1) // 2
        if history[mid][0] <= ts:
            lo = mid
        else:
            hi = mid - 1
    return history[lo][1]


def funding_per_day_bps_at_signal(history: list[tuple[datetime, float]], ts: datetime) -> float | None:
    """Convert funding rate (decimal per 8h) to bps/day: rate × 3 × 10000."""
    rate = funding_at(history, ts)
    if rate is None:
        return None
    return rate * 3 * 10000


def cocross_count(trades: list[dict], target: dict, tolerance_s: int) -> int:
    """Count OTHER deployed-16 trades with same side within ±tolerance_s of target."""
    target_ts = target["ts"]
    target_side = target["side"]
    target_symbol = target["symbol"]
    lo_ts = target_ts - timedelta(seconds=tolerance_s)
    hi_ts = target_ts + timedelta(seconds=tolerance_s)
    # Trades is sorted by ts; we could binary-search, but cost is cheap at n=2210.
    n = 0
    for t in trades:
        if t["symbol"] == target_symbol:
            continue
        if t["side"] != target_side:
            continue
        if lo_ts <= t["ts"] <= hi_ts:
            n += 1
    return n


def fmt_dollar(x: float) -> str:
    sign = "-" if x < 0 else "+"
    return f"{sign}${abs(x):,.0f}"


def main() -> int:
    print("CAT G — F1 ∧ F2 COMPOSITE FILTER (composite design D, locked)")
    print("=" * 80)
    print(f"Pre-reg:    cat_g_f1xf2_composite_decision_rule_2026-05-07.md")
    print(f"F1 thresh:  > {F1_THRESHOLD_BPS_PER_DAY} bp/day funding")
    print(f"F2 K:       ≥ {F2_K_THRESHOLD} other deployed-16 same-side crosses")
    print(f"F2 tol:     ±{F2_TIME_TOLERANCE_S} s")
    print(f"Universe:   {DEPLOYED_NOTIONAL} ({len(DEPLOYED_16)} symbols)")
    print()

    # 1. Load all deployed trades.
    print("Loading deployed-16 trades...")
    trades = load_trades(JOURNAL_DIR)
    print(f"  {len(trades):,} trades loaded ({trades[0]['ts'].date()} → {trades[-1]['ts'].date()})")

    # 2. Load funding histories.
    print("Loading funding histories...")
    funding: dict[str, list[tuple[datetime, float]]] = {}
    for sym in DEPLOYED_16:
        h = load_funding_history(sym)
        funding[sym] = h
        if not h:
            print(f"  WARNING: no funding data for {sym}")
    print(f"  funding loaded for {sum(1 for h in funding.values() if h)}/{len(DEPLOYED_16)} symbols")
    print()

    # 3. Tag every trade with f1_pass and f2_count.
    print("Computing F1 and F2 tags per trade...")
    f1_eligible = 0
    no_funding = 0
    for t in trades:
        sym = t["symbol"]
        bps = funding_per_day_bps_at_signal(funding.get(sym, []), t["ts"])
        if bps is None:
            t["f1_pass"] = False
            t["funding_bps"] = None
            no_funding += 1
            continue
        t["funding_bps"] = bps
        # Deployed strategy is SHORT-only — F1 SHORT condition: funding > +30bp/day.
        if t["side"] == "SHORT":
            t["f1_pass"] = bps > F1_THRESHOLD_BPS_PER_DAY
        else:
            t["f1_pass"] = bps < -F1_THRESHOLD_BPS_PER_DAY
        if t["f1_pass"]:
            f1_eligible += 1

    if no_funding > 0:
        print(f"  WARNING: {no_funding} trades had no matching funding history")
    print(f"  F1-pass trades: {f1_eligible}/{len(trades)} ({f1_eligible/len(trades)*100:.1f}%)")

    print("Computing F2 co-cross counts...")
    for t in trades:
        t["cocross"] = cocross_count(trades, t, F2_TIME_TOLERANCE_S)
    f2_eligible = sum(1 for t in trades if t["cocross"] >= F2_K_THRESHOLD)
    print(f"  F2-pass trades (co-cross ≥ {F2_K_THRESHOLD}): {f2_eligible}/{len(trades)} ({f2_eligible/len(trades)*100:.1f}%)")

    # 4. Composite filter.
    composite = [t for t in trades if t.get("f1_pass") and t["cocross"] >= F2_K_THRESHOLD]
    print(f"  COMPOSITE F1 ∧ F2: {len(composite)}/{len(trades)} ({len(composite)/len(trades)*100:.2f}%)")
    print()

    # 5. Per-window aggregates.
    print("Per-window walk-forward aggregates:")
    print()
    print(f"  {'Window':<6} {'Range':<23} {'unf-N':>6} {'unf-NET':>14} "
          f"{'flt-N':>6} {'flt-NET':>14} {'flt-NET/tr':>10} "
          f"{'Δ-WR':>6} {'flt≥unf':>7}")
    print(f"  {'-'*6} {'-'*23} {'-'*6} {'-'*14} {'-'*6} {'-'*14} {'-'*10} {'-'*6} {'-'*7}")

    window_results = []
    for label, start, end in WINDOWS:
        unf = [t for t in trades if start <= t["ts"] < end]
        flt = [t for t in composite if start <= t["ts"] < end]
        unf_n = len(unf)
        flt_n = len(flt)
        unf_pnl = sum(t["pnl_usd"] for t in unf)
        flt_pnl = sum(t["pnl_usd"] for t in flt)
        unf_npt = unf_pnl / unf_n if unf_n > 0 else 0
        flt_npt = flt_pnl / flt_n if flt_n > 0 else 0
        unf_wins = sum(1 for t in unf if t["outcome"] == "TARGET")
        flt_wins = sum(1 for t in flt if t["outcome"] == "TARGET")
        unf_wr = unf_wins / unf_n * 100 if unf_n > 0 else 0
        flt_wr = flt_wins / flt_n * 100 if flt_n > 0 else 0
        wr_delta = flt_wr - unf_wr
        flt_ge_unf = "✓" if flt_pnl >= unf_pnl else "✗"
        positive = flt_pnl > 0
        window_results.append({
            "label": label, "start": start, "end": end,
            "unf_n": unf_n, "unf_pnl": unf_pnl, "unf_npt": unf_npt,
            "flt_n": flt_n, "flt_pnl": flt_pnl, "flt_npt": flt_npt,
            "wr_delta": wr_delta, "positive": positive,
            "flt_ge_unf": flt_pnl >= unf_pnl,
        })
        print(f"  {label:<6} {start.date()}→{end.date()} {unf_n:>6} {fmt_dollar(unf_pnl):>14} "
              f"{flt_n:>6} {fmt_dollar(flt_pnl):>14} {fmt_dollar(flt_npt):>10} "
              f"{wr_delta:>+5.1f}p {flt_ge_unf:>7}")

    print()

    # 6. Aggregate stats.
    valid_windows = [w for w in window_results if w["unf_n"] > 10]  # exclude empty W3
    n_pos_windows = sum(1 for w in valid_windows if w["positive"])
    flt_total = sum(w["flt_pnl"] for w in valid_windows)
    unf_total = sum(w["unf_pnl"] for w in valid_windows)
    flt_n_total = sum(w["flt_n"] for w in valid_windows)
    unf_n_total = sum(w["unf_n"] for w in valid_windows)
    mean_annual_flt = flt_total / len(valid_windows) if valid_windows else 0
    flt_npt_total = flt_total / flt_n_total if flt_n_total > 0 else 0
    unf_npt_total = unf_total / unf_n_total if unf_n_total > 0 else 0
    flt_ge_unf_count = sum(1 for w in valid_windows if w["flt_ge_unf"])

    print("Aggregate (across valid windows):")
    print(f"  Valid windows                 : {len(valid_windows)} of {len(WINDOWS)}")
    print(f"  Positive flt windows          : {n_pos_windows}/{len(valid_windows)}")
    print(f"  Filtered total NET            : {fmt_dollar(flt_total)}")
    print(f"  Unfiltered total NET          : {fmt_dollar(unf_total)}")
    print(f"  Mean annual flt NET           : {fmt_dollar(mean_annual_flt)}/yr")
    print(f"  Filtered NET/trade            : {fmt_dollar(flt_npt_total)}")
    print(f"  Unfiltered NET/trade          : {fmt_dollar(unf_npt_total)}")
    print(f"  flt-NET-per-trade vs unf      : {flt_npt_total / unf_npt_total if unf_npt_total else 0:.2f}×")
    print(f"  Windows where flt-NET ≥ unf-NET: {flt_ge_unf_count}/{len(valid_windows)}")
    print()

    # 7. Apply locked decision rule.
    print("=" * 80)
    print("DECISION RULE EVALUATION (locked):")
    print()

    # DEPLOY-CANDIDATE
    dc_a = n_pos_windows >= 5
    dc_b = mean_annual_flt >= 50_000
    dc_c = flt_total >= unf_total
    print(f"  DEPLOY-CANDIDATE      ≥5/6 wins: {n_pos_windows}/6 {'✓' if dc_a else '✗'} | "
          f"mean ≥ $50k/yr: {fmt_dollar(mean_annual_flt)} {'✓' if dc_b else '✗'} | "
          f"flt ≥ unf: {fmt_dollar(flt_total)} vs {fmt_dollar(unf_total)} {'✓' if dc_c else '✗'}")
    is_deploy = dc_a and dc_b and dc_c

    # SHADOW DEPLOY
    sd_a = n_pos_windows >= 4
    sd_b = mean_annual_flt >= 30_000
    sd_c = (flt_npt_total / unf_npt_total) >= 1.5 if unf_npt_total > 0 else False
    print(f"  SHADOW DEPLOY         ≥4/6 wins: {n_pos_windows}/6 {'✓' if sd_a else '✗'} | "
          f"mean ≥ $30k/yr: {fmt_dollar(mean_annual_flt)} {'✓' if sd_b else '✗'} | "
          f"flt $/tr ≥ 1.5× unf: {flt_npt_total/unf_npt_total if unf_npt_total else 0:.2f}× {'✓' if sd_c else '✗'}")
    is_shadow = sd_a and sd_b and sd_c

    # WALK-FORWARD CANDIDATE
    wf_a = n_pos_windows >= 3
    wf_b = mean_annual_flt > 0
    wf_c = flt_npt_total > unf_npt_total
    print(f"  WALK-FORWARD CAND.    ≥3/6 wins: {n_pos_windows}/6 {'✓' if wf_a else '✗'} | "
          f"mean > 0: {fmt_dollar(mean_annual_flt)} {'✓' if wf_b else '✗'} | "
          f"flt $/tr > unf $/tr: {fmt_dollar(flt_npt_total)} > {fmt_dollar(unf_npt_total)} {'✓' if wf_c else '✗'}")
    is_wfc = wf_a and wf_b and wf_c

    if is_deploy:
        verdict = "DEPLOY-CANDIDATE"
    elif is_shadow:
        verdict = "SHADOW DEPLOY"
    elif is_wfc:
        verdict = "WALK-FORWARD CANDIDATE"
    else:
        verdict = "REJECT"

    print()
    print("=" * 80)
    print(f"VERDICT: {verdict}")
    print("=" * 80)

    return 0


if __name__ == "__main__":
    sys.exit(main())
