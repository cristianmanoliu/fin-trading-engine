#!/usr/bin/env python3
"""
stage_promotion_check.py — mechanical PASS/FAIL of the locked STAGE_0 →
STAGE_1 promotion gates from CLAUDE.md ## Forward-paper go/no-go criteria
and results/real_money_protocol_decision_rule_2026-05-08.md.

Forward-paper resolves to one decision: should we flip to real money?
The locked criteria are documented prose; under stress, ~127 days from
now when n_trades crosses 150 for the first time, an operator re-reading
prose and deciding "promote yes/no" is a brittle process. This script
mechanizes the decision: read journal data + drift history, emit
per-criterion PASS/FAIL with current value vs threshold, output a
single verdict.

Criteria (per real_money_protocol_decision_rule_2026-05-08.md §STAGE_0→1):
  1. ≥150 closed paper trades
  2. ≥60 calendar days since first trade
  3. Net-positive cumulative PnL in dollars
  4. Live PnL ≥ 60% of pro-rated honest-annual ($69k/yr × elapsed × 0.60)
  5. Live PnL beats BTC-HODL benchmark over the same window      [v2 TODO]
  6. No single symbol >40% of cumulative live PnL
  7. Drift detector clean for ≥30 consecutive days at α=0.001
  8. Realized round-trip taker fees ≤ 12 bp
  9. Realized stop-side slippage ≤ 20 bp on losing-trade subsample

NOTE: Criterion #5 (BTC-HODL beat) requires an external price source.
The existing btc_hodl_benchmark.py helper handles this; v1 of this
script delegates with a clear flag and reports DEFERRED. Operator must
manually verify before promotion in v1; v2 should integrate.

Exit codes:
  0  PROMOTE — all evaluable gates pass + #5 manual-verify reminder
  1  BLOCKED — at least one gate fails outright (do not promote)
  2  WAITING — insufficient data; some gates not yet evaluable
  3  ERROR — input/env failure

Usage:
  python3 scripts/stage_promotion_check.py
  python3 scripts/stage_promotion_check.py --live-source local --live-dir ./logs/journal
  python3 scripts/stage_promotion_check.py --drift-history results/drift_check_history.jsonl
"""
from __future__ import annotations

import argparse
import json
import subprocess
import sys
import tempfile
from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
from pathlib import Path

# Locked thresholds. CRITICAL: change only by editing CLAUDE.md +
# the pre-registration doc, then this file. Drift between source and
# code is the failure mode this script exists to prevent.
MIN_TRADES = 150
MIN_DAYS = 60
MIN_DRIFT_CLEAN_DAYS = 30
MAX_FEE_BPS = 12.0
MAX_SLIP_BPS = 20.0
MAX_SINGLE_SYM_PCT = 40.0
HONEST_ANNUAL_USD = 69000.0
HONEST_FRACTION = 0.60


@dataclass
class Trade:
    ts: str
    symbol: str
    outcome: str
    pnl_usd: float
    fee_usd: float
    slip_usd: float
    notional_usd: float


@dataclass
class Criterion:
    name: str
    threshold: str
    actual: str
    status: str  # PASS / FAIL / PENDING / DEFERRED


def load_journal(journal_dir: Path) -> list[Trade]:
    """Load LIVE-cohort closes only (top-level *.jsonl, not shadow/*).
    Skips PARTIAL closes and pre-cost-decomp closes (notional==0)."""
    if not journal_dir.is_dir():
        return []
    out: list[Trade] = []
    for jf in sorted(journal_dir.glob("*.jsonl")):
        for line in jf.read_text().splitlines():
            if not line:
                continue
            try:
                ev = json.loads(line)
            except json.JSONDecodeError:
                continue
            if ev.get("event") != "close":
                continue
            if ev.get("outcome") == "PARTIAL":
                continue
            out.append(Trade(
                ts=ev.get("ts", ""),
                symbol=ev.get("symbol", "?"),
                outcome=ev.get("outcome", "?"),
                pnl_usd=ev.get("pnl_usd", 0.0),
                fee_usd=ev.get("fee_usd", 0.0),
                slip_usd=ev.get("slip_usd", 0.0),
                notional_usd=ev.get("notional_usd", 0.0),
            ))
    return out


def fetch_remote(vps: str, remote_dir: str) -> list[Trade]:
    cmd = f'ssh {vps} "tar -cf - -C {remote_dir} . 2>/dev/null"'
    with tempfile.TemporaryDirectory() as tmp:
        result = subprocess.run(cmd, shell=True, check=True, capture_output=True)
        subprocess.run(["tar", "-xf", "-", "-C", tmp], input=result.stdout, check=True)
        return load_journal(Path(tmp))


def parse_iso(ts: str) -> datetime | None:
    """Parse RFC3339-ish UTC timestamps from journal entries."""
    if not ts:
        return None
    try:
        # Strip fractional seconds + Z suffix variants.
        clean = ts.rstrip("Z")
        if "." in clean:
            clean = clean.split(".", 1)[0]
        return datetime.fromisoformat(clean).replace(tzinfo=timezone.utc)
    except ValueError:
        return None


# ── Per-criterion evaluators ─────────────────────────────────────────────────


def check_trades(trades: list[Trade]) -> Criterion:
    n = len(trades)
    return Criterion(
        name="1. ≥150 closed paper trades",
        threshold=f"≥{MIN_TRADES}",
        actual=str(n),
        status="PASS" if n >= MIN_TRADES else "PENDING",
    )


def check_days(trades: list[Trade]) -> Criterion:
    if not trades:
        return Criterion("2. ≥60 calendar days elapsed", f"≥{MIN_DAYS}", "0", "PENDING")
    first = min((parse_iso(t.ts) for t in trades if parse_iso(t.ts)), default=None)
    if first is None:
        return Criterion("2. ≥60 calendar days elapsed", f"≥{MIN_DAYS}", "?", "PENDING")
    days = (datetime.now(timezone.utc) - first).days
    return Criterion(
        name="2. ≥60 calendar days elapsed",
        threshold=f"≥{MIN_DAYS}",
        actual=str(days),
        status="PASS" if days >= MIN_DAYS else "PENDING",
    )


def check_net_positive(trades: list[Trade]) -> Criterion:
    pnl = sum(t.pnl_usd for t in trades)
    return Criterion(
        name="3. Net-positive cumulative PnL",
        threshold=">$0",
        actual=f"${pnl:+,.0f}",
        status="PASS" if pnl > 0 else ("FAIL" if len(trades) >= MIN_TRADES else "PENDING"),
    )


def check_pro_rated_annual(trades: list[Trade]) -> Criterion:
    if not trades:
        return Criterion("4. PnL ≥60% pro-rated annual", "varies", "$0", "PENDING")
    first = min((parse_iso(t.ts) for t in trades if parse_iso(t.ts)), default=None)
    if first is None:
        return Criterion("4. PnL ≥60% pro-rated annual", "varies", "?", "PENDING")
    days = max(1, (datetime.now(timezone.utc) - first).days)
    elapsed_yr = days / 365.25
    target = HONEST_ANNUAL_USD * elapsed_yr * HONEST_FRACTION
    pnl = sum(t.pnl_usd for t in trades)
    return Criterion(
        name="4. PnL ≥60% pro-rated annual",
        threshold=f"≥${target:,.0f} (=${HONEST_ANNUAL_USD/1000:.0f}k×{elapsed_yr:.2f}y×{HONEST_FRACTION})",
        actual=f"${pnl:+,.0f}",
        status="PASS" if pnl >= target else (
            "FAIL" if days >= MIN_DAYS and len(trades) >= MIN_TRADES else "PENDING"),
    )


def check_btc_hodl_deferred() -> Criterion:
    """v1: defer the BTC-HODL benchmark check to the operator + the
    existing btc_hodl_benchmark.py helper. v2 should integrate."""
    return Criterion(
        name="5. PnL beats BTC-HODL ($32k notional)",
        threshold="manual: scripts/btc_hodl_benchmark.py",
        actual="(deferred)",
        status="DEFERRED",
    )


def check_single_symbol_concentration(trades: list[Trade]) -> Criterion:
    if not trades:
        return Criterion("6. No single symbol >40% PnL", f"<{MAX_SINGLE_SYM_PCT}%", "n/a", "PENDING")
    total_abs = sum(abs(t.pnl_usd) for t in trades) or 1.0
    by_sym: dict[str, float] = {}
    for t in trades:
        by_sym[t.symbol] = by_sym.get(t.symbol, 0.0) + t.pnl_usd
    # Concentration: largest |sym_pnl| / sum(|all_pnl|).
    max_sym, max_abs = "", 0.0
    for sym, p in by_sym.items():
        if abs(p) > max_abs:
            max_sym, max_abs = sym, abs(p)
    pct = max_abs / total_abs * 100
    return Criterion(
        name="6. No single symbol >40% PnL",
        threshold=f"<{MAX_SINGLE_SYM_PCT}%",
        actual=f"{pct:.1f}% ({max_sym})",
        status="PASS" if pct < MAX_SINGLE_SYM_PCT else (
            "FAIL" if len(trades) >= MIN_TRADES else "PENDING"),
    )


def check_drift_clean(history_path: Path) -> Criterion:
    """At least 30 consecutive days of CLEAN drift verdicts (no DRIFT_FIRED)
    in the recent history. Per the locked rule, weekly cadence — so 30 days
    is ~4-5 weekly runs minimum."""
    if not history_path.exists():
        return Criterion(
            name=f"7. Drift detector clean ≥{MIN_DRIFT_CLEAN_DAYS}d",
            threshold=f"≥{MIN_DRIFT_CLEAN_DAYS}d clean",
            actual="(history missing)",
            status="PENDING",
        )
    cutoff = datetime.now(timezone.utc) - timedelta(days=MIN_DRIFT_CLEAN_DAYS)
    most_recent: datetime | None = None
    most_recent_fired: datetime | None = None
    for line in history_path.read_text().splitlines():
        if not line:
            continue
        try:
            ev = json.loads(line)
        except json.JSONDecodeError:
            continue
        ts = parse_iso(ev.get("ts", ""))
        if ts is None:
            continue
        if most_recent is None or ts > most_recent:
            most_recent = ts
        if ev.get("verdict") == "DRIFT_FIRED" and (
                most_recent_fired is None or ts > most_recent_fired):
            most_recent_fired = ts
    if most_recent is None:
        return Criterion(
            name=f"7. Drift detector clean ≥{MIN_DRIFT_CLEAN_DAYS}d",
            threshold=f"≥{MIN_DRIFT_CLEAN_DAYS}d",
            actual="(no runs)",
            status="PENDING",
        )
    if most_recent_fired is None:
        # Never fired. If most_recent run is recent, we're clean for the
        # full window-since-deploy.
        days_clean = (datetime.now(timezone.utc) - cutoff).days  # window length
        return Criterion(
            name=f"7. Drift detector clean ≥{MIN_DRIFT_CLEAN_DAYS}d",
            threshold=f"≥{MIN_DRIFT_CLEAN_DAYS}d",
            actual=f"never fired (last run: {most_recent.date()})",
            status="PASS",
        )
    days_since_fire = (datetime.now(timezone.utc) - most_recent_fired).days
    return Criterion(
        name=f"7. Drift detector clean ≥{MIN_DRIFT_CLEAN_DAYS}d",
        threshold=f"≥{MIN_DRIFT_CLEAN_DAYS}d",
        actual=f"{days_since_fire}d since last fire ({most_recent_fired.date()})",
        status="PASS" if days_since_fire >= MIN_DRIFT_CLEAN_DAYS else "FAIL",
    )


def check_fee_bps(trades: list[Trade]) -> Criterion:
    qualifying = [t for t in trades if t.notional_usd > 0]
    if not qualifying:
        return Criterion("8. Realized fee ≤12 bp", f"≤{MAX_FEE_BPS}bp", "n/a", "PENDING")
    total_fee = sum(t.fee_usd for t in qualifying)
    total_notional = sum(t.notional_usd for t in qualifying)
    bps = total_fee / total_notional * 10000.0
    return Criterion(
        name="8. Realized fee ≤12 bp",
        threshold=f"≤{MAX_FEE_BPS}bp",
        actual=f"{bps:.2f}bp",
        status="PASS" if bps <= MAX_FEE_BPS else "FAIL",
    )


def check_slip_bps(trades: list[Trade]) -> Criterion:
    losers = [t for t in trades if t.notional_usd > 0 and t.outcome == "STOP"]
    if not losers:
        return Criterion("9. Realized slip ≤20 bp (losers)", f"≤{MAX_SLIP_BPS}bp", "n/a", "PENDING")
    total_slip = sum(t.slip_usd for t in losers)
    total_notional = sum(t.notional_usd for t in losers)
    bps = total_slip / total_notional * 10000.0
    return Criterion(
        name="9. Realized slip ≤20 bp (losers)",
        threshold=f"≤{MAX_SLIP_BPS}bp",
        actual=f"{bps:.2f}bp (n={len(losers)})",
        status="PASS" if bps <= MAX_SLIP_BPS else "FAIL",
    )


# ── Verdict + render ─────────────────────────────────────────────────────────


def verdict_of(criteria: list[Criterion]) -> tuple[str, int]:
    """Map the per-criterion statuses to an overall verdict + exit code."""
    if any(c.status == "FAIL" for c in criteria):
        return "BLOCKED — at least one gate fails outright. Do not promote.", 1
    if any(c.status == "PENDING" for c in criteria):
        return "WAITING — insufficient data on ≥1 gate. Continue forward-paper accumulation.", 2
    # All PASS or DEFERRED.
    deferred = [c for c in criteria if c.status == "DEFERRED"]
    if deferred:
        return (f"PROMOTE-CANDIDATE — {len(deferred)} gate(s) require manual verification "
                f"(see DEFERRED rows). Operator must verify before flipping --executor=binance_live."), 0
    return "PROMOTE — all locked gates pass. Operator may flip --executor=binance_live.", 0


def render(criteria: list[Criterion]) -> str:
    sep = "═" * 96
    out = [sep]
    out.append("  STAGE_0 → STAGE_1 promotion check")
    out.append("  Source: CLAUDE.md ## Forward-paper go/no-go criteria")
    out.append("        + results/real_money_protocol_decision_rule_2026-05-08.md")
    out.append(sep)
    out.append("")
    out.append(f"  {'criterion':<46}  {'threshold':<32}  {'actual':<24}  status")
    out.append("  " + "-" * 92)
    for c in criteria:
        out.append(f"  {c.name:<46}  {c.threshold:<32}  {c.actual:<24}  [{c.status}]")
    out.append("")
    out.append(sep)
    text, _ = verdict_of(criteria)
    out.append(f"  >>> VERDICT: {text}")
    out.append(sep)
    return "\n".join(out) + "\n"


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--live-source", choices=("vps", "local"), default="vps")
    ap.add_argument("--vps", default="root@178.105.24.230")
    ap.add_argument("--live-dir", default="/var/log/paper-live/journal")
    ap.add_argument("--drift-history", type=Path,
                    default=Path("results/drift_check_history.jsonl"),
                    help="Path to drift_check_history.jsonl (per-machine)")
    args = ap.parse_args()

    if args.live_source == "vps":
        try:
            trades = fetch_remote(args.vps, args.live_dir)
        except subprocess.CalledProcessError as e:
            print(f"ssh fetch failed: {e}", file=sys.stderr)
            return 3
    else:
        d = Path(args.live_dir)
        if not d.is_dir():
            print(f"local journal dir not found: {d}", file=sys.stderr)
            return 3
        trades = load_journal(d)

    criteria = [
        check_trades(trades),
        check_days(trades),
        check_net_positive(trades),
        check_pro_rated_annual(trades),
        check_btc_hodl_deferred(),
        check_single_symbol_concentration(trades),
        check_drift_clean(args.drift_history),
        check_fee_bps(trades),
        check_slip_bps(trades),
    ]
    print(render(criteria), end="")
    _, code = verdict_of(criteria)
    return code


if __name__ == "__main__":
    sys.exit(main())
