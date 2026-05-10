#!/usr/bin/env python3
"""
Functional tests for forward_paper_status.sh.

Drives the bash script in `local` mode via subprocess with a constructed
JOURNAL_DIR so the audit-pattern fixes (typo detection, JOURNAL_DIR
override honoring, helper-output validation, top_syms abs-PnL sort) all
have regression coverage. Avoids ssh + the Binance price-fetch path by
using local mode and zero open positions.

Run:
  python3 scripts/test_forward_paper_status.py
"""
from __future__ import annotations

import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
SCRIPT = REPO / "scripts" / "forward_paper_status.sh"


def write_close(jdir: Path, symbol: str, ts: str, pnl: float,
                outcome: str = "STOP",
                fee: float = 100.0, slip: float = 50.0,
                notional: float = 100000.0,
                cohort: str = "live") -> None:
    """Append a paired open + close to the cohort's journal file. Designed
    to produce zero open positions (one open + one close per pair) so the
    test path doesn't hit the Binance price-fetch network call."""
    if cohort == "live":
        target = jdir
    else:
        target = jdir / cohort
    target.mkdir(parents=True, exist_ok=True)
    ym = ts[:7]  # YYYY-MM
    path = target / f"{symbol}-{ym}.jsonl"
    open_evt = json.dumps({
        "event": "open", "symbol": symbol,
        "ts": ts.replace("close", "open"),
        "side": "LONG", "entry": 100, "stop": 99, "target": 106,
    })
    close_evt = json.dumps({
        "event": "close", "symbol": symbol, "ts": ts,
        "side": "LONG", "entry": 100, "exit": 99,
        "outcome": outcome, "pnl_usd": pnl,
        "fee_usd": fee, "slip_usd": slip, "notional_usd": notional,
    })
    with path.open("a") as f:
        f.write(open_evt + "\n")
        f.write(close_evt + "\n")


def run_script(journal_dir: str, host: str = "local",
               extra_env: dict | None = None) -> tuple[int, str, str]:
    env = os.environ.copy()
    env["JOURNAL_DIR"] = journal_dir
    if extra_env:
        env.update(extra_env)
    result = subprocess.run(
        ["bash", str(SCRIPT), host],
        capture_output=True, text=True, env=env,
    )
    return result.returncode, result.stdout, result.stderr


class TypoGuardTest(unittest.TestCase):

    def test_missing_journal_dir_exits_2(self):
        # Audit-pattern regression: a typo'd JOURNAL_DIR used to silently
        # render "(no data yet)" — indistinguishable from a legitimate
        # fresh-deploy state. The fix detects the missing dir and exits 2
        # with a stderr message naming the path + remediation.
        code, out, err = run_script("/tmp/definitely-not-a-real-path-xyz123")
        self.assertEqual(code, 2,
            f"expected exit 2 (typo'd dir), got {code}\nstdout:\n{out}\nstderr:\n{err}")
        self.assertIn("not found", err)
        self.assertIn("operator misconfiguration", err)
        self.assertIn("definitely-not-a-real-path-xyz123", err)

    def test_empty_journal_dir_does_not_falsely_typo(self):
        # An empty directory IS a legitimate fresh-deploy state. Must NOT
        # be confused with a typo (which is "directory missing entirely").
        with tempfile.TemporaryDirectory() as tmp:
            code, out, err = run_script(tmp)
            self.assertNotEqual(code, 2,
                f"empty dir should NOT trip the typo guard\nstderr:\n{err}")
            # Empty dir → live cohort emits NODATA → script renders
            # "(no data yet)" line and exits 0.
            self.assertIn("(no data yet)", out)


class JournalDirOverrideTest(unittest.TestCase):

    def test_local_journal_dir_override_renders_synthetic_data(self):
        # Verify JOURNAL_DIR override is actually consumed in local mode.
        # If the override were ignored (the previous remote-mode bug
        # mirrored locally), the synthetic XLMUSDT close would not appear
        # in the rendered output.
        with tempfile.TemporaryDirectory() as tmp:
            d = Path(tmp)
            write_close(d, "XLMUSDT", "2026-05-08T08:31:59Z", -1306.45)
            code, out, err = run_script(str(d))
            self.assertEqual(code, 0,
                f"expected exit 0, got {code}\nstderr:\n{err}")
            self.assertIn("XLMUSDT", out,
                "synthetic close was not parsed — JOURNAL_DIR override "
                "may not be honored")
            self.assertIn("Trades closed:          1", out)


class TopSymsSortTest(unittest.TestCase):

    def test_top_syms_sorted_by_absolute_pnl(self):
        # Audit-pattern regression: previously sort -gr ranked by signed
        # PnL, so in mixed cohorts (winners + losers) the biggest losers
        # were hidden behind the biggest winners. The fix sorts by abs
        # PnL — consistent with the single_sym_pct gate.
        # Construct a mixed cohort: 1 big winner (+5000) + 1 big loser
        # (-9000). Expected: big loser appears first in top_syms.
        with tempfile.TemporaryDirectory() as tmp:
            d = Path(tmp)
            write_close(d, "WINNERUSDT", "2026-05-08T08:00:00Z",
                        +5000.0, outcome="TARGET", slip=0.0)
            write_close(d, "LOSERUSDT",  "2026-05-08T09:00:00Z",
                        -9000.0, outcome="STOP")
            code, out, err = run_script(str(d))
            self.assertEqual(code, 0, f"stderr:\n{err}")
            # Find the "Top symbols:" line and check ordering.
            top_line = next((ln for ln in out.splitlines()
                             if "Top symbols:" in ln), None)
            self.assertIsNotNone(top_line, f"no Top symbols line in:\n{out}")
            loser_pos = top_line.find("LOSERUSDT")
            winner_pos = top_line.find("WINNERUSDT")
            self.assertGreaterEqual(loser_pos, 0,
                f"LOSERUSDT missing from top: {top_line}")
            self.assertGreaterEqual(winner_pos, 0,
                f"WINNERUSDT missing from top: {top_line}")
            self.assertLess(loser_pos, winner_pos,
                "biggest absolute contributor (LOSERUSDT $9k) must "
                f"appear before WINNERUSDT $5k. Got: {top_line!r}")


class HodlHelperValidationTest(unittest.TestCase):
    """Helper-output validation regression. Replaces $HODL_HELPER with a
    fake script that emits the right number of fields but with garbage
    numerics — must route to PENDING, not silent PASS."""

    def _run_with_fake_helper(self, helper_body: str) -> tuple[int, str, str]:
        with tempfile.TemporaryDirectory() as tmp:
            d = Path(tmp)
            write_close(d, "XLMUSDT", "2026-05-08T08:31:59Z", -1306.0)
            # Fake helper sits inside the journal tmpdir; we path it via
            # extra_env that the script doesn't read directly. Instead,
            # inject by replacing scripts/btc_hodl_benchmark.py path is
            # not feasible — the script computes HODL_HELPER from its own
            # dirname. So we drop the fake into a dedicated dir and patch
            # the script's PATH... actually, simpler: write the fake to
            # the same dirname the script computes (scripts/) is too
            # invasive. We instead leverage the fact that the script's
            # helper invocation uses `[[ -x "$HODL_HELPER" ]]`. By
            # temporarily renaming the real helper, we exercise the
            # missing-helper path; that's a sibling concern but verifies
            # the helper_failed=1 routing.
            # For full helper-malformed-output testing we'd need to fork
            # the script — out of scope for this regression. Instead this
            # class verifies the missing-helper routing already in place.
            real_helper = REPO / "scripts" / "btc_hodl_benchmark.py"
            backup = REPO / "scripts" / "btc_hodl_benchmark.py.testbak"
            had_real = real_helper.exists()
            if had_real:
                real_helper.rename(backup)
            try:
                fake_path = REPO / "scripts" / "btc_hodl_benchmark.py"
                fake_path.write_text("#!/bin/bash\n" + helper_body + "\n")
                fake_path.chmod(0o755)
                code, out, err = run_script(str(d))
            finally:
                if fake_path.exists():
                    fake_path.unlink()
                if had_real:
                    backup.rename(real_helper)
            return code, out, err

    def test_helper_emits_garbage_numerics_routes_to_pending(self):
        # Helper outputs 7 tab-separated fields, but numerics are non-
        # parseable strings. Without the validation fix, the awk math
        # downstream would coerce "banana" → 0 and report PASS.
        helper_body = (
            'printf "%s\\t%s\\t%s\\t%s\\t%s\\t%s\\t%s\\n" '
            '"banana" "split" "icecream" "sundae" "cherry" "yum" ""'
        )
        code, out, err = self._run_with_fake_helper(helper_body)
        self.assertEqual(code, 0,
            f"expected exit 0 (rendered, but PENDING), got {code}\n{err}")
        # The HODL line must show PENDING (validation rejected garbage)
        # rather than render the garbage values + PASS.
        hodl_line = next((ln for ln in out.splitlines()
                          if "BTC-HODL" in ln), None)
        self.assertIsNotNone(hodl_line, f"no BTC-HODL line in:\n{out}")
        self.assertIn("PENDING", hodl_line,
            f"garbage helper output must route to PENDING, got: {hodl_line!r}")


if __name__ == "__main__":
    unittest.main(verbosity=2)
