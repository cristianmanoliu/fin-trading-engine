#!/usr/bin/env python3
"""
Functional tests for layer3_verdict.sh.

Drives the wrapper via subprocess with synthetic journal fixtures so the
locked Layer 3 acceptance gates can be exercised end-to-end before any
real testnet credentials exist. Covers:

- input-shape errors (missing/empty dirs) → exit 3
- duration shortfall → exit 4 (default) / exit 0 (--skip-min-days)
- signal divergence → exit 2
- threshold violation → exit 1
- happy-path PASS with both gates met → exit 0
- explicit --testnet-start override

The wrapper builds cmd/journal_diff to a temp binary on each invocation
(go run collapses inner exit codes to 1, breaking the 0/1/2/3 taxonomy
the wrapper depends on). Tests are slower than the trajectory tests as
a result (~3-4s total) but exercise the real binary, not a stub.

Run:
  python3 scripts/test_layer3_verdict.py
"""
from __future__ import annotations

import datetime as dt
import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
SCRIPT = REPO / "scripts" / "layer3_verdict.sh"


def write_pair(jdir: Path, symbol: str, open_ts: dt.datetime,
               pnl: float, *,
               outcome: str = "TARGET",
               entry: float = 100.0, exit_price: float = 106.0,
               side: str = "LONG", reason: str = "test") -> None:
    """Append a paired open + close to <jdir>/<symbol>-<YYYY-MM>.jsonl.

    Identical key (symbol, open_ts, side) on both sides of a layer3 diff
    means the pair MATCHES — pnl_usd diff drives the verdict.
    """
    jdir.mkdir(parents=True, exist_ok=True)
    open_str = open_ts.strftime("%Y-%m-%dT%H:%M:%SZ")
    close_str = (open_ts + dt.timedelta(minutes=30)).strftime("%Y-%m-%dT%H:%M:%SZ")
    path = jdir / f"{symbol}-{open_ts.strftime('%Y-%m')}.jsonl"
    with path.open("a") as f:
        f.write(json.dumps({
            "event": "open", "symbol": symbol, "ts": open_str,
            "side": side, "entry": entry, "stop": entry - 1,
            "target": exit_price, "reason": reason,
        }) + "\n")
        f.write(json.dumps({
            "event": "close", "symbol": symbol, "ts": close_str,
            "side": side, "entry": entry, "exit": exit_price,
            "stop": entry - 1, "target": exit_price,
            "outcome": outcome, "pnl_usd": pnl, "reason": reason,
        }) + "\n")


def run_wrapper(*args: str, env_extra: dict | None = None) -> tuple[int, str, str]:
    cmd = ["bash", str(SCRIPT), *args]
    env = os.environ.copy()
    if env_extra:
        env.update(env_extra)
    result = subprocess.run(cmd, capture_output=True, text=True, env=env)
    return result.returncode, result.stdout, result.stderr


class InputErrorTest(unittest.TestCase):

    def test_both_dirs_missing_exits_3(self):
        # Default paths point at /var/log/paper-live/* which don't exist
        # in test env — verify graceful input-error exit.
        code, out, err = run_wrapper(
            "--stub-dir", "/tmp/no-such-stub-xyz",
            "--testnet-dir", "/tmp/no-such-tn-xyz",
        )
        self.assertEqual(code, 3,
            f"missing dirs must exit 3 (input error), got {code}\n{err}")
        self.assertIn("not a directory", err)
        self.assertIn("false PASS", err)

    def test_empty_dir_exits_3(self):
        # Dirs exist but no .jsonl files — Layer 3 requires both engines
        # to have written data, otherwise zero trades on either side
        # would silently look like "no violations = PASS."
        with tempfile.TemporaryDirectory() as tmp_a, \
             tempfile.TemporaryDirectory() as tmp_b:
            code, out, err = run_wrapper(
                "--stub-dir", tmp_a, "--testnet-dir", tmp_b,
            )
            self.assertEqual(code, 3,
                f"empty dirs must exit 3, got {code}\n{err}")
            self.assertIn("no .jsonl files", err)


class DurationGateTest(unittest.TestCase):

    def test_short_window_exits_4(self):
        # Both dirs have valid trades but the testnet window is <7d.
        with tempfile.TemporaryDirectory() as tmp_a, \
             tempfile.TemporaryDirectory() as tmp_b:
            stub, tn = Path(tmp_a), Path(tmp_b)
            recent = dt.datetime.now(dt.timezone.utc).replace(tzinfo=None) - dt.timedelta(days=2)
            write_pair(stub, "BTCUSDT", recent, +5000.0)
            write_pair(tn,   "BTCUSDT", recent, +5000.0)
            code, out, err = run_wrapper(
                "--stub-dir", str(stub), "--testnet-dir", str(tn),
            )
            self.assertEqual(code, 4,
                f"<7d testnet must exit 4 (insufficient duration), "
                f"got {code}\nstdout:\n{out}\nstderr:\n{err}")
            self.assertIn("INSUFFICIENT_DURATION", out)
            self.assertIn("NOT YET EVALUABLE", out)

    def test_skip_min_days_overrides_duration(self):
        # Operator dry-run before 7d elapsed — gate becomes advisory.
        with tempfile.TemporaryDirectory() as tmp_a, \
             tempfile.TemporaryDirectory() as tmp_b:
            stub, tn = Path(tmp_a), Path(tmp_b)
            recent = dt.datetime.now(dt.timezone.utc).replace(tzinfo=None) - dt.timedelta(hours=4)
            write_pair(stub, "ETHUSDT", recent, +1000.0)
            write_pair(tn,   "ETHUSDT", recent, +1000.0)
            code, out, err = run_wrapper(
                "--stub-dir", str(stub), "--testnet-dir", str(tn),
                "--skip-min-days",
            )
            self.assertEqual(code, 0,
                f"--skip-min-days must allow PASS, got {code}\n{err}\n{out}")
            self.assertIn("DRY-RUN", out)

    def test_explicit_testnet_start_overrides_journal_inspection(self):
        # When operator passes --testnet-start, the script trusts it
        # without inspecting journal events. Verify by passing an old
        # date that wouldn't match what's in the journal.
        with tempfile.TemporaryDirectory() as tmp_a, \
             tempfile.TemporaryDirectory() as tmp_b:
            stub, tn = Path(tmp_a), Path(tmp_b)
            recent = dt.datetime.now(dt.timezone.utc).replace(tzinfo=None) - dt.timedelta(hours=4)
            write_pair(stub, "BTCUSDT", recent, +5000.0)
            write_pair(tn,   "BTCUSDT", recent, +5000.0)
            old = (dt.datetime.now(dt.timezone.utc).replace(tzinfo=None) - dt.timedelta(days=15)
                   ).strftime("%Y-%m-%dT%H:%M:%SZ")
            code, out, err = run_wrapper(
                "--stub-dir", str(stub), "--testnet-dir", str(tn),
                "--testnet-start", old,
            )
            # Journal-derived start would be 4h ago = exit 4. Operator-
            # supplied 15d ago = exit 0 (PASS). The override worked.
            self.assertEqual(code, 0,
                f"explicit --testnet-start 15d ago must PASS, got "
                f"{code}\n{err}\n{out}")
            self.assertIn("operator-supplied", out)


class ParityGateTest(unittest.TestCase):

    def test_signal_divergence_exits_2(self):
        # Stub has a trade testnet doesn't (or vice versa) → exit 2.
        with tempfile.TemporaryDirectory() as tmp_a, \
             tempfile.TemporaryDirectory() as tmp_b:
            stub, tn = Path(tmp_a), Path(tmp_b)
            base = dt.datetime.now(dt.timezone.utc).replace(tzinfo=None) - dt.timedelta(days=10)
            write_pair(stub, "BTCUSDT", base, +5000.0)
            write_pair(stub, "ETHUSDT", base + dt.timedelta(hours=1), +1000.0)
            # Testnet missing the ETH trade.
            write_pair(tn,   "BTCUSDT", base, +5000.0)
            code, out, err = run_wrapper(
                "--stub-dir", str(stub), "--testnet-dir", str(tn),
            )
            self.assertEqual(code, 2,
                f"signal divergence must exit 2, got {code}\n{out}")
            self.assertIn("SIGNAL_DIVERGENCE", out)
            self.assertIn("MUST NOT promote", out)

    def test_threshold_violation_exits_1(self):
        # Same trades on both sides, but pnl differs by >0.5%.
        with tempfile.TemporaryDirectory() as tmp_a, \
             tempfile.TemporaryDirectory() as tmp_b:
            stub, tn = Path(tmp_a), Path(tmp_b)
            base = dt.datetime.now(dt.timezone.utc).replace(tzinfo=None) - dt.timedelta(days=10)
            write_pair(stub, "BTCUSDT", base, +5000.0)
            # 2% diff = above threshold
            write_pair(tn,   "BTCUSDT", base, +5100.0)
            code, out, err = run_wrapper(
                "--stub-dir", str(stub), "--testnet-dir", str(tn),
            )
            self.assertEqual(code, 1,
                f"threshold violation must exit 1, got {code}\n{out}")
            self.assertIn("THRESHOLD", out)
            self.assertIn("MUST NOT promote", out)

    def test_happy_path_pass_exit_0(self):
        # 10 days elapsed, pnl identical, same trades both sides → PASS.
        with tempfile.TemporaryDirectory() as tmp_a, \
             tempfile.TemporaryDirectory() as tmp_b:
            stub, tn = Path(tmp_a), Path(tmp_b)
            base = dt.datetime.now(dt.timezone.utc).replace(tzinfo=None) - dt.timedelta(days=10)
            for i, sym in enumerate(["BTCUSDT", "ETHUSDT", "SOLUSDT"]):
                ts = base + dt.timedelta(hours=i * 5)
                write_pair(stub, sym, ts, +1000.0 * (i + 1))
                write_pair(tn,   sym, ts, +1000.0 * (i + 1))
            code, out, err = run_wrapper(
                "--stub-dir", str(stub), "--testnet-dir", str(tn),
            )
            self.assertEqual(code, 0,
                f"happy path must PASS (exit 0), got {code}\n{out}")
            self.assertIn("PASS — Layer 3 criterion met", out)
            self.assertIn("STAGE_1 promotion", out)

    def test_within_threshold_diff_passes(self):
        # 0.4% diff < 0.5% locked threshold → PASS (parity gate).
        with tempfile.TemporaryDirectory() as tmp_a, \
             tempfile.TemporaryDirectory() as tmp_b:
            stub, tn = Path(tmp_a), Path(tmp_b)
            base = dt.datetime.now(dt.timezone.utc).replace(tzinfo=None) - dt.timedelta(days=10)
            write_pair(stub, "BTCUSDT", base, +5000.0)
            # 0.4% diff
            write_pair(tn,   "BTCUSDT", base, +4980.0)
            code, out, err = run_wrapper(
                "--stub-dir", str(stub), "--testnet-dir", str(tn),
            )
            self.assertEqual(code, 0,
                f"within-threshold diff must PASS, got {code}\n{out}")


if __name__ == "__main__":
    unittest.main(verbosity=2)
