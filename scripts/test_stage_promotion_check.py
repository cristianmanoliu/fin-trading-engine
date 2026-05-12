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

    def test_check_days_name_surfaces_first_trade_anchor(self):
        """TIME-ANCHOR AMBIGUITY pin: pre-reg
        real_money_protocol_decision_rule_2026-05-08.md line 134 anchors
        elapsed-time on "forward-paper start" (deploy date); the LIMBO
        rule (forward_paper_outcome_resolution_decision_rule_2026-05-10.md
        line 47) anchors on "first close"; this implementation anchors on
        FIRST TRADE timestamp. The discrepancy is small in current state
        but compounds in a quiet-signal regime. Lock the implementation
        choice into the criterion name so any operator reading the report
        sees explicitly which anchor is in force. A future change to use
        deploy-start as the anchor MUST update this test in lockstep so
        doc-vs-code drift cannot recur silently."""
        old_ts = (datetime.now(timezone.utc) - timedelta(days=65)).strftime("%Y-%m-%dT%H:%M:%SZ")
        c = sp.check_days([sp.Trade(old_ts, "BTC", "STOP", -1, 0, 0, 0)])
        self.assertIn("since first trade", c.name,
            f"check_days name must surface the first-trade anchor explicitly; "
            f"got: {c.name!r}")

    def test_check_pro_rated_annual_name_surfaces_first_trade_anchor(self):
        """Sibling pin to test_check_days_name_surfaces_first_trade_anchor.
        check_pro_rated_annual shares the same anchor and the same pre-reg
        ambiguity. Pin the explicit anchor in the criterion name."""
        old_ts = (datetime.now(timezone.utc) - timedelta(days=65)).strftime("%Y-%m-%dT%H:%M:%SZ")
        trades = [sp.Trade(old_ts, "BTC", "TARGET", 100, 0, 0, 0)] * 200
        c = sp.check_pro_rated_annual(trades)
        self.assertIn("since first trade", c.name,
            f"check_pro_rated_annual name must surface the first-trade anchor "
            f"explicitly; got: {c.name!r}")

    def test_benchmark_notional_default_matches_locked_spec(self):
        """SPEC-vs-REALITY pin (D2 amendment 2026-05-12): CLAUDE.md
        criterion 5 specifies $16k notional matching the deployed-16 fleet
        × $1k stake reality. Pre-amendment the locked text said $32k —
        carried over from the deployed-32 era without update when the
        fleet shrank 2026-05-07. See
        results/btc_hodl_notional_amendment_2026-05-12.md.

        This test pins the default value. Any future change to the
        locked notional MUST update: (1) CLAUDE.md text, (2) the constant
        + docstring in stage_promotion_check.py, (3) forward_paper_status.sh,
        (4) test_criterion_coverage.py LOCKED dict, (5) this test.
        Pre-reg-amendment-of-locked-criterion changes require a dated
        amendment file in results/ documenting the rationale."""
        self.assertEqual(sp.BENCHMARK_NOTIONAL, 16000.0,
            f"BENCHMARK_NOTIONAL default must match CLAUDE.md locked spec "
            f"($16k matching deployed-16 fleet reality, D2 amendment "
            f"2026-05-12); got {sp.BENCHMARK_NOTIONAL}. "
            f"If operator amended the locked criterion, update CLAUDE.md, "
            f"the docstring above the constant, forward_paper_status.sh, "
            f"test_criterion_coverage.py LOCKED, AND this test in lockstep.")

    def test_dashboard_slip_threshold_matches_gate_deploy_threshold(self):
        """DRIFT pin: scripts/forward_paper_status.sh's slip gate must use
        the DEPLOY threshold (≤20bp per CLAUDE.md), not the KILL threshold
        (>25bp advisory). Pre-fix the dashboard used KILL_MAX_SLIP_BP=25
        for its overall PASS verdict → operator saw PASS at 22bp while
        the formal gate would FAIL. Pin by string-scanning the dashboard
        source: the s_slip computation must reference MAX_SLIP_BPS (not
        KILL_MAX_SLIP_BP), AND MAX_SLIP_BPS must match
        stage_promotion_check.py's MAX_SLIP_BPS constant value."""
        dashboard = (REPO / "scripts" / "forward_paper_status.sh").read_text()
        self.assertIn('s_slip=$(pass_or_fail "$slip_bps_val" "$MAX_SLIP_BPS" le)',
            dashboard,
            "dashboard slip gate must evaluate against MAX_SLIP_BPS "
            "(deploy threshold), not KILL_MAX_SLIP_BP — drift to kill "
            "threshold would mask deploy-gate FAILs in the 20-25bp band.")
        # Extract the dashboard's constant value via a simple line scan.
        for ln in dashboard.splitlines():
            if ln.startswith("MAX_SLIP_BPS="):
                # e.g. "MAX_SLIP_BPS=20  # comment..."
                val = float(ln.split("=", 1)[1].split()[0])
                self.assertEqual(val, sp.MAX_SLIP_BPS,
                    f"dashboard MAX_SLIP_BPS={val} ≠ gate {sp.MAX_SLIP_BPS}; "
                    f"these must match so operator's dashboard PASS aligns "
                    f"with formal gate PASS")
                break
        else:
            self.fail("dashboard does not define MAX_SLIP_BPS — drift risk")

    def test_dashboard_fee_threshold_matches_gate_deploy_threshold(self):
        """Sibling pin for fee threshold. Same constraint, lower magnitude
        risk (current dashboard + gate both use 12bp; rename was for
        naming consistency only)."""
        dashboard = (REPO / "scripts" / "forward_paper_status.sh").read_text()
        self.assertIn('s_fee=$(pass_or_fail "$fee_bps_val" "$MAX_FEE_BPS" le)',
            dashboard,
            "dashboard fee gate must evaluate against MAX_FEE_BPS "
            "consistent with the formal gate's MAX_FEE_BPS constant")
        for ln in dashboard.splitlines():
            if ln.startswith("MAX_FEE_BPS="):
                val = float(ln.split("=", 1)[1].split()[0])
                self.assertEqual(val, sp.MAX_FEE_BPS,
                    f"dashboard MAX_FEE_BPS={val} ≠ gate {sp.MAX_FEE_BPS}")
                break
        else:
            self.fail("dashboard does not define MAX_FEE_BPS — drift risk")

    def test_benchmark_notional_env_override_propagates_to_subprocess(self):
        """The constant is loaded at module-import time from
        BENCHMARK_NOTIONAL env var. The gate evaluator now mirrors the
        operator dashboard (forward_paper_status.sh line 40) which has
        the same env override — closing the operator-visible
        inconsistency where dashboard could reconcile to current fleet
        but the formal gate could not.

        Tested via subprocess (separate Python process picks up the
        env at import time)."""
        env = {**os.environ, "BENCHMARK_NOTIONAL": "16000"}
        result = subprocess.run(
            ["python3", "-c",
             "import sys; sys.path.insert(0, 'scripts'); "
             "import stage_promotion_check; "
             "print(stage_promotion_check.BENCHMARK_NOTIONAL)"],
            cwd=str(REPO),
            env=env,
            capture_output=True, text=True,
        )
        self.assertEqual(result.returncode, 0,
            f"subprocess failed: {result.stderr}")
        self.assertEqual(result.stdout.strip(), "16000.0",
            f"BENCHMARK_NOTIONAL env override did not propagate; "
            f"got stdout={result.stdout!r}")

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
        # Fire >30d ago + recent CLEAN since = PASS (clean window of 30d
        # satisfied AND most-recent within freshness threshold).
        with tempfile.TemporaryDirectory() as tmp:
            history = Path(tmp) / "h.jsonl"
            now = datetime.now(timezone.utc)
            write_drift_history(history, [
                ((now - timedelta(days=60)).strftime("%Y-%m-%dT%H:%M:%SZ"), "DRIFT_FIRED"),
                ((now - timedelta(days=20)).strftime("%Y-%m-%dT%H:%M:%SZ"), "CLEAN"),
                ((now - timedelta(days=5)).strftime("%Y-%m-%dT%H:%M:%SZ"), "CLEAN"),
            ])
            c = sp.check_drift_clean(history)
            self.assertEqual(c.status, "PASS")

    def test_check_drift_stale_history_pending(self):
        # Most-recent run >14d ago → stale (cron may be dead) → PENDING,
        # never PASS, even when the visible history is cleanly never-fired.
        # Closes a fail-open: a long-ago "clean" history with stopped
        # cron would otherwise return PASS forever.
        with tempfile.TemporaryDirectory() as tmp:
            history = Path(tmp) / "h.jsonl"
            now = datetime.now(timezone.utc)
            write_drift_history(history, [
                ((now - timedelta(days=21)).strftime("%Y-%m-%dT%H:%M:%SZ"), "CLEAN"),
                ((now - timedelta(days=20)).strftime("%Y-%m-%dT%H:%M:%SZ"), "CLEAN"),
            ])
            c = sp.check_drift_clean(history)
            self.assertEqual(c.status, "PENDING")
            self.assertIn("stale", c.actual.lower())

    def test_check_drift_at_stage_stale_history_pending(self):
        # Same fail-open closure for the parameterized at-stage variant.
        with tempfile.TemporaryDirectory() as tmp:
            history = Path(tmp) / "h.jsonl"
            now = datetime.now(timezone.utc)
            write_drift_history(history, [
                ((now - timedelta(days=30)).strftime("%Y-%m-%dT%H:%M:%SZ"), "CLEAN"),
            ])
            c = sp.check_drift_clean_at_stage(history, 30, "test")
            self.assertEqual(c.status, "PENDING")
            self.assertIn("stale", c.actual.lower())


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


class VerdictAggregationTest(unittest.TestCase):
    """Direct tests of verdict_of() — exit-code contract is critical
    because weekly_audit.sh maps each code to a Telegram tier."""

    def _crit(self, status: str) -> sp.Criterion:
        return sp.Criterion("test", "≥0", "n/a", status)

    def test_deferred_only_returns_exit_4_not_0(self):
        """Regression: DEFERRED + all-other-PASS used to return exit 0,
        which weekly_audit.sh case 0) interprets as "PROMOTION READY"
        and fires a CRITICAL Telegram alert. A transient Binance API
        failure during BTC-HODL helper would have falsely paged the
        operator with "ready to deploy real money."

        Post-fix: DEFERRED → exit 4 (PROMOTE-CANDIDATE), which the
        weekly cron handles with WARN tier (not CRITICAL).
        """
        crits = [self._crit("PASS"), self._crit("PASS"), self._crit("DEFERRED")]
        text, code = sp.verdict_of(crits)
        self.assertEqual(code, 4,
            f"DEFERRED + all-PASS must return exit 4 (PROMOTE-CANDIDATE), "
            f"not 0 (PROMOTE which fires CRITICAL Telegram). Got {code}.")
        self.assertIn("PROMOTE-CANDIDATE", text)

    def test_all_pass_still_returns_exit_0(self):
        """Sanity: the genuine all-pass path must remain exit 0.
        Otherwise the operator would never be alerted on real promotion
        readiness."""
        crits = [self._crit("PASS")] * 9
        _, code = sp.verdict_of(crits)
        self.assertEqual(code, 0)

    def test_fail_takes_precedence_over_deferred(self):
        """A FAIL gate must produce exit 1 BLOCKED regardless of any
        DEFERRED gates — failing-gate signal cannot be muted by a
        coexisting deferred state."""
        crits = [self._crit("PASS"), self._crit("FAIL"), self._crit("DEFERRED")]
        _, code = sp.verdict_of(crits)
        self.assertEqual(code, 1)

    def test_pending_takes_precedence_over_deferred(self):
        """PENDING + DEFERRED → WAITING (2). Because PENDING means the
        evaluable answer isn't determined yet, we shouldn't grant the
        promotion-candidate verdict on insufficient data."""
        crits = [self._crit("PASS"), self._crit("PENDING"), self._crit("DEFERRED")]
        _, code = sp.verdict_of(crits)
        self.assertEqual(code, 2)


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


# ───────────────────────────────────────────────────────────────────────────
# Multi-stage extension tests (STAGE_1 → 2, 2 → 3, 3 → 4)
# ───────────────────────────────────────────────────────────────────────────


def iso(dt: datetime) -> str:
    return dt.strftime("%Y-%m-%dT%H:%M:%SZ")


def write_trades_window(jdir: Path, start: datetime, n: int,
                        win_rate: float = 0.21, win_pnl: float = 6000.0,
                        loss_pnl: float = -1000.0, fee_usd: float = 100.0,
                        slip_usd: float = 50.0, notional_usd: float = 100000.0,
                        single_sym: str | None = None,
                        end: datetime | None = None) -> None:
    """Generate n closes spread evenly between start and end (default: now).

    Distinct from `write_trades` (which keys off start_days_ago) — this
    variant takes an explicit window so multi-stage tests can position
    trades inside any specific stage. The two helpers don't share code
    because their key responsibilities (n_floor + days-since-FIRST-trade
    vs explicit window) diverge enough that merging would muddle both.
    """
    end_eff = end if end is not None else datetime.now(timezone.utc)
    span = end_eff - start
    jdir.mkdir(parents=True, exist_ok=True)
    syms = [single_sym] if single_sym else ["BTCUSDT", "ETHUSDT", "XLMUSDT"]
    interval = span / max(1, n)
    by_month: dict[str, list[str]] = {}
    # Interleave wins evenly across the timeline using a running accumulator
    # rather than "first n_wins are wins". The latter clusters all wins at
    # the start of the window and breaks any test that examines a sub-window
    # (e.g. "net-positive last 30d") because the recent slice would be
    # all-losses. Accumulator pattern: at win_rate=0.21 it picks roughly
    # every 4.76th trade as a win, evenly spaced.
    running = 0.0
    for i in range(n):
        ts = start + interval * i
        running += win_rate
        is_win = running >= 1.0
        if is_win:
            running -= 1.0
        sym = syms[i % len(syms)]
        ts_str = iso(ts)
        ts_close_str = iso(ts + timedelta(minutes=30))
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
        # If a prior stage wrote to this same file (cross-stage cohort),
        # append rather than overwrite so trades from both stages persist.
        if path.exists():
            existing = path.read_text().rstrip("\n")
            path.write_text(existing + "\n" + "\n".join(lines) + "\n")
        else:
            path.write_text("\n".join(lines) + "\n")


class HelpersTest(unittest.TestCase):

    def test_filter_trades_after_excludes_earlier(self):
        cutoff = datetime(2026, 5, 1, tzinfo=timezone.utc)
        before = sp.Trade("2026-04-15T00:00:00Z", "BTC", "STOP", -1, 0, 0, 0)
        on = sp.Trade("2026-05-01T00:00:00Z", "BTC", "STOP", -1, 0, 0, 0)
        after = sp.Trade("2026-05-15T00:00:00Z", "BTC", "STOP", -1, 0, 0, 0)
        result = sp.filter_trades_after([before, on, after], cutoff)
        self.assertEqual(len(result), 2)  # on + after, not before

    def test_filter_trades_skips_unparseable_ts(self):
        cutoff = datetime(2026, 5, 1, tzinfo=timezone.utc)
        bad = sp.Trade("not-a-ts", "BTC", "STOP", -1, 0, 0, 0)
        good = sp.Trade("2026-05-15T00:00:00Z", "BTC", "STOP", -1, 0, 0, 0)
        result = sp.filter_trades_after([bad, good], cutoff)
        self.assertEqual(len(result), 1)
        self.assertEqual(result[0].symbol, "BTC")

    def test_parse_stage_start_arg_rejects_empty(self):
        with self.assertRaises(ValueError):
            sp.parse_stage_start_arg("", "--stage-1-start")

    def test_parse_stage_start_arg_rejects_garbage(self):
        with self.assertRaises(ValueError):
            sp.parse_stage_start_arg("Tuesday-ish", "--stage-1-start")

    def test_parse_stage_start_arg_accepts_valid_iso(self):
        dt = sp.parse_stage_start_arg("2026-05-01T00:00:00Z", "--stage-1-start")
        self.assertEqual(dt.year, 2026)
        self.assertEqual(dt.month, 5)
        self.assertEqual(dt.day, 1)


class Stage1To2UnitTest(unittest.TestCase):
    """Unit tests for STAGE_1 → STAGE_2 evaluators."""

    def test_check_n_trades_passes_at_threshold(self):
        trades = [sp.Trade("2026-05-01T00:00:00Z", "BTC", "STOP", -1, 0, 0, 0)] * 50
        c = sp.check_n_trades(trades, 50, "test")
        self.assertEqual(c.status, "PASS")

    def test_check_n_trades_pending_below(self):
        trades = [sp.Trade("2026-05-01T00:00:00Z", "BTC", "STOP", -1, 0, 0, 0)] * 49
        c = sp.check_n_trades(trades, 50, "test")
        self.assertEqual(c.status, "PENDING")

    def test_check_days_at_stage_pass(self):
        start = datetime.now(timezone.utc) - timedelta(days=35)
        c = sp.check_days_at_stage(start, 30, "test")
        self.assertEqual(c.status, "PASS")
        self.assertIn("35d", c.actual)

    def test_check_days_at_stage_pending(self):
        start = datetime.now(timezone.utc) - timedelta(days=20)
        c = sp.check_days_at_stage(start, 30, "test")
        self.assertEqual(c.status, "PENDING")

    def test_check_fee_within_tolerance_pass(self):
        # 10bp realized × $100k notional = $100 fee. 5% tolerance = ceiling 10.5bp.
        trades = [sp.Trade("2026-05-01T00:00:00Z", "BTC", "STOP", -1, 100, 50, 100000)] * 10
        c = sp.check_fee_within_tolerance(trades, 10.0, 0.05, "test")
        self.assertEqual(c.status, "PASS")

    def test_check_fee_within_tolerance_fail(self):
        # 11bp realized > 10.5bp ceiling → FAIL.
        trades = [sp.Trade("2026-05-01T00:00:00Z", "BTC", "STOP", -1, 110, 50, 100000)] * 10
        c = sp.check_fee_within_tolerance(trades, 10.0, 0.05, "test")
        self.assertEqual(c.status, "FAIL")

    def test_check_slip_within_tolerance_only_losers(self):
        # Mix winners (no slip) + losers at 5bp. 20% tolerance = ceiling 6bp.
        winners = [sp.Trade("2026-05-01T00:00:00Z", "BTC", "TARGET", 1, 100, 0, 100000)] * 10
        losers = [sp.Trade("2026-05-01T00:00:00Z", "BTC", "STOP", -1, 100, 50, 100000)] * 10
        c = sp.check_slip_within_tolerance(winners + losers, 5.0, 0.20, "test")
        self.assertEqual(c.status, "PASS")
        self.assertIn("5.00", c.actual)

    def test_check_slip_within_tolerance_fail(self):
        # 7bp slip > 6bp ceiling → FAIL.
        losers = [sp.Trade("2026-05-01T00:00:00Z", "BTC", "STOP", -1, 100, 70, 100000)] * 10
        c = sp.check_slip_within_tolerance(losers, 5.0, 0.20, "test")
        self.assertEqual(c.status, "FAIL")

    def test_check_no_daily_loss_above_under_floor_pending(self):
        # Below n_floor → PENDING (sampling variance protection).
        trades = [sp.Trade("2026-05-01T00:00:00Z", "BTC", "STOP", -300, 100, 50, 100000)] * 5
        c = sp.check_no_daily_loss_above(trades, 200.0, 50, "test")
        self.assertEqual(c.status, "PENDING")

    def test_check_no_daily_loss_above_fires_on_bad_day(self):
        # 60 trades on the same day, all losing $5 each = -$300 day < -$200 cap.
        trades = [sp.Trade("2026-05-01T10:00:00Z", "BTC", "STOP", -5, 100, 50, 100000)] * 60
        c = sp.check_no_daily_loss_above(trades, 200.0, 50, "test")
        self.assertEqual(c.status, "FAIL")

    def test_check_no_daily_loss_above_pass_when_no_bad_day(self):
        # 60 trades spread across days, each day -$50 net.
        base = datetime(2026, 5, 1, tzinfo=timezone.utc)
        trades = [
            sp.Trade(iso(base + timedelta(days=i)), "BTC", "STOP", -50, 100, 50, 100000)
            for i in range(60)
        ]
        c = sp.check_no_daily_loss_above(trades, 200.0, 50, "test")
        self.assertEqual(c.status, "PASS")

    def test_check_net_positive_at_stage_pass(self):
        trades = [sp.Trade("2026-05-01T00:00:00Z", "BTC", "TARGET", 100, 0, 0, 0)] * 50
        c = sp.check_net_positive_at_stage(trades, 50, "test")
        self.assertEqual(c.status, "PASS")

    def test_check_net_positive_at_stage_below_floor_pending(self):
        # Negative pnl but below floor → PENDING, not FAIL.
        trades = [sp.Trade("2026-05-01T00:00:00Z", "BTC", "STOP", -100, 0, 0, 0)] * 5
        c = sp.check_net_positive_at_stage(trades, 50, "test")
        self.assertEqual(c.status, "PENDING")

    def test_check_net_positive_at_stage_above_floor_fail(self):
        trades = [sp.Trade("2026-05-01T00:00:00Z", "BTC", "STOP", -100, 0, 0, 0)] * 50
        c = sp.check_net_positive_at_stage(trades, 50, "test")
        self.assertEqual(c.status, "FAIL")


class Stage2To3UnitTest(unittest.TestCase):
    """Unit tests for STAGE_2 → STAGE_3 evaluators."""

    def test_recent_window_stability_both_pass(self):
        # 30 trades at exactly modeled fee + slip ceiling → PASS.
        trades = [sp.Trade("2026-05-01T00:00:00Z", "BTC", "STOP", -1, 100, 50, 100000)] * 30
        c = sp.check_recent_window_stability(
            trades, 30, 10.0, 5.0, 0.10, "test")
        self.assertEqual(c.status, "PASS")

    def test_recent_window_stability_fee_fails(self):
        # 12bp fee > 11bp ceiling (10×1.10) → FAIL.
        trades = [sp.Trade("2026-05-01T00:00:00Z", "BTC", "STOP", -1, 120, 50, 100000)] * 30
        c = sp.check_recent_window_stability(
            trades, 30, 10.0, 5.0, 0.10, "test")
        self.assertEqual(c.status, "FAIL")
        self.assertIn("12.00", c.actual)

    def test_recent_window_stability_slip_fails(self):
        # Fee fine (10bp), but slip 6bp > 5.5bp ceiling (5×1.10) → FAIL.
        trades = [sp.Trade("2026-05-01T00:00:00Z", "BTC", "STOP", -1, 100, 60, 100000)] * 30
        c = sp.check_recent_window_stability(
            trades, 30, 10.0, 5.0, 0.10, "test")
        self.assertEqual(c.status, "FAIL")

    def test_recent_window_stability_window_too_small(self):
        # Only 20 trades w/ cost data, need 30 → PENDING.
        trades = [sp.Trade("2026-05-01T00:00:00Z", "BTC", "STOP", -1, 100, 50, 100000)] * 20
        c = sp.check_recent_window_stability(
            trades, 30, 10.0, 5.0, 0.10, "test")
        self.assertEqual(c.status, "PENDING")

    def test_recent_window_stability_no_losers_pending(self):
        # 30 winners → slip is unmeasurable → PENDING, NOT PASS. Fail-open
        # closure: the pre-reg gate requires fee/slip stable (conjunction),
        # so granting PASS based on fee alone would lie about slip stability.
        trades = [sp.Trade("2026-05-01T00:00:00Z", "BTC", "TARGET", 1, 100, 0, 100000)] * 30
        c = sp.check_recent_window_stability(
            trades, 30, 10.0, 5.0, 0.10, "test")
        self.assertEqual(c.status, "PENDING")
        self.assertIn("unmeasurable", c.actual.lower())

    def test_recent_net_positive_pass(self):
        # 30 wins in last 5 days = +$30k → PASS.
        recent = datetime.now(timezone.utc) - timedelta(days=5)
        trades = [sp.Trade(iso(recent + timedelta(hours=i)), "BTC", "TARGET",
                          1000, 100, 0, 100000) for i in range(30)]
        c = sp.check_recent_net_positive(trades, 30, 30, "test")
        self.assertEqual(c.status, "PASS")

    def test_recent_net_positive_excludes_old(self):
        # 50 old wins (> 30 days ago) → only 0 in window → PENDING (need 30).
        old = datetime.now(timezone.utc) - timedelta(days=60)
        trades = [sp.Trade(iso(old + timedelta(hours=i)), "BTC", "TARGET",
                          1000, 100, 0, 100000) for i in range(50)]
        c = sp.check_recent_net_positive(trades, 30, 30, "test")
        self.assertEqual(c.status, "PENDING")

    def test_check_single_symbol_at_stage_n_floor_blocks_spurious_kill(self):
        # 1 trade on one sym = 100% concentration but n=1 → PENDING, not FAIL.
        trades = [sp.Trade("2026-05-01T00:00:00Z", "BTC", "STOP", -100, 0, 0, 0)]
        c = sp.check_single_symbol_at_stage(trades, 40.0, 100, "test")
        self.assertEqual(c.status, "PENDING")


class Stage3To4UnitTest(unittest.TestCase):
    """Unit tests for STAGE_3 → STAGE_4 evaluators."""

    def test_annualized_at_stage_pass(self):
        # Threshold at 90d: $69k × (90/365.25) × 0.5 = $8,500.
        # Need $8.5k+ over 90d → 30 wins × $300 = $9k.
        start = datetime.now(timezone.utc) - timedelta(days=90)
        trades = [sp.Trade(iso(start + timedelta(days=i*3)), "BTC", "TARGET",
                          300, 100, 0, 100000) for i in range(30)]
        c = sp.check_annualized_at_stage(trades, start, 0.50, "test", 30)
        self.assertEqual(c.status, "PASS")

    def test_annualized_at_stage_below_threshold_fails(self):
        start = datetime.now(timezone.utc) - timedelta(days=90)
        # Only $1k pnl over 90d, threshold is ~$8.5k → FAIL.
        trades = [sp.Trade(iso(start + timedelta(days=i*3)), "BTC", "TARGET",
                          33, 100, 0, 100000) for i in range(30)]
        c = sp.check_annualized_at_stage(trades, start, 0.50, "test", 30)
        self.assertEqual(c.status, "FAIL")

    def test_annualized_at_stage_low_n_pending(self):
        start = datetime.now(timezone.utc) - timedelta(days=90)
        trades = [sp.Trade(iso(start), "BTC", "TARGET", 100000, 100, 0, 100000)] * 5
        c = sp.check_annualized_at_stage(trades, start, 0.50, "test", 30)
        self.assertEqual(c.status, "PENDING")


class MultiStageVerdictIntegrationTest(unittest.TestCase):
    """End-to-end exit-code tests for STAGE_1/2/3 transitions via CLI."""

    def test_stage_1_missing_start_arg_errors(self):
        code, out = run_cli([
            "--from-stage", "STAGE_1",
            "--live-source", "local",
            "--live-dir", "/tmp/__nonexistent_stage_1__",
        ])
        self.assertEqual(code, 3, f"expected ERROR, got {code}\n{out}")
        self.assertIn("--stage-1-start is required", out)

    def test_stage_2_missing_one_start_errors(self):
        code, out = run_cli([
            "--from-stage", "STAGE_2",
            "--stage-1-start", "2026-04-01T00:00:00Z",
            # Missing --stage-2-start.
            "--live-source", "local",
            "--live-dir", "/tmp/__nonexistent_stage_2__",
        ])
        self.assertEqual(code, 3, f"expected ERROR, got {code}\n{out}")
        self.assertIn("--stage-2-start is required", out)

    def test_stage_3_missing_one_start_errors(self):
        # Missing --stage-3-start specifically.
        code, out = run_cli([
            "--from-stage", "STAGE_3",
            "--stage-1-start", "2026-01-01T00:00:00Z",
            "--stage-2-start", "2026-02-01T00:00:00Z",
            "--live-source", "local",
            "--live-dir", "/tmp/__nonexistent_stage_3__",
        ])
        self.assertEqual(code, 3, f"expected ERROR, got {code}\n{out}")
        self.assertIn("--stage-3-start is required", out)

    def test_stage_2_inverted_starts_errors(self):
        """Operator typo: --stage-1-start AFTER --stage-2-start (e.g. flags
        accidentally swapped). Pre-fix: filter_trades_after silently
        produced nonsense windows where pre-stage_1 trades counted toward
        STAGE_2 cumulative metrics. Now rejected at arg-parse time."""
        code, out = run_cli([
            "--from-stage", "STAGE_2",
            "--stage-1-start", "2026-08-01T00:00:00Z",
            "--stage-2-start", "2026-05-01T00:00:00Z",  # before stage-1
            "--live-source", "local",
            "--live-dir", "/tmp",
        ])
        self.assertEqual(code, 3, f"expected ERROR on inversion, got {code}\n{out}")
        self.assertIn("chronological", out.lower())

    def test_stage_3_inverted_starts_errors(self):
        """Same defense across all three stage-start flags."""
        code, out = run_cli([
            "--from-stage", "STAGE_3",
            "--stage-1-start", "2026-01-01T00:00:00Z",
            "--stage-2-start", "2026-06-01T00:00:00Z",
            "--stage-3-start", "2026-03-01T00:00:00Z",  # before stage-2
            "--live-source", "local",
            "--live-dir", "/tmp",
        ])
        self.assertEqual(code, 3, f"expected ERROR on inversion, got {code}\n{out}")
        self.assertIn("chronological", out.lower())

    def test_stage_2_equal_starts_errors(self):
        """Stage starts must be STRICTLY chronological — equal timestamps
        are a degenerate case (zero-day STAGE_1 window) that the operator
        almost certainly didn't intend."""
        code, out = run_cli([
            "--from-stage", "STAGE_2",
            "--stage-1-start", "2026-05-01T00:00:00Z",
            "--stage-2-start", "2026-05-01T00:00:00Z",
            "--live-source", "local",
            "--live-dir", "/tmp",
        ])
        self.assertEqual(code, 3, f"expected ERROR on equal stamps, got {code}\n{out}")
        self.assertIn("chronological", out.lower())

    def test_stage_1_malformed_start_errors(self):
        code, out = run_cli([
            "--from-stage", "STAGE_1",
            "--stage-1-start", "Tuesday afternoon",
            "--live-source", "local",
            "--live-dir", "/tmp",
        ])
        self.assertEqual(code, 3, f"expected ERROR, got {code}\n{out}")
        self.assertIn("not a valid ISO timestamp", out)

    def test_stage_1_promote_path_all_gates_pass(self):
        with tempfile.TemporaryDirectory() as tmp:
            jdir = Path(tmp) / "journal"
            history = Path(tmp) / "history.jsonl"
            now = datetime.now(timezone.utc)
            stage_1_start = now - timedelta(days=35)
            # STAGE_1 economics: $100/trade stake, ~6:1 RR. Choosing
            # loss_pnl=-40 keeps daily net loss below the $200 cap even on
            # a 4-loser day (4 × $40 = $160 < $200). Fixture mirrors real
            # STAGE_1 scale: small per-trade pnl, ~$15k notional → fee 10bp,
            # slip 5bp (modeled). 60 trades / 35d = ~1.7/day, sustainable
            # under 5min interleaving across 3 symbols.
            write_trades_window(jdir, stage_1_start, n=60,
                                win_rate=0.21, win_pnl=240, loss_pnl=-40,
                                fee_usd=15, slip_usd=7.5, notional_usd=15000)
            write_drift_history(history, [
                ((now - timedelta(days=21)).strftime("%Y-%m-%dT%H:%M:%SZ"), "CLEAN"),
                ((now - timedelta(days=14)).strftime("%Y-%m-%dT%H:%M:%SZ"), "CLEAN"),
                ((now - timedelta(days=7)).strftime("%Y-%m-%dT%H:%M:%SZ"), "CLEAN"),
            ])
            code, out = run_cli([
                "--from-stage", "STAGE_1",
                "--stage-1-start", iso(stage_1_start),
                "--live-source", "local",
                "--live-dir", str(jdir),
                "--drift-history", str(history),
            ])
            self.assertEqual(code, 0,
                f"expected PROMOTE (exit 0), got {code}\n{out}")
            self.assertIn("PROMOTE", out)

    def test_stage_1_blocked_on_fee_over_tolerance(self):
        with tempfile.TemporaryDirectory() as tmp:
            jdir = Path(tmp) / "journal"
            history = Path(tmp) / "history.jsonl"
            now = datetime.now(timezone.utc)
            stage_1_start = now - timedelta(days=35)
            # Fee 11bp > 10.5bp ceiling → FAIL.
            write_trades_window(jdir, stage_1_start, n=60,
                                fee_usd=110, slip_usd=50)
            write_drift_history(history, [
                ((now - timedelta(days=21)).strftime("%Y-%m-%dT%H:%M:%SZ"), "CLEAN"),
            ])
            code, out = run_cli([
                "--from-stage", "STAGE_1",
                "--stage-1-start", iso(stage_1_start),
                "--live-source", "local",
                "--live-dir", str(jdir),
                "--drift-history", str(history),
            ])
            self.assertEqual(code, 1,
                f"expected BLOCKED, got {code}\n{out}")
            self.assertIn("BLOCKED", out)

    def test_stage_1_segments_out_pre_stage_trades(self):
        """Critical correctness: trades from BEFORE stage_1_start (e.g. residual
        STAGE_0 paper trades in the same journal directory) must NOT count
        toward STAGE_1 gates. Otherwise the operator would be evaluating
        STAGE_1 gates against a pool that's mostly pre-stage paper data."""
        with tempfile.TemporaryDirectory() as tmp:
            jdir = Path(tmp) / "journal"
            history = Path(tmp) / "history.jsonl"
            now = datetime.now(timezone.utc)
            # 200 PAPER (pre-S1) trades that would WAY exceed thresholds.
            paper_start = now - timedelta(days=120)
            paper_end = now - timedelta(days=40)
            write_trades_window(jdir, paper_start, n=200,
                                win_rate=0.5, win_pnl=10000, loss_pnl=-100,
                                fee_usd=100, slip_usd=50, end=paper_end)
            # Only 5 actual S1 trades (below 50 floor).
            stage_1_start = now - timedelta(days=35)
            write_trades_window(jdir, stage_1_start, n=5,
                                win_rate=0.5, win_pnl=10000, loss_pnl=-100,
                                fee_usd=100, slip_usd=50)
            write_drift_history(history, [
                ((now - timedelta(days=21)).strftime("%Y-%m-%dT%H:%M:%SZ"), "CLEAN"),
            ])
            code, out = run_cli([
                "--from-stage", "STAGE_1",
                "--stage-1-start", iso(stage_1_start),
                "--live-source", "local",
                "--live-dir", str(jdir),
                "--drift-history", str(history),
            ])
            # Expected: WAITING (only 5 S1 trades, below 50). If segmentation
            # broken, the 200 paper trades would push it to PROMOTE — that's
            # the regression this test catches.
            self.assertEqual(code, 2,
                f"expected WAITING (S1 segmentation), got {code}\n{out}")
            self.assertIn("WAITING", out)
            # The trade-count line should show 5, not 205.
            self.assertIn(" 5  ", out, f"expected n=5 in output:\n{out}")

    def test_stage_2_promote_path(self):
        with tempfile.TemporaryDirectory() as tmp:
            jdir = Path(tmp) / "journal"
            history = Path(tmp) / "history.jsonl"
            now = datetime.now(timezone.utc)
            stage_1_start = now - timedelta(days=80)
            stage_2_start = now - timedelta(days=65)
            # 110 S1+S2 trades — exceeds 100 floor.
            write_trades_window(jdir, stage_1_start, n=110,
                                win_rate=0.21, win_pnl=6000, loss_pnl=-1000,
                                fee_usd=100, slip_usd=50, notional_usd=100000)
            write_drift_history(history, [
                ((now - timedelta(days=44)).strftime("%Y-%m-%dT%H:%M:%SZ"), "CLEAN"),
                ((now - timedelta(days=14)).strftime("%Y-%m-%dT%H:%M:%SZ"), "CLEAN"),
            ])
            code, out = run_cli([
                "--from-stage", "STAGE_2",
                "--stage-1-start", iso(stage_1_start),
                "--stage-2-start", iso(stage_2_start),
                "--live-source", "local",
                "--live-dir", str(jdir),
                "--drift-history", str(history),
            ])
            self.assertEqual(code, 0,
                f"expected PROMOTE, got {code}\n{out}")
            self.assertIn("PROMOTE", out)

    def test_stage_3_promote_path(self):
        with tempfile.TemporaryDirectory() as tmp:
            jdir = Path(tmp) / "journal"
            history = Path(tmp) / "history.jsonl"
            now = datetime.now(timezone.utc)
            stage_1_start = now - timedelta(days=180)
            stage_2_start = now - timedelta(days=150)
            stage_3_start = now - timedelta(days=95)
            # 220 cumulative (≥200), reasonable cost profile.
            write_trades_window(jdir, stage_1_start, n=220,
                                win_rate=0.21, win_pnl=6000, loss_pnl=-1000,
                                fee_usd=100, slip_usd=50, notional_usd=100000)
            write_drift_history(history, [
                ((now - timedelta(days=80)).strftime("%Y-%m-%dT%H:%M:%SZ"), "CLEAN"),
                ((now - timedelta(days=40)).strftime("%Y-%m-%dT%H:%M:%SZ"), "CLEAN"),
                ((now - timedelta(days=10)).strftime("%Y-%m-%dT%H:%M:%SZ"), "CLEAN"),
            ])
            code, out = run_cli([
                "--from-stage", "STAGE_3",
                "--stage-1-start", iso(stage_1_start),
                "--stage-2-start", iso(stage_2_start),
                "--stage-3-start", iso(stage_3_start),
                "--live-source", "local",
                "--live-dir", str(jdir),
                "--drift-history", str(history),
            ])
            self.assertEqual(code, 0,
                f"expected PROMOTE, got {code}\n{out}")
            self.assertIn("PROMOTE", out)


if __name__ == "__main__":
    unittest.main(verbosity=2)
