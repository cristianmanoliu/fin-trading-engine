#!/usr/bin/env python3
"""
Functional tests for drift_decompose.py — the committed producer of the
per-metric benign decomposition of drift-detector firings (docs/findings/
2026-07-04.md), prepared as the Rule-6 OPERATOR_REVIEW rationale evidence for
the ~2026-08-25 forward-paper verdict.

These guard the recipe that was first derived by hand on 2026-07-04. A silent
regression here would corrupt the evidence distinguishing structural-benign
drift offsets (cost-scaling, exit-mix, censoring) from real edge deterioration
at the verdict session.

Tests exercise the script via subprocess against synthetic journal fixtures and
assert: (1) like-for-like classification by realized R (NOT the outcome label —
max-hold closes are journaled TARGET/STOP by pnl sign), (2) the benign /
not-benign / insufficient / input-error exit-code contract, (3) live-dir
subdirs (shadow/ layer3/ archive/) are skipped, (4) the frozen backtest
reference reproduces the 2026-07-04 findings numbers byte-for-byte.
"""
from __future__ import annotations

import json
import subprocess
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).parent / "drift_decompose.py"
REPO = Path(__file__).resolve().parent.parent
REF_DIR = REPO / "results" / "hod_journals" / "2026-05-07-mfe"


def write_journal(jdir: Path, symbol: str, events: list[dict]) -> None:
    jdir.mkdir(parents=True, exist_ok=True)
    with (jdir / f"{symbol}-2026-06.jsonl").open("w") as fh:
        for ev in events:
            fh.write(json.dumps(ev) + "\n")


def close_ev(pnl: float, exit_price: float, *, entry: float = 100.0,
             stop: float = 101.0, target: float = 94.0,
             gross: float | None = None, fee: float = 0.0, slip: float = 0.0,
             notional: float = 0.0) -> dict:
    """SHORT close: stop_dist=1.0, target R=6.0 with the defaults.
    exit=94 -> realized 6R (target win); exit=101 -> -1R (stop);
    exit between -> max-hold territory."""
    ev = {
        "event": "close", "symbol": "AAAUSDT", "ts": "2026-06-20T00:00:00Z",
        "side": "SHORT", "entry": entry, "exit": exit_price, "stop": stop,
        "target": target, "pnl_usd": pnl,
        # Deliberately mislabel some outcomes below in tests — the script must
        # classify by realized R, never by this label.
        "outcome": "TARGET" if pnl > 0 else "STOP",
        "reason": "ema_cross SHORT | rr=6.0",
    }
    if gross is not None:
        ev["gross_usd"] = gross
        ev["fee_usd"] = fee
        ev["slip_usd"] = slip
        ev["notional_usd"] = notional
    return ev


def bt_target_win() -> dict:
    return close_ev(5970.0, 94.0)


def bt_stop_loss() -> dict:
    return close_ev(-1065.0, 101.0)


def live_target_win(pnl: float = 5930.0) -> dict:
    return close_ev(pnl, 94.0, gross=6000.0, fee=100.0, slip=0.0, notional=100000.0)


def live_stop_loss(gross: float = -1010.0) -> dict:
    return close_ev(gross - 150.0, 101.0, gross=gross, fee=100.0, slip=50.0,
                    notional=100000.0)


def run_dd(live: Path, bt: Path, extra: list[str] | None = None) -> subprocess.CompletedProcess:
    cmd = ["python3", str(SCRIPT), "--live-dir", str(live), "--backtest-dir", str(bt)]
    return subprocess.run(cmd + (extra or []), capture_output=True, text=True)


class DriftDecomposeTest(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        root = Path(self._tmp.name)
        self.live = root / "live"
        self.bt = root / "bt"

    def tearDown(self):
        self._tmp.cleanup()

    def fill_benign(self):
        """Backtest and live agree like-for-like: target wins within 1%,
        stop overshoot 1000/100000 - stake... gross -1010 on 100k notional
        = 10 USD overshoot = 1bp <= 10bp cap."""
        write_journal(self.bt, "AAAUSDT",
                      [bt_target_win()] * 6 + [bt_stop_loss()] * 25)
        write_journal(self.live, "AAAUSDT",
                      [live_target_win()] * 6 + [live_stop_loss()] * 25)

    def test_benign_case_exit0(self):
        self.fill_benign()
        r = run_dd(self.live, self.bt)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("BENIGN-consistent", r.stdout)
        # like-for-like rows present
        self.assertIn("target wins", r.stdout)
        self.assertIn("stop losses", r.stdout)

    def test_classifies_by_realized_r_not_outcome_label(self):
        # A max-hold winner journaled outcome=TARGET (exit=98 -> realized 2R)
        # must land in the max-hold bucket, not dilute target-win means.
        write_journal(self.bt, "AAAUSDT",
                      [bt_target_win()] * 6 + [bt_stop_loss()] * 25)
        write_journal(self.live, "AAAUSDT",
                      [live_target_win()] * 6 + [live_stop_loss()] * 25
                      + [close_ev(2000.0, 98.0, gross=2050.0, fee=50.0,
                                  notional=100000.0)])
        r = run_dd(self.live, self.bt)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        # 6 target wins (not 7) — the 2R close is max-hold despite its label
        self.assertRegex(r.stdout, r"target wins.*n=\s*6.*n=\s*6")
        self.assertRegex(r.stdout, r"max-hold wins.*n=\s*0.*n=\s*1")

    def test_flip_on_target_win_gap(self):
        # Live target wins 20% below backtest -> real-drift signature -> exit 1.
        write_journal(self.bt, "AAAUSDT",
                      [bt_target_win()] * 6 + [bt_stop_loss()] * 25)
        write_journal(self.live, "AAAUSDT",
                      [live_target_win(4500.0)] * 6 + [live_stop_loss()] * 25)
        r = run_dd(self.live, self.bt)
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("NOT explained", r.stdout)

    def test_flip_on_stop_overshoot(self):
        # Live losers' gross -1150 on 100k notional = 15bp overshoot > 10bp cap.
        write_journal(self.bt, "AAAUSDT",
                      [bt_target_win()] * 6 + [bt_stop_loss()] * 25)
        write_journal(self.live, "AAAUSDT",
                      [live_target_win()] * 6 + [live_stop_loss(-1150.0)] * 25)
        r = run_dd(self.live, self.bt)
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("overshoot", r.stdout)

    def test_insufficient_data_exit2(self):
        write_journal(self.bt, "AAAUSDT",
                      [bt_target_win()] * 6 + [bt_stop_loss()] * 25)
        write_journal(self.live, "AAAUSDT",
                      [live_target_win()] * 2 + [live_stop_loss()] * 3)
        r = run_dd(self.live, self.bt)
        self.assertEqual(r.returncode, 2, r.stdout + r.stderr)
        self.assertIn("INSUFFICIENT", r.stdout + r.stderr)

    def test_input_error_exit3_on_missing_dir(self):
        write_journal(self.live, "AAAUSDT", [live_target_win()])
        r = run_dd(self.live, self.bt / "nope")
        self.assertEqual(r.returncode, 3, r.stdout + r.stderr)

    def test_live_subdirs_skipped(self):
        self.fill_benign()
        # A shadow journal with absurd numbers must not affect the result.
        write_journal(self.live / "shadow" / "alt5-15-504", "AAAUSDT",
                      [close_ev(-99999.0, 101.0, gross=-99999.0, notional=100000.0)] * 50)
        r = run_dd(self.live, self.bt)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)

    @unittest.skipUnless(REF_DIR.is_dir(), "frozen backtest reference not present")
    def test_frozen_reference_reproduces_findings_2026_07_04(self):
        # The committed 2026-05-07-mfe reference is frozen in git; the findings
        # doc numbers must reproduce exactly: 384 target wins mean +5971,
        # 1712 stop losses mean -1068, 107 max-hold wins mean +2107.
        empty_live = Path(self._tmp.name) / "empty"
        empty_live.mkdir()
        r = run_dd(empty_live, REF_DIR)
        # No live data -> insufficient (exit 2), but the backtest table prints.
        self.assertEqual(r.returncode, 2, r.stdout + r.stderr)
        self.assertRegex(r.stdout, r"target wins\s*\|\s*n=\s*384\b.*\+5971")
        self.assertRegex(r.stdout, r"stop losses\s*\|\s*n=\s*1712\b.*-1068")
        self.assertRegex(r.stdout, r"max-hold wins\s*\|\s*n=\s*107\b.*\+2107")


if __name__ == "__main__":
    unittest.main()
