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

---

## Update — 2026-06-02 (n=41 closed) — 06-07 PRE-STAGE

Re-read for 06-07 pre-confirmation. Drift history UNCHANGED since 2026-05-31
(4 fires, anchor 2026-05-27, latest 2026-05-31T06:07:36Z). **The post-06-03
weekly cadence run has NOT landed yet** — exit-4 7d-pair still pending as
predicted. Detector run pure read-only (`live_vs_backtest_drift.py`, wrapper
NOT invoked, history NOT mutated).

### Detector output (2026-06-02, live n=37 in detector window)

```
pnl_per_trade   +310.89 → -730.29   Δ-1041.18   p=0.0001   ★ DRIFT
mfe_r             +2.29 → +0.94      Δ-1.36       p=0.0000   ★ DRIFT
mae_r             +0.95 → +0.97      Δ+0.02       p=0.4204   ✓ ok
loss_pnl_abs   +1065.34 → +1110.17   Δ+44.83      p=0.0002   · single-test
win_pnl        +5129.12 → +5917.66   Δ+788.54     p=0.0000   ★ DRIFT
WR                22.2% → 5.4%        Δ-16.81pp    p=0.0143   ✓ ok
```

Same canonical censoring fingerprint: `mfe_r`★ + `win_pnl`★, `mae_r`✓ flat
(p=0.4204 >> 0.0002). Winner truncation, not adverse excursion.

### Condition 1 — `mae_r` flat ✓ PASS (unchanged)

```
mae_r losers: n=38  min=1.0000  max=1.0710  mean=1.0104  p50=1.0050
>1.071 (prior max): 0 trades
>1.10 (edge-death threshold): 0 trades
```

**Max unchanged at 1.0710. Mean unchanged at 1.0104** vs 2026-06-01. Zero
deepening across 3 readings (05-31, 06-01, 06-02). Backtest p90=1.03, p99=1.19;
live max still below backtest p90.

### Condition 2 — Open positions ✓ PASS (IMPROVED vs 06-01)

6 open SHORT (was 7; **DOTUSDT closed TARGET — see below**). Live Binance prices:

| Symbol       | Entry    | Current  | Target   | Stop     | %→target | Status |
|---|---|---|---|---|---|---|
| 1000SHIBUSDT | 0.005711 | 0.005434 | 0.004884 | 0.005849 | 33.5% | ✓ favorable |
| KAVAUSDT     | 0.058800 | 0.054900 | 0.051240 | 0.060060 | 51.6% | ✓ favorable |
| ROSEUSDT     | 0.010270 | 0.008980 | 0.004803 | 0.011181 | 23.6% | ✓ favorable |
| ADAUSDT      | 0.230900 | 0.222900 | 0.189875 | 0.237737 | 19.5% | ✓ favorable (flipped fav since 06-01) |
| 1INCHUSDT    | 0.085000 | 0.083800 | 0.068274 | 0.087788 |  7.2% | ✓ favorable (flipped fav since 06-01) |
| IMXUSDT      | 0.158400 | 0.158800 | 0.117810 | 0.165165 | -1.0% | ⚠ adverse-vs-entry (94% buffer to stop) |

**5/6 favorable (was 4/7 on 06-01).** 1INCH + ADA flipped from adverse→favorable.
IMX sole adverse, -1.0% above entry, still 94% of entry→stop gap as buffer — noise,
not stop-approach. Condition 2 strengthened.

### Condition 3 — `n_closed_winners < ~15` ✓ PASS

```
outcome breakdown: 38 STOP, 3 TARGET (= 3 closed winners)
```

3 winners vs the ~15 threshold. **New winner since 06-01: DOTUSDT booked TARGET
2026-06-02 07:03:15Z, +$5,944.38, mfe_r=6.015, mae_r=0.988.** This is the direct
censoring prediction realized — the 06-01 doc flagged DOT as "+65% to target,
favorable winner-in-progress"; it matured to the 6:1 exit exactly as the censoring
thesis required. Three winners now: SHIB +$5,915, ENS +$5,920, DOT +$5,944 — all
~6:1, all healthy at close.

### 06-02 Verdict

**ALL THREE CONDITIONS PASS → CENSORING-BENIGN → HOLD.**

Stronger than 06-01: mae_r still flat (zero deepening, 3 readings), opens improved
to 5/6 favorable, and a third winner booked at full 6:1 — the censoring blind-spot
made concrete. No edge-death signal present.

### 06-07 disposition (mechanical — when launchd fires Sunday exit-4)

1. **DO NOT honor the kill.** Auto-kill candidate ≠ auto-kill. Operator-in-loop.
2. `bash scripts/journal_fetch.sh` → `python3 scripts/live_vs_backtest_drift.py
   --live-source local --live-dir results/journal_cache` (read-only; **never** the
   wrapper `run_drift_check.sh` — it appends history and manufactures the 7d pair).
3. Extract losers-only mae_r. **Flip-to-edge-death triggers (else HOLD):**
   - new max materially > 1.10–1.15, or mean > 1.05
   - open positions en-masse hitting stops with adverse excursion
4. Watch IMX: if it hits stop, that's a normal STOP outcome (mae_r ~1.0), NOT
   evidence of deepening. Only mass adverse-stop or a structural mae_r break flips.
5. Append a 06-07 section here; record HOLD if conditions hold.

Three consecutive readings (05-31, 06-01, 06-02) all CENSORING-BENIGN with mae_r
pinned at 1.071 max. The 06-07 exit-4 resolves to HOLD unless a structural break
appears in the next 5 days.

### Tooling — committed cross-check producer (2026-06-02)

The Condition-1 (losers mae_r) + Condition-3 (winner count) blocks above are now
produced by a committed, tested, read-only script instead of an ephemeral /tmp
recipe:

```
bash scripts/journal_fetch.sh root@178.105.24.230   # refresh cache (read-only rsync)
python3 scripts/crosscheck9_losers_mae.py            # emits the doc blocks + verdict
```

Exit codes: 0 = HOLD-consistent (losers mae_r max ≤ 1.10 AND mean ≤ 1.05);
1 = FLIP candidate (structural deepening — escalate to honor-kill review);
2 = NO DATA. Reads top-level `results/journal_cache/*.jsonl` only (LIVE engine;
subdirs skipped by construction). Does NOT touch `drift_check_history.jsonl` and
does NOT run the `run_drift_check.sh` wrapper. Validated 2026-06-02: reproduces
the 06-02 block byte-for-byte (n=38, max=1.0710, mean=1.0104, p50=1.0050).

**Condition 2 (open positions favorable) is still operator-checked** — it needs
live marks (`GET https://fapi.binance.com/fapi/v1/ticker/price`), which the script
deliberately does not fetch. On 06-07: run the script for Conditions 1 & 3, do the
manual price check for Condition 2, then append a 06-07 section and record the
disposition.

Tests: `python3 -m unittest scripts.test_crosscheck9_losers_mae` (9 tests, incl.
subdir-leak guard + FLIP-trigger guards).

---

## Update — 2026-06-04 (n=44 closed) — 06-07 PRE-CONFIRMATION (4th reading)

Operator-requested re-run (`run cross-check 9`). Drift history UNCHANGED since
2026-05-31 (4 fires, anchor 2026-05-27, latest 2026-05-31T06:07:36Z). **Post-06-03
weekly cadence run still has NOT landed** — exit-4 7d-pair remains pending as
predicted. Conditions 1 & 3 via committed `scripts/crosscheck9_losers_mae.py`
(read-only, exit 0); Condition 2 via single-batch `fapi.binance.com/fapi/v1/ticker/price`.
Wrapper `run_drift_check.sh` NOT invoked; `drift_check_history.jsonl` NOT mutated.

### Condition 1 — `mae_r` flat ✓ PASS (unchanged, 4th identical reading)

```
mae_r losers: n=38  min=1.0000  max=1.0710  mean=1.0104  p50=1.0050
>1.071 (prior max): 0 trades
>1.10 (edge-death threshold): 0 trades
```

**Byte-identical to 05-31 / 06-01 / 06-02.** n=38, max=1.0710, mean=1.0104 — zero
loser deepening across four readings spanning 4 days. No new losers since June 1
(all June closes were wins). Backtest p90=1.03, p99=1.19; live max still below p90.

### Condition 2 — Open positions favorable ✓ PASS (STRONGEST reading — 3/3)

3 open SHORT (was 6 on 06-02; KAVA/ADA/SHIB booked TARGET in June, ROSE still open).
Live Binance USDT-M marks 2026-06-04 ~18:51 UTC:

| Symbol    | Entry    | Current  | Target   | Stop     | %→target | Status |
|---|---|---|---|---|---|---|
| 1INCHUSDT | 0.085000 | 0.076200 | 0.068274 | 0.087788 | 52.6% | ✓ favorable |
| IMXUSDT   | 0.158400 | 0.145800 | 0.117810 | 0.165165 | 31.0% | ✓ favorable (flipped fav since 06-02) |
| ROSEUSDT  | 0.010270 | 0.007590 | 0.004803 | 0.011181 | 49.0% | ✓ favorable |

**3/3 favorable (was 5/6 on 06-02).** IMX — the sole adverse position on 06-02 —
has flipped to 31% toward target. No position near its stop. All three are
winners-in-progress.

### Condition 3 — `n_closed_winners < ~15` ✓ PASS

```
outcome breakdown: 38 STOP, 6 TARGET
n_closed_winners: 6
```

6 winners vs the ~15 threshold (was 3 on 06-02). **Three new winners since 06-02,
all booked at full ~6:1 in the June regime flip:** DOTUSDT +$5,944 (06-02),
KAVAUSDT +$6,008 (06-04), ADAUSDT +$5,977 (06-04), 1000SHIBUSDT +$5,866 (06-04).
The censoring thesis realized again: positions flagged favorable-in-progress in the
06-02 doc (SHIB, KAVA) matured to the 6:1 exit. June live: 7T 4W/3L, WR 57%,
+$20,531 — the regime that produced the May censoring has turned.

### 06-04 Verdict

**ALL THREE CONDITIONS PASS → CENSORING-BENIGN → HOLD.**

Strongest reading in the series: mae_r still pinned (4 identical readings), opens
now 3/3 favorable, winner count doubled 3→6 with three fresh 6:1 books. No
edge-death signal anywhere. The censoring blind-spot interpretation is now
corroborated by the regime flip — what looked like edge-death in May was winner
truncation; June booked the suppressed winners.

### 06-07 disposition (UNCHANGED — mechanical when launchd fires Sunday exit-4)

1. **DO NOT honor the kill.** Auto-kill candidate ≠ auto-kill. Operator-in-loop.
2. `bash scripts/journal_fetch.sh root@178.105.24.230` → `python3 scripts/crosscheck9_losers_mae.py`
   (Conditions 1 & 3) + single-batch `fapi.binance.com/fapi/v1/ticker/price` (Condition 2).
   **Never** the wrapper `run_drift_check.sh` (appends history → manufactures the 7d pair).
3. Flip-to-edge-death triggers (else HOLD): losers mae_r new max > 1.10–1.15 OR
   mean > 1.05; OR open positions en-masse hitting stops with adverse excursion.
4. Append a 06-07 section; record HOLD if conditions hold.

Four consecutive readings (05-31, 06-01, 06-02, 06-04) all CENSORING-BENIGN, mae_r
pinned at 1.071 max. The 06-07 exit-4 resolves to HOLD barring a structural break
in the next ~3 days.
