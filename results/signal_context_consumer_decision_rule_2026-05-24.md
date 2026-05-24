# Signal-context consumer — pre-registered decision rule

**Status:** LOCKED 2026-05-24, BEFORE writing any consumer code.
**Scope:** `scripts/signal_context_fetch.sh` + `scripts/signal_context_inspect.py`

---

## What is being built

Read-only tooling to fetch and describe the already-running signal-context sidecar stream
(C2 capability enabled since 2026-05-08 via `PAPER_LIVE_SIGNAL_CONTEXT_DIR` on VPS).

The stream captures 16+ indicator fields per emitted signal across 4 cohorts:
- `live` — deployed-16 engines, 4H EMA9/21 short-only 6:1 RR mh504
- `alt5-15-336` — shadow, EMA5/15 mh336
- `alt5-15-504` — shadow, EMA5/15 mh504
- `bb20` — shadow, Bollinger mode

Source schema: `pkg/strategy/signal_context.go:21-64`.

---

## What the inspector reports (strictly descriptive)

The inspector MUST only emit:
- Per-cohort and per-symbol record counts
- First/last record timestamp per cohort
- Schema field-presence rates (% of records where each field is non-zero / populated)
- Per-symbol vs per-cohort totals (surfaces silent capture failures)
- Any discrepancy between record counts and journal close-counts for the same window

The inspector MUST NOT emit:
- Any cohort-outcome analysis (signal-context joined to journal close-events → win/loss)
- Any comparison between cohorts' WR or PnL
- Any "better/worse" judgment about the deployed strategy
- Any recommendation or verdict

Rationale: cohort-outcome joins are the analytical surface that requires a milestone-2-level
pre-reg and family-wise α budget. The inspector is "describe the data we have" only.

---

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | CLEAN — cache found, records parsed, summary emitted |
| 1 | WARN — cache found but one or more cohorts has 0 records |
| 2 | SCHEMA_WARN — records found but required fields (event, symbol, ts) missing in ≥1 record |
| 3 | INPUT_ERROR — cache directory missing or empty entirely |
| 4 | PARSE_ERROR — JSONL malformed (non-fatal per record, fatal for file) |

Exit code 0 with empty cache is FORBIDDEN (missing-input → silent-success fail-open shape).

---

## Audit-lens compliance (per docs/AUDIT_LENS.md)

Applied before code is written:

1. **Missing-input → silent-success**: empty cache = exit 3 INPUT_ERROR, not 0.
2. **Writer-equals-model**: not applicable — inspector reads observational data only.
3. **Sibling-bug propagation**: schema field list derived by grep from
   `pkg/strategy/signal_context.go`, not transcribed manually. Future analyzer
   scripts that read the same stream must apply the same lens before merging.
4. **Telegram-tier dual-sense**: no Telegram calls in this tool.
5. **Operator-action-path**: not on one. Explicitly diagnostic.

---

## When this data becomes decision-relevant

Strictly AFTER forward-paper resolution (KILL or PROMOTE artifact written).

Post-resolution, cohort-outcome analysis against this stream requires its own pre-reg
covering: cohort definitions, minimum n per bin, comparison methodology, verdict criteria,
and family-wise α correction if multiple cohort splits are tested.

---

## Calibration artifact

`scripts/signal_context_inspect.py` run on 2026-05-24 → output committed to
`results/signal_context_capture_descriptive_2026-05-24.md`. This locks "what was captured
as of this date" for longitudinal reference.

---

## Files this rule governs

- `scripts/signal_context_fetch.sh` — VPS → local rsync pull
- `scripts/signal_context_inspect.py` — descriptive summary only
- `results/signal_context_cache/` — local cache directory (gitignored)
- `results/signal_context_capture_descriptive_2026-05-24.md` — committed snapshot
