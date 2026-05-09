#!/usr/bin/env python3
"""
Functional tests for forward_paper_trajectory.py.

Covers parser robustness, single-vs-multi-snapshot rendering, cohort
filtering, and graceful handling of partial/malformed snapshots.

Run:
  python3 scripts/test_forward_paper_trajectory.py
"""
from __future__ import annotations

import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
TRAJ = REPO / "scripts" / "forward_paper_trajectory.py"

# Importable parser/renderer for unit-level tests.
sys.path.insert(0, str(REPO / "scripts"))
import forward_paper_trajectory as t  # noqa: E402


def make_snapshot(days: int, trades: int, wr_pct: float, pnl: int,
                  fee: float = 10.0, slip: float = 5.0,
                  single_pct: float = 100.0,
                  cohort: str = "live") -> str:
    """Build a forward-paper-status-style cohort section."""
    return f"""
  ── {cohort} ───────────────────────────────────────────────────────────────────
    Days elapsed:           {days} / 60              [IN-PROGRESS]
    Trades closed:          {trades} / 150              [IN-PROGRESS]
    Wins / WR:              0 / {wr_pct}%             [PENDING]
    Net PnL:             ${pnl:+d}                     [INSUFFICIENT]
    Realized fee bps:    {fee:.2f}   / ≤12bp                [PASS]
    Realized slip bps:   {slip:.2f}    / ≤25bp (losers)       [PASS]
    BTC-HODL Δ vs $32000: $-1306         (HODL=$+0)  [INSUFFICIENT]
    30d windows / kill-pairs: 0 / 0                    [PENDING]
    Single-sym pct:      {single_pct:.1f}% (XLMUSDT)        [INSUFFICIENT]
    Top symbols:         XLMUSDT@-1306
    First trade:         2026-05-08T08:31:59
    Last trade:          2026-05-08T08:31:59
    >>> VERDICT: WAITING (insufficient data)
"""


class TrajectoryParserTest(unittest.TestCase):

    def test_single_cohort_parses_all_metrics(self):
        with tempfile.TemporaryDirectory() as tmp:
            p = Path(tmp) / "2026-05-08.txt"
            p.write_text(make_snapshot(days=0, trades=5, wr_pct=20.0, pnl=-1000))
            snap = t.parse_snapshot(p)
            self.assertEqual(snap.date, "2026-05-08")
            self.assertIn("live", snap.cohorts)
            self.assertEqual(snap.cohorts["live"]["days"], "0")
            self.assertEqual(snap.cohorts["live"]["trades"], "5")
            self.assertEqual(snap.cohorts["live"]["wr_pct"], "20.0")
            self.assertEqual(snap.cohorts["live"]["pnl_usd"], "-1000")
            self.assertEqual(snap.cohorts["live"]["fee_bps"], "10.00")
            self.assertEqual(snap.cohorts["live"]["slip_bps"], "5.00")
            self.assertEqual(snap.cohorts["live"]["single_pct"], "100.0")

    def test_multiple_cohorts_parsed_independently(self):
        with tempfile.TemporaryDirectory() as tmp:
            text = (
                make_snapshot(days=0, trades=1, wr_pct=0.0, pnl=-1306, cohort="live")
                + make_snapshot(days=0, trades=2, wr_pct=0.0, pnl=-2143, cohort="shadow/alt5-15-336")
                + make_snapshot(days=0, trades=3, wr_pct=33.3, pnl=+500, cohort="shadow/bb20")
            )
            p = Path(tmp) / "2026-05-08.txt"
            p.write_text(text)
            snap = t.parse_snapshot(p)
            self.assertEqual(set(snap.cohorts.keys()),
                             {"live", "shadow/alt5-15-336", "shadow/bb20"})
            self.assertEqual(snap.cohorts["shadow/bb20"]["pnl_usd"], "+500")

    def test_renders_baseline_message_for_single_snapshot(self):
        with tempfile.TemporaryDirectory() as tmp:
            p = Path(tmp) / "2026-05-08.txt"
            p.write_text(make_snapshot(days=0, trades=1, wr_pct=0.0, pnl=-1306))
            snap = t.parse_snapshot(p)
            out = t.render_trajectory([snap])
            self.assertIn("1 snapshot", out)
            self.assertIn("baseline-only", out)
            # No Δ column when only one snapshot.
            self.assertNotIn("Δ", out)

    def test_renders_delta_column_for_multi_snapshot(self):
        with tempfile.TemporaryDirectory() as tmp:
            d = Path(tmp)
            (d / "2026-05-08.txt").write_text(
                make_snapshot(days=0, trades=1, wr_pct=0.0, pnl=-1306))
            (d / "2026-05-15.txt").write_text(
                make_snapshot(days=7, trades=10, wr_pct=20.0, pnl=+2400))
            snaps = [t.parse_snapshot(p) for p in sorted(d.glob("*.txt"))]
            out = t.render_trajectory(snaps)
            self.assertIn("2 snapshots", out)
            self.assertIn("2026-05-08", out)
            self.assertIn("2026-05-15", out)
            self.assertIn("Δ", out)
            # Trades went 1→10, Δ = +9.
            self.assertRegex(out, r"Trades closed.*\b1\b.*\b10\b.*\+\s*9\.00")
            # PnL went -1306 → +2400, Δ = +3706.
            self.assertRegex(out, r"Net PnL.*-1306.*2400.*\+\s*3706\.00")

    def test_cohort_filter_isolates_single_cohort(self):
        with tempfile.TemporaryDirectory() as tmp:
            p = Path(tmp) / "2026-05-08.txt"
            p.write_text(
                make_snapshot(days=0, trades=1, wr_pct=0.0, pnl=-1306, cohort="live")
                + make_snapshot(days=0, trades=2, wr_pct=0.0, pnl=-2143, cohort="shadow/alt5-15-336")
            )
            snap = t.parse_snapshot(p)
            out = t.render_trajectory([snap], cohort_filter="live")
            self.assertIn("cohort: live", out)
            self.assertNotIn("cohort: shadow/alt5-15-336", out)

    def test_cohort_filter_unknown_cohort_returns_message(self):
        with tempfile.TemporaryDirectory() as tmp:
            p = Path(tmp) / "2026-05-08.txt"
            p.write_text(make_snapshot(days=0, trades=1, wr_pct=0.0, pnl=-1306))
            snap = t.parse_snapshot(p)
            out = t.render_trajectory([snap], cohort_filter="shadow/foo")
            self.assertIn("not found", out)

    def test_missing_metric_renders_em_dash(self):
        # Cohort header but no metric body — defensive. Real-world: a cohort
        # with no closes ("no closes yet — N open" line) gets parsed as
        # an empty metric dict.
        with tempfile.TemporaryDirectory() as tmp:
            p = Path(tmp) / "2026-05-08.txt"
            p.write_text(
                "  ── shadow/bb20 ───────────────────────────\n"
                "  no closes yet — 13 open (13 LONG, 0 SHORT)\n"
            )
            snap = t.parse_snapshot(p)
            out = t.render_trajectory([snap])
            self.assertIn("cohort: shadow/bb20", out)
            self.assertIn("—", out)


class TrajectoryCLITest(unittest.TestCase):

    def test_missing_dir_exits_2(self):
        result = subprocess.run(
            ["python3", str(TRAJ), "--dir", "/nonexistent/path"],
            capture_output=True, text=True,
        )
        self.assertEqual(result.returncode, 2)
        self.assertIn("snapshot dir not found", result.stderr)

    def test_empty_dir_exits_2(self):
        with tempfile.TemporaryDirectory() as tmp:
            result = subprocess.run(
                ["python3", str(TRAJ), "--dir", tmp],
                capture_output=True, text=True,
            )
            self.assertEqual(result.returncode, 2)
            self.assertIn("no snapshots", result.stderr)

    def test_real_snapshot_dir_renders_successfully(self):
        # Smoke test against the actual repo snapshot dir if present.
        snap_dir = REPO / "results" / "forward_paper_snapshots"
        if not snap_dir.is_dir() or not list(snap_dir.glob("*.txt")):
            self.skipTest("no real snapshots checked in")
        result = subprocess.run(
            ["python3", str(TRAJ)],
            capture_output=True, text=True, cwd=str(REPO),
        )
        self.assertEqual(result.returncode, 0,
            f"trajectory failed:\n{result.stderr}")
        self.assertIn("trajectory", result.stdout.lower())


if __name__ == "__main__":
    unittest.main(verbosity=2)
