# Stop-Distance Cost Filter — VERDICT: REJECT (pre-registration NOT opened)

**Status:** Hypothesis generated, tested, and **falsified within a single session**. No
decision rule was locked, because the candidate died before it earned one. Recorded here so
the idea is not re-discovered and re-litigated later.
**Date:** 2026-07-26
**Author:** Claude (session-generated), reviewed by Cristian Manoliu
**Data:** `results/journal_cache/` — 118 paired live forward-paper trades, 2026-05-05 → 2026-07-26.
**Spends:** zero forward-paper degrees of freedom (diagnostic on already-realized trades; no
config change, no new backtest sweep, no live behavior change).

## Origin

Session opened with the operator's observation that "these algos are kind of dying," followed
by a request to look for anything that could revive the existing algos or seed a new candidate.
A cost decomposition of the live journals produced a genuinely new and load-bearing fact
(§1). That fact suggested a filter (§2). The filter was then tested and rejected (§3–§4).

## 1. The cost decomposition — CONFIRMED, and it is the important finding

Live forward-paper, 118 closed trades:

| Component | Value |
|---|---:|
| **Gross PnL** | **+$3,489** |
| Fees | −$7,909 |
| Slippage | −$3,329 |
| **NET PnL** | **−$7,750** |
| Total notional | $7,909,212 |
| Avg notional / trade | $67,027 |
| **Implied avg leverage on $1k stake** | **67.0×** |
| Costs as % of gross | 322% |

**The signal is gross-positive. Execution cost is 3.2× the gross edge.**

Mechanism: the strategy risks a fixed $1k against a *wick-based* stop. Position size is
`$1k / |entry − stop|`, so a tight stop mechanically forces enormous notional, and fees are
charged on notional, not stake. Measured on the live opens (n=124):

| Stop distance (% of entry) | Implied leverage | Round-trip fee @10bp as % of stake |
|---|---:|---:|
| p10 — 0.80% | 124.7× | **12.47%** |
| p25 — 1.11% | 89.9× | 8.99% |
| p50 — 1.84% | 54.2× | 5.42% |
| p75 — 2.55% | 39.2× | 3.92% |
| p90 — 3.79% | 26.4× | 2.64% |

This restates the known Option-C cost geometry (`CLAUDE.md`, "Realistic fee/slippage
modeling") in live-realized terms, and quantifies it per-trade for the first time.

### 1a. The cost lever is real but insufficient — decisive

Sensitivity of NET to fee schedule, holding realized gross and slippage fixed:

| Scenario | Fee (bp) | Fees | Slippage | NET |
|---|---:|---:|---:|---:|
| Current: taker/taker (Binance Regular) | 10.0 | −$7,909 | −$3,329 | −$7,750 |
| Taker/taker + BNB discount | 9.0 | −$7,118 | −$3,329 | −$6,959 |
| Maker entry / taker exit | 7.0 | −$5,536 | −$3,329 | −$5,377 |
| Maker/maker (both legs post-only) | 4.0 | −$3,164 | −$3,329 | −$3,004 |
| Maker/maker + VIP1 | 3.0 | −$2,373 | −$3,329 | −$2,213 |
| Maker/maker, slippage halved | 4.0 | −$3,164 | −$1,665 | −$1,340 |

**Breakeven fee = 0.20 bp.** Even a zero-fee venue leaves this net-negative on slippage alone
(≈ −$3,300). Maker/maker at 4bp — already optimistic, since stop and target legs mostly cannot
be posted passively — still nets −$3,004.

> **Consequence for the venue port:** fee reduction is the largest single lever in the system
> and it is **not large enough to make this strategy profitable**. The port
> (`project_fee_reduction_venue_port`) remains justified on venue-access grounds, but it must
> not be sold internally as a route to profitability for the current config.

## 2. The hypothesis that this suggested (and which FAILED)

> **H:** Because cost scales as `1/stop_distance`, trades entered on unusually tight stops
> are structurally unprofitable. Filtering them out should raise NET.

Naive threshold scan over the realized trades appeared to support it:

| Min stop % | Kept | Gross | NET | WR |
|---|---:|---:|---:|---:|
| 0.00 (all) | 118 | $3,489 | −$7,750 | 16.9% |
| 0.80 | 107 | −$13,448 | −$22,508 | 15.0% |
| 1.50 | 70 | $2,904 | −$1,135 | 18.6% |
| 2.50 | 32 | −$854 | −$2,173 | 21.9% |
| **3.00** | **22** | $2,192 | **+$1,421** | 27.3% |

A threshold exists (≥3.0%) at which the realized book flips positive. **This is the number
that must not be trusted**, and §3–§4 explain why.

## 3. Mechanism test — the filter is cost-arbitrage only, and the cancellation is exact

If the filter worked, gross edge would have to be *independent* of stop distance (so that
removing high-cost trades is a free lunch). It is not free, because the gross edge is
concentrated in exactly the trades the filter removes.

| Quintile | Stop % range | n | Mean cost (R) | Mean **gross** (R) | Mean net (R) |
|---|---|---:|---:|---:|---:|
| Q1 (tightest) | 0.49–1.03% | 23 | 0.184 | **+0.516** | **+0.332** |
| Q2 | 1.05–1.48% | 24 | 0.119 | −0.428 | −0.548 |
| Q3 | 1.49–2.05% | 23 | 0.080 | −0.090 | −0.171 |
| Q4 | 2.13–2.80% | 24 | 0.060 | −0.134 | −0.194 |
| Q5 (widest) | 2.90–8.87% | 24 | 0.036 | +0.300 | +0.264 |

- `corr(stop_distance, cost)` = **−0.766** — mechanism CONFIRMED; cost really is driven by stop tightness.
- `corr(stop_distance, gross)` = **+0.011** — gross edge is **uncorrelated** with stop distance.
- `corr(stop_distance, net)` = +0.029.

**Q1 — the tightest, most expensive quintile — is the single best quintile on NET (+0.332R).**
The proposed filter would have deleted the best-performing trades in the book. The net column
is non-monotone (best at both extremes, worst in the middle), which is the signature of noise,
not of a cost gradient.

## 4. Permutation test — the profitable threshold is indistinguishable from luck

Null: stop distance carries no information about PnL. Realized PnLs were randomly permuted
against realized stop distances (20,000 shuffles); for each shuffle, the *best NET over all
thresholds* (min 20 trades retained) was recorded — i.e. the null is given the same
threshold-search freedom the observed result enjoyed.

| Quantity | Value |
|---|---:|
| Observed best NET over all thresholds | $7,398 |
| Null median | $9,863 |
| Null p90 | $23,298 |
| Null p95 | $27,249 |
| Null p99 | $34,682 |
| **p-value** | **0.601** |

**p = 0.601.** A random reshuffle beats the observed result 60% of the time, and the observed
best sits *below the null median*. The apparent +$1,421 is threshold-mining on 22 trades.

## Verdict: REJECT

The hypothesis fails on two independent grounds — a direct mechanism test (gross edge is
uncorrelated with stop distance; the filter removes the best quintile) and a permutation test
(p = 0.601). **No pre-registration is opened and no threshold is committed.** Opening a
pre-reg here would have converted a falsified idea into a live research thread on sunk-cost
grounds.

## What survives

1. **§1 and §1a stand as findings** and are the durable product of this session: the live
   edge is gross-positive (+$3,489) and is destroyed by a 67× average leverage that no
   available fee schedule can offset (breakeven 0.20 bp).
2. **The verdict framing for ~2026-08-25 sharpens.** The question is not "did the signal
   work" — it did, gross. It is "is a +$3,489-per-118-trade gross edge expressible at 67×
   leverage?" On these numbers, no, at any venue reachable by this operator.
3. **The ledger stays closed** (`docs/RESEARCH_BACKLOG.md`). This episode is a worked example
   of *why*: a plausible mechanism, a real supporting correlation (−0.766), and a profitable
   threshold — all of which dissolved under a null that was given the same search freedom.
   PBO 0.52 predicts exactly this.

## Reproduction

All numbers derive from `results/journal_cache/` (gitignored; repopulate with
`bash scripts/journal_fetch.sh`) via the paired open→close reduction described in §3, using
per-close `pnl_usd` / `fee_usd` / `slip_usd` / `notional_usd`. Trades are paired
chronologically per symbol (naive last-open scans surface superseded opens — see
`docs/findings/` Cond-2 note). Permutation test: seed 7, 20,000 shuffles, minimum 20 trades
retained per threshold.

## Cross-references

- `results/backtest_overfit_pbo_dsr_verdict_2026-05-29.md` — PBO 0.52 / DSR(34) 0.658; the
  prior that correctly predicted this outcome.
- `docs/RESEARCH_BACKLOG.md` — closed ledger; "each additional candidate widens the haircut."
- `CLAUDE.md` → "Realistic fee/slippage modeling is mandatory" — the cost geometry this
  quantifies in live terms.
