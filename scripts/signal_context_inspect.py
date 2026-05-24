#!/usr/bin/env python3
"""signal_context_inspect.py — Descriptive summary of signal-context sidecar stream.

Reports: record counts, date ranges, schema field-presence rates per cohort.
DOES NOT join to journal close-events or produce any cohort-outcome analysis.
Pre-reg: results/signal_context_consumer_decision_rule_2026-05-24.md

Exit codes:
  0  CLEAN    — cache found, records parsed, summary emitted
  1  WARN     — cache found but ≥1 cohort has 0 records
  2  SCHEMA_WARN — records found but required fields missing in ≥1 record
  3  INPUT_ERROR — cache directory missing or empty
  4  PARSE_ERROR — JSONL malformed in ≥1 file
"""
from __future__ import annotations

import json
import sys
from collections import defaultdict
from pathlib import Path
from typing import Any

# ── Schema source of truth ───────────────────────────────────────────────────
# Field list derived by grep from pkg/strategy/signal_context.go:21-64.
# DO NOT transcribe manually — keep in sync with that struct.
REQUIRED_FIELDS = {"event", "symbol", "ts", "label", "side", "entry", "stop",
                   "target", "rr", "reason", "signal_tf"}

OPTIONAL_INDICATOR_FIELDS = [
    "ema9", "ema21", "ema_spread_pct",
    "atr", "realized_vol_30d_ann",
    "bb_upper", "bb_mid", "bb_lower",
    "bias",
    "vwap", "pdh", "pdl", "dist_pdh_pct", "dist_pdl_pct",
    "funding_rate_8h", "funding_bps_per_day",
    "side_filter",
]
def load_cohort(cohort_dir: Path) -> tuple[list[dict[str, Any]], list[str]]:
    """Load all JSONL records from a cohort directory. Returns (records, errors)."""
    records: list[dict[str, Any]] = []
    errors: list[str] = []
    for jsonl_file in sorted(cohort_dir.glob("*.jsonl")):
        for lineno, raw in enumerate(jsonl_file.read_text().splitlines(), 1):
            raw = raw.strip()
            if not raw:
                continue
            try:
                rec = json.loads(raw)
                records.append(rec)
            except json.JSONDecodeError as exc:
                errors.append(f"{jsonl_file.name}:{lineno}: {exc}")
    return records, errors


def field_key_presence_pct(records: list[dict[str, Any]], fields: list[str]) -> dict[str, float]:
    """Return % of records where the field key exists (regardless of value).

    Use this to detect truly absent fields — i.e., the writer never emitted the field.
    A field with a legitimate zero value (e.g., ema_spread_pct=0.0) shows 100% here.
    """
    if not records:
        return {f: 0.0 for f in fields}
    return {f: sum(1 for r in records if f in r) / len(records) * 100 for f in fields}


def field_nonzero_presence_pct(records: list[dict[str, Any]], fields: list[str]) -> dict[str, float]:
    """Return % of records where the field is present AND non-zero/non-empty.

    Legacy semantics (pre-cleanup). Useful for numeric fields where zero means 'uncalculated';
    misleading for fields where zero is a legitimate measurement (e.g., exact PDH cross).
    """
    if not records:
        return {f: 0.0 for f in fields}
    return {
        f: sum(1 for r in records if r.get(f) not in (None, 0, 0.0, "")) / len(records) * 100
        for f in fields
    }


def date_range(records: list[dict[str, Any]]) -> tuple[str, str]:
    ts_vals = [r["ts"] for r in records if "ts" in r]
    if not ts_vals:
        return ("–", "–")
    return (min(ts_vals)[:10], max(ts_vals)[:10])


def per_symbol_counts(records: list[dict[str, Any]]) -> dict[str, int]:
    counts: dict[str, int] = defaultdict(int)
    for r in records:
        sym = r.get("symbol", "UNKNOWN")
        counts[sym] += 1
    return dict(sorted(counts.items()))


def check_required_fields(records: list[dict[str, Any]]) -> list[str]:
    issues = []
    for i, r in enumerate(records):
        missing = REQUIRED_FIELDS - set(r.keys())
        if missing:
            issues.append(f"record {i}: missing required fields {sorted(missing)}")
    return issues


def main() -> int:
    import argparse
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--cache-dir",
        default=str(Path(__file__).parent.parent / "results" / "signal_context_cache"),
        help="local signal-context cache directory",
    )
    parser.add_argument("--json", action="store_true", help="emit machine-readable JSON")
    args = parser.parse_args()

    cache_dir = Path(args.cache_dir)

    # ── INPUT_ERROR: missing or empty cache ──────────────────────────────────
    if not cache_dir.is_dir():
        print(f"INPUT_ERROR: cache directory not found: {cache_dir}", file=sys.stderr)
        print("Run: bash scripts/signal_context_fetch.sh", file=sys.stderr)
        return 3

    cohort_dirs = sorted(d for d in cache_dir.iterdir() if d.is_dir())
    if not cohort_dirs:
        print(f"INPUT_ERROR: cache directory empty: {cache_dir}", file=sys.stderr)
        print("Run: bash scripts/signal_context_fetch.sh", file=sys.stderr)
        return 3

    # ── Load all cohorts ─────────────────────────────────────────────────────
    all_cohort_data: dict[str, dict] = {}
    global_parse_errors: list[str] = []
    global_schema_errors: list[str] = []
    any_empty_cohort = False

    for cohort_dir in cohort_dirs:
        label = cohort_dir.name
        records, parse_errors = load_cohort(cohort_dir)
        schema_errors = check_required_fields(records)
        key_pres = field_key_presence_pct(records, OPTIONAL_INDICATOR_FIELDS)
        nonzero_pres = field_nonzero_presence_pct(records, OPTIONAL_INDICATOR_FIELDS)
        sym_counts = per_symbol_counts(records)
        first_ts, last_ts = date_range(records)

        all_cohort_data[label] = {
            "records": len(records),
            "symbols": sym_counts,
            "date_first": first_ts,
            "date_last": last_ts,
            "field_key_presence_pct": key_pres,
            "field_nonzero_presence_pct": nonzero_pres,
            "parse_errors": parse_errors,
            "schema_errors": schema_errors,
        }
        global_parse_errors.extend(f"[{label}] {e}" for e in parse_errors)
        global_schema_errors.extend(f"[{label}] {e}" for e in schema_errors)
        if len(records) == 0:
            any_empty_cohort = True

    # ── Render ───────────────────────────────────────────────────────────────
    if args.json:
        print(json.dumps(all_cohort_data, indent=2))
    else:
        _render_human(all_cohort_data)

    if global_parse_errors:
        print("\nPARSE_ERROR: malformed JSONL detected:", file=sys.stderr)
        for e in global_parse_errors:
            print(f"  {e}", file=sys.stderr)
        return 4

    if global_schema_errors:
        print("\nSCHEMA_WARN: required fields missing:", file=sys.stderr)
        for e in global_schema_errors[:10]:
            print(f"  {e}", file=sys.stderr)
        return 2

    if any_empty_cohort:
        print("\nWARN: one or more cohorts have 0 records.", file=sys.stderr)
        return 1

    return 0


def _render_human(all_cohort_data: dict[str, dict]) -> None:
    total_records = sum(v["records"] for v in all_cohort_data.values())
    print()
    print("══════════════════════════════════════════════════════════════════")
    print("  Signal-Context Sidecar Inspection")
    print("  Pre-reg: results/signal_context_consumer_decision_rule_2026-05-24.md")
    print("  DESCRIPTIVE ONLY — no cohort-outcome analysis")
    print("══════════════════════════════════════════════════════════════════")
    print(f"\n  Total records across all cohorts: {total_records}\n")

    for label, data in all_cohort_data.items():
        n = data["records"]
        print(f"  ── {label} ({'NO RECORDS' if n == 0 else str(n) + ' records'}) "
              f"  {data['date_first']} → {data['date_last']} ──")

        if n == 0:
            print("    (empty — run signal_context_fetch.sh)")
            continue

        # Per-symbol breakdown
        for sym, cnt in sorted(data["symbols"].items()):
            print(f"    {sym:<20} {cnt:>3} record(s)")

        # Field-presence for optional indicator fields grouped by category.
        # key_pres: field key exists in record (detects writer not emitting the field).
        # nz_pres: field key exists AND value is non-zero (legacy; can mislead on
        #          exact-zero legitimate readings like ema_spread_pct=0.0 at cross).
        # ABSENT flag keys off key_pres so exact-zero readings don't get mislabeled.
        key_pres = data["field_key_presence_pct"]
        nz_pres = data["field_nonzero_presence_pct"]
        print()
        print(f"    {'Field':<25} {'Key%':>8}  {'NonZero%':>9}")
        print(f"    {'─────':<25} {'────':>8}  {'────────':>9}")

        categories = [
            ("EMA", ["ema9", "ema21", "ema_spread_pct"]),
            ("Volatility/ATR", ["atr", "realized_vol_30d_ann"]),
            ("Bollinger", ["bb_upper", "bb_mid", "bb_lower"]),
            ("Context", ["bias", "vwap", "pdh", "pdl", "dist_pdh_pct", "dist_pdl_pct"]),
            ("Funding", ["funding_rate_8h", "funding_bps_per_day"]),
            ("Config", ["side_filter"]),
        ]
        for cat_name, fields in categories:
            first = True
            for f in fields:
                kp = key_pres.get(f, 0.0)
                nzp = nz_pres.get(f, 0.0)
                flag = " ← ABSENT" if kp == 0.0 else (" ← sparse" if kp < 50 else "")
                prefix = f"    [{cat_name}]" if first else "           "
                print(f"    {prefix:<25} {f:<22} {kp:>6.1f}%  {nzp:>7.1f}%{flag}")
                first = False
        print()

    print("══════════════════════════════════════════════════════════════════")
    print("  NOTE: this tool reports data availability only.")
    print("  Cohort-outcome analysis (signal-context × journal closes) is")
    print("  deferred until forward-paper resolution per the pre-reg above.")
    print("══════════════════════════════════════════════════════════════════")
    print()


if __name__ == "__main__":
    sys.exit(main())
