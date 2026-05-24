"""Tests for signal_context_inspect.py (cleanup commit).

Pins all 4 non-zero exit codes with deliberate-bad-input shapes.
Covers the zero-vs-absent fix: field_key_presence_pct vs field_nonzero_presence_pct.
"""
from __future__ import annotations

import json
import subprocess
import sys
from pathlib import Path

import pytest

ROOT = Path(__file__).parent.parent
SCRIPT = ROOT / "scripts" / "signal_context_inspect.py"


# ── Helper ────────────────────────────────────────────────────────────────────

def run_cli(*args):
    return subprocess.run(
        [sys.executable, str(SCRIPT)] + list(args),
        capture_output=True, text=True,
    )


def make_signal_record(symbol="BTCUSDT", ts="2026-05-24T10:00:00Z", **extras):
    base = {
        "event": "signal_context", "symbol": symbol, "ts": ts,
        "label": "live", "side": "short", "entry": 68000.0, "stop": 69000.0,
        "target": 62000.0, "rr": 6.0, "reason": "ema_cross", "signal_tf": "4H",
        "ema9": 67800.0, "ema21": 68100.0, "ema_spread_pct": -0.44,
        "bias": -1, "vwap": 67900.0, "pdh": 68500.0, "pdl": 66000.0,
        "dist_pdh_pct": 0.73, "dist_pdl_pct": 2.94, "side_filter": "short",
    }
    base.update(extras)
    return json.dumps(base)


def make_cache(tmp_path, cohort, records):
    d = tmp_path / cohort
    d.mkdir(parents=True)
    if records:
        (d / "BTCUSDT-2026-05.jsonl").write_text("\n".join(records) + "\n")
    return d


# ── Exit code 0: CLEAN ────────────────────────────────────────────────────────

def test_clean_exit_zero(tmp_path):
    cache = tmp_path / "cache"
    make_cache(cache, "live", [make_signal_record()])
    r = run_cli("--cache-dir", str(cache))
    assert r.returncode == 0, r.stderr


def test_clean_output_contains_required_sections(tmp_path):
    cache = tmp_path / "cache"
    make_cache(cache, "live", [make_signal_record()])
    r = run_cli("--cache-dir", str(cache))
    assert r.returncode == 0
    assert "DESCRIPTIVE ONLY" in r.stdout
    assert "Total records" in r.stdout


# ── Exit code 1: WARN — ≥1 cohort with 0 records ─────────────────────────────

def test_warn_on_empty_cohort(tmp_path):
    cache = tmp_path / "cache"
    d = cache / "live"
    d.mkdir(parents=True)
    (d / "empty.jsonl").write_text("")  # file present but no records
    r = run_cli("--cache-dir", str(cache))
    assert r.returncode == 1, r.stdout + r.stderr
    assert "WARN" in r.stderr


def test_warn_when_cohort_dir_exists_but_no_jsonl(tmp_path):
    cache = tmp_path / "cache"
    (cache / "live").mkdir(parents=True)  # dir present, no *.jsonl files
    r = run_cli("--cache-dir", str(cache))
    assert r.returncode == 1, r.stdout + r.stderr


# ── Exit code 2: SCHEMA_WARN — required fields missing ───────────────────────

def test_schema_warn_on_missing_required_field(tmp_path):
    cache = tmp_path / "cache"
    bad = json.dumps({"event": "signal_context", "symbol": "BTCUSDT"})  # missing 'ts'
    make_cache(cache, "live", [bad])
    r = run_cli("--cache-dir", str(cache))
    assert r.returncode == 2, r.stdout + r.stderr
    assert "SCHEMA_WARN" in r.stderr


# ── Exit code 3: INPUT_ERROR — missing or empty cache dir ────────────────────

def test_input_error_cache_missing(tmp_path):
    r = run_cli("--cache-dir", str(tmp_path / "nonexistent"))
    assert r.returncode == 3
    assert "INPUT_ERROR" in r.stderr


def test_input_error_cache_empty_of_cohorts(tmp_path):
    cache = tmp_path / "cache"
    cache.mkdir()  # dir exists but no cohort subdirs
    r = run_cli("--cache-dir", str(cache))
    assert r.returncode == 3, r.stdout + r.stderr
    assert "INPUT_ERROR" in r.stderr


# ── Exit code 4: PARSE_ERROR — malformed JSONL ───────────────────────────────

def test_parse_error_malformed_jsonl(tmp_path):
    cache = tmp_path / "cache"
    d = cache / "live"
    d.mkdir(parents=True)
    (d / "BTCUSDT-2026-05.jsonl").write_text("this is not json\n")
    r = run_cli("--cache-dir", str(cache))
    assert r.returncode == 4, r.stdout + r.stderr
    assert "PARSE_ERROR" in r.stderr


# ── Zero-vs-absent fix ────────────────────────────────────────────────────────

def test_field_key_presence_vs_nonzero_presence():
    """Key% and NonZero% differ when a field has exact-zero values."""
    from signal_context_inspect import field_key_presence_pct, field_nonzero_presence_pct

    records = [
        {"ema_spread_pct": 0.0},   # present with zero value (e.g., exact cross)
        {"ema_spread_pct": -0.44},  # present with non-zero value
        {},                         # field absent entirely
    ]
    key_pct = field_key_presence_pct(records, ["ema_spread_pct"])
    nz_pct = field_nonzero_presence_pct(records, ["ema_spread_pct"])

    # 2 of 3 records have the key → 66.7%
    assert abs(key_pct["ema_spread_pct"] - 66.7) < 0.5
    # Only 1 of 3 records has a nonzero value → 33.3%
    assert abs(nz_pct["ema_spread_pct"] - 33.3) < 0.5


def test_absent_field_shows_zero_in_both():
    """Field never written → both Key% and NonZero% are 0%."""
    from signal_context_inspect import field_key_presence_pct, field_nonzero_presence_pct

    records = [{"other_field": 1.0}, {"other_field": 2.0}]
    key_pct = field_key_presence_pct(records, ["atr"])
    nz_pct = field_nonzero_presence_pct(records, ["atr"])
    assert key_pct["atr"] == 0.0
    assert nz_pct["atr"] == 0.0


def test_fully_present_field_shows_100_in_both():
    """Field always populated non-zero → both Key% and NonZero% are 100%."""
    from signal_context_inspect import field_key_presence_pct, field_nonzero_presence_pct

    records = [{"ema9": 67800.0}, {"ema9": 68100.0}]
    key_pct = field_key_presence_pct(records, ["ema9"])
    nz_pct = field_nonzero_presence_pct(records, ["ema9"])
    assert key_pct["ema9"] == 100.0
    assert nz_pct["ema9"] == 100.0


def test_renderer_shows_two_presence_columns(tmp_path):
    """Human output must include both Key% and NonZero% column headers."""
    cache = tmp_path / "cache"
    make_cache(cache, "live", [make_signal_record()])
    r = run_cli("--cache-dir", str(cache))
    assert r.returncode == 0
    assert "Key%" in r.stdout
    assert "NonZero%" in r.stdout


if __name__ == "__main__":
    import traceback
    import tempfile
    tests = [(k, v) for k, v in sorted(globals().items()) if k.startswith("test_")]
    passed = failed = 0
    for name, fn in tests:
        try:
            import inspect
            sig = inspect.signature(fn)
            if "tmp_path" in sig.parameters:
                with tempfile.TemporaryDirectory() as td:
                    fn(Path(td))
            else:
                fn()
            print(f"  PASS  {name}")
            passed += 1
        except Exception as exc:
            print(f"  FAIL  {name}: {exc}")
            traceback.print_exc()
            failed += 1
    print(f"\n{passed} passed, {failed} failed")
