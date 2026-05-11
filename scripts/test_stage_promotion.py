#!/usr/bin/env python3
"""
Functional tests for stage_promotion.sh.

Drives the subcommand-per-phase orchestrator against a synthetic project
root so each phase can be exercised without touching the real VPS or
production results/ directory. Covers:

  - subcommand dispatch (no-args, unknown subcommand)
  - phase1: happy path, missing gate, gate not ALL_GREEN, newer kill,
            invalid transition, concurrent-promotion guard
  - phase2: prereq enforcement, operator-confirm gate
  - phase3: prereq enforcement, dry-run happy path
  - phase4: pending-data (exit 9), sufficient data + confirm (exit 0)
  - phase5: pending-data (window not elapsed), elapsed + confirm
  - phase6: finalizes artifact (.in-progress → final filename)
  - status: shows current phase progress
  - rollback: confirmation gate, dry-run happy path
  - state-out-of-order: phase invoked without prereq → exit 7

Implementation mirrors test_milestone2_launch.py: copy the script + the
shared lib/notify.sh into a temp ROOT, set up scripts/deploy/results
fixtures, invoke via subprocess. Use STAGE_PROMOTION_DRY_RUN=1 to skip
real deploy calls and STAGE_PROMOTION_LOCAL_JOURNAL for Phase 4 journal
fixtures.

Run:
  python3 scripts/test_stage_promotion.py
"""
from __future__ import annotations

import datetime as dt
import json
import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
SCRIPT_SRC = REPO / "scripts" / "stage_promotion.sh"
NOTIFY_LIB_SRC = REPO / "scripts" / "lib" / "notify.sh"


def scaffold_root(tmp: Path) -> Path:
    """Build a synthetic project root suitable for stage_promotion.sh.

    The script computes SCRIPT_DIR from BASH_SOURCE[0] and ROOT as its
    parent. Copying the script under <tmp>/scripts/ + faking everything
    else under <tmp>/ produces a fully self-contained sandbox.
    """
    (tmp / "scripts").mkdir()
    (tmp / "scripts" / "lib").mkdir()
    (tmp / "results").mkdir()
    (tmp / "deploy").mkdir()

    shutil.copy(SCRIPT_SRC, tmp / "scripts" / "stage_promotion.sh")
    (tmp / "scripts" / "stage_promotion.sh").chmod(0o755)

    # Real notify.sh — the script sources it and expects notify_telegram
    # to be defined. The function is a graceful no-op when Telegram env
    # vars are unset (which the test invocation guarantees).
    shutil.copy(NOTIFY_LIB_SRC, tmp / "scripts" / "lib" / "notify.sh")

    # Stub redeploy.sh + post_deploy_check.sh — invoked by phase3/rollback
    # via absolute path through ROOT. The script uses STAGE_PROMOTION_DRY_RUN=1
    # to skip these but having them present (and exit 0) makes a non-DRY
    # test path possible if we want it later.
    (tmp / "deploy" / "redeploy.sh").write_text("#!/usr/bin/env bash\nexit 0\n")
    (tmp / "deploy" / "redeploy.sh").chmod(0o755)
    (tmp / "scripts" / "post_deploy_check.sh").write_text("#!/usr/bin/env bash\nexit 0\n")
    (tmp / "scripts" / "post_deploy_check.sh").chmod(0o755)

    return tmp


def write_gate_doc(tmp: Path, *, kind: str = "paper",
                   composite: str = "ALL_GREEN",
                   date: str = "2026-09-05") -> Path:
    """Write a synthetic gate doc that phase1 will read.

    kind="paper" writes forward_paper_completion_review_<date>.md for the
    paper→STAGE_1 transition; kind="STAGE_1" writes a prior-stage promotion
    artifact for the STAGE_1→STAGE_2 transition.
    """
    if kind == "paper":
        path = tmp / "results" / f"forward_paper_completion_review_{date}.md"
    else:
        path = tmp / "results" / f"stage_promotion_{date}_paper_to_{kind}.md"
    path.write_text(
        f"# Gate document — {kind}\n"
        f"Composite verdict: {composite}\n"
        f"(test fixture)\n"
    )
    return path


def run_sp(tmp: Path, *args: str,
           env_extra: dict[str, str] | None = None) -> tuple[int, str, str]:
    cmd = ["bash", str(tmp / "scripts" / "stage_promotion.sh"), *args]
    env = os.environ.copy()
    # Strip Telegram secrets so the stubbed notifier never accidentally
    # POSTs to a real chat if the env-var-unset gate were missed.
    for k in ("TELEGRAM_BOT_TOKEN", "TELEGRAM_CHAT_ID"):
        env.pop(k, None)
    if env_extra:
        env.update(env_extra)
    result = subprocess.run(cmd, capture_output=True, text=True, env=env)
    return result.returncode, result.stdout, result.stderr


def in_progress_artifact(tmp: Path) -> Path | None:
    matches = list((tmp / "results").glob("stage_promotion_*.md.in-progress"))
    return matches[0] if len(matches) == 1 else None


# ── Tests ────────────────────────────────────────────────────────────────────

class DispatchTest(unittest.TestCase):
    """Subcommand-dispatch boundary: no-args + unknown command."""

    def test_no_args_exits_3(self):
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d))
            code, _, err = run_sp(tmp)
            self.assertEqual(code, 3, err)
            self.assertIn("Subcommands:", err)

    def test_unknown_subcommand_exits_3(self):
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d))
            code, _, err = run_sp(tmp, "nuke")
            self.assertEqual(code, 3, err)
            self.assertIn("unknown-subcommand", err)


class Phase1Test(unittest.TestCase):
    """Phase 1 GATE verification: green path + four failure shapes."""

    def test_phase1_happy_path(self):
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d))
            write_gate_doc(tmp, kind="paper", composite="ALL_GREEN")
            code, out, err = run_sp(tmp, "phase1", "paper", "STAGE_1")
            self.assertEqual(code, 0, err)
            self.assertIn("Phase 1 complete", out)
            artifact = in_progress_artifact(tmp)
            self.assertIsNotNone(artifact)
            content = artifact.read_text()
            self.assertIn("STATE: PHASE_1_COMPLETE", content)
            self.assertIn("paper → STAGE_1", content)
            self.assertIn("$100", content)

    def test_phase1_missing_gate_exits_2(self):
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d))
            code, _, err = run_sp(tmp, "phase1", "paper", "STAGE_1")
            self.assertEqual(code, 2, err)
            self.assertIn("phase1-gate-missing", err)

    def test_phase1_gate_not_green_exits_1(self):
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d))
            write_gate_doc(tmp, kind="paper", composite="AMBER")
            code, _, err = run_sp(tmp, "phase1", "paper", "STAGE_1")
            self.assertEqual(code, 1, err)
            self.assertIn("phase1-gate-not-green", err)

    def test_phase1_newer_kill_blocks(self):
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d))
            gate = write_gate_doc(tmp, kind="paper", composite="ALL_GREEN")
            # Make sure the kill artifact has a strictly-newer mtime than gate.
            kill_path = tmp / "results" / "kill_2026-09-08_real_drift.md"
            kill_path.write_text("# HARD KILL\n")
            # Set kill mtime to gate's mtime + 1 day
            gate_mtime = gate.stat().st_mtime
            os.utime(kill_path, (gate_mtime + 86400, gate_mtime + 86400))
            code, _, err = run_sp(tmp, "phase1", "paper", "STAGE_1")
            self.assertEqual(code, 1, err)
            self.assertIn("phase1-newer-kill", err)

    def test_phase1_invalid_transition_exits_3(self):
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d))
            code, _, err = run_sp(tmp, "phase1", "paper", "STAGE_3")
            self.assertEqual(code, 3, err)
            self.assertIn("invalid-transition", err)

    def test_phase1_invalid_to_stage_exits_3(self):
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d))
            code, _, err = run_sp(tmp, "phase1", "paper", "STAGE_99")
            self.assertEqual(code, 3, err)

    def test_phase1_concurrent_promotion_blocked(self):
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d))
            # Plant an existing in-progress artifact
            existing = tmp / "results" / "stage_promotion_2026-08-01_paper_to_STAGE_1.md.in-progress"
            existing.write_text("# pretend prior promotion\nSTATE: PHASE_1_COMPLETE at 2026-08-01T00:00:00Z\n")
            write_gate_doc(tmp, kind="paper", composite="ALL_GREEN")
            code, _, err = run_sp(tmp, "phase1", "paper", "STAGE_1")
            self.assertEqual(code, 7, err)
            self.assertIn("concurrent-promotion-blocked", err)

    def test_phase1_rule_doc_doesnt_false_positive(self):
        """The locked decision-rule doc for forward_paper_completion_review
        contains the literal 'Composite verdict: ALL_GREEN' inside its template
        scaffold (describing what an ALL_GREEN verdict looks like). Without
        an explicit exclude for *_decision_rule_*/*_template_*/*_verdict_*
        filenames, phase1 would false-positive against the rule and create a
        spurious in-progress artifact in production. Caught 2026-05-10 during
        local smoke — this test pins the fix against regression.
        """
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d))
            # Plant ONLY a decision-rule doc with the ALL_GREEN literal — no
            # actual verdict artifact. phase1 must refuse.
            rule_doc = tmp / "results" / "forward_paper_completion_review_decision_rule_2026-05-08.md"
            rule_doc.write_text(
                "# Forward-paper completion review (locked rule)\n"
                "\n"
                "Template scaffold below — when the review fires, the operator writes:\n"
                "```\n"
                "Composite verdict: ALL_GREEN\n"
                "```\n"
                "This file is the RULE not the VERDICT.\n"
            )
            code, _, err = run_sp(tmp, "phase1", "paper", "STAGE_1")
            self.assertEqual(code, 2, err)
            self.assertIn("phase1-gate-missing", err)


class Phase2Test(unittest.TestCase):
    """Phase 2 CONFIG: prereq + operator-confirm gate."""

    def _setup_after_phase1(self, tmp: Path) -> None:
        write_gate_doc(tmp, kind="paper")
        code, _, err = run_sp(tmp, "phase1", "paper", "STAGE_1")
        self.assertEqual(code, 0, err)

    def test_phase2_without_phase1_exits_7(self):
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d))
            code, _, err = run_sp(tmp, "phase2")
            # No artifact exists at all
            self.assertEqual(code, 7, err)
            self.assertIn("no-active-promotion", err)

    def test_phase2_without_confirm_exits_6(self):
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d))
            self._setup_after_phase1(tmp)
            code, _, err = run_sp(tmp, "phase2")
            self.assertEqual(code, 6, err)
            self.assertIn("phase2-awaiting-confirm", err)

    def test_phase2_with_confirm_completes(self):
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d))
            self._setup_after_phase1(tmp)
            code, out, err = run_sp(tmp, "phase2", env_extra={"STAGE_PROMOTION_CONFIRM": "YES", "STAGE_PROMOTION_DRY_RUN": "1"})
            self.assertEqual(code, 0, err)
            self.assertIn("Phase 2 complete", out)
            content = in_progress_artifact(tmp).read_text()
            self.assertIn("STATE: PHASE_2_COMPLETE", content)


class Phase3Test(unittest.TestCase):
    """Phase 3 DEPLOY: prereq + DRY_RUN happy path."""

    def _setup_after_phase2(self, tmp: Path) -> None:
        write_gate_doc(tmp, kind="paper")
        run_sp(tmp, "phase1", "paper", "STAGE_1")
        run_sp(tmp, "phase2", env_extra={"STAGE_PROMOTION_CONFIRM": "YES", "STAGE_PROMOTION_DRY_RUN": "1"})

    def test_phase3_without_phase2_exits_7(self):
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d))
            write_gate_doc(tmp, kind="paper")
            run_sp(tmp, "phase1", "paper", "STAGE_1")
            code, _, err = run_sp(tmp, "phase3", env_extra={"STAGE_PROMOTION_DRY_RUN": "1"})
            self.assertEqual(code, 7, err)

    def test_phase3_dry_run_happy_path(self):
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d))
            self._setup_after_phase2(tmp)
            code, out, err = run_sp(tmp, "phase3", env_extra={"STAGE_PROMOTION_DRY_RUN": "1"})
            self.assertEqual(code, 0, err)
            self.assertIn("Phase 3 complete", out)
            content = in_progress_artifact(tmp).read_text()
            self.assertIn("STATE: PHASE_3_COMPLETE", content)
            self.assertIn("DEPLOY_TIMESTAMP:", content)


class Phase4Test(unittest.TestCase):
    """Phase 4 first-N-trade: pending vs sufficient data + confirm gate."""

    def _setup_after_phase3(self, tmp: Path) -> Path:
        """Returns the path of an empty journal dir prepared for tests."""
        write_gate_doc(tmp, kind="paper")
        run_sp(tmp, "phase1", "paper", "STAGE_1")
        run_sp(tmp, "phase2", env_extra={"STAGE_PROMOTION_CONFIRM": "YES", "STAGE_PROMOTION_DRY_RUN": "1"})
        run_sp(tmp, "phase3", env_extra={"STAGE_PROMOTION_DRY_RUN": "1"})
        journal_dir = tmp / "journal"
        journal_dir.mkdir()
        return journal_dir

    def _write_close(self, journal_dir: Path, *, symbol: str, ts: str) -> None:
        path = journal_dir / f"{symbol}-2026-09.jsonl"
        entry = {"event": "close", "symbol": symbol, "ts": ts, "outcome": "TARGET"}
        with path.open("a") as f:
            f.write(json.dumps(entry) + "\n")

    def test_phase4_zero_closes_exits_9(self):
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d))
            jd = self._setup_after_phase3(tmp)
            code, out, _ = run_sp(tmp, "phase4",
                                  env_extra={"STAGE_PROMOTION_LOCAL_JOURNAL": str(jd)})
            self.assertEqual(code, 9, out)
            self.assertIn("Waiting on 3 more trades", out)

    def test_phase4_partial_closes_exits_9(self):
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d))
            jd = self._setup_after_phase3(tmp)
            future = (dt.datetime.now(dt.timezone.utc)
                      + dt.timedelta(hours=1)).strftime("%Y-%m-%dT%H:%M:%SZ")
            self._write_close(jd, symbol="BTCUSDT", ts=future)
            self._write_close(jd, symbol="ETHUSDT", ts=future)
            code, out, _ = run_sp(tmp, "phase4",
                                  env_extra={"STAGE_PROMOTION_LOCAL_JOURNAL": str(jd)})
            self.assertEqual(code, 9, out)
            self.assertIn("Waiting on 1 more trades", out)

    def test_phase4_sufficient_closes_without_confirm_exits_6(self):
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d))
            jd = self._setup_after_phase3(tmp)
            future = (dt.datetime.now(dt.timezone.utc)
                      + dt.timedelta(hours=1)).strftime("%Y-%m-%dT%H:%M:%SZ")
            for sym in ("BTCUSDT", "ETHUSDT", "SOLUSDT"):
                self._write_close(jd, symbol=sym, ts=future)
            code, _, err = run_sp(tmp, "phase4",
                                  env_extra={"STAGE_PROMOTION_LOCAL_JOURNAL": str(jd)})
            self.assertEqual(code, 6, err)
            self.assertIn("phase4-awaiting-confirm", err)

    def test_phase4_sufficient_closes_with_confirm_completes(self):
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d))
            jd = self._setup_after_phase3(tmp)
            future = (dt.datetime.now(dt.timezone.utc)
                      + dt.timedelta(hours=1)).strftime("%Y-%m-%dT%H:%M:%SZ")
            for sym in ("BTCUSDT", "ETHUSDT", "SOLUSDT"):
                self._write_close(jd, symbol=sym, ts=future)
            code, out, err = run_sp(tmp, "phase4", env_extra={
                "STAGE_PROMOTION_LOCAL_JOURNAL": str(jd),
                "STAGE_PROMOTION_CONFIRM": "YES",
            })
            self.assertEqual(code, 0, err)
            self.assertIn("Phase 4 complete", out)


class Phase5Test(unittest.TestCase):
    """Phase 5 monitoring: window pending vs elapsed + confirm gate."""

    def _setup_after_phase4(self, tmp: Path, deploy_hours_ago: int) -> None:
        write_gate_doc(tmp, kind="paper")
        run_sp(tmp, "phase1", "paper", "STAGE_1")
        run_sp(tmp, "phase2", env_extra={"STAGE_PROMOTION_CONFIRM": "YES", "STAGE_PROMOTION_DRY_RUN": "1"})
        run_sp(tmp, "phase3", env_extra={"STAGE_PROMOTION_DRY_RUN": "1"})
        # Rewrite DEPLOY_TIMESTAMP to be N hours ago — phase5 reads from artifact.
        artifact = in_progress_artifact(tmp)
        old_ts = (dt.datetime.now(dt.timezone.utc)
                  - dt.timedelta(hours=deploy_hours_ago)).strftime("%Y-%m-%dT%H:%M:%SZ")
        content = artifact.read_text()
        # Replace the existing DEPLOY_TIMESTAMP line.
        import re
        content = re.sub(r"DEPLOY_TIMESTAMP: \S+", f"DEPLOY_TIMESTAMP: {old_ts}", content)
        artifact.write_text(content)
        # Phase 4 needs to complete — easiest path: write fixture closes
        # immediately after the rewritten deploy ts.
        journal_dir = tmp / "journal"
        journal_dir.mkdir()
        for sym in ("BTCUSDT", "ETHUSDT", "SOLUSDT"):
            (journal_dir / f"{sym}-2026-09.jsonl").write_text(
                json.dumps({"event": "close", "symbol": sym, "ts": old_ts,
                            "outcome": "TARGET"}) + "\n"
            )
        run_sp(tmp, "phase4", env_extra={
            "STAGE_PROMOTION_LOCAL_JOURNAL": str(journal_dir),
            "STAGE_PROMOTION_CONFIRM": "YES",
        })

    def test_phase5_window_not_elapsed_exits_9(self):
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d))
            self._setup_after_phase4(tmp, deploy_hours_ago=2)  # STAGE_1 wants 24h
            code, out, _ = run_sp(tmp, "phase5")
            self.assertEqual(code, 9, out)
            self.assertIn("remaining in monitoring window", out)

    def test_phase5_window_elapsed_without_confirm_exits_6(self):
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d))
            self._setup_after_phase4(tmp, deploy_hours_ago=25)
            code, _, err = run_sp(tmp, "phase5")
            self.assertEqual(code, 6, err)
            self.assertIn("phase5-awaiting-confirm", err)

    def test_phase5_window_elapsed_with_confirm_completes(self):
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d))
            self._setup_after_phase4(tmp, deploy_hours_ago=25)
            code, out, err = run_sp(tmp, "phase5", env_extra={"STAGE_PROMOTION_CONFIRM": "YES", "STAGE_PROMOTION_DRY_RUN": "1"})
            self.assertEqual(code, 0, err)
            self.assertIn("Phase 5 complete", out)


class Phase6Test(unittest.TestCase):
    """Phase 6 finalize: .in-progress → final filename + closure skeleton."""

    def test_phase6_finalizes_artifact(self):
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d))
            write_gate_doc(tmp, kind="paper")
            run_sp(tmp, "phase1", "paper", "STAGE_1")
            run_sp(tmp, "phase2", env_extra={"STAGE_PROMOTION_CONFIRM": "YES", "STAGE_PROMOTION_DRY_RUN": "1"})
            run_sp(tmp, "phase3", env_extra={"STAGE_PROMOTION_DRY_RUN": "1"})
            # Walk back deploy timestamp + add closes so phase4/5 can complete.
            artifact = in_progress_artifact(tmp)
            old_ts = (dt.datetime.now(dt.timezone.utc)
                      - dt.timedelta(hours=25)).strftime("%Y-%m-%dT%H:%M:%SZ")
            content = artifact.read_text()
            import re
            content = re.sub(r"DEPLOY_TIMESTAMP: \S+", f"DEPLOY_TIMESTAMP: {old_ts}", content)
            artifact.write_text(content)
            journal_dir = tmp / "journal"
            journal_dir.mkdir()
            for sym in ("BTCUSDT", "ETHUSDT", "SOLUSDT"):
                (journal_dir / f"{sym}-2026-09.jsonl").write_text(
                    json.dumps({"event": "close", "symbol": sym, "ts": old_ts,
                                "outcome": "TARGET"}) + "\n"
                )
            run_sp(tmp, "phase4", env_extra={
                "STAGE_PROMOTION_LOCAL_JOURNAL": str(journal_dir),
                "STAGE_PROMOTION_CONFIRM": "YES",
            })
            run_sp(tmp, "phase5", env_extra={"STAGE_PROMOTION_CONFIRM": "YES", "STAGE_PROMOTION_DRY_RUN": "1"})
            code, out, err = run_sp(tmp, "phase6")
            self.assertEqual(code, 0, err)
            self.assertIn("Phase 6 complete", out)
            # .in-progress is gone, final exists
            self.assertIsNone(in_progress_artifact(tmp))
            finals = list((tmp / "results").glob("stage_promotion_*_paper_to_STAGE_1.md"))
            self.assertEqual(len(finals), 1)
            content = finals[0].read_text()
            self.assertIn("STATE: PHASE_6_COMPLETE", content)
            # Closure template skeleton is included
            self.assertIn("Risk acceptance ledger", content)
            self.assertIn("REST polling lag", content)


class StatusTest(unittest.TestCase):
    """Status command outputs current phase progress."""

    def test_status_after_phase1(self):
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d))
            write_gate_doc(tmp, kind="paper")
            run_sp(tmp, "phase1", "paper", "STAGE_1")
            code, out, _ = run_sp(tmp, "status")
            self.assertEqual(code, 0, out)
            self.assertIn("✓ phase1 at", out)
            self.assertIn("· phase2 pending", out)


class RollbackTest(unittest.TestCase):
    """Rollback: confirm gate + DRY_RUN happy path."""

    def _setup_promotion_in_flight(self, tmp: Path) -> None:
        write_gate_doc(tmp, kind="paper")
        run_sp(tmp, "phase1", "paper", "STAGE_1")
        run_sp(tmp, "phase2", env_extra={"STAGE_PROMOTION_CONFIRM": "YES", "STAGE_PROMOTION_DRY_RUN": "1"})
        run_sp(tmp, "phase3", env_extra={"STAGE_PROMOTION_DRY_RUN": "1"})

    def test_rollback_without_confirm_exits_6(self):
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d))
            self._setup_promotion_in_flight(tmp)
            code, _, err = run_sp(tmp, "rollback")
            self.assertEqual(code, 6, err)
            self.assertIn("rollback-awaiting-confirm", err)

    def test_rollback_with_confirm_exits_8(self):
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d))
            self._setup_promotion_in_flight(tmp)
            code, out, err = run_sp(tmp, "rollback", env_extra={
                "STAGE_PROMOTION_CONFIRM": "YES",
                "STAGE_PROMOTION_DRY_RUN": "1",
            })
            self.assertEqual(code, 8, err)
            self.assertIn("Rollback complete", out)
            content = in_progress_artifact(tmp).read_text()
            self.assertIn("STATE: ROLLBACK_COMPLETE", content)


class VpsTargetConsistencyTest(unittest.TestCase):
    """Pin the STAGE_PROMOTION_VPS override is honored consistently across
    every ssh-invocation path. Pre-fix, phase3's STAGE_4 halt block
    hardcoded `root@178.105.24.230` while phase2/phase4 honored
    STAGE_PROMOTION_VPS — an operator setting the override would silently
    hit production for the STAGE_4 halt. Track 7 audit 2026-05-11."""

    def test_no_hardcoded_ssh_target_remains(self):
        """Source-scan: any line invoking `ssh ...root@178.105.24.230`
        (whitespace-separated target) is a fail-open. Legitimate uses
        of the IP must go through `${STAGE_PROMOTION_VPS:-root@...}`
        expansion (preceded by `:-`, not whitespace)."""
        import re
        source = SCRIPT_SRC.read_text()
        # Match "ssh", then any non-newline chars, then whitespace,
        # then the literal IP. Excludes `:-root@178.105.24.230` because
        # that's preceded by `:-`, not whitespace.
        pattern = re.compile(r"ssh[^\n]*\sroot@178\.105\.24\.230")
        hits = []
        for i, line in enumerate(source.splitlines(), 1):
            if pattern.search(line):
                hits.append((i, line))
        # Filter out lines inside heredoc instruction text — those are
        # docs the operator sees, not invocations. A heredoc with
        # ${STAGE_PROMOTION_VPS:-root@...} expands correctly.
        real_invocations = [
            (i, l) for i, l in hits
            if ":-root@178.105.24.230" not in l
        ]
        self.assertEqual(
            real_invocations, [],
            f"Hardcoded ssh-target found (should use "
            f"$target / ${{STAGE_PROMOTION_VPS:-root@...}}): {real_invocations}")

    def test_phase3_stage4_halt_shows_target_in_dry_run(self):
        """Positive test: with STAGE_PROMOTION_VPS=root@alt-host and
        --phase3 (DRY_RUN, TO=STAGE_4), the script's stdout includes
        `target: root@alt-host` proving the override propagated."""
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d))
            # Set up an in-progress STAGE_3 → STAGE_4 promotion with
            # phases 1 + 2 complete.
            artifact = tmp / "results" / "stage_promotion_2026-09-05_STAGE_3_to_STAGE_4.md.in-progress"
            artifact.write_text(
                "# STAGE promotion — STAGE_3 → STAGE_4\n"
                "STATE: PHASE_1_COMPLETE at 2026-09-05T00:00:00Z\n"
                "STATE: PHASE_2_COMPLETE at 2026-09-05T00:01:00Z\n"
            )
            code, out, err = run_sp(tmp, "phase3", env_extra={
                "STAGE_PROMOTION_DRY_RUN": "1",
                "STAGE_PROMOTION_CONFIRM": "YES",
                "STAGE_PROMOTION_VPS": "root@alt-host-for-test",
            })
            # phase3 may exit 0 (full success) or some other code if the
            # dry-run path doesn't fully succeed — the contract here is
            # just that the target was used during the STAGE_4 halt block.
            self.assertIn("target: root@alt-host-for-test", out,
                f"phase3 STAGE_4 halt did not honor STAGE_PROMOTION_VPS — "
                f"saw output: {out[:500]}\nstderr: {err[:500]}")


class Phase4SshFailureTest(unittest.TestCase):
    """Pin the SSH_FAILURE sentinel routing — pre-fix, ssh failure in
    count_post_deploy_closes aborted the script ungracefully (no
    Telegram, no clean exit code). Post-fix, routes to exit 5
    (PHASE_RUNTIME_ERROR) with a clear diagnostic."""

    def test_phase4_ssh_failure_exits_5(self):
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d))
            # Set up phases 1-3 complete with DEPLOY_TIMESTAMP.
            past = (dt.datetime.now(dt.timezone.utc) - dt.timedelta(days=1)
                    ).strftime("%Y-%m-%dT%H:%M:%SZ")
            artifact = tmp / "results" / "stage_promotion_2026-09-05_paper_to_STAGE_1.md.in-progress"
            artifact.write_text(
                "# STAGE promotion — paper → STAGE_1\n"
                "STATE: PHASE_1_COMPLETE at 2026-09-05T00:00:00Z\n"
                "STATE: PHASE_2_COMPLETE at 2026-09-05T00:01:00Z\n"
                f"DEPLOY_TIMESTAMP: {past}\n"
                "STATE: PHASE_3_COMPLETE at 2026-09-05T00:02:00Z\n"
            )
            # Inject a fake ssh in PATH that exits non-zero (simulates
            # connection failure, auth, etc.).
            fake_bin = tmp / "fake_bin"
            fake_bin.mkdir()
            (fake_bin / "ssh").write_text(
                "#!/usr/bin/env bash\n"
                "echo 'ssh: connect to host failed' >&2\n"
                "exit 255\n"
            )
            (fake_bin / "ssh").chmod(0o755)
            env_extra = {
                "PATH": f"{fake_bin}:{os.environ['PATH']}",
                # Don't set STAGE_PROMOTION_LOCAL_JOURNAL — force the
                # remote ssh branch.
            }
            code, out, err = run_sp(tmp, "phase4", env_extra=env_extra)
            self.assertEqual(code, 5,
                f"ssh-failure must route to exit 5 (PHASE_RUNTIME_ERROR), "
                f"got {code}\nstdout: {out[:300]}\nstderr: {err[:300]}")
            self.assertIn("ssh to VPS failed", err)
            self.assertIn("rc=255", err)


if __name__ == "__main__":
    unittest.main(verbosity=2)
