# Kraken Futures appropriateness quiz — derivatives mechanics primer

**Purpose:** review before retaking the Kraken Futures appropriateness quiz.
First attempt failed 2026-07-07 (30-day cooldown expired ~2026-08-06).
Per `docs/venue_port_admin_checklist.md` step 3: "review derivatives mechanics
(margin, leverage, liquidation, funding, max-loss scenarios) — answer honestly,
do NOT inflate real-money trading history."

**Updated 2026-09-19** with Kraken-specific details and a self-quiz section (§8).

MiFID II appropriateness tests are pass/fail knowledge checks, not judgment calls.
They're checking that you understand what can go wrong before letting you access
leveraged derivatives. Answer from genuine understanding — inflating experience or
guessing to "sound right" is exactly what caused the first fail and is generally
detectable in how the questions are structured (they cross-check answers against
each other).

---

## 1. What a perpetual future actually is

A perpetual future (perp) is a derivative contract that tracks an underlying
asset's price with **no expiry date** — unlike a traditional future (quarterly,
settles on a fixed date). You never take delivery of the underlying; you're
purely trading the price difference, cash-settled.

Because there's no expiry to force convergence with spot price, perps use a
**funding rate** mechanism instead (see §4).

## 2. Margin — the collateral, not the position size

- **Margin** = collateral you post to open/hold a leveraged position. It is
  *not* the full notional value of the position — that's the point of leverage.
- **Initial margin**: required to open the position. Kraken shows this as a
  percentage — e.g. our BCHUSDT-equivalent (PF_BCHUSD) is tier-1 max 50×
  leverage = 2% initial margin. 2% of notional = the collateral needed.
- **Maintenance margin**: the *minimum* collateral that must remain in the
  position before it gets liquidated. Always lower than initial margin.
- **Margin call / liquidation trigger**: as the position moves against you,
  your equity in the position shrinks toward maintenance margin. Cross this
  and the exchange force-closes you (see §3).

**Key distinction the quiz likely probes:** margin is not a fee, not a loss,
not "rent" — it's collateral you get back (or lose, in whole or part) depending
on how the trade closes.

## 3. Leverage and liquidation

- **Leverage** = notional position size ÷ margin posted. 50× leverage means a
  $1,000 margin controls a $50,000 notional position.
- **Higher leverage = closer liquidation price.** At 50×, a ~2% adverse move in
  the underlying wipes out your margin. At 10×, it takes a ~10% adverse move.
  This is the single most important quiz concept: **leverage does not change
  your expected return, it changes how much price movement you can survive
  before forced liquidation.**
- **Liquidation is not "you lose the trade" — it's the exchange force-closing
  your position at (or through) a liquidation price to prevent your account
  going negative**, usually with a penalty (liquidation fee) added on top of
  the loss. You do not get to choose the exit price or timing once liquidation
  triggers.
- **Cross vs. isolated margin** (confirm which Kraken defaults to / offers):
  - *Isolated*: only the margin allocated to that specific position is at risk;
    losses can't cascade into your other positions or wallet balance beyond
    what you allocated.
  - *Cross*: your entire account balance backs every open position; a large
    loss on one position can draw down margin available to others, and can
    also delay liquidation (more collateral available) but risk more total
    capital if things go badly.
- **Max-loss scenario:** in isolated margin, your max loss on a single position
  is the margin you posted to it (barring extreme gap/slippage past the
  liquidation price in a fast market, which can occasionally cause loss beyond
  posted margin — this is a known tail risk exchanges disclose). In cross
  margin, max loss can be your entire account balance.

## 4. Funding rate — the perpetual's anchor mechanism

- Paid periodically (Kraken: 8h cadence on our 16 instruments) between long and
  short position holders directly — **not paid to/from the exchange**.
- **Direction:** when perp price > underlying spot (contract trading at a
  premium, usually because more traders are long/bullish), **longs pay
  shorts**. When perp trades at a discount, **shorts pay longs**. This pulls
  the perp price back toward spot.
- It's a small periodic cash flow, not a fee tied to trade execution — you pay
  or receive it just for holding a position through the funding timestamp,
  regardless of whether the position is winning or losing.
- Over a long holding period funding can materially add to or erode PnL
  independent of price direction — worth knowing as a "hidden" cost/benefit
  distinct from the entry/exit spread and trading fees.

## 5. Fees vs. margin vs. funding — three distinct costs

Don't conflate these (a common quiz trap):

| Cost | What it is | When charged |
|---|---|---|
| Trading fee (maker/taker) | Percentage of notional, paid to exchange | Every open + close |
| Funding rate | Peer-to-peer payment between longs/shorts | Every funding interval (8h) while position open |
| Margin | Collateral, returned unless lost to the trade | Posted at open, released at close (minus losses) |
| Liquidation fee | Penalty charged on forced closure | Only if liquidated |

## 6. Volatility and gap risk

- Crypto perps can move fast — liquidation engines rely on continuous
  liquidity to close positions at/near the liquidation price. In a fast/thin
  market, the position may close at a worse price than the theoretical
  liquidation price (slippage past liquidation), which is how losses can
  occasionally exceed posted margin in isolated mode.
- This is why exchanges gate high leverage behind appropriateness checks: the
  downside is fast, mechanical, and can exceed naive "I'll just watch the
  price and close in time" assumptions — liquidation is automatic and doesn't
  wait for you to react.

## 7. Answering the quiz itself

- Answer based on real understanding of the above, not a guess at what sounds
  "conservative enough" or "experienced enough."
- Do not overstate trading history or experience — checklist explicitly warns
  against this; inconsistent answers (claiming deep experience while getting
  mechanics wrong) is a more likely fail pattern than plain unfamiliarity.
- If a question asks about maximum possible loss, the honest answer for
  leveraged derivatives is generally "can exceed the margin posted" (isolated,
  tail/gap risk) or "your full account balance" (cross) — not "limited to what
  I choose to risk," which undersells real leverage risk.
- If asked about experience with derivatives: you have ~2.5 months of paper
  (simulated) trading on a systematic strategy, testnet validation in
  progress, zero real-money derivatives trades to date. State that plainly.

## 8. Kraken-specific details

Kraken Futures operates under **Payward Europe (CySEC 342/17, MiFID II)**.
The quiz is an EEA regulatory requirement, not a Kraken preference.

**Kraken Futures product specifics:**
- **Multi-collateral** ("flexible futures"): EUR, USD, crypto all accepted as margin.
  This means your EUR deposit can serve as collateral directly (no forced conversion).
- **Funding interval:** 8 hours (same as Binance). Funding is settled at 04:00, 12:00,
  20:00 UTC.
- **Fee tiers:** base tier is 5 bp taker / 2 bp maker per side = 10 bp round-trip
  taker. Volume discounts exist but are irrelevant at our scale.
- **Position modes:** Kraken offers both isolated and cross margin. Default is cross.
  For the crash overlay, isolated margin is preferred (caps per-position risk).
- **Leverage limits:** instrument maximum varies (50× on majors, 10-20× on alts).
  EEA retail clients MAY receive lower limits after the appropriateness quiz,
  depending on quiz outcome. Our strategy needs only ~18× implicit leverage
  ($500 stake, ~$28k notional) so even a 20× cap works.

**What the quiz is NOT:**
- Not a test of trading skill or strategy knowledge.
- Not checking whether you will be profitable.
- It IS checking whether you understand the RISKS of leveraged derivatives:
  how you can lose more than you intended, how liquidation works, and that
  past performance (including your own simulations) does not guarantee future
  results.

## 9. Self-quiz (answer these before attempting the real quiz)

Work through each question. If you cannot answer confidently, re-read the
relevant section above.

**Q1.** You open a SHORT perpetual position on PF_BCHUSD at $400 with 10× leverage
and $1,000 margin (isolated). BCH price rises to $500 (25% adverse move).
What happens to your position?

> Your position is liquidated. At 10× leverage, a ~10% adverse move wipes
> out your margin. A 25% move means you were liquidated well before $500,
> and the exchange force-closed your position. Your loss is approximately
> your entire $1,000 margin (possibly slightly more due to liquidation fee
> and slippage in a fast market).

**Q2.** What is the maximum you can lose on a single isolated-margin position?

> In normal conditions: the margin you posted to that position. In extreme
> conditions (gap, thin order book, liquidation engine slippage): potentially
> slightly more than posted margin. This is a tail risk that exchanges
> disclose but cannot eliminate.

**Q3.** You hold a SHORT perp and the funding rate is negative (perp trading
below spot). Who pays whom?

> When funding is negative (perp < spot), shorts pay longs. You would pay
> the funding rate. This is a cost of holding the position, separate from
> trading fees and from how the trade eventually closes.

**Q4.** What is the difference between margin and a trading fee?

> Margin is collateral. You get it back (minus any trading losses) when the
> position closes. A trading fee is a cost charged by the exchange on every
> order execution. Margin is not a fee, not a rent, and not a loss by itself.

**Q5.** Can you lose more than your initial deposit on Kraken Futures?

> Under cross margin: yes, your entire account balance is at risk from any
> single position. Under isolated margin: in normal conditions, no (capped
> at the margin posted to that position). In extreme gap/slippage scenarios,
> losses can slightly exceed posted margin even in isolated mode.

**Q6.** You have 2.5 months of paper (simulated) trading experience with
a systematic strategy, testnet validation, and zero real-money derivatives
trades. If the quiz asks about your trading experience, what do you say?

> State exactly that: simulated/paper trading experience only, zero real-money
> derivatives trades. Do not inflate. The quiz cross-checks answers against
> each other, so claiming deep experience while getting mechanics wrong
> is a more likely fail pattern than stating limited experience honestly.

**Q7.** Why does a perpetual future need a funding rate mechanism?

> Because it has no expiry date. Traditional futures converge to spot at
> expiry. Without that, there is nothing to anchor the perp price to the
> underlying. The funding rate does this: when the perp trades above spot,
> longs pay shorts (incentivizing shorts to push price down). When below
> spot, shorts pay longs. This pulls the perp price toward the spot price.

**Q8.** What happens if you use higher leverage?

> Higher leverage means your liquidation price is closer to your entry price.
> The position can survive less adverse price movement before being
> force-closed. Leverage does not change your expected return; it changes
> how much price movement you can survive.

---

**Related:** `docs/venue_port_admin_checklist.md` (step 3 status + retake date),
`results/venue_scouting_2026-06-10.md` (why Kraken is the presumptive primary
venue).
