#!/usr/bin/env python3
"""Functional tests for stage_promotion_check.py.

Synthetic journal fixtures exercise each verdict path:
  exit 0 PROMOTE — all gates pass
  exit 1 BLOCKED — one gate fails outright
  exit 2 WAITING — insufficient data
  exit 3 ERROR  — input error

Each criterion gets its own pinpoint test so a future refactor that
breaks a single criterion doesn't get masked by the verdict aggregation.
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
SCRIPT = REPO / "scripts" / "stage_promotion_check.py"

sys.path.insert(0, str(REPO / "scripts"))
import stage_promotion_check as sp  # noqa: E402


def write_trades(jdir: Path, n: int, start_days_ago: int = 70,
                 win_rate: float = 0.21, win_pnl: float = 6000.0,
                 loss_pnl: float = -1000.0, fee_usd: float = 100.0,
                 slip_usd: float = 50.0, notional_usd: float = 100000.0,
                 single_sym: str | None = None) -> None:
    """Generate n closes spread evenly across [now-start_days_ago, now].

    single_sym: if set, all trades on that symbol; else round-robin three
    symbols (proves no single-symbol concentration in default cases).
    """
    jdir.mkdir(parents=True, exist_ok=True)
    syms = [single_sym] if single_sym else ["BTCUSDT", "ETHUSDT", "XLMUSDT"]
    start = datetime.now(timezone.utc) - timedelta(days=start_days_ago)
    interval = timedelta(days=start_days_ago) / max(1, n)
    n_wins = round(n * win_rate)

    by_month: dict[str, list[str]] = {}
    for i in range(n):
        ts = start + interval * i
        is_win = i < n_wins
        sym = syms[i % len(syms)]
        ts_str = ts.strftime("%Y-%m-%dT%H:%M:%SZ")
        ts_close_str = (ts + timedelta(minutes=30)).strftime("%Y-%m-%dT%H:%M:%SZ")
        month = ts.strftime("%Y-%m")
        key = f"{sym}-{month}"
        lines = by_month.setdefault(key, [])
        lines.append(json.dumps({
            "event": "open", "symbol": sym, "ts": ts_str,
            "side": "LONG", "entry": 100, "stop": 99, "target": 106,
        }))
        lines.append(json.dumps({
            "event": "close", "symbol": sym, "ts": ts_close_str,
            "side": "LONG", "entry": 100, "exit": 99, "stop": 99, "target": 106,
            "outcome": "TARGET" if is_win else "STOP",
            "pnl_usd": win_pnl if is_win else loss_pnl,
            "fee_usd": fee_usd,
            "slip_usd": 0 if is_win else slip_usd,
            "notional_usd": notional_usd,
        }))
    for key, lines in by_month.items():
        path = jdir / f"{key}.jsonl"
        path.write_text("\n".join(lines) + "\n")


def write_drift_history(path: Path, runs: list[tuple[str, str]]) -> None:
    """runs: list of (iso_ts, verdict). verdict in CLEAN/DRIFT_FIRED/INSUFFICIENT_DATA."""
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w") as f:
        for ts, verdict in runs:
            f.write(json.dumps({
                "ts": ts, "verdict": verdict,
                "exit_code": 0 if verdict == "CLEAN" else (1 if verdict == "DRIFT_FIRED" else 2),
            }) + "\n")


def run_cli(args: list[str], cwd: Path | None = None) -> tuple[int, str]:
    result = subprocess.run(
        ["python3", str(SCRIPT)] + args,
        capture_output=True, text=True, cwd=str(cwd or REPO),
    )
    return result.returncode, result.stdout + result.stderr


class CriterionUnitTest(unittest.TestCase):
    """Unit tests for individual criterion evaluators — fastest path
    to per-criterion correctness without subprocess overhead."""

    def test_check_trades_passes_at_threshold(self):
        trades = [sp.Trade("2026-05-01T00:00:00Z", "BTC", "STOP", -1, 0, 0, 0)] * 150
        c = sp.check_trades(trades)
        self.assertEqual(c.status, "PASS")

    def test_check_trades_pending_below_threshold(self):
        trades = [sp.Trade("2026-05-01T00:00:00Z", "BTC", "STOP", -1, 0, 0, 0)] * 149
        c = sp.check_trades(trades)
        self.assertEqual(c.status, "PENDING")

    def test_check_days_uses_first_trade(self):
        old_ts = (datetime.now(timezone.utc) - timedelta(days=65)).strftime("%Y-%m-%dT%H:%M:%SZ")
        trades = [sp.Trade(old_ts, "BTC", "STOP", -1, 0, 0, 0)]
        c = sp.check_days(trades)
        self.assertEqual(c.status, "PASS")

    def test_check_net_positive(self):
        trades = [sp.Trade("2026-05-01T00:00:00Z", "BTC", "TARGET", 100, 0, 0, 0)] * 150
        c = sp.check_net_positive(trades)
        self.assertEqual(c.status, "PASS")
        # Net-negative at full sample → FAIL.
        trades = [sp.Trade("2026-05-01T00:00:00Z", "BTC", "STOP", -100, 0, 0, 0)] * 150
        c = sp.check_net_positive(trades)
        self.assertEqual(c.status, "FAIL")

    def test_check_fee_bps(self):
        # 10bp on $100k notional = $100 fee.
        trades = [sp.Trade("2026-05-01T00:00:00Z", "BTC", "STOP", -1, 100, 50, 100000)] * 10
        c = sp.check_fee_bps(trades)
        self.assertEqual(c.status, "PASS")
        self.assertIn("10.00", c.actual)

    def test_check_fee_bps_fails_above_threshold(self):
        # 15bp fee > 12bp threshold → FAIL.
        trades = [sp.Trade("2026-05-01T00:00:00Z", "BTC", "STOP", -1, 150, 50, 100000)] * 10
        c = sp.check_fee_bps(trades)
        self.assertEqual(c.status, "FAIL")

    def test_check_slip_only_on_losers(self):
        # 10 losers at 5bp slip + 10 winners (slip excluded).
        losers = [sp.Trade("2026-05-01T00:00:00Z", "BTC", "STOP", -1, 100, 50, 100000)] * 10
        winners = [sp.Trade("2026-05-01T00:00:00Z", "BTC", "TARGET", 1, 100, 0, 100000)] * 10
        c = sp.check_slip_bps(losers + winners)
        self.assertEqual(c.status, "PASS")
        self.assertIn("5.00", c.actual)

    def test_check_single_symbol_concentration(self):
        # All trades on one symbol → 100% concentration → FAIL (at threshold).
        trades = [sp.Trade("2026-05-01T00:00:00Z", "BTC", "STOP", -100, 0, 0, 0)] * 150
        c = sp.check_single_symbol_concentration(trades)
        self.assertEqual(c.status, "FAIL")

    def test_check_drift_clean_never_fired(self):
        with tempfile.TemporaryDirectory() as tmp:
            history = Path(tmp) / "h.jsonl"
            now = datetime.now(timezone.utc)
            write_drift_history(history, [
                ((now - timedelta(days=14)).strftime("%Y-%m-%dT%H:%M:%SZ"), "CLEAN"),
                ((now - timedelta(days=7)).strftime("%Y-%m-%dT%H:%M:%SZ"), "CLEAN"),
            ])
            c = sp.check_drift_clean(history)
            self.assertEqual(c.status, "PASS")

    def test_check_drift_recent_fire_fails(self):
        with tempfile.TemporaryDirectory() as tmp:
            history = Path(tmp) / "h.jsonl"
            now = datetime.now(timezone.utc)
            write_drift_history(history, [
                ((now - timedelta(days=10)).strftime("%Y-%m-%dT%H:%M:%SZ"), "DRIFT_FIRED"),
            ])
            c = sp.check_drift_clean(history)
            self.assertEqual(c.status, "FAIL")

    def test_check_drift_old_fire_passes(self):
        # Fire >30d ago + clean since = PASS (clean window of 30d satisfied).
        with tempfile.TemporaryDirectory() as tmp:
            history = Path(tmp) / "h.jsonl"
            now = datetime.now(timezone.utc)
            write_drift_history(history, [
                ((now - timedelta(days=60)).strftime("%Y-%m-%dT%H:%M:%SZ"), "DRIFT_FIRED"),
                ((now - timedelta(days=20)).strftime("%Y-%m-%dT%H:%M:%SZ"), "CLEAN"),
            ])
            c = sp.check_drift_clean(history)
            self.assertEqual(c.status, "PASS")


def write_btc_stub(path: Path, fields: list[str]) -> None:
    """Write a tiny Python script that ignores stdin and emits the given
    7 TSV fields. Used to stub btc_hodl_benchmark.py in tests so they
    don't hit Binance. fields: [cum_strat, cum_hodl, cum_delta, n_w,
    n_underperf, kill_t, warning]."""
    script = (
        "#!/usr/bin/env python3\n"
        "import sys\n"
        f"sys.stdin.read()\n"
        f"print('\\t'.join({fields!r}))\n"
    )
    path.write_text(script)
    path.chmod(0o755)


class BTCHODLCriterionTest(unittest.TestCase):
    """Tests for the v2 mechanical BTC-HODL check. Each test stubs
    btc_hodl_benchmark.py via sp.BTC_HELPER override so no Binance
    network calls happen in CI."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmpdir = Path(self.tmp.name)
        self.stub = self.tmpdir / "btc_stub.py"
        self.original_helper = sp.BTC_HELPER

    def tearDown(self):
        sp.BTC_HELPER = self.original_helper
        self.tmp.cleanup()

    def _stub(self, fields: list[str]) -> None:
        write_btc_stub(self.stub, fields)
        sp.BTC_HELPER = self.stub

    def test_no_trades_pending(self):
        c = sp.check_btc_hodl([])
        self.assertEqual(c.status, "PENDING")

    def test_strategy_beats_hodl_below_n_floor_pending(self):
        # delta>0 but n<MIN_TRADES → PENDING (mirror the n-floor pattern
        # used elsewhere; pre-promotion we don't grant a PASS at low n).
        self._stub(["1000", "500", "500", "5", "0", "False", ""])
        trades = [sp.Trade("2026-05-01T00:00:00Z", "BTC", "TARGET",
                          1000, 100, 0, 100000)] * 10
        c = sp.check_btc_hodl(trades)
        self.assertEqual(c.status, "PENDING")
        self.assertIn("Δ $+500", c.actual)

    def test_strategy_beats_hodl_at_n_floor_passes(self):
        self._stub(["50000", "10000", "40000", "30", "0", "False", ""])
        trades = [sp.Trade("2026-05-01T00:00:00Z", "BTC", "TARGET",
                          333, 100, 0, 100000)] * 150
        c = sp.check_btc_hodl(trades)
        self.assertEqual(c.status, "PASS")

    def test_strategy_underperforms_hodl_at_n_floor_fails(self):
        self._stub(["1000", "5000", "-4000", "30", "1", "False", ""])
        trades = [sp.Trade("2026-05-01T00:00:00Z", "BTC", "STOP",
                          7, 100, 50, 100000)] * 150
        c = sp.check_btc_hodl(trades)
        self.assertEqual(c.status, "FAIL")
        self.assertIn("Δ $-4,000", c.actual)

    def test_helper_warning_yields_deferred(self):
        # Helper sets warning="no-btc-price" when Binance API is down.
        # Should not produce a spurious FAIL — defer to operator.
        self._stub(["0", "0", "0", "0", "0", "False", "no-btc-price"])
        trades = [sp.Trade("2026-05-01T00:00:00Z", "BTC", "STOP",
                          -1000, 100, 50, 100000)] * 150
        c = sp.check_btc_hodl(trades)
        self.assertEqual(c.status, "DEFERRED")
        self.assertIn("no-btc-price", c.actual)

    def test_helper_missing_yields_deferred(self):
        sp.BTC_HELPER = self.tmpdir / "nonexistent_helper.py"
        trades = [sp.Trade("2026-05-01T00:00:00Z", "BTC", "STOP",
                          -1000, 100, 50, 100000)] * 150
        c = sp.check_btc_hodl(trades)
        self.assertEqual(c.status, "DEFERRED")
        self.assertIn("not found", c.actual)

    def test_helper_returns_unexpected_output_yields_deferred(self):
        # Stub emits only 3 fields instead of 7.
        self._stub(["100", "50", "50"])
        trades = [sp.Trade("2026-05-01T00:00:00Z", "BTC", "STOP",
                          -1000, 100, 50, 100000)] * 150
        c = sp.check_btc_hodl(trades)
        self.assertEqual(c.status, "DEFERRED")
        self.assertIn("unexpected", c.actual)


class VerdictIntegrationTest(unittest.TestCase):
    """End-to-end exit-code tests against synthetic journal directories."""

    def test_promote_path_all_gates_pass(self):
        with tempfile.TemporaryDirectory() as tmp:
            jdir = Path(tmp) / "journal"
            history = Path(tmp) / "history.jsonl"
            stub = Path(tmp) / "btc_stub.py"
            write_btc_stub(stub, ["50000", "10000", "40000", "5", "0", "False", ""])
            write_trades(jdir, n=160, start_days_ago=65,
                         win_rate=0.21, win_pnl=2500, loss_pnl=-300,
                         fee_usd=100, slip_usd=50, notional_usd=100000)
            now = datetime.now(timezone.utc)
            write_drift_history(history, [
                ((now - timedelta(days=21)).strftime("%Y-%m-%dT%H:%M:%SZ"), "CLEAN"),
                ((now - timedelta(days=14)).strftime("%Y-%m-%dT%H:%M:%SZ"), "CLEAN"),
                ((now - timedelta(days=7)).strftime("%Y-%m-%dT%H:%M:%SZ"), "CLEAN"),
            ])
            # Pass btc-hodl-helper override via sp.BTC_HELPER monkey-patch
            # in a child shell. Cleanest: env-var override on the script.
            # Use a wrapper that injects sys.path then monkey-patches.
            wrapper = Path(tmp) / "wrapper.py"
            wrapper.write_text(
                f"import sys\n"
                f"sys.path.insert(0, {str(REPO / 'scripts')!r})\n"
                f"import stage_promotion_check as sp\n"
                f"sp.BTC_HELPER = {str(stub)!r}\n"
                f"sys.argv = ['stage_promotion_check.py',\n"
                f"            '--live-source', 'local',\n"
                f"            '--live-dir', {str(jdir)!r},\n"
                f"            '--drift-history', {str(history)!r}]\n"
                f"sys.exit(sp.main())\n"
            )
            result = subprocess.run(
                ["python3", str(wrapper)],
                capture_output=True, text=True, cwd=str(REPO),
            )
            code = result.returncode
            out = result.stdout + result.stderr
            self.assertEqual(code, 0,
                f"expected PROMOTE (exit 0), got {code}\n{out}")
            self.assertIn("PROMOTE", out)

    def test_blocked_path_one_gate_fails(self):
        with tempfile.TemporaryDirectory() as tmp:
            jdir = Path(tmp) / "journal"
            history = Path(tmp) / "history.jsonl"
            # 160 trades, 65 days, but FEE BPS is 15bp (over 12bp limit).
            write_trades(jdir, n=160, start_days_ago=65, fee_usd=150)
            now = datetime.now(timezone.utc)
            write_drift_history(history, [
                ((now - timedelta(days=21)).strftime("%Y-%m-%dT%H:%M:%SZ"), "CLEAN"),
            ])
            code, out = run_cli([
                "--live-source", "local",
                "--live-dir", str(jdir),
                "--drift-history", str(history),
            ])
            self.assertEqual(code, 1,
                f"expected BLOCKED (exit 1), got {code}\n{out}")
            self.assertIn("BLOCKED", out)

    def test_waiting_path_insufficient_data(self):
        with tempfile.TemporaryDirectory() as tmp:
            jdir = Path(tmp) / "journal"
            history = Path(tmp) / "history.jsonl"
            # Only 10 trades → trades-PENDING → WAITING.
            write_trades(jdir, n=10, start_days_ago=5)
            code, out = run_cli([
                "--live-source", "local",
                "--live-dir", str(jdir),
                "--drift-history", str(history),
            ])
            self.assertEqual(code, 2,
                f"expected WAITING (exit 2), got {code}\n{out}")
            self.assertIn("WAITING", out)

    def test_error_missing_local_dir(self):
        code, out = run_cli([
            "--live-source", "local",
            "--live-dir", "/nonexistent/path",
        ])
        self.assertEqual(code, 3,
            f"expected ERROR (exit 3) on missing dir, got {code}\n{out}")


if __name__ == "__main__":
    unittest.main(verbosity=2)
