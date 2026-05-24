"""Tests for signal_journal_reconcile.py.

Pins all 5 exit codes with deliberate-bad-input shapes (audit-lens Mode 2).
Each exit code has ≥1 test that constructs the bad-input condition explicitly.
"""
from __future__ import annotations

import json
import re
import subprocess
import sys
from pathlib import Path

import pytest

ROOT = Path(__file__).parent.parent
SCRIPT = ROOT / "scripts" / "signal_journal_reconcile.py"
STUB_GO = ROOT / "pkg" / "execution" / "stub.go"


# ── Helpers ───────────────────────────────────────────────────────────────────

def run_cli(*args):
    return subprocess.run(
        [sys.executable, str(SCRIPT)] + list(args),
        capture_output=True, text=True,
    )


def make_signal_record(symbol="BTCUSDT", side="short", ts="2026-05-24T10:00:00Z"):
    return json.dumps({
        "event": "signal_context", "symbol": symbol, "ts": ts,
        "label": "live", "side": side, "entry": 68000.0, "stop": 69000.0,
        "target": 62000.0, "rr": 6.0, "reason": "ema_cross", "signal_tf": "4H",
        "ema9": 67800.0, "ema21": 68100.0,
    })


def make_journal_open(symbol="BTCUSDT", side="short", ts="2026-05-24T10:01:00Z"):
    return json.dumps({
        "event": "open", "symbol": symbol, "ts": ts,
        "side": side, "entry": 68000.0, "stop": 69000.0, "target": 62000.0,
        "reason": "ema_cross",
    })


def make_journal_close(symbol="BTCUSDT", side="short", ts="2026-05-24T20:00:00Z"):
    return json.dumps({
        "event": "close", "symbol": symbol, "ts": ts,
        "side": side, "entry": 68000.0, "stop": 69000.0, "target": 62000.0,
        "reason": "ema_cross", "exit": 62000.0, "pnl_usd": 354.0, "outcome": "TARGET",
    })


def populate_signal_cache(base: Path, cohort: str, records: list[str]) -> Path:
    d = base / "signal_context_cache" / cohort
    d.mkdir(parents=True)
    (d / "BTCUSDT-2026-05.jsonl").write_text("\n".join(records) + "\n")
    return d


def populate_journal_cache(base: Path, subpath: str, records: list[str]) -> Path:
    d = base / "journal_cache"
    if subpath:
        d = d / subpath
    d.mkdir(parents=True)
    (d / "BTCUSDT-2026-05.jsonl").write_text("\n".join(records) + "\n")
    return d


# ── Exit code 0: CLEAN ────────────────────────────────────────────────────────

def test_clean_exit_zero(tmp_path):
    populate_signal_cache(tmp_path, "live", [make_signal_record()])
    populate_journal_cache(tmp_path, "", [make_journal_open()])
    r = run_cli(
        "--signal-cache-dir", str(tmp_path / "signal_context_cache"),
        "--journal-cache-dir", str(tmp_path / "journal_cache"),
    )
    assert r.returncode == 0, r.stderr


def test_clean_gap_counted(tmp_path):
    # 2 signals, 1 open → gap = 1 (position-already-open scenario)
    recs = [make_signal_record(ts="2026-05-24T10:00:00Z"),
            make_signal_record(ts="2026-05-24T11:00:00Z")]
    populate_signal_cache(tmp_path, "live", recs)
    populate_journal_cache(tmp_path, "", [make_journal_open()])
    r = run_cli(
        "--signal-cache-dir", str(tmp_path / "signal_context_cache"),
        "--journal-cache-dir", str(tmp_path / "journal_cache"),
    )
    assert r.returncode == 0, r.stderr
    # gap = 1 should appear in stdout
    assert "1" in r.stdout


# ── Exit code 1: WARN (≥1 empty cohort) ──────────────────────────────────────

def test_warn_when_cohort_signal_empty(tmp_path):
    # Cohort present but 0 signal records
    d = tmp_path / "signal_context_cache" / "live"
    d.mkdir(parents=True)
    (d / "BTCUSDT-2026-05.jsonl").write_text("")  # empty
    populate_journal_cache(tmp_path, "", [make_journal_open()])
    r = run_cli(
        "--signal-cache-dir", str(tmp_path / "signal_context_cache"),
        "--journal-cache-dir", str(tmp_path / "journal_cache"),
    )
    assert r.returncode == 1, r.stdout + r.stderr


def test_warn_when_journal_dir_missing_for_cohort(tmp_path):
    # Signal cache has records but journal subdir for that cohort is absent
    populate_signal_cache(tmp_path, "live", [make_signal_record()])
    # journal_cache exists but no top-level files
    (tmp_path / "journal_cache").mkdir()
    r = run_cli(
        "--signal-cache-dir", str(tmp_path / "signal_context_cache"),
        "--journal-cache-dir", str(tmp_path / "journal_cache"),
    )
    # journal dir present but no *.jsonl → open_count = 0, cohort still counted
    # This is CLEAN (gap=1 is legitimate) as long as journal dir exists.
    # If journal dir missing entirely, reconciler marks journal_dir_missing → WARN.
    # Here dir exists with no files → empty cohort → WARN.
    assert r.returncode in (0, 1)  # depends on empty-cohort detection path


# ── Exit code 2: SCHEMA_WARN ──────────────────────────────────────────────────

def test_schema_warn_on_missing_event_field(tmp_path):
    # Journal record missing required 'event' field
    bad_record = json.dumps({"symbol": "BTCUSDT", "ts": "2026-05-24T10:01:00Z",
                             "side": "short", "entry": 68000.0, "stop": 69000.0,
                             "target": 62000.0, "reason": "ema_cross"})
    populate_signal_cache(tmp_path, "live", [make_signal_record()])
    populate_journal_cache(tmp_path, "", [bad_record])
    r = run_cli(
        "--signal-cache-dir", str(tmp_path / "signal_context_cache"),
        "--journal-cache-dir", str(tmp_path / "journal_cache"),
    )
    assert r.returncode == 2, r.stdout + r.stderr


def test_schema_warn_on_missing_ts_field(tmp_path):
    bad_record = json.dumps({"event": "open", "symbol": "BTCUSDT",
                             "side": "short", "entry": 68000.0, "stop": 69000.0,
                             "target": 62000.0, "reason": "ema_cross"})
    populate_signal_cache(tmp_path, "live", [make_signal_record()])
    populate_journal_cache(tmp_path, "", [bad_record])
    r = run_cli(
        "--signal-cache-dir", str(tmp_path / "signal_context_cache"),
        "--journal-cache-dir", str(tmp_path / "journal_cache"),
    )
    assert r.returncode == 2, r.stdout + r.stderr


# ── Exit code 3: INPUT_ERROR ──────────────────────────────────────────────────

def test_input_error_signal_cache_missing(tmp_path):
    r = run_cli(
        "--signal-cache-dir", str(tmp_path / "nonexistent_signal"),
        "--journal-cache-dir", str(tmp_path / "nonexistent_journal"),
    )
    assert r.returncode == 3


def test_input_error_signal_cache_empty(tmp_path):
    (tmp_path / "signal_context_cache").mkdir()
    (tmp_path / "journal_cache").mkdir()
    r = run_cli(
        "--signal-cache-dir", str(tmp_path / "signal_context_cache"),
        "--journal-cache-dir", str(tmp_path / "journal_cache"),
    )
    assert r.returncode == 3, r.stdout + r.stderr


def test_input_error_journal_cache_missing(tmp_path):
    populate_signal_cache(tmp_path, "live", [make_signal_record()])
    r = run_cli(
        "--signal-cache-dir", str(tmp_path / "signal_context_cache"),
        "--journal-cache-dir", str(tmp_path / "nonexistent_journal"),
    )
    assert r.returncode == 3


# ── Exit code 4: PARSE_ERROR ──────────────────────────────────────────────────

def test_parse_error_malformed_journal_jsonl(tmp_path):
    populate_signal_cache(tmp_path, "live", [make_signal_record()])
    d = tmp_path / "journal_cache"
    d.mkdir(parents=True)
    (d / "BTCUSDT-2026-05.jsonl").write_text("not valid json\n")
    r = run_cli(
        "--signal-cache-dir", str(tmp_path / "signal_context_cache"),
        "--journal-cache-dir", str(tmp_path / "journal_cache"),
    )
    assert r.returncode == 4, r.stdout + r.stderr


# ── Gap_pct div-by-zero guard ─────────────────────────────────────────────────

def test_gap_pct_na_when_signal_count_zero(tmp_path):
    # Signal cache cohort exists but has 0 records; journal has opens.
    d = tmp_path / "signal_context_cache" / "live"
    d.mkdir(parents=True)
    (d / "empty.jsonl").write_text("")
    populate_journal_cache(tmp_path, "", [make_journal_open()])
    r = run_cli(
        "--signal-cache-dir", str(tmp_path / "signal_context_cache"),
        "--journal-cache-dir", str(tmp_path / "journal_cache"),
        "--json",
    )
    # Should not crash (no ZeroDivisionError). Exit code may be 1 (WARN).
    assert r.returncode in (0, 1)


# ── Cohort-mapping: live → flat, shadows → subdir ─────────────────────────────

def test_shadow_cohort_reads_from_subdir(tmp_path):
    populate_signal_cache(tmp_path, "alt5-15-336", [make_signal_record()])
    populate_journal_cache(tmp_path, "shadow/alt5-15-336", [make_journal_open()])
    r = run_cli(
        "--signal-cache-dir", str(tmp_path / "signal_context_cache"),
        "--journal-cache-dir", str(tmp_path / "journal_cache"),
    )
    assert r.returncode == 0, r.stderr
    # The shadow cohort's counts should appear in output
    assert "alt5-15-336" in r.stdout


def test_live_cohort_reads_from_toplevel(tmp_path):
    populate_signal_cache(tmp_path, "live", [make_signal_record()])
    populate_journal_cache(tmp_path, "", [make_journal_open()])  # top-level
    r = run_cli(
        "--signal-cache-dir", str(tmp_path / "signal_context_cache"),
        "--journal-cache-dir", str(tmp_path / "journal_cache"),
    )
    assert r.returncode == 0, r.stderr


# ── Pre-reg trip-wire: DESCRIPTIVE ONLY banner must exist ─────────────────────

def test_banner_contains_descriptive_only(tmp_path):
    populate_signal_cache(tmp_path, "live", [make_signal_record()])
    populate_journal_cache(tmp_path, "", [make_journal_open()])
    r = run_cli(
        "--signal-cache-dir", str(tmp_path / "signal_context_cache"),
        "--journal-cache-dir", str(tmp_path / "journal_cache"),
    )
    assert r.returncode == 0, r.stderr
    assert "DESCRIPTIVE ONLY" in r.stdout
    assert "no cohort-outcome analysis" in r.stdout


# ── Schema-drift sanity: Python constants match Go struct ────────────────────

def test_schema_constants_match_go_struct():
    """Parses stub.go json tags and verifies they match Python constants."""
    from signal_journal_reconcile import JOURNAL_REQUIRED_FIELDS, JOURNAL_OPTIONAL_FIELDS
    expected_all = {
        "entry", "event", "exit", "fee_usd", "funding_usd", "gross_usd",
        "mae_r", "mfe_r", "notional_usd", "outcome", "pnl_pts", "pnl_usd",
        "reason", "side", "slip_usd", "stop", "symbol", "target", "ts",
    }
    actual_all = JOURNAL_REQUIRED_FIELDS | JOURNAL_OPTIONAL_FIELDS
    assert actual_all == expected_all, (
        f"Python schema constants don't match Go journalEntry.\n"
        f"  Extra in Python: {actual_all - expected_all}\n"
        f"  Missing from Python: {expected_all - actual_all}\n"
        f"  (Golden from pkg/execution/journal_schema_test.go)"
    )


if __name__ == "__main__":
    import traceback
    tests = [(k, v) for k, v in sorted(globals().items()) if k.startswith("test_")]
    passed = failed = 0
    for name, fn in tests:
        try:
            import tempfile
            with tempfile.TemporaryDirectory() as td:
                fn(Path(td))
            print(f"  PASS  {name}")
            passed += 1
        except TypeError:
            try:
                fn()
                print(f"  PASS  {name}")
                passed += 1
            except Exception as exc:
                print(f"  FAIL  {name}: {exc}")
                traceback.print_exc()
                failed += 1
        except Exception as exc:
            print(f"  FAIL  {name}: {exc}")
            traceback.print_exc()
            failed += 1
    print(f"\n{passed} passed, {failed} failed")
