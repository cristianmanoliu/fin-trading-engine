# 2026-05-19 research synthesis — six pre-registered sweeps

**Status:** Operator-facing synthesis. NOT a deployment authorization. The 6 individual sweep docs are the data of record; this synthesizes their findings for cross-reference and future-session navigation.

## TL;DR

Six pre-registered walk-forward sweeps run during operator-requested research while waiting for forward-paper data. **No finding triggered any deployment.** LIVE config is mechanically defensible against every test class.

| # | Sweep | Cells | Headline finding |
|---:|---|---:|---|
| 1 | EMA × Timeframe | 11 | 1 cell (4H 5/21) nominally beats LIVE +34%; 1D weaker than 4H; MACD-classic mid-pack |
| 2 | 1D Bias Confluence | 5 | Clean refutation — all 4 variants lose −23% to −59% |
| 3 | Vol Regime Filter | 6 | Tight loses, loose ties; May-7 SUPPORTIVE doesn't replicate at slip=5 |
| 4 | Side Filter | 3 | STRONG confirmation of shorts-only; longs catastrophic in W3 (−$380k) |
| 5 | Multi-Level TP | 6 | Variance-reduction trade-off; all positive; cell 3 (4R/0.5) best at −18% |
| 6 | Trailing Stop | 6 | Loose trail (3R) least-bad at −9%; tight trail breaks W3; trail beats MLTP at loose thresholds |

**Total measurements:** 37 cells × 3 windows × 57 symbols = ~110 backtest runs in ~6 hours wall-clock.

## Cross-cutting findings

### 1. LIVE config is the surviving configuration

The baseline (4H EMA 9/21, mh504, shorts-only, wick stop, 6:1 RR, slip=5/fee=10) appears in every sweep's cell 0. Across all 6 sweeps, the baseline:
- Always returns **+$295,292 mean NET** (3/3 windows positive)
- Always wins W3 (+$65k — the only positive W3 against many variants that turn it negative)
- Is the choice that no perturbation decisively beats on a robust metric

This is itself an important finding: **the LIVE configuration is not arbitrary.** It survives 31+ structural perturbations spanning entry parameters, entry filters, side selection, and exit mechanisms.

### 2. The W3 (2025-05 → 2026-04) collapse pattern

Five of the six sweeps tested mechanisms that affect W3 amplitude. The pattern is now firmly established:

| Sweep | Mechanism class | W3 baseline → variants |
|---|---|---|
| #2 (confluence) | Entry bias filter | +$65k → −$25k to −$148k |
| #3 (vol tight) | Entry regime filter | +$65k → −$24k to −$26k |
| #4 (longs/both) | Strategy direction | +$65k → −$206k to −$380k |
| #5 (MLTP) | Exit mechanism (partial) | +$65k → +$28k to −$10k (stable) |
| #6 (trail) | Exit mechanism (full) | +$65k → +$52k to −$154k (tight kills, loose survives) |

**Pattern:** The 2025-2026 walk-forward year punishes ANY tight-threshold modification of the LIVE strategy. The two mechanisms that DON'T catastrophically break W3 are:
- **MLTP across all settings** (variance reduction protects W3)
- **Loose trail (3R)** (almost never fires, doesn't disturb structure)

Strategic implication: future operators considering ANY structural change should expect W3 hostility unless the change is specifically designed for regime transitions.

### 3. Cross-mechanism insight: trail beats MLTP at loose thresholds

The most novel finding of the day is the relationship between the two major exit mechanisms:

| Activation R | MLTP drag (partial close) | Trail drag (full close on reversal) |
|---:|---:|---:|
| 2R | −41% | −29% |
| 3R | −28% | −9% |
| 4R | −18% | n/a (untested at 4R) |

At loose thresholds, trail wins because:
- For trades that pass threshold and continue to 6R: trail keeps full position riding (only moves stop); MLTP already closed partial position at lower R
- For trades that pass threshold then reverse: trail exits at break-even (full); MLTP locked half at +R but other half went to stop

At loose 3R activation, most trades that hit the threshold continue to 6R, so trail's "ride the whole position" beats MLTP's "lock partial, let rest ride."

This is the first time today a finding came from COMPARING sweeps, not just analyzing one in isolation.

### 4. Cost-model fragility

Sweep #3 (vol filter) and sweep #6 (trail) both have prior single-point tests from 2026-05-07 at slip=15bp. The replication results at current cost model (slip=5):

| Prior test | May 7 verdict | Today's relative verdict | Conclusion |
|---|---|---|---|
| vol-1.20 (E1) | SUPPORTIVE | −25% vs baseline | Does NOT replicate — verdict flipped |
| trail-1.0R (B1) | REJECTED | −74% vs baseline | Replicates direction; magnitude amplified |

Lesson: **backtest verdicts can flip when cost model assumptions change**. Same as the 2026-05-05 Option C fee-illusion finding. Any future "interesting backtest finding" must be re-verified at production cost model before being taken seriously.

### 5. Documented trade-off frontier (filed for future risk-management decisions)

If a future stage imposes a per-window-variance constraint (drawdown caps, Sharpe targets, regulatory limits), the documented options are:

| Configuration | Mean drag vs LIVE | Side benefit |
|---|---:|---|
| LIVE (no modification) | 0% | Maximum mean NET |
| MLTP cell 3 (mid-4R, frac 0.5) | −18% | W2/W3 amplitude reduction ~70%; W3 stable |
| Trail cell 5 (TRAIL_R 3.0) | −9% | Downside protection on the rare trades that reach +3R then reverse |
| MLTP cell 4 (mid-3R, frac 0.33) | −21% | Smoother amplitude reduction with smaller upside loss |

**Under current setup (no constraints), LIVE wins.** Under any hypothetical constraint, these are the documented options.

### 6. Parameter family observations (non-decision)

From sweep #1, the 4H EMA family clusters around baseline performance:

| Variant | Mean NET | Trade count |
|---|---:|---:|
| 4H 5/21 | +$395k (+34%) | 7,128 (+28%) |
| 4H 9/21 LIVE | +$295k (reference) | 5,572 |
| 4H 7/14 | +$294k | 7,402 |
| 4H 12/26 (MACD) | +$229k | 4,394 |
| 4H 10/30 | +$193k | 4,501 |
| 4H 21/50 | +$112k | 2,356 |
| 4H 8/34 | +$157k (NEGATIVE — 1/3 windows) | 4,736 |

Cell 4 (5/21) is the nominal winner but at 28% more trades (more cost surface, more amortization). Could be real edge OR multi-comparison artifact. **Filed as research observation; not actionable per locked decision rule.**

## What this synthesis explicitly does NOT do

- Does NOT alter the locked decision rules of any individual sweep. Each sweep's pre-reg stands.
- Does NOT authorize deployment of any cell from any sweep.
- Does NOT propose follow-up sweeps. Any follow-up requires fresh pre-reg.
- Does NOT replace the data-of-record in individual sweep docs. This is a navigation aid.

## Forward-paper status (unchanged by research)

- LIVE: 16 engines on stub, 4H EMA 9/21 mh504 shorts. PnL day 11/60: -$5,340 across 17 trades.
- 3 shadows: alt5-15-336, alt5-15-504, bb20.
- Layer 3 wrap active on KAVA+ENS (since 2026-05-19 07:55 UTC).
- STAGE_1 ETA: ~2026-08-13.
- Drift detector: clean (last run 2d ago).
- Layer 3 cron: installed Sunday 10:00 UTC weekly.

## How to read this artifact

**For future-me or future-operator considering "should we change X?":**
1. Check if X was tested today (table at top, find the relevant sweep).
2. Open the sweep doc for full results + decision rule.
3. Apply the locked decision rule (research-only, non-actionable).
4. If the question is genuinely new, write a fresh pre-reg.

**For session-handoff:** the 6 sweep docs + this synthesis are permanent research artifacts in `results/`. The day's research is locked in git.

## Index of all 6 sweep artifacts

- `results/ema_tf_exploratory_grid_2026-05-19.md` + `.csv` + `scripts/ema_tf_exploratory_sweep.sh`
- `results/confl_1d_bias_sweep_2026-05-19.md` + `.csv` + `scripts/confl_1d_bias_sweep.sh`
- `results/vol_filter_sweep_2026-05-19.md` + `.csv` + `scripts/vol_filter_sweep.sh`
- `results/side_filter_validation_2026-05-19.md` + `.csv` + `scripts/side_filter_validation_sweep.sh`
- `results/mltp_exit_sweep_2026-05-19.md` + `.csv` + `scripts/mltp_exit_sweep.sh`
- `results/trailing_stop_sweep_2026-05-19.md` + `.csv` + `scripts/trailing_stop_sweep.sh`
- `results/research_synthesis_2026-05-19.md` — this doc

Each sweep doc contains: context, locked grid, held-constant parameters, walk-forward windows, pre-registered predictions, decision rule, results table, mechanism interpretation, what changes/doesn't change, future-session reading note.
