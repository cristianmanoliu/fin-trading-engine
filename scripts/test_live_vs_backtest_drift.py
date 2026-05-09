#!/usr/bin/env python3
"""
Functional tests for live_vs_backtest_drift.py — the decision-grade kill
mechanism for forward-paper.

The detector was Monte-Carlo calibrated offline (DETECTOR_VIABLE,
TP=100% at all operating points; results/drift_detector_calibration_verdict_2026-05-07.md)
but had zero end-to-end tests. A silent regression in this script would
disarm the only kill mechanism we trust.

These tests exercise the script via subprocess against synthetic journal
fixtures and assert exit codes match the locked contract:
  0 = clean (no Bonferroni-significant divergence)
  1 = drift (Bonferroni-significant on ≥1 metric)
  2 = insufficient data (n_live < N_MIN, or fixtures missing)

Run:
  python3 scripts/test_live_vs_backtest_drift.py
"""
from __future__ import annotations

import json
import subprocess
import tempfile
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
DRIFT_SCRIPT = REPO / "scripts" / "live_vs_backtest_drift.py"


def write_journal(jdir: Path, symbol: str, month: str, trades: list[dict]) -> None:
    """Write a synthetic <symbol>-<month>.jsonl with paired open/close events.

    Each trade dict accepts: side, outcome (TARGET/STOP/PARTIAL/TIME),
    pnl_usd, mfe_r (default 0), mae_r (default 0).
    """
    jdir.mkdir(parents=True, exist_ok=True)
    out = jdir / f"{symbol}-{month}.jsonl"
    lines = []
    for i, t in enumerate(trades):
        # Synthetic timestamps — only ordering matters for the parser.
        ts_open = f"2026-05-{(i % 28) + 1:02d}T{(i % 24):02d}:00:00Z"
        ts_close = f"2026-05-{(i % 28) + 1:02d}T{(i % 24):02d}:30:00Z"
        lines.append(json.dumps({
            "event": "open", "ts": ts_open,
            "side": t["side"], "symbol": symbol,
            "entry": 100.0, "stop": 99.0, "target": 106.0,
        }))
        lines.append(json.dumps({
            "event": "close", "ts": ts_close,
            "outcome": t["outcome"],
            "pnl_usd": t["pnl_usd"],
            "mfe_r": t.get("mfe_r", 0.0),
            "mae_r": t.get("mae_r", 0.0),
        }))
    out.write_text("\n".join(lines) + "\n")


def run_drift(backtest_dir: Path, live_dir: Path, n_min: int = 30) -> subprocess.CompletedProcess:
    """Invoke the drift detector with --live-source local. Returns CompletedProcess."""
    return subprocess.run(
        [
            "python3", str(DRIFT_SCRIPT),
            "--backtest-dir", str(backtest_dir),
            "--live-source", "local",
            "--live-dir", str(live_dir),
            "--n-min", str(n_min),
        ],
        capture_output=True, text=True,
    )


def make_winning_strategy(n: int, wr: float = 0.21, win_pnl: float = 6000.0,
                          loss_pnl: float = -1000.0) -> list[dict]:
    """Generate n trades with the given empirical WR. Wins paid +win_pnl, losses -loss_pnl."""
    trades = []
    n_wins = round(n * wr)
    for i in range(n):
        is_win = i < n_wins
        trades.append({
            "side": "LONG",
            "outcome": "TARGET" if is_win else "STOP",
            "pnl_usd": win_pnl if is_win else loss_pnl,
            "mfe_r": 6.0 if is_win else 0.5,
            "mae_r": -0.5 if is_win else -1.0,
        })
    return trades


class DriftDetectorFunctionalTest(unittest.TestCase):

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmpdir = Path(self.tmp.name)
        self.backtest_dir = self.tmpdir / "backtest"
        self.live_dir = self.tmpdir / "live"

    def tearDown(self):
        self.tmp.cleanup()

    def test_insufficient_data_below_n_min_exits_2(self):
        # n_live < N_MIN=30 → INSUFFICIENT regardless of distribution match.
        write_journal(self.backtest_dir, "BTCUSDT", "2025-01",
                      make_winning_strategy(500))
        write_journal(self.live_dir, "BTCUSDT", "2026-05",
                      make_winning_strategy(5))
        result = run_drift(self.backtest_dir, self.live_dir)
        self.assertEqual(result.returncode, 2,
            f"expected exit 2 (INSUFFICIENT), got {result.returncode}\n"
            f"stdout:\n{result.stdout}\nstderr:\n{result.stderr}")
        self.assertIn("INSUFFICIENT DATA", result.stdout)

    def test_zero_live_trades_exits_2(self):
        # Empty live dir → 0 < N_MIN → INSUFFICIENT.
        write_journal(self.backtest_dir, "BTCUSDT", "2025-01",
                      make_winning_strategy(500))
        self.live_dir.mkdir(parents=True, exist_ok=True)
        result = run_drift(self.backtest_dir, self.live_dir)
        self.assertEqual(result.returncode, 2,
            f"expected exit 2 on empty live dir, got {result.returncode}\n"
            f"stdout:\n{result.stdout}")

    def test_clean_identical_distributions_exits_0(self):
        # Backtest and live drawn from the same 21% WR distribution at
        # different sample sizes. Bonferroni at α=0.001/6 = 1.67e-4 should
        # not trip — population means are equal by construction.
        write_journal(self.backtest_dir, "BTCUSDT", "2025-01",
                      make_winning_strategy(500))
        write_journal(self.live_dir, "BTCUSDT", "2026-05",
                      make_winning_strategy(50))
        result = run_drift(self.backtest_dir, self.live_dir)
        self.assertEqual(result.returncode, 0,
            f"expected exit 0 (CLEAN) on matched distributions, got {result.returncode}\n"
            f"stdout:\n{result.stdout}")
        self.assertIn("No Bonferroni-significant drift", result.stdout)

    def test_dead_strategy_all_losses_fires_drift(self):
        # Backtest: healthy 21% WR. Live: 0% WR (every trade stops out).
        # WR proportion z-test should easily clear Bonferroni.
        write_journal(self.backtest_dir, "BTCUSDT", "2025-01",
                      make_winning_strategy(500, wr=0.21))
        write_journal(self.live_dir, "BTCUSDT", "2026-05",
                      make_winning_strategy(50, wr=0.0))
        result = run_drift(self.backtest_dir, self.live_dir)
        self.assertEqual(result.returncode, 1,
            f"expected exit 1 (DRIFT) on dead strategy, got {result.returncode}\n"
            f"stdout:\n{result.stdout}")
        self.assertIn("DRIFT DETECTED", result.stdout)

    def test_pnl_distribution_shift_fires_drift(self):
        # Backtest mean PnL strongly positive; live mean strongly negative
        # at matched WR (so the WR proportion test is fine but pnl mean
        # shifts dramatically). Welch's t on pnl_per_trade should fire.
        write_journal(self.backtest_dir, "BTCUSDT", "2025-01",
                      make_winning_strategy(500, win_pnl=10000.0, loss_pnl=-100.0))
        # Same WR, but each trade is much smaller and the loser is much
        # bigger relative to the winner — net pnl distribution diverges.
        write_journal(self.live_dir, "BTCUSDT", "2026-05",
                      make_winning_strategy(50, win_pnl=100.0, loss_pnl=-10000.0))
        result = run_drift(self.backtest_dir, self.live_dir)
        self.assertEqual(result.returncode, 1,
            f"expected exit 1 (DRIFT) on pnl shift, got {result.returncode}\n"
            f"stdout:\n{result.stdout}")
        self.assertIn("DRIFT DETECTED", result.stdout)

    def test_partial_closes_are_excluded(self):
        # PARTIAL outcomes (B2 mid-R partial-take) must be skipped per the
        # detector's load_local_trades logic. A backtest of healthy trades
        # plus a live journal of n=50 PARTIAL events should be treated as
        # n_live=0 → INSUFFICIENT, not as 50 zero-pnl trades that would
        # flag a synthetic divergence.
        write_journal(self.backtest_dir, "BTCUSDT", "2025-01",
                      make_winning_strategy(500))
        partials = [
            {"side": "LONG", "outcome": "PARTIAL", "pnl_usd": 0.0}
            for _ in range(50)
        ]
        write_journal(self.live_dir, "BTCUSDT", "2026-05", partials)
        result = run_drift(self.backtest_dir, self.live_dir)
        # If PARTIAL skipping works, n_live stays at 0 → exit 2.
        self.assertEqual(result.returncode, 2,
            f"PARTIAL outcomes should be excluded → INSUFFICIENT (exit 2), got {result.returncode}\n"
            f"stdout:\n{result.stdout}")


if __name__ == "__main__":
    unittest.main(verbosity=2)
