#!/usr/bin/env python3
"""
Unit tests for kraken_demo_smoke.py env selection (demo default, --prod opt-in).

Covers:
  - resolve_env([])          → demo base URL, demo env file, KRAKEN_DEMO_* keys
  - resolve_env(["--prod"])  → prod base URL, prod env file, KRAKEN_* keys
  - resolve_env unknown arg  → SystemExit(2)
  - load_env reads the prefix-selected key pair from an env file

No network access — signing/GET paths are exercised only by the live smoke run.

Run:
  python3 scripts/test_kraken_demo_smoke.py
"""
from __future__ import annotations

import importlib.util
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
SCRIPT = REPO / "scripts" / "kraken_demo_smoke.py"

spec = importlib.util.spec_from_file_location("kraken_demo_smoke", SCRIPT)
smoke = importlib.util.module_from_spec(spec)
spec.loader.exec_module(smoke)


class TestResolveEnv(unittest.TestCase):
    def test_default_is_demo(self):
        base, env_path, prefix = smoke.resolve_env([])
        self.assertEqual(base, "https://demo-futures.kraken.com")
        self.assertEqual(env_path, "~/.kraken-futures-demo.env")
        self.assertEqual(prefix, "KRAKEN_DEMO_")

    def test_prod_flag(self):
        base, env_path, prefix = smoke.resolve_env(["--prod"])
        self.assertEqual(base, "https://futures.kraken.com")
        self.assertEqual(env_path, "~/.kraken-futures.env")
        self.assertEqual(prefix, "KRAKEN_")

    def test_unknown_arg_exits_2(self):
        with self.assertRaises(SystemExit) as cm:
            smoke.resolve_env(["--bogus"])
        self.assertEqual(cm.exception.code, 2)


class TestLoadEnv(unittest.TestCase):
    def _write_env(self, content: str) -> str:
        f = tempfile.NamedTemporaryFile(
            mode="w", suffix=".env", delete=False)
        f.write(content)
        f.close()
        self.addCleanup(Path(f.name).unlink)
        return f.name

    def test_reads_demo_prefixed_keys(self):
        path = self._write_env(
            "# comment\n"
            "KRAKEN_DEMO_API_KEY=demokey\n"
            "KRAKEN_DEMO_API_SECRET=demosecret\n")
        key, secret = smoke.load_env(path, "KRAKEN_DEMO_")
        self.assertEqual(key, "demokey")
        self.assertEqual(secret, "demosecret")

    def test_reads_prod_prefixed_keys(self):
        path = self._write_env(
            "KRAKEN_API_KEY=prodkey\n"
            "KRAKEN_API_SECRET=prodsecret\n")
        key, secret = smoke.load_env(path, "KRAKEN_")
        self.assertEqual(key, "prodkey")
        self.assertEqual(secret, "prodsecret")

    def test_missing_key_raises_keyerror(self):
        path = self._write_env("KRAKEN_API_KEY=onlykey\n")
        with self.assertRaises(KeyError):
            smoke.load_env(path, "KRAKEN_")


class TestMissingCredsFile(unittest.TestCase):
    def test_prod_missing_env_file_exits_2_no_traceback(self):
        env = os.environ.copy()
        env["HOME"] = tempfile.mkdtemp()
        proc = subprocess.run(
            [sys.executable, str(SCRIPT), "--prod"],
            capture_output=True, text=True, env=env)
        self.assertEqual(proc.returncode, 2)
        self.assertNotIn("Traceback", proc.stderr)
        self.assertIn(".kraken-futures.env", proc.stderr)
        self.assertIn("step 6", proc.stderr)


if __name__ == "__main__":
    unittest.main()
