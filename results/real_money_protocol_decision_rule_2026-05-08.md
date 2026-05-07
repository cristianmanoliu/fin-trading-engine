# Real-Money Small-Tranche Deployment Protocol — PRE-REGISTERED Decision Rule

**Status:** Locked before forward-paper validation completes. Same discipline
as every analysis pre-reg today — design before observing data.

This is a **methodology pre-registration**, not a hypothesis test. It locks
the staged-deployment rules that will be mechanically applied when
forward-paper validation resolves. The purpose is to remove temptation to
"just slightly modify" criteria after seeing live results.

## Why pre-register before forward-paper resolves

When forward-paper produces a "deploy" signal, three pressures will push
toward riskier choices:

1. **Skip stages**: "Paper worked, just go to $1k directly."
2. **Loosen criteria**: "Slip is just barely over threshold, give it
   another week."
3. **Compress timelines**: "We've already waited months."

Each is a path to overconfident first deployment. Pre-registering today,
when emotional stake is low, locks a structured de-risking ladder that
applies mechanically regardless of what live data looks like.

## Stages

| Stage | Per-trade stake | Peak notional (16 sym all-active) | Purpose |
|:---:|---:|---:|:---|
| STAGE_0 | $0 (paper only) | $0 | Forward-paper validation |
| STAGE_1 | $100 | $1,600 | Validate execution path, real fees, margin behavior |
| STAGE_2 | $300 | $4,800 | Validate slippage scaling at moderate size |
| STAGE_3 | $500 | $8,000 | Validate liquidity at near-deployed size |
| STAGE_4 | $1,000 | $16,000 | Full deployed |

**Exchange**: Binance USDT-Perp only. The strategy is fully validated
there (A1, A3, edge-stability, drift-detector calibration). Adding Bybit
or other venues is a separate next-milestone hypothesis (Cat X gave
backtest replication ROBUST but live cross-exchange is unvalidated).

## Promotion criteria (LOCKED — all must hold for each transition)

### STAGE_0 → STAGE_1 (paper → first real money)

ALL of:
- Forward-paper deploy criteria from CLAUDE.md met:
  - ≥150 closed paper trades
  - ≥60 calendar days net-positive in dollar terms
  - Live PnL ≥ 60% of pro-rated honest-annual ($69k/yr × elapsed × 0.60)
  - Live PnL beats BTC HODL with $32k notional over the same window
  - No single symbol >40% of cumulative paper PnL
- **Drift detector clean for ≥30 consecutive days** at α=0.001 weekly cadence
- Realized round-trip taker fees ≤ 12 bp (vs 10 bp modeled)
- Realized stop-side slippage ≤ 20 bp on losing-trade subsample

### STAGE_1 → STAGE_2 ($100 → $300)

ALL of:
- ≥50 closed real-money trades at STAGE_1
- ≥30 calendar days at STAGE_1
- Realized round-trip fee within 5% of STAGE_1 modeled (≤ 10.5 bp)
- Realized stop-side slip within 20% of modeled (≤ 6 bp on losers)
- Drift detector clean for ≥30 days at α=0.001 weekly cadence
- No single-day net loss exceeding 2× per-trade stake ($200 at STAGE_1)
- Net-positive cumulative at STAGE_1

### STAGE_2 → STAGE_3 ($300 → $500)

ALL of:
- ≥100 closed real-money trades cumulative (STAGE_1 + STAGE_2)
- ≥60 calendar days at STAGE_2
- Realized fee/slip stable: most recent 30-trade window within 10% of
  modeled
- Drift detector clean for ≥45 days at α=0.001 weekly cadence
- Net-positive over most recent 30-day window
- No single-symbol >40% of STAGE_1+STAGE_2 cumulative PnL

### STAGE_3 → STAGE_4 ($500 → $1,000)

ALL of:
- ≥200 closed real-money trades cumulative
- ≥90 calendar days at STAGE_3
- Realized fee/slip stable across STAGE_2+STAGE_3 (90+ day window)
- Drift detector clean for ≥60 days at α=0.001 weekly cadence
- Net-positive cumulative across STAGE_2+STAGE_3
- Annualized realized NET ≥ 50% of pro-rated honest-annual ($69k/yr ×
  elapsed × 0.50)

## Kill criteria (LOCKED — any one fires → STOP entire protocol)

When ANY of these fire, the protocol enters KILLED state:

1. **Drift detector confirmed fire** at α=0.001:
   - Two firings ≥7 days apart, OR
   - Single firing combined with `forward_paper_status.sh` advisory KILL
2. **Realized stop-side slip >30 bp** sustained over ≥30 most recent
   trades (well below A2's 81-bp linear-cost cliff but above the
   25-bp investigation threshold)
3. **Three consecutive calendar days** net-negative each exceeding 5×
   the current stage's per-trade stake (e.g., -$500 each day at STAGE_1)
4. **Single-symbol concentration >50%** of cumulative real-money PnL
   (more permissive than the 40% paper bar since calibration showed 40%
   is natural variance; 50% indicates real concentration risk)
5. **Engine emits any unrecoverable error**: margin call, liquidation,
   exchange-side ban, account suspension, broker/API hard failure
6. **Drawdown >20%** of total real-money PnL over any 60-day window

### When kill fires

- **Immediately stop opening new positions** (set engine to disable signal generation)
- **Allow existing positions to close** at their stops/targets — do NOT
  panic-close (preserves trade-level economics; mass closure crystallizes
  arbitrary losses)
- **Mark the milestone KILLED**
- **No automatic resumption** within this milestone. Resumption requires
  a fresh next-milestone pre-registration with revised protocol
  reflecting whatever we learned from the kill

## Time gates (minimum days at each stage)

These cap promotion speed even if all other criteria pass:

| Stage | Minimum days |
|:---:|---:|
| STAGE_1 | 30 days |
| STAGE_2 | 60 days |
| STAGE_3 | 90 days |
| STAGE_4 | indefinite (steady-state) |

Time gates compound with trade-count gates — both must be satisfied.

Total minimum time STAGE_1 → STAGE_4: 180 days, plus the forward-paper
gate of 60+ days (likely 127 days at fleet rate to hit 150 trades). So
the EARLIEST the strategy reaches full $1k/trade deployment from now
is roughly **307 days from forward-paper start**, or approximately
**2027-03-09** assuming forward-paper started 2026-05-05.

## What is NOT permitted (locked)

- ❌ No skipping stages ($0 → $300 not permitted; $0 → $1k REJECTED)
- ❌ No promotion before time gate, regardless of other criteria
- ❌ No retry after kill within this milestone
- ❌ No size doubling within a stage
- ❌ No "trial run" at higher size to test scaling
- ❌ No cherry-picking which trades count (all real-money trades
     contribute to cumulative metrics)
- ❌ No relaxing kill criteria mid-stage
- ❌ No pausing-and-resuming a stage to wait out a bad period

## What IS permitted

- ✓ STAYING at a stage indefinitely if criteria don't progress
- ✓ Reverting one stage if criteria backslide (e.g., STAGE_3 → STAGE_2
     if realized slip exceeds STAGE_3 thresholds without triggering kill)
- ✓ Adjusting symbol mix at stage boundaries (e.g., dropping a
     persistent loser at STAGE_2 → STAGE_3 transition with documented
     reason — but this requires a separate fresh pre-registration if
     the change is structural; minor opt-out of a delisted/SETTLING
     symbol is operational, not pre-registered)

## Pre-registered priors

Hard to assign priors over a year-long live deployment, but as guideposts:

| Outcome | Prior |
|---|---:|
| Reaches STAGE_4 successfully within 12 months | 25% |
| Reaches STAGE_3 (intermediate success) | 40% |
| Killed at STAGE_1 or STAGE_2 (execution surprises) | 20% |
| Killed at STAGE_3 or STAGE_4 (late-stage degradation) | 15% |

Reasoning: the strategy is robustly validated on backtest. The
main remaining risks are real-money execution (which 5-fold validation
cannot detect) and rare regime events. The 40% prior on STAGE_3 reflects
"the strategy works as expected but real-world friction (margin reuse,
liquidations, market impact at scale) limits achievable scale."

## Cross-references

- `CLAUDE.md ## Forward-paper go/no-go criteria` — STAGE_0 → STAGE_1 gate
- `results/drift_detector_calibration_verdict_2026-05-07.md` — α=0.001 deploy
- `results/drift_detector_time_to_detection_verdict_2026-05-08.md` — weekly cadence rationale
- `results/slip_cliff_verdict_2026-05-07.md` — 81-bp linear cliff (kill threshold context)
- `results/bootstrap_ci_verdict_2026-05-07.md` — annual NET expectation source

## What this pre-registration produces — and what it does NOT

WILL: a locked staged-deployment protocol that applies mechanically when
forward-paper validation resolves. Each promotion or kill decision is a
mechanical rule application, no design choices remaining at decision time.

WILL NOT: predict the outcome. The protocol is the procedure, not the
prediction. Whether the strategy actually reaches STAGE_4 depends on
live market conditions over the next 12+ months.

## End-of-milestone status

This pre-registration is the **last commit** of today's research milestone.
It locks the methodology for the project's next phase (real-money
deployment). When forward-paper resolves and a STAGE_0 → STAGE_1 promotion
is on the table, this protocol applies mechanically.

Until then: the deployed paper engines continue running. The drift
detector remains the operational kill mechanism (run weekly per the
2026-05-08 time-to-detection finding). The kill bar advisories in
forward_paper_status.sh remain investigation triggers. The 7 recovered
positions continue tracking until they resolve.

The historical 2020-2025 backtest investigation is closed. Forward-paper
data accumulation is the next data flow.
