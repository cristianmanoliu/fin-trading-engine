#!/usr/bin/env python3
"""Functional tests for realized_cost_trajectory.py."""
from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
SCRIPT = REPO / "scripts" / "realized_cost_trajectory.py"

sys.path.insert(0, str(REPO / "scripts"))
import realized_cost_trajectory as rc  # noqa: E402


def write_journal(jdir: Path, symbol: str, month: str, closes: list[dict],
                  cohort: str = "live") -> None:
    """Write <jdir>[/<cohort_path>]/<symbol>-<month>.jsonl with paired
    open/close events. cohort='live' → top-level; 'shadow/<label>' →
    shadow/<label>/ subdir."""
    if cohort == "live":
        target = jdir
    else:
        target = jdir / cohort
    target.mkdir(parents=True, exist_ok=True)
    path = target / f"{symbol}-{month}.jsonl"
    lines = []
    for i, c in enumerate(closes):
        ts_open = f"2026-05-08T{i:02d}:00:00Z"
        ts_close = f"2026-05-08T{i:02d}:30:00Z"
        lines.append(json.dumps({
            "event": "open", "symbol": symbol, "ts": ts_open,
            "side": "LONG", "entry": 100, "stop": 99, "target": 106,
        }))
        lines.append(json.dumps({
            "event": "close", "symbol": symbol, "ts": ts_close,
            "side": "LONG", "entry": 100, "exit": 99, "stop": 99, "target": 106,
            **c,
        }))
    path.write_text("\n".join(lines) + "\n")


def run_cli(args: list[str], env: dict | None = None) -> tuple[int, str]:
    cmd = ["python3", str(SCRIPT)] + args
    runenv = os.environ.copy()
    if env:
        runenv.update(env)
    result = subprocess.run(cmd, capture_output=True, text=True, env=runenv)
    return result.returncode, result.stdout + result.stderr


class RealizedCostParserTest(unittest.TestCase):

    def test_skips_pre_decomp_closes(self):
        # closes with notional_usd <= 0 should be excluded — those are the
        # pre-extension closes that don't carry the cost columns.
        with tempfile.TemporaryDirectory() as tmp:
            d = Path(tmp)
            write_journal(d, "BTCUSDT", "2026-05", [
                {"outcome": "STOP", "pnl_usd": -1000, "fee_usd": 100,
                 "slip_usd": 50, "notional_usd": 100000},
                {"outcome": "TARGET", "pnl_usd": 6000, "fee_usd": 0,
                 "slip_usd": 0, "notional_usd": 0},  # pre-decomp
            ])
            trades = rc.load_journals(d)
            self.assertEqual(len(trades), 1, "pre-decomp close should be skipped")

    def test_skips_partial_closes(self):
        with tempfile.TemporaryDirectory() as tmp:
            d = Path(tmp)
            write_journal(d, "BTCUSDT", "2026-05", [
                {"outcome": "PARTIAL", "pnl_usd": 1000, "fee_usd": 50,
                 "slip_usd": 0, "notional_usd": 50000},
                {"outcome": "TARGET", "pnl_usd": 5000, "fee_usd": 50,
                 "slip_usd": 0, "notional_usd": 50000},
            ])
            trades = rc.load_journals(d)
            self.assertEqual(len(trades), 1, "PARTIAL should be skipped")
            self.assertEqual(trades[0].outcome, "TARGET")

    def test_fee_and_slip_bps_math(self):
        # 10bp fee = $100 / $100k; 5bp slip = $50 / $100k.
        with tempfile.TemporaryDirectory() as tmp:
            d = Path(tmp)
            write_journal(d, "BTCUSDT", "2026-05", [
                {"outcome": "STOP", "pnl_usd": -1000, "fee_usd": 100,
                 "slip_usd": 50, "notional_usd": 100000},
            ])
            trades = rc.load_journals(d)
            self.assertEqual(len(trades), 1)
            self.assertAlmostEqual(trades[0].fee_bps, 10.0, places=4)
            self.assertAlmostEqual(trades[0].slip_bps, 5.0, places=4)

    def test_slip_bps_zero_for_target_outcome(self):
        # Winners have slip_usd=0 by design (limit fill). bps reads 0.
        with tempfile.TemporaryDirectory() as tmp:
            d = Path(tmp)
            write_journal(d, "BTCUSDT", "2026-05", [
                {"outcome": "TARGET", "pnl_usd": 5000, "fee_usd": 100,
                 "slip_usd": 0, "notional_usd": 100000},
            ])
            trades = rc.load_journals(d)
            self.assertEqual(trades[0].slip_bps, 0.0)

    def test_picks_up_shadow_subdirs(self):
        with tempfile.TemporaryDirectory() as tmp:
            d = Path(tmp)
            write_journal(d, "BTCUSDT", "2026-05", [
                {"outcome": "STOP", "pnl_usd": -1000, "fee_usd": 100,
                 "slip_usd": 50, "notional_usd": 100000},
            ], cohort="live")
            write_journal(d, "ETHUSDT", "2026-05", [
                {"outcome": "TARGET", "pnl_usd": 5000, "fee_usd": 100,
                 "slip_usd": 0, "notional_usd": 100000},
            ], cohort="shadow/bb20")
            trades = rc.load_journals(d)
            cohorts = {t.cohort for t in trades}
            self.assertEqual(cohorts, {"live", "shadow/bb20"})

    def test_empty_journals_yields_empty(self):
        with tempfile.TemporaryDirectory() as tmp:
            trades = rc.load_journals(Path(tmp))
            self.assertEqual(trades, [])

    def test_render_flags_kill_threshold_breach(self):
        # 13bp fee > 12bp kill → ★fee>kill flag.
        with tempfile.TemporaryDirectory() as tmp:
            d = Path(tmp)
            write_journal(d, "BTCUSDT", "2026-05", [
                {"outcome": "STOP", "pnl_usd": -1000, "fee_usd": 130,
                 "slip_usd": 50, "notional_usd": 100000},
            ])
            trades = rc.load_journals(d)
            out = rc.render(trades, None)
            self.assertIn("★fee>kill", out)

    def test_render_flags_slip_breach(self):
        # 30bp slip > 25bp kill → ★slip>kill flag.
        with tempfile.TemporaryDirectory() as tmp:
            d = Path(tmp)
            write_journal(d, "BTCUSDT", "2026-05", [
                {"outcome": "STOP", "pnl_usd": -1000, "fee_usd": 100,
                 "slip_usd": 300, "notional_usd": 100000},
            ])
            out = rc.render(rc.load_journals(d), None)
            self.assertIn("★slip>kill", out)

    def test_render_cohort_filter(self):
        with tempfile.TemporaryDirectory() as tmp:
            d = Path(tmp)
            write_journal(d, "BTC", "2026-05", [
                {"outcome": "STOP", "pnl_usd": -1, "fee_usd": 100,
                 "slip_usd": 50, "notional_usd": 100000}], cohort="live")
            write_journal(d, "ETH", "2026-05", [
                {"outcome": "STOP", "pnl_usd": -1, "fee_usd": 100,
                 "slip_usd": 50, "notional_usd": 100000}], cohort="shadow/bb20")
            out = rc.render(rc.load_journals(d), cohort_filter="live")
            self.assertIn("BTC", out)
            self.assertNotIn("ETH", out)

    def test_render_summary_passes_when_within_thresholds(self):
        with tempfile.TemporaryDirectory() as tmp:
            d = Path(tmp)
            write_journal(d, "BTCUSDT", "2026-05", [
                {"outcome": "STOP", "pnl_usd": -1000, "fee_usd": 100,
                 "slip_usd": 50, "notional_usd": 100000},
            ])
            out = rc.render(rc.load_journals(d), None)
            self.assertIn("[PASS]", out)
            self.assertNotIn("[FAIL]", out)


class RealizedCostCLITest(unittest.TestCase):

    def test_empty_local_dir_exits_2(self):
        with tempfile.TemporaryDirectory() as tmp:
            code, out = run_cli(["--live-source", "local", "--live-dir", tmp])
            self.assertEqual(code, 2,
                f"expected exit 2 (no qualifying trades), got {code}\n{out}")

    def test_missing_local_dir_exits_3(self):
        code, out = run_cli(
            ["--live-source", "local", "--live-dir", "/nonexistent/path"])
        self.assertEqual(code, 3,
            f"expected exit 3 (input error), got {code}\n{out}")

    def test_valid_local_dir_exits_0(self):
        with tempfile.TemporaryDirectory() as tmp:
            d = Path(tmp)
            write_journal(d, "BTCUSDT", "2026-05", [
                {"outcome": "STOP", "pnl_usd": -1000, "fee_usd": 100,
                 "slip_usd": 50, "notional_usd": 100000},
                {"outcome": "TARGET", "pnl_usd": 5000, "fee_usd": 100,
                 "slip_usd": 0, "notional_usd": 100000},
            ])
            code, out = run_cli(["--live-source", "local", "--live-dir", str(d)])
            self.assertEqual(code, 0,
                f"expected exit 0 (rendered), got {code}\n{out}")
            self.assertIn("Realized cost trajectory", out)
            self.assertIn("BTCUSDT", out)


if __name__ == "__main__":
    unittest.main(verbosity=2)
