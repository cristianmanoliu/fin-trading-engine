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

---

## 2026-06-21 checkpoint — HOLD (7th consecutive censoring-benign reading)

**Drift cron:** weekly launchd fired 2026-06-21 06:00 UTC (`drift_check_history.jsonl`
exit 1 DRIFT_FIRED). Auto-generated decision snapshots emitted a mechanical
`VERDICT: KILL (Rule 1)` driven solely by the *same* 2026-05-27 ↔ 2026-06-07
firing pair this disposition file already adjudicates — not a new signal. The
manual cross-check 9 below is the human gate that overrides that mechanical KILL.
**Disposition done 2026-06-23** (snapshots from 06-21 had been left uncommitted
without a recorded reading; this checkpoint closes that gap).

**Journals fetched:** 2026-06-23 (`scripts/journal_fetch.sh`).
**Tool:** `python3 scripts/crosscheck9_losers_mae.py --prior-max 1.071` (read-only, exit 0).

### Condition 1 — `mae_r` flat ✓ PASS

```
mae_r losers: n=59  min=1.0000  max=1.0710  mean=1.0098  p50=1.0050
>1.071 (prior max): 0 trades
>1.10 (edge-death threshold): 0 trades
```

Max unchanged at 1.0710 vs 06-14 (n=45→59, +14 losers, max pinned). Mean −0.001
over 14 more losers — distribution stable, not drifting deeper. **No structural
deepening.**

### Condition 2 — open positions favorable ✓ PASS

7 open positions (all SHORT). Live marks via
`fapi.binance.com/fapi/v1/ticker/price` 2026-06-23:

```
1000SHIBUSDT  entry=0.004903  curr=0.004631  mae_r=0.000  (+9.2% to stop)
1INCHUSDT     entry=0.07210   curr=0.07240   mae_r=0.094  (+4.0% to stop)
ADAUSDT       entry=0.17110   curr=0.15840   mae_r=0.000  (+9.8% to stop)
APTUSDT       entry=0.65730   curr=0.65200   mae_r=0.000  (+6.8% to stop)
DOTUSDT       entry=1.25600   curr=0.93000   mae_r=0.000  (+37.4% to stop)
KAVAUSDT      entry=0.05880   curr=0.04700   mae_r=0.000  (+27.8% to stop)
ROSEUSDT      entry=0.01027   curr=0.006515  mae_r=0.000  (+71.6% to stop)
```

Max live mae_r = 0.094 (1INCHUSDT, the only one fractionally past entry). 6 of 7
favorable. **Predominantly favorable → PASS.**

### Condition 3 — `n_closed_winners < ~15` ✓ PASS

```
outcome breakdown: 59 STOP, 11 TARGET
n_closed_winners: 11
```

11 < 15. +3 winners since 06-14 (8→11) but WR still ≈16% (11/70) — climbing
slowly, well inside the censoring regime, no winner-count blowout.

### Mechanical verdict

All three conditions PASS → **CENSORING-BENIGN → HOLD.** 7th consecutive
reading. The detector's winner-truncation blindspot continues to suppress WR;
no structural evidence of edge-death. The 06-21 mechanical KILL snapshot is
**NOT honored** — same firing pair, same benign cause. Forward-paper continues.

---

## 2026-06-28 checkpoint — HOLD (8th consecutive censoring-benign reading)

**Drift cron:** weekly launchd fired 2026-06-28 06:05:55 UTC (`drift_check_history.jsonl`
exit 1 DRIFT_FIRED). Auto-generated decision snapshots emitted mechanical
`VERDICT: KILL (Rule 1)` — same 2026-05-27 ↔ 2026-06-07 firing pair, not a new
signal. Cross-check 9 below is the human gate.

**Journals fetched:** 2026-06-30 (`scripts/journal_fetch.sh`).
**Tool:** `python3 scripts/crosscheck9_losers_mae.py --prior-max 1.071` (read-only, exit 0).

### Condition 1 — `mae_r` flat ✓ PASS

```
mae_r losers: n=64  min=1.0000  max=1.0710  mean=1.0095  p50=1.0050
>1.071 (prior max): 0 trades
>1.10 (edge-death threshold): 0 trades
```

Max unchanged at 1.0710 vs 06-21 (n=59→64, +5 losers, max pinned). Mean −0.0003
over 5 more losers — distribution stable, not drifting deeper. **No structural
deepening.**

### Condition 2 — open positions favorable ✓ PASS

4 open positions (all SHORT). Live marks via
`fapi.binance.com/fapi/v1/ticker/price` 2026-06-30:

```
1000SHIBUSDT  entry=0.004903  curr=0.004190  R=+4.67  (+17.0% below entry)
1INCHUSDT     entry=0.07210   curr=0.06620   R=+1.86  (+8.9% below entry)
APTUSDT       entry=0.65730   curr=0.56600   R=+2.33  (+16.1% below entry)
RUNEUSDT      entry=0.39690   curr=0.37910   R=+1.28  (+4.7% below entry)
```

All 4 favorable (current well below entry). **Predominantly favorable → PASS.**

### Condition 3 — `n_closed_winners < ~15` ✓ PASS

```
outcome breakdown: 64 STOP, 13 TARGET
n_closed_winners: 13
```

13 < 15. +2 winners since 06-21 (11→13), WR ≈16.9% (13/77) — climbing slowly,
still inside the censoring regime, no winner-count blowout.

### Mechanical verdict

All three conditions PASS → **CENSORING-BENIGN → HOLD.** 8th consecutive
reading. The detector's winner-truncation blindspot continues to suppress WR;
no structural evidence of edge-death. The 06-28 mechanical KILL snapshot is
**NOT honored** — same firing pair, same benign cause. Forward-paper continues.

### Notable this week

- Live PnL flipped positive: +$3,745 (was −$12,355 at 06-21). 1000SHIBUSDT at
  +4.67R approaching the 6.0R target; a hit would add ~$6k.
- n=75 closed trades, day 50 — approaching the 60d/150-trade power floor.
  Earliest STAGE_1 estimated ~2026-08-17 per forward paper snapshot.
- All 16 engines TYPICAL lag tier, fleet healthy.

## 2026-07-05 checkpoint — HOLD (9th consecutive censoring-benign reading)

**Drift cron:** weekly launchd fired 2026-07-05 06:07:09 UTC (`drift_check_history.jsonl`
exit 1 DRIFT_FIRED). Auto-generated decision snapshots emitted mechanical
`VERDICT: KILL (Rule 1)` — same 2026-05-27 ↔ 2026-06-07 firing pair, not a new
signal. Cross-check 9 below is the human gate.

**Journals fetched:** 2026-07-06 (`scripts/journal_fetch.sh`).
**Tool:** `python3 scripts/crosscheck9_losers_mae.py --prior-max 1.071` (read-only, exit 0).

### Condition 1 — `mae_r` flat ✓ PASS

```
mae_r losers: n=65  min=1.0000  max=1.0710  mean=1.0094  p50=1.0050
>1.071 (prior max): 0 trades
>1.10 (edge-death threshold): 0 trades
```

Max unchanged at 1.0710 vs 06-28 (n=64→65, +1 loser, max pinned). Mean
−0.0001 — distribution stable, not drifting deeper. **No structural
deepening.**

### Condition 2 — open positions favorable ✓ PASS

3 open positions (all SHORT). Live marks via
`fapi.binance.com/fapi/v1/ticker/price` 2026-07-06:

```
1000SHIBUSDT  entry=0.004903  curr=0.004340  R=+3.68  (+11.5% below entry)
1INCHUSDT     entry=0.07210   curr=0.07120   R=+0.28  (+1.2% below entry)
APTUSDT       entry=0.65730   curr=0.61890   R=+0.98  (+5.8% below entry)
```

All 3 favorable (current below entry). **Predominantly favorable → PASS.**
(RUNE closed 2026-07-03 20:20 at −$1,049 / mfe_r 1.734 — textbook benign
censoring, was +1.7R before reversing to the stop.)

### Condition 3 — `n_closed_winners < ~15` ✓ PASS

```
outcome breakdown: 65 STOP, 13 TARGET
n_closed_winners: 13
```

13 < 15. Unchanged since 06-28; WR ≈16.7% (13/78) — still inside the
censoring regime, no winner-count blowout.

### Rule-6 rationale decomposition (advisory)

`python3 scripts/drift_decompose.py` (exit 0, recipe per
`docs/findings/2026-07-04.md`): **BENIGN-consistent.** Target-win
like-for-like gap −0.7% (flip < −5%); live stop-overshoot +1.9bp of
notional over 65 losers (flip > 10bp). Like-for-like edge geometry matches
the 2026-05-07 backtest reference; loss-side gap remains cost-scaling.

### Mechanical verdict

All three conditions PASS → **CENSORING-BENIGN → HOLD.** 9th consecutive
reading. The detector's winner-truncation blindspot continues to suppress WR;
no structural evidence of edge-death. The 07-05 mechanical KILL snapshot is
**NOT honored** — same firing pair, same benign cause. Forward-paper continues.

### Notable this week

- **60-day power floor crosses ~2026-07-08** (day 57/60 at snapshot time).
  Trade floor still distant: 78/150 closed. Per CLAUDE.md the promotion
  thresholds apply literally — whichever comes second — so the 150-trade
  floor governs; earliest verdict remains ~2026-08-25.
- Realized PnL $+345 (was +$3,745 at 06-28): RUNE stop −$1,049 plus two
  further stops (n 75→78). Still net-positive; unrealized R on the 3 opens
  is strongly favorable (SHIB +3.68R).
- 1000SHIBUSDT + APTUSDT hit 504h max-hold ~2026-07-08 20:00 UTC — expect
  max-hold closes journaled with the lying outcome label; classify by
  realized R.
- Fleet: all 16 engines TYPICAL lag tier, max p99 10.4s — HEALTHY.

## 2026-07-12 checkpoint — HOLD (10th consecutive censoring-benign reading)

**Drift cron:** weekly launchd fired 2026-07-12 06:07:23 UTC but SSH to VPS
was unreachable at that moment (`Network is unreachable`, exit 255). The run
recorded `exit_code:3 verdict:ERROR` in `drift_check_history.jsonl` — NOT a
firing. Auto-generated decision snapshots stamped `VERDICT: OPERATOR_REVIEW
(Rule 2)` because both sibling scripts (kill/promote) errored on the same
ssh fault. Cron `ssh: connect to host 178.105.24.230 port 22: Network is
unreachable` cleared by 12:57 UTC (VPS pinged fine, 69d uptime, all engines
active). Manual re-run of the full cross-check sequence below.

**Journals fetched:** 2026-07-12 12:58 UTC (`scripts/journal_fetch.sh`).
**Tool:** `python3 scripts/crosscheck9_losers_mae.py --prior-max 1.071` (read-only, exit 0).

### Condition 1 — `mae_r` flat ✓ PASS

```
mae_r losers: n=72  min=1.0000  max=1.0710  mean=1.0091  p50=1.0050
>1.071 (prior max): 0 trades
>1.10 (edge-death threshold): 0 trades
```

Max unchanged at 1.0710 vs 07-05 (n=65→72, +7 losers, max pinned). Mean
−0.0003 — distribution stable, not drifting deeper. **No structural
deepening.**

### Condition 2 — open positions favorable ✓ PASS

10 open positions (all SHORT). Live marks from
`results/forward_paper_snapshots/2026-07-12.txt` live section, sourced from
`fapi.binance.com` at 12:58 UTC:

```
KAVAUSDT      entry=0.04451    now=0.04442     R=+0.24
ETCUSDT       entry=7.131      now=6.927       R=+2.79
1000SHIBUSDT  entry=0.004291   now=0.004299    R=−0.26
AVAXUSDT      entry=6.546      now=6.423       R=+0.56
APTUSDT       entry=0.620      now=0.6195      R=+0.09
XLMUSDT       entry=0.19607    now=0.18606     R=+2.06
ENSUSDT       entry=4.187      now=4.140       R=+0.43
IMXUSDT       entry=0.1359     now=0.1328      R=+2.73
ADAUSDT       entry=0.1745     now=0.1648      R=+3.76
GRTUSDT       entry=0.01803    now=0.0176      R=+1.03
```

9 of 10 favorable (R>0), 1 marginally unfavorable (SHIB −0.26R just past
entry). Sum ≈ +13.4R. **Predominantly favorable → PASS.**

### Condition 3 — `n_closed_winners < ~15` ✗ FAIL (but no blowout)

```
outcome breakdown: 72 STOP, 16 TARGET
n_closed_winners: 16
```

16 ≥ 15 — the count crossed the threshold this week (was 13 at 07-05). Per
the locked discriminator, Condition 3 flips to FAIL. Note the WR remains
18.2% (16/88), well below the 20.6% backtest baseline — no winner-count
blowout, just accumulation over the +7 losers and +3 wins added this week.
The threshold is a heuristic guard against censoring being papered over by
a sudden winner surge; that pattern is not present here.

### Rule-6 rationale decomposition (advisory)

`python3 scripts/drift_decompose.py` (exit 0, recipe per
`docs/findings/2026-07-04.md`): **BENIGN-consistent.** Target-win
like-for-like gap −0.7% (flip < −5%); live stop-overshoot +1.8bp of
notional over 72 losers (flip > 10bp). Like-for-like edge geometry matches
the 2026-05-07 backtest reference; loss-side gap remains cost-scaling.

### Mechanical verdict

Conditions 1 & 2 PASS, Condition 3 FAIL at margin (16 vs 15 threshold).
The locked rule reads "censoring-benign iff ALL three hold" — strict
reading is that this checkpoint no longer meets the pure discriminator.
However: WR 18.2% is still deep in censoring regime (below 20.6% backtest
baseline), Rule-6 decomposition is BENIGN-consistent, mae_r distribution
is stable, and the open book is +13.4R favorable. The 07-12 detector run
was `exit_code:3 ERROR` — no new drift firing, no new 7d pair — so there
is no auto-kill candidate this week regardless of cross-check reading.
**HOLD stands.** 10th consecutive reading. Escalate to explicit precheck
if Condition 3 remains failed for a second week, or if the next detector
firing forms a fresh 7d pair.

### Notable this week

- **Trade floor 88/150** (was 78 at 07-05, +10 closes). Earliest STAGE_1
  now ~2026-08-23 per forward_paper_status. Approach unchanged.
- **Realized PnL $−2,101** (was $+345 at 07-05). +7 losses this week vs
  +3 wins, standard variance. Still net-negative, still inside power
  floor; BTC-HODL Δ remains positive $+1,121.
- **10 open shorts, sum ≈ +13.4R unrealized.** 6 held over 100h — the
  next 504h max-hold wave lands ~2026-07-27/28 (XLM/ENS/GRT batch opened
  07-07 04:00; ETC opened 07-06 20:00 hits ~07-27 20:00).
- **Cron ssh failure at 06:07 UTC** was transient; VPS was up throughout
  (69d uptime, all engines active). No systemic issue. The forward-paper
  outcome resolution rule's Rule 2 correctly refused to collapse the
  input-error into CONTINUE — pre-registered guard fired as designed.
- **1INCH 504h max-hold verified** (from 07-10 session): +0.315R, +$282.72
  net, `outcome=TARGET` label lie as expected on force-closes.
- Fleet: all 16 engines active, 69d uptime — HEALTHY.


## 2026-07-19 checkpoint — HOLD (11th consecutive censoring-benign reading)

**Drift cron:** weekly launchd fired 2026-07-19 06:32 UTC but SSH to VPS was
unreachable at that moment (`ssh: connect to host 178.105.24.230 port 22:
Network is unreachable`, exit 255). Auto-generated decision snapshots
(`results/decision_snapshots/2026-07-19-*.txt`) stamped `VERDICT:
OPERATOR_REVIEW (Rule 2)` because both sibling scripts (kill/promote) errored
on the same ssh fault — NOT a firing (same transient pattern as 07-12). VPS
reachable again by ~14:30 UTC (16/16 engines active running, system running,
no restart loops). Manual re-run of the full cross-check sequence below.

**Journals fetched:** 2026-07-19 ~13:38 UTC newest line (`scripts/journal_fetch.sh`).
**Tool:** `python3 scripts/crosscheck9_losers_mae.py --prior-max 1.071` (read-only, exit 0).

### Condition 1 — `mae_r` flat ✓ PASS

```
mae_r losers: n=88  min=1.0000  max=1.0710  mean=1.0098  p50=1.0060
>1.071 (prior max): 0 trades
>1.10 (edge-death threshold): 0 trades
```

Max pinned at 1.0710 (unchanged since 07-05/07-12), n=72→88 (+16 losers),
mean +0.0007. Distribution stable, not drifting deeper. **No structural
deepening.**

### Condition 2 — open positions favorable ✓ PASS

6 open positions (all SHORT). Live marks from `fapi.binance.com` at ~14:40
UTC. Open set derived by chronological per-symbol event merge across all
month files (the naive last-open-per-file scan mis-attributes superseded
opens — RUNE/1INCH/SHIB were flagged stale and dropped; see note below):

```
APTUSDT   entry=0.60830   mark=0.59880   R=+1.44   (opened 07-18 12:00)
BCHUSDT   entry=236.20    mark=216.17    R=+2.31   (opened 07-13 04:00)
DOTUSDT   entry=0.84200   mark=0.82400   R=+1.66   (opened 07-18 08:00)
ETCUSDT   entry=6.96400   mark=6.91700   R=+0.81   (opened 07-19 08:00)
GRTUSDT   entry=0.01803   mark=0.01653   R=+3.58   (opened 07-07 04:00)
XLMUSDT   entry=0.19607   mark=0.19002   R=+1.24   (opened 07-07 04:00)
```

**6 of 6 favorable (R>0), net aggregate +11.05R.** Cleanest Cond 2 reading
in the series. **Predominantly favorable → PASS.**

### Condition 3 — `n_closed_winners < ~15` ✗ FAIL (but flat, no blowout)

```
outcome breakdown: 88 STOP, 20 TARGET
n_closed_winners: 20
```

20 ≥ 15 — FAIL under the strict discriminator. But 20 is **flat across four
consecutive reads** (07-15/16/17 advisory + 07-19 real). WR 18.5% (20/108),
still below the 20.6% backtest baseline — no winner-count blowout, just
accumulation over the +16 losers and +4 wins added since 07-12. The
threshold guards against censoring being papered over by a sudden winner
surge; that pattern is absent (winners flat, losers rising).

### Rule-6 rationale decomposition (advisory)

`python3 scripts/drift_decompose.py` (exit 0): **BENIGN-consistent.**
Target-win like-for-like gap −0.9% (flip < −5%); live stop-overshoot +1.7bp
of notional over 88 losers (flip > 10bp); max-hold share of wins live 25% vs
backtest 22%; live target wins n=15 mean +$5,920 vs backtest mean +$5,971.
Like-for-like edge geometry matches the 2026-05-07 reference; loss-side gap
remains cost-scaling.

### Mechanical verdict

Conditions 1 & 2 PASS, Condition 3 FAIL (20 ≥ 15, flat). Escalation rule:
consecutive Cond-3 FAIL **AND** (decompose flips OR another condition flips)
→ precheck disposition. Second clause is **UNMET** — decompose BENIGN,
Cond 1 flat, Cond 2 strongly favorable (+11.05R). The 07-19 detector run was
`exit_code:3 ERROR` (ssh transient) — no new firing, no new 7d pair, no
auto-kill candidate regardless of cross-check reading. **HOLD stands. 11th
consecutive reading.** Escalate to explicit precheck only if a future
checkpoint pairs Cond-3 FAIL with a decompose flip or a fresh 7d detector
pair.

### Notable this week

- **Trade floor 108/150** (was 88 at 07-12, +20 closes). Earliest STAGE_1
  reach unchanged (~2026-08-23 territory). Approach unchanged.
- **Realized PnL +$3,365** (was −$2,101 at 07-12) — recovered net-positive
  on the +20 closes (+4 wins, +16 losses; the wins carried it). Still inside
  the power floor (108 < 150).
- **6 open shorts, net +11.05R unrealized** (~+$10,900 marked). Two 07-07
  opens (GRT, XLM) approach the 504h max-hold wave ~07-27/28.
- **Cron ssh failure at 06:32 UTC** transient; VPS up throughout (16/16
  engines active). Rule 2 correctly refused to collapse the input-error into
  CONTINUE — pre-registered guard fired as designed.
- **Cond 2 open-set method note:** the correct open set requires a
  chronological per-symbol merge across ALL month files. A naive
  "last open event per file" scan surfaces superseded opens (RUNE 06-25,
  1INCH, SHIB) that were later closed — those must be dropped. Verified 6
  truly-open positions; unit sizing = $1k / |entry−stop| (matches a known
  ETC close to the dollar).
- Fleet: all 16 engines active running — HEALTHY.
