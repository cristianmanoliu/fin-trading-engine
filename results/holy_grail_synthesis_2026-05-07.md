# Holy-Grail Hunt — Session Synthesis 2026-05-07

## What we tested today

| | Hypothesis | Result | Verdict |
|---|---|---|---|
| HG1 | Exit framework leaves money on table — partial-TP / stop-to-BE rescues 2-3R losers | 6:1 fixed-RR is empirically near-optimal. Best-case partial-TP at any T ≥ 1 is NEGATIVE (winner-side cost dominates). | **CLOSED** |
| HG2-naive | 4H both-sides captures untapped long-side edge | 4H both-sides nets $121k/yr vs $130k/yr short-only. Longs ADD only +$20k while CANNIBALIZING −$67k of short trades. Net −6.9%. | **CLOSED** |
| HG2-funding | Funding asymmetry is the reason longs fail; remove funding cost and longs become viable | Longs without funding net +$68k/yr (vs +$20k with). Funding adds ~$14k/yr asymmetry to short-only. **But shorts still beat longs 8.7-to-1 even funding-neutral.** Funding is NOT the main driver of side asymmetry. | **CLOSED** |

All three credible "easy holy grail" paths are exhausted on the 5y dataset.

## What this jointly implies

The deployed strategy's edge is:
1. **Real** — survives 6-window walk-forward (mean +$130k/yr, CI [-$111k, +$372k])
2. **Structural to short-side at 4H EMA cross** — not exit-rule luck (HG1), not the
   short-only filter being too aggressive (HG2-naive), not funding asymmetry (HG2-funding)
3. **Captured by the existing implementation** — there is no obvious mechanical
   improvement available on the historical sample

Three plausible underlying mechanisms remain — and they're INDISTINGUISHABLE
from history alone:

- **Sample-bias**: 2020-2025 had heavy bear chunks (Nov21→Nov22 decline). If
  the strategy works because the sample was net-bearish, future shorts may fail.
- **Structural trend-following asymmetry**: in crypto, rallies are sharp + brief
  while declines are sustained. 4H bear-cross may genuinely capture multi-day
  downtrends more reliably than bull-cross captures multi-day uptrends.
- **Wick-stop geometry**: capitulation tops have long upper wicks (where short
  stops live), allowing more room before stop-out than for symmetric long trades.

Forward-paper data discriminates these — backtest cannot.

## What this means for the holy grail framing

The "find the holy grail" mental model assumed there was a meaningful
under-exploited improvement detectable from the existing 5y data. After
six commits worth of analysis today (Bug 4/5 fixes, B1 hour-of-day, A1
bootstrap, A2 slip-cliff, B3 features, HG1 MFE/MAE, HG2 both-sides), the
honest reading is:

**There isn't one to find by sweeping the historical sample further.**

The remaining holy-grail-grade moves require either:

1. **Forward-paper data accumulation** (the rigor frame default) — the next
   60 days of out-of-sample observations will tell us more about which
   mechanism is real than any further backtest sweep can.

2. **Architecture-level changes** that open genuinely orthogonal mechanisms:
   - **HG4 cross-sectional**: rank universe by recent return, long bottom-decile
     + short top-decile. Pure mean-reversion across symbols. Requires per-symbol
     Runner architecture change → portfolio coordinator. Multi-day work.
   - **Funding-rate-as-INDEPENDENT-signal**: standalone Cat F1 entry trigger
     when funding crosses extreme thresholds (NOT a filter on existing signal).
     Different mechanism (position crowding mean-reversion) than EMA cross
     (price momentum). Pre-register single hypothesis + walk-forward.
   - **Multi-symbol confluence**: only fire EMA cross signals when N+ symbols
     cross simultaneously — captures market-wide regime shifts only. Untested.

3. **External data feeds** that aren't in the current pipeline:
   - Onchain data (large transfers, exchange inflows/outflows)
   - Options-derived sentiment (put/call skew, IV term structure)
   - Cross-asset (TOTAL3, BTC.D, SPX correlation)
   These each open a new mechanism axis but require significant infrastructure.

## What was instrumented this session

Even though no holy grail emerged, the session produced lasting infrastructure:

- `signal_context.go` — captures rich state at every signal emission for
  forward-paper pattern matching (~108 trades expected over 60 days)
- `bootstrap_ci.py` — autocorrelation-aware CI estimation
- `slip_cliff.sh` — fine-grained cost sensitivity sweep
- `b3_symbol_run.sh` + `b3_features.py` — universe-wide feature analysis
- `mfe_mae_analysis.py` — exit-policy decomposition framework
- `Stub.MaxFavorableR` always-on tracking (silent bug fix)
- Updated CLAUDE.md kill-rule annotation (cliff at 81bp not 25bp)

Each is reusable infrastructure for the eventual forward-paper analysis cycle.

## Honest recommendation

**Stop hunting and start waiting.** The instrumentation is in place. The
existing strategy has a documented edge with quantified uncertainty. The
ambiguity that remains (sample-bias vs structural vs wick-geometry) is
forward-data-resolvable, not backtest-resolvable.

If the user wants to keep iterating in research mode, the highest-conviction
truly-unspent direction is **funding-rate-as-INDEPENDENT-signal** (Cat F1).
It's a single hypothesis on a documented mechanism (position-crowding
mean-reversion in perp futures), can be pre-registered with a clean
walk-forward decision rule, and is genuinely orthogonal to the deployed
EMA-cross strategy. Engineering: ~2h. Statistical risk: real but bounded
by pre-registration.

If not — the forward-paper window is in flight, the next 4H close fires
in <4h, and the signal-context sidecars will start filling. **That's the
data that matters now.**
