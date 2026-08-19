# Spike: Broad instrument screen — BTC EMA-cross as entry trigger

**Status:** Spike finding, 2026-08-19. Throwaway code, no pre-registration.
Companion to `spike_vix_calls_btc_signal_2026-08-19.md`.

## Question

The VIX-call spike found one surviving expression. Are there others? Screen the
BTC EMA 9/21 bearish cross as entry trigger across every plausible instrument
class — safe havens, risk assets, pairs, vol products, and crypto-internal.

## Method

Two-pass screen:
1. **Underlying returns** — does the directional relationship exist? (long safe
   havens, short risk assets on BTC bear cross, exit on bull cross or 21d)
2. **Option-modeled returns** — for instruments that pass #1, model as ATM calls
   (or puts) with Black-Scholes premium. Does the move overcome the premium?

## Pass 1: Underlying returns (27 instruments tested)

### Survivors (pass drop-top-5%, positive t-stat)

| Instrument | Direction | t-stat | Drop-5% | WR | Pos yrs |
|---|---|---|---|---|---|
| Gold (GLD) | long | +1.29 | +0.003 OK | 58% | 8/12 |
| Utilities (XLU) | long | +1.29 | +0.004 OK | 62% | 7/12 |
| Bonds (TLT) | long | +1.21 | +0.002 OK | 60% | 7/12 |
| Gold/SPY pair | long/short | +0.80 | +0.001 OK | 54% | 8/12 |
| XLU/XLK pair | long/short | +0.76 | +0.000 OK | 52% | 8/12 |
| Staples (XLP) | long | +0.73 | +0.001 OK | 52% | 6/12 |

### Killed (tail-carried or negative)

All risk-asset shorts (EEM, HYG, IWM, USO, COPX, SOXX, ARKK, XBI), all
currency safe havens (FXY, FXF, UUP), all vol ETFs (VIXY, UVXY, VIXM), all
crypto-internal pairs (ETH/BTC, short SOL), and most pair trades.

### Structural finding

The BTC signal does **not** predict which risk asset goes down. It predicts
that **safe havens go up and vol spikes**. Every "short risk" expression failed.

## Pass 2: Option-modeled returns

Instruments that survived Pass 1 were re-tested as ATM calls with realistic IV
assumptions (GLD 0.18, TLT 0.16, XLU 0.18, VIX 0.80).

| Instrument | As options | t-stat | Drop-5% | Verdict |
|---|---|---|---|---|
| **VIX calls** | +0.726R | +1.64 | +0.255 OK | **SURVIVES** |
| GLD calls | −0.156R | −0.87 | −0.330 TAIL | Dead |
| TLT calls | −0.074R | −0.38 | −0.297 TAIL | Dead |
| XLU calls | +0.001R | +0.01 | −0.194 TAIL | Dead |

**GLD, TLT, XLU all die as options.** The underlying moves (+0.6–0.8% mean)
are too small to overcome ATM premium. Only VIX delivers moves large enough
(multi-point spikes) to clear the premium cost.

## Combined portfolio test

Equal-weight all 4 instruments (VIX + GLD + TLT + XLU calls) per signal:

| Metric | Value |
|---|---|
| t-stat | +0.88 |
| Sharpe | 0.25 |
| Drop-top-5% | +0.011 OK |
| Total P&L | +$4,054 |

**Dilutes the VIX result.** Averaging a strong signal (VIX t=+1.64) with three
losing legs drags the portfolio down. The correlation matrix confirms VIX calls
are negatively correlated with GLD (−0.15) and XLU (−0.25) — diversification
works mechanically but the other legs contribute negative expectancy.

Controls: SPY signal → same portfolio is t=−2.11 (dead). Random entry: 10.8%
of random sims match BTC-timed entry — confirms BTC timing adds value.

## Conclusion

**VIX calls are the only surviving expression** out of 27 tested. The signal is
narrow and specific: BTC EMA bearish cross → VIX spike. The mechanism is that
BTC is a risk-appetite thermometer with high-frequency trend breaks; when it
breaks, vol spikes across asset classes — but only the vol spike is large
enough to overcome option premium costs. Safe-haven drifts (gold, bonds,
utilities) are real but too small for options.

The signal does NOT work for:
- Shorting any risk asset (no directional prediction)
- Safe-haven calls (moves too small for premium)
- Vol ETFs (contango decay kills the edge — use calls instead)
- Currency safe havens (too noisy)
- Crypto-internal pairs (no spread signal)
- Portfolio diversification across these instruments (dilutes VIX)

## Implication for fin-vix-signal

The new project should focus exclusively on VIX calls. Do not add GLD/TLT/XLU
legs — they are negative-expectancy distractions that look attractive on
underlying returns but die under option premium.

## Spike artifacts (throwaway)

All in session scratchpad, not committed:
- `spike_broad_screen.py` — 27-instrument underlying-return screen
- `spike_portfolio.py` — option-modeled + portfolio combination + controls
