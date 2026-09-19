# VERDICT — EMA 5/15 crash overlay on mid/small-cap alts

**Pre-registration:** `crash_overlay_revival_prereg_2026-09-19.md` (locked
2026-09-19, before this evaluation).

**Backtest data:** EMA 5/15, 4H signal, short only, 6:1 R:R, max-hold 504h,
fee 10bp RT + slip 5bp, funding CSVs, exact fills, include boundary. Full
57-symbol universe run 2026-09-19. Book narrowed by top-10 exclusion + Kraken
slippage screen to 13 symbols.

**Stake:** $500 (notional ~$28k per trade).

---

## The 13-symbol book

1000SHIB, AAVE, ARB, BCH, CRV, HBAR, INJ, LTC, NEAR, SUI, UNI, WLD, XLM.

3,171 trades over 2020-01 to 2026-07. NET +$128,334 (+$19,744/yr).
Gross +$245,442. Costs $117,109 (48% of gross).

---

## Gate evaluation

### C1. BTC correlation < -0.3

Quarterly correlation(BTC return, strategy P&L) = **-0.514**.

26 quarters, 9 with BTC < -5%. The strategy consistently pays when BTC drops
and bleeds when BTC rises.

**PASS.**

### C2. Crash-quarter reliability >= 80%

**9 of 9 BTC-down quarters positive (100%).**

| Quarter | BTC return | Strategy P&L |
|---|---|---|
| 2020-Q1 | -11.0% | +$5,345 |
| 2021-Q2 | -40.4% | +$6,117 |
| 2022-Q2 | -56.9% | +$24,229 |
| 2022-Q4 | -14.3% | +$16,675 |
| 2023-Q3 | -11.8% | +$14,157 |
| 2024-Q2 | -9.9% | +$46,161 |
| 2025-Q1 | -12.8% | +$53,671 |
| 2025-Q4 | -26.1% | +$17,414 |
| 2026-Q1 | -23.2% | +$25,070 |

Total crash income: +$208,839. Mean per crash quarter: +$23,204.
Smallest crash payout: +$5,345 (2020-Q1, only 3 months of data for some symbols).

**PASS.**

### C3. Bounded bleed: median non-crash quarter > -$12k

17 non-crash quarters. Median: **-$10,576.**

Range: [-$35,278, +$45,676]. Worst 3: -$35,278, -$29,558, -$19,975.
Mean: -$4,481. 6 of 17 non-crash quarters were positive.

The worst individual quarter (-$35,278) is severe, but the median stays
inside the bound. Expected annual bleed: ~$18k (mean × 4 non-crash quarters).
Expected annual crash income: ~$32k. Net expected annual: +$14k.

**PASS** (median -$10,576 > -$12,000 threshold).

### C4. Gross/cost >= 2.0x

Mean gross R per trade: +0.2009. Mean cost R per trade: 0.0715.
Ratio: **2.10x**.

The 48% cost share is high for an alpha book. For a hedge product, it means
about half of gross goes to execution costs. The strategy remains net-positive
because winners pay 6R and offset many 1R losers.

**PASS** (2.10x >= 2.0x threshold).

### C5. Venue access

**PENDING.** Kraken futures onboarding not completed.

---

## Recorded (non-gating) observations

- **Drop-top-5% mean R:** -0.18 (tail-carried, as expected for a crash product)
- **t-stat:** +5.71 on the full ex-top-10 universe. Not computed on the
  13-symbol book alone (the slippage screen is a venue constraint, not a
  strategy parameter, so statistical significance is evaluated on the full
  strategy universe).
- **By-year P&L** (at $500 stake): 2020 -$16k, 2021 -$36k, 2022 +$39k,
  2023 +$18k, 2024 +$40k, 2025 +$79k, 2026 +$4k. Positive in 5/7 years.
- **Day concentration:** structural, not measured separately on this book.
  The full-universe figure was 136% of P&L in top 20 days (net of non-top-20
  days is negative). This is the product definition: long periods of bleed,
  punctuated by crash payouts.

---

## Result

**4 of 4 evaluable gates PASS. C5 (venue) PENDING.**

**CONDITIONAL ACCEPT.** The backtest case for the crash overlay is confirmed.
Proceed to venue onboarding. On C5 pass, move to 3-month paper trading per
§4 of the pre-registration.

---

## What happens next (on C5 pass)

1. Open Kraken futures account (MiFID quiz, KYC, fund with EUR).
2. Place one manual trade to confirm fee schedule.
3. Pre-register paper trading criteria (separate document):
   - 3 months minimum
   - Execution reliability (all entries/exits without manual intervention)
   - Realized slippage within 2x of modeled (<=10bp)
   - No single symbol > 40% of P&L
   - Bleed within 2x of backtest median (-$21k per non-crash quarter ceiling)
4. Build or adapt the engine for Kraken's API (symbol naming, SHIB unit
   conversion, funding model, order placement).
5. Paper trade 3 months.
6. Evaluate paper trading criteria.

## What happens next (on C5 fail)

If Kraken derivatives access cannot be obtained (quiz failure, regulatory
block, account rejection), evaluate OKX EU as the fallback venue per
`venue_scouting_2026-06-10.md`. The slippage screen must be re-run on
any alternative venue.
