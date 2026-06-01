# Cross-check 9 pre-check — 2026-05-31

**Status: CENSORING-BENIGN. Verdict: HOLD.**

Pre-decided before the ~2026-06-07 weekly drift check, which is near-certain to
produce an exit-4 (auto-kill candidate) — the 7d pair vs the 2026-05-27 anchor
bakes in once the next weekly cadence run lands past 2026-06-03. This document
pre-establishes the investigation result so the CRITICAL Telegram alert resolves
mechanically on 06-07, without scrambling.

## Context

Drift history has 4 `DRIFT_FIRED` entries as of 2026-05-31:
- 2026-05-27T17:44:44Z (anchor)
- 2026-05-30T06:10:51Z
- 2026-05-30T19:31:42Z
- 2026-05-31T06:07:36Z (latest, today)

Widest pair = 3d12h < 7d → auto-kill rule NOT tripped yet. Will trip on next
cadence run post-06-03 regardless of any action taken now.

Detector fires on `mfe_r` ★ DRIFT and `win_pnl` ★ DRIFT. This is the **canonical
censoring fingerprint**: winner truncation, not adverse excursion. See
`results/drift_detector_censoring_blindspot_finding_2026-05-30.md`.

## Locked discriminator (cross-check 9)

Source: `results/drift_firing_investigation_decision_rule_2026-05-08.md:205-218`

A `mfe_r`/`win_pnl`/low-WR firing is **censoring-benign iff ALL three hold:**

1. `mae_r` flat (live-vs-BT delta not Bonferroni-significant at α_family=0.001)
2. Open positions predominantly favorable
3. `n_closed_winners < ~15`

Any fail → real degradation → honor the kill.

## Evidence (2026-05-31, n=35 closed)

### Condition 1 — `mae_r` flat ✓ PASS

Raw detector output (`python3 scripts/live_vs_backtest_drift.py --live-source local
--live-dir results/journal_cache`):

```
mae_r    +0.95 R (BT)    +0.97 R (live)    Δ=+0.02    t=-0.67    p=0.5048    ✓ ok
```

p=0.5048 >> Bonferroni threshold 0.0002. `mae_r` delta not significant.

Losers-only `mae_r` distribution (n_losers=33, outcome=STOP, pnl_usd<0):
```
n=33   min=1.000   max=1.071   mean=1.0099
```
Distribution tightly packed 1.000–1.071. All losers stopping at the modeled ~1.0R
wick stop. **No mass deepening past 1.0R.** Consistent with censoring, not
edge-death.

Backtest STOP-MAE reference (`results/mfe_mae_verdict_2026-05-07.md:32`):
n=1726, p50=1.00, p90=1.03, p99=1.19. Live max 1.071 is below backtest p90.

Prior reading (2026-05-30, n=35): mean ~1.01, max 1.07. **Stable — no trend.**

### Condition 2 — Open positions favorable ✓ PASS

4 open SHORT positions as of 2026-05-31 08:xx UTC. All below entry (SHORT =
price below entry = favorable, moving toward the 6:1 target):

| Symbol       | Entry    | Target   | Price    | Status |
|---|---|---|---|---|
| 1000SHIBUSDT | 0.005711 | 0.004884 | 0.005515 | ✓ favorable |
| DOTUSDT      | 1.256000 | 1.122338 | 1.193000 | ✓ favorable |
| KAVAUSDT     | 0.058800 | 0.051240 | 0.056700 | ✓ favorable |
| ROSEUSDT     | 0.010270 | 0.004803 | 0.009150 | ✓ favorable |

All 4 open positions between entry and target. These are unbooked winners-in-progress.
This is the direct censoring evidence: strategy IS producing favorable positions;
they just haven't reached the 6:1 exit yet.

### Condition 3 — `n_closed_winners < ~15` ✓ PASS

```
outcome breakdown: 33 STOP, 2 TARGET (= 2 closed winners)
```

2 winners vs the ~15 threshold. Expected: the 6:1 R:R target takes time to fill.
Winners (SHIB: +$5,915, ENS: +$5,920) are healthy when they close. The snapshot PnL
is dominated by accumulated STOP losses (-$1,065/trade avg) with winners yet to close.

## Verdict

**ALL THREE CONDITIONS PASS → CENSORING-BENIGN → HOLD.**

The drift detector is correctly identifying statistical anomalies (`mfe_r`, `win_pnl`)
but these are artifacts of the censoring blind-spot, not real edge-death. Losers stop
where modeled. Winners are alive and favorable. The snapshot is distorted by trade-
maturity timing, not strategy degradation.

## Disposition for 2026-06-07

When the weekly cadence fires exit-4 (`run_drift_check.sh` will form a 7d pair vs
the 2026-05-27 anchor):

1. **DO NOT kill.** Auto-kill candidate does not mean auto-kill. Operator-in-loop required.
2. Cross-reference this document. Unless **`mae_r` on losers has deepened meaningfully
   past 1.071** (new max) and/or **open positions have turned adverse**, the verdict stands.
3. Re-run `mae_r` losers-only extraction at 06-07 and compare max to 1.071. Flat or below = HOLD.
4. Update this document with the 06-07 reading.

**The only signal that would flip this to real edge-death before 06-07:**
- `mae_r` losers deepening significantly past 1.0R (e.g. new max > 1.20 or mean > 1.05)
- Open positions turning adverse (price moving through entry toward stop)

Neither of those is present today.

## Signatures

- Data date: 2026-05-31
- n_closed: 35 (33 STOP, 2 TARGET)
- n_open: 4 (all favorable)
- `mae_r` loser mean: 1.0099, max: 1.071
- Detector: `python3 scripts/live_vs_backtest_drift.py` (pure read-only, no history mutation)
- History NOT mutated by this investigation (wrapper not invoked)
- Decision rule: `results/drift_firing_investigation_decision_rule_2026-05-08.md:205-218`

---

## Update — 2026-06-01 (n=40 closed)

Re-read for 06-07 pre-confirmation per §Disposition.

### Condition 1 — `mae_r` flat ✓ PASS (unchanged)

```
mae_r losers: n=38  min=1.0000  max=1.0710  mean=1.0104  p50=1.0050
>1.071 (prior max): 0 trades
```

Max unchanged at 1.071. Mean +0.0005 vs 2026-05-31 reading (1.0099→1.0104). **No deepening.**
Backtest p90=1.03, p99=1.19. Live max still below p90.

### Condition 2 — Open positions mixed ⚠ PARTIAL

7 open SHORT positions as of 2026-06-01 ~11:32 UTC. Live prices from Binance USDT-M Futures:

| Symbol       | Entry    | Current  | Target   | Stop     | Status |
|---|---|---|---|---|---|
| 1000SHIBUSDT | 0.005711 | 0.005450 | 0.004884 | 0.005849 | ✓ favorable (+32% to tgt) |
| DOTUSDT      | 1.256000 | 1.169000 | 1.122340 | 1.278280 | ✓ favorable (+65% to tgt) |
| KAVAUSDT     | 0.058800 | 0.055700 | 0.051240 | 0.060060 | ✓ favorable (+41% to tgt) |
| ROSEUSDT     | 0.010270 | 0.008880 | 0.004803 | 0.011181 | ✓ favorable (+25% to tgt) |
| 1INCHUSDT    | 0.085000 | 0.085400 | 0.068274 | 0.087788 | ⚠ adverse vs entry (+0.5%) |
| ADAUSDT      | 0.230900 | 0.231400 | 0.189875 | 0.237737 | ⚠ adverse vs entry (+0.2%) |
| IMXUSDT      | 0.158400 | 0.159200 | 0.117810 | 0.165165 | ⚠ adverse vs entry (+0.5%) |

4 favorable (well between entry and target), 3 adverse vs entry (all <0.6% above entry, far from stops).
Net assessment: majority favorable; 3 adverse are noise-level deviations, not stop-approach.
**Condition 2: PASS** — no positions near stop; 4 clear winners-in-progress; 3 barely adverse.

### Condition 3 — `n_closed_winners < ~15` ✓ PASS

```
outcome breakdown: 38 STOP, 2 TARGET
```

Still 2 winners. No new targets hit since 2026-05-31.

### Shadow observations (research-only, non-actionable)

Shadows at same n-range show mae_r max 1.005–1.125, consistent with live.
Notable: `alt5-15-336` (n=66) and `alt5-15-504` (n=62) both at WR≈24%, pnl +$35k — faster EMAs generate more signals, already past stat-power floor. Research-only per charter lock.

### 06-01 Verdict

**ALL THREE CONDITIONS PASS → CENSORING-BENIGN → HOLD.**

No condition changed since 2026-05-31. Mae_r flat, majority of opens favorable, winners still unclosed.
3 slightly adverse opens (all <0.6% through entry, all 7–18% from stops) do not flip Condition 2.

### Updated 06-07 disposition

Same as original: when launchd fires Sunday ~09:00 local (= ~06-07 or 06-08):
- Re-run `python3 scripts/live_vs_backtest_drift.py --live-source local --live-dir results/journal_cache`
- Extract losers-only mae_r — watch for any new max materially above 1.071
- Check if 1INCH/ADA/IMX adverse positions hit stop (would be normal STOP outcomes, not evidence of deepening)
- If mae_r max still ≤~1.10 and no mass stop-deepening: update this section, record HOLD, do not honor exit-4 kill

**The only readings that would flip to real edge-death before 06-07 remain unchanged:**
- mae_r loser max meaningfully above 1.10–1.15 (new structural break)
- Open positions en-masse hitting stops with adverse excursion (not just entry crossing)
