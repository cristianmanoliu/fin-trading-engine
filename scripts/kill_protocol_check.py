#!/usr/bin/env python3
"""
kill_protocol_check.py — mechanical evaluation of the locked kill
criteria from results/real_money_protocol_decision_rule_2026-05-08.md
§"Kill criteria (LOCKED)".

Symmetric counterpart to stage_promotion_check.py: when forward-paper
runs, the operator faces TWO decisions, not one:

  promotion: should we go to the next stage?  → stage_promotion_check.py
  kill:      should we stop the entire protocol?  → THIS

Kill fires earlier in the timeline than any promotion (which needs
months of accumulation), so this tool's value is more time-sensitive.

Locked criteria (per pre-reg §Kill, 6 listed; 4 are mechanizable here,
2 are operator-judgment):

  1. Drift detector confirmed fire at α=0.001:
       - Two firings ≥7 days apart, OR
       - Single firing combined with forward_paper_status advisory KILL
     [MECHANIZED: reads results/drift_check_history.jsonl]
  2. Realized stop-side slip >30 bp sustained over ≥30 most recent trades
     [MECHANIZED: rolling 30-trade window]
  3. Three consecutive calendar days each net-loss exceeding 5× current
     stage stake
     [PARTIAL: needs current stage info; v1 evaluates against $1k stake
      assumption — operator must verify stage; explicit OPERATOR-VERIFY tag]
  4. Single-symbol concentration >50% of cumulative real-money PnL
     [MECHANIZED]
  5. Engine emits unrecoverable error (margin call, liquidation, ban)
     [DEFERRED: requires log-pattern detection across systemd journals;
      operator-verify against post_deploy_check.sh §5 and Telegram alerts]
  6. Drawdown >20% over any 60-day window
     [MECHANIZED: rolling window of cumulative pnl peak/trough]

Exit codes:
  0  CONTINUE — no kill criterion fires; forward-paper continues
  1  KILL — at least one criterion fires; operator MUST stop the protocol
     (refer to pre-reg §"When kill fires" for the kill execution path)
  2  WAITING — insufficient data; some criteria not yet evaluable
  3  ERROR — input/env failure
  4  OPERATOR-VERIFY — mechanical criteria all pass, but operator must
     check the deferred criteria (#3 stage-stake, #5 unrecoverable error)
     before declaring CONTINUE

Usage:
  python3 scripts/kill_protocol_check.py
  python3 scripts/kill_protocol_check.py --stage STAGE_0
  python3 scripts/kill_protocol_check.py --live-source local
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

# Locked thresholds. CHANGE ONLY by editing the pre-reg + this file.
SLIP_KILL_BPS = 30.0
SLIP_WINDOW_TRADES = 30
SINGLE_SYM_KILL_PCT = 50.0
DRAWDOWN_KILL_PCT = 20.0
DRAWDOWN_WINDOW_DAYS = 60
DAILY_LOSS_MULT = 5.0  # 5× per-trade stake
DAILY_LOSS_CONSECUTIVE = 3

# Stage-stake table (from pre-reg §Stages).
STAGE_STAKE = {
    "STAGE_0": 1000.0,  # paper-money default
    "STAGE_1": 100.0,
    "STAGE_2": 300.0,
    "STAGE_3": 500.0,
    "STAGE_4": 1000.0,
}

# Drift-history freshness — see stage_promotion_check.DRIFT_HISTORY_STALE_DAYS.
# Mirror constant kept independent (no cross-script import) to avoid coupling
# the two tools through anything other than their shared file format.
DRIFT_HISTORY_STALE_DAYS = 14


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
    status: str  # CONTINUE / KILL / PENDING / OPERATOR-VERIFY / DEFERRED


def parse_iso(ts: str) -> datetime | None:
    if not ts:
        return None
    try:
        clean = ts.rstrip("Z")
        if "." in clean:
            clean = clean.split(".", 1)[0]
        return datetime.fromisoformat(clean).replace(tzinfo=timezone.utc)
    except ValueError:
        return None


def load_journal(journal_dir: Path) -> list[Trade]:
    """Load LIVE-cohort closes only, sorted chronologically."""
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
    out.sort(key=lambda t: t.ts)
    return out


def fetch_remote(vps: str, remote_dir: str) -> list[Trade]:
    cmd = f'ssh {vps} "tar -cf - -C {remote_dir} . 2>/dev/null"'
    with tempfile.TemporaryDirectory() as tmp:
        result = subprocess.run(cmd, shell=True, check=True, capture_output=True)
        subprocess.run(["tar", "-xf", "-", "-C", tmp], input=result.stdout, check=True)
        return load_journal(Path(tmp))


# ── Per-criterion evaluators ─────────────────────────────────────────────────


def check_drift_two_firings(history_path: Path) -> Criterion:
    """Kill criterion #1: two drift-detector firings ≥7 days apart in history.
    The single-firing-combined-with-status-KILL variant requires forward_paper_status
    output parsing; v1 reports just the two-firings-≥7d form.

    Fail-open closed: previously, an empty/corrupt history file produced
    "0 firings" → CONTINUE → green light despite no monitoring at all.
    We now distinguish (a) zero parseable runs entirely (file empty or
    corrupt — PENDING) from (b) parseable runs with zero firings (genuine
    clean — CONTINUE). Also flags stale histories (most recent run >14d
    old → PENDING) to catch silently-stopped drift cron.
    """
    if not history_path.exists():
        return Criterion(
            "1. Drift detector confirmed fire (≥2 firings, ≥7d apart)",
            "0 firings",
            "(history missing)",
            "PENDING",
        )
    firings: list[datetime] = []
    most_recent_run: datetime | None = None
    total_runs = 0
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
        total_runs += 1
        if most_recent_run is None or ts > most_recent_run:
            most_recent_run = ts
        if ev.get("verdict") == "DRIFT_FIRED":
            firings.append(ts)
    firings.sort()
    if total_runs == 0:
        return Criterion(
            "1. Drift detector confirmed fire (≥2 firings, ≥7d apart)",
            "0 firings",
            "(history file exists but has no parseable runs)",
            "PENDING",
        )
    # Stale-history gate: if the cron silently stopped, a long-ago "clean"
    # history would forever return CONTINUE. Refuse to grant CONTINUE
    # without recent monitoring evidence.
    days_since_recent = (datetime.now(timezone.utc) - most_recent_run).days
    if days_since_recent > DRIFT_HISTORY_STALE_DAYS:
        return Criterion(
            "1. Drift detector confirmed fire (≥2 firings, ≥7d apart)",
            f"<2 firings, fresh ≤{DRIFT_HISTORY_STALE_DAYS}d",
            f"stale: last run {days_since_recent}d ago "
            f"({most_recent_run.date()}), drift cron may be inactive",
            "PENDING",
        )
    if not firings:
        return Criterion(
            "1. Drift detector confirmed fire (≥2 firings, ≥7d apart)",
            "0 firings",
            f"0 firings ({total_runs} clean runs, last {most_recent_run.date()})",
            "CONTINUE",
        )
    # Find any pair ≥7 days apart.
    for i in range(len(firings)):
        for j in range(i + 1, len(firings)):
            if (firings[j] - firings[i]).days >= 7:
                return Criterion(
                    "1. Drift detector confirmed fire (≥2 firings, ≥7d apart)",
                    "<2 firings or <7d apart",
                    f"PAIR: {firings[i].date()} ↔ {firings[j].date()}",
                    "KILL",
                )
    return Criterion(
        "1. Drift detector confirmed fire (≥2 firings, ≥7d apart)",
        "<2 firings or <7d apart",
        f"{len(firings)} firing(s), max gap <7d",
        "CONTINUE",
    )


def check_recent_slip(trades: list[Trade]) -> Criterion:
    """Kill #2: realized slip >30bp over the most recent 30-trade window."""
    losers = [t for t in trades if t.notional_usd > 0 and t.outcome == "STOP"]
    if len(losers) < SLIP_WINDOW_TRADES:
        return Criterion(
            f"2. Slip >{SLIP_KILL_BPS}bp over last {SLIP_WINDOW_TRADES} losers",
            f"<{SLIP_KILL_BPS}bp",
            f"only {len(losers)} losers",
            "PENDING",
        )
    window = losers[-SLIP_WINDOW_TRADES:]
    total_slip = sum(t.slip_usd for t in window)
    total_notional = sum(t.notional_usd for t in window)
    bps = total_slip / total_notional * 10000.0
    return Criterion(
        f"2. Slip >{SLIP_KILL_BPS}bp over last {SLIP_WINDOW_TRADES} losers",
        f"≤{SLIP_KILL_BPS}bp",
        f"{bps:.2f}bp",
        "KILL" if bps > SLIP_KILL_BPS else "CONTINUE",
    )


def check_consecutive_daily_loss(trades: list[Trade], stage: str) -> Criterion:
    """Kill #3: three consecutive calendar days each net-loss > 5× stage stake.
    Operator-judgment-adjacent because it depends on stage; we report the
    mechanical result + tag with OPERATOR-VERIFY for stake confirmation."""
    stake = STAGE_STAKE.get(stage)
    if stake is None:
        return Criterion(
            f"3. 3 consecutive days each loss >{DAILY_LOSS_MULT}× stake ({stage})",
            f"-${0:.0f}/day",
            f"unknown stage: {stage}",
            "PENDING",
        )
    threshold = -stake * DAILY_LOSS_MULT
    if not trades:
        return Criterion(
            f"3. 3 consecutive days each loss >{DAILY_LOSS_MULT}× stake ({stage})",
            f"<{DAILY_LOSS_CONSECUTIVE} consecutive days",
            "no trades",
            "PENDING",
        )
    # Filter trades through parse_iso so corrupt/empty ts entries are
    # dropped here rather than crashing the strptime call below. Pre-fix
    # path: a single trade with t.ts="" raised ValueError → uncaught →
    # script exited 1 → weekly_audit interpreted exit 1 as KILL → fired a
    # false CRITICAL Telegram "STOP THE PROTOCOL". The fix isolates ts
    # parsing to one well-defined boundary (parse_iso) which already
    # returns None on malformed input.
    by_day: dict[str, float] = {}
    for t in trades:
        parsed = parse_iso(t.ts)
        if parsed is None:
            continue
        d = parsed.date().isoformat()
        by_day[d] = by_day.get(d, 0.0) + t.pnl_usd
    if not by_day:
        # Every trade had a corrupt ts — we have data but can't time-grade
        # it. PENDING (not CONTINUE) so the operator notices the journal
        # is broken instead of receiving a falsely-clean kill verdict.
        return Criterion(
            f"3. 3 consecutive days each loss >{DAILY_LOSS_MULT}× stake (${stake:.0f}, {stage})",
            f"<{DAILY_LOSS_CONSECUTIVE} consecutive days < ${threshold:.0f}",
            f"all {len(trades)} trade timestamps unparseable",
            "PENDING",
        )
    days_sorted = sorted(by_day.keys())
    consecutive = 0
    bad_runs: list[list[str]] = []
    current_run: list[str] = []
    prev_date: datetime | None = None
    for day_str in days_sorted:
        if by_day[day_str] >= threshold:
            consecutive = 0
            current_run = []
            prev_date = None
            continue
        day_dt = datetime.strptime(day_str, "%Y-%m-%d")
        if prev_date is not None and (day_dt - prev_date).days > 1:
            consecutive = 0
            current_run = []
        consecutive += 1
        current_run.append(day_str)
        prev_date = day_dt
        if consecutive >= DAILY_LOSS_CONSECUTIVE:
            bad_runs.append(list(current_run))
    actual = ("worst run: " + ", ".join(bad_runs[-1])) if bad_runs else "no qualifying run"
    status = "KILL" if bad_runs else (
        "OPERATOR-VERIFY" if stage == "STAGE_0" else "CONTINUE")
    # OPERATOR-VERIFY for STAGE_0 because the stake assumption ($1k for paper)
    # may not match what the operator considers "stake" for kill purposes.
    return Criterion(
        f"3. 3 consecutive days each loss >{DAILY_LOSS_MULT}× stake (${stake:.0f}, {stage})",
        f"<{DAILY_LOSS_CONSECUTIVE} consecutive days < ${threshold:.0f}",
        actual,
        status,
    )


def check_single_symbol_kill(trades: list[Trade]) -> Criterion:
    """Kill #4: single-symbol >50% of cumulative pnl.
    More permissive than promotion #6 (40% bar) because pre-reg recognized
    40% concentration as natural variance; 50% indicates real risk.

    n_floor=30 matches the drift detector's calibrated minimum: with <30
    closed trades, "single-symbol concentration" is sampling variance,
    not a real signal. Pre-reg is silent on n_floor; this is the
    pragmatic implementation choice. Without the gate, the script
    would fire spurious KILL at n=1 (any single trade is 100%).
    """
    SINGLE_SYM_N_FLOOR = 30
    if len(trades) < SINGLE_SYM_N_FLOOR:
        return Criterion(
            f"4. Single-symbol >{SINGLE_SYM_KILL_PCT}% PnL",
            f"<{SINGLE_SYM_KILL_PCT}%",
            f"only {len(trades)} trades (need ≥{SINGLE_SYM_N_FLOOR})",
            "PENDING",
        )
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
        f"4. Single-symbol >{SINGLE_SYM_KILL_PCT}% PnL",
        f"≤{SINGLE_SYM_KILL_PCT}%",
        f"{pct:.1f}% ({max_sym})",
        "KILL" if pct > SINGLE_SYM_KILL_PCT else "CONTINUE",
    )


def check_unrecoverable_error_deferred() -> Criterion:
    """Kill #5: unrecoverable engine/exchange error. Detection requires
    log-pattern matching against systemd journals — outside the scope of
    a journal-data-only check. Telegram CRITICAL alerts + post_deploy_check
    §5 (ERROR-level events) cover this in practice."""
    return Criterion(
        "5. Unrecoverable engine/exchange error",
        "operator-verify (Telegram + post_deploy_check §5)",
        "(deferred)",
        "DEFERRED",
    )


def check_drawdown(trades: list[Trade]) -> Criterion:
    """Kill #6: drawdown >20% over any 60-day window. Computes max drawdown
    on running cumulative pnl. v1 uses the full trade history; the 60-day
    window constraint is approximated by skipping the check if total span
    is <60d (PENDING)."""
    if not trades:
        return Criterion(
            f"6. Drawdown >{DRAWDOWN_KILL_PCT}% over {DRAWDOWN_WINDOW_DAYS}d window",
            f"≤{DRAWDOWN_KILL_PCT}%",
            "no trades",
            "PENDING",
        )
    parsed = [(parse_iso(t.ts), t.pnl_usd) for t in trades]
    parsed = [(ts, p) for ts, p in parsed if ts is not None]
    if not parsed:
        return Criterion(
            f"6. Drawdown >{DRAWDOWN_KILL_PCT}% over {DRAWDOWN_WINDOW_DAYS}d window",
            f"≤{DRAWDOWN_KILL_PCT}%",
            "(timestamp parse failed)",
            "PENDING",
        )
    span_days = (parsed[-1][0] - parsed[0][0]).days
    if span_days < DRAWDOWN_WINDOW_DAYS:
        return Criterion(
            f"6. Drawdown >{DRAWDOWN_KILL_PCT}% over {DRAWDOWN_WINDOW_DAYS}d window",
            f"≤{DRAWDOWN_KILL_PCT}%",
            f"only {span_days}d of history",
            "PENDING",
        )
    # Rolling window of the last 60 days; track peak + trough.
    window_start = parsed[-1][0] - timedelta(days=DRAWDOWN_WINDOW_DAYS)
    window = [(ts, p) for ts, p in parsed if ts >= window_start]
    cum = 0.0
    peak = 0.0
    max_dd_abs = 0.0
    for _, p in window:
        cum += p
        if cum > peak:
            peak = cum
        dd_abs = peak - cum
        if dd_abs > max_dd_abs:
            max_dd_abs = dd_abs
    # Drawdown as % of peak (when peak > 0). When peak ≤ 0 the strategy never
    # had a positive equity curve in this window — kill criterion shape doesn't
    # cleanly apply; report PENDING.
    if peak <= 0:
        return Criterion(
            f"6. Drawdown >{DRAWDOWN_KILL_PCT}% over {DRAWDOWN_WINDOW_DAYS}d window",
            f"≤{DRAWDOWN_KILL_PCT}%",
            f"peak≤0 in window (${peak:.0f}); kill-shape n/a",
            "PENDING",
        )
    dd_pct = max_dd_abs / peak * 100
    return Criterion(
        f"6. Drawdown >{DRAWDOWN_KILL_PCT}% over {DRAWDOWN_WINDOW_DAYS}d window",
        f"≤{DRAWDOWN_KILL_PCT}%",
        f"{dd_pct:.1f}% (peak ${peak:.0f}, trough ${peak-max_dd_abs:.0f})",
        "KILL" if dd_pct > DRAWDOWN_KILL_PCT else "CONTINUE",
    )


# ── Verdict + render ─────────────────────────────────────────────────────────


def verdict_of(criteria: list[Criterion]) -> tuple[str, int]:
    if any(c.status == "KILL" for c in criteria):
        return ("KILL — at least one kill criterion fires. Stop the protocol "
                "immediately per pre-reg §'When kill fires' (cease new positions, "
                "let existing positions close at stops/targets, mark milestone KILLED, "
                "no auto-resumption)."), 1
    if any(c.status == "PENDING" for c in criteria):
        return ("WAITING — insufficient data on ≥1 criterion. Continue forward-paper "
                "monitoring."), 2
    has_op_verify = any(c.status == "OPERATOR-VERIFY" for c in criteria)
    has_deferred = any(c.status == "DEFERRED" for c in criteria)
    if has_op_verify or has_deferred:
        n = sum(1 for c in criteria if c.status in ("OPERATOR-VERIFY", "DEFERRED"))
        return (f"OPERATOR-VERIFY — mechanical kill criteria pass, but {n} "
                f"criterion/criteria require operator confirmation (see DEFERRED / "
                f"OPERATOR-VERIFY rows). Default action: CONTINUE if those check out."), 4
    return "CONTINUE — no kill criterion fires; forward-paper continues.", 0


def render(criteria: list[Criterion], stage: str) -> str:
    sep = "═" * 100
    out = [sep]
    out.append(f"  Kill-protocol check (stage: {stage})")
    out.append("  Source: results/real_money_protocol_decision_rule_2026-05-08.md §Kill criteria")
    out.append(sep)
    out.append("")
    out.append(f"  {'criterion':<58}  {'threshold':<28}  {'actual':<28}  status")
    out.append("  " + "-" * 98)
    for c in criteria:
        out.append(f"  {c.name:<58}  {c.threshold:<28}  {c.actual:<28}  [{c.status}]")
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
                    default=Path("results/drift_check_history.jsonl"))
    ap.add_argument("--stage", default="STAGE_0",
                    choices=tuple(STAGE_STAKE.keys()),
                    help="Current stage for the per-stage stake-multiple check")
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
        check_drift_two_firings(args.drift_history),
        check_recent_slip(trades),
        check_consecutive_daily_loss(trades, args.stage),
        check_single_symbol_kill(trades),
        check_unrecoverable_error_deferred(),
        check_drawdown(trades),
    ]
    print(render(criteria, args.stage), end="")
    _, code = verdict_of(criteria)
    return code


if __name__ == "__main__":
    sys.exit(main())
