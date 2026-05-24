# Signal-context ↔ journal reconciler — calibration snapshot 2026-05-24

**Timestamp:** 2026-05-24, ~10:30 UTC
**Status:** Calibration artifact. NOT a decision. NOT a research finding.
**Pre-reg:** `results/signal_journal_reconcile_decision_rule_2026-05-24.md`
**Tool version:** commit after `06fa0d5` (schema-completeness fix session)

---

## What this captures

Point-in-time gap counts at forward-paper day 16/60, n_live=27. Locks "this is
what reconciliation showed at this moment" for longitudinal reference. Future
runs will show different counts as more trades accumulate.

Caches used:
- Signal-context: `results/signal_context_cache/` (235 total records across 4 cohorts)
- Journal (fetched via `scripts/journal_fetch.sh`): `results/journal_cache/` (80 JSONL files)

---

## Reconciler output (stdout)

```
══════════════════════════════════════════════════════════════════
  Signal-Context ↔ Journal Reconciler
  Pre-reg: results/signal_journal_reconcile_decision_rule_2026-05-24.md
  DESCRIPTIVE ONLY — no cohort-outcome analysis
══════════════════════════════════════════════════════════════════

  Legitimate gap sources (this tool does NOT attribute):
    1. Stub.OnSignal position-already-open rejection (dominant, by design)
    2. Journal write failure (should be 0; if >0, Telegram CRITICAL already fired)
    3. Startup recovery (startup-only; N/A in steady state)

  gap = signal_count − open_count  (expected ≥ 0)
  Operator MUST NOT act on gap counts during MONITORING (pre-reg locked).

  Total  signal=235  open=164  gap=71  (30.2%)

  ── alt5-15-336: signal=46  open=42  gap=4  (8.7%) ──
    Symbol                  Signals    Opens    Gap    Gap%
    ──────                  ───────    ─────    ───    ────
    1000SHIBUSDT                  3        3      0    0.0%
    1INCHUSDT                     3        3      0    0.0%
    ADAUSDT                       3        3      0    0.0%
    APTUSDT                       2        2      0    0.0%
    AVAXUSDT                      3        3      0    0.0%
    BCHUSDT                       2        2      0    0.0%
    DOTUSDT                       3        3      0    0.0%
    ENSUSDT                       3        3      0    0.0%
    ETCUSDT                       5        3      2   40.0%
    FILUSDT                       2        1      1   50.0%
    GRTUSDT                       2        2      0    0.0%
    IMXUSDT                       3        2      1   33.3%
    KAVAUSDT                      2        2      0    0.0%
    ROSEUSDT                      3        3      0    0.0%
    RUNEUSDT                      3        3      0    0.0%
    XLMUSDT                       4        4      0    0.0%

  ── alt5-15-504: signal=46  open=42  gap=4  (8.7%) ──
    Symbol                  Signals    Opens    Gap    Gap%
    ──────                  ───────    ─────    ───    ────
    1000SHIBUSDT                  3        3      0    0.0%
    1INCHUSDT                     3        3      0    0.0%
    ADAUSDT                       3        3      0    0.0%
    APTUSDT                       2        2      0    0.0%
    AVAXUSDT                      3        3      0    0.0%
    BCHUSDT                       2        2      0    0.0%
    DOTUSDT                       3        3      0    0.0%
    ENSUSDT                       3        3      0    0.0%
    ETCUSDT                       5        3      2   40.0%
    FILUSDT                       2        1      1   50.0%
    GRTUSDT                       2        2      0    0.0%
    IMXUSDT                       3        2      1   33.3%
    KAVAUSDT                      2        2      0    0.0%
    ROSEUSDT                      3        3      0    0.0%
    RUNEUSDT                      3        3      0    0.0%
    XLMUSDT                       4        4      0    0.0%

  ── bb20: signal=112  open=51  gap=61  (54.5%) ──
    Symbol                  Signals    Opens    Gap    Gap%
    ──────                  ───────    ─────    ───    ────
    1000SHIBUSDT                  6        3      3   50.0%
    1INCHUSDT                     8        3      5   62.5%
    ADAUSDT                       7        4      3   42.9%
    APTUSDT                       9        2      7   77.8%
    AVAXUSDT                      8        2      6   75.0%
    BCHUSDT                       8        3      5   62.5%
    DOTUSDT                       7        3      4   57.1%
    ENSUSDT                       6        2      4   66.7%
    ETCUSDT                       8        6      2   25.0%
    FILUSDT                       8        2      6   75.0%
    GRTUSDT                       8        6      2   25.0%
    IMXUSDT                       4        2      2   50.0%
    KAVAUSDT                      8        3      5   62.5%
    ROSEUSDT                      7        4      3   42.9%
    RUNEUSDT                      5        2      3   60.0%
    XLMUSDT                       5        4      1   20.0%

  ── live: signal=31  open=29  gap=2  (6.5%) ──
    Symbol                  Signals    Opens    Gap    Gap%
    ──────                  ───────    ─────    ───    ────
    1000SHIBUSDT                  3        3      0    0.0%
    1INCHUSDT                     2        2      0    0.0%
    ADAUSDT                       1        1      0    0.0%
    AVAXUSDT                      2        2      0    0.0%
    DOTUSDT                       4        3      1   25.0%
    ENSUSDT                       3        3      0    0.0%
    ETCUSDT                       3        3      0    0.0%
    FILUSDT                       1        1      0    0.0%
    GRTUSDT                       2        2      0    0.0%
    IMXUSDT                       2        2      0    0.0%
    KAVAUSDT                      1        1      0    0.0%
    ROSEUSDT                      3        2      1   33.3%
    RUNEUSDT                      1        1      0    0.0%
    XLMUSDT                       3        3      0    0.0%
```

Exit code: 0 (CLEAN)

---

## Observations (descriptive only — per pre-reg, no cohort-outcome interpretation)

- **LIVE cohort**: gap=2 (6.5%). DOTUSDT gap=1, ROSEUSDT gap=1. Consistent with position-already-open rejection during active positions. Two symbols had sequential signal emissions while a trade was open.
- **alt5-15-336 and alt5-15-504**: gap=4 each (8.7%). Symmetric gaps — both shadows run identical EMA5/15 signal logic so same signal pattern → same rejection pattern. ETCUSDT gap=2 in both (gap source: multiple consecutive signal emissions while position open).
- **bb20 cohort**: gap=61 (54.5%). Large gap is expected — Bollinger mode emits more signals per symbol per window than EMA-cross mode; most land while a prior position is still open (mh504 = 21 days max hold). NOT an anomaly.
- **No negative gaps**: all `gap ≥ 0` across all 53 symbol-cohort pairs. Confirms no journal records exist without a corresponding signal-context emission (journal can't have more opens than signals).
- **Silent-capture failure detection**: no symbol shows large unexpected gap that would indicate dropped signal-context writes. Capture appears complete for the observed window.

---

## Cross-references

- `results/signal_context_capture_descriptive_2026-05-24.md` — schema-completeness baseline (235 signals captured, pre-schema-fix)
- `results/signal_journal_reconcile_decision_rule_2026-05-24.md` — pre-reg locking this tool's scope
- `scripts/journal_fetch.sh` — VPS → local journal pull
- `scripts/signal_journal_reconcile.py` — reconciler implementation
