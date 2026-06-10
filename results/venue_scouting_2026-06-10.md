# Venue scouting for the real-money port — descriptive, non-binding (2026-06-10)

**Type:** research note (NOT a pre-registration; the port milestone, if it ever activates,
pre-registers its own venue decision). Triggered by Binance support confirming **no EEA
futures activation planned, no timeline** — the venue port is now the default real-money
path if forward-paper emits PROMOTE.

**Method:** EU-access verified via current (2026-06) regulatory reporting; symbol coverage
verified DIRECTLY against each venue's public instruments API on 2026-06-10 (not from
marketing pages); fees from official schedules.

## The comparison (deployed-16 coverage checked live)

| venue | EEA retail perps | our 16 symbols | taker/maker (base) | RT taker cost | leverage | notes |
|---|---|---|---|---|---|---|
| **Kraken Pro (Payward Europe, CySEC 342/17, MiFID II)** | **LIVE for EU clients** | **16/16** ✅ | 5bp / 2bp | **10bp = backtest assumption exactly** | up to 10× | EUR accepted as collateral (operator funds in EUR); EEA-specific fee page confirms same-as-global schedule; partial liquidations going live 2026-03-26 |
| Bybit EU (MiCAR Austria, FMA) | NOT yet — derivatives "rolling out" under MiFID II, per-state leverage caps | 16/16 (SHIB as SHIB1000USDT) | 5.5bp / 2bp | 11bp | varies by state | spot+margin live; perps timeline unclear; re-check at port time |
| OKX EU (MiCA+MiFID II, full EEA passport) | stock X-perps live for retail; crypto-perp retail scope to verify | unverified (API geo-403) | 5bp / 2bp | 10bp | varies | strong licenses; verify crypto-perp retail access + instruments at port time |
| Hyperliquid (DEX) | accessible (no EU gate today; US-blocked) | **12/16 — missing ROSE, GRT, 1INCH, KAVA** | 4.5bp / 1.5bp | 9bp | high | hourly funding (model change); self-custody/bridge ops; MiCA July-2026 boundary = regulatory uncertainty for EU users |

Binance baseline for reference: 5bp taker (4.5bp w/ BNB) — blocked for operator (EEA).

## Findings

1. **No venue materially beats our modeled costs; one matches them with full coverage.**
   Kraken = 10bp RT taker at base tier — the exact `--fee-bps 10` the entire validation
   stack assumes. Hyperliquid's 9bp is 10% cheaper but drops 4 of 16 deployed symbols
   (−25% of the book) — a worse trade than the fee saving.
2. **Kraken is the presumptive port target** (pending its own pre-reg): EU-regulated
   (MiFID II), 16/16 coverage, identical fee geometry, EUR collateral matches the
   operator's funding currency, documented EEA fee schedule.
3. **The 10× leverage cap changes CAPITAL requirements, not strategy viability.** Binance
   implicit leverage ran ~50× notional/stake. At 10×, margin per position ≈ notional/10:
   a $100-risk trade at ~2% stop ≈ $5k notional ≈ $500 margin. With 3-4 concurrent
   positions, STAGE_1 needs ~$1.5-2k of margin capital, not ~$300. **The port pre-reg
   must model margin-vs-concurrency explicitly** (this interacts with the already-open
   "margin reuse / concurrent trades unmodeled" risk).
4. **Maker-entry opportunity (port-milestone test, not now):** Kraken maker = 2bp; if
   entries can rest as limit orders without degrading fill quality, RT drops toward
   4-7bp — a bigger saving than the lost BNB discount. Requires its own validation
   (changes fill behavior).
5. **Funding model check at port time:** Kraken perp funding mechanics must be re-based
   against our funding-CSV assumptions (Binance 8h cadence baked into accrual + the
   shorts-receive-on-average finding).
6. Bybit/OKX = credible fallbacks; both need a fresh access+instruments check at port
   time (Bybit perps not yet live for EEA retail; OKX crypto-perp retail scope
   unverified, their API geo-blocks this IP).
7. **Spot/margin shorting rejected** (any venue): strategy is short-only; borrow interest
   replaces funding income shorts currently receive; alt borrow supply unreliable; 5y of
   perp-denominated validation wouldn't transfer.
8. Romania-specific community signal (operator request): no useful Reddit threads
   surfaced; aggregator listings for Romania name Bitget/Bybit/Kraken/Binance — weak
   evidence either way, superseded by the direct license/API checks above.

## Non-actions

No code, no accounts, no executor work until the forward-paper verdict. If PROMOTE fires,
the port milestone opens with this note as input and pre-registers: venue, fee/funding
re-basing, margin-concurrency model, Layer 2/3-equivalent gates on the chosen venue.
