#!/usr/bin/env python3
"""
Tests for forward_paper_resolution.py.

Lens-applied design: every external-state read paired with a deliberate
failure-mode test. Covers all 7 rules + 5 input-error shapes.

Run:
  python3 scripts/test_forward_paper_resolution.py
"""
from __future__ import annotations

import datetime as dt
import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
SCRIPT = REPO / "scripts" / "forward_paper_resolution.py"

sys.path.insert(0, str(REPO / "scripts"))
import forward_paper_resolution as fpr  # noqa: E402


# ── snapshot helpers ─────────────────────────────────────────────────────────


SNAPSHOT_TEMPLATE = """\
══════════════════════════════════════════════════════════════════════════════
  Forward-paper go/no-go status — {date} 09:00 UTC
  Source: root@178.105.24.230  (/var/log/paper-live/journal)
══════════════════════════════════════════════════════════════════════════════

  ── live ───────────────────────────────────────────────────────────────────
    Days elapsed:        {days:4d} / 60              [IN-PROGRESS]
    Trades closed:       {trades:4d} / 150              [IN-PROGRESS]
    Wins / WR:           {wins:4d} / {wr_pct:.1f}%             [PENDING]
    Net PnL:             ${pnl}                     [INSUFFICIENT]
    Realized fee bps:    {fee:.2f}   / ≤12bp                [PASS]
    Realized slip bps:   {slip:.2f}    / ≤25bp (losers)       [PASS]
    Single-sym pct:      {single:.1f}% (XLMUSDT)        [INSUFFICIENT]
    First trade:         2026-05-08T08:31:59
    Last trade:          2026-05-08T08:31:59
    >>> VERDICT: WAITING (insufficient data)
"""


def write_snapshot(snap_dir: Path, date: str, *,
                   days: int = 0, trades: int = 0, wins: int = 0,
                   wr_pct: float = 0.0, pnl: int = 0,
                   fee: float = 10.0, slip: float = 5.0,
                   single: float = 0.0) -> Path:
    snap_dir.mkdir(parents=True, exist_ok=True)
    p = snap_dir / f"{date}.txt"
    p.write_text(SNAPSHOT_TEMPLATE.format(
        date=date, days=days, trades=trades, wins=wins, wr_pct=wr_pct,
        pnl=f"{pnl:+d}", fee=fee, slip=slip, single=single,
    ))
    return p


def write_drift_history(history_path: Path, *, exit_code: int = 0,
                        ts: str | None = None) -> None:
    history_path.parent.mkdir(parents=True, exist_ok=True)
    if ts is None:
        ts = dt.datetime.now(dt.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    history_path.write_text(json.dumps({
        "ts": ts, "exit_code": exit_code,
    }) + "\n")


def today_str() -> str:
    return dt.datetime.now(dt.timezone.utc).strftime("%Y-%m-%d")


def days_ago(n: int) -> str:
    return (dt.datetime.now(dt.timezone.utc) - dt.timedelta(days=n)).strftime("%Y-%m-%d")


def _build_inputs(tmp: Path, **kwargs) -> fpr.Inputs:
    """Build a fully-populated Inputs for direct rule evaluation,
    bypassing argparse + subprocess. Defaults keep the rule tree
    on the CONTINUE branch unless individual fields are overridden."""
    snap = write_snapshot(
        tmp / "snapshots", kwargs.get("date", today_str()),
        days=kwargs.get("days", 5),
        trades=kwargs.get("trades", 100),
        wins=kwargs.get("wins", 25),
        wr_pct=kwargs.get("wr_pct", 25.0),
        pnl=kwargs.get("pnl", 1000),
        fee=kwargs.get("fee", 10.0),
        slip=kwargs.get("slip", 5.0),
        single=kwargs.get("single", 20.0),
    )
    return fpr.Inputs(
        snapshot_path=snap,
        snapshot_date=snap.stem,
        snapshot_age_days=kwargs.get("snap_age", 0),
        live_n_trades=kwargs.get("trades", 100),
        live_n_days=kwargs.get("days", 5),
        live_pnl_usd=float(kwargs.get("pnl", 1000)),
        live_wr_pct=kwargs.get("wr_pct", 25.0),
        realized_fee_bps=kwargs.get("fee", 10.0),
        realized_slip_bps=kwargs.get("slip", 5.0),
        single_sym_pct=kwargs.get("single", 20.0),
        hodl_delta_usd=None,
        drift_last_exit=kwargs.get("drift_last_exit", 0),
        drift_last_ts=kwargs.get("drift_last_ts", today_str()),
        drift_age_days=kwargs.get("drift_age_days", 0),
        kill_check_exit=kwargs.get("kill_check_exit", 0),
        promote_check_exit=kwargs.get("promote_check_exit", 2),
    )


# ── rule unit tests ──────────────────────────────────────────────────────────


class Rule1KillTest(unittest.TestCase):

    def test_drift_exit_4_fires_kill(self):
        with tempfile.TemporaryDirectory() as tmp:
            inp = _build_inputs(Path(tmp), drift_last_exit=4)
            v = fpr.evaluate(inp)
            self.assertEqual(v.code, 4)
            self.assertEqual(v.label, "KILL")
            self.assertEqual(v.rule, "Rule 1")
            self.assertTrue(any("drift wrapper exit 4" in r for r in v.reasons))

    def test_kill_check_exit_1_fires_kill(self):
        with tempfile.TemporaryDirectory() as tmp:
            inp = _build_inputs(Path(tmp), kill_check_exit=1)
            v = fpr.evaluate(inp)
            self.assertEqual(v.code, 4)
            self.assertEqual(v.label, "KILL")

    def test_single_sym_kill_delegated_to_kill_protocol_check(self):
        # The single_sym >= 50% locked criterion is captured via
        # kill_protocol_check.py exit 1 (NOT inline in this script) so the
        # n_trades floor that kill_protocol_check applies internally is
        # respected. Without that delegation, n=1 (single trade = 100% of
        # pnl) would fire a spurious KILL. Self-audit catch 2026-05-10:
        # production smoke run with the redundant inline check fired
        # KILL on the n=1 paper data, validating the delegation choice.
        with tempfile.TemporaryDirectory() as tmp:
            inp = _build_inputs(Path(tmp), trades=1, single=100.0,
                                kill_check_exit=0)  # kill_protocol_check
            v = fpr.evaluate(inp)
            # MUST NOT fire KILL despite single_sym=100% (n=1 below kill_check's floor)
            self.assertNotEqual(v.label, "KILL",
                "single_sym=100% at n=1 must NOT fire KILL — kill_protocol_check "
                "owns the gate and applies n_trades floor internally")
            # Should hit Rule 3 (n<50 → CONTINUE)
            self.assertEqual(v.label, "CONTINUE")
            self.assertEqual(v.rule, "Rule 3")

        # And when kill_protocol_check DOES fire (sufficient n + breach), KILL.
        with tempfile.TemporaryDirectory() as tmp:
            inp = _build_inputs(Path(tmp), trades=200, single=55.0,
                                kill_check_exit=1)
            v = fpr.evaluate(inp)
            self.assertEqual(v.label, "KILL")
            self.assertTrue(any("kill_protocol_check fires" in r for r in v.reasons))


class Rule2OperatorReviewTest(unittest.TestCase):

    def test_drift_history_missing_review(self):
        with tempfile.TemporaryDirectory() as tmp:
            inp = _build_inputs(Path(tmp), drift_age_days=None,
                                drift_last_exit=None, drift_last_ts=None)
            v = fpr.evaluate(inp)
            self.assertEqual(v.code, 3)
            self.assertEqual(v.label, "OPERATOR_REVIEW")
            self.assertTrue(any("monitoring is DARK" in r for r in v.reasons))

    def test_drift_history_stale_review(self):
        with tempfile.TemporaryDirectory() as tmp:
            inp = _build_inputs(Path(tmp), drift_age_days=20)
            v = fpr.evaluate(inp)
            self.assertEqual(v.code, 3)
            self.assertEqual(v.label, "OPERATOR_REVIEW")
            self.assertTrue(any("cron may be dead" in r for r in v.reasons))

    def test_snapshot_stale_review(self):
        with tempfile.TemporaryDirectory() as tmp:
            inp = _build_inputs(Path(tmp), snap_age=15)
            v = fpr.evaluate(inp)
            self.assertEqual(v.code, 3)
            self.assertEqual(v.label, "OPERATOR_REVIEW")
            self.assertTrue(any("snapshot 15d old" in r for r in v.reasons))

    def test_kill_check_exit_3_review(self):
        with tempfile.TemporaryDirectory() as tmp:
            inp = _build_inputs(Path(tmp), kill_check_exit=3)
            v = fpr.evaluate(inp)
            self.assertEqual(v.code, 3)
            self.assertEqual(v.label, "OPERATOR_REVIEW")
            self.assertTrue(any("sibling decision script error" in r for r in v.reasons))


class Rule3InsufficientDataTest(unittest.TestCase):

    def test_n_trades_below_floor_continue(self):
        # The n=9 emotional case from 2026-05-10. Rule 3 mandates CONTINUE.
        with tempfile.TemporaryDirectory() as tmp:
            inp = _build_inputs(Path(tmp), trades=9, days=2, pnl=-1306)
            v = fpr.evaluate(inp)
            self.assertEqual(v.code, 0)
            self.assertEqual(v.label, "CONTINUE")
            self.assertEqual(v.rule, "Rule 3")

    def test_n_trades_at_floor_advances_past_rule3(self):
        with tempfile.TemporaryDirectory() as tmp:
            inp = _build_inputs(Path(tmp), trades=50)
            v = fpr.evaluate(inp)
            # At the floor, Rule 3 doesn't fire — advances to later rules.
            self.assertNotEqual(v.rule, "Rule 3")


class Rule4PromoteTest(unittest.TestCase):

    def test_all_gates_promote(self):
        with tempfile.TemporaryDirectory() as tmp:
            inp = _build_inputs(Path(tmp), trades=200, days=70, wr_pct=22.0,
                                pnl=15000, fee=10.0, slip=5.0, single=15.0,
                                drift_last_exit=0,
                                kill_check_exit=0, promote_check_exit=0)
            v = fpr.evaluate(inp)
            self.assertEqual(v.code, 1)
            self.assertEqual(v.label, "PROMOTE")
            self.assertEqual(v.rule, "Rule 4")


class Rule5WatchTest(unittest.TestCase):

    def test_drift_single_firing_watch(self):
        with tempfile.TemporaryDirectory() as tmp:
            inp = _build_inputs(Path(tmp), trades=70, drift_last_exit=1)
            v = fpr.evaluate(inp)
            self.assertEqual(v.code, 2)
            self.assertEqual(v.label, "WATCH")

    def test_fee_in_slack_window_watch(self):
        with tempfile.TemporaryDirectory() as tmp:
            inp = _build_inputs(Path(tmp), trades=70, fee=11.5)
            v = fpr.evaluate(inp)
            self.assertEqual(v.label, "WATCH")
            self.assertTrue(any("fee 11.50bp" in r for r in v.reasons))


class Rule6OperatorReviewAmbiguousTest(unittest.TestCase):

    def test_three_soft_signals_review(self):
        with tempfile.TemporaryDirectory() as tmp:
            inp = _build_inputs(Path(tmp), trades=70, days=35,
                                drift_last_exit=1, fee=11.5, slip=23.0,
                                pnl=-500)
            v = fpr.evaluate(inp)
            self.assertEqual(v.code, 3)
            self.assertEqual(v.label, "OPERATOR_REVIEW")
            self.assertTrue(any("3+ soft signals" in r for r in v.reasons))

    def test_low_trade_rate_review(self):
        # n_days > 90 with n_trades < 100: well below 1.18/day fleet rate.
        with tempfile.TemporaryDirectory() as tmp:
            inp = _build_inputs(Path(tmp), trades=80, days=100,
                                kill_check_exit=0, promote_check_exit=2)
            v = fpr.evaluate(inp)
            self.assertEqual(v.label, "OPERATOR_REVIEW")
            self.assertTrue(any("low trade rate" in r for r in v.reasons))

    def test_slow_bleed_review(self):
        # n_days >= 60 with positive PnL but < 30% of pro-rated.
        # Pro-rated at 60d = 69000 * 60/365 ≈ $11.3k. 30% = $3.4k.
        # PnL = $1500 < $3.4k → slow bleed.
        with tempfile.TemporaryDirectory() as tmp:
            inp = _build_inputs(Path(tmp), trades=120, days=60, pnl=1500,
                                kill_check_exit=0, promote_check_exit=1)
            v = fpr.evaluate(inp)
            self.assertEqual(v.label, "OPERATOR_REVIEW")
            self.assertTrue(any("slow-bleed" in r for r in v.reasons))


class Rule7DefaultContinueTest(unittest.TestCase):

    def test_clean_state_continues(self):
        with tempfile.TemporaryDirectory() as tmp:
            inp = _build_inputs(Path(tmp), trades=80, days=20, pnl=2000,
                                fee=10.0, slip=5.0, single=20.0,
                                drift_last_exit=0,
                                kill_check_exit=0, promote_check_exit=2)
            v = fpr.evaluate(inp)
            self.assertEqual(v.code, 0)
            self.assertEqual(v.label, "CONTINUE")
            self.assertEqual(v.rule, "Rule 7")


# ── input-error CLI tests ────────────────────────────────────────────────────


def _run(args: list[str]) -> tuple[int, str, str]:
    """Run the script with controlled args. The test ALWAYS provides
    --kill-exit + --promote-exit overrides so subprocess invocation of
    sibling scripts is bypassed."""
    res = subprocess.run(
        ["python3", str(SCRIPT)] + args + ["--kill-exit", "0",
                                            "--promote-exit", "2"],
        capture_output=True, text=True,
    )
    return res.returncode, res.stdout, res.stderr


class InputErrorTest(unittest.TestCase):

    def test_missing_snapshot_dir_exits_5(self):
        with tempfile.TemporaryDirectory() as tmp:
            empty = Path(tmp) / "no-such-dir"
            history = Path(tmp) / "drift.jsonl"
            write_drift_history(history)
            code, out, err = _run([
                "--snapshot-dir", str(empty),
                "--drift-history", str(history),
            ])
            self.assertEqual(code, 5,
                f"missing snapshot dir must exit 5 INPUT_ERROR; got {code}\n{err}")
            self.assertIn("no snapshot found", err)

    def test_unparseable_snapshot_filename_exits_5(self):
        with tempfile.TemporaryDirectory() as tmp:
            sd = Path(tmp) / "snapshots"
            sd.mkdir()
            # Snapshot named with non-date format.
            (sd / "garbage.txt").write_text("nothing")
            history = Path(tmp) / "drift.jsonl"
            write_drift_history(history)
            code, out, err = _run([
                "--snapshot-dir", str(sd),
                "--drift-history", str(history),
            ])
            self.assertEqual(code, 5,
                f"unparseable snapshot date must exit 5; got {code}\n{err}")
            self.assertIn("does not parse as YYYY-MM-DD", err)

    def test_json_output_on_input_error(self):
        with tempfile.TemporaryDirectory() as tmp:
            empty = Path(tmp) / "no-such-dir"
            history = Path(tmp) / "drift.jsonl"
            write_drift_history(history)
            code, out, err = _run([
                "--snapshot-dir", str(empty),
                "--drift-history", str(history),
                "--json",
            ])
            self.assertEqual(code, 5)
            payload = json.loads(out)
            self.assertEqual(payload["verdict"], "INPUT_ERROR")
            self.assertEqual(payload["exit_code"], 5)


class CLISmokeTest(unittest.TestCase):
    """End-to-end runs that produce a valid verdict — proves the
    argparse + gather + evaluate + render pipeline composes."""

    def _setup(self, tmp: Path, *, trades=9, snap_age=0, drift_age=0):
        sd = tmp / "snapshots"
        write_snapshot(sd, days_ago(snap_age), trades=trades, days=2, pnl=-1306)
        history = tmp / "drift.jsonl"
        ts = (dt.datetime.now(dt.timezone.utc)
              - dt.timedelta(days=drift_age)).strftime("%Y-%m-%dT%H:%M:%SZ")
        write_drift_history(history, exit_code=0, ts=ts)
        return sd, history

    def test_n9_emotional_case_continues(self):
        # The end-to-end smoke that proves Rule 3 handles 2026-05-10's
        # n=9 reaction mechanically.
        with tempfile.TemporaryDirectory() as tmp:
            sd, history = self._setup(Path(tmp), trades=9)
            code, out, err = _run([
                "--snapshot-dir", str(sd),
                "--drift-history", str(history),
            ])
            self.assertEqual(code, 0,
                f"n=9 must CONTINUE per Rule 3; got {code}\n{out}\n{err}")
            self.assertIn("CONTINUE", out)
            self.assertIn("Rule 3", out)
            self.assertIn("n=9", out)

    def test_json_output_on_continue(self):
        with tempfile.TemporaryDirectory() as tmp:
            sd, history = self._setup(Path(tmp), trades=9)
            code, out, err = _run([
                "--snapshot-dir", str(sd),
                "--drift-history", str(history),
                "--json",
            ])
            self.assertEqual(code, 0)
            payload = json.loads(out)
            self.assertEqual(payload["verdict"], "CONTINUE")
            self.assertEqual(payload["exit_code"], 0)
            self.assertEqual(payload["rule"], "Rule 3")
            self.assertEqual(payload["live_n_trades"], 9)


class SiblingArgPropagationTest(unittest.TestCase):
    """Pin the T9 ssh-target-consistency fix: invoke_sibling must propagate
    --vps / --live-source / --live-dir to sibling subprocess calls so that
    a `forward_paper_resolution --live-source local` invocation doesn't
    silently ssh to the production VPS via unconfigured siblings.

    Pre-fix, the sibling subprocesses were invoked with bare
    `["python3", str(path)]` — no args propagated. The resolution
    rehearsal claimed --local but actually still hit the VPS for the
    kill_protocol and stage_promotion checks via subprocess.
    """

    def test_invoke_sibling_propagates_args(self):
        """Monkey-patch subprocess.run, verify sibling_args appears in cmd."""
        from unittest import mock
        captured: list = []
        def fake_run(*args, **kwargs):
            captured.append((args, kwargs))
            result = mock.Mock()
            result.returncode = 0
            return result
        # Pretend the sibling script exists.
        with mock.patch.object(fpr.subprocess, "run", side_effect=fake_run), \
             mock.patch.object(fpr.Path, "is_file", return_value=True):
            rc = fpr.invoke_sibling(
                "kill_protocol_check.py", None,
                sibling_args=["--live-source", "local",
                              "--vps", "root@test-vps",
                              "--live-dir", "/tmp/journal"],
            )
        self.assertEqual(rc, 0)
        self.assertEqual(len(captured), 1)
        cmd = captured[0][0][0]
        # cmd should be: ["python3", "<path>", "--live-source", "local",
        #                  "--vps", "root@test-vps", "--live-dir", "/tmp/journal"]
        self.assertIn("--live-source", cmd)
        self.assertIn("local", cmd)
        self.assertIn("--vps", cmd)
        self.assertIn("root@test-vps", cmd)
        self.assertIn("--live-dir", cmd)
        self.assertIn("/tmp/journal", cmd)

    def test_invoke_sibling_no_args_omits_flags(self):
        """When sibling_args is empty/None, the subprocess cmd is just
        ['python3', path] — siblings fall back to their own defaults."""
        from unittest import mock
        captured: list = []
        def fake_run(*args, **kwargs):
            captured.append((args, kwargs))
            result = mock.Mock()
            result.returncode = 0
            return result
        with mock.patch.object(fpr.subprocess, "run", side_effect=fake_run), \
             mock.patch.object(fpr.Path, "is_file", return_value=True):
            fpr.invoke_sibling("kill_protocol_check.py", None, sibling_args=[])
        cmd = captured[0][0][0]
        self.assertEqual(len(cmd), 2,
            f"empty sibling_args should yield bare ['python3', path], got {cmd}")
        self.assertEqual(cmd[0], "python3")

    def test_invoke_sibling_override_short_circuits(self):
        """When override is provided, sibling is NOT invoked at all —
        subprocess.run never called. Existing test contract preserved."""
        from unittest import mock
        with mock.patch.object(fpr.subprocess, "run") as mock_run:
            rc = fpr.invoke_sibling("kill_protocol_check.py", 1,
                                    sibling_args=["--live-source", "local"])
        self.assertEqual(rc, 1)
        mock_run.assert_not_called()


if __name__ == "__main__":
    unittest.main(verbosity=2)
