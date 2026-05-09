#!/usr/bin/env python3
"""cat_g_prime_universe57.py — apply the locked decision rule from
`results/cat_g_prime_universe57_decision_rule_2026-05-08.md` to the
deployed-16 trade pool, with the F2 co-cross denominator expanded to
universe-57.

Composite filter (design D, identical to Cat G except F2 denominator):
  F1 condition: 8h funding × 3 × 10000 > +30 bp/day
  F2 condition: ≥5 OTHER universe-57 symbols had a same-side cross
                within ±60s

Output: walk-forward 6-window NET (filtered vs unfiltered),
mechanical verdict per locked decision tiers.
"""

from __future__ import annotations

import csv
import json
import sys
from datetime import datetime, timedelta, timezone
from pathlib import Path

# ── Locked parameters ────────────────────────────────────────────────────────
F1_THRESHOLD_BPS_PER_DAY = 30.0
F2_K_THRESHOLD = 5
F2_TIME_TOLERANCE_S = 60

ROOT = Path(__file__).resolve().parent.parent
DEPLOYED_JOURNAL_DIR = ROOT / "results" / "hod_journals" / "2026-05-07"
UNIVERSE_JOURNAL_DIR = ROOT / "results" / "hod_journals" / "2026-05-07-univ57"
FUNDING_DIR = ROOT / "data" / "funding"

DEPLOYED_16 = [
    "ROSEUSDT", "BCHUSDT", "GRTUSDT", "1INCHUSDT", "ADAUSDT", "KAVAUSDT",
    "1000SHIBUSDT", "ENSUSDT", "XLMUSDT", "IMXUSDT", "ETCUSDT", "RUNEUSDT",
    "AVAXUSDT", "APTUSDT", "DOTUSDT", "FILUSDT",
]

WINDOWS = [
    ("W-2", datetime(2020, 5, 1, tzinfo=timezone.utc),  datetime(2021, 4, 30, tzinfo=timezone.utc)),
    ("W-1", datetime(2021, 5, 1, tzinfo=timezone.utc),  datetime(2022, 4, 30, tzinfo=timezone.utc)),
    ("W0",  datetime(2022, 5, 1, tzinfo=timezone.utc),  datetime(2023, 4, 30, tzinfo=timezone.utc)),
    ("W1",  datetime(2023, 5, 1, tzinfo=timezone.utc),  datetime(2024, 4, 30, tzinfo=timezone.utc)),
    ("W2",  datetime(2024, 5, 1, tzinfo=timezone.utc),  datetime(2025, 4, 30, tzinfo=timezone.utc)),
    ("W3",  datetime(2025, 5, 1, tzinfo=timezone.utc),  datetime(2026, 4, 30, tzinfo=timezone.utc)),
]


def load_trades(journal_dir: Path) -> list[dict]:
    """Pair open/close events; return all trades with full attributes."""
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
    if not history or ts < history[0][0]:
        return None
    lo, hi = 0, len(history) - 1
    while lo < hi:
        mid = (lo + hi + 1) // 2
        if history[mid][0] <= ts:
            lo = mid
        else:
            hi = mid - 1
    return history[lo][1]


def funding_per_day_bps_at_signal(history: list[tuple[datetime, float]], ts: datetime) -> float | None:
    rate = funding_at(history, ts)
    if rate is None:
        return None
    return rate * 3 * 10000


def cocross_count_universe(target: dict, universe_trades: list[dict],
                            tolerance_s: int) -> int:
    """Count OTHER universe-57 trades with same side within ±tolerance_s."""
    target_ts = target["ts"]
    target_side = target["side"]
    target_symbol = target["symbol"]
    lo_ts = target_ts - timedelta(seconds=tolerance_s)
    hi_ts = target_ts + timedelta(seconds=tolerance_s)
    n = 0
    for u in universe_trades:
        if u["symbol"] == target_symbol:
            continue
        if u["side"] != target_side:
            continue
        if lo_ts <= u["ts"] <= hi_ts:
            n += 1
    return n


def fmt_dollar(x: float) -> str:
    sign = "-" if x < 0 else "+"
    return f"{sign}${abs(x):,.0f}"


def main() -> int:
    print("CAT G' — F1 ∧ F2 COMPOSITE, UNIVERSE-57 DENOMINATOR (locked)")
    print("=" * 80)
    print("Pre-reg:    cat_g_prime_universe57_decision_rule_2026-05-08.md")
    print(f"F1 thresh:  > {F1_THRESHOLD_BPS_PER_DAY} bp/day funding (locked)")
    print(f"F2 K:       ≥ {F2_K_THRESHOLD} other universe-57 same-side crosses (locked)")
    print(f"F2 tol:     ±{F2_TIME_TOLERANCE_S} s (locked)")
    print("Trade pool: deployed-16 (locked)")
    print("F2 univ:    universe-57 (NEW: only change vs Cat G)")
    print()

    print("Loading deployed-16 trades...")
    deployed_trades = load_trades(DEPLOYED_JOURNAL_DIR)
    print(f"  {len(deployed_trades):,} deployed trades")

    print("Loading universe-57 trades (for F2 co-cross denominator)...")
    universe_trades = load_trades(UNIVERSE_JOURNAL_DIR)
    print(f"  {len(universe_trades):,} universe trades")

    print("Loading funding histories...")
    funding: dict[str, list[tuple[datetime, float]]] = {}
    for sym in DEPLOYED_16:
        funding[sym] = load_funding_history(sym)
    n_with = sum(1 for h in funding.values() if h)
    print(f"  funding loaded for {n_with}/{len(DEPLOYED_16)} deployed symbols")
    print()

    print("Computing F1 and F2 tags per deployed trade...")
    for t in deployed_trades:
        bps = funding_per_day_bps_at_signal(funding.get(t["symbol"], []), t["ts"])
        t["funding_bps"] = bps
        if bps is None:
            t["f1_pass"] = False
        elif t["side"] == "SHORT":
            t["f1_pass"] = bps > F1_THRESHOLD_BPS_PER_DAY
        else:
            t["f1_pass"] = bps < -F1_THRESHOLD_BPS_PER_DAY
        t["cocross"] = cocross_count_universe(t, universe_trades, F2_TIME_TOLERANCE_S)

    f1_eligible = sum(1 for t in deployed_trades if t["f1_pass"])
    f2_eligible = sum(1 for t in deployed_trades if t["cocross"] >= F2_K_THRESHOLD)
    composite = [t for t in deployed_trades if t["f1_pass"] and t["cocross"] >= F2_K_THRESHOLD]
    print(f"  F1-pass:                 {f1_eligible:>4}/{len(deployed_trades)} ({f1_eligible/len(deployed_trades)*100:.1f}%)")
    print(f"  F2-pass (univ-57 K=5):   {f2_eligible:>4}/{len(deployed_trades)} ({f2_eligible/len(deployed_trades)*100:.1f}%)")
    print(f"  COMPOSITE F1 ∧ F2:       {len(composite):>4}/{len(deployed_trades)} ({len(composite)/len(deployed_trades)*100:.2f}%)")
    print()

    print("Per-window walk-forward aggregates:")
    print()
    print(f"  {'Window':<6} {'Range':<23} {'unf-N':>6} {'unf-NET':>14} "
          f"{'flt-N':>6} {'flt-NET':>14} {'flt-NET/tr':>10} "
          f"{'Δ-WR':>7} {'flt≥unf':>7}")
    print(f"  {'-'*6} {'-'*23} {'-'*6} {'-'*14} {'-'*6} {'-'*14} {'-'*10} {'-'*7} {'-'*7}")

    window_results = []
    for label, start, end in WINDOWS:
        unf = [t for t in deployed_trades if start <= t["ts"] < end]
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
            "label": label, "unf_n": unf_n, "unf_pnl": unf_pnl, "unf_npt": unf_npt,
            "flt_n": flt_n, "flt_pnl": flt_pnl, "flt_npt": flt_npt,
            "wr_delta": wr_delta, "positive": positive,
            "flt_ge_unf": flt_pnl >= unf_pnl,
        })
        print(f"  {label:<6} {start.date()}→{end.date()} {unf_n:>6} {fmt_dollar(unf_pnl):>14} "
              f"{flt_n:>6} {fmt_dollar(flt_pnl):>14} {fmt_dollar(flt_npt):>10} "
              f"{wr_delta:>+6.1f}p {flt_ge_unf:>7}")
    print()

    valid = [w for w in window_results if w["unf_n"] > 10]
    n_pos = sum(1 for w in valid if w["positive"])
    flt_total = sum(w["flt_pnl"] for w in valid)
    unf_total = sum(w["unf_pnl"] for w in valid)
    flt_n_total = sum(w["flt_n"] for w in valid)
    unf_n_total = sum(w["unf_n"] for w in valid)
    mean_annual_flt = flt_total / len(valid) if valid else 0
    flt_npt_total = flt_total / flt_n_total if flt_n_total > 0 else 0
    unf_npt_total = unf_total / unf_n_total if unf_n_total > 0 else 0
    flt_ge_unf_count = sum(1 for w in valid if w["flt_ge_unf"])

    print("Aggregate (across valid windows):")
    print(f"  Valid windows                   : {len(valid)} of {len(WINDOWS)}")
    print(f"  Positive flt windows            : {n_pos}/{len(valid)}")
    print(f"  Filtered total NET              : {fmt_dollar(flt_total)}")
    print(f"  Unfiltered total NET            : {fmt_dollar(unf_total)}")
    print(f"  Mean annual flt NET             : {fmt_dollar(mean_annual_flt)}/yr")
    print(f"  Filtered NET/trade              : {fmt_dollar(flt_npt_total)}")
    print(f"  Unfiltered NET/trade            : {fmt_dollar(unf_npt_total)}")
    print(f"  flt-NET-per-trade vs unf        : {flt_npt_total / unf_npt_total if unf_npt_total else 0:.2f}×")
    print(f"  Windows where flt-NET ≥ unf-NET : {flt_ge_unf_count}/{len(valid)}")
    print()

    print("=" * 80)
    print("DECISION RULE EVALUATION (locked, same tiers as Cat G):")
    print()

    dc_a = n_pos >= 5
    dc_b = mean_annual_flt >= 50_000
    dc_c = flt_total >= unf_total
    print(f"  DEPLOY-CANDIDATE      ≥5/6 wins: {n_pos}/6 {'✓' if dc_a else '✗'} | "
          f"mean ≥ $50k/yr: {fmt_dollar(mean_annual_flt)} {'✓' if dc_b else '✗'} | "
          f"flt ≥ unf: {fmt_dollar(flt_total)} vs {fmt_dollar(unf_total)} {'✓' if dc_c else '✗'}")
    is_deploy = dc_a and dc_b and dc_c

    sd_a = n_pos >= 4
    sd_b = mean_annual_flt >= 30_000
    sd_c = (flt_npt_total / unf_npt_total) >= 1.5 if unf_npt_total > 0 else False
    print(f"  SHADOW DEPLOY         ≥4/6 wins: {n_pos}/6 {'✓' if sd_a else '✗'} | "
          f"mean ≥ $30k/yr: {fmt_dollar(mean_annual_flt)} {'✓' if sd_b else '✗'} | "
          f"flt $/tr ≥ 1.5× unf: {flt_npt_total/unf_npt_total if unf_npt_total else 0:.2f}× {'✓' if sd_c else '✗'}")
    is_shadow = sd_a and sd_b and sd_c

    wf_a = n_pos >= 3
    wf_b = mean_annual_flt > 0
    wf_c = flt_npt_total > unf_npt_total
    print(f"  WALK-FORWARD CAND.    ≥3/6 wins: {n_pos}/6 {'✓' if wf_a else '✗'} | "
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
