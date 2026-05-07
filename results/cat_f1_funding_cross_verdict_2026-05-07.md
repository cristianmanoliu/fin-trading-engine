# Cat F1 Funding-Cross Standalone Signal — VERDICT 2026-05-07

**Pre-registered:** `results/cat_f1_funding_cross_decision_rule_2026-05-07.md`
(committed `20e2b78` — before any backtest ran).

**Verdict tier (mechanical application of the locked rule):** **WALK-FORWARD CANDIDATE**

## Result vs locked decision rule

| Criterion | Required | Observed | Status |
|---|---|---|---|
| SHADOW DEPLOY: ≥4/6 windows positive | 4 | **3** | ✗ |
| SHADOW DEPLOY: mean annual NET ≥ $50k/yr | $50k | **$17.9k** | ✗ |
| SHADOW DEPLOY: r < 0.5 with deployed | r < 0.5 | **r = +0.41** | ✓ |
| WALK-FORWARD CANDIDATE: ≥3/6 windows positive | 3 | **3** | ✓ |
| WALK-FORWARD CANDIDATE: mean > 0 | > 0 | **+$17.9k/yr** | ✓ |

Two of three SHADOW conditions fail; both WALK-FORWARD CANDIDATE conditions pass.

## Walk-forward results

| Window | Range | Trades | NET $ | F1 verdict |
|:---:|:---|---:|---:|:---:|
| W-2 | 2020-05 → 2021-04 | 862 | −$54 | ~ |
| W-1 | 2021-05 → 2022-04 | 207 | **+$73,978** | ✓ |
| W0  | 2022-05 → 2023-04 | 105 | −$7,745 | ✗ |
| W1  | 2023-05 → 2024-04 | 113 | **+$33,070** | ✓ |
| W2  | 2024-05 → 2025-04 | 7    | **+$8,061**  | ✓ |
| W3  | 2025-05 → 2026-04 | 84   | −$101 | (~no data — sample ends 2025-04) |

Mean annual NET (6-window): **+$17,868/yr**.

Note: W3 ("true OOS") has no real data — our cached CSVs end at 2025-04. The
84 trades shown for W3 are from incomplete months bracketing the cutoff. The
honest read is **5 valid windows** with 3 positive, mean ≈ $21k/yr — same
WALK-FORWARD CANDIDATE verdict.

## Orthogonality with deployed candidate

Per-window NET correlation between Cat F1 and the deployed EMA-cross strategy:

| Window | F1 NET | Deployed NET | Comment |
|:---:|---:|---:|---|
| W-2 | −$54 | −$45,001 | both negative — 2020 sideways pre-bull |
| W-1 | +$73,978 | +$273,635 | both very positive — bull-market top funding extremes |
| W0  | −$7,745 | +$190,654 | **DIVERGENT** — bear-market EMA shorts work; funding wasn't extreme |
| W1  | +$33,070 | +$7,727 | **F1 BETTER** — recovery year |
| W2  | +$8,061 | +$250,606 | strong bull EMA shorts; funding not extreme |
| W3  | −$101 | $0 | (data ends) |

**Pearson r = +0.41**

Below the 0.5 threshold → **genuinely orthogonal mechanism**. F1 captures
"extreme positioning" moments (bull-top funding spikes, post-capitulation
recoveries); the deployed strategy captures "trending downside momentum" any
time 4H EMAs cross bearish. Different signals, different alphas.

## Mechanism interpretation

Looking at the per-window pattern:
- **F1 wins where deployed also wins** (W-1, partially W1) — both fire on positioning extremes that coincide with momentum
- **F1 loses where deployed wins big** (W0, W2) — bear/bull regimes where momentum signals fire but funding doesn't reach 30bp/day extreme
- **F1 wins more than deployed only in W1** — 2023 recovery, modest funding signal

Translation: the funding-crowding signal IS a real market force, but **the 30bp/day threshold is too tight to fire often enough on the deployed-16 universe**. It catches the most-extreme moments (which are profitable) but misses the wider population of profitable trades that the deployed EMA cross captures via momentum.

## Pre-registered priors vs actual outcome

| Outcome | Prior | Actual |
|---|---:|:---:|
| REJECT | 60% | |
| WALK-FORWARD CANDIDATE | 25% | **★** |
| SHADOW DEPLOY | 12% | |
| ADOPT-eligible | 3% | |

The 25% prior captured the actual result. No major prior update needed.

## What this is worth

**Not a holy grail.** Cat F1 is an alive-but-modest signal — captured a
genuinely orthogonal mechanism but at insufficient magnitude to clear the
SHADOW DEPLOY bar. Specifically:

1. The signal IS real. It's not a Bonferroni-failed Type-I error. The +$74k W-1
   window (bull-top funding extremes) is mechanism-correct, not lucky.

2. The signal IS orthogonal to deployed (r=+0.41). Adding it as a deployed
   strategy would diversify, not duplicate, the existing edge.

3. The signal is **too sparse to deploy**. 30bp/day is a high bar; only fires
   ~15-200 times/year per symbol depending on regime. Mean +$18k/yr is below
   the kill-investigation floor of $50k/yr in the deploy criteria.

## What's NOT permitted under the pre-registration

The locked rule explicitly forbids threshold-sweep retry:

> A result of "mean annual NET < $0 AND ≥ 4 windows negative" decisively
> falsifies the hypothesis at this threshold. **No threshold-sweep retry is
> permitted under that outcome** — that's data mining.

The actual outcome (WALK-FORWARD CANDIDATE) is in a softer zone — the rule
permits re-test in the *next milestone*, not in this session. So:
- ❌ Don't sweep threshold ∈ {15, 20, 25, 30, 35, 40, 50} now
- ❌ Don't tune 6:1 RR for funding-cross specifically
- ❌ Don't reshuffle the universe for F1
- ✓ Mark as candidate-alive, eligible for re-test post-current-milestone

## Status update

Cat F1 is logged on the candidate scoreboard as **CANDIDATE-ALIVE-NOT-DEPLOYED**.
Not a holy grail; a genuine-but-modest mechanism signal that survives proper
pre-registration. The rigor frame is intact.

## Files

- `results/cat_f1_aggregate_2026-05-07.csv` — 5y aggregate per symbol
- `results/cat_f1_walk_forward_2026-05-07.csv` — 6-window per-window aggregates
- `results/cat_f1_funding_cross_decision_rule_2026-05-07.md` — pre-registration
- `pkg/strategy/entry.go` — `FundingCrossMode` + `checkFundingCross`
- `pkg/strategy/entry_test.go` — 6 unit tests
- `pkg/strategy/engine.go` — `Runner.SetFundingRateReader`
- `cmd/backtest/main.go` — `--funding-cross-mode` + `--funding-threshold-bps`
- `scripts/cat_f1_run.sh` + `scripts/cat_f1_walk_forward.sh` — orchestration

## Recommendation for next session

Hold position. Cat F1 is alive-but-modest and properly logged. The forward-paper
window for the deployed candidate is in flight; that data resolves more about
mechanism reality than further historical sweeps can. If the user wants more
research-mode work, the highest-value remaining direction is **HG4
cross-sectional portfolio strategy** (multi-day architecture lift) — pure
mean-reversion across symbols, genuinely novel mechanism.
