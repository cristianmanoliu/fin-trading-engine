#!/usr/bin/env python3
"""
realized_cost_trajectory.py — track realized fee_bps and slip_bps over
trade index, surfacing trend instead of just the running average.

forward_paper_status.sh shows the cumulative average:

    Realized fee bps:    10.00   / ≤12bp   [PASS]
    Realized slip bps:    5.00   / ≤25bp   [PASS]

That's the deploy-decision gate (CLAUDE.md ## Forward-paper go/no-go).
But a slowly-drifting cost stack — fee at 10 → 10.5 → 11 → 11.5 → 12 —
would average to a passing number while clearly trending toward the
kill cliff. This script surfaces the trend by walking trades in order
and printing each trade's realized cost along with the running average.

Cost decomposition is sourced from the journal entries' cost-decomp
schema (notional_usd > 0 gate skips pre-extension closes that lack
the columns). Slip is computed only on losing trades (outcome=STOP)
because slippage is modeled on losers only — winners are limit fills.

Reads journals via local filesystem OR ssh to VPS, identical to
live_vs_backtest_drift.py (so the same operator credentials work).

Exit codes:
  0  rendered ≥1 trajectory point
  2  no qualifying trades found (no closes with cost decomp yet)
  3  input error (bad path / unreachable host / etc)

Usage:
  python3 scripts/realized_cost_trajectory.py
  python3 scripts/realized_cost_trajectory.py --live-source local --live-dir ./logs/journal
  python3 scripts/realized_cost_trajectory.py --cohort live
"""
from __future__ import annotations

import argparse
import json
import shlex
import subprocess
import sys
import tempfile
from dataclasses import dataclass
from pathlib import Path

# Locked thresholds from CLAUDE.md ## Forward-paper go/no-go criteria.
MODELED_FEE_BPS = 10.0
MODELED_SLIP_BPS = 5.0
KILL_FEE_BPS = 12.0
KILL_SLIP_BPS = 25.0


@dataclass
class TradeCost:
    cohort: str
    ts: str
    symbol: str
    outcome: str
    pnl_usd: float
    fee_usd: float
    slip_usd: float
    notional_usd: float

    @property
    def fee_bps(self) -> float:
        if self.notional_usd <= 0:
            return 0.0
        return self.fee_usd / self.notional_usd * 10000.0

    @property
    def slip_bps(self) -> float:
        if self.notional_usd <= 0:
            return 0.0
        return self.slip_usd / self.notional_usd * 10000.0


@dataclass
class JournalStats:
    """Per-scan accounting needed to distinguish 'no closes yet' (legit
    early-stage state) from 'all closes pre-decomp' (operator forgot to
    restart engine after the cost-decomp extension landed) — the audit-
    pattern dual where empty input maps to wrong verdict."""
    files_scanned: int = 0
    total_closes: int = 0
    pre_decomp_skipped: int = 0
    partial_skipped: int = 0
    qualifying: int = 0


def load_journals(journal_dir: Path) -> tuple[list[TradeCost], JournalStats]:
    """Walk *.jsonl files (top-level + shadow subdirs) and emit closed
    trades with cost decomposition + scan stats. Cohort is derived from
    path: top-level files are 'live'; shadow/<label>/*.jsonl is
    'shadow/<label>'.

    Stats let main() distinguish three operator states that all produce
    `len(trades)==0`: (a) directory missing/empty (input error),
    (b) closes exist but none qualify (pre-decomp masking — restart
    engine), (c) no closes yet (genuine early-stage). Without (b)
    surfaced, an operator who forgets to restart engines after the
    cost-decomp extension landed sees the same green-ish 'no qualifying
    trades' message as a fresh deploy.
    """
    trades: list[TradeCost] = []
    stats = JournalStats()
    if not journal_dir.is_dir():
        return trades, stats

    # Top-level → live
    for jf in sorted(journal_dir.glob("*.jsonl")):
        stats.files_scanned += 1
        trades.extend(_parse_one(jf, "live", stats))
    # Shadow subdirs
    shadow_root = journal_dir / "shadow"
    if shadow_root.is_dir():
        for label_dir in sorted(p for p in shadow_root.iterdir() if p.is_dir()):
            for jf in sorted(label_dir.glob("*.jsonl")):
                stats.files_scanned += 1
                trades.extend(_parse_one(jf, f"shadow/{label_dir.name}", stats))
    return trades, stats


def _num(v) -> float:
    """Coerce a JSON value to float defensively. Returns 0.0 for None,
    missing, or non-numeric values. Without this, a journal entry with
    `"fee_usd": null` (or a string-encoded number) makes _parse_one
    construct a TradeCost with None in a numeric field, and the next
    fee_bps division crashes the script with TypeError. The Telegram-tier
    dual sense of the audit pattern: a Python exit-1 crash gets routed
    to the wrong cron tier when a wrapper maps "non-zero = critical."
    """
    if v is None:
        return 0.0
    try:
        return float(v)
    except (TypeError, ValueError):
        return 0.0


def _parse_one(path: Path, cohort: str, stats: JournalStats) -> list[TradeCost]:
    out: list[TradeCost] = []
    for line in path.read_text().splitlines():
        if not line:
            continue
        try:
            ev = json.loads(line)
        except json.JSONDecodeError:
            continue
        if ev.get("event") != "close":
            continue
        stats.total_closes += 1
        # Skip pre-decomp closes (notional == 0 means cost columns weren't
        # written; they show 0 fee/slip and would falsely pull averages down).
        notional = _num(ev.get("notional_usd"))
        if notional <= 0:
            stats.pre_decomp_skipped += 1
            continue
        if ev.get("outcome") == "PARTIAL":
            stats.partial_skipped += 1
            continue
        stats.qualifying += 1
        out.append(TradeCost(
            cohort=cohort,
            ts=ev.get("ts", ""),
            symbol=ev.get("symbol", "?"),
            outcome=ev.get("outcome", "?"),
            pnl_usd=_num(ev.get("pnl_usd")),
            fee_usd=_num(ev.get("fee_usd")),
            slip_usd=_num(ev.get("slip_usd")),
            notional_usd=notional,
        ))
    return out


def fetch_remote(vps: str, remote_dir: str) -> tuple[list[TradeCost], JournalStats]:
    # Avoid shell=True with f-string interpolation. The local side now uses
    # list-arg subprocess (no local shell), and the remote command quotes
    # `remote_dir` via shlex.quote so a path containing shell metacharacters
    # (`;`, `$()`, `"`) cannot inject on the remote side either. ssh accepts
    # the remote command as a single argument and runs it through the remote
    # user's shell verbatim — quoting is the right defense.
    remote_cmd = f"tar -cf - -C {shlex.quote(remote_dir)} . 2>/dev/null"
    with tempfile.TemporaryDirectory() as tmp:
        result = subprocess.run(
            ["ssh", vps, remote_cmd], check=True, capture_output=True)
        subprocess.run(
            ["tar", "-xf", "-", "-C", tmp], input=result.stdout, check=True)
        return load_journals(Path(tmp))


def render(trades: list[TradeCost], cohort_filter: str | None) -> str:
    if cohort_filter:
        trades = [t for t in trades if t.cohort == cohort_filter]
    if not trades:
        return "(no qualifying trades — closes with cost decomposition required)\n"

    # Sort chronologically across all cohorts.
    trades.sort(key=lambda t: (t.ts, t.cohort, t.symbol))

    sep = "═" * 92
    out: list[str] = [sep]
    if cohort_filter:
        out.append(f"  Realized cost trajectory — {len(trades)} trades · cohort: {cohort_filter}")
    else:
        cohorts = sorted({t.cohort for t in trades})
        out.append(f"  Realized cost trajectory — {len(trades)} trades · cohorts: {', '.join(cohorts)}")
    out.append(f"  Modeled: fee={MODELED_FEE_BPS}bp / slip={MODELED_SLIP_BPS}bp"
               f"   Kill: fee>{KILL_FEE_BPS}bp / slip>{KILL_SLIP_BPS}bp")
    out.append(sep)
    out.append("")
    out.append(f"  {'#':>3}  {'date':<10}  {'cohort':<22}  {'symbol':<12}  "
               f"{'outcome':<7}  {'fee_bp':>7}  {'slip_bp':>8}  {'cum_fee':>8}  {'cum_slip':>9}")
    out.append("  " + "-" * 88)

    cum_fee_sum = 0.0
    cum_slip_sum = 0.0
    cum_n = 0
    cum_n_losers = 0
    for i, t in enumerate(trades, start=1):
        cum_n += 1
        cum_fee_sum += t.fee_bps
        cum_fee_avg = cum_fee_sum / cum_n
        if t.outcome == "STOP":
            cum_n_losers += 1
            cum_slip_sum += t.slip_bps
            cum_slip_avg = cum_slip_sum / cum_n_losers
            slip_str = f"{t.slip_bps:>7.2f}"
        else:
            cum_slip_avg = cum_slip_sum / cum_n_losers if cum_n_losers > 0 else 0.0
            slip_str = f"{'—':>7}"
        date = t.ts[:10] if len(t.ts) >= 10 else t.ts
        flag = ""
        if t.fee_bps > KILL_FEE_BPS:
            flag += " ★fee>kill"
        if t.outcome == "STOP" and t.slip_bps > KILL_SLIP_BPS:
            flag += " ★slip>kill"
        out.append(f"  {i:>3}  {date:<10}  {t.cohort:<22}  {t.symbol:<12}  "
                   f"{t.outcome:<7}  {t.fee_bps:>6.2f}  {slip_str}  "
                   f"{cum_fee_avg:>7.2f}   {cum_slip_avg:>8.2f}{flag}")

    out.append("")
    out.append(sep)
    out.append(f"  Cumulative: n={cum_n} (losers={cum_n_losers})")
    final_fee_avg = cum_fee_sum / cum_n
    fee_status = "PASS" if final_fee_avg <= KILL_FEE_BPS else "FAIL"
    out.append(f"    fee   = {final_fee_avg:.2f} bp  / modeled {MODELED_FEE_BPS}bp / kill {KILL_FEE_BPS}bp  [{fee_status}]")
    # Slip is computed only on losing trades. With zero losers there is no
    # data to evaluate the gate against — emit PENDING (gate unevaluated)
    # rather than PASS via 0.0 ≤ 25.0. The kill-bar mis-calibration verdict
    # 2026-05-07 (memory: kill_bar_miscalibration_2026-05-07) found this
    # exact "no data = clean" failure mode in the threshold kill mechanism;
    # the audit pattern keeps the same lens applied here.
    if cum_n_losers > 0:
        final_slip_avg = cum_slip_sum / cum_n_losers
        slip_status = "PASS" if final_slip_avg <= KILL_SLIP_BPS else "FAIL"
        out.append(f"    slip  = {final_slip_avg:.2f} bp  / modeled {MODELED_SLIP_BPS}bp / kill {KILL_SLIP_BPS}bp  [{slip_status}]"
                   f"   (n_losers={cum_n_losers})")
    else:
        out.append(f"    slip  = —      bp  / modeled {MODELED_SLIP_BPS}bp / kill {KILL_SLIP_BPS}bp  [PENDING]"
                   f"   (n_losers=0; gate unevaluated until first STOP close)")
    out.append(sep)
    return "\n".join(out) + "\n"


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--live-source", choices=("vps", "local"), default="vps")
    ap.add_argument("--vps", default="root@178.105.24.230")
    ap.add_argument("--live-dir", default="/var/log/paper-live/journal",
                    help="Journal dir (remote path if vps, local path if local)")
    ap.add_argument("--cohort", default=None,
                    help="Filter to single cohort (e.g. 'live', 'shadow/bb20')")
    args = ap.parse_args()

    if args.live_source == "vps":
        try:
            trades, stats = fetch_remote(args.vps, args.live_dir)
        except subprocess.CalledProcessError as e:
            print(f"ssh fetch failed: {e}", file=sys.stderr)
            return 3
    else:
        d = Path(args.live_dir)
        if not d.is_dir():
            print(f"local journal dir not found: {d}", file=sys.stderr)
            return 3
        trades, stats = load_journals(d)

    # An empty journal dir (zero .jsonl files) is operator misconfiguration
    # — wrong path, missed mount, etc. — not an early-stage no-closes-yet
    # state. Previously this collapsed into exit 2 alongside legitimate
    # "no closes yet, keep waiting" — so a typo'd --live-dir or a remote
    # path that exists but is empty looked identical to a fresh deploy.
    if stats.files_scanned == 0:
        source = (f"remote {args.vps}:{args.live_dir}"
                  if args.live_source == "vps" else f"local {args.live_dir}")
        print(
            f"⚠ no .jsonl files found in {source} — operator "
            "misconfiguration likely (wrong path / missed mount / "
            "permissions). For genuine 'no closes yet' a journal dir "
            "still contains the open events, so an empty dir is a "
            "different failure mode.", file=sys.stderr)
        return 3

    # Distinguish "all closes pre-decomp" from "no closes yet". Both produce
    # `trades=[]` in the original code → both exit 2 → operator can't tell
    # whether to investigate stale journal format vs wait for first close.
    # Same audit pattern: missing-input shape silently maps to wrong verdict.
    if not trades and stats.total_closes > 0:
        print(
            f"⚠ {stats.total_closes} close event(s) found across "
            f"{stats.files_scanned} journal file(s), but ALL skipped "
            f"({stats.pre_decomp_skipped} pre-decomp / "
            f"{stats.partial_skipped} PARTIAL). Restart engines to "
            "enable cost-decomposition columns on new closes — old "
            "closes lacking notional_usd/fee_usd/slip_usd cannot be "
            "back-filled.", file=sys.stderr)
        # Still print a minimal summary block so the operator sees the
        # script ran; the stderr warning routes Telegram tier separately.

    # Cohort filter is applied here (not just inside render) so the exit
    # code reflects whether the FILTERED set has data. Previously a typo'd
    # --cohort exited 0 because main checked the unfiltered list — silent
    # PASS on operator misconfiguration.
    filtered = trades
    if args.cohort:
        filtered = [t for t in trades if t.cohort == args.cohort]
        if not filtered and trades:
            cohorts_seen = sorted({t.cohort for t in trades})
            print(
                f"cohort {args.cohort!r} matched 0 of {len(trades)} "
                f"qualifying trades; available cohorts: "
                f"{', '.join(cohorts_seen)}",
                file=sys.stderr)

    print(render(filtered, args.cohort), end="")
    return 0 if filtered else 2


if __name__ == "__main__":
    sys.exit(main())
