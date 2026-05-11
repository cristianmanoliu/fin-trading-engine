#!/usr/bin/env python3
"""
test_forward_paper_timeline.py — Tests for forward_paper_timeline.py.

Covers:
  - project() pure-function semantics across rate sources
  - Binding-gate selection (which gate fires last)
  - count_live_trades() PARTIAL handling (matches stage_promotion_check)
  - Input-error paths (missing dir, no jsonl files)
  - Override rate path

Run:
  python3 scripts/test_forward_paper_timeline.py
"""
from __future__ import annotations

import json
import subprocess
import sys
import tempfile
import unittest
from datetime import timedelta
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
SCRIPT = REPO / "scripts" / "forward_paper_timeline.py"

sys.path.insert(0, str(REPO / "scripts"))
import forward_paper_timeline as fpt  # noqa: E402


def write_close(jdir: Path, symbol: str, ts: str, outcome: str = "TARGET") -> None:
    """Append an open+close pair to <jdir>/<symbol>-<YYYY-MM>.jsonl."""
    jdir.mkdir(parents=True, exist_ok=True)
    ym = ts[:7]
    path = jdir / f"{symbol}-{ym}.jsonl"
    open_ts = ts.replace("T09:", "T08:")
    with path.open("a") as f:
        f.write(json.dumps({
            "event": "open", "symbol": symbol, "ts": open_ts,
            "side": "LONG", "entry": 100, "stop": 99, "target": 106,
        }) + "\n")
        f.write(json.dumps({
            "event": "close", "symbol": symbol, "ts": ts,
            "side": "LONG", "entry": 100, "exit": 106,
            "pnl_usd": 100, "outcome": outcome,
        }) + "\n")


class CountLiveTradesTest(unittest.TestCase):
    """count_live_trades semantics — must match stage_promotion_check's
    load_journal so projection aligns with formal gate evaluator."""

    def test_empty_dir_returns_zero_zero(self):
        with tempfile.TemporaryDirectory() as tmp:
            closes, files = fpt.count_live_trades(Path(tmp))
            self.assertEqual(closes, 0)
            self.assertEqual(files, 0)

    def test_partial_close_excluded(self):
        with tempfile.TemporaryDirectory() as tmp:
            jdir = Path(tmp)
            write_close(jdir, "BTCUSDT", "2026-05-10T09:00:00Z", outcome="TARGET")
            write_close(jdir, "BTCUSDT", "2026-05-11T09:00:00Z", outcome="PARTIAL")
            write_close(jdir, "BTCUSDT", "2026-05-12T09:00:00Z", outcome="STOP")
            closes, _ = fpt.count_live_trades(jdir)
            # PARTIAL excluded → TARGET + STOP only = 2
            self.assertEqual(closes, 2,
                "PARTIAL closes must be excluded to align with "
                "stage_promotion_check.load_journal semantics")

    def test_shadow_subdir_excluded(self):
        # Top-level jsonl = live; shadow/<label>/*.jsonl = shadows.
        # Timeline tool projects against LIVE count only.
        with tempfile.TemporaryDirectory() as tmp:
            jdir = Path(tmp)
            write_close(jdir, "BTCUSDT", "2026-05-10T09:00:00Z")
            shadow_dir = jdir / "shadow" / "bb20"
            write_close(shadow_dir, "ETHUSDT", "2026-05-10T09:00:00Z")
            closes, _ = fpt.count_live_trades(jdir)
            self.assertEqual(closes, 1,
                "Shadow subdirs must be excluded; only top-level jsonl counted")

    def test_missing_dir_raises(self):
        with self.assertRaises(FileNotFoundError):
            fpt.count_live_trades(Path("/tmp/definitely-not-real-xyz"))

    def test_corrupt_lines_tolerated(self):
        with tempfile.TemporaryDirectory() as tmp:
            jdir = Path(tmp)
            path = jdir / "BTCUSDT-2026-05.jsonl"
            path.write_text(
                json.dumps({"event": "open", "symbol": "BTCUSDT", "ts": "2026-05-10T08:00:00Z"}) + "\n"
                + json.dumps({"event": "close", "symbol": "BTCUSDT", "ts": "2026-05-10T09:00:00Z",
                              "outcome": "TARGET", "pnl_usd": 100}) + "\n"
                + "garbage line\n"
                + json.dumps({"event": "close", "symbol": "BTCUSDT", "ts": "2026-05-10T10:00:00Z",
                              "outcome": "STOP", "pnl_usd": -50}) + "\n"
            )
            closes, _ = fpt.count_live_trades(jdir)
            # Corrupt line skipped; 2 valid closes counted.
            self.assertEqual(closes, 2)


class ProjectionTest(unittest.TestCase):
    """project() is a pure function over (trades, override, now) → Projection.
    No I/O — easy to test exhaustively."""

    def test_observed_rate_used_when_trades_present(self):
        # 60 days elapsed, 70 trades → rate = 70/60 ≈ 1.167/d
        now = fpt.FORWARD_PAPER_START + timedelta(days=60)
        p = fpt.project(trades_closed=70, now=now)
        self.assertEqual(p.rate_source, "observed")
        self.assertAlmostEqual(p.observed_rate_per_day, 70 / 60, places=3)
        self.assertAlmostEqual(p.used_rate_per_day, p.observed_rate_per_day, places=3)

    def test_historical_fallback_when_zero_trades(self):
        # 10 days elapsed, 0 trades → can't compute observed; use fleet rate.
        now = fpt.FORWARD_PAPER_START + timedelta(days=10)
        p = fpt.project(trades_closed=0, now=now)
        self.assertEqual(p.rate_source, "historical_fleet")
        self.assertEqual(p.used_rate_per_day, fpt.HISTORICAL_FLEET_RATE)

    def test_override_rate_takes_precedence(self):
        # User wants what-if projection at 2.0/day.
        now = fpt.FORWARD_PAPER_START + timedelta(days=30)
        p = fpt.project(trades_closed=50, override_rate=2.0, now=now)
        self.assertEqual(p.rate_source, "override")
        self.assertEqual(p.used_rate_per_day, 2.0)
        # n=150 at 2.0/d = 75 days from start.
        expected = fpt.FORWARD_PAPER_START + timedelta(days=150 / 2.0)
        self.assertEqual(p.trade_gate_date.date(), expected.date())

    def test_binding_gate_calendar_when_high_rate(self):
        # At rate=3.0/d, n=150 fires at day 50 — calendar gate (day 60) binds.
        now = fpt.FORWARD_PAPER_START + timedelta(days=5)
        p = fpt.project(trades_closed=15, override_rate=3.0, now=now)
        self.assertEqual(p.binding_gate, "calendar")

    def test_binding_gate_trade_count_at_historical_rate(self):
        # At rate=1.18/d, n=150 fires at day ~127 — well past calendar (day 60).
        # Trade-count gate binds, matching CLAUDE.md note.
        now = fpt.FORWARD_PAPER_START + timedelta(days=5)
        p = fpt.project(trades_closed=6, now=now)  # rate ~ 1.2/d
        self.assertEqual(p.binding_gate, "trade-count")
        self.assertGreater(p.trade_gate_date, p.calendar_gate_date)

    def test_earliest_stage_1_is_max_of_gates(self):
        # Earliest STAGE_1 = LATER of (calendar_gate, trade_gate).
        now = fpt.FORWARD_PAPER_START + timedelta(days=5)
        p = fpt.project(trades_closed=6, now=now)
        self.assertEqual(p.earliest_stage_1, max(p.calendar_gate_date, p.trade_gate_date))

    def test_days_to_stage_1_non_negative(self):
        # Even if STAGE_1 date is in the past (already reachable), days_to >= 0.
        now = fpt.FORWARD_PAPER_START + timedelta(days=200)  # 200d in
        p = fpt.project(trades_closed=300, now=now)
        self.assertGreaterEqual(p.days_to_stage_1, 0)


class RenderTest(unittest.TestCase):
    """Smoke tests for the text rendering — verifies key labels are present."""

    def test_quiet_format_includes_key_fields(self):
        now = fpt.FORWARD_PAPER_START + timedelta(days=30)
        p = fpt.project(trades_closed=40, now=now)
        out = fpt.render(p, quiet=True)
        self.assertIn("day 30", out)
        self.assertIn("40 trades", out)
        self.assertIn("STAGE_1 earliest", out)
        self.assertIn("(", out)  # days-remaining annotation

    def test_full_format_includes_section_headers(self):
        now = fpt.FORWARD_PAPER_START + timedelta(days=30)
        p = fpt.project(trades_closed=40, now=now)
        out = fpt.render(p, quiet=False)
        self.assertIn("Forward-paper STAGE_1 promotion timeline", out)
        self.assertIn("Locked criteria", out)
        self.assertIn("Binding gate:", out)
        self.assertIn("Earliest STAGE_1", out)

    def test_low_rate_warning_surfaces(self):
        # rate < 70% of historical → "well below" note.
        now = fpt.FORWARD_PAPER_START + timedelta(days=30)
        p = fpt.project(trades_closed=15, now=now)  # rate=0.5/d (~42% of 1.18)
        out = fpt.render(p)
        self.assertIn("well below historical", out.lower())

    def test_zero_trades_advisory(self):
        now = fpt.FORWARD_PAPER_START + timedelta(days=10)
        p = fpt.project(trades_closed=0, now=now)
        out = fpt.render(p)
        self.assertIn("zero observed trades", out.lower())


class CLITest(unittest.TestCase):
    """End-to-end CLI invocations."""

    def _run(self, *args: str) -> tuple[int, str, str]:
        cmd = ["python3", str(SCRIPT), *args]
        r = subprocess.run(cmd, capture_output=True, text=True)
        return r.returncode, r.stdout, r.stderr

    def test_missing_journal_dir_exits_3(self):
        code, _, err = self._run("--journal-dir", "/tmp/no-such-path-xyz123")
        self.assertEqual(code, 3, f"expected exit 3 on missing dir, got {code}")
        self.assertIn("not found", err)

    def test_empty_journal_dir_exits_3(self):
        # Dir exists but no .jsonl files = wrong path or fresh-deploy.
        # Surface as input-error (exit 3) with remediation hint.
        with tempfile.TemporaryDirectory() as tmp:
            code, _, err = self._run("--journal-dir", tmp)
            self.assertEqual(code, 3,
                f"empty dir must exit 3 (input error), got {code}\n{err}")
            self.assertIn("no *.jsonl", err)

    def test_synthetic_journal_produces_quiet_summary(self):
        with tempfile.TemporaryDirectory() as tmp:
            jdir = Path(tmp)
            for i in range(10):
                ts = (fpt.FORWARD_PAPER_START + timedelta(days=i)).strftime("%Y-%m-%dT%H:%M:%SZ")
                write_close(jdir, "BTCUSDT", ts)
            code, out, _ = self._run("--journal-dir", str(jdir), "--quiet")
            self.assertEqual(code, 0)
            self.assertIn("trades", out)
            self.assertIn("STAGE_1 earliest", out)

    def test_override_rate_flag_affects_projection(self):
        # Two synthetic journals, same trades; two rate overrides; different
        # projected STAGE_1 dates.
        with tempfile.TemporaryDirectory() as tmp:
            jdir = Path(tmp)
            for i in range(5):
                ts = (fpt.FORWARD_PAPER_START + timedelta(days=i)).strftime("%Y-%m-%dT%H:%M:%SZ")
                write_close(jdir, "BTCUSDT", ts)
            code_a, out_a, _ = self._run("--journal-dir", str(jdir),
                                          "--rate", "1.0", "--quiet")
            code_b, out_b, _ = self._run("--journal-dir", str(jdir),
                                          "--rate", "5.0", "--quiet")
            self.assertEqual(code_a, 0)
            self.assertEqual(code_b, 0)
            # Different rates → different STAGE_1 projections → different output.
            self.assertNotEqual(out_a, out_b,
                "override-rate flag must affect the projected timeline")


if __name__ == "__main__":
    unittest.main(verbosity=2)
