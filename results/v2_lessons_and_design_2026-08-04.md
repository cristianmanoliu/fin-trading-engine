# Version 2 — what the run taught, and how to build the next one

Companion to `forward_paper_closeout_2026-08-04.md`. That document records
what happened. This one records **what to do differently**, written while the
evidence is still fresh and before hindsight smooths it over.

Scope: alt-currency (crypto perpetual) trading specifically.

---

## 1. The single most important number

**Break-even was 8 bp. The run executed at 10 bp.**

Holding everything else constant and varying only the fee rate:

| Fee | Net |
|---|---|
| 10 bp (actual) | **−$1,651** |
| **8 bp** | **+$184** ← break-even |
| 5 bp | +$2,937 |
| 3 bp | +$4,773 |
| 0 bp | +$7,526 |
| −2 bp (maker rebate) | +$9,361 |

The strategy did not fail by an order of magnitude. **It failed by 2 basis
points.** Gross edge was +$85/trade; costs were $97/trade.

This is the correction to the story told throughout the run. "Cost geometry
kills it" was accurate but imprecise, and the imprecision mattered: it implied
the gap was structural and unbridgeable. It is structural, but it is *narrow*.
A venue at 8 bp or below, or maker execution anywhere, flips the sign.

**What this does NOT mean:** that v1 should be revived. At +$184 net on 134
trades over 88 days, break-even fees buy a strategy that is statistically
indistinguishable from zero, on a book whose top 5% of trades carry more than
100% of the P&L. Fixing fees converts a clear loser into an unproven
coin-flip. That is not a business — but it *is* a much better starting point
than "the class is dead."

---

## 2. Six lessons, ranked by how much they would change

### L1. Fee assumption is the whole ballgame — validate it FIRST, not last

The entire 88-day run was, in effect, a test of whether gross edge exceeded
10 bp round-trip. That question could have been answered in an afternoon with
the backtest, before writing an execution layer, a journal replayer, a
watchdog, or a Telegram notifier.

**v2 rule: the first artifact is a break-even fee curve.** Before any
infrastructure: what fee level does this strategy need? If the answer is
below what your venue actually charges, stop. Everything else is premature.

### L2. Cost scales with notional; edge scales with risk — that ratio is the strategy

Sizing to a $1,000 risk against a ~1.7% stop forces ~56× notional. Fees are
charged on notional, twice. So cost per trade is `2 × fee_bps × stake /
stop_pct` — inversely proportional to stop distance.

Tight stops are not free precision. **A stop 2× tighter doubles your fee
burden.** The strategy's own stop-distance quartiles show no monotonic
relationship with P&L (Q1 +$2.9k, Q2 −$8.7k, Q3 +$5.5k, Q4 −$1.4k) — the
tight stops were not buying anything the wide ones weren't.

**v2 rule: compute `cost / gross_edge` per candidate at design time.** If
costs exceed ~30% of gross, the strategy is fee-fragile regardless of how
good the backtest looks.

### L3. Venue access is a hard gate — check it before writing code

Real-money execution was blocked the entire time by a region restriction
(EEA/Romania) discovered *after* the executor, kill-switch, safety gates, and
two layers of testnet validation were built. Binance confirmed no timeline.

**v2 rule: open and fund the account first.** Place one $10 trade manually.
If you cannot do that, the venue does not exist for you, and no amount of code
changes that.

### L4. Drop-top-5% is the honesty check that decides everything

All 8 research shadows showed positive net. Five of them go negative when the
top 5% of trades are removed. Live goes from −$1.6k to −$43k.

This is the same tail-mirage lesson recorded in Phase 0 and re-learned here.
By-year positivity, aggregate net, and Sharpe all survive tail concentration.
Drop-top-5% does not.

**v2 rule: no strategy advances on aggregate P&L. Report median trade and
net-without-top-5% on every candidate, always.**

### L5. Running N variants and picking the winner is selection, not discovery

8 shadows on the same tick stream produced a spread from +$78k to +$2k. The
best one looks compelling. It is the max of 8 draws from a distribution whose
mean is roughly zero after honest costs.

**v2 rule: pre-register the variant count and apply a multiple-comparison
correction, or run one strategy.** A shadow that was not pre-registered as
the primary is evidence about the *class*, never a promotion candidate.

### L6. Instrument the label, not just the number

7 of 24 winners (29%) carried `outcome: "TARGET"` on max-hold force-closes
that never touched target. P&L was unaffected, but every WR-by-outcome
analysis during the run was subtly wrong until this was caught.

**v2 rule: exit reason is written by the code path that performs the exit,
never inferred. One enum, one writer, asserted in a test.**

---

## 3. What v2 should actually be

Ranked by expected value, given everything above.

### Option A — same strategy class, maker execution (highest EV)

The 8 bp finding says the edge is real but thin. Maker orders on a venue with
a rebate turn −10 bp into roughly −2 bp to +2 bp: a 12 bp swing on a strategy
that needed 2.

**Hard problem to solve first:** the strategy currently enters on a 4H EMA
cross at market. Maker execution means resting a limit order and accepting
that some fills never happen. **Adverse selection is the risk** — you get
filled on the trades that go against you and miss the ones that run. That can
easily cost more than the 12 bp it saves.

**How to test it cheaply:** replay the existing 417-journal archive. For each
entry, ask whether a limit order at the signal price would have filled within
N minutes using the tick data. Fill rate and the P&L of filled-vs-missed
trades answers the question **without writing an execution layer**. This is a
few days of analysis on data you already have.

**Go/no-go:** if maker fill rate is >70% and missed trades are not
systematically the winners, build it. Otherwise the class is closed for good.

### Option B — wider-stop variant of the same signal (cheap to test)

Cost per trade is inversely proportional to stop distance. A 3.5% stop instead
of 1.75% halves fee burden. The quartile data shows wide stops were not
systematically worse.

This is a **backtest question requiring zero infrastructure** — the engine
still exists in the repo. Run it against the 5y data at 10 bp with
drop-top-5% honesty reporting. If a wider-stop variant clears costs at
*current* fees, that is a better answer than Option A because it needs no
venue change.

**Caveat:** this is a new parameter search on a class where the search was
declared closed at N≈85. Pre-register it as a single hypothesis with a fixed
threshold before running, or it becomes cell 86 of an overfit sweep.

### Option C — different asset class (the honest diversification)

`fin-equity-lab` already exists, running since 2026-06-30, combo_blend at
Sharpe 0.95 OOS, verdict ~2027-07. Equities have fees 1–2 orders of magnitude
lower relative to typical holding-period moves, which is exactly the constraint
that killed v1.

**This is the strongest option and it is already running.** It needs patience,
not new work.

### What NOT to build

- **Not another EMA-crossover perp strategy at taker fees.** That question is
  answered.
- **Not sub-5m timeframes.** Presumptively fee-dead; the geometry is worse at
  every shorter horizon.
- **Not a "better filter" on v1's signal.** Entry filters, regime overlays,
  and side filters were all tested (N≈85, regime F=0/9). The problem was never
  signal quality.
- **Not more shadows.** See L5.

---

## 4. The process changes that matter most

1. **Order of operations inverts.** v1 was: build engine → validate strategy →
   discover fees kill it → discover venue is blocked. v2 is: **venue access →
   break-even fee curve → honest backtest (drop-top-5%) → only then any code.**

2. **One pre-registered hypothesis per run.** Not 8 shadows. The pre-registration
   discipline was the best thing about v1 and should be kept exactly as-is —
   it is why this project produced a clean answer instead of a rationalization.

3. **Kill criteria should include a "would break-even fees save this?" branch.**
   v1's kill criteria were all about drift and drawdown. None asked the
   question that turned out to decide everything.

4. **Budget the calendar honestly.** 88 days of forward-paper produced a result
   that a one-afternoon fee-sensitivity analysis would have predicted. Forward
   paper is for validating *execution*, not for discovering *arithmetic*.

---

## 5. What v1 was actually worth

It cost ~3.5 months, ~€45 in hosting, and **zero dollars of trading capital**.
It produced:

- A falsified strategy class, with the mechanism understood to 2 bp precision.
- A working, tested engine (journal replay, watchdog, drift detection, staged
  promotion protocol) that is directly reusable.
- A pre-registration discipline that held under pressure — including when the
  answer became uncomfortable and when the operator repeatedly asked whether it
  could be revived. It was never fudged.
- The break-even finding in §1, which is the actual starting point for v2 and
  which did not exist until the run was closed out.

The one thing it did not produce is the formal verdict, terminated 21 days
early at 134 of a pre-registered 150 trades. That cost is recorded honestly in
the close-out doc.

**Recommended next action:** Option A's maker-fill replay against the archived
journals. It is a few days of analysis, requires no new infrastructure, uses
data that already exists, and definitively answers whether this class is worth
a v2 at all — before anything gets built.
