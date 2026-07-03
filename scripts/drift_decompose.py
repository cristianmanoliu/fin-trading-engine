#!/usr/bin/env python3
"""
drift_decompose.py — committed, read-only producer of the per-metric benign
decomposition of drift-detector firings (docs/findings/2026-07-04.md).

WHY THIS EXISTS
---------------
The weekly drift detector fires persistently on metrics whose offsets are
STRUCTURAL, not edge-death: win_pnl (max-hold exit-mix), loss_pnl_abs
(fee+slip scale with notional; live 2026 stops are tighter than the 5y
reference average; small real stop-overshoot from REST-lag), mfe_r
(censoring). Because |t| grows with sqrt(n) against the FIXED reference,
drift exit-0 CLEAN is mechanically unreachable before the ~2026-08-25
verdict, which lands the resolution in Rule 6 OPERATOR_REVIEW
(results/forward_paper_outcome_resolution_decision_rule_2026-05-10.md).
Rule 6 requires a documented rationale. This script produces that
rationale's like-for-like evidence in one reproducible, tested command —
not improvised at the verdict session from a findings-doc recipe.

WHAT IT DOES (and does NOT)
---------------------------
- READ-ONLY. Reads top-level *.jsonl close events from --live-dir (LIVE
  engine only: glob does not recurse, so shadow/ layer3/ archive/ are
  excluded by construction) and from --backtest-dir (the drift detector's
  frozen reference, default results/hod_journals/2026-05-07-mfe).
- Classifies every close by REALIZED R against the trade's own target R —
  never by the journal `outcome` label (max-hold force-closes are labeled
  TARGET/STOP by pnl sign; documented in pkg/execution/stub.go ~line 522).
  NOTE: the frozen reference's close `ts` is generation wall-clock (it
  predates the journalTS fix) — hold time must never be computed from it,
  which is why classification is R-based.
- Prints the like-for-like table (target wins / max-hold wins / stop
  losses / max-hold losses), the notional comparison, and the live
  stop-overshoot in bps of notional.
- ADVISORY verdict only. The thresholds below are descriptive defaults
  for flagging "this no longer looks like the benign 2026-07-04
  decomposition"; they are NOT locked kill/promote criteria and do not
  modify any locked rule.

EXIT CODES (audit-lens: input failure must never read as benign):
  0  BENIGN-consistent : like-for-like target wins within tolerance AND
                         live stop-overshoot within cap
  1  NOT explained     : decomposition no longer accounts for the drift
                         firings (real-drift signature) — investigate
  2  INSUFFICIENT      : too few live trades to read (backtest table
                         still printed)
  3  INPUT ERROR       : missing/empty inputs or unusable reference

USAGE
-----
  python3 scripts/drift_decompose.py
  python3 scripts/drift_decompose.py --live-dir results/journal_cache \
      --backtest-dir results/hod_journals/2026-05-07-mfe --stake-usd 1000
"""
from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

# Advisory flip thresholds (descriptive defaults, NOT locked criteria).
# 2026-07-04 measured: like-for-like target-win gap -0.7%; overshoot ~2.6bp.
WIN_GAP_FLIP = -0.05        # live target-win mean >5% BELOW backtest
OVERSHOOT_FLIP_BPS = 10.0   # mean stop-overshoot beyond modeled slip, bps of
                            # notional (locked slip criterion is 20bp total)

# Advisory power floor before the live side is readable at all.
MIN_LIVE_TARGET_WINS = 5
MIN_LIVE_STOP_LOSSES = 20

TARGET_R_TOL = 0.02   # realized R within this of the trade's own target R
STOP_R_EDGE = -0.98   # realized R at/below this = stop-out


def load_closes(jdir: Path) -> list[dict]:
    """Top-level close events only; non-recursive by construction."""
    closes: list[dict] = []
    for jf in sorted(jdir.glob("*.jsonl")):
        with jf.open() as fh:
            for line in fh:
                line = line.strip()
                if not line:
                    continue
                try:
                    ev = json.loads(line)
                except json.JSONDecodeError:
                    print(f"warn: skipping unparseable line in {jf.name}",
                          file=sys.stderr)
                    continue
                if ev.get("event") == "close":
                    closes.append(ev)
    return closes


def realized_r(ev: dict) -> float | None:
    entry = ev.get("entry", 0.0)
    stop = ev.get("stop", 0.0)
    exit_p = ev.get("exit", 0.0)
    sd = abs(stop - entry)
    if sd <= 0:
        return None
    move = (entry - exit_p) if ev.get("side") == "SHORT" else (exit_p - entry)
    return move / sd


def target_r(ev: dict) -> float | None:
    entry = ev.get("entry", 0.0)
    stop = ev.get("stop", 0.0)
    tgt = ev.get("target", 0.0)
    sd = abs(stop - entry)
    if sd <= 0:
        return None
    return abs(entry - tgt) / sd


class Buckets:
    def __init__(self) -> None:
        self.target_wins: list[dict] = []
        self.mh_wins: list[dict] = []
        self.stop_losses: list[dict] = []
        self.mh_losses: list[dict] = []
        self.skipped = 0
        self.notionals: list[float] = []

    def add(self, ev: dict, stake: float) -> None:
        r = realized_r(ev)
        tr = target_r(ev)
        pnl = ev.get("pnl_usd", 0.0)
        if r is None or tr is None or pnl == 0.0:
            self.skipped += 1
            return
        notional = ev.get("notional_usd", 0.0)
        if not notional:
            entry = ev.get("entry", 0.0)
            sd = abs(ev.get("stop", 0.0) - entry)
            notional = stake * entry / sd if sd > 0 else 0.0
        if notional > 0:
            self.notionals.append(notional)
        if pnl > 0:
            (self.target_wins if r >= tr - TARGET_R_TOL else self.mh_wins).append(ev)
        else:
            (self.stop_losses if r <= STOP_R_EDGE else self.mh_losses).append(ev)


def mean_pnl(evs: list[dict]) -> float:
    return sum(e.get("pnl_usd", 0.0) for e in evs) / len(evs) if evs else 0.0


def bucketize(closes: list[dict], stake: float) -> Buckets:
    b = Buckets()
    for ev in closes:
        b.add(ev, stake)
    return b


def overshoot_bps(stop_losses: list[dict], stake: float) -> tuple[float | None, int]:
    """Mean stop-overshoot beyond -1R gross, in bps of notional, over losers
    that carry the cost decomposition (gross_usd present). Pre-extension
    closes lack it and are excluded."""
    vals: list[float] = []
    for ev in stop_losses:
        gross = ev.get("gross_usd")
        notional = ev.get("notional_usd", 0.0)
        if gross is None or notional <= 0:
            continue
        vals.append((-gross - stake) / notional * 1e4)
    if not vals:
        return None, 0
    return sum(vals) / len(vals), len(vals)


def print_table(bt: Buckets, live: Buckets) -> None:
    def row(name: str, b_evs: list[dict], l_evs: list[dict]) -> None:
        print(f"  {name:<15}| n={len(b_evs):>5} mean {mean_pnl(b_evs):>+7.0f} "
              f"| n={len(l_evs):>4} mean {mean_pnl(l_evs):>+7.0f}")

    print("  like-for-like  |  backtest reference  |  live")
    row("target wins", bt.target_wins, live.target_wins)
    row("max-hold wins", bt.mh_wins, live.mh_wins)
    row("stop losses", bt.stop_losses, live.stop_losses)
    row("max-hold losses", bt.mh_losses, live.mh_losses)
    bt_wins = len(bt.target_wins) + len(bt.mh_wins)
    lv_wins = len(live.target_wins) + len(live.mh_wins)
    if bt_wins and lv_wins:
        print(f"  max-hold share of wins: backtest {100 * len(bt.mh_wins) / bt_wins:.0f}% "
              f"| live {100 * len(live.mh_wins) / lv_wins:.0f}%")
    bt_ntl = sum(bt.notionals) / len(bt.notionals) if bt.notionals else 0.0
    lv_ntl = sum(live.notionals) / len(live.notionals) if live.notionals else 0.0
    print(f"  mean notional: backtest ~${bt_ntl:,.0f} (implied) | live ${lv_ntl:,.0f}")


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--live-dir", default="results/journal_cache")
    ap.add_argument("--backtest-dir", default="results/hod_journals/2026-05-07-mfe")
    ap.add_argument("--stake-usd", type=float, default=1000.0)
    args = ap.parse_args()

    live_dir = Path(args.live_dir)
    bt_dir = Path(args.backtest_dir)
    for name, d in (("--live-dir", live_dir), ("--backtest-dir", bt_dir)):
        if not d.is_dir():
            print(f"INPUT ERROR: {name} not a directory: {d}", file=sys.stderr)
            return 3

    bt_closes = load_closes(bt_dir)
    if not bt_closes:
        print(f"INPUT ERROR: no close events in reference {bt_dir}", file=sys.stderr)
        return 3
    live_closes = load_closes(live_dir)

    bt = bucketize(bt_closes, args.stake_usd)
    live = bucketize(live_closes, args.stake_usd)
    if not bt.target_wins:
        print(f"INPUT ERROR: reference has zero target wins — unusable: {bt_dir}",
              file=sys.stderr)
        return 3

    print("Drift-firing benign decomposition (advisory; docs/findings/2026-07-04.md)")
    print(f"  backtest: {bt_dir}  ({len(bt_closes)} closes)")
    print(f"  live:     {live_dir}  ({len(live_closes)} closes)")
    print()
    print_table(bt, live)
    print()

    if (len(live.target_wins) < MIN_LIVE_TARGET_WINS
            or len(live.stop_losses) < MIN_LIVE_STOP_LOSSES):
        print(f"INSUFFICIENT live data: target wins {len(live.target_wins)} "
              f"(need ≥{MIN_LIVE_TARGET_WINS}), stop losses {len(live.stop_losses)} "
              f"(need ≥{MIN_LIVE_STOP_LOSSES}). No verdict.")
        return 2

    win_gap = (mean_pnl(live.target_wins) - mean_pnl(bt.target_wins)) \
        / mean_pnl(bt.target_wins)
    os_bps, os_n = overshoot_bps(live.stop_losses, args.stake_usd)

    print(f"  target-win like-for-like gap: {win_gap * 100:+.1f}% "
          f"(flip if < {WIN_GAP_FLIP * 100:.0f}%)")
    if os_bps is None:
        print("  live stop-overshoot: n/a (no losers carry cost decomposition)")
    else:
        print(f"  live stop-overshoot: {os_bps:+.1f}bp of notional over {os_n} losers "
              f"(flip if > {OVERSHOOT_FLIP_BPS:.0f}bp)")
    print()

    reasons: list[str] = []
    if win_gap < WIN_GAP_FLIP:
        reasons.append(f"live target wins {win_gap * 100:+.1f}% below backtest "
                       f"(tolerance {WIN_GAP_FLIP * 100:.0f}%)")
    if os_bps is not None and os_bps > OVERSHOOT_FLIP_BPS:
        reasons.append(f"stop-overshoot {os_bps:+.1f}bp exceeds "
                       f"{OVERSHOOT_FLIP_BPS:.0f}bp cap")

    if reasons:
        print("VERDICT: drift firings NOT explained by the benign decomposition:")
        for r in reasons:
            print(f"  ✗ {r}")
        print("Investigate before treating DRIFT_FIRED as censoring-benign.")
        return 1

    print("VERDICT: BENIGN-consistent — like-for-like edge geometry matches the")
    print("reference; loss-side gap remains cost-scaling + small overshoot.")
    print("(Advisory only. Rule-6 rationale still requires the operator to check")
    print(" WR/PnL/HODL gates + cross-check 9 Condition 2 as usual.)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
