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


if __name__ == "__main__":
    unittest.main(verbosity=2)
