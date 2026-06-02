#!/usr/bin/env python3
"""
crosscheck9_losers_mae.py — committed, read-only producer of the cross-check 9
Condition-1 (losers-only mae_r distribution) + Condition-3 (closed-winner count)
blocks, in the exact text format the drift-firing precheck docs already use.

WHY THIS EXISTS
---------------
The ~06-07 weekly launchd drift check is near-certain to fire exit-4 (auto-kill
candidate) because a 7d pair forms vs the 2026-05-27 anchor. The HOLD-vs-honor-kill
call is gated by cross-check 9 (results/drift_firing_investigation_decision_rule_
2026-05-08.md:205-218), whose discriminating evidence is the losers-only mae_r
distribution: is it pinned at ~1.0R (censoring-benign, HOLD) or deepening past
~1.10R (real edge-death, honor the kill)?

Until now that distribution was re-derived from an ephemeral /tmp script and
hand-transcribed into results/drift_firings/*-precheck.md every fire. This script
codifies that recipe (handoff gotcha "Live P&L parse recipe") so the 06-07
disposition is one command, reproducible, and tested — not improvised under a
CRITICAL Telegram alert.

WHAT IT DOES (and does NOT)
---------------------------
- READ-ONLY. Reads top-level results/journal_cache/*.jsonl LIVE-engine journals.
  glob("*.jsonl") does NOT recurse, so the layer3/ shadow/ testnet/ archive/
  subdirs are skipped by construction — matching the LIVE-only parse recipe.
- Emits Condition 1 (losers mae_r: n/min/max/mean/p50 + count>prior-max + count>1.10)
  and Condition 3 (outcome breakdown, n_closed_winners) in copy-paste doc format.
- Prints a mechanical HOLD / FLIP verdict on Conditions 1 & 3 against the locked
  edge-death thresholds (max > 1.10 OR mean > 1.05  =>  FLIP candidate).
- Does NOT touch drift_check_history.jsonl. Does NOT run the wrapper
  run_drift_check.sh (which appends history and would manufacture the 7d pair).
- Does NOT fetch live prices. Condition 2 (open positions favorable) needs live
  marks and is left to the operator — the script only reminds.

This is the censoring/edge-death discriminator's data half. It does not by itself
decide HOLD: Condition 2 (operator-checked) must also hold.

USAGE
-----
  python3 scripts/crosscheck9_losers_mae.py
  python3 scripts/crosscheck9_losers_mae.py --live-dir results/journal_cache
  python3 scripts/crosscheck9_losers_mae.py --prior-max 1.071   # flag new-max count

EXIT CODES (advisory — Condition 2 still required before any disposition):
  0  HOLD-consistent : losers mae_r flat (max <= 1.10 AND mean <= 1.05)
  1  FLIP candidate  : losers mae_r structurally deepened (max > 1.10 OR mean > 1.05)
  2  NO DATA         : no LIVE closed losers found in --live-dir
"""
from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

# Locked edge-death thresholds (results/drift_firing_investigation_decision_rule_
# 2026-05-08.md + 2026-05-31 precheck §"only signal that would flip"). A losers
# mae_r reading flips cross-check 9 Condition 1 to real edge-death iff EITHER holds.
FLIP_MAX_THRESHOLD = 1.10   # new losers mae_r max materially above this
FLIP_MEAN_THRESHOLD = 1.05  # OR losers mae_r mean above this


def percentile(xs: list[float], q: float) -> float:
    """Nearest-rank-ish linear-interpolated percentile, q in [0,1]. Matches the
    p50 the precheck docs report (e.g. p50=1.0050 on a tightly-packed loser set)."""
    if not xs:
        return float("nan")
    s = sorted(xs)
    if len(s) == 1:
        return s[0]
    pos = q * (len(s) - 1)
    lo = int(pos)
    hi = min(lo + 1, len(s) - 1)
    frac = pos - lo
    return s[lo] + (s[hi] - s[lo]) * frac


def load_live_closes(live_dir: Path) -> list[dict]:
    """Top-level LIVE-engine close events only. glob('*.jsonl') is non-recursive,
    so layer3/ shadow/ testnet/ archive/ subdirs are excluded by construction."""
    closes: list[dict] = []
    for jf in sorted(live_dir.glob("*.jsonl")):
        with jf.open() as fh:
            for line in fh:
                line = line.strip()
                if not line:
                    continue
                try:
                    ev = json.loads(line)
                except json.JSONDecodeError:
                    # Corrupt/partial trailing line — skip, never crash the
                    # cross-check on a single bad record (silent-on-corrupt-input
                    # is the cataloged anti-pattern; we log to stderr instead).
                    print(f"warn: skipping unparseable line in {jf.name}", file=sys.stderr)
                    continue
                if ev.get("event") == "close":
                    closes.append(ev)
    return closes


def is_loser(ev: dict) -> bool:
    """Cross-check 9 loser definition: a STOP outcome with negative dollar PnL.
    outcome is authoritative (the reason string always says rr=6.0)."""
    return ev.get("outcome") == "STOP" and ev.get("pnl_usd", 0.0) < 0.0


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--live-dir", default="results/journal_cache",
                    help="dir of top-level LIVE journal *.jsonl (default: results/journal_cache)")
    ap.add_argument("--prior-max", type=float, default=1.071,
                    help="prior losers mae_r max to count exceedances against (default: 1.071)")
    args = ap.parse_args()

    live_dir = Path(args.live_dir)
    if not live_dir.is_dir():
        print(f"error: --live-dir not a directory: {live_dir}", file=sys.stderr)
        return 2

    closes = load_live_closes(live_dir)
    if not closes:
        print(f"NO DATA: no LIVE close events in {live_dir}", file=sys.stderr)
        return 2

    losers = [ev for ev in closes if is_loser(ev)]
    mae = sorted(float(ev.get("mae_r", 0.0)) for ev in losers)

    # ── Outcome breakdown (Condition 3) ──────────────────────────────────────
    by_outcome: dict[str, int] = {}
    for ev in closes:
        o = ev.get("outcome", "?")
        by_outcome[o] = by_outcome.get(o, 0) + 1
    n_winners = by_outcome.get("TARGET", 0)
    breakdown = ", ".join(f"{n} {o}" for o, n in sorted(by_outcome.items()))

    if not mae:
        print(f"NO DATA: {len(closes)} closes but zero STOP losers in {live_dir}",
              file=sys.stderr)
        print(f"outcome breakdown: {breakdown}")
        return 2

    n = len(mae)
    mn, mx = mae[0], mae[-1]
    mean = sum(mae) / n
    p50 = percentile(mae, 0.50)
    over_prior = sum(1 for x in mae if x > args.prior_max)
    over_flip = sum(1 for x in mae if x > FLIP_MAX_THRESHOLD)

    # ── Condition 1 block — copy-paste into the precheck doc ──────────────────
    print("### Condition 1 — `mae_r` flat (losers-only distribution)")
    print()
    print("```")
    print(f"mae_r losers: n={n}  min={mn:.4f}  max={mx:.4f}  mean={mean:.4f}  p50={p50:.4f}")
    print(f">{args.prior_max:.3f} (prior max): {over_prior} trades")
    print(f">{FLIP_MAX_THRESHOLD:.2f} (edge-death threshold): {over_flip} trades")
    print("```")
    print()

    # ── Condition 3 block ─────────────────────────────────────────────────────
    print("### Condition 3 — `n_closed_winners < ~15`")
    print()
    print("```")
    print(f"outcome breakdown: {breakdown}")
    print(f"n_closed_winners: {n_winners}")
    print("```")
    print()

    # ── Mechanical verdict on Conditions 1 & 3 (Condition 2 is operator-checked)
    flip = mx > FLIP_MAX_THRESHOLD or mean > FLIP_MEAN_THRESHOLD
    cond3_ok = n_winners < 15
    print("### Mechanical verdict (Conditions 1 & 3 only)")
    print()
    if flip:
        print(f"Condition 1: ✗ FLIP candidate — losers mae_r deepened "
              f"(max={mx:.4f} > {FLIP_MAX_THRESHOLD} OR mean={mean:.4f} > {FLIP_MEAN_THRESHOLD}).")
    else:
        print(f"Condition 1: ✓ flat — max={mx:.4f} ≤ {FLIP_MAX_THRESHOLD}, "
              f"mean={mean:.4f} ≤ {FLIP_MEAN_THRESHOLD}. No structural deepening.")
    print(f"Condition 3: {'✓' if cond3_ok else '✗'} n_closed_winners={n_winners} "
          f"{'<' if cond3_ok else '≥'} 15.")
    print()
    print("⚠ Condition 2 (open positions favorable) is NOT computed here — it needs "
          "live marks. Run the operator price check before recording any HOLD.")
    print("⚠ This script does not mutate drift_check_history.jsonl. Do NOT run "
          "scripts/run_drift_check.sh manually (it appends history → manufactures the 7d pair).")

    return 1 if flip else 0


if __name__ == "__main__":
    raise SystemExit(main())
