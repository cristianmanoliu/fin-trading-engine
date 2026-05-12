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


# ── Lens-applied audit gaps ──────────────────────────────────────────────────


def _make_fake_diff_binary(tmpdir: Path, exit_code: int,
                           stdout: str = "fake journal_diff output\n") -> Path:
    """Write a 3-line shell script as a journal_diff stand-in. Tests use this
    via LAYER3_VERDICT_DIFF_OVERRIDE to inject canned exit codes that exercise
    the wrapper's dispatch logic without rebuilding Go."""
    fixture = tmpdir / "fake_journal_diff.sh"
    fixture.write_text(
        f"#!/usr/bin/env bash\n"
        f"echo {shlex_quote(stdout)}\n"
        f"exit {exit_code}\n"
    )
    fixture.chmod(0o755)
    return fixture


def shlex_quote(s: str) -> str:
    import shlex
    return shlex.quote(s)


class UnexpectedDiffExitTest(unittest.TestCase):
    """Closes F1 — the silent-on-corrupt-input pattern applied to subprocess
    exit codes. Pre-fix, journal_diff returning any code outside {0,1,2,3}
    fell through to the duration-then-PASS branch, which is a fail-open: a
    panic'd diff binary (exit 4) or a SIGSEGV (139) would promote a
    fundamentally-broken Layer 3 to STAGE_1.

    All these tests use LAYER3_VERDICT_DIFF_OVERRIDE to inject the desired
    exit code without rebuilding Go (faster + tests the contract directly).
    """

    def _setup_journals(self, stub: Path, tn: Path) -> None:
        """Both journals must contain trades for the duration check to run
        and the dispatch to reach the diff invocation."""
        base = dt.datetime.now(dt.timezone.utc).replace(tzinfo=None) \
               - dt.timedelta(days=10)
        write_pair(stub, "BTCUSDT", base, +5000.0)
        write_pair(tn,   "BTCUSDT", base, +5000.0)

    def test_diff_exit_4_panic_routes_to_input_error(self):
        """journal_diff exits 4 on Go panic (recoverPanic in main.go).
        Pre-fix, this fell through to PASS. Post-fix → exit 3."""
        with tempfile.TemporaryDirectory() as tmp_a, \
             tempfile.TemporaryDirectory() as tmp_b, \
             tempfile.TemporaryDirectory() as tmp_bin:
            stub, tn = Path(tmp_a), Path(tmp_b)
            self._setup_journals(stub, tn)
            fake = _make_fake_diff_binary(Path(tmp_bin), 4,
                                          stdout="PANIC: nil pointer\n")
            code, out, err = run_wrapper(
                "--stub-dir", str(stub), "--testnet-dir", str(tn),
                env_extra={"LAYER3_VERDICT_DIFF_OVERRIDE": str(fake)},
            )
            self.assertEqual(code, 3,
                f"diff_exit=4 (PANIC) must route to exit 3 not silently PASS, "
                f"got {code}\n{out}")
            self.assertIn("panicked", out)
            self.assertNotIn("PASS — Layer 3 criterion met", out)

    def test_diff_exit_139_segfault_routes_to_input_error(self):
        """SIGSEGV → exit 139. Pre-fix, fell through to PASS."""
        with tempfile.TemporaryDirectory() as tmp_a, \
             tempfile.TemporaryDirectory() as tmp_b, \
             tempfile.TemporaryDirectory() as tmp_bin:
            stub, tn = Path(tmp_a), Path(tmp_b)
            self._setup_journals(stub, tn)
            fake = _make_fake_diff_binary(Path(tmp_bin), 139)
            code, out, _ = run_wrapper(
                "--stub-dir", str(stub), "--testnet-dir", str(tn),
                env_extra={"LAYER3_VERDICT_DIFF_OVERRIDE": str(fake)},
            )
            self.assertEqual(code, 3,
                f"SIGSEGV-shape exit must route to exit 3, got {code}\n{out}")
            self.assertIn("unexpectedly", out)
            self.assertIn("139", out)

    def test_diff_exit_99_unknown_code_routes_to_input_error(self):
        """Any future exit code we don't yet recognise — refuse to PASS."""
        with tempfile.TemporaryDirectory() as tmp_a, \
             tempfile.TemporaryDirectory() as tmp_b, \
             tempfile.TemporaryDirectory() as tmp_bin:
            stub, tn = Path(tmp_a), Path(tmp_b)
            self._setup_journals(stub, tn)
            fake = _make_fake_diff_binary(Path(tmp_bin), 99)
            code, out, _ = run_wrapper(
                "--stub-dir", str(stub), "--testnet-dir", str(tn),
                env_extra={"LAYER3_VERDICT_DIFF_OVERRIDE": str(fake)},
            )
            self.assertEqual(code, 3,
                f"unknown diff_exit must route to exit 3, got {code}\n{out}")
            self.assertIn("99", out)

    def test_override_missing_binary_exits_3(self):
        """If the test harness points the override at a non-executable
        path, the script fails loudly rather than silently using the
        built binary or producing a misleading verdict."""
        with tempfile.TemporaryDirectory() as tmp_a, \
             tempfile.TemporaryDirectory() as tmp_b:
            stub, tn = Path(tmp_a), Path(tmp_b)
            self._setup_journals(stub, tn)
            code, _, err = run_wrapper(
                "--stub-dir", str(stub), "--testnet-dir", str(tn),
                env_extra={"LAYER3_VERDICT_DIFF_OVERRIDE":
                           "/tmp/does-not-exist-layer3-test"},
            )
            self.assertEqual(code, 3, err)
            self.assertIn("not executable", err)

    def test_override_diff_exit_0_still_passes(self):
        """Sanity check: the override doesn't break the happy path.
        Verifies the env-hook is wiring up correctly under the override
        path AND the normal-path build."""
        with tempfile.TemporaryDirectory() as tmp_a, \
             tempfile.TemporaryDirectory() as tmp_b, \
             tempfile.TemporaryDirectory() as tmp_bin:
            stub, tn = Path(tmp_a), Path(tmp_b)
            self._setup_journals(stub, tn)
            fake = _make_fake_diff_binary(Path(tmp_bin), 0)
            code, out, _ = run_wrapper(
                "--stub-dir", str(stub), "--testnet-dir", str(tn),
                env_extra={"LAYER3_VERDICT_DIFF_OVERRIDE": str(fake)},
            )
            self.assertEqual(code, 0,
                f"override with exit 0 must PASS, got {code}\n{out}")
            self.assertIn("PASS — Layer 3 criterion met", out)


class FutureTimestampGuardTest(unittest.TestCase):
    """Closes F2 — a future testnet-start (clock skew or operator typo)
    pre-fix produced elapsed_days < 0, which routes to INSUFFICIENT_DURATION
    (exit 4, "keep running, re-check") instead of INPUT_ERROR (exit 3, "fix
    the clock now"). The wrong advice would have a real operator silently
    accumulating noise instead of fixing the actual problem."""

    def test_future_testnet_start_exits_3(self):
        """--testnet-start one day in the future → exit 3, not exit 4."""
        with tempfile.TemporaryDirectory() as tmp_a, \
             tempfile.TemporaryDirectory() as tmp_b:
            stub, tn = Path(tmp_a), Path(tmp_b)
            base = dt.datetime.now(dt.timezone.utc).replace(tzinfo=None) \
                   - dt.timedelta(days=10)
            write_pair(stub, "BTCUSDT", base, +5000.0)
            write_pair(tn,   "BTCUSDT", base, +5000.0)
            future = (dt.datetime.now(dt.timezone.utc).replace(tzinfo=None)
                      + dt.timedelta(days=1)
                      ).strftime("%Y-%m-%dT%H:%M:%SZ")
            code, _, err = run_wrapper(
                "--stub-dir", str(stub), "--testnet-dir", str(tn),
                "--testnet-start", future,
            )
            self.assertEqual(code, 3, f"future start must exit 3, got {code}\n{err}")
            self.assertIn("future", err)
            # Verify it's NOT routing to the "keep running" branch.
            self.assertNotIn("NOT YET EVALUABLE", err)


class ExitCodePrecedenceTest(unittest.TestCase):
    """The wrapper's precedence comment claims: THRESHOLD > SIGNAL_DIV >
    INPUT_ERROR > duration > PASS. Pin each precedence pair with a test
    so a future refactor that moves the if-branches can't silently break
    the contract."""

    def _setup_journals_short_window(self, stub: Path, tn: Path) -> None:
        """Trades within the last 2 days — duration gate would fire."""
        recent = dt.datetime.now(dt.timezone.utc).replace(tzinfo=None) \
                 - dt.timedelta(days=2)
        write_pair(stub, "BTCUSDT", recent, +5000.0)
        write_pair(tn,   "BTCUSDT", recent, +5000.0)

    def test_threshold_violation_overrides_short_window(self):
        """diff_exit=1 (THRESHOLD) wins over duration < 7d."""
        with tempfile.TemporaryDirectory() as tmp_a, \
             tempfile.TemporaryDirectory() as tmp_b, \
             tempfile.TemporaryDirectory() as tmp_bin:
            stub, tn = Path(tmp_a), Path(tmp_b)
            self._setup_journals_short_window(stub, tn)
            fake = _make_fake_diff_binary(Path(tmp_bin), 1)
            code, out, _ = run_wrapper(
                "--stub-dir", str(stub), "--testnet-dir", str(tn),
                env_extra={"LAYER3_VERDICT_DIFF_OVERRIDE": str(fake)},
            )
            self.assertEqual(code, 1,
                f"THRESHOLD must win over duration shortfall, got {code}\n{out}")
            self.assertIn("THRESHOLD", out)

    def test_signal_divergence_overrides_short_window(self):
        """diff_exit=2 (SIGNAL_DIV) wins over duration < 7d."""
        with tempfile.TemporaryDirectory() as tmp_a, \
             tempfile.TemporaryDirectory() as tmp_b, \
             tempfile.TemporaryDirectory() as tmp_bin:
            stub, tn = Path(tmp_a), Path(tmp_b)
            self._setup_journals_short_window(stub, tn)
            fake = _make_fake_diff_binary(Path(tmp_bin), 2)
            code, out, _ = run_wrapper(
                "--stub-dir", str(stub), "--testnet-dir", str(tn),
                env_extra={"LAYER3_VERDICT_DIFF_OVERRIDE": str(fake)},
            )
            self.assertEqual(code, 2,
                f"SIGNAL_DIV must win over duration shortfall, got {code}\n{out}")
            self.assertIn("SIGNAL_DIVERGENCE", out)

    def test_panic_overrides_short_window(self):
        """diff_exit=4 (PANIC) wins over duration shortfall and routes to 3."""
        with tempfile.TemporaryDirectory() as tmp_a, \
             tempfile.TemporaryDirectory() as tmp_b, \
             tempfile.TemporaryDirectory() as tmp_bin:
            stub, tn = Path(tmp_a), Path(tmp_b)
            self._setup_journals_short_window(stub, tn)
            fake = _make_fake_diff_binary(Path(tmp_bin), 4)
            code, out, _ = run_wrapper(
                "--stub-dir", str(stub), "--testnet-dir", str(tn),
                env_extra={"LAYER3_VERDICT_DIFF_OVERRIDE": str(fake)},
            )
            self.assertEqual(code, 3,
                f"PANIC must win over duration shortfall + route to 3, "
                f"got {code}\n{out}")

    def test_skip_min_days_does_not_mask_threshold(self):
        """--skip-min-days makes duration advisory, but THRESHOLD still
        wins — operator dry-running mustn't accidentally PASS a real
        threshold violation."""
        with tempfile.TemporaryDirectory() as tmp_a, \
             tempfile.TemporaryDirectory() as tmp_b, \
             tempfile.TemporaryDirectory() as tmp_bin:
            stub, tn = Path(tmp_a), Path(tmp_b)
            self._setup_journals_short_window(stub, tn)
            fake = _make_fake_diff_binary(Path(tmp_bin), 1)
            code, out, _ = run_wrapper(
                "--stub-dir", str(stub), "--testnet-dir", str(tn),
                "--skip-min-days",
                env_extra={"LAYER3_VERDICT_DIFF_OVERRIDE": str(fake)},
            )
            self.assertEqual(code, 1,
                f"--skip-min-days must not mask THRESHOLD, got {code}\n{out}")


class CliFlagTest(unittest.TestCase):
    """Pin the CLI surface against accidental contract changes."""

    def test_unknown_flag_exits_3(self):
        code, _, err = run_wrapper("--nonsense", "value")
        self.assertEqual(code, 3, f"unknown flag must exit 3, got {code}\n{err}")
        self.assertIn("unknown flag", err)


class Layer3TopologyRegressionTest(unittest.TestCase):
    """End-to-end regression for the shadow-asymmetry fail-CLOSE that blocked
    every Layer 3 run before the journal_diff --include-shadows fix.

    The production topology (cmd/engine/main.go):
        STUB primary:    <journal>/<sym>-<month>.jsonl                      (Layer 3 dir-a)
        ShadowRunners:   <journal>/shadow/<label>/<sym>-<month>.jsonl       (only on stub side)
        Testnet shadow:  <layer3-testnet-dir>/<sym>-<month>.jsonl           (Layer 3 dir-b)

    Pre-fix, the diff binary unconditionally walked shadow/<label>/ on both
    sides. Since the testnet shadow doesn't produce shadow/<label>/ subdirs
    (only its single live executor writes), every stub-side shadow trade
    landed as "only-in-A" → SIGNAL_DIVERGENCE (exit 2) → wrapper returned
    Layer 3 FAIL. Operator could NEVER reach STAGE_1 from this gate.

    Post-fix, default behavior excludes shadows. Layer 3 PASSes when the
    live trades match, regardless of shadow tree presence on the stub side.
    """

    def test_stub_side_shadows_does_not_false_diverge(self):
        with tempfile.TemporaryDirectory() as tmp_a, \
             tempfile.TemporaryDirectory() as tmp_b:
            stub, tn = Path(tmp_a), Path(tmp_b)
            base = dt.datetime.now(dt.timezone.utc).replace(tzinfo=None) \
                   - dt.timedelta(days=10)

            # Live trade on BOTH sides — Layer 3 verdict should compare these.
            write_pair(stub, "BTCUSDT", base, +5000.0)
            write_pair(tn,   "BTCUSDT", base, +5000.0)

            # Shadow trades ONLY on the stub side — production layout.
            # Three shadow strategies, multiple trades each, mirroring the
            # 1 live + 3 shadow per-engine fleet config from CLAUDE.md.
            for label in ("alt5-15-336", "alt5-15-504", "bb20"):
                shadow_dir = stub / "shadow" / label
                for i, sym in enumerate(("ETHUSDT", "SOLUSDT")):
                    ts = base + dt.timedelta(hours=i)
                    write_pair(shadow_dir, sym, ts, +1000.0 * (i + 1))

            code, out, err = run_wrapper(
                "--stub-dir", str(stub), "--testnet-dir", str(tn),
            )
            self.assertEqual(code, 0,
                f"Layer 3 must PASS when live matches, even with stub-side "
                f"shadows asymmetric to testnet — pre-fix this exited 2 "
                f"(SIGNAL_DIVERGENCE) blocking every legitimate STAGE_1 "
                f"promotion attempt.\nexit={code}\nstdout:\n{out}\nstderr:\n{err}")
            self.assertIn("PASS — Layer 3 criterion met", out)
            # Verify shadow trades did NOT leak into the report (would be
            # reported as Only-A trades pre-fix).
            self.assertNotIn("ETHUSDT", out,
                f"shadow ETHUSDT trade leaked into Layer 3 report — "
                f"default shadow-exclusion broken.\nout:\n{out}")
            self.assertNotIn("SOLUSDT", out,
                f"shadow SOLUSDT trade leaked into Layer 3 report — "
                f"default shadow-exclusion broken.\nout:\n{out}")


class JournalFieldsMatchWriterTest(unittest.TestCase):
    """L3-extension (2026-05-12): the writer-equals-fixture lens (L2-7) applied
    to the Layer 3 journal-schema contract.

    The wrapper reads `select(.event=="open") | .ts` from journal files; the
    test fixture (write_pair) generates JSON with hardcoded field names —
    event/ts/symbol/side/entry/exit/stop/target/outcome/pnl_usd/reason — and
    both are silently kept in sync with each other but NOT against the live
    writer (pkg/execution/stub.go's journalEntry struct + the canonical
    Event: "open"/"close" assignments in stub.go AND binance_live.go).

    If a future refactor renames any field tag (e.g. `event` → `event_type`)
    or any event value, write_pair() also uses the old name and the bash
    tests still pass — but in production the wrapper's jq filter sees zero
    matches against real journals, silently producing "no open events" → exit
    3 with a misleading "schema may have changed" message instead of an
    explicit drift alert.

    Pattern family: writer-equals-fixture drift, locked first in
    test_layer2_smoke.py::FixtureMsgVsEngineEmitTest. This is the second
    paired implementation of the pin. Adjacent to writer-equals-model
    (docs/AUDIT_LENS.md adjacent-pattern, 2026-05-10-pm)."""

    STRUCT_SOURCE = REPO / "pkg" / "execution" / "stub.go"
    EVENT_WRITERS = [
        REPO / "pkg" / "execution" / "stub.go",
        REPO / "pkg" / "execution" / "binance_live.go",
    ]

    # JSON field names referenced by EITHER write_pair() in this test file
    # OR the layer3_verdict.sh jq filter. Each MUST exist as `json:"FIELD"`
    # (or `json:"FIELD,omitempty"` etc.) in the journalEntry struct.
    REQUIRED_FIELDS = [
        "event",     # script jq + fixture (.event=="open")
        "symbol",    # fixture, journal_diff key
        "ts",        # script jq + fixture (.ts for oldest-open derivation)
        "side",      # fixture
        "entry",     # fixture
        "exit",      # fixture (close events)
        "stop",      # fixture
        "target",    # fixture
        "outcome",   # fixture (close events)
        "pnl_usd",   # fixture (drives the threshold diff)
        "reason",    # fixture
    ]

    @classmethod
    def setUpClass(cls):
        if not cls.STRUCT_SOURCE.exists():
            raise unittest.SkipTest(f"writer source missing: {cls.STRUCT_SOURCE}")
        cls.struct_text = cls.STRUCT_SOURCE.read_text(encoding="utf-8")
        cls.writer_texts = {}
        for p in cls.EVENT_WRITERS:
            if not p.exists():
                raise unittest.SkipTest(f"writer source missing: {p}")
            cls.writer_texts[p.name] = p.read_text(encoding="utf-8")
        cls.script_text = SCRIPT.read_text(encoding="utf-8")
        cls.test_text = Path(__file__).read_text(encoding="utf-8")

    def test_each_required_field_has_json_tag_in_writer(self):
        """Every field name used by write_pair() OR the layer3_verdict.sh jq
        filter must exist as a `json:"FIELD"` tag (or variant) in the
        journalEntry struct."""
        import re
        for field in self.REQUIRED_FIELDS:
            pattern = rf'json:"{re.escape(field)}(?:,|")'
            self.assertRegex(
                self.struct_text, pattern,
                f"field {field!r} used by test fixture or layer3_verdict.sh "
                f"but no `json:\"{field}\"` (or variant) tag found in "
                f"{self.STRUCT_SOURCE.name} — writer-equals-fixture drift; "
                f"the fixture stayed in sync with the script but the live "
                f"writer renamed/removed the tag. Lockstep update required.",
            )

    def test_script_jq_filter_references_event_field(self):
        """Confirm the script still reads the .event field. If a future refactor
        switches to `.type` etc., this assertion + REQUIRED_FIELDS must update
        in lockstep with the writer."""
        self.assertIn(
            'select(.event=="open")', self.script_text,
            "layer3_verdict.sh jq filter no longer reads .event — "
            "update REQUIRED_FIELDS + the writer in lockstep with the script.",
        )

    def test_event_value_open_is_emitted_by_every_writer(self):
        """The literal Event: "open" assignment must appear in EACH writer
        (Stub + BinanceLive). A rename in one but not both would surface
        downstream as SIGNAL_DIVERGENCE (exit 2) rather than as the actual
        cause (field-value drift), routing operators to the wrong diagnostic."""
        for name, text in self.writer_texts.items():
            self.assertRegex(
                text, r'Event:\s*"open"',
                f"{name} no longer assigns Event: \"open\" — the wrapper's "
                f"jq filter `select(.event==\"open\")` would silently match "
                f"zero rows from this writer's journal in production.",
            )

    def test_event_value_close_is_emitted_by_every_writer(self):
        """Close-event literal must appear in EACH writer too — journal_diff
        pairs open/close events by key; a rename of \"close\" breaks pairing
        and surfaces as SIGNAL_DIVERGENCE with no signal-divergent trades."""
        for name, text in self.writer_texts.items():
            self.assertRegex(
                text, r'Event:\s*"close"',
                f"{name} no longer assigns Event: \"close\" — close-event "
                f"pairing in journal_diff breaks silently against this "
                f"writer's journal.",
            )

    def test_fixture_event_values_present_in_fixture(self):
        """Belt-and-braces: the test fixture (write_pair) itself must still
        emit the canonical event values. Pinning here means a fixture-only
        rename (without updating the writer) also fires this test."""
        self.assertIn(
            '"event": "open"', self.test_text,
            "test fixture write_pair() no longer emits \"event\": \"open\" "
            "— rename in lockstep with the writer + script jq filter.",
        )
        self.assertIn(
            '"event": "close"', self.test_text,
            "test fixture write_pair() no longer emits \"event\": \"close\".",
        )


if __name__ == "__main__":
    unittest.main(verbosity=2)
