#!/usr/bin/env python3
"""
forward_paper_resolution.py — apply the locked LIMBO decision rule
from results/forward_paper_outcome_resolution_decision_rule_2026-05-10.md

Synthesizes the outputs of three existing decision-grade signals
(drift_check_history, kill_protocol_check, stage_promotion_check) PLUS
the latest forward-paper snapshot into a single 5-verdict outcome:

  CONTINUE / WATCH / PROMOTE / KILL / OPERATOR_REVIEW

This is the script the locked LIMBO rule explicitly defers ("rule
locked, no script yet") with the intent to wire into weekly_audit.sh
as the 6th stage so the operator's natural Sunday-cadence interaction
includes the verdict mechanically — no manual seven-rule application
under day-60 emotional load.

Audit-pattern lens applied DURING DESIGN (not retrofitted):

  - Missing snapshot → INPUT_ERROR (exit 5), distinct from CONTINUE
  - Stale snapshot (>8 days) → OPERATOR_REVIEW (cron silently skipped)
  - Missing/empty drift history → OPERATOR_REVIEW (Rule 2 — monitoring dark)
  - Stale drift history (>14 days) → OPERATOR_REVIEW (Rule 2)
  - Snapshot parse failure → INPUT_ERROR
  - kill_protocol exit 3 / promote_check exit 3 → OPERATOR_REVIEW
    (sibling script's input error escalates rather than collapses)
  - All numeric metric extraction defensively guarded against missing
    fields (None passed through to rules that explicitly handle it)

Exit codes (tier-mapped per locked rule's Telegram mapping):

  0  CONTINUE         no Telegram alert; operator dashboard only
  1  PROMOTE          INFO — operator confirmation gate
  2  WATCH            INFO — flagged in weekly digest, no kill
  3  OPERATOR_REVIEW  WARN — manual rule cross-check needed
  4  KILL             CRITICAL — locked criterion fired
  5  INPUT_ERROR      WARN — distinct from CONTINUE; not a "no kill" pass

Usage:
  python3 scripts/forward_paper_resolution.py
  python3 scripts/forward_paper_resolution.py --snapshot results/forward_paper_snapshots/2026-05-10.txt
  python3 scripts/forward_paper_resolution.py --drift-history results/drift_check_history.jsonl
  python3 scripts/forward_paper_resolution.py --kill-exit 0 --promote-exit 2  # for testing
"""
from __future__ import annotations

import argparse
import datetime as dt
import json
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
SCRIPTS = REPO / "scripts"
DEFAULT_SNAPSHOT_DIR = REPO / "results" / "forward_paper_snapshots"
DEFAULT_DRIFT_HISTORY = REPO / "results" / "drift_check_history.jsonl"

# Locked thresholds — sourced from the LIMBO rule + the kill rule. Constants
# centralized so a future locked-rule update touches one place.
SNAPSHOT_STALE_DAYS = 8           # one cron cycle + 1d slack
DRIFT_HISTORY_STALE_DAYS = 14     # two missed cron cycles
N_TRADES_FLOOR = 50               # below this, ONLY Rule 3 CONTINUE applies
N_TRADES_PROMOTE = 150            # locked from real_money_protocol
N_DAYS_PROMOTE = 60               # locked from real_money_protocol

# Rule 5 WATCH slack-window thresholds (locked from the LIMBO rule).
FEE_WATCH_LO, FEE_WATCH_HI = 11.0, 12.0
SLIP_WATCH_LO, SLIP_WATCH_HI = 22.0, 25.0
SINGLE_SYM_WATCH, SINGLE_SYM_KILL = 40.0, 50.0

# Rule 6 OPERATOR_REVIEW slow-bleed threshold.
SLOW_BLEED_PNL_FRAC = 0.30        # < 30% of pro-rated expected
EXPECTED_ANNUAL_PNL_USD = 69000   # honest train-only-shortlist annual baseline


# Make the trajectory parser importable without packaging.
sys.path.insert(0, str(SCRIPTS))
import forward_paper_trajectory as fpt  # noqa: E402

# Import the audit-pattern memo's findings here as code: each external-state
# read is paired with an explicit failure-mode mapping.


# ── inputs ──────────────────────────────────────────────────────────────────


@dataclass
class Inputs:
    """All external-state reads, normalized for rule evaluation. None values
    are explicit — rules that consume an Optional[float] handle the missing
    case rather than collapsing to a default that would mislead the verdict."""
    snapshot_path: Path
    snapshot_date: str
    snapshot_age_days: int
    live_n_trades: int | None
    live_n_days: int | None
    live_pnl_usd: float | None
    live_wr_pct: float | None
    realized_fee_bps: float | None
    realized_slip_bps: float | None
    single_sym_pct: float | None
    hodl_delta_usd: float | None  # not currently parsed; reserved for future
    drift_last_exit: int | None
    drift_last_ts: str | None
    drift_age_days: int | None
    kill_check_exit: int | None
    promote_check_exit: int | None


def find_latest_snapshot(snapshot_dir: Path) -> Path | None:
    """Return the lexicographically-greatest YYYY-MM-DD.txt snapshot, or
    None if directory missing/empty. The caller maps None to INPUT_ERROR."""
    if not snapshot_dir.is_dir():
        return None
    candidates = sorted(snapshot_dir.glob("*.txt"))
    return candidates[-1] if candidates else None


def parse_snapshot_date(path: Path) -> str:
    return path.stem  # filename is YYYY-MM-DD


def days_since(ts_str: str, now: dt.datetime) -> int | None:
    """Days between an RFC3339 / ISO date string and `now`. Returns None on
    parse failure — caller routes that to OPERATOR_REVIEW per the audit
    lens (parse failure must NOT silently default to 0 days)."""
    if not ts_str:
        return None
    # Try multiple formats, mirroring forward_paper_status.sh's tolerance.
    for fmt in ("%Y-%m-%dT%H:%M:%SZ", "%Y-%m-%dT%H:%M:%S.%fZ", "%Y-%m-%d"):
        try:
            ts = dt.datetime.strptime(ts_str.split(".")[0].rstrip("Z") + ("Z" if fmt.endswith("Z") else ""), fmt)
            return (now - ts).days
        except ValueError:
            continue
    return None


def read_drift_state(history_path: Path, now: dt.datetime) -> tuple[int | None, str | None, int | None]:
    """Read the tail entry of drift_check_history.jsonl. Returns
    (last_exit_code, last_ts, age_days). Audit-pattern guards:

      - file missing → (None, None, None) — caller routes to OPERATOR_REVIEW
        (Rule 2: monitoring dark)
      - file empty → (None, None, None)
      - tail entry missing exit_code or ts → (None, None, None) — explicit
        rather than defaulting to 0 (which would falsely PASS Rule 1 KILL)
      - corrupt JSON on tail → (None, None, None)
    """
    if not history_path.is_file():
        return None, None, None
    try:
        with history_path.open() as f:
            lines = [ln for ln in f if ln.strip()]
        if not lines:
            return None, None, None
        tail = json.loads(lines[-1])
    except (json.JSONDecodeError, OSError):
        return None, None, None
    exit_code = tail.get("exit_code")
    ts = tail.get("ts")
    if not isinstance(exit_code, int) or not isinstance(ts, str):
        return None, None, None
    age = days_since(ts, now)
    return exit_code, ts, age


def parse_live_metrics(snap: fpt.Snapshot) -> dict[str, float | int | None]:
    """Extract live-cohort metrics from a parsed snapshot. Each metric
    returns None if absent — explicit-not-zero so the rules can distinguish
    'cohort had no closes' from 'cohort had 0 PnL.'"""
    cohort = snap.cohorts.get("live", {})
    def _num(key, cast=float) -> float | int | None:
        raw = cohort.get(key)
        if raw is None:
            return None
        try:
            return cast(raw)
        except (TypeError, ValueError):
            return None
    return {
        "live_n_trades": _num("trades", int),
        "live_n_days": _num("days", int),
        "live_pnl_usd": _num("pnl_usd"),
        "live_wr_pct": _num("wr_pct"),
        "realized_fee_bps": _num("fee_bps"),
        "realized_slip_bps": _num("slip_bps"),
        "single_sym_pct": _num("single_pct"),
    }


def invoke_sibling(script_name: str, override: int | None,
                   sibling_args: list[str] | None = None) -> int | None:
    """Invoke a sibling decision script (kill_protocol_check or
    stage_promotion_check) and return its exit code. Override allows tests
    to inject a specific exit code without running the actual script.
    Returns None if the script is missing — caller maps to OPERATOR_REVIEW.

    sibling_args propagates --vps / --live-source / --live-dir to the
    sibling subprocess so a `forward_paper_resolution --live-source local`
    invocation actually runs the LIMBO synthesis against local journals
    (pre-fix, the sibling subprocesses used their default VPS regardless
    of the parent's --live-source — same ssh-target-consistency shape
    as Track 7's stage_promotion F1).
    """
    if override is not None:
        return override
    path = SCRIPTS / script_name
    if not path.is_file():
        return None
    cmd = ["python3", str(path)]
    if sibling_args:
        cmd.extend(sibling_args)
    try:
        result = subprocess.run(
            cmd,
            capture_output=True, text=True, timeout=120,
        )
    except (subprocess.TimeoutExpired, OSError):
        return None
    return result.returncode


def gather_inputs(args: argparse.Namespace) -> tuple[Inputs | None, str | None]:
    """Returns (inputs, error_message). On error, inputs is None and the
    caller emits INPUT_ERROR with the specific reason — the audit-pattern
    contract: every input failure shape gets its own message, not silent
    defaults."""
    now = dt.datetime.now(dt.timezone.utc).replace(tzinfo=None)

    # Snapshot.
    snapshot_path = (Path(args.snapshot) if args.snapshot
                     else find_latest_snapshot(Path(args.snapshot_dir)))
    if snapshot_path is None:
        return None, (
            f"no snapshot found in {args.snapshot_dir}. "
            "Run scripts/forward_paper_status.sh > "
            "results/forward_paper_snapshots/$(date -u +%Y-%m-%d).txt or "
            "let weekly_audit.sh produce one.")
    if not snapshot_path.is_file():
        return None, f"snapshot path is not a file: {snapshot_path}"

    snapshot_date = parse_snapshot_date(snapshot_path)
    snap_age = days_since(snapshot_date, now)
    if snap_age is None:
        return None, f"snapshot filename does not parse as YYYY-MM-DD: {snapshot_date}"

    try:
        snap = fpt.parse_snapshot(snapshot_path)
    except (OSError, ValueError) as e:
        return None, f"snapshot parse failed: {snapshot_path}: {e}"

    metrics = parse_live_metrics(snap)

    # Drift history.
    drift_path = Path(args.drift_history)
    drift_exit, drift_ts, drift_age = read_drift_state(drift_path, now)

    # Sibling scripts (or test overrides). Propagate --vps / --live-source /
    # --live-dir so sibling subprocesses honor the same data-source as the
    # parent — closes the ssh-target-consistency shape where
    # `forward_paper_resolution --live-source local` would still ssh to
    # the production VPS via the unconfigured sibling subprocess.
    sibling_args: list[str] = []
    if args.live_source:
        sibling_args.extend(["--live-source", args.live_source])
    if args.vps:
        sibling_args.extend(["--vps", args.vps])
    if args.live_dir:
        sibling_args.extend(["--live-dir", args.live_dir])
    kill_exit = invoke_sibling("kill_protocol_check.py", args.kill_exit,
                               sibling_args=sibling_args)
    promote_exit = invoke_sibling("stage_promotion_check.py", args.promote_exit,
                                  sibling_args=sibling_args)

    return Inputs(
        snapshot_path=snapshot_path,
        snapshot_date=snapshot_date,
        snapshot_age_days=snap_age,
        live_n_trades=metrics["live_n_trades"],
        live_n_days=metrics["live_n_days"],
        live_pnl_usd=metrics["live_pnl_usd"],
        live_wr_pct=metrics["live_wr_pct"],
        realized_fee_bps=metrics["realized_fee_bps"],
        realized_slip_bps=metrics["realized_slip_bps"],
        single_sym_pct=metrics["single_sym_pct"],
        hodl_delta_usd=None,
        drift_last_exit=drift_exit,
        drift_last_ts=drift_ts,
        drift_age_days=drift_age,
        kill_check_exit=kill_exit,
        promote_check_exit=promote_exit,
    ), None


# ── decision tree ────────────────────────────────────────────────────────────


@dataclass
class Verdict:
    code: int          # exit code
    label: str         # CONTINUE / WATCH / PROMOTE / KILL / OPERATOR_REVIEW
    rule: str          # which rule fired
    reasons: list[str] # human-readable reasons
    inputs: Inputs


def evaluate(inp: Inputs) -> Verdict:
    """Apply Rules 1-7 in order; first match wins. The rule order is locked
    by the LIMBO decision rule (Rule 1 KILL precedes everything; Rule 7
    default → CONTINUE)."""

    # ── Rule 1: KILL ────────────────────────────────────────────────────────
    # The locked criteria are CAPTURED via the sibling decision scripts —
    # kill_protocol_check.py is the source of truth for slip>30, drawdown,
    # single_sym>=50, and consecutive daily-loss. Reading them here directly
    # would (a) duplicate logic and (b) bypass the n_trades floor those
    # scripts apply internally — at n=1 the single_sym ratio is always 100%
    # (one trade = 100% of pnl) which would fire a spurious KILL.
    # Self-audit catch during development 2026-05-10. The two genuine
    # signals here that aren't in the sibling scripts:
    #   - drift wrapper exit 4 (two firings rule, locally computed by the
    #     drift wrapper — distinct from kill_protocol_check's rolling 30-trade
    #     slip check or drawdown check)
    #   - kill_protocol_check itself firing
    kill_reasons = []
    if inp.drift_last_exit == 4:
        kill_reasons.append("drift wrapper exit 4 (two firings ≥7d apart "
                            "OR drift+threshold match)")
    if inp.kill_check_exit == 1:
        kill_reasons.append("kill_protocol_check fires (locked criteria: "
                            "slip>30, drawdown 20%, single-sym>50%, "
                            "3-consecutive-day-loss>5×stake)")
    if kill_reasons:
        return Verdict(4, "KILL", "Rule 1", kill_reasons, inp)

    # ── Rule 2: drift cron silently dead → OPERATOR_REVIEW ──────────────────
    review_reasons = []
    if inp.drift_age_days is None:
        review_reasons.append("drift_check_history.jsonl missing or "
                              "tail entry malformed — monitoring is DARK")
    elif inp.drift_age_days > DRIFT_HISTORY_STALE_DAYS:
        review_reasons.append(f"drift_check_history last entry "
                              f"{inp.drift_age_days}d ago (>{DRIFT_HISTORY_STALE_DAYS}d) — "
                              "weekly cron may be dead; absence of firings "
                              "is NOT confirmation of strategy health when "
                              "monitoring itself is dark")
    if inp.snapshot_age_days > SNAPSHOT_STALE_DAYS:
        review_reasons.append(f"snapshot {inp.snapshot_age_days}d old "
                              f"(>{SNAPSHOT_STALE_DAYS}d) — weekly_audit.sh "
                              "may have skipped a snapshot")
    if (inp.kill_check_exit == 3 or inp.promote_check_exit == 3
            or inp.kill_check_exit is None or inp.promote_check_exit is None):
        review_reasons.append(
            f"sibling decision script error (kill_exit={inp.kill_check_exit}, "
            f"promote_exit={inp.promote_check_exit}) — input/env failure must "
            "not collapse into CONTINUE")
    if inp.kill_check_exit == 4 or inp.promote_check_exit == 4:
        review_reasons.append(
            "deferred verification gate (kill_protocol_check exit 4 or "
            "stage_promotion_check exit 4) — operator must verify before "
            "treating the broader outcome")
    if review_reasons:
        return Verdict(3, "OPERATOR_REVIEW", "Rule 2", review_reasons, inp)

    # ── Rule 3: insufficient data → CONTINUE ────────────────────────────────
    # Drift detector requires N=50 minimum; below this, ONLY CONTINUE applies.
    # Threshold gates are advisory at any n (kill-bar mis-calibration).
    if inp.live_n_trades is None or inp.live_n_trades < N_TRADES_FLOOR:
        return Verdict(0, "CONTINUE", "Rule 3",
                       [f"n_trades_live={inp.live_n_trades} < {N_TRADES_FLOOR} "
                        "(statistical-power floor; below this nothing but "
                        "CONTINUE applies — handles the n=9 emotional case)"],
                       inp)

    # ── Rule 4: PROMOTE ─────────────────────────────────────────────────────
    # ALL gates must be true. The sibling stage_promotion_check is the
    # source of truth — we mirror its conclusion (exit 0 = all gates pass).
    if (inp.promote_check_exit == 0
            and inp.kill_check_exit in (0, 2)
            and inp.drift_last_exit == 0):
        return Verdict(1, "PROMOTE", "Rule 4",
                       ["stage_promotion_check exit 0 (all 9 gates pass)",
                        f"kill_check exit {inp.kill_check_exit} (no kill criterion fires)",
                        "drift detector last firing CLEAN"],
                       inp)

    # ── Rule 5: WATCH (1-2 soft signals) ────────────────────────────────────
    soft = []
    if inp.drift_last_exit == 1:
        soft.append("drift detector single firing — investigation tier")
    if (inp.realized_fee_bps is not None
            and FEE_WATCH_LO < inp.realized_fee_bps <= FEE_WATCH_HI):
        soft.append(f"realized fee {inp.realized_fee_bps:.2f}bp in slack "
                    f"window ({FEE_WATCH_LO}-{FEE_WATCH_HI}bp)")
    if (inp.realized_slip_bps is not None
            and SLIP_WATCH_LO < inp.realized_slip_bps <= SLIP_WATCH_HI):
        soft.append(f"realized slip {inp.realized_slip_bps:.2f}bp in slack "
                    f"window ({SLIP_WATCH_LO}-{SLIP_WATCH_HI}bp)")
    if (inp.single_sym_pct is not None
            and SINGLE_SYM_WATCH <= inp.single_sym_pct < SINGLE_SYM_KILL):
        soft.append(f"single_sym_pct {inp.single_sym_pct:.1f}% in slack "
                    f"window ({SINGLE_SYM_WATCH}-{SINGLE_SYM_KILL}%)")
    if (inp.live_pnl_usd is not None and inp.live_pnl_usd < 0
            and inp.live_n_days is not None and inp.live_n_days >= 30):
        soft.append(f"live PnL {inp.live_pnl_usd:+.0f} negative at "
                    f"{inp.live_n_days}d (within bounded-losses range)")
    if 1 <= len(soft) <= 2:
        return Verdict(2, "WATCH", "Rule 5", soft, inp)

    # ── Rule 6: OPERATOR_REVIEW (genuinely ambiguous) ────────────────────────
    review_reasons = []
    if len(soft) >= 3:
        review_reasons.append(
            "3+ soft signals fire simultaneously: " + "; ".join(soft))
    if (inp.live_n_days is not None and inp.live_n_days > 90
            and inp.live_n_trades is not None and inp.live_n_trades < 100):
        review_reasons.append(
            f"low trade rate: n={inp.live_n_trades} after "
            f"{inp.live_n_days}d (well below 1.18/day fleet rate)")
    # Slow-bleed: positive but <30% of pro-rated expectation.
    # NOTE: live_n_days here comes from forward_paper_status.sh which
    # measures "days since first trade" (not deploy date). Pre-reg
    # real_money_protocol_decision_rule_2026-05-08.md line 134 anchors
    # elapsed reasoning on forward-paper deploy start; LIMBO rule
    # (forward_paper_outcome_resolution_decision_rule_2026-05-10.md
    # line 47) anchors on "first close". This implementation matches
    # forward_paper_status's first-trade anchor — surfaced explicitly
    # in the diagnostic string below so any operator reading the
    # verdict sees which anchor is in force. See TIME-ANCHOR AMBIGUITY
    # comment in stage_promotion_check.check_days.
    if (inp.live_pnl_usd is not None and inp.live_pnl_usd > 0
            and inp.live_n_days is not None and inp.live_n_days >= 60):
        pro_rated = EXPECTED_ANNUAL_PNL_USD * inp.live_n_days / 365
        if inp.live_pnl_usd < SLOW_BLEED_PNL_FRAC * pro_rated:
            review_reasons.append(
                f"slow-bleed: live PnL +${inp.live_pnl_usd:.0f} < "
                f"{SLOW_BLEED_PNL_FRAC*100:.0f}% of pro-rated "
                f"${pro_rated:.0f} (n_days={inp.live_n_days} since first trade, "
                f"expected ${EXPECTED_ANNUAL_PNL_USD}/yr × {inp.live_n_days}/365)")
    if review_reasons:
        return Verdict(3, "OPERATOR_REVIEW", "Rule 6", review_reasons, inp)

    # ── Rule 7: default → CONTINUE ──────────────────────────────────────────
    return Verdict(0, "CONTINUE", "Rule 7",
                   ["no rule fired — re-evaluate next weekly cadence"], inp)


# ── render ───────────────────────────────────────────────────────────────────


def render(v: Verdict) -> str:
    sep = "═" * 78
    lines = [sep, f"  Forward-paper resolution — {dt.datetime.now(dt.timezone.utc).strftime('%Y-%m-%d %H:%M UTC')}",
             "  Locked rule: results/forward_paper_outcome_resolution_decision_rule_2026-05-10.md",
             sep, ""]
    inp = v.inputs
    lines.append(f"  Snapshot:        {inp.snapshot_path} ({inp.snapshot_age_days}d old)")
    lines.append(f"  Live cohort:     n={inp.live_n_trades} trades / "
                 f"{inp.live_n_days} days / pnl=${inp.live_pnl_usd}")
    lines.append(f"  Drift state:     last exit={inp.drift_last_exit} "
                 f"({inp.drift_age_days}d ago)")
    lines.append(f"  Sibling exits:   kill={inp.kill_check_exit} promote={inp.promote_check_exit}")
    lines.append("")
    lines.append(f"  >>> VERDICT: {v.label}  ({v.rule})")
    for r in v.reasons:
        lines.append(f"      • {r}")
    lines.append("")
    lines.append(sep)
    return "\n".join(lines) + "\n"


# ── main ─────────────────────────────────────────────────────────────────────


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--snapshot", default=None,
                    help="Specific snapshot file to evaluate (default: latest in --snapshot-dir)")
    ap.add_argument("--snapshot-dir", default=str(DEFAULT_SNAPSHOT_DIR))
    ap.add_argument("--drift-history", default=str(DEFAULT_DRIFT_HISTORY))
    # Pass-through to sibling decision scripts (kill_protocol_check +
    # stage_promotion_check). Without these, the rehearsal/operator
    # invoking with --live-source local would still ssh to production
    # via the sibling subprocesses — ssh-target-consistency fail-open
    # shape. Defaults to None so unset flags don't override siblings'
    # own defaults.
    ap.add_argument("--vps", default=None,
                    help="Forwarded to sibling scripts (default: sibling's own default)")
    ap.add_argument("--live-source", choices=("vps", "local"), default=None,
                    help="Forwarded to sibling scripts (vps/local)")
    ap.add_argument("--live-dir", default=None,
                    help="Forwarded to sibling scripts")
    ap.add_argument("--kill-exit", type=int, default=None,
                    help="Override kill_protocol_check exit (testing)")
    ap.add_argument("--promote-exit", type=int, default=None,
                    help="Override stage_promotion_check exit (testing)")
    ap.add_argument("--json", action="store_true",
                    help="Emit JSON instead of human-readable output")
    args = ap.parse_args()

    inputs, err = gather_inputs(args)
    if inputs is None:
        if args.json:
            print(json.dumps({"verdict": "INPUT_ERROR", "exit_code": 5,
                              "error": err}))
        else:
            print(f"INPUT_ERROR: {err}", file=sys.stderr)
        return 5

    verdict = evaluate(inputs)

    if args.json:
        print(json.dumps({
            "verdict": verdict.label,
            "exit_code": verdict.code,
            "rule": verdict.rule,
            "reasons": verdict.reasons,
            "snapshot_date": inputs.snapshot_date,
            "live_n_trades": inputs.live_n_trades,
            "live_n_days": inputs.live_n_days,
            "live_pnl_usd": inputs.live_pnl_usd,
            "drift_last_exit": inputs.drift_last_exit,
        }))
    else:
        print(render(verdict), end="")
    return verdict.code


if __name__ == "__main__":
    sys.exit(main())
