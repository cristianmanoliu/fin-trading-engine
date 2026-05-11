#!/usr/bin/env python3
"""
test_criterion_coverage.py — Locked-criterion / implementation alignment.

CLAUDE.md "## Forward-paper go/no-go criteria" locks specific numeric
thresholds for the STAGE_0 → STAGE_1 promotion gate and advisory kill
triggers. Multiple tools consume these constants:

  scripts/stage_promotion_check.py     (formal gate evaluator)
  scripts/forward_paper_status.sh      (operator dashboard)
  scripts/forward_paper_resolution.py  (LIMBO rule synthesis)
  scripts/kill_protocol_check.py       (kill mechanism)
  pkg/strategy/entry.go                (TargetRR fallback)

Today (2026-05-11) found FIVE drift instances where these consumers
disagreed with each other and/or with the locked spec:

  T12  TIME-ANCHOR ambiguity (3 pre-reg positions, 4th in impl)
  T13a BTC-HODL notional ($32k locked vs $16k current fleet reality)
  T13b PARTIAL handling between dashboard and gate (latent)
  T13c slip threshold dashboard/gate (HIGH — dashboard PASS at 22bp
       while formal gate FAIL at same value)
  T14  TargetRR fallback (HIGH — 2 sites used 2.0 vs 5 sites used 6.0;
       checkEMACrossover is the LIVE STRATEGY)

This test enumerates each LOCKED constant and asserts every documented
implementation site uses the same value. A future change to any locked
criterion MUST update both CLAUDE.md AND this test AND every impl site
in lockstep — preventing silent drift recurrence.

Run:
  python3 scripts/test_criterion_coverage.py

When a LOCKED value changes (operator decision, pre-reg amendment):
  1. Update CLAUDE.md text
  2. Update LOCKED dict below
  3. Update each impl site
  4. Re-run this test → should pass; if not, you missed a site
"""
from __future__ import annotations

import re
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent


# Locked criterion values per CLAUDE.md. Source of truth. Changes require
# operator approval + matching CLAUDE.md update.
#
# Format: name -> (value, claude_md_section_note)
LOCKED: dict[str, tuple[float, str]] = {
    "MIN_TRADES":          (150,     "deploy criterion 1: ≥150 live trades"),
    "MIN_DAYS":            (60,      "deploy criterion 1: ≥60 calendar days"),
    "MAX_FEE_BPS":         (12.0,    "deploy criterion 2: ≤12bp realized fees"),
    "MAX_SLIP_BPS":        (20.0,    "deploy criterion 3: ≤20bp realized slip"),
    "HONEST_ANNUAL_USD":   (69000.0, "deploy criterion 4: pro-rated annual base"),
    "HONEST_FRACTION":     (0.60,    "deploy criterion 4: 60% of pro-rated"),
    "MAX_SINGLE_SYM_PCT":  (40.0,    "deploy criterion 6: no single sym >40% PnL"),
    "BENCHMARK_NOTIONAL":  (32000.0, "deploy criterion 5: BTC-HODL $32k notional (T13a pending)"),
    "KILL_MAX_SLIP_BP":    (25,      "advisory kill criterion: slip >25bp"),
    "MIN_WR_PCT":          (14,      "advisory kill criterion: WR <14% over ≥150 trades"),
    "TARGET_RR_FALLBACK":  (6.0,     "entry detector fallback when TargetRR<=0 (T14)"),
    "ALPHA_DRIFT":         (0.001,   "drift detector family-wise alpha (decision-grade kill)"),
}


# Each site is (file, regex). Regex must capture the numeric value as
# group 1. The captured value (parsed as float) MUST equal LOCKED[name][0].
#
# Sites are kept narrow (focused on the SoT assignment, not every text
# mention) so a doc-string referencing the value doesn't trip the test.
SITES: dict[str, list[tuple[str, str]]] = {
    "MIN_TRADES": [
        ("scripts/stage_promotion_check.py",
         r"^MIN_TRADES\s*=\s*(\d+)"),
        ("scripts/forward_paper_status.sh",
         r"^MIN_TRADES=(\d+)"),
    ],
    "MIN_DAYS": [
        ("scripts/stage_promotion_check.py",
         r"^MIN_DAYS\s*=\s*(\d+)"),
        ("scripts/forward_paper_status.sh",
         r"^MIN_DAYS=(\d+)"),
    ],
    "MAX_FEE_BPS": [
        ("scripts/stage_promotion_check.py",
         r"^MAX_FEE_BPS\s*=\s*([\d.]+)"),
        ("scripts/forward_paper_status.sh",
         r"^MAX_FEE_BPS=([\d.]+)"),
    ],
    "MAX_SLIP_BPS": [
        ("scripts/stage_promotion_check.py",
         r"^MAX_SLIP_BPS\s*=\s*([\d.]+)"),
        ("scripts/forward_paper_status.sh",
         r"^MAX_SLIP_BPS=([\d.]+)"),
    ],
    "HONEST_ANNUAL_USD": [
        ("scripts/stage_promotion_check.py",
         r"^HONEST_ANNUAL_USD\s*=\s*([\d.]+)"),
        ("scripts/forward_paper_resolution.py",
         r"^EXPECTED_ANNUAL_PNL_USD\s*=\s*(\d+)"),
    ],
    "HONEST_FRACTION": [
        ("scripts/stage_promotion_check.py",
         r"^HONEST_FRACTION\s*=\s*([\d.]+)"),
    ],
    "MAX_SINGLE_SYM_PCT": [
        ("scripts/stage_promotion_check.py",
         r"^MAX_SINGLE_SYM_PCT\s*=\s*([\d.]+)"),
    ],
    "BENCHMARK_NOTIONAL": [
        ("scripts/stage_promotion_check.py",
         # Default fallback inside os.environ.get("BENCHMARK_NOTIONAL", "32000")
         r'os\.environ\.get\("BENCHMARK_NOTIONAL",\s*"(\d+)"\)'),
        ("scripts/forward_paper_status.sh",
         # Default value in `${VAR:-default}` syntax.
         r'BENCHMARK_NOTIONAL="\$\{BENCHMARK_NOTIONAL:-(\d+)\}"'),
    ],
    "KILL_MAX_SLIP_BP": [
        ("scripts/forward_paper_status.sh",
         r"^KILL_MAX_SLIP_BP=(\d+)"),
    ],
    "MIN_WR_PCT": [
        ("scripts/forward_paper_status.sh",
         r"^MIN_WR_PCT=(\d+)"),
    ],
    "TARGET_RR_FALLBACK": [
        # entry.go has 7 fallback sites (post-T14 all aligned to 6.0).
        # The Go regression test TestTargetRRFallbackConsistency already
        # pins all 7 sites via source-scan. This Python test acts as a
        # belt-and-suspenders cross-language assertion that the agreed
        # value remains 6.0.
        ("pkg/strategy/entry.go",
         r"(?:targetMult|rr)\s*=\s*6\.0\b"),
    ],
    "ALPHA_DRIFT": [
        ("scripts/live_vs_backtest_drift.py",
         r"^ALPHA\s*=\s*([\d.]+)"),
    ],
}


class CriterionCoverageTest(unittest.TestCase):
    """For each locked criterion, verify every documented implementation
    site uses the same value. A SITE entry missing from this map is NOT
    an immediate test failure — it just means that consumer isn't pinned.
    Failures indicate either:

    (1) An impl drifted from the locked value (BUG — fix the impl).
    (2) The locked value was changed without updating this test (PROCESS
        — operator must update LOCKED and re-validate all sites).
    """

    def _read(self, rel_path: str) -> str:
        path = REPO / rel_path
        self.assertTrue(path.is_file(),
                        f"site path missing: {rel_path}")
        return path.read_text()

    def _assert_site_value(self, name: str, file_path: str,
                           pattern: str, expected: float) -> None:
        """Find every match of `pattern` in `file_path` and assert each
        captured value parses to `expected` (float-equality)."""
        text = self._read(file_path)
        matches = re.findall(pattern, text, re.MULTILINE)
        self.assertTrue(
            matches,
            f"{name}: pattern not found in {file_path}\n"
            f"  pattern: {pattern!r}\n"
            f"  Likely a refactor moved the constant; update SITES map.")
        for match in matches:
            # Some patterns don't capture a group (e.g., the TARGET_RR
            # pattern uses a non-capturing alternation). In that case
            # re.findall returns the full match string; coerce.
            captured = match if isinstance(match, str) else match[0]
            # Match strings may contain just the numeric value already
            # (capturing group) OR the entire matched substring (no
            # explicit capture group with parentheses). Extract a
            # plausible number from whatever we got.
            num_match = re.search(r"[\d.]+", captured)
            self.assertIsNotNone(
                num_match,
                f"{name}: matched string has no numeric component: "
                f"{captured!r} in {file_path}")
            actual = float(num_match.group())
            self.assertAlmostEqual(
                actual, expected, places=4,
                msg=(f"{name} drift in {file_path}: locked={expected} "
                     f"vs actual={actual}\n"
                     f"  matched: {captured!r}\n"
                     f"  pattern: {pattern!r}\n"
                     f"  ACTION: either fix the impl OR update LOCKED + "
                     f"CLAUDE.md (locked-criterion change requires "
                     f"operator approval)."))

    def test_all_locked_criteria_present_in_SITES(self):
        """Every entry in LOCKED should have at least one impl site in
        SITES. A LOCKED entry with no SITES means that criterion isn't
        being enforced — operator should know."""
        missing = [k for k in LOCKED if k not in SITES or not SITES[k]]
        self.assertFalse(
            missing,
            f"LOCKED criteria without enforcement sites: {missing}\n"
            f"  Add at least one (file, regex) pair to SITES for each, "
            f"or document why no enforcement is needed.")

    def test_every_site_matches_locked_value(self):
        """The core enforcement: scan each documented site, verify the
        captured numeric value equals the LOCKED value."""
        failures = []
        for name, sites in SITES.items():
            if name not in LOCKED:
                failures.append(f"SITES has '{name}' not in LOCKED — "
                                "add to LOCKED or remove from SITES")
                continue
            expected = LOCKED[name][0]
            for file_path, pattern in sites:
                try:
                    self._assert_site_value(name, file_path, pattern, expected)
                except AssertionError as e:
                    failures.append(str(e))

        if failures:
            details = "\n\n".join(failures)
            self.fail(f"{len(failures)} criterion-coverage failure(s):\n\n{details}")


class LockedValuesSanityTest(unittest.TestCase):
    """Defensive: the LOCKED dict above is the source of truth in this
    test file. Pin the values against CLAUDE.md's documented numbers so
    a typo in the LOCKED dict can't silently align all impl sites to a
    WRONG value (which would pass the cross-check above but be invalid).

    These assertions are duplicate-by-design — they encode the SAME
    numbers as LOCKED, in a separate location, so a single typo can't
    propagate. A real LOCKED change requires updating BOTH sites.
    """

    def test_min_trades_is_150(self):
        self.assertEqual(LOCKED["MIN_TRADES"][0], 150,
            "CLAUDE.md: '≥150 live trades' — pre-reg change requires "
            "operator approval (pre-registration discipline).")

    def test_min_days_is_60(self):
        self.assertEqual(LOCKED["MIN_DAYS"][0], 60,
            "CLAUDE.md: '≥60 calendar days net-positive'")

    def test_max_fee_bps_is_12(self):
        self.assertEqual(LOCKED["MAX_FEE_BPS"][0], 12.0,
            "CLAUDE.md: 'Realized round-trip taker fees ≤ 12 bp'")

    def test_max_slip_bps_is_20(self):
        self.assertEqual(LOCKED["MAX_SLIP_BPS"][0], 20.0,
            "CLAUDE.md: 'Realized stop-side slippage ≤ 20 bp on the "
            "losing-trade subsample' (DEPLOY criterion, NOT kill threshold 25bp)")

    def test_honest_annual_is_69k(self):
        self.assertEqual(LOCKED["HONEST_ANNUAL_USD"][0], 69000.0,
            "CLAUDE.md: '$69k/yr × elapsed-fraction × 0.60'")

    def test_honest_fraction_is_60pct(self):
        self.assertEqual(LOCKED["HONEST_FRACTION"][0], 0.60,
            "CLAUDE.md: '60% of pro-rated honest-annual'")

    def test_max_single_sym_is_40pct(self):
        self.assertEqual(LOCKED["MAX_SINGLE_SYM_PCT"][0], 40.0,
            "CLAUDE.md: 'No single symbol contributes >40% of cumulative live PnL'")

    def test_benchmark_notional_is_32k(self):
        self.assertEqual(LOCKED["BENCHMARK_NOTIONAL"][0], 32000.0,
            "CLAUDE.md: '$32k notional' (T13a pending operator decision "
            "about deployed-32 vs deployed-16 reality)")

    def test_kill_slip_threshold_is_25(self):
        self.assertEqual(LOCKED["KILL_MAX_SLIP_BP"][0], 25,
            "CLAUDE.md kill criterion: 'Realized stop-side slippage > 25 bp'")

    def test_min_wr_pct_is_14(self):
        self.assertEqual(LOCKED["MIN_WR_PCT"][0], 14,
            "CLAUDE.md kill criterion: 'Realized WR < 14% over ≥150 trades' "
            "(breakeven ≈ 14.3% at 6:1 RR)")

    def test_target_rr_fallback_is_6(self):
        self.assertEqual(LOCKED["TARGET_RR_FALLBACK"][0], 6.0,
            "CLAUDE.md candidate strategy: 'fixed 6:1 R:R take-profit' "
            "(T14 aligned all 7 entry.go fallback sites to 6.0)")

    def test_alpha_drift_is_001(self):
        self.assertEqual(LOCKED["ALPHA_DRIFT"][0], 0.001,
            "CLAUDE.md drift detector: 'α_family=0.001' (calibration verdict 2026-05-07)")


if __name__ == "__main__":
    unittest.main(verbosity=2)
