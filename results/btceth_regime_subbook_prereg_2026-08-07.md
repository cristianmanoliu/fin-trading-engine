# PRE-REGISTRATION — BTC/ETH regime-gated short sub-book (C4)

**Status: LOCKED 2026-08-07, before any code is run against the combined gate.**
**Type:** single-hypothesis pre-registration. One hypothesis, one threshold, one
run. This is **not** a sweep and must not become one.

**Trial budget: 1.** This is trial N≈86 of a search whose overfit gate already
returned DSR(37) = 0.0010 and PBO 0.5302. That is the reason for the unusually
harsh bar in §4: a single additional trial against an exhausted search surface
must clear a *higher* bar than the first trial did, not a lower one.

---

## 1. Why this is being run at all

The 20-candidate orthogonal search (`results/orthogonal_search_synthesis_2026-06-10.md`)
closed with 15 NO-GO, 3 data-blocked, and **2 MARGINAL** results. Both marginals
were shelved for one stated reason, recorded in both verdicts:

> "The gate works only on BTC+ETH … a gate measurable on 2 of 16 symbols is not
> a book-wide overlay." — `coinbase_premium_verdict_2026-06-10.md`

That blocker was a property of the **deployed book**, not of the signals. The
book closed 2026-08-04. The synthesis explicitly recorded the follow-up:

> "IF the project ever runs a **BTC/ETH-only short sub-book**, the combined
> (#15 fear-high AND #16 US-not-bidding) gate is the first thing to test on it."

This document is that test, pre-registered before running it. Nothing else in
the closed project is reopened by it: the EMA parameter search stays closed, the
maker route stays closed (`maker_execution_verdict_2026-08-05.md`), the
wider-stop route stays closed (`wider_stop_option_b_disposition_2026-08-05.md`).

## 2. The two input signals (measured, not assumed)

Both are exogenous to the price series and independent of each other:
**corr(premium-z, VRP-z) = −0.12** over 1,789 common days.

**#16 Coinbase premium** — `coinbase_premium_verdict_2026-06-10.md`
Premium = (Coinbase close − Binance close) / Binance close, 90d rolling z.

| sym | short-ret \| z<0 (US not bidding) | short-ret \| z>0 | Welch t | years |
|---|---:|---:|---:|---|
| BTC | **+8.1 bp** | −24.9 bp | **+2.39** | 6/7 |
| ETH | **+12.2 bp** | −30.0 bp | **+2.29** | 6/7 |

**#15 DVOL VRP** — `dvol_vrp_verdict_2026-06-10.md`
VRP = Deribit DVOL implied − realized vol, z-scored.

| sym | short-ret \| z<0 (complacency) | short-ret \| z>0 (fear) | Welch t | years |
|---|---:|---:|---:|---|
| ETH | **+19.6 bp** | −16.5 bp | **+2.04** | 5/6 |
| BTC | +1.0 bp | −9.5 bp | +0.79 | — |

**Both standalone directional variants (A) are DEAD** — Sharpe ≈ 0, median
negative, tail-inverting on drop-top-5%. Only the **gate** form (B) survived.
This pre-registration therefore tests a gate, never a standalone signal.

### 2b. The sign correction that this design depends on

**The #15 gate runs OPPOSITE to the shorthand used in the synthesis.** The
synthesis line "short only when **fear-high** AND US-not-bidding" is wrong
against its own verdict: `dvol_vrp_verdict` measures shorts doing **worse**
under fear (z>0 → −16.5 bp) and **better** under complacency (z<0 → +19.6 bp),
because after fear extremes the market mean-reverts up.

The correct combined condition is:

```
SHORT only when   VRP-z < 0  (complacency)   AND   premium-z < 0  (US not bidding)
```

Anyone implementing the synthesis sentence literally would have inverted the
larger of the two signals. Recorded here because it would have silently produced
a wrong-sign result that looked like a clean falsification.

## 3. The hypothesis

> **H1:** On a BTC+ETH-only short book, restricting entries to days where both
> VRP-z < 0 and premium-z < 0 produces gross R per trade ≥ 3× cost R,
> out-of-sample, without depending on the top 5% of trades.

The 3× multiplier is the frontier's own screening rule
(`viability_frontier_2026-07-27.md` §"30-second screening rule"), adopted
unchanged: live gross historically arrives at ~1/10 of backtest gross while cost
arrives at 1.0× of model.

**Cost basis (fixed now, not chosen after seeing results):** fee 10 bp RT +
slip 5 bp, per gate 5 of the batch-2 spec and the production cost model. No
maker assumption — that route is closed.

**Null:** the combined gate does not raise gross R per trade to ≥3× cost R, or
does so only via the top 5% of trades, or only in one era.

## 4. Accept / reject — thresholds fixed before the run

**ACCEPT (all six must hold):**

1. **Gross ≥ 3× cost R** at fee 10 bp + slip 5 bp on the gated book.
2. **Median trade > 0.** Not mean. (L4 — three candidates in the last search
   passed on the mean and died here.)
3. **Drop-top-5% still positive.** The single most decisive honesty check in
   this project's history.
4. **By-year: positive in ≥ 5 of 7 years**, and positive in ≥ 2 distinct
   regimes (2022 bear / 2023–24 chop-bull / 2025–26). Gate 4 of the batch-2
   spec, applied unchanged.
5. **Both symbols same sign.** BTC and ETH must agree. A one-symbol result is
   what made #15 marginal in the first place; accepting one here would repeat
   the exact error the source verdict named.
6. **Power floor: ≥ 60 gated trades.** Below that the result is unresolvable
   and the verdict is INSUFFICIENT, not NO-GO.

**REJECT on any single failure.** No partial credit, no "promising, worth
another cell." Failure of any one criterion closes C4 permanently and with it
the last pre-registered lead in this repo.

**Explicitly NOT permitted after seeing results:** changing the z-threshold
(fixed at 0), changing Z_WIN (fixed at 90, as in both source studies), changing
the symbol set, switching to an OR gate, testing a 1-of-2 variant, or adding a
third signal. Any of those is a new pre-registration and a new trial against a
DSR budget that is already at 0.001.

## 5. Method

Data and tooling all exist; nothing new is fetched or built.

| Input | Source | Status |
|---|---|---|
| Coinbase daily close BTC/ETH | `scripts/fetch_flow_data.py` → `data/flow/coinbase/` | reusable, idempotent |
| Deribit DVOL BTC/ETH | `scripts/fetch_dvol.py` → `data/dvol/` | reusable, idempotent |
| Binance 1m klines | `data/*-1m-*.csv` (symlinked) | in hand |
| Trade stream | `scripts/gen_live_journals.sh` | regenerates deployed-book journals |

Gate construction follows `coinbase_premium_study.py::variant_b` exactly:
90d trailing window, z computed from history **strictly prior to** day `d`
(`range(max(0, i - Z_WIN), i)` — the current day is excluded), signal on day
`d`, return measured `d → d+1`. **The look-ahead exclusion is load-bearing** and
must be preserved verbatim; a left-edge-inclusive window is the exact
resample look-ahead trap recorded in `project_batch3_closed_pandas_traps`.

Walk-forward: 3 windows, per gate 1 of the batch-2 spec.

## 6. What ACCEPT would and would not mean

**Would mean:** one pre-registered, honestly-gated result on a 2-symbol book —
grounds for a *further* pre-registered validation, nothing else.

**Would NOT mean:** deploy. Two independent constraints survive any result here:

1. **Venue.** Binance futures remain region-blocked for this operator with no
   timeline (`project_binance_futures_region_block`, confirmed by support
   2026-06-10). Kraken is 10 bp RT — *identical* to what the failed run paid, so
   a port buys nothing on cost.
2. **Signability.** BTC was the **single worst symbol** in the full-57 run
   (−$110k); the base edge is a mid-cap-alt crowding harvest. A BTC/ETH book
   trades the two symbols where the underlying signal is weakest. At ~1.2
   trades/day scaled to 2 symbols, resolving the result to ±5pp WR takes years.

**A clean ACCEPT here is a research finding, not a business.** That is stated
before the run so it cannot be re-negotiated after a positive one.

## 7. Failure mode this design is most likely to hit

Stated in advance, per project discipline: **the gate will pass on t-statistics
and fail on drop-top-5%.** Both source signals were measured as *conditional
mean* effects on daily returns; neither was ever tested for tail concentration
on an actual trade book. The prior from this project is unambiguous — #11, #12
and #20 all produced significant means that the median/tail checks then killed.

If that is what happens, it is a clean falsification and C4 closes.

## Cross-references

- Frontier / screening rule: `results/viability_frontier_2026-07-27.md`
- Search closure + the follow-up this executes: `results/orthogonal_search_synthesis_2026-06-10.md`
- Source verdicts: `results/coinbase_premium_verdict_2026-06-10.md` (#16),
  `results/dvol_vrp_verdict_2026-06-10.md` (#15)
- Overfit budget: `results/overfit_expansion_2026-06-09.md` (DSR→0.001, PR≈1.9)
- Honesty lessons applied: `results/v2_lessons_and_design_2026-08-04.md` (L4, L5, L7)
- Closed routes not reopened: `results/maker_execution_verdict_2026-08-05.md`,
  `results/wider_stop_option_b_disposition_2026-08-05.md`
