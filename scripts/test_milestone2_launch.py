#!/usr/bin/env python3
"""
Functional tests for milestone2_launch.sh.

Drives the orchestrator via subprocess with a synthetic project root so the
locked execution sequence + audit-lens compliance gates can be exercised
end-to-end before milestone-2 is actually live. Covers the five cases the
runbook (§audit-lens-compliance bullet 4) requires:

    missing data            → EXIT_ENV_OR_INPUT_ERROR (3)
    missing binary          → EXIT_ENV_OR_INPUT_ERROR (3)
    partial-completion resume → A2_VERDICT restored from phase-1 artifact
    override-without-reason → EXIT_ENV_OR_INPUT_ERROR (3)
    contradiction-mid-sequence → EXIT_CONTRADICTION_HALTED (5)

Plus the boundary cases the audit-pattern lens added:

    trigger-not-detected    → EXIT_TRIGGER_NOT_DETECTED (2)
    invalid resume point    → EXIT_ENV_OR_INPUT_ERROR (3)
    phase runner crash      → EXIT_PHASE_RUNTIME_ERROR (4)
    verdict missing post-runner → EXIT_VERDICT_INCONCLUSIVE (1)
    happy path (all ADOPT)  → EXIT_ALL_VERDICTS_WRITTEN (0)

Implementation: each test sets up an isolated temp ROOT with a copy of
milestone2_launch.sh + a stub notify.sh under `scripts/`, a stub
`bin/backtest`, sample data/funding CSVs, and per-phase stub runners. The
script's SCRIPT_DIR resolves to the temp scripts/ dir (so ROOT resolves to
the temp root); production source on disk is never invoked.

Run:
  python3 scripts/test_milestone2_launch.py
"""
from __future__ import annotations

import os
import shutil
import subprocess
import tempfile
import textwrap
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
SCRIPT_SRC = REPO / "scripts" / "milestone2_launch.sh"


def scaffold_root(tmp: Path, *,
                  with_binary: bool = True,
                  with_data: bool = True,
                  with_funding: bool = True,
                  phase_runners: dict[str, str] | None = None) -> Path:
    """Build a synthetic project root suitable for milestone2_launch.sh.

    The script computes SCRIPT_DIR from BASH_SOURCE[0] and ROOT as its
    parent. Copying the script under <tmp>/scripts/ + faking everything
    else under <tmp>/ produces a fully self-contained sandbox.
    """
    (tmp / "scripts").mkdir()
    (tmp / "scripts" / "lib").mkdir()
    (tmp / "results").mkdir()
    if with_data:
        (tmp / "data").mkdir()
        (tmp / "data" / "BTCUSDT-1m-2024-01.csv").write_text("ts,open,high,low,close,volume\n")
    if with_funding:
        (tmp / "data" / "funding").mkdir(parents=True, exist_ok=True)
        (tmp / "data" / "funding" / "BTCUSDT.csv").write_text("ts,rate\n")
    if with_binary:
        (tmp / "bin").mkdir()
        bp = tmp / "bin" / "backtest"
        bp.write_text("#!/usr/bin/env bash\nexit 0\n")
        bp.chmod(0o755)

    shutil.copy(SCRIPT_SRC, tmp / "scripts" / "milestone2_launch.sh")
    (tmp / "scripts" / "milestone2_launch.sh").chmod(0o755)

    # Stub notify.sh — silent, never blocks. The real script's only
    # dependency from lib/notify.sh is the `notify_telegram` function name.
    (tmp / "scripts" / "lib" / "notify.sh").write_text(
        '#!/usr/bin/env bash\n'
        'notify_telegram() { :; }\n'
    )

    # Default phase stubs: every phase writes an ADOPT verdict. Tests
    # override per-phase via the phase_runners arg.
    today = subprocess.check_output(["date", "-u", "+%F"], text=True).strip()
    for i, c in enumerate(("a2", "a1", "c1", "b2", "d1"), start=1):
        runner = tmp / "scripts" / f"m2_phase_{c}.sh"
        body = (phase_runners or {}).get(c)
        if body is None:
            body = textwrap.dedent(f"""\
                #!/usr/bin/env bash
                cat > "${{0%/scripts/*}}/results/m2_phase{i}_{c}_verdict_{today}.md" <<EOF
                # Phase {i} verdict
                Mechanical verdict: ADOPT
                EOF
                exit 0
            """)
        runner.write_text(body)
        runner.chmod(0o755)
    return tmp


def write_trigger(tmp: Path) -> Path:
    """Write a STAGE_1 promotion artifact so the trigger gate clears."""
    artifact = tmp / "results" / "stage_promotion_2026-09-09.md"
    artifact.write_text(
        "# Stage promotion 2026-09-09\n"
        "Composite verdict: ALL_GREEN\n"
        "Direction: paper → STAGE_1\n"
    )
    return artifact


def run_launch(tmp: Path, *args: str,
               env_extra: dict[str, str] | None = None) -> tuple[int, str, str]:
    cmd = ["bash", str(tmp / "scripts" / "milestone2_launch.sh"), *args]
    env = os.environ.copy()
    # Strip any inherited Telegram secrets so the stubbed notifier never
    # accidentally posts to a real chat if the stub is bypassed.
    for k in ("TELEGRAM_BOT_TOKEN", "TELEGRAM_CHAT_ID"):
        env.pop(k, None)
    if env_extra:
        env.update(env_extra)
    result = subprocess.run(cmd, capture_output=True, text=True, env=env)
    return result.returncode, result.stdout, result.stderr


# ── Tests ────────────────────────────────────────────────────────────────────

class TriggerGateTest(unittest.TestCase):
    """Trigger detection must fire NOISILY when neither STAGE_1 promotion
    nor strategy-kill artifact is present (runbook §audit-lens-compliance
    bullet 3)."""

    def test_no_trigger_returns_2(self):
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d))
            code, _, err = run_launch(tmp)
            self.assertEqual(code, 2, err)
            self.assertIn("trigger-not-detected", err)

    def test_stage1_promotion_clears_gate(self):
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d))
            write_trigger(tmp)
            code, out, err = run_launch(tmp, "--dry-run")
            self.assertEqual(code, 0, err)
            self.assertIn("stage1_promotion", out)


class OverrideTest(unittest.TestCase):
    """Override must require BOTH the magic literal AND a non-empty
    reason (runbook §trigger-condition)."""

    def test_override_without_reason_exits_3(self):
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d))
            code, _, err = run_launch(
                tmp,
                env_extra={"MILESTONE2_OVERRIDE": "YES_I_UNDERSTAND"},
            )
            self.assertEqual(code, 3, err)
            self.assertIn("override-without-reason", err)

    def test_override_malformed_magic_exits_3(self):
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d))
            code, _, err = run_launch(
                tmp,
                env_extra={
                    "MILESTONE2_OVERRIDE": "yes",  # wrong literal
                    "MILESTONE2_OVERRIDE_REASON": "x",
                },
            )
            self.assertEqual(code, 3, err)
            self.assertIn("override-malformed", err)

    def test_override_with_reason_writes_audit(self):
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d))
            code, _, err = run_launch(
                tmp,
                "--dry-run",
                env_extra={
                    "MILESTONE2_OVERRIDE": "YES_I_UNDERSTAND",
                    "MILESTONE2_OVERRIDE_REASON": "test reason",
                },
            )
            self.assertEqual(code, 0, err)
            audit_files = list((tmp / "results").glob("milestone2_override_*.md"))
            self.assertEqual(len(audit_files), 1, "override audit not written")
            content = audit_files[0].read_text()
            self.assertIn("test reason", content)


class PreflightTest(unittest.TestCase):
    """Missing data or missing binary must fail preflight with exit 3."""

    def test_missing_data_exits_3(self):
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d), with_data=False)
            write_trigger(tmp)
            code, _, err = run_launch(tmp, "--dry-run")
            self.assertEqual(code, 3, err)
            self.assertIn("preflight-failed", err)
            self.assertIn("Sample data file missing", err)

    def test_missing_binary_and_no_go_exits_3(self):
        # `go` resolves via PATH; we can't reliably remove it from CI.
        # Instead delete bin/backtest AND restrict PATH to the minimum
        # needed to run bash itself (so `command -v go` also fails). The
        # /bin:/usr/bin minimum keeps bash + coreutils discoverable without
        # exposing developer toolchains.
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d), with_binary=False)
            write_trigger(tmp)
            code, _, err = run_launch(
                tmp,
                "--dry-run",
                env_extra={"PATH": "/bin:/usr/bin"},
            )
            self.assertEqual(code, 3, err)
            self.assertIn("Neither bin/backtest nor 'go' available", err)

    def test_missing_phase_runner_exits_3(self):
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d))
            (tmp / "scripts" / "m2_phase_a2.sh").unlink()
            write_trigger(tmp)
            code, _, err = run_launch(tmp, "--dry-run")
            self.assertEqual(code, 3, err)
            self.assertIn("Phase runner not found", err)


class ResumeTest(unittest.TestCase):
    """Resume must (a) validate the resume label, (b) restore A2_VERDICT
    from the phase-1 verdict artifact when starting at phase ≥ 2, (c)
    refuse to resume past a CONTRADICTION."""

    def test_invalid_resume_label_exits_3(self):
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d))
            write_trigger(tmp)
            code, _, err = run_launch(
                tmp,
                env_extra={"MILESTONE2_RESUME": "phase99"},
            )
            self.assertEqual(code, 3, err)
            self.assertIn("invalid-resume-point", err)

    def test_resume_phase2_without_phase1_verdict_exits_3(self):
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d))
            write_trigger(tmp)
            code, _, err = run_launch(
                tmp,
                env_extra={"MILESTONE2_RESUME": "phase2"},
            )
            self.assertEqual(code, 3, err)
            self.assertIn("resume-missing-phase1-verdict", err)

    def test_resume_phase3_restores_a2_verdict(self):
        # Make phase 2 and 3 runners assert that A2_VERDICT is non-empty
        # — this is the actual baseline-inheritance contract test.
        runners = {}
        today = subprocess.check_output(["date", "-u", "+%F"], text=True).strip()
        for i, c in enumerate(("a1", "c1"), start=2):
            runners[c] = textwrap.dedent(f"""\
                #!/usr/bin/env bash
                if [[ -z "${{A2_VERDICT:-}}" ]]; then
                    echo "BASELINE-INHERITANCE VIOLATION: A2_VERDICT empty" >&2
                    exit 1
                fi
                if [[ "${{A2_VERDICT}}" != "ADOPT" ]]; then
                    echo "WRONG VERDICT: expected ADOPT, got ${{A2_VERDICT}}" >&2
                    exit 1
                fi
                cat > "${{0%/scripts/*}}/results/m2_phase{i}_{c}_verdict_{today}.md" <<EOF
                Mechanical verdict: ADOPT
                EOF
                exit 0
            """)
        # Phases 4-5 don't need the assertion to pass the test; default ADOPT stubs.
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d), phase_runners=runners)
            write_trigger(tmp)
            # Pre-populate phase-1 verdict (the artifact resume reads).
            (tmp / "results" / f"m2_phase1_a2_verdict_2026-05-09.md").write_text(
                "Mechanical verdict: ADOPT\n"
            )
            code, out, err = run_launch(
                tmp,
                env_extra={"MILESTONE2_RESUME": "phase3"},
            )
            self.assertEqual(code, 0, f"out={out!r} err={err!r}")
            self.assertIn("Restored A2_VERDICT=ADOPT", out)
            # Phase 1 + 2 are skipped per resume; only phase 3-5 ran.
            self.assertIn("Phase 1: A2 (skipped via MILESTONE2_RESUME)", out)
            self.assertIn("Phase 2: A1 (skipped via MILESTONE2_RESUME)", out)

    def test_resume_past_contradiction_refuses(self):
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d))
            write_trigger(tmp)
            # Pre-populate phase-1 verdict with CONTRADICTION — resume must refuse.
            (tmp / "results" / "m2_phase1_a2_verdict_2026-05-09.md").write_text(
                "Mechanical verdict: CONTRADICTION\n"
            )
            code, _, err = run_launch(
                tmp,
                env_extra={"MILESTONE2_RESUME": "phase2"},
            )
            self.assertEqual(code, 5, err)
            self.assertIn("resume-on-contradicted-phase1", err)


class PhaseFailureTest(unittest.TestCase):
    """Phase runner failures must produce the locked exit code per shape."""

    def test_contradiction_mid_sequence_exits_5(self):
        today = subprocess.check_output(["date", "-u", "+%F"], text=True).strip()
        runners = {
            "a2": textwrap.dedent(f"""\
                #!/usr/bin/env bash
                cat > "${{0%/scripts/*}}/results/m2_phase1_a2_verdict_{today}.md" <<EOF
                Mechanical verdict: CONTRADICTION
                EOF
                exit 0
            """)
        }
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d), phase_runners=runners)
            write_trigger(tmp)
            code, _, err = run_launch(tmp)
            self.assertEqual(code, 5, err)
            self.assertIn("phase1-a2-contradiction", err)

    def test_runner_crash_exits_4(self):
        runners = {
            "a2": "#!/usr/bin/env bash\nexit 17\n",
        }
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d), phase_runners=runners)
            write_trigger(tmp)
            code, _, err = run_launch(tmp)
            self.assertEqual(code, 4, err)
            self.assertIn("phase1-a2-crashed", err)
            self.assertIn("exited 17", err)

    def test_runner_ok_but_no_verdict_exits_1(self):
        runners = {
            "a2": "#!/usr/bin/env bash\nexit 0\n",  # writes no verdict
        }
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d), phase_runners=runners)
            write_trigger(tmp)
            code, _, err = run_launch(tmp)
            self.assertEqual(code, 1, err)
            self.assertIn("phase1-a2-no-verdict", err)

    def test_verdict_present_but_unparseable_exits_1(self):
        today = subprocess.check_output(["date", "-u", "+%F"], text=True).strip()
        runners = {
            "a2": textwrap.dedent(f"""\
                #!/usr/bin/env bash
                cat > "${{0%/scripts/*}}/results/m2_phase1_a2_verdict_{today}.md" <<EOF
                # No Mechanical verdict line here.
                EOF
                exit 0
            """)
        }
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d), phase_runners=runners)
            write_trigger(tmp)
            code, _, err = run_launch(tmp)
            self.assertEqual(code, 1, err)
            self.assertIn("phase1-a2-unparseable", err)


class HappyPathTest(unittest.TestCase):
    """All five phases produce ADOPT verdicts → exit 0 with summary."""

    def test_all_adopt_exits_0(self):
        with tempfile.TemporaryDirectory() as d:
            tmp = scaffold_root(Path(d))
            write_trigger(tmp)
            code, out, err = run_launch(tmp)
            self.assertEqual(code, 0, err)
            self.assertIn("All phases complete", out)
            for c in ("a2", "a1", "c1", "b2", "d1"):
                self.assertIn(f"{c}=ADOPT", out)


if __name__ == "__main__":
    unittest.main(verbosity=2)
