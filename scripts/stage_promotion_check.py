#!/usr/bin/env python3
"""
stage_promotion_check.py — mechanical PASS/FAIL of the locked promotion
gates for ALL FOUR stage transitions (STAGE_0→1, 1→2, 2→3, 3→4) from
results/real_money_protocol_decision_rule_2026-05-08.md plus
CLAUDE.md ## Forward-paper go/no-go criteria.

Each stage transition has its own locked gate set. Without per-stage
mechanization, the operator falls back to manual judgment exactly when
stakes rise (real-money $300 → $500 → $1k decisions). This script
applies the locked rules verbatim regardless of which transition is
active — passed via --from-stage.

Forward-paper → STAGE_1 is the first transition (~127 days from
2026-05-05); STAGE_3 → STAGE_4 is the last (earliest 2027-03-09 per
the locked time gates). All four are mechanized here so the same
tool serves the entire 12+ month deployment ladder.

────────────────────────────────────────────────────────────────────────
Stage-specific gate sets (per real_money_protocol_decision_rule_2026-05-08.md)
────────────────────────────────────────────────────────────────────────

STAGE_0 → STAGE_1 (paper → $100/trade, 9 gates):
  1. ≥150 closed paper trades
  2. ≥60 calendar days since first trade
  3. Net-positive cumulative PnL in dollars
  4. Live PnL ≥ 60% of pro-rated honest-annual ($69k/yr × elapsed × 0.60)
  5. Live PnL beats BTC-HODL benchmark over the same window    [MECHANIZED]
  6. No single symbol >40% of cumulative live PnL
  7. Drift detector clean for ≥30 consecutive days at α=0.001
  8. Realized round-trip taker fees ≤ 12 bp
  9. Realized stop-side slippage ≤ 20 bp on losing-trade subsample

STAGE_1 → STAGE_2 ($100 → $300, 7 gates) — requires --stage-1-start:
  1. ≥50 closed real-money trades at STAGE_1
  2. ≥30 calendar days at STAGE_1
  3. Realized fee within 5% of modeled (≤ 10.5 bp at 10 bp model)
  4. Realized slip within 20% of modeled (≤ 6 bp at 5 bp model)
  5. Drift detector clean for ≥30 days at α=0.001
  6. No single-day net loss exceeding 2× per-trade stake ($200 at STAGE_1)
  7. Net-positive cumulative at STAGE_1

STAGE_2 → STAGE_3 ($300 → $500, 6 gates) — requires --stage-1-start + --stage-2-start:
  1. ≥100 closed real-money trades cumulative (STAGE_1 + STAGE_2)
  2. ≥60 calendar days at STAGE_2
  3. Realized fee/slip stable: most recent 30-trade window within 10% of modeled
  4. Drift detector clean for ≥45 days
  5. Net-positive over most recent 30-day window
  6. No single-symbol >40% of STAGE_1+STAGE_2 cumulative PnL

STAGE_3 → STAGE_4 ($500 → $1,000, 6 gates) — requires --stage-2-start + --stage-3-start:
  1. ≥200 closed real-money trades cumulative
  2. ≥90 calendar days at STAGE_3
  3. Realized fee/slip stable across STAGE_2+STAGE_3 (90+ day window)
  4. Drift detector clean for ≥60 days
  5. Net-positive cumulative across STAGE_2+STAGE_3
  6. Annualized realized NET at STAGE_3 ≥ 50% pro-rated ($69k/yr × elapsed × 0.50)

Criterion #5 of STAGE_0→1 (BTC-HODL beat) integrates btc_hodl_benchmark.py — pipes
trades as TSV, parses cumulative strategy-vs-HODL delta. Falls back
to DEFERRED on helper error (Binance API down, timeout, decode
failure) so a transient network blip doesn't trigger a spurious FAIL
on a decision-grade gate.

Exit codes:
  0  PROMOTE — all locked gates pass; safe to flip the next-stage executor
  1  BLOCKED — at least one gate fails outright (do not promote)
  2  WAITING — insufficient data; some gates not yet evaluable
  3  ERROR — input/env failure (incl. missing required --stage-N-start arg)
  4  PROMOTE-CANDIDATE — all mechanizable gates pass but ≥1 DEFERRED
     (e.g. BTC-HODL helper unavailable). Operator must verify the deferred
     gate(s) before flipping the executor. Distinct from exit 0 so the
     weekly cron can WARN rather than CRITICAL on transient helper failures.

Usage:
  # STAGE_0 → STAGE_1 (default, paper → first real money)
  python3 scripts/stage_promotion_check.py

  # STAGE_1 → STAGE_2
  python3 scripts/stage_promotion_check.py \\
    --from-stage STAGE_1 --stage-1-start 2026-08-15T00:00:00Z

  # STAGE_2 → STAGE_3
  python3 scripts/stage_promotion_check.py --from-stage STAGE_2 \\
    --stage-1-start 2026-08-15T00:00:00Z \\
    --stage-2-start 2026-09-30T00:00:00Z

  # STAGE_3 → STAGE_4
  python3 scripts/stage_promotion_check.py --from-stage STAGE_3 \\
    --stage-2-start 2026-09-30T00:00:00Z \\
    --stage-3-start 2026-12-15T00:00:00Z
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

# Locked thresholds. CRITICAL: change only by editing
# results/real_money_protocol_decision_rule_2026-05-08.md + CLAUDE.md +
# this file. Drift between source-of-truth and code is the failure mode
# this script exists to prevent.

# ── STAGE_0 → STAGE_1 (paper → first real money) ────────────────────────────
MIN_TRADES = 150
MIN_DAYS = 60
MIN_DRIFT_CLEAN_DAYS = 30
MAX_FEE_BPS = 12.0
MAX_SLIP_BPS = 20.0
MAX_SINGLE_SYM_PCT = 40.0
HONEST_ANNUAL_USD = 69000.0
HONEST_FRACTION = 0.60
BENCHMARK_NOTIONAL = 32000.0  # CLAUDE.md locked HODL benchmark size

# ── Modeled costs (Binance USDT-M Futures Regular tier, per CLAUDE.md) ─────
# Round-trip taker fee: 5 bp/side × 2 sides = 10 bp.
# Stop-side slip: 5 bp on the losing-trade subsample.
MODELED_FEE_BPS = 10.0
MODELED_SLIP_BPS = 5.0

# ── Drift-history freshness ────────────────────────────────────────────────
# If the most recent drift run is older than this, we treat the history as
# stale (likely dead cron) and refuse to grant any "drift clean" gate based
# on it. Drift cadence is weekly (Sunday 09:00 via launchd); 14 days = two
# missed cycles, well past "one missed Sunday" noise. This closes the
# fail-open where a long-stopped cron + an old never-fired history would
# return PASS forever.
DRIFT_HISTORY_STALE_DAYS = 14

# ── STAGE_1 → STAGE_2 ($100 → $300) ─────────────────────────────────────────
S1_MIN_TRADES = 50
S1_MIN_DAYS = 30
S1_DRIFT_CLEAN_DAYS = 30
S1_FEE_TOLERANCE = 0.05  # 5% of modeled → 10 × 1.05 = 10.5 bp ceiling
S1_SLIP_TOLERANCE = 0.20  # 20% of modeled → 5 × 1.20 = 6.0 bp ceiling
S1_PER_TRADE_STAKE_USD = 100.0
S1_DAILY_LOSS_MULT = 2.0  # cap = -$200/day at STAGE_1

# ── STAGE_2 → STAGE_3 ($300 → $500) ─────────────────────────────────────────
S2_MIN_TRADES_CUMULATIVE = 100  # S1 + S2 combined
S2_MIN_DAYS = 60
S2_DRIFT_CLEAN_DAYS = 45
S2_RECENT_WINDOW_TRADES = 30
S2_RECENT_TOLERANCE = 0.10  # 10% band: fee in [9, 11] bp; slip in [4.5, 5.5] bp
S2_RECENT_NET_DAYS = 30
S2_MAX_SINGLE_SYM_PCT = 40.0  # same as STAGE_0→1; "real concentration" doesn't change

# ── STAGE_3 → STAGE_4 ($500 → $1,000) ───────────────────────────────────────
S3_MIN_TRADES_CUMULATIVE = 200
S3_MIN_DAYS = 90
S3_DRIFT_CLEAN_DAYS = 60
S3_STABLE_WINDOW_DAYS = 90  # S2+S3 stable-cost window
S3_STABLE_TOLERANCE = 0.10  # same 10% band
S3_HONEST_FRACTION = 0.50  # tighter than STAGE_0's 0.60 — full-deployed expectation

# ── Stage transitions registry ──────────────────────────────────────────────
# from-stage → (to-stage, render-label, required-stage-start-args).
# main() validates required args before fetching data, returning exit 3
# if any are missing — that's the "missing input → silent success" trap
# we explicitly close out.
STAGE_TRANSITIONS = {
    "STAGE_0": ("STAGE_1", "STAGE_0 → STAGE_1 promotion check", set()),
    "STAGE_1": ("STAGE_2", "STAGE_1 → STAGE_2 promotion check", {"stage_1_start"}),
    "STAGE_2": ("STAGE_3", "STAGE_2 → STAGE_3 promotion check",
                {"stage_1_start", "stage_2_start"}),
    "STAGE_3": ("STAGE_4", "STAGE_3 → STAGE_4 promotion check",
                {"stage_1_start", "stage_2_start", "stage_3_start"}),
}

# Path to the BTC-HODL helper. Module-level so tests can monkey-patch
# to a stub script that emits canned TSV without hitting Binance.
BTC_HELPER = Path(__file__).resolve().parent / "btc_hodl_benchmark.py"


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


def check_btc_hodl(trades: list[Trade]) -> Criterion:
    """v2: integrate btc_hodl_benchmark.py — pipe trades as TSV, parse
    cumulative strategy vs HODL delta. Pass when delta > 0 (strategy
    NET beats buy-and-hold over the same window).

    Falls back to DEFERRED on any helper failure (Binance API down,
    timeout, decode error) — better to surface a deferral than emit a
    spurious FAIL when the benchmark itself couldn't be computed.

    HODL helper emits TSV: cum_strategy cum_hodl cum_delta n_windows
    n_underperf_pairs kill_triggered warning."""
    name = "5. PnL beats BTC-HODL ($32k notional)"
    if not trades:
        return Criterion(name, ">$0 vs HODL", "no trades", "PENDING")

    # Explicit path check — subprocess.run("python3", missing_script) doesn't
    # raise FileNotFoundError (python3 itself exists; it just errors at the
    # script-open stage). Catch this case up-front so the test/operator gets
    # a clean DEFERRED with a useful actual message.
    helper_path = Path(str(BTC_HELPER))
    if not helper_path.is_file():
        return Criterion(name, ">$0 vs HODL",
                         f"helper not found at {helper_path}",
                         "DEFERRED")

    tsv_lines = [
        f"{t.ts}\t{t.symbol}\t{t.pnl_usd}\t{t.outcome}"
        for t in trades
    ]
    tsv_input = "\n".join(tsv_lines) + "\n"

    try:
        result = subprocess.run(
            ["python3", str(helper_path),
             "--benchmark-notional", str(BENCHMARK_NOTIONAL)],
            input=tsv_input,
            capture_output=True, text=True,
            timeout=30,
        )
    except subprocess.TimeoutExpired:
        return Criterion(name, ">$0 vs HODL",
                         "btc_hodl_benchmark timed out (>30s)",
                         "DEFERRED")
    except FileNotFoundError:
        return Criterion(name, ">$0 vs HODL",
                         "python3 not found in PATH",
                         "DEFERRED")

    if result.returncode != 0:
        err = result.stderr.strip()[:60] or "non-zero exit"
        return Criterion(name, ">$0 vs HODL", f"helper error: {err}", "DEFERRED")

    # rstrip("\n") only — the helper's emit() outputs 7 tab-separated fields
    # but the 7th (warning) is empty when the benchmark succeeds, so a plain
    # .strip() would clip the trailing tab and produce 6 fields. Keep the
    # empty-string warning field intact by stripping only the newline.
    parts = result.stdout.rstrip("\n").split("\t")
    if len(parts) < 7:
        return Criterion(name, ">$0 vs HODL",
                         f"unexpected output ({len(parts)} fields)", "DEFERRED")

    cum_strat_s, cum_hodl_s, cum_delta_s, _nw, _nu, _kt, warning = parts[:7]
    if warning:
        return Criterion(name, ">$0 vs HODL",
                         f"benchmark warning: {warning}", "DEFERRED")

    try:
        cum_strategy = float(cum_strat_s)
        cum_hodl = float(cum_hodl_s)
        cum_delta = float(cum_delta_s)
    except ValueError:
        return Criterion(name, ">$0 vs HODL",
                         f"non-numeric output: {result.stdout.strip()[:60]}",
                         "DEFERRED")

    actual = (f"strategy ${cum_strategy:+,.0f} vs HODL ${cum_hodl:+,.0f} "
              f"(Δ ${cum_delta:+,.0f})")
    # PASS only when n ≥ MIN_TRADES — a positive delta at low n could be
    # lucky 10 trades, not a real outperformance signal. Gate symmetric to
    # FAIL: both grant the verdict only above n-floor. Below n-floor the
    # answer is "we don't know yet."
    if len(trades) < MIN_TRADES:
        status = "PENDING"
    elif cum_delta > 0:
        status = "PASS"
    else:
        status = "FAIL"
    return Criterion(name, ">$0 vs HODL", actual, status)


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
    # Freshness gate (closes the dead-cron fail-open). If the most recent
    # drift run is older than DRIFT_HISTORY_STALE_DAYS, we have no current
    # observation supporting the "clean" claim — refuse to grant PASS.
    days_since_recent = (datetime.now(timezone.utc) - most_recent).days
    if days_since_recent > DRIFT_HISTORY_STALE_DAYS:
        return Criterion(
            name=f"7. Drift detector clean ≥{MIN_DRIFT_CLEAN_DAYS}d",
            threshold=f"≥{MIN_DRIFT_CLEAN_DAYS}d",
            actual=f"stale history: last run {days_since_recent}d ago "
                   f"(>{DRIFT_HISTORY_STALE_DAYS}d threshold)",
            status="PENDING",
        )
    if most_recent_fired is None:
        # Never fired AND fresh enough. Genuine clean window.
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


# ── Trade segmentation helpers ───────────────────────────────────────────────


def filter_trades_after(trades: list[Trade], start: datetime) -> list[Trade]:
    """Trades whose first-parsed ts is >= start. Trades with unparseable
    ts are excluded — we can't place them in time so they can't be
    confidently assigned to a stage. Same n_floor pattern: better PENDING
    on ambiguity than a confident wrong answer."""
    out = []
    for t in trades:
        ts = parse_iso(t.ts)
        if ts is None:
            continue
        if ts >= start:
            out.append(t)
    return out


def filter_trades_in_window(trades: list[Trade], start: datetime,
                            end: datetime | None = None) -> list[Trade]:
    """Trades in [start, end). If end is None, end = now."""
    end_eff = end if end is not None else datetime.now(timezone.utc)
    out = []
    for t in trades:
        ts = parse_iso(t.ts)
        if ts is None:
            continue
        if start <= ts < end_eff:
            out.append(t)
    return out


def parse_stage_start_arg(value: str, name: str) -> datetime:
    """Parse a CLI-provided ISO stage-start timestamp.

    Raises ValueError if value is empty or malformed — caller should
    convert this to exit 3. Reason this is its own function: the
    "missing input → silent success" trap is exactly that empty/None
    inputs would default to epoch (or "now", or whatever fallback)
    and silently produce a truthy verdict. Explicit parse → explicit
    failure when bad."""
    if not value:
        raise ValueError(f"{name} is required for this stage transition")
    parsed = parse_iso(value)
    if parsed is None:
        raise ValueError(f"{name} is not a valid ISO timestamp: {value!r}")
    return parsed


# ── Per-stage evaluators ─────────────────────────────────────────────────────


def evaluate_stage_0_to_1(trades: list[Trade],
                          drift_history: Path) -> list[Criterion]:
    """STAGE_0 → STAGE_1 (paper → first real money). 9 gates."""
    return [
        check_trades(trades),
        check_days(trades),
        check_net_positive(trades),
        check_pro_rated_annual(trades),
        check_btc_hodl(trades),
        check_single_symbol_concentration(trades),
        check_drift_clean(drift_history),
        check_fee_bps(trades),
        check_slip_bps(trades),
    ]


# ── Parameterized check functions (reused across STAGE_1+ evaluators) ────────


def check_n_trades(trades: list[Trade], threshold: int, name: str) -> Criterion:
    """Generic trade-count gate: PASS if len ≥ threshold, else PENDING."""
    n = len(trades)
    return Criterion(
        name=name,
        threshold=f"≥{threshold}",
        actual=str(n),
        status="PASS" if n >= threshold else "PENDING",
    )


def check_days_at_stage(stage_start: datetime, min_days: int, name: str) -> Criterion:
    """Calendar days from stage_start to now."""
    days = (datetime.now(timezone.utc) - stage_start).days
    return Criterion(
        name=name,
        threshold=f"≥{min_days}d",
        actual=f"{days}d (since {stage_start.date()})",
        status="PASS" if days >= min_days else "PENDING",
    )


def check_fee_within_tolerance(trades: list[Trade], modeled_bps: float,
                               tolerance: float, name: str) -> Criterion:
    """Realized fee bps must be within (1+tolerance)× of modeled.
    PENDING if no qualifying notional."""
    qualifying = [t for t in trades if t.notional_usd > 0]
    ceiling = modeled_bps * (1 + tolerance)
    if not qualifying:
        return Criterion(name, f"≤{ceiling:.2f}bp", "n/a", "PENDING")
    total_fee = sum(t.fee_usd for t in qualifying)
    total_notional = sum(t.notional_usd for t in qualifying)
    bps = total_fee / total_notional * 10000.0
    return Criterion(
        name=name,
        threshold=f"≤{ceiling:.2f}bp ({modeled_bps:.0f}bp×{1+tolerance:.2f})",
        actual=f"{bps:.2f}bp",
        status="PASS" if bps <= ceiling else "FAIL",
    )


def check_slip_within_tolerance(trades: list[Trade], modeled_bps: float,
                                tolerance: float, name: str) -> Criterion:
    """Realized slip bps on losers must be within (1+tolerance)× of modeled."""
    losers = [t for t in trades if t.notional_usd > 0 and t.outcome == "STOP"]
    ceiling = modeled_bps * (1 + tolerance)
    if not losers:
        return Criterion(name, f"≤{ceiling:.2f}bp", "n/a", "PENDING")
    total_slip = sum(t.slip_usd for t in losers)
    total_notional = sum(t.notional_usd for t in losers)
    bps = total_slip / total_notional * 10000.0
    return Criterion(
        name=name,
        threshold=f"≤{ceiling:.2f}bp ({modeled_bps:.1f}bp×{1+tolerance:.2f})",
        actual=f"{bps:.2f}bp (n={len(losers)})",
        status="PASS" if bps <= ceiling else "FAIL",
    )


def check_drift_clean_at_stage(history_path: Path, min_days: int, name: str) -> Criterion:
    """Same logic as check_drift_clean but with parameterized window length.
    Each stage requires a different drift-clean window (30/30/45/60d)."""
    if not history_path.exists():
        return Criterion(name, f"≥{min_days}d clean", "(history missing)", "PENDING")
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
        return Criterion(name, f"≥{min_days}d", "(no runs)", "PENDING")
    # Freshness gate — same shape as STAGE_0's check_drift_clean. Stale
    # history → PENDING, not PASS. Closes the dead-cron fail-open at every
    # stage transition (STAGE_1 onwards has min_days 30/45/60 — the gap
    # between min_days and DRIFT_HISTORY_STALE_DAYS=14 widens with stage,
    # so the freshness floor matters more, not less, at later stages).
    days_since_recent = (datetime.now(timezone.utc) - most_recent).days
    if days_since_recent > DRIFT_HISTORY_STALE_DAYS:
        return Criterion(
            name=name,
            threshold=f"≥{min_days}d",
            actual=f"stale history: last run {days_since_recent}d ago "
                   f"(>{DRIFT_HISTORY_STALE_DAYS}d threshold)",
            status="PENDING",
        )
    if most_recent_fired is None:
        return Criterion(name, f"≥{min_days}d",
                         f"never fired (last run: {most_recent.date()})", "PASS")
    days_since_fire = (datetime.now(timezone.utc) - most_recent_fired).days
    return Criterion(
        name=name,
        threshold=f"≥{min_days}d",
        actual=f"{days_since_fire}d since last fire ({most_recent_fired.date()})",
        status="PASS" if days_since_fire >= min_days else "FAIL",
    )


def check_no_daily_loss_above(trades: list[Trade], cap_usd: float,
                              n_floor: int, name: str) -> Criterion:
    """No single calendar day's net pnl can be more negative than -cap_usd.

    n_floor: below this many trades, PENDING (sampling variance — at n=2
    a single bad day is unrepresentative). Aligns with the kill-protocol
    n_floor pattern that prevented spurious KILL at low n.
    """
    if len(trades) < n_floor:
        return Criterion(name, f"≥-${cap_usd:.0f}/day",
                         f"only {len(trades)} trades (need ≥{n_floor})", "PENDING")
    by_day: dict[str, float] = {}
    for t in trades:
        d = t.ts[:10]
        by_day[d] = by_day.get(d, 0.0) + t.pnl_usd
    if not by_day:
        return Criterion(name, f"≥-${cap_usd:.0f}/day", "no parsed days", "PENDING")
    worst_day, worst_pnl = min(by_day.items(), key=lambda kv: kv[1])
    return Criterion(
        name=name,
        threshold=f"≥-${cap_usd:.0f}/day",
        actual=f"worst: ${worst_pnl:+,.0f} ({worst_day})",
        status="FAIL" if worst_pnl < -cap_usd else "PASS",
    )


def check_net_positive_at_stage(trades: list[Trade], n_floor: int, name: str) -> Criterion:
    """Net-positive cumulative PnL at this stage. PENDING below n_floor;
    FAIL only at n_floor + actually negative. Same shape as STAGE_0 #3."""
    pnl = sum(t.pnl_usd for t in trades)
    if len(trades) < n_floor:
        return Criterion(name, ">$0", f"${pnl:+,.0f} (n<{n_floor})", "PENDING")
    return Criterion(name, ">$0", f"${pnl:+,.0f}",
                     "PASS" if pnl > 0 else "FAIL")


def check_single_symbol_at_stage(trades: list[Trade], max_pct: float,
                                 n_floor: int, name: str) -> Criterion:
    """Single-symbol concentration. Same logic as STAGE_0 #6 but parameterized."""
    if len(trades) < n_floor:
        return Criterion(name, f"<{max_pct}%",
                         f"only {len(trades)} trades", "PENDING")
    total_abs = sum(abs(t.pnl_usd) for t in trades) or 1.0
    by_sym: dict[str, float] = {}
    for t in trades:
        by_sym[t.symbol] = by_sym.get(t.symbol, 0.0) + t.pnl_usd
    max_sym, max_abs = "", 0.0
    for sym, p in by_sym.items():
        if abs(p) > max_abs:
            max_sym, max_abs = sym, abs(p)
    pct = max_abs / total_abs * 100
    return Criterion(
        name=name,
        threshold=f"<{max_pct}%",
        actual=f"{pct:.1f}% ({max_sym})",
        status="PASS" if pct < max_pct else "FAIL",
    )


def check_recent_window_stability(trades: list[Trade], window_n: int,
                                  modeled_fee_bps: float, modeled_slip_bps: float,
                                  tolerance: float, name: str) -> Criterion:
    """Last `window_n` trades — fee AND slip both within `tolerance` of modeled.
    Both metrics must pass; if either fails, the whole gate fails. Reports
    the worse-performing metric in `actual` so the operator sees what's drifting.

    Reasoning: pre-reg requires "fee/slip stable" — interpreting the
    conjunction strictly. If only one is checked, the other could drift
    silently across stages.
    """
    qualifying = [t for t in trades if t.notional_usd > 0]
    if len(qualifying) < window_n:
        return Criterion(
            name=name,
            threshold=f"last {window_n} trades stable",
            actual=f"only {len(qualifying)} trades w/ cost data (need ≥{window_n})",
            status="PENDING",
        )
    window = qualifying[-window_n:]
    total_notional = sum(t.notional_usd for t in window) or 1.0
    fee_bps = sum(t.fee_usd for t in window) / total_notional * 10000.0
    losers = [t for t in window if t.outcome == "STOP"]
    fee_ceiling = modeled_fee_bps * (1 + tolerance)
    slip_ceiling = modeled_slip_bps * (1 + tolerance)
    threshold = (f"fee≤{fee_ceiling:.2f}bp + slip≤{slip_ceiling:.2f}bp "
                 f"(±{tolerance*100:.0f}%)")
    # No losers in the window means slip is unmeasurable. The pre-reg
    # gate is "fee/slip stable" (conjunction). Asserting stability of an
    # unmeasured metric is a fail-open — if a future cohort hits a
    # 30-trade winning streak, this branch would have granted PASS
    # without any slip evidence. Force PENDING instead so the operator
    # waits for losers before promoting at moderate-stake stages.
    if not losers:
        return Criterion(
            name=name,
            threshold=threshold,
            actual=f"fee {fee_bps:.2f}bp; slip unmeasurable (0 losers in window)",
            status="PENDING",
        )
    loser_notional = sum(t.notional_usd for t in losers) or 1.0
    slip_bps = sum(t.slip_usd for t in losers) / loser_notional * 10000.0
    fee_pass = fee_bps <= fee_ceiling
    slip_pass = slip_bps <= slip_ceiling
    return Criterion(
        name=name,
        threshold=threshold,
        actual=f"fee {fee_bps:.2f}bp; slip {slip_bps:.2f}bp",
        status="PASS" if (fee_pass and slip_pass) else "FAIL",
    )


def check_recent_net_positive(trades: list[Trade], window_days: int,
                              n_floor: int, name: str) -> Criterion:
    """Net-positive over the last `window_days` calendar days.

    n_floor at window scope: if fewer than n_floor trades fall in the
    window, PENDING — too few data points for "is it net-positive in
    this window" to be a meaningful question.
    """
    cutoff = datetime.now(timezone.utc) - timedelta(days=window_days)
    in_window = [t for t in trades
                 if (parse_iso(t.ts) or datetime.min.replace(tzinfo=timezone.utc)) >= cutoff]
    if len(in_window) < n_floor:
        return Criterion(
            name=name,
            threshold=">$0 over last {}d".format(window_days),
            actual=f"only {len(in_window)} trades in window (need ≥{n_floor})",
            status="PENDING",
        )
    pnl = sum(t.pnl_usd for t in in_window)
    return Criterion(
        name=name,
        threshold=f">$0 over last {window_days}d",
        actual=f"${pnl:+,.0f} (n={len(in_window)})",
        status="PASS" if pnl > 0 else "FAIL",
    )


def check_annualized_at_stage(trades: list[Trade], stage_start: datetime,
                              fraction: float, name: str,
                              n_floor: int) -> Criterion:
    """Annualized realized NET ≥ HONEST_ANNUAL × elapsed × fraction.

    Same form as STAGE_0 #4 (check_pro_rated_annual) but parameterized
    by stage_start (not "first trade") and fraction (0.50 vs 0.60).

    The metric is realized NET over the stage window vs the threshold
    that the same window would project at full deployed scaling × fraction.
    """
    days = max(1, (datetime.now(timezone.utc) - stage_start).days)
    elapsed_yr = days / 365.25
    target = HONEST_ANNUAL_USD * elapsed_yr * fraction
    pnl = sum(t.pnl_usd for t in trades)
    threshold = (f"≥${target:,.0f} (=${HONEST_ANNUAL_USD/1000:.0f}k×"
                 f"{elapsed_yr:.2f}y×{fraction})")
    if len(trades) < n_floor:
        return Criterion(name, threshold,
                         f"${pnl:+,.0f} (n<{n_floor})", "PENDING")
    return Criterion(
        name=name,
        threshold=threshold,
        actual=f"${pnl:+,.0f}",
        status="PASS" if pnl >= target else "FAIL",
    )


# ── STAGE_1 → STAGE_2 ($100 → $300) ─────────────────────────────────────────


def evaluate_stage_1_to_2(all_trades: list[Trade], drift_history: Path,
                          stage_1_start: datetime) -> list[Criterion]:
    """STAGE_1 → STAGE_2. 7 gates per pre-reg.

    Trade segmentation: only trades with ts >= stage_1_start count
    (filters out residual paper trades from STAGE_0)."""
    s1_trades = filter_trades_after(all_trades, stage_1_start)
    return [
        check_n_trades(
            s1_trades, S1_MIN_TRADES,
            "1. ≥50 closed STAGE_1 trades"),
        check_days_at_stage(
            stage_1_start, S1_MIN_DAYS,
            "2. ≥30 calendar days at STAGE_1"),
        check_fee_within_tolerance(
            s1_trades, MODELED_FEE_BPS, S1_FEE_TOLERANCE,
            "3. Realized fee within 5% of modeled"),
        check_slip_within_tolerance(
            s1_trades, MODELED_SLIP_BPS, S1_SLIP_TOLERANCE,
            "4. Realized slip within 20% of modeled"),
        check_drift_clean_at_stage(
            drift_history, S1_DRIFT_CLEAN_DAYS,
            "5. Drift detector clean ≥30d"),
        check_no_daily_loss_above(
            s1_trades, S1_PER_TRADE_STAKE_USD * S1_DAILY_LOSS_MULT,
            S1_MIN_TRADES,
            f"6. No single-day loss >${S1_PER_TRADE_STAKE_USD * S1_DAILY_LOSS_MULT:.0f}"
            f" ({S1_DAILY_LOSS_MULT:.0f}× ${S1_PER_TRADE_STAKE_USD:.0f} stake)"),
        check_net_positive_at_stage(
            s1_trades, S1_MIN_TRADES,
            "7. Net-positive cumulative at STAGE_1"),
    ]


# ── STAGE_2 → STAGE_3 ($300 → $500) ─────────────────────────────────────────


def evaluate_stage_2_to_3(all_trades: list[Trade], drift_history: Path,
                          stage_1_start: datetime,
                          stage_2_start: datetime) -> list[Criterion]:
    """STAGE_2 → STAGE_3. 6 gates.

    Segmentation:
      s12_trades = trades since stage_1_start (cumulative S1+S2)
      Stage_2_start drives "≥60d at STAGE_2" only.
    """
    s12_trades = filter_trades_after(all_trades, stage_1_start)
    return [
        check_n_trades(
            s12_trades, S2_MIN_TRADES_CUMULATIVE,
            "1. ≥100 closed trades cumulative (S1+S2)"),
        check_days_at_stage(
            stage_2_start, S2_MIN_DAYS,
            "2. ≥60 calendar days at STAGE_2"),
        check_recent_window_stability(
            s12_trades, S2_RECENT_WINDOW_TRADES,
            MODELED_FEE_BPS, MODELED_SLIP_BPS, S2_RECENT_TOLERANCE,
            f"3. Fee/slip stable in last {S2_RECENT_WINDOW_TRADES} trades"),
        check_drift_clean_at_stage(
            drift_history, S2_DRIFT_CLEAN_DAYS,
            "4. Drift detector clean ≥45d"),
        check_recent_net_positive(
            s12_trades, S2_RECENT_NET_DAYS, S2_RECENT_WINDOW_TRADES,
            f"5. Net-positive over most recent {S2_RECENT_NET_DAYS}d"),
        check_single_symbol_at_stage(
            s12_trades, S2_MAX_SINGLE_SYM_PCT, S2_MIN_TRADES_CUMULATIVE,
            f"6. No single symbol >{S2_MAX_SINGLE_SYM_PCT:.0f}% S1+S2 PnL"),
    ]


# ── STAGE_3 → STAGE_4 ($500 → $1,000) ───────────────────────────────────────


def evaluate_stage_3_to_4(all_trades: list[Trade], drift_history: Path,
                          stage_1_start: datetime, stage_2_start: datetime,
                          stage_3_start: datetime) -> list[Criterion]:
    """STAGE_3 → STAGE_4. 6 gates — the final promotion to deployed size.

    Segmentation (three nested windows):
      s123_trades = trades since stage_1_start (all real-money cumulative)
      s23_trades  = trades since stage_2_start (S2+S3 for net-positive #5)
      s3_trades   = trades since stage_3_start (S3 alone for #2/#6)

    Note: stage_3_start is used for both the days-at-stage gate AND the
    annualized-NET gate, since "elapsed" in the pre-reg formula
    "$69k/yr × elapsed × 0.50" is most naturally interpreted as time at
    STAGE_3 (the stage we're trying to leave). Reading "elapsed" as
    total real-money time gives a stale denominator and inflates the
    threshold past what the strategy could realistically clear at any
    stake.
    """
    s123_trades = filter_trades_after(all_trades, stage_1_start)
    s23_trades = filter_trades_after(all_trades, stage_2_start)
    s3_trades = filter_trades_after(all_trades, stage_3_start)
    # Stable-cost window: last 90d of trades (S2+S3 in practice).
    s23_recent_cutoff = (datetime.now(timezone.utc)
                         - timedelta(days=S3_STABLE_WINDOW_DAYS))
    s23_recent_window = filter_trades_after(s23_trades, s23_recent_cutoff)
    return [
        check_n_trades(
            s123_trades, S3_MIN_TRADES_CUMULATIVE,
            "1. ≥200 closed trades cumulative (S1+S2+S3)"),
        check_days_at_stage(
            stage_3_start, S3_MIN_DAYS,
            "2. ≥90 calendar days at STAGE_3"),
        check_recent_window_stability(
            s23_recent_window, S2_RECENT_WINDOW_TRADES,
            MODELED_FEE_BPS, MODELED_SLIP_BPS, S3_STABLE_TOLERANCE,
            f"3. Fee/slip stable across last {S3_STABLE_WINDOW_DAYS}d (S2+S3)"),
        check_drift_clean_at_stage(
            drift_history, S3_DRIFT_CLEAN_DAYS,
            "4. Drift detector clean ≥60d"),
        check_net_positive_at_stage(
            s23_trades, S2_MIN_TRADES_CUMULATIVE,
            "5. Net-positive cumulative across S2+S3"),
        check_annualized_at_stage(
            s3_trades, stage_3_start, S3_HONEST_FRACTION,
            "6. Annualized NET ≥50% pro-rated at STAGE_3",
            S2_RECENT_WINDOW_TRADES),
    ]


# ── Verdict + render ─────────────────────────────────────────────────────────


def verdict_of(criteria: list[Criterion]) -> tuple[str, int]:
    """Map the per-criterion statuses to an overall verdict + exit code."""
    if any(c.status == "FAIL" for c in criteria):
        return "BLOCKED — at least one gate fails outright. Do not promote.", 1
    if any(c.status == "PENDING" for c in criteria):
        return "WAITING — insufficient data on ≥1 gate. Continue forward-paper accumulation.", 2
    # All PASS or DEFERRED.
    # Exit 4 (not 0) on DEFERRED-but-otherwise-PASS so weekly_audit.sh can
    # distinguish "operator must verify deferred gate" (WARN tier) from
    # "all gates mechanically passed, deploy now" (CRITICAL tier). Pre-fix,
    # a transient Binance API blip during the BTC-HODL helper call would
    # have triggered a CRITICAL "PROMOTION READY" Telegram on every weekly
    # run until the helper succeeded again.
    deferred = [c for c in criteria if c.status == "DEFERRED"]
    if deferred:
        return (f"PROMOTE-CANDIDATE — {len(deferred)} gate(s) require manual verification "
                f"(see DEFERRED rows). Operator must verify before flipping the next-stage executor."), 4
    return "PROMOTE — all locked gates pass. Operator may flip to the next stage.", 0


def render(criteria: list[Criterion], label: str | None = None) -> str:
    """Render the per-criterion table.

    label: header text for the report — defaults to "STAGE_0 → STAGE_1
    promotion check" for backward compatibility with pre-multi-stage
    callers (test_promote_path_all_gates_pass, weekly_audit cron).
    """
    sep = "═" * 96
    out = [sep]
    out.append(f"  {label or 'STAGE_0 → STAGE_1 promotion check'}")
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
    ap.add_argument("--from-stage", choices=tuple(STAGE_TRANSITIONS.keys()),
                    default="STAGE_0",
                    help="Which stage transition to evaluate (default: STAGE_0 → STAGE_1)")
    ap.add_argument("--stage-1-start", default="",
                    help="ISO ts when STAGE_1 began. Required for --from-stage STAGE_1 or STAGE_2.")
    ap.add_argument("--stage-2-start", default="",
                    help="ISO ts when STAGE_2 began. Required for --from-stage STAGE_2 or STAGE_3.")
    ap.add_argument("--stage-3-start", default="",
                    help="ISO ts when STAGE_3 began. Required for --from-stage STAGE_3.")
    ap.add_argument("--live-source", choices=("vps", "local"), default="vps")
    ap.add_argument("--vps", default="root@178.105.24.230")
    ap.add_argument("--live-dir", default="/var/log/paper-live/journal")
    ap.add_argument("--drift-history", type=Path,
                    default=Path("results/drift_check_history.jsonl"),
                    help="Path to drift_check_history.jsonl (per-machine)")
    args = ap.parse_args()

    # Validate required stage-start args BEFORE fetching data — fast-fail
    # on missing input, never default to a fake value that fail-opens.
    _, label, required = STAGE_TRANSITIONS[args.from_stage]
    stage_starts: dict[str, datetime] = {}
    name_to_arg = {
        "stage_1_start": ("--stage-1-start", args.stage_1_start),
        "stage_2_start": ("--stage-2-start", args.stage_2_start),
        "stage_3_start": ("--stage-3-start", args.stage_3_start),
    }
    for req in required:
        flag_name, value = name_to_arg[req]
        try:
            stage_starts[req] = parse_stage_start_arg(value, flag_name)
        except ValueError as e:
            print(f"error: {e}", file=sys.stderr)
            return 3

    # Chronological-order validation. Without this, an operator typo
    # (swapped --stage-1-start and --stage-2-start) silently produces
    # nonsense filtered windows: filter_trades_after(s2_start) returns a
    # SUPERSET of filter_trades_after(s1_start) when s2_start < s1_start,
    # so pre-stage_1 paper trades count toward STAGE_2 cumulative
    # metrics. Reject the inversion loudly at exit 3.
    chrono_order = ["stage_1_start", "stage_2_start", "stage_3_start"]
    present = [k for k in chrono_order if k in stage_starts]
    for earlier, later in zip(present, present[1:]):
        if stage_starts[earlier] >= stage_starts[later]:
            print(
                f"error: stage-start timestamps must be strictly chronological. "
                f"--{earlier.replace('_', '-')} ({stage_starts[earlier].date()}) "
                f"must be before --{later.replace('_', '-')} "
                f"({stage_starts[later].date()})",
                file=sys.stderr,
            )
            return 3

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

    if args.from_stage == "STAGE_0":
        criteria = evaluate_stage_0_to_1(trades, args.drift_history)
    elif args.from_stage == "STAGE_1":
        criteria = evaluate_stage_1_to_2(
            trades, args.drift_history, stage_starts["stage_1_start"])
    elif args.from_stage == "STAGE_2":
        criteria = evaluate_stage_2_to_3(
            trades, args.drift_history,
            stage_starts["stage_1_start"], stage_starts["stage_2_start"])
    elif args.from_stage == "STAGE_3":
        criteria = evaluate_stage_3_to_4(
            trades, args.drift_history,
            stage_starts["stage_1_start"], stage_starts["stage_2_start"],
            stage_starts["stage_3_start"])
    else:
        # Should be unreachable due to argparse choices=, but be explicit.
        print(f"error: unknown stage {args.from_stage}", file=sys.stderr)
        return 3

    print(render(criteria, label), end="")
    _, code = verdict_of(criteria)
    return code


if __name__ == "__main__":
    sys.exit(main())
