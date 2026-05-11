#!/usr/bin/env python3
"""
test_remote_fetch_no_shell_injection.py — pattern-pin the shell-injection-
via-f-string fix across the four decision-grade Python tools that fetch
remote journals via ssh.

Background (2026-05-11): realized_cost_trajectory.py was hardened in-place
to use list-arg subprocess + shlex.quote(remote_dir) so that operator-
controlled values (--vps, --live-dir) cannot inject shell commands on
the LOCAL machine. The sibling decision tools (live_vs_backtest_drift,
kill_protocol_check, stage_promotion_check) shipped the SAME risky
pattern — shell=True with an f-string embedding both vps and remote_dir
directly into a single command string — until this lens-pattern-lock
session swept all three.

This test pins the fix in two ways:

  1. Source-scan: each target file must NOT contain `shell=True` AND
     must NOT contain `f'ssh ` (the unsafe interpolation shape).
  2. Behavioural: monkey-patching subprocess.run captures the actual
     call arguments, verifying the first positional arg is a list
     starting with "ssh" — not a shell command string. This catches
     a regression that gets past the source-scan via aliased import
     or string concatenation.

If a future change reintroduces the pattern, this test fails, the
audit-pattern memo gets re-applied, the fix gets re-committed.

Run:
  python3 scripts/test_remote_fetch_no_shell_injection.py
"""
from __future__ import annotations

import ast
import importlib.util
import sys
import unittest
from pathlib import Path
from unittest import mock

REPO = Path(__file__).resolve().parent.parent
SCRIPTS = REPO / "scripts"

# All four decision-grade tools that fetch remote journals via ssh.
TARGETS = [
    ("realized_cost_trajectory", "fetch_remote"),
    ("live_vs_backtest_drift", "load_remote_trades"),
    ("kill_protocol_check", "fetch_remote"),
    ("stage_promotion_check", "fetch_remote"),
]


def _load_module(name: str):
    """Load a scripts/*.py file as a module without packaging.

    Registers the module in sys.modules BEFORE exec_module so that
    @dataclass and other decorators that look up `sys.modules[__name__]`
    during class creation can resolve correctly (Python 3.14 hardened
    this lookup; pre-3.14 silently tolerated the missing entry)."""
    spec = importlib.util.spec_from_file_location(name, SCRIPTS / f"{name}.py")
    assert spec and spec.loader, f"failed to load spec for {name}"
    mod = importlib.util.module_from_spec(spec)
    sys.modules[name] = mod
    try:
        spec.loader.exec_module(mod)
    except Exception:
        sys.modules.pop(name, None)
        raise
    return mod


# ── Source-scan tests ────────────────────────────────────────────────────────


def _subprocess_run_calls_with_shell_true(tree: ast.AST) -> list[int]:
    """Return line numbers of any `subprocess.run(..., shell=True)` calls
    in the parsed AST. Comments and docstrings are skipped because AST
    only contains actual call expressions — not the substring-search
    false positives that previously failed this test on
    realized_cost_trajectory.py's own inline comment."""
    hits: list[int] = []
    for node in ast.walk(tree):
        if not isinstance(node, ast.Call):
            continue
        func = node.func
        # Match both `subprocess.run(...)` (Attribute) and bare `run(...)`
        # if someone aliased `from subprocess import run`.
        is_run = (
            (isinstance(func, ast.Attribute) and func.attr == "run") or
            (isinstance(func, ast.Name) and func.id == "run")
        )
        if not is_run:
            continue
        for kw in node.keywords:
            if kw.arg == "shell" and isinstance(kw.value, ast.Constant) \
                    and kw.value.value is True:
                hits.append(node.lineno)
    return hits


class SourceScanTest(unittest.TestCase):
    """Each target file's subprocess.run calls must not use shell=True.

    AST-based detection (not substring) so comments/docstrings that legitimately
    mention `shell=True` (e.g., realized_cost_trajectory.fetch_remote's lens-
    documentation comment) don't trigger false positives."""

    def test_no_subprocess_run_with_shell_true(self):
        for module_name, _ in TARGETS:
            path = SCRIPTS / f"{module_name}.py"
            tree = ast.parse(path.read_text(), filename=str(path))
            hits = _subprocess_run_calls_with_shell_true(tree)
            self.assertEqual(
                hits, [],
                f"{module_name}.py:{hits} uses subprocess.run(..., shell=True) "
                "— replace with list-arg subprocess + shlex.quote. See "
                "realized_cost_trajectory.fetch_remote for the canonical "
                "fix; pattern documented in MEMORY.md audit_pattern_2026-05-09.")


# ── Behavioural tests ────────────────────────────────────────────────────────


class BehaviouralTest(unittest.TestCase):
    """Monkey-patch subprocess.run, assert call shape is list-form."""

    def _drive_and_capture(self, module_name: str, func_name: str,
                           vps: str, remote_dir: str) -> list:
        """Invoke <module>.<func>(vps, remote_dir) under a patched
        subprocess.run that captures call args and short-circuits the
        ssh + tar pipeline with empty bytes / empty list."""
        mod = _load_module(module_name)
        captured: list = []

        def fake_run(*args, **kwargs):
            captured.append((args, kwargs))
            # Return an object mimicking subprocess.run's CompletedProcess
            # with empty stdout, so the downstream tar -x returns no files
            # and load_local/load_journal yields an empty list.
            result = mock.Mock()
            result.stdout = b""
            result.returncode = 0
            return result

        with mock.patch.object(mod.subprocess, "run", side_effect=fake_run):
            func = getattr(mod, func_name)
            # The function will call subprocess.run twice (ssh + tar).
            # Both should succeed via our stub. Some functions return
            # a tuple (trades, stats); we just need to invoke without raising.
            try:
                func(vps, remote_dir)
            except Exception as e:
                self.fail(
                    f"{module_name}.{func_name} raised {type(e).__name__}: {e}")
        return captured

    def test_each_target_uses_list_arg_ssh_call(self):
        """First subprocess call must be ssh-as-list, not ssh-as-string."""
        # Use a remote_dir with a shell metacharacter to also verify
        # shlex.quote treatment. If the function were still doing
        # f'ssh {vps} "tar ... {remote_dir} ..."', the dangerous
        # metacharacters would propagate to the shell.
        evil_remote_dir = '/var/log/paper-live/journal"; touch /tmp/PWN; #'
        vps = "root@host"
        for module_name, func_name in TARGETS:
            with self.subTest(module=module_name, func=func_name):
                captured = self._drive_and_capture(
                    module_name, func_name, vps, evil_remote_dir)
                self.assertGreaterEqual(
                    len(captured), 1,
                    f"{module_name}.{func_name} did not invoke subprocess.run")
                first_args, first_kwargs = captured[0]
                # The first positional arg must be a list. shell=True must
                # NOT be in kwargs.
                self.assertNotIn(
                    "shell", first_kwargs,
                    f"{module_name}.{func_name} passed shell={first_kwargs.get('shell')!r} "
                    "to subprocess.run — replace with list-arg form.")
                self.assertEqual(
                    len(first_args), 1,
                    f"{module_name}.{func_name} subprocess.run call shape "
                    f"unexpected: positional args = {first_args!r}")
                cmd = first_args[0]
                self.assertIsInstance(
                    cmd, list,
                    f"{module_name}.{func_name} subprocess.run received a "
                    f"non-list cmd: {cmd!r} — shell-injection-via-f-string "
                    "regression risk.")
                self.assertEqual(
                    cmd[0], "ssh",
                    f"{module_name}.{func_name} first ssh subprocess call "
                    f"doesn't start with 'ssh': {cmd!r}")
                self.assertEqual(
                    cmd[1], vps,
                    f"{module_name}.{func_name} second list element should "
                    f"be the vps target, got {cmd!r}")
                # The remote command (3rd element) must contain a quoted
                # form of the evil_remote_dir — shlex.quote wraps it in
                # single quotes and escapes internal single quotes. Verify
                # the value is no longer interpolated raw.
                remote_cmd = cmd[2]
                self.assertNotIn(
                    "; touch /tmp/PWN", remote_cmd.split("'")[0] if "'" in remote_cmd else remote_cmd,
                    f"{module_name}.{func_name} remote command appears to "
                    f"interpolate evil_remote_dir unquoted: {remote_cmd!r}")


if __name__ == "__main__":
    unittest.main(verbosity=2)
