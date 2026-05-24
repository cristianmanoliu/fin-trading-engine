# Signal-context ↔ journal reconciler — pre-registered decision rule

**Status:** LOCKED 2026-05-24, BEFORE writing any reconciler code.
**Scope:** `scripts/journal_fetch.sh` + `scripts/signal_journal_reconcile.py`

---

## What is being built

Read-only tooling to:
1. Fetch journal JSONL files from VPS into a local cache (`results/journal_cache/`), mirroring
   the existing signal-context cache pattern (`results/signal_context_cache/`).
2. Compare per-cohort × per-symbol signal-emission counts (from signal-context sidecar) against
   journal open-event counts (from journal JSONL files) to surface silent-capture gaps.

**Not being built:** cohort-outcome analysis. The reconciler reads signal-context and journal
OPEN events only — it does NOT join signal-context records to journal CLOSE events and does NOT
derive per-signal win/loss.

---

## What the reconciler reports (strictly descriptive)

The reconciler MUST only emit:
- Per-cohort and per-symbol: `signal_count`, `open_count`, `gap = signal_count − open_count`,
  `gap_pct = gap / signal_count × 100`
- A banner naming the 3 legitimate gap sources (see below) with an explicit disclaimer that
  this tool does NOT attribute gap to any specific source
- A point-in-time caveat: gap counts are at moment-of-fetch only, not a trend

The reconciler MUST NOT emit:
- Any cohort-outcome analysis (signal-context joined to journal close-events → win/loss/WR/PnL)
- Any comparison between cohorts' WR, PnL, or Sharpe
- Any "better/worse" judgment about the deployed strategy
- Any recommendation, verdict, or PROMOTE/KILL/HOLD signal
- Any attribution of gap count to a specific cause (that attribution requires joining
  timestamps to journal entries, which is the forbidden cohort-outcome join)

---

## Legitimate gap sources (documented, not diagnosed)

A non-zero gap (signal_count > open_count) for a given cohort/symbol may result from ANY
combination of:

1. **Position-already-open guard** (`pkg/execution/stub.go:443-449`): `Stub.OnSignal`
   rejects when a position is already open. This is the dominant source in steady-state
   live conditions. NOT a bug — designed behavior.
2. **Journal write failure**: mkdir/open/write error produces `slog.Error` + Telegram
   CRITICAL alert. Journal record is lost. Should produce 0 gap in a healthy system;
   gap > 0 from this source = monitoring alert already fired.
3. **Startup recovery** (`Stub.RecoverFromJournal`): restores pre-existing open position
   at startup; the recovered position prevents a double-open if a signal fires immediately.
   N/A in steady-state (recovery is a startup-only event).

This tool does NOT diagnose which source produced the gap.

---

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | CLEAN — both caches found, table rendered |
| 1 | WARN — ≥1 cohort has 0 records in either cache (other cohorts may have data) |
| 2 | SCHEMA_WARN — journal record missing required field (event, symbol, or ts) |
| 3 | INPUT_ERROR — cache directory missing or empty entirely |
| 4 | PARSE_ERROR — JSONL malformed (non-fatal per record, fatal for file) |

Exit code 0 with empty/missing cache is FORBIDDEN (missing-input → silent-success fail-open).

---

## Audit-lens compliance (per docs/AUDIT_LENS.md)

Applied before code is written:

1. **Missing-input → silent-success**: cache absent → exit 3; empty cache → exit 3; ≥1 empty
   cohort with others non-empty → exit 1 WARN (distinct from total-empty → exit 3).
2. **Writer-equals-model**: ACTIVELY DISCLAIMED. The reconciler reads two writers
   (strategy-side signal-context, executor-side journal) and produces NO threshold-gated
   verdict. No threshold exists → nothing to be tautologically PASS.
3. **Sibling-bug propagation**: `load_cohort` and `per_symbol_counts` IMPORTED from
   `signal_context_inspect`, not reimplemented. Schema constants derived by grep from
   `pkg/execution/stub.go:159-187`, pinned by `journal_schema_test.go:TestJournalEntry_FieldSetIsPinned`.
4. **Telegram-tier dual-sense**: no Telegram calls in this tool. Not wired to cron.
5. **Operator-action-path**: explicitly NOT on one. Operator MUST NOT act on gap counts
   during MONITORING (see below).

---

## Operator constraints during MONITORING

**During forward-paper monitoring (current state — STAGE_0, n < 150, day < 60):**
- Gap counts are informational only.
- A non-zero gap is expected (position-already-open rejections are normal).
- Operator MUST NOT treat gap counts as a kill or promote trigger.
- Operator MUST NOT fire the reconciler more frequently than weekly (same cadence discipline
  as drift detector).
- The reconciler is NOT included in `scripts/weekly_audit.sh`.

---

## When this data becomes decision-relevant

Strictly AFTER forward-paper resolution (KILL or PROMOTE artifact written).

Post-resolution, a gap count analysis may surface whether silent-capture failures biased the
signal-context cohort data used for post-mortem analysis. That application requires its own
pre-reg covering: gap-impact methodology, threshold for "too much missing data," and what
verdict changes if a cohort is excluded due to high gap%.

---

## Files this rule governs

- `scripts/journal_fetch.sh` — VPS → local rsync pull (read-only on remote)
- `scripts/signal_journal_reconcile.py` — descriptive gap-count report only
- `results/journal_cache/` — local cache directory (gitignored)
- `results/signal_journal_reconcile_descriptive_2026-05-24.md` — committed snapshot
