#!/usr/bin/env python3
"""
Functional tests for crosscheck9_losers_mae.py — the committed producer of the
cross-check 9 Condition-1 (losers mae_r) + Condition-3 (winner count) blocks that
gate the ~06-07 drift exit-4 HOLD-vs-honor-kill decision.

These guard the recipe that was previously an un-versioned /tmp one-off. A silent
regression here would corrupt the only evidence distinguishing censoring-benign
(HOLD) from real edge-death (honor the kill) under a CRITICAL Telegram alert.

Tests exercise the script via subprocess against synthetic journal fixtures and
assert: (1) the exact distribution block + verdict, (2) the FLIP triggers on
max>1.10 and mean>1.05, (3) subdirs (layer3/ shadow/ testnet/ archive/) are
skipped, (4) the read-only / no-data exit contract.
"""
from __future__ import annotations

import json
import subprocess
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).parent / "crosscheck9_losers_mae.py"


def write_journal(jdir: Path, symbol: str, events: list[dict]) -> None:
    jdir.mkdir(parents=True, exist_ok=True)
    with (jdir / f"{symbol}-2026-05.jsonl").open("w") as fh:
        for ev in events:
            fh.write(json.dumps(ev) + "\n")


def close_ev(symbol: str, outcome: str, pnl: float, mae_r: float) -> dict:
    return {
        "event": "close", "symbol": symbol, "ts": "2026-05-20T00:00:00Z",
        "side": "SHORT", "entry": 1.0, "exit": 1.0, "stop": 1.01, "target": 0.94,
        "pnl_usd": pnl, "outcome": outcome, "reason": "ema_cross SHORT | rr=6.0",
        "mfe_r": 6.0 if outcome == "TARGET" else 0.2, "mae_r": mae_r,
    }


def run_cc9(live_dir: Path, prior_max: float | None = None) -> subprocess.CompletedProcess:
    cmd = ["python3", str(SCRIPT), "--live-dir", str(live_dir)]
    if prior_max is not None:
        cmd += ["--prior-max", str(prior_max)]
    return subprocess.run(cmd, capture_output=True, text=True)


class CrossCheck9Test(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.live = Path(self._tmp.name)

    def tearDown(self):
        self._tmp.cleanup()

    def test_exact_distribution_and_hold_verdict(self):
        # 3 losers at 1.00 / 1.02 / 1.07  ->  min 1.00, max 1.07, mean 1.03, p50 1.02
        # + 1 TARGET winner. All flat -> exit 0 (HOLD-consistent).
        write_journal(self.live, "AAAUSDT", [
            close_ev("AAAUSDT", "STOP", -1000.0, 1.00),
            close_ev("AAAUSDT", "STOP", -1010.0, 1.02),
            close_ev("AAAUSDT", "STOP", -1070.0, 1.07),
            close_ev("AAAUSDT", "TARGET", 6000.0, 0.99),
        ])
        r = run_cc9(self.live)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn("n=3  min=1.0000  max=1.0700  mean=1.0300  p50=1.0200", r.stdout)
        self.assertIn("3 STOP, 1 TARGET", r.stdout)
        self.assertIn("n_closed_winners: 1", r.stdout)
        self.assertIn("✓ flat", r.stdout)

    def test_flip_on_max_above_threshold(self):
        # One loser deepens to 1.20 -> max > 1.10 -> FLIP candidate -> exit 1.
        write_journal(self.live, "BBBUSDT", [
            close_ev("BBBUSDT", "STOP", -1000.0, 1.00),
            close_ev("BBBUSDT", "STOP", -1200.0, 1.20),
        ])
        r = run_cc9(self.live)
        self.assertEqual(r.returncode, 1, r.stderr)
        self.assertIn("FLIP candidate", r.stdout)
        self.assertIn(">1.10 (edge-death threshold): 1 trades", r.stdout)

    def test_flip_on_mean_above_threshold(self):
        # All losers at 1.06: max 1.06 (<=1.10) but mean 1.06 (>1.05) -> FLIP, exit 1.
        write_journal(self.live, "CCCUSDT", [
            close_ev("CCCUSDT", "STOP", -1060.0, 1.06),
            close_ev("CCCUSDT", "STOP", -1060.0, 1.06),
            close_ev("CCCUSDT", "STOP", -1060.0, 1.06),
        ])
        r = run_cc9(self.live)
        self.assertEqual(r.returncode, 1, r.stderr)
        self.assertIn("FLIP candidate", r.stdout)

    def test_subdirs_are_skipped(self):
        # A deepened loser in layer3/ shadow/ testnet/ archive/ must NOT leak into
        # the LIVE losers distribution. Top-level stays flat -> exit 0.
        write_journal(self.live, "LIVEUSDT", [
            close_ev("LIVEUSDT", "STOP", -1000.0, 1.00),
            close_ev("LIVEUSDT", "STOP", -1010.0, 1.01),
        ])
        for sub in ("layer3", "shadow", "testnet", "archive"):
            write_journal(self.live / sub, "POISONUSDT", [
                close_ev("POISONUSDT", "STOP", -9000.0, 9.99),  # would FLIP if read
            ])
        r = run_cc9(self.live)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn("n=2", r.stdout)
        self.assertIn("max=1.0100", r.stdout)
        self.assertNotIn("9.99", r.stdout)

    def test_prior_max_exceedance_count(self):
        # --prior-max 1.05: two losers (1.06, 1.07) exceed it.
        write_journal(self.live, "DDDUSDT", [
            close_ev("DDDUSDT", "STOP", -1000.0, 1.00),
            close_ev("DDDUSDT", "STOP", -1060.0, 1.06),
            close_ev("DDDUSDT", "STOP", -1070.0, 1.07),
        ])
        r = run_cc9(self.live, prior_max=1.05)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn(">1.050 (prior max): 2 trades", r.stdout)

    def test_no_losers_exits_2(self):
        write_journal(self.live, "EEEUSDT", [
            close_ev("EEEUSDT", "TARGET", 6000.0, 0.95),
        ])
        r = run_cc9(self.live)
        self.assertEqual(r.returncode, 2, r.stderr)
        self.assertIn("zero STOP losers", r.stderr)

    def test_empty_dir_exits_2(self):
        r = run_cc9(self.live)
        self.assertEqual(r.returncode, 2, r.stderr)
        self.assertIn("no LIVE close events", r.stderr)

    def test_corrupt_line_is_skipped_not_fatal(self):
        # A garbage trailing line must not crash the cross-check; the valid losers
        # still produce the distribution.
        jdir = self.live
        jdir.mkdir(parents=True, exist_ok=True)
        with (jdir / "FFFUSDT-2026-05.jsonl").open("w") as fh:
            fh.write(json.dumps(close_ev("FFFUSDT", "STOP", -1000.0, 1.00)) + "\n")
            fh.write("{not valid json\n")
            fh.write(json.dumps(close_ev("FFFUSDT", "STOP", -1010.0, 1.01)) + "\n")
        r = run_cc9(jdir)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn("n=2", r.stdout)
        self.assertIn("skipping unparseable line", r.stderr)

    def test_pnl_positive_stop_not_counted_as_loser(self):
        # outcome STOP but pnl>=0 is not a loser (defensive: never happens live,
        # but the loser definition must be exact).
        write_journal(self.live, "GGGUSDT", [
            close_ev("GGGUSDT", "STOP", -1000.0, 1.00),
            close_ev("GGGUSDT", "STOP", 5.0, 9.99),  # non-negative -> excluded
        ])
        r = run_cc9(self.live)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn("n=1", r.stdout)
        self.assertNotIn("9.99", r.stdout)


if __name__ == "__main__":
    unittest.main()
