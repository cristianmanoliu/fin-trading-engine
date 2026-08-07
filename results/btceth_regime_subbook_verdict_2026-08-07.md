# VERDICT — C4 BTC/ETH regime-gated short sub-book: **NO-GO** (2026-08-07)

**Type:** verdict. Mechanical application of
`results/btceth_regime_subbook_prereg_2026-08-07.md` (locked `daa12c9`, before
the combined gate was ever run).
**Script:** `scripts/btceth_regime_subbook_study.py` (`--selftest` passes).
**Result: REJECT — 6 of 11 criteria fail. C4 closes.**

This was the last pre-registered lead in the repo. It is now closed.

---

## 1. Result

Gate: `VRP-z < 0 (complacency) AND premium-z < 0 (US not bidding)`.
Costs: fee 10bp RT + slip 5bp. 1,864 common days per symbol.

| | BTC | ETH |
|---|---:|---:|
| Gated trades | 378 (20.3% of days) | 406 (21.8%) |
| Gross mean | +15.83 bp | +20.19 bp |
| Gross median | +4.44 bp | +2.61 bp |
| Win rate | 52% | 50% |
| **Drop-top-5%** | **−20.87 bp** | **−23.83 bp** |
| Net of 15bp cost | +0.83 bp | +5.19 bp |
| Required (3× cost) | 45 bp | 45 bp |
| By-year positive | 3/6 | 4/6 |

**Criteria: 5 pass, 6 fail.**

| # | Criterion | BTC | ETH |
|---|---|---|---|
| 1 | gross ≥ 3× cost (45bp) | **FAIL** | **FAIL** |
| 2 | median > 0 | PASS | PASS |
| 3 | drop-top-5% > 0 | **FAIL** | **FAIL** |
| 4 | by-year ≥ 5/7 | **FAIL** (3/6) | **FAIL** (4/6) |
| 5 | both symbols same sign | PASS | — |
| 6 | n ≥ 60 | PASS | PASS |

Per §4, failure of any single criterion is a REJECT. No partial credit.

## 2. The gate is real. It is just far too small.

This is not a wrong-sign or broken-signal result — worth stating precisely,
because the failure is quantitative, not qualitative:

- The gate **opens on ~21% of days**, so it is genuinely selective.
- Both symbols are **positive and same-sign**, and both medians are positive.
- The combined gate does **lift** gross: BTC +15.8 bp and ETH +20.2 bp against
  the ungated conditional means of +8.1 bp and +12.2 bp respectively. Stacking
  two independent signals (corr −0.12) roughly doubled the conditional edge,
  exactly as the independence argument predicted it would.

**And it still misses by a factor of ~2.5.** The frontier requires 45 bp gross
to survive a 15 bp cost with the historical 10× backtest-to-live gross decay.
The best result here is 20.19 bp. Net-of-cost is +0.83 bp (BTC) and +5.19 bp
(ETH) per trade — inside the noise of any realistic slippage estimate, and this
is the *in-sample* number.

The two signals did everything that was claimed for them. The cost term is
still granite.

## 3. The pre-registered failure mode fired

§7 predicted, in advance:

> "The gate will pass on t-statistics and fail on drop-top-5%. Both source
> signals were measured as *conditional mean* effects on daily returns; neither
> was ever tested for tail concentration on an actual trade book."

That is exactly what happened. Both symbols carry a **positive mean and a
positive median but a strongly negative drop-top-5%** (−20.87 / −23.83 bp) —
the entire gross edge lives in the top 5% of days. Removing them inverts the
result on both symbols.

The by-year decomposition tells the same story from a different angle:
**2026 alone is +151 bp (BTC) and +263 bp (ETH)** against negative or
small-positive numbers in most other years. This is the identical tail-mirage
pattern that killed candidates #11, #12 and #20 — significant means, positive
by-year headline, no median-and-tail robustness. This project has now hit it
four times.

## 4. The sign correction (recorded permanently)

`orthogonal_search_synthesis_2026-06-10.md` §"The two real signals" describes
the combined gate as *"short only when fear-high AND US-not-bidding."* **The
fear-high half is inverted** relative to its own source verdict:
`dvol_vrp_verdict_2026-06-10.md` measures shorts at **−16.5 bp under fear
(z>0)** and **+19.6 bp under complacency (z<0)**.

Both source studies were re-run 2026-08-07 and reproduce to the decimal
(premium t=+2.39 BTC / +2.29 ETH; ETH VRP t=+2.04, complacency bucket
+19.64 bp). The correct gate is `VRP-z < 0`, which is what was tested here.

Anyone implementing the synthesis sentence literally would have inverted the
larger of the two signals and produced a wrong-sign falsification that looked
clean. The synthesis sentence should not be quoted without this correction.

## 5. What this closes

- **C4 is closed.** Not "closed pending better data" — the gate was measurable,
  well-powered (378/406 trades, well above the 60 floor), correctly signed, and
  it failed by 2.5×.
- **The two MARGINAL findings from the 20-candidate search are now resolved
  NO-GO.** They were the only items the search left flagged as actionable-later.
  The follow-up the synthesis recorded has been executed and answered.
- **The orthogonal search is closed with no survivors.** Final tally across all
  20 candidates: 17 NO-GO/NO-BUILD/PRECLUDED, 3 data-blocked, **0 deployable**.

Combined with the closed price-only search (DSR 0.001, PR≈1.9), the closed
maker route, and the closed wider-stop route: **there is no remaining
untested lead in this project.**

## 6. Cost of this trial

One trial, one afternoon, zero new data fetched, zero infrastructure. No
capital at risk. The pre-registration was locked in a separate commit before
the study was written, so the thresholds could not move to meet the result —
and they did not need to: the failure is not marginal.

## Cross-references

- Pre-registration (locked before the run): `results/btceth_regime_subbook_prereg_2026-08-07.md`
- Source verdicts, both reproduced 2026-08-07: `results/dvol_vrp_verdict_2026-06-10.md` (#15),
  `results/coinbase_premium_verdict_2026-06-10.md` (#16)
- Search this closes out: `results/orthogonal_search_synthesis_2026-06-10.md`
- Screening rule applied: `results/viability_frontier_2026-07-27.md`
- Tail-mirage precedent: `results/v2_lessons_and_design_2026-08-04.md` L4
