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
            trades = rc.load_journals(d)[0]
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
            trades = rc.load_journals(d)[0]
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
            trades = rc.load_journals(d)[0]
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
            trades = rc.load_journals(d)[0]
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
            trades = rc.load_journals(d)[0]
            cohorts = {t.cohort for t in trades}
            self.assertEqual(cohorts, {"live", "shadow/bb20"})

    def test_empty_journals_yields_empty(self):
        with tempfile.TemporaryDirectory() as tmp:
            trades, stats = rc.load_journals(Path(tmp))
            self.assertEqual(trades, [])
            self.assertEqual(stats.files_scanned, 0)
            self.assertEqual(stats.total_closes, 0)

    def test_stats_distinguishes_pre_decomp_from_no_closes(self):
        # All-pre-decomp journal: closes exist but none qualify. Stats
        # must reflect the operator-actionable state ("restart engine to
        # enable cost columns") rather than collapsing to look the same
        # as "no closes yet".
        with tempfile.TemporaryDirectory() as tmp:
            d = Path(tmp)
            write_journal(d, "BTCUSDT", "2026-05", [
                {"outcome": "STOP", "pnl_usd": -1000, "fee_usd": 0,
                 "slip_usd": 0, "notional_usd": 0},
                {"outcome": "TARGET", "pnl_usd": 6000, "fee_usd": 0,
                 "slip_usd": 0, "notional_usd": 0},
            ])
            trades, stats = rc.load_journals(d)
            self.assertEqual(trades, [])
            self.assertEqual(stats.files_scanned, 1)
            self.assertEqual(stats.total_closes, 2)
            self.assertEqual(stats.pre_decomp_skipped, 2)
            self.assertEqual(stats.qualifying, 0)

    def test_cli_warns_when_all_pre_decomp(self):
        # CLI prints the pre-decomp warning to stderr instead of letting
        # the operator confuse "stale format" with "no closes yet".
        with tempfile.TemporaryDirectory() as tmp:
            d = Path(tmp)
            write_journal(d, "BTCUSDT", "2026-05", [
                {"outcome": "STOP", "pnl_usd": -1000, "fee_usd": 0,
                 "slip_usd": 0, "notional_usd": 0},
            ])
            code, out = run_cli(
                ["--live-source", "local", "--live-dir", str(d)])
            self.assertEqual(code, 2,
                f"expected exit 2 (no qualifying), got {code}")
            self.assertIn("close event(s) found", out)
            self.assertIn("pre-decomp", out)
            self.assertIn("Restart engines", out)

    def test_handles_null_numeric_fields_without_crash(self):
        # Journal entries with `"fee_usd": null` (or other non-numeric
        # numeric fields) used to TypeError out of _parse_one and crash
        # the script. The dual-sense audit pattern: a Python exit-1 from
        # a crash routes to the wrong cron tier in any wrapper that maps
        # non-zero → critical. _num() coerces defensively to 0.0.
        with tempfile.TemporaryDirectory() as tmp:
            d = Path(tmp)
            # notional_usd>0 is required to pass the qualifier gate; the
            # other numeric fields are explicitly null.
            (d / "BTCUSDT-2026-05.jsonl").write_text(
                json.dumps({"event": "open", "symbol": "BTCUSDT",
                            "ts": "2026-05-08T12:00:00Z", "side": "LONG",
                            "entry": 60000, "stop": 59000, "target": 66000})
                + "\n"
                + json.dumps({"event": "close", "symbol": "BTCUSDT",
                              "ts": "2026-05-08T12:30:00Z", "side": "LONG",
                              "entry": 60000, "exit": 59000,
                              "outcome": "STOP", "pnl_usd": None,
                              "fee_usd": None, "slip_usd": None,
                              "notional_usd": 100000})
                + "\n"
            )
            trades, stats = rc.load_journals(d)
            self.assertEqual(len(trades), 1)
            self.assertEqual(trades[0].fee_usd, 0.0)
            self.assertEqual(trades[0].slip_usd, 0.0)
            self.assertEqual(trades[0].pnl_usd, 0.0)
            # Render must complete without raising.
            out = rc.render(trades, None)
            self.assertIn("BTCUSDT", out)

    def test_handles_string_encoded_numerics(self):
        # Defensive: string-encoded numerics (e.g. "100" instead of 100)
        # also coerce cleanly.
        with tempfile.TemporaryDirectory() as tmp:
            d = Path(tmp)
            (d / "BTCUSDT-2026-05.jsonl").write_text(
                json.dumps({"event": "open", "symbol": "BTCUSDT",
                            "ts": "2026-05-08T12:00:00Z", "side": "LONG",
                            "entry": 60000, "stop": 59000, "target": 66000})
                + "\n"
                + json.dumps({"event": "close", "symbol": "BTCUSDT",
                              "ts": "2026-05-08T12:30:00Z",
                              "outcome": "STOP", "pnl_usd": "-1000",
                              "fee_usd": "100", "slip_usd": "50",
                              "notional_usd": "100000"})
                + "\n"
            )
            trades, stats = rc.load_journals(d)
            self.assertEqual(len(trades), 1)
            self.assertEqual(trades[0].fee_usd, 100.0)
            self.assertAlmostEqual(trades[0].fee_bps, 10.0, places=4)

    def test_render_flags_kill_threshold_breach(self):
        # 13bp fee > 12bp kill → ★fee>kill flag.
        with tempfile.TemporaryDirectory() as tmp:
            d = Path(tmp)
            write_journal(d, "BTCUSDT", "2026-05", [
                {"outcome": "STOP", "pnl_usd": -1000, "fee_usd": 130,
                 "slip_usd": 50, "notional_usd": 100000},
            ])
            trades = rc.load_journals(d)[0]
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
            out = rc.render(rc.load_journals(d)[0], None)
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
            out = rc.render(rc.load_journals(d)[0], cohort_filter="live")
            self.assertIn("BTC", out)
            self.assertNotIn("ETH", out)

    def test_render_slip_pending_when_no_losers(self):
        # All-winners scenario — slip cannot be evaluated, so the gate
        # must read PENDING (not PASS via 0.0 ≤ 25.0). This was the same
        # "no data = clean" fail-open shape that mis-calibrated the
        # threshold kill mechanism in 2026-05-07; same audit lens here.
        with tempfile.TemporaryDirectory() as tmp:
            d = Path(tmp)
            write_journal(d, "BTCUSDT", "2026-05", [
                {"outcome": "TARGET", "pnl_usd": 5000, "fee_usd": 100,
                 "slip_usd": 0, "notional_usd": 100000},
                {"outcome": "TARGET", "pnl_usd": 4000, "fee_usd": 80,
                 "slip_usd": 0, "notional_usd": 80000},
            ])
            out = rc.render(rc.load_journals(d)[0], None)
            self.assertIn("[PENDING]", out,
                "n_losers=0 must read PENDING, not PASS via 0.0≤25.0")
            self.assertIn("n_losers=0", out)
            self.assertIn("gate unevaluated", out)
            # Fee gate is still evaluable (n=2 trades) → must show PASS.
            self.assertIn("[PASS]", out)

    def test_render_summary_passes_when_within_thresholds(self):
        with tempfile.TemporaryDirectory() as tmp:
            d = Path(tmp)
            write_journal(d, "BTCUSDT", "2026-05", [
                {"outcome": "STOP", "pnl_usd": -1000, "fee_usd": 100,
                 "slip_usd": 50, "notional_usd": 100000},
            ])
            out = rc.render(rc.load_journals(d)[0], None)
            self.assertIn("[PASS]", out)
            self.assertNotIn("[FAIL]", out)


class RealizedCostCLITest(unittest.TestCase):

    def test_empty_local_dir_exits_3(self):
        # Empty journal dir = zero .jsonl files = operator misconfiguration.
        # This used to exit 2 (collapsed with legit "no closes yet"); the
        # audit-pattern fix surfaces it as exit 3 input-error so cron
        # wrappers can route Telegram tier separately from "early-stage".
        with tempfile.TemporaryDirectory() as tmp:
            code, out = run_cli(["--live-source", "local", "--live-dir", tmp])
            self.assertEqual(code, 3,
                f"expected exit 3 (empty dir = operator error), got {code}\n{out}")
            self.assertIn("no .jsonl files", out)
            self.assertIn("misconfiguration", out)

    def test_dir_with_files_but_no_closes_exits_2(self):
        # Files exist but contain only open events — legitimate fresh-
        # deploy "no closes yet" state. Must distinguish from empty-dir
        # operator-error (exit 3) above.
        with tempfile.TemporaryDirectory() as tmp:
            d = Path(tmp)
            (d / "BTCUSDT-2026-05.jsonl").write_text(
                json.dumps({"event": "open", "symbol": "BTCUSDT",
                            "ts": "2026-05-08T12:00:00Z", "side": "LONG",
                            "entry": 60000, "stop": 59000, "target": 66000})
                + "\n"
            )
            code, out = run_cli(["--live-source", "local", "--live-dir", str(d)])
            self.assertEqual(code, 2,
                f"expected exit 2 (no closes yet, legit), got {code}\n{out}")
            self.assertNotIn("misconfiguration", out)

    def test_missing_local_dir_exits_3(self):
        code, out = run_cli(
            ["--live-source", "local", "--live-dir", "/nonexistent/path"])
        self.assertEqual(code, 3,
            f"expected exit 3 (input error), got {code}\n{out}")

    def test_cohort_filter_no_match_exits_2(self):
        # Operator typo'd --cohort to a non-existent label. Previously the
        # main exit-code check ran against the UNFILTERED trades list, so
        # the script printed "(no qualifying trades)" but exited 0 = PASS.
        # The fix moves filtering to main so exit 2 reflects the filtered
        # state.
        with tempfile.TemporaryDirectory() as tmp:
            d = Path(tmp)
            write_journal(d, "BTCUSDT", "2026-05", [
                {"outcome": "STOP", "pnl_usd": -1000, "fee_usd": 100,
                 "slip_usd": 50, "notional_usd": 100000},
            ])
            code, out = run_cli([
                "--live-source", "local", "--live-dir", str(d),
                "--cohort", "shadow/typoed-name",
            ])
            self.assertEqual(code, 2,
                f"expected exit 2 (filter excluded all trades), got {code}\n{out}")
            self.assertIn("matched 0", out)
            self.assertIn("available cohorts", out)

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
