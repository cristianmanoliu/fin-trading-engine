#!/usr/bin/env python3
"""
Functional tests for layer2_smoke.sh.

Drives the smoke harness via subprocess in DRY_RUN mode to exercise the
env-var validation and log-analysis gates without actually invoking
cmd/engine or reaching testnet.binancefuture.com. Covers:

  - missing BINANCE_API_KEY                  → exit 3 (env)
  - missing BINANCE_API_SECRET                → exit 3 (env)
  - missing LAYER2_SMOKE_ACKNOWLEDGE_TESTNET  → exit 3 (env)
  - unknown CLI arg                           → exit 3 (env)
  - full DRY_RUN happy path                   → exit 0 (pass)
  - --duration override is honoured           → log contains correct duration

The DRY_RUN mode simulates a clean engine log so the analysis phase can
verify the happy path. Auth-failure / heartbeat-missing / generic-error
branches are exercised via injected log content rather than real engine
invocation (the analysis phase greps the log file).

Run:
  python3 scripts/test_layer2_smoke.py
"""
from __future__ import annotations

import os
import subprocess
import tempfile
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
SCRIPT = REPO / "scripts" / "layer2_smoke.sh"


def run_smoke(*args: str, env_extra: dict[str, str] | None = None) -> tuple[int, str, str]:
    cmd = ["bash", str(SCRIPT), *args]
    env = os.environ.copy()
    for k in ("TELEGRAM_BOT_TOKEN", "TELEGRAM_CHAT_ID",
              "BINANCE_API_KEY", "BINANCE_API_SECRET",
              "LAYER2_SMOKE_ACKNOWLEDGE_TESTNET",
              "LAYER2_SMOKE_DRY_RUN",
              "LAYER2_SMOKE_INJECT_LOG"):
        env.pop(k, None)
    if env_extra:
        env.update(env_extra)
    result = subprocess.run(cmd, capture_output=True, text=True, env=env)
    return result.returncode, result.stdout, result.stderr


def _inject_env(log_content: str, tmpdir: Path) -> dict[str, str]:
    """Write log_content to a fixture file inside tmpdir and return the
    env extras needed to feed it into the smoke as the injected log."""
    fixture = tmpdir / "injected.log"
    fixture.write_text(log_content)
    return {
        "BINANCE_API_KEY": "fake",
        "BINANCE_API_SECRET": "fake",
        "LAYER2_SMOKE_ACKNOWLEDGE_TESTNET": "YES",
        "LAYER2_SMOKE_DRY_RUN": "1",
        "LAYER2_SMOKE_INJECT_LOG": str(fixture),
    }


# ── Tests ────────────────────────────────────────────────────────────────────

class EnvValidationTest(unittest.TestCase):
    """Env-var gates fire before any expensive operation."""

    def test_missing_api_key_exits_3(self):
        code, _, err = run_smoke("BTCUSDT")
        self.assertEqual(code, 3, err)
        self.assertIn("env-key-missing", err)

    def test_missing_api_secret_exits_3(self):
        code, _, err = run_smoke("BTCUSDT", env_extra={"BINANCE_API_KEY": "fake"})
        self.assertEqual(code, 3, err)
        self.assertIn("env-secret-missing", err)

    def test_missing_intent_acknowledge_exits_3(self):
        code, _, err = run_smoke("BTCUSDT", env_extra={
            "BINANCE_API_KEY": "fake",
            "BINANCE_API_SECRET": "fake",
        })
        self.assertEqual(code, 3, err)
        self.assertIn("testnet-intent-not-acknowledged", err)

    def test_unknown_arg_exits_3(self):
        code, _, err = run_smoke("BTCUSDT", "--nonsense", "value",
                                 env_extra={"BINANCE_API_KEY": "fake",
                                            "BINANCE_API_SECRET": "fake",
                                            "LAYER2_SMOKE_ACKNOWLEDGE_TESTNET": "YES"})
        self.assertEqual(code, 3, err)


class DryRunHappyPathTest(unittest.TestCase):
    """Full DRY_RUN traversal: all gates pass, synthetic log analysis succeeds."""

    def test_happy_path_exits_0(self):
        code, out, err = run_smoke("BTCUSDT", env_extra={
            "BINANCE_API_KEY": "fake",
            "BINANCE_API_SECRET": "fake",
            "LAYER2_SMOKE_ACKNOWLEDGE_TESTNET": "YES",
            "LAYER2_SMOKE_DRY_RUN": "1",
        })
        self.assertEqual(code, 0, err)
        self.assertIn("layer2_smoke PASS", out)
        self.assertIn("heartbeats=1", out)
        self.assertIn("backfill_complete=1", out)

    def test_custom_duration_propagates(self):
        code, out, _ = run_smoke("ETHUSDT", "--duration", "600", env_extra={
            "BINANCE_API_KEY": "fake",
            "BINANCE_API_SECRET": "fake",
            "LAYER2_SMOKE_ACKNOWLEDGE_TESTNET": "YES",
            "LAYER2_SMOKE_DRY_RUN": "1",
        })
        self.assertEqual(code, 0, out)
        self.assertIn("duration=600s", out)
        self.assertIn("ETHUSDT", out)


class LogAnalysisFailureModeTest(unittest.TestCase):
    """Exercise the analysis-phase failure shapes via LAYER2_SMOKE_INJECT_LOG.

    Each test injects a canned log fixture that the smoke's grep-based
    analysis reads, then verifies the correct exit code + die() subject
    line. Closes the documented gap where the script's 4 analysis-phase
    failure branches (auth, error-line, no-heartbeat, no-backfill) were
    pattern-pinned only by manual smoke at commit time.
    """

    def _run_with_log(self, log_content: str) -> tuple[int, str, str]:
        # Use tempfile + cleanup pattern. Each test owns its own fixture
        # so they can run in any order (and in parallel under pytest -n).
        with tempfile.TemporaryDirectory() as td:
            env = _inject_env(log_content, Path(td))
            return run_smoke("BTCUSDT", env_extra=env)

    def test_auth_failure_binance_code_2014_exits_2(self):
        """Binance's -2014 (bad key format) doesn't emit HTTP 401/403 —
        L2-3 added explicit code-pattern matching so this lands at exit 2
        not the catch-all exit 6 (which would misroute to Telegram tier
        'engine error' instead of 'credential rejected')."""
        log = (
            '{"level":"INFO","msg":"backfill complete","symbol":"BTCUSDT"}\n'
            '{"level":"INFO","msg":"heartbeat","symbol":"BTCUSDT"}\n'
            '{"level":"ERROR","msg":"binance reject","code":-2014,'
            '"reason":"API-key format invalid"}\n'
        )
        code, _, err = self._run_with_log(log)
        self.assertEqual(code, 2, err)
        self.assertIn("credential-rejected", err)

    def test_auth_failure_http_403_exits_2(self):
        """Generic HTTP 403 in log → auth tier."""
        log = (
            '{"level":"INFO","msg":"backfill complete","symbol":"BTCUSDT"}\n'
            '{"level":"INFO","msg":"heartbeat","symbol":"BTCUSDT"}\n'
            '{"level":"WARN","msg":"order rejected","status":403}\n'
        )
        code, _, err = self._run_with_log(log)
        self.assertEqual(code, 2, err)
        self.assertIn("credential-rejected", err)

    def test_generic_error_line_exits_6(self):
        """Non-auth ERROR-level line → exit 6 (engine-error-during-smoke).
        Verifies the auth check runs BEFORE the generic-error check, so an
        auth failure with the literal token 'ERROR' in it doesn't land in
        the wrong bucket. Here we use a non-auth shape."""
        log = (
            '{"level":"INFO","msg":"backfill complete","symbol":"BTCUSDT"}\n'
            '{"level":"INFO","msg":"heartbeat","symbol":"BTCUSDT"}\n'
            '{"level":"ERROR","msg":"unexpected nil pointer in tick handler"}\n'
        )
        code, _, err = self._run_with_log(log)
        self.assertEqual(code, 6, err)
        self.assertIn("engine-error-during-smoke", err)

    def test_no_heartbeat_exits_1(self):
        """Backfill landed but no heartbeat fired → exit 1 (no-heartbeat).
        This is the 'engine startup got stuck' shape; distinct from a
        crash (exit 6) and from auth failure (exit 2)."""
        log = (
            '{"level":"INFO","msg":"backfill complete","symbol":"BTCUSDT"}\n'
        )
        code, _, err = self._run_with_log(log)
        self.assertEqual(code, 1, err)
        self.assertIn("no-heartbeat", err)

    def test_no_backfill_exits_6(self):
        """Heartbeat fired but no backfill-complete log → exit 6
        (backfill-incomplete). L2-4 added this gate: a warming-up
        heartbeat ('no ticks yet') matches the heartbeat substring,
        so without the backfill gate an unprimed engine would PASS."""
        log = (
            '{"level":"INFO","msg":"heartbeat","symbol":"BTCUSDT",'
            '"warning":"warming up (no ticks yet)"}\n'
        )
        code, _, err = self._run_with_log(log)
        self.assertEqual(code, 6, err)
        self.assertIn("backfill-incomplete", err)

    def test_missing_inject_fixture_exits_3(self):
        """If the test harness points LAYER2_SMOKE_INJECT_LOG at a path
        that doesn't exist, the smoke fails loudly (env tier) rather
        than producing a misleading PASS from an empty/missing fixture."""
        env = {
            "BINANCE_API_KEY": "fake",
            "BINANCE_API_SECRET": "fake",
            "LAYER2_SMOKE_ACKNOWLEDGE_TESTNET": "YES",
            "LAYER2_SMOKE_DRY_RUN": "1",
            "LAYER2_SMOKE_INJECT_LOG": "/tmp/does-not-exist-2026-05-11.log",
        }
        code, _, err = run_smoke("BTCUSDT", env_extra=env)
        self.assertEqual(code, 3, err)
        self.assertIn("inject-log-missing", err)


class SymbolConfigResolutionTest(unittest.TestCase):
    """L2-6 (2026-05-12 first-real-run finding): cmd/engine has no --symbol
    flag — symbol is sourced from the YAML's symbol: key. SYMBOL → config
    mapping must (a) succeed for known per-symbol YAMLs, (b) fail loudly
    via SMOKE_FAIL_ENV when no YAML exists for the requested symbol."""

    def test_btcusdt_resolves_to_per_symbol_yaml(self):
        """Happy path: BTCUSDT has configs/btcusdt.yaml; smoke reaches phase 4."""
        code, out, err = run_smoke("BTCUSDT", env_extra={
            "BINANCE_API_KEY": "fake",
            "BINANCE_API_SECRET": "fake",
            "LAYER2_SMOKE_ACKNOWLEDGE_TESTNET": "YES",
            "LAYER2_SMOKE_DRY_RUN": "1",
        })
        self.assertEqual(code, 0, err)
        self.assertIn("configs/btcusdt.yaml", out)
        self.assertIn("per-symbol config:", out)

    def test_unknown_symbol_exits_3_with_diagnostic(self):
        """Failure path (non-DRY_RUN): if SYMBOL has no per-symbol YAML,
        die with SMOKE_FAIL_ENV (exit 3) BEFORE invoking cmd/engine.
        Pre-fix: --symbol $SYMBOL was passed to cmd/engine, which has no
        such flag, so the engine exited rc=2 ("flag provided but not
        defined") within 100ms — misclassified by phase 4 as
        engine-crashed-early (exit 6, CRITICAL Telegram tier).
        Note: this test uses non-DRY_RUN to exercise the existence check
        (which is bypassed under DRY_RUN to keep the env-only test path)."""
        code, _, err = run_smoke("NEVERUSDT", env_extra={
            "BINANCE_API_KEY": "fake",
            "BINANCE_API_SECRET": "fake",
            "LAYER2_SMOKE_ACKNOWLEDGE_TESTNET": "YES",
            # NOT setting LAYER2_SMOKE_DRY_RUN — the existence check is
            # bypassed under DRY_RUN. Phase 1.5 (the new check) runs
            # before the timeout-pre-flight (phase 1 end), so this fails
            # cleanly even on systems without GNU timeout.
        })
        self.assertEqual(code, 3, err)
        self.assertIn("missing-symbol-config", err)


class FixtureMsgVsEngineEmitTest(unittest.TestCase):
    """L2-7 (2026-05-12 first-real-run finding): the DRY_RUN synthetic log
    had `"msg":"backfill complete"` but the engine emits
    `"msg":"kline backfill complete"` (pkg/marketdata/binance.go:220).
    The HAS_BACKFILL grep matched the fixture (DRY_RUN PASS) but NOT the
    real engine log (real smoke FAIL even when backfill succeeded).

    Pin: each msg field in the DRY_RUN heredoc MUST appear as a substring
    in the live engine source. If a future engine refactor renames a msg
    field, the heredoc and grep pattern must both be updated in lockstep,
    or this test fails. Symmetric pin: the HAS_BACKFILL/N_HEARTBEATS
    regexes in the analysis phase also live in the engine source.

    Pattern family: writer-equals-fixture drift, adjacent to
    writer-equals-model (docs/AUDIT_LENS.md adjacent-pattern, 2026-05-10-pm)."""

    REPO_GO_SOURCES = [
        REPO / "pkg" / "marketdata" / "binance.go",
        REPO / "pkg" / "marketdata" / "heartbeat.go",
        REPO / "cmd" / "engine" / "main.go",
    ]

    @classmethod
    def setUpClass(cls):
        cls.all_go_source = "\n".join(
            p.read_text(encoding="utf-8") for p in cls.REPO_GO_SOURCES if p.exists()
        )
        if not cls.all_go_source:
            raise unittest.SkipTest(
                f"Engine Go sources not readable: {cls.REPO_GO_SOURCES}"
            )

    def _smoke_text(self) -> str:
        return SCRIPT.read_text(encoding="utf-8")

    def test_synthetic_kline_backfill_complete_matches_engine_emit(self):
        """The exact substring used in the DRY_RUN heredoc + the
        HAS_BACKFILL grep regex must appear in pkg/marketdata/binance.go."""
        smoke = self._smoke_text()
        self.assertIn(
            '"msg":"kline backfill complete"',
            smoke,
            "synthetic DRY_RUN log no longer emits kline-backfill-complete",
        )
        self.assertIn(
            '"msg":"kline backfill complete',
            smoke,
            "HAS_BACKFILL grep no longer references kline-backfill-complete",
        )
        # The engine emits this via slog.Info("kline backfill complete", ...)
        # which renders as `"msg":"kline backfill complete"` in JSON output.
        self.assertIn(
            "kline backfill complete",
            self.all_go_source,
            "engine source no longer contains 'kline backfill complete' — "
            "smoke gate regex would silently no-op. Update heredoc + grep "
            "+ this assertion in lockstep.",
        )

    def test_synthetic_heartbeat_matches_engine_emit(self):
        """Canonical heartbeat msg comes from snapshotForLog returning the
        literal string "heartbeat" (heartbeat.go around line 203).
        The variable indirection means there's no `slog.Info("heartbeat...`
        call — the assertion anchors on the returned-string idiom instead."""
        smoke = self._smoke_text()
        self.assertIn('"msg":"heartbeat"', smoke)
        self.assertIn(
            '"heartbeat", args',
            self.all_go_source,
            "canonical heartbeat msg-string return not found in engine source "
            "(expected: `return slog.Level..., \"heartbeat\", args, ...` in heartbeat.go)",
        )

    def test_synthetic_testnet_executor_active_matches_engine_emit(self):
        """The TESTNET EXECUTOR ACTIVE WARN is the operator-visible
        confirmation that orders route to testnet. The string is a
        contract between cmd/engine and any monitoring/audit tooling."""
        smoke = self._smoke_text()
        marker = "TESTNET EXECUTOR ACTIVE — orders will be sent to Binance TESTNET (no real capital)"
        self.assertIn(marker, smoke)
        self.assertIn(
            marker,
            self.all_go_source,
            "engine source no longer contains the testnet-executor-active marker",
        )


if __name__ == "__main__":
    unittest.main(verbosity=2)
