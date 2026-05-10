# Glossary — plain-English definitions

Every jargon term used across this project's docs, defined for someone who
hasn't worked in quant trading or statistics. Each entry has:

- **Tooltip** — one short line that appears when you hover over the term
  in another doc (rendered via `<abbr title="...">term</abbr>`)
- **Longer context** — what it means, why it matters here, examples

If you find a term in any project doc that's not in this glossary, it's a
gap. Add it.

---

## Statistics

### α (alpha) / significance threshold

**Tooltip:** *The maximum chance you're willing to accept of "seeing a
real result when there isn't one." Smaller is stricter.*

If you flip a fair coin 100 times, you might get 60 heads by pure luck —
that's not a sign the coin is biased. α is the line you draw: "if a result
this extreme would happen by chance less than α of the time, I'll call it
real." Common values: α=0.05 (5% chance of being fooled), α=0.01 (1%),
α=0.001 (0.1% — very strict).

### Bonferroni correction

**Tooltip:** *When you test many ideas at once, divide your significance
threshold by the count. Tests 5 things at α=0.05 → each test must clear
α=0.01.*

If you test enough random ideas at α=0.05, ~5% of them will look "real"
just by chance. Bonferroni divides α by the number of tests so the family
of tests collectively keeps the same false-positive rate. It's conservative
(some real edges get missed) but the math is dead simple. In this project,
milestone-2 tests 5 ideas at family-wise α=0.05 → individual α=0.01.

### Bootstrap / block bootstrap / bootstrap CI

**Tooltip:** *Pretend the data you have IS the population, resample it
many times, see how much your number wiggles. The 2.5%/97.5% wiggle
extremes are your confidence interval.*

Most statistics assume you know the underlying distribution. When you
don't, bootstrapping lets you estimate uncertainty by repeatedly drawing
random samples (with replacement) from your actual data. A "block bootstrap"
draws contiguous chunks instead of single observations to preserve
autocorrelation (important for time-series like trading P&L where today's
trade outcome correlates with yesterday's). The 95% confidence interval is
the 2.5% and 97.5% percentiles of the resampled distribution.

### Walk-forward / walk-forward CI

**Tooltip:** *Split history into many train/test windows that march
forward through time. Test your strategy on each; mean tells you expected
performance, variance tells you regime risk.*

If you optimize a strategy on 2020-2024 data and test it on 2025, you've
done one out-of-sample test. Walk-forward does this many times — train on
[2020-2022], test on [2023]; train on [2020-2023], test on [2024]; etc. The
mean across windows is your unbiased performance estimate; the spread
between windows tells you how much the strategy's edge depends on regime.
Anchoring expectations to walk-forward (not bootstrap on aggregated
trades) avoids underestimating regime variance.

### Sharpe ratio

**Tooltip:** *Return divided by volatility. Higher = more profit per unit
of stomach-churn. 1.0 is OK, 2.0 is good, 3.0+ is exceptional.*

Annual return divided by annual standard deviation of returns. Two
strategies that both make $100k/year aren't equivalent if one swings
±$300k month-to-month and the other swings ±$30k. Sharpe penalizes
volatility. Beware: Sharpe assumes normal-ish returns; crypto strategies
with fat tails can have misleadingly high Sharpe.

### Power floor / statistical power

**Tooltip:** *The minimum sample size needed to reliably detect a real
effect. Below the floor, you can't trust either positive or negative
results.*

If a strategy's true win-rate is 21% and breakeven is 14%, you need ~70
trades to reliably see the gap above noise. Below ~70 trades, you might
see anywhere from 10% to 35% just from variance, so you can't tell if the
strategy is working. The "60-day power floor" in this project means: don't
draw any conclusion from forward-paper before n=70 trades have accumulated.

### p-value

**Tooltip:** *The probability that a result this extreme would occur
purely by chance, if there were no real effect.*

If you flip a coin 10 times and get 9 heads, the p-value of "this isn't a
fair coin" is about 0.02 — there's a 2% chance of seeing 9+ heads with a
fair coin. p-values below α get called "significant." A common mistake:
p-value is NOT "the probability the effect is real" — it's "the probability
of seeing this much evidence under the assumption of no effect."

### Welch t-test / z-test

**Tooltip:** *Standard tools for asking "are these two distributions
actually different, or did I just see noise?"*

t-test compares means. Welch's variant doesn't assume the two samples have
equal variance (more realistic for our data). z-test compares proportions
(like win-rates between live and backtest). Both produce a p-value; if
p < α, you reject the null hypothesis "they're the same."

### Block-bootstrap CI / stationary bootstrap

**Tooltip:** *Bootstrap variant that draws contiguous chunks of data
(not single points) to preserve time-correlation. The right tool for
trading P&L.*

Trading P&L has lag-1 autocorrelation: a winning trade is slightly more
likely to be followed by another. Naive bootstrap (single-trade
resampling) breaks this and underestimates uncertainty. Block bootstrap
draws blocks of length L (typically L=√N where N is sample size).
Stationary bootstrap randomizes block length to avoid edge artifacts.

### Spearman correlation

**Tooltip:** *Correlation that ranks data first instead of using raw
values. Robust to outliers and non-linear relationships.*

Standard (Pearson) correlation can be misleading if one trade contributes
20× the typical P&L. Spearman replaces each value with its rank (1, 2, 3,
...) and correlates the ranks. Ranges from −1 (perfect inverse) to +1
(perfect positive); 0 = no monotonic relationship.

---

## Trading mechanics

### bps (basis points)

**Tooltip:** *1 bp = 0.01% = 0.0001. 100 bps = 1%. Used for fees, slippage,
funding rates because they're small numbers.*

A 10 bp fee on a $100k position is $100. "Realized fee bps ≤ 12" means
the actual fees-per-trade as a fraction of position notional must average
below 0.12%. The trading universe rounds to bps because percent has too
few decimals (10 bp = 0.1% feels easier to discuss than 0.001).

### EMA / EMA crossover

**Tooltip:** *Exponential Moving Average — like a moving average but
reacts faster to recent prices. EMA crossover = signal when fast-EMA
crosses slow-EMA.*

EMA9 and EMA21 are common settings: 9-period and 21-period exponential
moving averages. When EMA9 crosses BELOW EMA21, that's a bearish signal
(short entry). The "exponential" weight means recent prices count more
than old ones; a simple moving average treats all periods equally.

### ATR (Average True Range)

**Tooltip:** *A measure of how big a typical candle is. Used for sizing
stops and positions in proportion to volatility.*

True Range for one period = max of (high-low, |high-prev_close|,
|low-prev_close|). ATR is a moving average of that. ATR(14) = 14-period
ATR. "ATR-targeted sizing" means: size each trade so $-risk per trade is
proportional to ATR — bigger stake on quiet symbols, smaller on volatile.

### Wick stop / body stop

**Tooltip:** *Wick stop = stop-loss placed just past the candle's
extreme high/low (the "wick"). Body stop = at the open/close. Wick is
tighter and triggers fewer false stops.*

A 4H candle has a body (open-to-close) and wicks (extreme highs/lows).
Wick stops put your stop just outside the wick — if price comes back to
that level, you assume the original signal was wrong. Body stops are
wider but more conservative. This project uses wick stops on 4H signals.

### Target R:R / target_rr

**Tooltip:** *Take-profit distance divided by stop-loss distance. 6:1 RR
= you risk $1 to make $6.*

If your stop is 1% below entry and your target is 6% above entry, you have
a 6:1 reward-to-risk ratio. With 6:1 RR, you only need to win ~14% of the
time to break even (1 win pays for 6 losses minus fees). Higher target_rr
needs fewer wins but each trade takes longer to resolve.

### Max-hold / max-hold-hours

**Tooltip:** *Time limit on a position. After N hours, force-close at
market regardless of profit/loss.*

A 504-hour max-hold = 21 days. Without a max-hold, a position that drifts
sideways for months ties up capital and accrues funding indefinitely. The
max-hold caps the worst-case capital-tied-up duration; trade-off is some
positions get closed mid-move.

### Side-filter

**Tooltip:** *Restriction on which trade direction to take. "side-filter
short" = only take SHORT signals, ignore LONG signals.*

In this project, 4H short-only outperformed both-sides because longs
underperformed across the universe (mechanism analysis 2026-05-06).
Side-filter is a no-op on the engine for the filtered direction —
signals fire but don't open positions.

### Funding rate / funding accrual

**Tooltip:** *On perpetual futures, longs and shorts pay each other
periodically to keep the perp price near spot. Bull market: longs pay
shorts. Bear: shorts pay longs.*

Binance USDT-M perps settle funding every 8 hours. A funding rate of
+0.01% per 8h means longs pay shorts 0.01% of position notional. Across
3 years of bull market, holding shorts can passively earn ~10% APR from
funding alone. In our 5y data the net is roughly $0 because regimes
flip — bulls and bears cancel. Critical: net ≈ 0 averaged over a long
window does NOT mean it's free; in any given trade, funding is a real
cost or income.

### Stop slippage

**Tooltip:** *When a stop-market order fires, the actual fill price is
usually worse than the trigger price. The gap is "slippage."*

If your stop is at $50,000 and the market gaps through it to $49,950
before your order fills, you slipped 50 bp. We model slip on losers only
because winners hit limit orders (no slippage). Realized slip > modeled
slip is a kill criterion.

### HODL / BTC-HODL benchmark

**Tooltip:** *Just-buy-and-hold-Bitcoin baseline. Strategy must beat
this to be worth the trouble.*

If buying BTC and doing nothing made $50k over the same window your
active strategy made $40k, you should have HODL'd. BTC-HODL benchmark in
this project compares strategy P&L against $32k notional buy-and-hold
over the same date range. Two consecutive 30-day windows underperforming
HODL by >$5k each is an advisory kill criterion.

### Notional / position notional

**Tooltip:** *The dollar size of your position, including leverage. NOT
the same as your stake.*

If you put up $1000 stake at 100× leverage, your notional is $100,000.
Fees are charged on notional, not stake. At 10 bp round-trip, $100k
notional pays $100 in fees per trade. This is why "small" stakes can
generate large fee bills under leverage.

### Delta-neutral

**Tooltip:** *A position whose total exposure to price direction is
zero. Long $X spot + short $X perp = zero delta.*

If BTC goes up 5%, your long spot gains 5% and your short perp loses 5%
— net flat. Profit comes from funding rate or basis arbitrage instead of
price direction. Genuinely market-neutral strategies are rare; "delta-
neutral funding arbitrage" is one of the few.

---

## Operational discipline

### Pre-registration

**Tooltip:** *Locking the rule (threshold, gate, decision) BEFORE
looking at the data. Prevents post-hoc story-fitting.*

Anyone can find a "winning" pattern in past data if they search hard
enough — that's the post-hoc selection trap. Pre-registration writes
down the exact criterion ("strategy passes if Sharpe ≥ 1.2 baseline at
α=0.01") before running the backtest. The result then mechanically
ADOPTs or REJECTs. The discipline charter (`results/INDEX.md`) explains
why.

### Decision rule / verdict

**Tooltip:** *Decision rule = the locked criterion BEFORE data. Verdict
= the mechanical answer when the rule is applied to data. Always
paired.*

`*_decision_rule_*.md` is the locked rule (written first). `*_verdict_*.md`
is the result of applying it. The asymmetry — many rules, fewer verdicts
— means lots of pre-registrations are still awaiting their data
(operational pre-regs that activate on future events).

### Forward-paper / paper trading

**Tooltip:** *Trading the strategy with real market data and real
exchange feeds, but fake money. Validates the engine without risking
capital.*

The 16 engines on Hetzner are paper-trading: real Binance prices, real
WebSocket data, real signal generation, real trade tracking — but fills
are simulated by the Stub executor, no money at risk. Forward-paper is
the gate between backtest and real-money.

### Shadow strategy

**Tooltip:** *A second strategy running in parallel on the same engine,
generating its own trade journal but NOT taking the live position.
Comparison data without real-money risk.*

Each engine runs 1 live + 3 shadows. If the live config is "EMA9/21 4H
short rr=6", a shadow might be "EMA5/15 4H short rr=6". Both see the
same ticks; both emit signals; only live's signals open positions. The
shadow's journal lets us compare what would have happened without
deploying capital.

### Drift detector

**Tooltip:** *Statistical test that compares live performance against
backtest expectation. Fires when divergence is too big to be chance.*

`scripts/live_vs_backtest_drift.py` runs Welch t-tests on key metrics
(WR, fee bps, slip bps, NET) comparing live cohort against backtest
reference distribution. Bonferroni-corrected at α_family=0.001. Single
firing = investigation; two firings 7+ days apart = auto-kill candidate.
The decision-grade kill mechanism.

### Kill bar / threshold kill

**Tooltip:** *Simple threshold-based kill criteria like "WR < 14%" or
"slip > 25 bp." Calibration showed these are advisory only — too many
false positives.*

Threshold kill bars in `forward_paper_status.sh` were originally meant
to be the kill mechanism. Calibration found 30-40% false-positive rate
under null (calibration verdict 2026-05-07). They're now treated as
investigation triggers, not auto-kill. The drift detector replaced them
as the decision-grade mechanism.

### LIMBO state

**Tooltip:** *Forward-paper outcome where calendar gate (60d) is met but
trade gate (150 trades) isn't. The locked decision rule says CONTINUE
below n=50.*

At fleet rate 1.18/day, day-60 lands at ~70 trades. Trade gate (150)
binds. The LIMBO rule (`forward_paper_outcome_resolution_decision_rule_2026-05-10.md`)
mechanically maps this and 4 other ambiguous outcomes to one of:
CONTINUE / WATCH / PROMOTE / KILL / OPERATOR_REVIEW.

### STAGE_1 promotion / staged real money

**Tooltip:** *First step of paper-to-real-money: $100/trade. Stages 1-4
($100 → $300 → $500 → $1000) gradually scale exposure as live history
accumulates.*

Locked in `real_money_protocol_decision_rule_2026-05-08.md`. Each stage
gate requires N trades + M days at the prior stage with stable cost
stack and clean drift detector. The 4-stage structure prevents jumping
from $0 to $1000/trade on day 1.

### Runbook

**Tooltip:** *Locked execution sequence for a high-stakes operation.
Same procedure each time, parameterized only by stage. Prevents
improvisation under stress.*

`stage_promotion_runbook` locks the 6 phases of every STAGE promotion.
`auto_kill_execution` locks the 6 phases of every kill. `milestone2_runbook`
locks the 5 phases of milestone-2 candidate execution. The shared
discipline: writing the playbook BEFORE the high-stakes moment, so the
moment is mechanical.

### Post-hoc selection

**Tooltip:** *Looking at the data, then picking the threshold/strategy
that "works." Generates spurious results, even with honest intent.*

The most insidious form: you run 50 backtests, see one with a great
Sharpe, write up that one. The other 49 don't go in the paper. Reader
sees a +Sharpe strategy; truth is you cherry-picked from a noise
distribution. Pre-registration is the antidote.

### Degrees of freedom (DoF)

**Tooltip:** *Each parameter you tune by looking at outcomes "spends"
some of your statistical budget. Spend too much and your "result" is
just curve-fit noise.*

Every backtest sweep cell, every threshold adjustment, every
"let me try this and see" consumes DoF. With infinite DoF spent, anything
looks like an edge. The locked discipline "Don't tune target_rr,
signal_tf, or side-filter — every additional sweep cell consumes
statistical degrees of freedom you've already spent" preserves the
remaining budget for genuinely new mechanism classes.

---

## Audit lens

### Fail-open

**Tooltip:** *A bug where missing/empty/corrupt input silently maps to
the success branch. Operator sees green when reality is "no monitoring."*

The most common pattern in this codebase's tooling. Defaults like `:-0`,
`(.field // 0)`, `dict.get(key, 0)` are fail-open hatches if they aren't
distinguished from genuine zero. The `docs/AUDIT_LENS.md` doc catalogs
57 instances closed across 6 sessions.

### Silent fallthrough

**Tooltip:** *Code path that quietly produces a result without raising
an alarm. Looks like success; isn't.*

Distinct from "fail-open" in connotation: fail-open is the pattern;
silent fallthrough is the code symptom. A `|| true` after a curl call
swallows the exit code; a `try/except: pass` discards the error. Both
are silent-fallthrough mechanisms.

### Telegram-tier dual sense

**Tooltip:** *When verdict tools feed Telegram tier mapping, bad-input
crashes can route to the wrong tier. Python TypeError → exit 1 →
CRITICAL kill page on a parse error.*

Audit the tier mapping AS WELL as the verdict logic. A script that
correctly emits exit 1 for "verdict failed" and maps exit 1 to CRITICAL
also routes any uncaught Python exception to CRITICAL — which is wrong
for a parse error.

### Lens-as-audit / lens-as-design-tool / lens-as-self-correction

**Tooltip:** *Three modes of applying the audit lens. Audit = retroactive,
review existing code. Design-tool = prospective, build guards in. Self-
correction = your own just-shipped code.*

All three demonstrated within the 2026-05-10 session. Self-correction is
the most surprising one — the lens works on code you wrote 12 minutes
ago. Documented examples in `docs/AUDIT_LENS.md`.

### Exit-code contract

**Tooltip:** *A tool's commitment to map specific failure shapes to
specific exit codes. 0 = success, 1 = data shortage, 2 = trigger missing,
3 = misconfig, etc. Lets cron / wrapper / Telegram tier discriminate.*

Without an exit-code contract, every failure routes to "exit 1" (POSIX
default) and the wrapper can't tell missing-config from genuine
data-shortage from real-emergency. The milestone-2 launch script has 6
distinct exit codes per failure shape, each mapped to a Telegram tier.

---

## Cross-references

- `docs/AUDIT_LENS.md` — methodology that uses many of these terms
- `docs/OPERATOR_HANDBOOK.md` — operational reference (cadence, tools)
- `results/INDEX.md` — pre-registration discipline charter
- `CLAUDE.md` — project history with all jargon used in context

If you want even simpler explanations or worked examples, ask. The goal
of this glossary is to lower the read-cost of every other doc to "anyone
can follow it."
