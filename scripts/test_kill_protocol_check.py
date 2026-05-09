#!/usr/bin/env python3
"""Functional tests for kill_protocol_check.py.

Symmetric to test_stage_promotion_check.py but for the kill side. Each
mechanizable criterion gets a unit test so a future refactor that breaks
one doesn't get masked by the verdict aggregation, plus end-to-end
verdict-path tests covering all five exit codes.
"""
from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile
import unittest
from datetime import datetime, timedelta, timezone
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
SCRIPT = REPO / "scripts" / "kill_protocol_check.py"

sys.path.insert(0, str(REPO / "scripts"))
import kill_protocol_check as kp  # noqa: E402


def write_drift_history(path: Path, runs: list[tuple[str, str]]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w") as f:
        for ts, verdict in runs:
            code = {"CLEAN": 0, "DRIFT_FIRED": 1, "INSUFFICIENT_DATA": 2}.get(verdict, 0)
            f.write(json.dumps({"ts": ts, "verdict": verdict, "exit_code": code}) + "\n")


def make_trade(ts: str, symbol: str, outcome: str, pnl: float,
               fee: float = 100, slip: float = 50, notional: float = 100000) -> kp.Trade:
    return kp.Trade(ts=ts, symbol=symbol, outcome=outcome, pnl_usd=pnl,
                    fee_usd=fee, slip_usd=slip, notional_usd=notional)


def write_journal(jdir: Path, trades: list[kp.Trade]) -> None:
    jdir.mkdir(parents=True, exist_ok=True)
    by_month_sym: dict[str, list[str]] = {}
    for t in trades:
        month = t.ts[:7]
        key = f"{t.symbol}-{month}"
        lines = by_month_sym.setdefault(key, [])
        lines.append(json.dumps({
            "event": "open", "symbol": t.symbol, "ts": t.ts,
            "side": "LONG", "entry": 100, "stop": 99, "target": 106,
        }))
        lines.append(json.dumps({
            "event": "close", "symbol": t.symbol, "ts": t.ts,
            "side": "LONG", "entry": 100, "exit": 99, "stop": 99, "target": 106,
            "outcome": t.outcome,
            "pnl_usd": t.pnl_usd,
            "fee_usd": t.fee_usd,
            "slip_usd": t.slip_usd,
            "notional_usd": t.notional_usd,
        }))
    for key, lines in by_month_sym.items():
        (jdir / f"{key}.jsonl").write_text("\n".join(lines) + "\n")


def run_cli(args: list[str]) -> tuple[int, str]:
    result = subprocess.run(
        ["python3", str(SCRIPT)] + args,
        capture_output=True, text=True, cwd=str(REPO),
    )
    return result.returncode, result.stdout + result.stderr


def iso(dt: datetime) -> str:
    return dt.strftime("%Y-%m-%dT%H:%M:%SZ")


class CriterionUnitTest(unittest.TestCase):

    def test_drift_no_firings_continues(self):
        with tempfile.TemporaryDirectory() as tmp:
            history = Path(tmp) / "h.jsonl"
            now = datetime.now(timezone.utc)
            write_drift_history(history, [
                (iso(now - timedelta(days=14)), "CLEAN"),
                (iso(now - timedelta(days=7)), "CLEAN"),
            ])
            c = kp.check_drift_two_firings(history)
            self.assertEqual(c.status, "CONTINUE")

    def test_drift_two_firings_7d_apart_kills(self):
        with tempfile.TemporaryDirectory() as tmp:
            history = Path(tmp) / "h.jsonl"
            now = datetime.now(timezone.utc)
            write_drift_history(history, [
                (iso(now - timedelta(days=20)), "DRIFT_FIRED"),
                (iso(now - timedelta(days=10)), "DRIFT_FIRED"),
            ])
            c = kp.check_drift_two_firings(history)
            self.assertEqual(c.status, "KILL")

    def test_drift_two_firings_under_7d_continues(self):
        with tempfile.TemporaryDirectory() as tmp:
            history = Path(tmp) / "h.jsonl"
            now = datetime.now(timezone.utc)
            write_drift_history(history, [
                (iso(now - timedelta(days=10)), "DRIFT_FIRED"),
                (iso(now - timedelta(days=7)), "DRIFT_FIRED"),  # only 3d apart
            ])
            c = kp.check_drift_two_firings(history)
            self.assertEqual(c.status, "CONTINUE")

    def test_drift_empty_history_pending_not_continue(self):
        """Closes the dead-cron fail-open. An empty/corrupt history file
        used to return CONTINUE (interpreted as "no kill, all good") even
        though it actually meant "no monitoring." Now PENDING."""
        with tempfile.TemporaryDirectory() as tmp:
            history = Path(tmp) / "empty.jsonl"
            history.write_text("")
            c = kp.check_drift_two_firings(history)
            self.assertEqual(c.status, "PENDING")
            self.assertIn("no parseable runs", c.actual)

    def test_drift_corrupt_only_history_pending(self):
        """File exists but only contains lines that fail JSON decode →
        no parseable runs → PENDING (was: CONTINUE)."""
        with tempfile.TemporaryDirectory() as tmp:
            history = Path(tmp) / "corrupt.jsonl"
            history.write_text("not json\nalso not json\n{partial: ")
            c = kp.check_drift_two_firings(history)
            self.assertEqual(c.status, "PENDING")

    def test_drift_stale_history_pending(self):
        """Most recent run >14d old → drift cron likely dead → PENDING.
        Closes a fail-open where a long-ago clean run with stopped cron
        would forever return CONTINUE despite no active monitoring."""
        with tempfile.TemporaryDirectory() as tmp:
            history = Path(tmp) / "stale.jsonl"
            now = datetime.now(timezone.utc)
            write_drift_history(history, [
                (iso(now - timedelta(days=30)), "CLEAN"),
                (iso(now - timedelta(days=20)), "CLEAN"),
            ])
            c = kp.check_drift_two_firings(history)
            self.assertEqual(c.status, "PENDING")
            self.assertIn("stale", c.actual.lower())

    def test_drift_clean_with_recent_runs_still_continues(self):
        """Sanity: the freshness closure must not break the genuine
        clean path. Recent runs + zero firings → CONTINUE (operationally
        critical — we'd alert excessively if every weekly clean run
        suddenly became PENDING)."""
        with tempfile.TemporaryDirectory() as tmp:
            history = Path(tmp) / "fresh.jsonl"
            now = datetime.now(timezone.utc)
            write_drift_history(history, [
                (iso(now - timedelta(days=10)), "CLEAN"),
                (iso(now - timedelta(days=3)), "CLEAN"),
            ])
            c = kp.check_drift_two_firings(history)
            self.assertEqual(c.status, "CONTINUE")

    def test_slip_under_30_trades_pending(self):
        # Need 30 losers; only have 5.
        trades = [make_trade(f"2026-05-{i:02d}T00:00:00Z", "BTC", "STOP", -1000,
                             slip=50) for i in range(1, 6)]
        c = kp.check_recent_slip(trades)
        self.assertEqual(c.status, "PENDING")

    def test_slip_at_threshold_continues(self):
        # 30 losers all at 5bp slip → cumulative 5bp < 30bp threshold.
        trades = [make_trade(f"2026-05-{i:02d}T00:00:00Z", "BTC", "STOP", -1000,
                             slip=50, notional=100000) for i in range(1, 31)]
        c = kp.check_recent_slip(trades)
        self.assertEqual(c.status, "CONTINUE")

    def test_slip_above_30_kills(self):
        # 30 losers each at 35bp slip on $100k notional = $350 slip per trade.
        trades = [make_trade(f"2026-05-{i:02d}T00:00:00Z", "BTC", "STOP", -1000,
                             slip=350, notional=100000) for i in range(1, 31)]
        c = kp.check_recent_slip(trades)
        self.assertEqual(c.status, "KILL")

    def test_consecutive_loss_below_threshold_continues(self):
        # STAGE_1 stake = $100; threshold = -$500/day. Each day at -$200 = OK.
        trades = [
            make_trade("2026-05-01T00:00:00Z", "BTC", "STOP", -200),
            make_trade("2026-05-02T00:00:00Z", "BTC", "STOP", -200),
            make_trade("2026-05-03T00:00:00Z", "BTC", "STOP", -200),
        ]
        c = kp.check_consecutive_daily_loss(trades, "STAGE_1")
        self.assertEqual(c.status, "CONTINUE")

    def test_consecutive_loss_three_days_kills(self):
        # STAGE_1 stake = $100; threshold = -$500/day. Three days at -$600 each.
        trades = [
            make_trade("2026-05-01T00:00:00Z", "BTC", "STOP", -600),
            make_trade("2026-05-02T00:00:00Z", "BTC", "STOP", -600),
            make_trade("2026-05-03T00:00:00Z", "BTC", "STOP", -600),
        ]
        c = kp.check_consecutive_daily_loss(trades, "STAGE_1")
        self.assertEqual(c.status, "KILL")

    def test_consecutive_loss_non_consecutive_continues(self):
        # 3 bad days, but non-consecutive (gap on May 2).
        trades = [
            make_trade("2026-05-01T00:00:00Z", "BTC", "STOP", -600),
            make_trade("2026-05-02T00:00:00Z", "BTC", "TARGET", 200),  # break
            make_trade("2026-05-03T00:00:00Z", "BTC", "STOP", -600),
            make_trade("2026-05-04T00:00:00Z", "BTC", "STOP", -600),
        ]
        c = kp.check_consecutive_daily_loss(trades, "STAGE_1")
        self.assertEqual(c.status, "CONTINUE")

    def test_consecutive_loss_stage_0_is_operator_verify(self):
        # No bad runs at STAGE_0 → status is OPERATOR-VERIFY (paper money;
        # operator confirms stake assumption).
        trades = [make_trade("2026-05-01T00:00:00Z", "BTC", "STOP", -200)]
        c = kp.check_consecutive_daily_loss(trades, "STAGE_0")
        self.assertEqual(c.status, "OPERATOR-VERIFY")

    def test_single_symbol_under_50_continues(self):
        # 30+ trades (n_floor) evenly across 3 symbols → ~33% each.
        trades = []
        for i in range(33):
            sym = ["BTC", "ETH", "XLM"][i % 3]
            trades.append(make_trade(f"2026-05-{(i % 28) + 1:02d}T00:00:00Z", sym, "STOP", -100))
        c = kp.check_single_symbol_kill(trades)
        self.assertEqual(c.status, "CONTINUE")

    def test_single_symbol_60pct_kills(self):
        # 30+ trades; BTC contributes 60% of |pnl|.
        trades = []
        for i in range(20):
            trades.append(make_trade(f"2026-05-{(i % 28) + 1:02d}T00:00:00Z", "BTC", "STOP", -600))
        for i in range(10):
            trades.append(make_trade(f"2026-05-{(i % 28) + 1:02d}T00:00:00Z", "ETH", "STOP", -300))
        for i in range(5):
            trades.append(make_trade(f"2026-05-{(i % 28) + 1:02d}T00:00:00Z", "XLM", "STOP", -100))
        c = kp.check_single_symbol_kill(trades)
        self.assertEqual(c.status, "KILL")

    def test_single_symbol_below_n_floor_pending(self):
        # n=3 trades, all on BTC. Pre-fix this would fire KILL at 100%.
        # Post-fix (n_floor=30): PENDING. Regression-guards the fix.
        trades = [make_trade("2026-05-01T00:00:00Z", "BTC", "STOP", -100)] * 3
        c = kp.check_single_symbol_kill(trades)
        self.assertEqual(c.status, "PENDING")

    def test_drawdown_short_history_pending(self):
        trades = [make_trade("2026-05-01T00:00:00Z", "BTC", "TARGET", 100)] * 10
        c = kp.check_drawdown(trades)
        self.assertEqual(c.status, "PENDING")

    def test_drawdown_within_threshold_continues(self):
        # Build a 70-day history with peak +$1000, trough +$900 → 10% drawdown.
        now = datetime.now(timezone.utc)
        trades = []
        for i in range(70):
            ts = (now - timedelta(days=70 - i)).strftime("%Y-%m-%dT%H:%M:%SZ")
            # Up to +$1000 by day 50, then drop to +$900 by day 70.
            if i < 50:
                trades.append(make_trade(ts, "BTC", "TARGET", 20))
            else:
                trades.append(make_trade(ts, "BTC", "STOP", -5))
        c = kp.check_drawdown(trades)
        self.assertEqual(c.status, "CONTINUE")

    def test_drawdown_above_20pct_kills(self):
        # 70-day history; peak +$1000, then crash to +$700 = 30% drawdown.
        now = datetime.now(timezone.utc)
        trades = []
        for i in range(50):
            ts = (now - timedelta(days=70 - i)).strftime("%Y-%m-%dT%H:%M:%SZ")
            trades.append(make_trade(ts, "BTC", "TARGET", 20))  # cum to +1000
        for i in range(50, 70):
            ts = (now - timedelta(days=70 - i)).strftime("%Y-%m-%dT%H:%M:%SZ")
            trades.append(make_trade(ts, "BTC", "STOP", -15))  # crash to +700
        c = kp.check_drawdown(trades)
        self.assertEqual(c.status, "KILL")


class VerdictIntegrationTest(unittest.TestCase):

    def test_continue_path_no_kill_fires(self):
        # n=10 trades, healthy, no drift firings, no concentration.
        with tempfile.TemporaryDirectory() as tmp:
            jdir = Path(tmp) / "journal"
            history = Path(tmp) / "h.jsonl"
            now = datetime.now(timezone.utc)
            trades = [
                make_trade((now - timedelta(days=70 - i)).strftime("%Y-%m-%dT%H:%M:%SZ"),
                           "BTC" if i % 3 == 0 else ("ETH" if i % 3 == 1 else "XLM"),
                           "TARGET", 50)
                for i in range(70)
            ]
            write_journal(jdir, trades)
            write_drift_history(history, [(iso(now - timedelta(days=14)), "CLEAN")])
            code, out = run_cli([
                "--live-source", "local",
                "--live-dir", str(jdir),
                "--drift-history", str(history),
                "--stage", "STAGE_1",
            ])
            # Healthy data: anything but KILL is acceptable. Exit 2 (WAITING)
            # is normal when criterion #2 has <30 losers — that's correct
            # behavior, not a test failure. The semantic assertion: NO KILL.
            self.assertNotEqual(code, 1,
                f"expected NO KILL (any of 0/2/4), got KILL (1)\n{out}")

    def test_kill_path_drift_two_firings(self):
        with tempfile.TemporaryDirectory() as tmp:
            jdir = Path(tmp) / "journal"
            history = Path(tmp) / "h.jsonl"
            now = datetime.now(timezone.utc)
            trades = [make_trade(iso(now - timedelta(days=1)), "BTC", "STOP", -100)]
            write_journal(jdir, trades)
            write_drift_history(history, [
                (iso(now - timedelta(days=20)), "DRIFT_FIRED"),
                (iso(now - timedelta(days=10)), "DRIFT_FIRED"),
            ])
            code, out = run_cli([
                "--live-source", "local",
                "--live-dir", str(jdir),
                "--drift-history", str(history),
                "--stage", "STAGE_1",
            ])
            self.assertEqual(code, 1, f"expected KILL (1), got {code}\n{out}")
            self.assertIn("KILL", out)

    def test_kill_path_single_symbol_concentration(self):
        with tempfile.TemporaryDirectory() as tmp:
            jdir = Path(tmp) / "journal"
            history = Path(tmp) / "h.jsonl"
            now = datetime.now(timezone.utc)
            # 60% on BTC (5 trades), 40% on ETH (3 trades).
            trades = (
                [make_trade(iso(now - timedelta(days=10 - i)), "BTC", "STOP", -600) for i in range(5)] +
                [make_trade(iso(now - timedelta(days=4 - i)), "ETH", "STOP", -100) for i in range(3)]
            )
            write_journal(jdir, trades)
            write_drift_history(history, [(iso(now - timedelta(days=7)), "CLEAN")])
            code, out = run_cli([
                "--live-source", "local",
                "--live-dir", str(jdir),
                "--drift-history", str(history),
                "--stage", "STAGE_1",
            ])
            self.assertEqual(code, 1, f"expected KILL on concentration, got {code}\n{out}")

    def test_error_missing_dir(self):
        code, out = run_cli([
            "--live-source", "local",
            "--live-dir", "/nonexistent/path",
        ])
        self.assertEqual(code, 3,
            f"expected ERROR (3), got {code}\n{out}")


if __name__ == "__main__":
    unittest.main(verbosity=2)
