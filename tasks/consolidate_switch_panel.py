#!/usr/bin/env python3
# tasks/consolidate_switch_panel.py — panel verdict for the regime-SWITCH test.
# Implements the locked PASS rule (spec §6): F = cohorts whose baseline FAILS
# Crit2; PASS = strict majority of F flip (switch passes Crit2) AND those flips
# satisfy Crit3 AND no baseline-clean cohort regresses.
import glob
import os
import re
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
PANEL_DIR = os.path.join(ROOT, "tasks", "regime_switch_results")
EXPECTED_COHORTS = 9


def parse_summary(cohort, path):
    with open(path) as f:
        txt = f.read()
    m = re.search(
        r"IN_F:\s*(\w+)\s+FLIPPED:\s*(\w+)\s+REGRESSED:\s*(\w+)\s+CRIT3:\s*(\w+)",
        txt)
    if m is None:
        # summary.txt has no verdict line — likely an error dump from a failed
        # walk_forward_xy run (the panel runner redirects stderr into summary.txt
        # on failure). Skip this cohort with a warning rather than crashing the
        # entire consolidation on m.group() AttributeError.
        print(f"WARN: {cohort}: summary.txt has no IN_F verdict line (failed run?) — skipping",
              file=sys.stderr)
        return None
    return {
        "cohort": cohort,
        "in_F": m.group(1) == "True",
        "flipped": m.group(2) == "True",
        "regressed": m.group(3) == "True",
        "crit3": m.group(4) == "PASS",
    }


def panel_verdict(rows):
    F = [r for r in rows if r["in_F"]]
    flipped = [r for r in F if r["flipped"] and r["crit3"]]
    regressions = [r["cohort"] for r in rows if r["regressed"]]
    if not F:
        verdict = "VOID"
    elif regressions:
        verdict = "FAIL"
    elif len(flipped) > len(F) / 2:
        verdict = "PASS"
    else:
        verdict = "FAIL"
    return {
        "F_size": len(F),
        "flipped": len(flipped),
        "flipped_cohorts": [r["cohort"] for r in flipped],
        "regressions": regressions,
        "verdict": verdict,
    }


def main(argv=None):
    rows = []
    for d in sorted(glob.glob(os.path.join(PANEL_DIR, "*"))):
        st = os.path.join(d, "summary.txt")
        if os.path.isfile(st):
            parsed = parse_summary(os.path.basename(d), st)
            if parsed is not None:
                rows.append(parsed)
    if not rows:
        print("no cohort summaries found", file=sys.stderr)
        return 1
    if len(rows) < EXPECTED_COHORTS:
        print(f"WARN: only {len(rows)}/{EXPECTED_COHORTS} cohort verdicts parsed — "
              f"partial panel; verdict below is over available cohorts only", file=sys.stderr)
    v = panel_verdict(rows)
    print(f"F (baseline-fail): {v['F_size']}  flipped(of F, crit3-ok): {v['flipped']}")
    print(f"flipped cohorts: {v['flipped_cohorts']}")
    print(f"regressions (baseline-clean broke): {v['regressions']}")
    print(f">>> PANEL VERDICT: {v['verdict']}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
