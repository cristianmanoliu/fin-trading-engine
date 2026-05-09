#!/usr/bin/env python3
"""kill_bar_calibration.py — Monte Carlo calibration of the forward-paper
kill bar against the strategy's natural variance.

Pre-registered rule: `results/kill_bar_calibration_decision_rule_2026-05-07.md`.

Method: contiguous-window bootstrap. Each path = randomly chosen N-day window
from the historical 5y dataset. Apply scenario's PnL transform, compute the
kill-bar criteria, count which fire. Repeat B=1000 paths per scenario × horizon.

Scenarios: null (×1.0) / -30% (×0.7) / -50% (×0.5) / dead (mean shifted to 0).
Horizons: 60d, 90d, 120d, 180d.

Criteria evaluated:
  KILL_PNL    : Net PnL < 0 at horizon
  KILL_WR     : WR < 14% at horizon (only fires when ≥150 trades)
  KILL_SYM    : single-symbol pct > 40% of |total NET|
  KILL_HODL_C : cumulative HODL delta < $0
  KILL_HODL_W : two consecutive 30d windows underperform HODL by >$5k each

Criteria NOT evaluated (require fee/slip noise model, not in scope):
  KILL_FEE, KILL_SLIP — these only fire on realized-vs-modeled divergence,
  which is identical-by-construction in this simulation.
"""

from __future__ import annotations

import json
import random
import sys
from datetime import datetime, timedelta, timezone
from pathlib import Path

# Same-directory import for shared bootstrap helpers.
sys.path.insert(0, str(Path(__file__).resolve().parent))
from bootstrap_ci import fmt_dollar  # noqa: E402


# ── Configuration (matches kill bar in forward_paper_status.sh) ──────────────
WR_KILL_PCT = 14.0
SYM_KILL_PCT = 40.0
HODL_BENCHMARK_NOTIONAL = 32_000.0
HODL_KILL_WINDOW_USD = 5_000.0
MIN_TRADES_FOR_WR = 150  # WR criterion only fires once we have ≥150 trades

# ── Acceptable bands (from pre-registration, locked) ─────────────────────────
BAND_FP_NULL = 20.0   # FP rate at 90d under null ≤ 20%
BAND_TP_DEG50 = 50.0  # TP rate at 90d under deg50 ≥ 50%
BAND_TP_DEAD = 80.0   # TP rate at 90d under dead ≥ 80%
BORDERLINE_TOLERANCE_PP = 5.0

# ── Run-control ──────────────────────────────────────────────────────────────
B_PATHS = 1000
HORIZONS_DAYS = [60, 90, 120, 180]
RNG = random.Random(20260507)
BINANCE_KLINES_URL = "https://fapi.binance.com/fapi/v1/klines"


def load_trades(journal_dir: Path) -> list[dict]:
    """Load all close events with timestamps and per-trade attributes."""
    rows: list[dict] = []
    for jf in sorted(journal_dir.glob("*-*.jsonl")):
        open_event = None
        with jf.open() as f:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                ev = json.loads(line)
                if ev["event"] == "open":
                    open_event = ev
                elif ev["event"] == "close" and open_event is not None:
                    open_ts = datetime.fromisoformat(open_event["ts"].replace("Z", "+00:00"))
                    rows.append({
                        "ts": open_ts.astimezone(timezone.utc),
                        "symbol": open_event.get("symbol", "?"),
                        "pnl_usd": ev.get("pnl_usd", 0.0),
                        "outcome": ev.get("outcome", "STOP"),
                    })
                    open_event = None
    rows.sort(key=lambda r: r["ts"])
    return rows


def fetch_btc_daily_closes(start_dt: datetime, end_dt: datetime) -> dict:
    """Fetch daily BTCUSDT closes from Binance for the historical period."""
    import urllib.request
    out: dict = {}
    cursor = start_dt - timedelta(days=2)
    end_padded = end_dt + timedelta(days=2)
    while cursor < end_padded:
        start_ms = int(cursor.timestamp() * 1000)
        url = f"{BINANCE_KLINES_URL}?symbol=BTCUSDT&interval=1d&startTime={start_ms}&limit=1500"
        req = urllib.request.Request(url, headers={"User-Agent": "kill_bar_calibration"})
        with urllib.request.urlopen(req, timeout=30) as resp:
            data = json.loads(resp.read())
        if not data:
            break
        for k in data:
            d = datetime.fromtimestamp(k[0] / 1000, timezone.utc).date()
            out[d] = float(k[4])
        last_open_ms = data[-1][0]
        cursor = datetime.fromtimestamp(last_open_ms / 1000, timezone.utc) + timedelta(days=1)
        if len(data) < 1500:
            break
    return out


def btc_close_lookup(prices: dict, when: datetime) -> float | None:
    target = when.astimezone(timezone.utc).date()
    for delta in range(0, 8):
        cand = target - timedelta(days=delta)
        if cand in prices:
            return prices[cand]
    return None


def transform_scenario(pnls: list[float], scenario: str, global_mean: float = 0.0) -> list[float]:
    """Apply scenario transform. global_mean is the per-trade historical mean
    across the FULL dataset — used for "dead" so we shift each trade by the
    global mean (preserving variance) rather than by the window mean (which
    would zero variance per-path, the bug found in run-1)."""
    if scenario == "null":
        return list(pnls)
    if scenario == "deg30":
        return [p * 0.7 for p in pnls]
    if scenario == "deg50":
        return [p * 0.5 for p in pnls]
    if scenario == "dead":
        return [p - global_mean for p in pnls]
    raise ValueError(f"unknown scenario: {scenario}")


def evaluate_window(window_trades: list[dict], scenario: str,
                    btc_prices: dict, window_start: datetime,
                    window_end: datetime, global_mean: float = 0.0) -> dict:
    """Apply the kill bar to one simulated forward-paper window. Returns
    dict mapping criterion name → bool (True = fired)."""
    pnls = transform_scenario([t["pnl_usd"] for t in window_trades], scenario, global_mean)
    n_trades = len(pnls)
    total_pnl = sum(pnls)
    wins = sum(1 for t, p in zip(window_trades, pnls) if t["outcome"] in ("TARGET", "PARTIAL"))
    wr_pct = (wins / n_trades * 100) if n_trades > 0 else 0

    # Per-symbol aggregates
    sym_pnls: dict = {}
    for t, p in zip(window_trades, pnls):
        sym_pnls[t["symbol"]] = sym_pnls.get(t["symbol"], 0) + p
    abs_total = abs(total_pnl) if total_pnl != 0 else 1.0
    max_sym_pct = max((abs(s) / abs_total * 100 for s in sym_pnls.values()), default=0.0)

    # HODL cumulative
    p_start = btc_close_lookup(btc_prices, window_start)
    p_end = btc_close_lookup(btc_prices, window_end)
    if p_start and p_end and p_start > 0:
        hodl_total = HODL_BENCHMARK_NOTIONAL * (p_end / p_start - 1.0)
        hodl_delta = total_pnl - hodl_total
    else:
        hodl_total = 0
        hodl_delta = 0

    # HODL rolling-30d windows
    cursor = window_start
    win_size = timedelta(days=30)
    monthly_deltas: list[float] = []
    while cursor + win_size <= window_end:
        win_end = cursor + win_size
        win_pnl = sum(p for t, p in zip(window_trades, pnls)
                      if cursor <= t["ts"] < win_end)
        ps = btc_close_lookup(btc_prices, cursor)
        pe = btc_close_lookup(btc_prices, win_end)
        if ps and pe and ps > 0:
            win_hodl = HODL_BENCHMARK_NOTIONAL * (pe / ps - 1.0)
            monthly_deltas.append(win_pnl - win_hodl)
        cursor = win_end
    # Two-consecutive-windows kill: any pair of adjacent monthly deltas both < -threshold
    hodl_window_kill = False
    for i in range(len(monthly_deltas) - 1):
        if (monthly_deltas[i] < -HODL_KILL_WINDOW_USD and
                monthly_deltas[i + 1] < -HODL_KILL_WINDOW_USD):
            hodl_window_kill = True
            break

    return {
        "KILL_PNL": total_pnl < 0,
        "KILL_WR": (n_trades >= MIN_TRADES_FOR_WR) and (wr_pct < WR_KILL_PCT),
        "KILL_SYM": max_sym_pct > SYM_KILL_PCT,
        "KILL_HODL_C": hodl_delta < 0,
        "KILL_HODL_W": hodl_window_kill,
        "n_trades": n_trades,
        "total_pnl": total_pnl,
        "wr_pct": wr_pct,
        "max_sym_pct": max_sym_pct,
        "hodl_delta": hodl_delta,
        "n_monthly": len(monthly_deltas),
    }


def run_scenario(trades: list[dict], scenario: str, horizon_days: int,
                 btc_prices: dict, B: int, global_mean: float = 0.0) -> dict:
    """Run B Monte Carlo paths for a scenario × horizon. Returns fire rates."""
    if not trades:
        return {}
    earliest = trades[0]["ts"]
    latest = trades[-1]["ts"]
    span_days = (latest - earliest).total_seconds() / 86400.0
    max_start_offset = span_days - horizon_days
    if max_start_offset <= 0:
        return {}

    fires = {k: 0 for k in ("KILL_PNL", "KILL_WR", "KILL_SYM", "KILL_HODL_C", "KILL_HODL_W")}
    sum_n_trades = 0
    sum_total_pnl = 0.0
    sum_wr_when_eligible = 0.0
    n_wr_eligible = 0

    for _ in range(B):
        # Random window start (wrapped to integer days for stability)
        offset_days = RNG.uniform(0, max_start_offset)
        win_start = earliest + timedelta(days=offset_days)
        win_end = win_start + timedelta(days=horizon_days)
        window_trades = [t for t in trades if win_start <= t["ts"] < win_end]
        if not window_trades:
            continue
        result = evaluate_window(window_trades, scenario, btc_prices, win_start, win_end, global_mean)
        for k in fires:
            if result[k]:
                fires[k] += 1
        sum_n_trades += result["n_trades"]
        sum_total_pnl += result["total_pnl"]
        if result["n_trades"] >= MIN_TRADES_FOR_WR:
            sum_wr_when_eligible += result["wr_pct"]
            n_wr_eligible += 1

    return {
        "B": B,
        "fires": fires,
        "fire_rate_pct": {k: v / B * 100 for k, v in fires.items()},
        "mean_n_trades": sum_n_trades / B,
        "mean_total_pnl": sum_total_pnl / B,
        "wr_eligibility_pct": n_wr_eligible / B * 100,
        "mean_wr_when_eligible": sum_wr_when_eligible / n_wr_eligible if n_wr_eligible else 0,
    }


def main() -> int:
    args = sys.argv[1:]
    if args and Path(args[0]).is_dir():
        journal_dir = Path(args[0])
    else:
        journal_dir = Path(__file__).resolve().parent.parent / "results" / "hod_journals" / "2026-05-07"

    print("KILL-BAR CALIBRATION — kill_bar_calibration_decision_rule_2026-05-07.md")
    print("=" * 80)
    print(f"journal dir: {journal_dir}")
    print(f"paths/scenario: {B_PATHS}")
    print()

    print("Loading trades...")
    trades = load_trades(journal_dir)
    if not trades:
        print(f"  no trades in {journal_dir}")
        return 1
    earliest = trades[0]["ts"]
    latest = trades[-1]["ts"]
    global_mean = sum(t["pnl_usd"] for t in trades) / len(trades)
    print(f"  {len(trades):,} trades, {earliest.date()} → {latest.date()}")
    print(f"  global per-trade mean (used for dead scenario): {fmt_dollar(global_mean)}")

    print("Fetching BTC daily closes...")
    btc_prices = fetch_btc_daily_closes(earliest, latest)
    print(f"  {len(btc_prices):,} daily close prices")
    print()

    scenarios = ["null", "deg30", "deg50", "dead"]
    results: dict = {}
    for sc in scenarios:
        results[sc] = {}
        for h in HORIZONS_DAYS:
            print(f"  scenario={sc:>5}  horizon={h:>3}d  ", end="", flush=True)
            r = run_scenario(trades, sc, h, btc_prices, B_PATHS, global_mean)
            results[sc][h] = r
            print(f"trades={r.get('mean_n_trades', 0):.0f}  "
                  f"PNL={'fire' if r.get('fire_rate_pct',{}).get('KILL_PNL',0)>50 else 'ok':>4}")
    print()

    # Print results table
    criteria = ["KILL_PNL", "KILL_WR", "KILL_SYM", "KILL_HODL_C", "KILL_HODL_W"]
    print("Fire rates by criterion × scenario × horizon (% of B=1000 paths):")
    print()
    for h in HORIZONS_DAYS:
        print(f"  HORIZON = {h} days")
        print(f"    {'criterion':<14}  {'null':>6}  {'deg30':>6}  {'deg50':>6}  {'dead':>6}")
        print(f"    {'-'*14}  {'-'*6}  {'-'*6}  {'-'*6}  {'-'*6}")
        for c in criteria:
            row = [results[s][h]["fire_rate_pct"][c] for s in scenarios]
            print(f"    {c:<14}  {row[0]:>5.1f}%  {row[1]:>5.1f}%  {row[2]:>5.1f}%  {row[3]:>5.1f}%")
        print()

    # Apply locked decision rule (90d horizon)
    print("=" * 80)
    print("DECISION RULE EVALUATION (per pre-registration, 90d horizon):")
    print()
    issues = []
    borderline_issues = []
    for c in criteria:
        fp = results["null"][90]["fire_rate_pct"][c]
        tp_deg50 = results["deg50"][90]["fire_rate_pct"][c]
        tp_dead = results["dead"][90]["fire_rate_pct"][c]
        fp_ok = fp <= BAND_FP_NULL
        tp_deg50_ok = tp_deg50 >= BAND_TP_DEG50
        tp_dead_ok = tp_dead >= BAND_TP_DEAD
        # Borderline = within tolerance of failing
        fp_borderline = (BAND_FP_NULL < fp <= BAND_FP_NULL + BORDERLINE_TOLERANCE_PP)
        tp_deg50_borderline = (BAND_TP_DEG50 - BORDERLINE_TOLERANCE_PP <= tp_deg50 < BAND_TP_DEG50)
        tp_dead_borderline = (BAND_TP_DEAD - BORDERLINE_TOLERANCE_PP <= tp_dead < BAND_TP_DEAD)
        ok = fp_ok and tp_deg50_ok and tp_dead_ok
        borderline = fp_borderline or tp_deg50_borderline or tp_dead_borderline

        status = "✓ PASS" if ok else ("≈ BORDERLINE" if borderline else "✗ NEEDS_RECAL")
        print(f"  {c:<14}  FP(null)={fp:>5.1f}% {'✓' if fp_ok else '✗'}   "
              f"TP(deg50)={tp_deg50:>5.1f}% {'✓' if tp_deg50_ok else '✗'}   "
              f"TP(dead)={tp_dead:>5.1f}% {'✓' if tp_dead_ok else '✗'}   {status}")
        if not ok and not borderline:
            issues.append(c)
        elif not ok:
            borderline_issues.append(c)

    print()
    if not issues and not borderline_issues:
        verdict = "WELL_CALIBRATED"
    elif issues:
        verdict = "NEEDS_RECALIBRATION"
    else:
        verdict = "BORDERLINE"

    print("=" * 80)
    print(f"VERDICT: {verdict}")
    if issues:
        print(f"  Criteria failing bands: {', '.join(issues)}")
    if borderline_issues:
        print(f"  Borderline criteria: {', '.join(borderline_issues)}")
    print("=" * 80)
    print()
    print("Diagnostic context (mean per-path stats by scenario × horizon):")
    print()
    for sc in scenarios:
        print(f"  {sc}")
        for h in HORIZONS_DAYS:
            r = results[sc][h]
            print(f"    {h}d: trades={r['mean_n_trades']:>5.0f}  "
                  f"meanPnL={fmt_dollar(r['mean_total_pnl']):>12}  "
                  f"WR-eligibility={r['wr_eligibility_pct']:>5.1f}%")
    return 0


if __name__ == "__main__":
    sys.exit(main())
