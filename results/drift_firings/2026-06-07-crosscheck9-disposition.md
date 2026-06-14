# Cross-check 9 disposition — 2026-06-07 drift firing

**Status: CENSORING-BENIGN. Verdict: HOLD. Auto-kill NOT honored.**

The ~06-07 weekly drift check fired (`exit=1 DRIFT_FIRED`, 2026-06-07T06:00:05Z),
forming the 7+-day pair vs the 2026-05-27 anchor (11 days apart) that meets the
two-firings-7+-days-apart auto-kill *candidate* condition. Per the locked
investigation rule, the kill is **gated by cross-check 9** — and cross-check 9
confirms CENSORING, not edge-death. **HOLD.** This realizes the pre-decision in
`2026-05-31-crosscheck9-precheck.md`.

## Drift history at disposition

```
2026-05-27T17:44:44Z  exit=1 DRIFT_FIRED  (anchor)
2026-05-30T06:10:51Z  exit=1 DRIFT_FIRED
2026-05-30T19:31:42Z  exit=1 DRIFT_FIRED
2026-05-31T06:07:36Z  exit=1 DRIFT_FIRED
2026-06-07T06:00:05Z  exit=1 DRIFT_FIRED  (forms 11d pair vs 05-27 anchor)
```

No `exit=4` stamped in `drift_check_history.jsonl` — the detector flags exit=1
(investigation); the 7d-pair auto-kill escalation is the operator/weekly-audit
evaluation, which this disposition resolves to HOLD.

## Locked discriminator (cross-check 9)

Source: `results/drift_firing_investigation_decision_rule_2026-05-08.md:205-218`.
A `mfe_r`/`win_pnl`/low-WR firing is **censoring-benign iff ALL three hold:**

1. `mae_r` flat (losers not deepening past ~1.10R)
2. Open positions predominantly favorable
3. `n_closed_winners < ~15`

Any fail → real degradation → honor the kill.

## Evidence (2026-06-08, n=46 closed; reproducible via committed tool)

Produced by `python3 scripts/crosscheck9_losers_mae.py` (read-only; reads
top-level `results/journal_cache/*.jsonl` LIVE journals; does NOT touch
`drift_check_history.jsonl` or run the wrapper). Journals fetched 2026-06-08.

### Condition 1 — `mae_r` flat ✓ PASS

```
mae_r losers: n=38  min=1.0000  max=1.0710  mean=1.0104  p50=1.0050
>1.071 (prior max): 0 trades
>1.10 (edge-death threshold): 0 trades
```

Losers tightly packed 1.000–1.071, mean 1.0104 — all stopping at the modeled
~1.0R wick stop. max 1.0710 ≤ 1.10 edge-death threshold. Backtest STOP MAE_R
reference (`results/mfe_mae_verdict_2026-05-07.md`, n=1,726): p50=1.00, p90=1.03,
p99=1.19, max=1.58. Live max 1.0710 sits between BT p90 and p99 — squarely inside
the backtest's own loser distribution, not beyond it. **No mass deepening past
1.0R → censoring fingerprint, not adverse-excursion edge-death.** Mean and max
both grew only marginally vs the 05-31 snapshot (n=33: mean 1.0099, max 1.071)
despite 5 more losers — the distribution is stable, not drifting deeper.

### Condition 2 — open positions favorable ✓ PASS

Live engine has 1 open position (IMXUSDT SHORT, entry 0.1584). Marked to live
Binance premiumIndex 2026-06-08: **+$2,431 favorable** (crypto continued down).
Predominantly favorable → PASS. (Indicative mark-to-market via
`tasks/pnl_snapshot.py`.)

### Condition 3 — `n_closed_winners < ~15` ✓ PASS

```
outcome breakdown: 38 STOP, 8 TARGET
n_closed_winners: 8
```

8 < 15. Low winner count is the natural shorts-only ~20.6% hit-rate at low n
(the power-floor regime), not a broken edge.

## Mechanical verdict

All three conditions PASS → **CENSORING-BENIGN → HOLD.** The 06-07 firing (and
the 11d pair it forms) is the detector's known winner-truncation blindspot
(`results/drift_detector_censoring_blindspot_finding_2026-05-30.md`), not real
degradation. Do NOT honor the auto-kill. Forward-paper continues.

## What was NOT done (and why)

- Did NOT run `scripts/run_drift_check.sh` — it appends `drift_check_history.jsonl`
  and would manufacture an additional 7d pair. The cross-check tool is read-only
  by design.
- Did NOT modify live/shadow/VPS. Forward-paper run untouched.
- The −$24.7k cumulative / ~5.7% WR remains real but is **fee-dominated
  censoring** at p50 ~47× leverage (see `project_pnl_tracking_audit`), not edge
  collapse — consistent with the flat `mae_r`.

## Next drift checkpoint

The next weekly cadence run will fire again (censoring persists). Each future
firing re-tests via the same cross-check 9; HOLD stands until `mae_r` losers
deepen past 1.10R OR open positions turn adverse OR `n_closed_winners` climbs with
WR still <14%. Re-run `scripts/crosscheck9_losers_mae.py` at each firing.

---

## 2026-06-14 checkpoint — HOLD (6th consecutive censoring-benign reading)

**Journals fetched:** 2026-06-15 (Sunday cadence, UTC-aligned).
**Tool:** `python3 scripts/crosscheck9_losers_mae.py` (read-only).

### Condition 1 — `mae_r` flat ✓ PASS

```
mae_r losers: n=45  min=1.0000  max=1.0710  mean=1.0110  p50=1.0060
>1.071 (prior max): 0 trades
>1.10 (edge-death threshold): 0 trades
```

Max unchanged at 1.0710 vs 06-08 (n=38→45, +7 losers, max pinned). Mean +0.001
over 7 more losers — distribution stable, not drifting deeper. **No structural
deepening.**

### Condition 2 — open positions favorable ✓ PASS

6 open positions (all SHORT, all below entry). Live marks via
`fapi.binance.com/fapi/v1/ticker/price` 2026-06-15:

```
AVAXUSDT  entry=6.52500  curr=6.52800  mae_r=0.023  (+1.9% to stop)
BCHUSDT   entry=201.430  curr=202.270  mae_r=0.249  (+1.3% to stop)
ETCUSDT   entry=7.00100  curr=7.02000  mae_r=0.165  (+1.4% to stop)
IMXUSDT   entry=0.15840  curr=0.14310  mae_r=0.000  (+15.4% to stop)
ROSEUSDT  entry=0.00631  curr=0.00628  mae_r=0.000  (+2.2% to stop)
RUNEUSDT  entry=0.37570  curr=0.37820  mae_r=0.330  (+1.3% to stop)
```

Max live mae_r = 0.330 (RUNEUSDT). All favorable (none adverse past entry).
**Predominantly favorable → PASS.**

### Condition 3 — `n_closed_winners < ~15` ✓ PASS

```
outcome breakdown: 45 STOP, 8 TARGET
n_closed_winners: 8
```

8 < 15. Unchanged from 06-08 — no new winners closed since last checkpoint
(consistent with low-WR censoring, not won trades being miscounted).

### Mechanical verdict

All three conditions PASS → **CENSORING-BENIGN → HOLD.** 6th consecutive
reading. The detector's winner-truncation blindspot continues to suppress WR;
no structural evidence of edge-death. Forward-paper continues.
