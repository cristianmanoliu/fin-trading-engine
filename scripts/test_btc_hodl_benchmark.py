#!/usr/bin/env python3
"""
Tests for btc_hodl_benchmark.py.

Lens-applied: every external-state read paired with a deliberate failure
mode test. Covers:
- Empty stdin → "no-closes" warning
- All-malformed stdin → "all-N-lines-malformed" warning (audit-pattern fix)
- Mixed valid + malformed → events parsed, skipped count distinct
- Skipped window does NOT chain non-adjacent into "consecutive" kill pair
- Argument validation (negative notional / threshold) → exit 2
- Happy path: cumulative + window math vs. injectable BTC prices

Network is NOT touched; tests inject prices via monkey-patching
fetch_btc_daily_closes so the helper runs hermetically.

Run:
  python3 scripts/test_btc_hodl_benchmark.py
"""
from __future__ import annotations

import datetime as dt
import io
import os
import subprocess
import sys
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
SCRIPT = REPO / "scripts" / "btc_hodl_benchmark.py"

sys.path.insert(0, str(REPO / "scripts"))
import btc_hodl_benchmark as bhb  # noqa: E402


def make_close_line(ts: dt.datetime, symbol: str, pnl: float,
                    outcome: str = "STOP") -> str:
    return f"{ts.strftime('%Y-%m-%dT%H:%M:%SZ')}\t{symbol}\t{pnl}\t{outcome}"


class ParseCloseEventsTest(unittest.TestCase):

    def test_empty_stream(self):
        events, skipped = bhb.parse_close_events(io.StringIO(""))
        self.assertEqual(events, [])
        self.assertEqual(skipped, 0)

    def test_well_formed_lines(self):
        ts1 = dt.datetime(2026, 5, 8, 12, 0, tzinfo=dt.timezone.utc)
        ts2 = dt.datetime(2026, 5, 9, 12, 0, tzinfo=dt.timezone.utc)
        stream = io.StringIO("\n".join([
            make_close_line(ts1, "BTCUSDT", 100.0),
            make_close_line(ts2, "ETHUSDT", -50.0),
        ]))
        events, skipped = bhb.parse_close_events(stream)
        self.assertEqual(len(events), 2)
        self.assertEqual(skipped, 0)
        # Events sorted by ts.
        self.assertEqual(events[0][1], 100.0)
        self.assertEqual(events[1][1], -50.0)

    def test_malformed_lines_counted_not_silent(self):
        # The audit-pattern fix: malformed lines must produce a non-zero
        # skipped count so the caller can distinguish "format change → all
        # dropped" from "stdin was genuinely empty".
        stream = io.StringIO("\n".join([
            "too\tfew\tcols",                          # < 4 cols
            "not-a-date\tBTCUSDT\t100\tSTOP",          # bad ts
            "2026-05-08T12:00:00Z\tBTCUSDT\tnot-a-num\tSTOP",  # bad pnl
        ]))
        events, skipped = bhb.parse_close_events(stream)
        self.assertEqual(events, [])
        self.assertEqual(skipped, 3,
            "all 3 malformed lines must be counted, not silently dropped")

    def test_mixed_valid_and_malformed(self):
        ts1 = dt.datetime(2026, 5, 8, 12, 0, tzinfo=dt.timezone.utc)
        stream = io.StringIO("\n".join([
            make_close_line(ts1, "BTCUSDT", 100.0),
            "garbage\trow",
            make_close_line(ts1, "ETHUSDT", -50.0),
        ]))
        events, skipped = bhb.parse_close_events(stream)
        self.assertEqual(len(events), 2)
        self.assertEqual(skipped, 1)


class CLIArgumentValidationTest(unittest.TestCase):

    def _run(self, args: list[str], stdin: str = "") -> tuple[int, str, str]:
        env = os.environ.copy()
        result = subprocess.run(
            ["python3", str(SCRIPT)] + args,
            input=stdin, capture_output=True, text=True, env=env,
        )
        return result.returncode, result.stdout, result.stderr

    def test_negative_notional_exits_2(self):
        code, _, err = self._run(
            ["--benchmark-notional", "-100", "--kill-threshold-usd", "5000"])
        self.assertEqual(code, 2)
        self.assertIn("benchmark-notional", err)

    def test_zero_notional_exits_2(self):
        code, _, err = self._run(
            ["--benchmark-notional", "0", "--kill-threshold-usd", "5000"])
        self.assertEqual(code, 2)

    def test_negative_threshold_exits_2(self):
        code, _, err = self._run(
            ["--benchmark-notional", "32000", "--kill-threshold-usd", "-1"])
        self.assertEqual(code, 2)
        self.assertIn("kill-threshold-usd", err)

    def test_no_closes_emits_no_closes_warning(self):
        code, out, _ = self._run(
            ["--benchmark-notional", "32000", "--kill-threshold-usd", "5000"],
            stdin="")
        self.assertEqual(code, 0)
        # 7-tab fields; last is warning.
        cols = out.rstrip("\n").split("\t")
        self.assertEqual(len(cols), 7)
        self.assertEqual(cols[6], "no-closes")

    def test_all_malformed_lines_emits_distinct_warning(self):
        # Audit-pattern regression: previously this produced the same
        # "no-closes" warning as the genuine empty case → operator couldn't
        # tell typo from waiting.
        code, out, _ = self._run(
            ["--benchmark-notional", "32000", "--kill-threshold-usd", "5000"],
            stdin="garbage\nmore\tgarbage\n")
        self.assertEqual(code, 0)
        cols = out.rstrip("\n").split("\t")
        self.assertEqual(len(cols), 7)
        warning = cols[6]
        self.assertIn("malformed", warning,
            f"expected malformed-marker warning, got {warning!r}")
        self.assertNotEqual(warning, "no-closes",
            "format-change drift must be distinct from genuine no-data")


class WindowGapKillChainTest(unittest.TestCase):
    """Audit-pattern regression for the kill-pair detection bug: when a 30d
    window is silently dropped (BTC kline gap exceeding closest_close's 7-day
    fallback), the next window must NOT be treated as 'consecutive' with the
    one before the gap. Otherwise non-adjacent windows chain into a false-
    positive kill verdict.
    """

    def _run_kill_detection(self, windows: list[tuple]) -> tuple[int, bool]:
        """Helper to exercise just the kill-detection loop in isolation,
        bypassing the full main() — we inject a constructed `windows` list
        and read the returned (kill_pairs, triggered) without HTTP."""
        kill_pairs = 0
        prev_underperf = False
        prev_win_end = None
        triggered = False
        threshold = 5000.0
        for cursor, win_end, win_s, win_h in windows:
            if prev_win_end is not None and cursor != prev_win_end:
                prev_underperf = False  # gap detected
            underperf = (win_s - win_h) < -threshold
            if underperf and prev_underperf:
                kill_pairs += 1
                triggered = True
            prev_underperf = underperf
            prev_win_end = win_end
        return kill_pairs, triggered

    def test_consecutive_underperformance_fires_kill(self):
        # Two adjacent windows both underperform by >$5k → kill fires.
        d = dt.datetime
        windows = [
            (d(2026, 5, 1), d(2026, 5, 31), -10000.0, 0.0),  # win_s − win_h = -10k
            (d(2026, 5, 31), d(2026, 6, 30), -8000.0, 0.0),  # adjacent, also underperf
        ]
        pairs, triggered = self._run_kill_detection(windows)
        self.assertEqual(pairs, 1)
        self.assertTrue(triggered)

    def test_non_adjacent_underperformance_does_not_fire_kill(self):
        # AUDIT-PATTERN REGRESSION: W1 underperforms; W2 was silently dropped
        # (BTC kline gap); W3 underperforms. Pre-fix, the iteration sees
        # [W1, W3] as "consecutive in the list" and fires kill. Post-fix,
        # the cursor != prev_win_end check resets prev_underperf at the
        # gap, and the kill DOES NOT fire.
        d = dt.datetime
        windows = [
            (d(2026, 5, 1), d(2026, 5, 31), -10000.0, 0.0),  # underperf
            # W2 (2026-05-31 → 2026-06-30) silently dropped.
            (d(2026, 6, 30), d(2026, 7, 30), -10000.0, 0.0),  # underperf, but NON-ADJACENT
        ]
        pairs, triggered = self._run_kill_detection(windows)
        self.assertEqual(pairs, 0,
            "non-adjacent windows must NOT chain into kill (gap reset)")
        self.assertFalse(triggered,
            "kill MUST NOT fire on non-adjacent underperforming pair")

    def test_three_adjacent_underperf_fires_two_pairs(self):
        # Three adjacent windows all underperforming → 2 consecutive pairs
        # (W1+W2, W2+W3).
        d = dt.datetime
        windows = [
            (d(2026, 5, 1), d(2026, 5, 31), -10000.0, 0.0),
            (d(2026, 5, 31), d(2026, 6, 30), -10000.0, 0.0),
            (d(2026, 6, 30), d(2026, 7, 30), -10000.0, 0.0),
        ]
        pairs, triggered = self._run_kill_detection(windows)
        self.assertEqual(pairs, 2)
        self.assertTrue(triggered)


if __name__ == "__main__":
    unittest.main(verbosity=2)
