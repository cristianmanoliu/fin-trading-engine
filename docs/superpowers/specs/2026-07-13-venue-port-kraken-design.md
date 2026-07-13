# Venue port pre-registration — Kraken Futures executor (2026-07-13)

**Type:** standalone pre-registration (decision rule). Self-contained; references
existing locked docs as inputs. NOT an addendum to any prior pre-reg.

**Status:** LOCKED 2026-07-13. Activates when forward-paper emits PROMOTE or
OPERATOR_REVIEW resolved as promote (~2026-08-25 estimated). Applies mechanically
at that point; no further design decisions required.

**Operator sanction:** operator explicitly opened this pre-reg 2026-07-13 (session
log). Per CLAUDE.md: "Port = new executor + fresh Layer 2/3-equivalent validation +
fee/funding model re-check, pre-registered. Do NOT start the port before the
promote/kill verdict."

---

## 1. Scope — what this pre-reg decides

Four things, in dependency order:

1. **Venue:** Kraken Futures (`futures.kraken.com`, Payward Europe CySEC 342/17,
   MiFID II) as the execution venue for STAGE_1+. Binance USDT-M Futures is
   region-blocked for this operator (confirmed 2026-06-10 by Binance support; no
   EEA futures activation planned). OKX EU / Hyperliquid / Bybit EU are named
   fallbacks if Kraken access fails at port time (§6).
2. **Book:** 16 deployed symbols, unchanged. Signal data source: Binance public
   klines (unchanged — no data-side redesign in this milestone). Book expansion
   (up to 55 Kraken-covered symbols) is deferred to a named follow-on pre-reg,
   opened after STAGE_1 is stable and a per-symbol realistic-slippage screen has
   been run at Kraken's cost model.
3. **Executor architecture:** `KrakenLive` implementing the existing `Executor`
   interface from `pkg/strategy/engine.go`. Architecture mirrors
   `real_money_executor_architecture_decision_rule_2026-05-08.md` by reference.
   This pre-reg specifies only the Kraken-specific deltas (§4).
4. **Validation gates:** Layer 2/3-equivalent on Kraken before STAGE_1 real money
   (§5).

### What this pre-reg does NOT decide

- Internal implementation details of `KrakenLive` (HTTP client, concurrency
  primitives, retry parameters) — deferred to the implementation plan.
- Book expansion beyond 16 symbols — deferred to a follow-on pre-reg.
- Maker-order entry optimization (limit-IOC at STAGE_3+) — inherited from the
  Binance architecture rule, unchanged.
- Funding-net-zero revalidation — post-STAGE_1 audit item; funding model carries
  over (§4, delta 4).

---

## 2. Staging protocol amendment

`real_money_protocol_decision_rule_2026-05-08.md` reads: *"Exchange: Binance
USDT-Perp only."* This pre-reg supersedes that clause:

> **Exchange = Kraken Futures (Payward Europe) for all STAGE_1+ deployment.**

All other clauses in that doc carry over verbatim: promotion criteria, kill
triggers, stage thresholds (STAGE_1 $100 → STAGE_4 $1,000), drift-detector
cadence, locked kill criteria.

### Stake definition (unchanged)

**Stake = dollars-at-risk per trade** = (entry − stop) × qty, same as the locked
protocol. "Stake" is NOT margin posted. At STAGE_1, $100 at risk per trade.

### Capital planning note (not a rule)

At $100 stake, a 2% stop → ~$5k notional → ~$100–500 margin per position
(Kraken tier-1 initial margin: 2% for 50× instruments, 5% for 20×, 10% for 10×;
verify actual EEA-retail limits post-KYC — step 4 of
`docs/venue_port_admin_checklist.md`). With ≤4 concurrent positions, ~$1.5–2k on
deposit supports STAGE_1. Operator has $100k–$300k available post-PROMOTE;
capital is not the binding constraint.

---

## 3. Instrument coverage

All 16 deployed symbols are confirmed tradeable on Kraken Futures as of 2026-07-04
(verified against `futures.kraken.com/derivatives/api/v3/instruments`; see
`results/venue_scouting_2026-06-10.md` addendum).

| Binance symbol  | Kraken perp   | Tier-1 max lev | Initial margin |
|:----------------|:--------------|---------------:|:---------------|
| ROSEUSDT        | PF_ROSEUSD    | 10×            | 10%            |
| BCHUSDT         | PF_BCHUSD     | 50×            | 2%             |
| GRTUSDT         | PF_GRTUSD     | 20×            | 5%             |
| 1INCHUSDT       | PF_1INCHUSD   | 10×            | 10%            |
| ADAUSDT         | PF_ADAUSD     | 50×            | 2%             |
| KAVAUSDT        | PF_KAVAUSD    | 10×            | 10%            |
| 1000SHIBUSDT    | PF_SHIBUSD    | 50×            | 2%             |
| ENSUSDT         | PF_ENSUSD     | 20×            | 5%             |
| XLMUSDT         | PF_XLMUSD     | 50×            | 2%             |
| IMXUSDT         | PF_IMXUSD     | 20×            | 5%             |
| ETCUSDT         | PF_ETCUSD     | 50×            | 2%             |
| RUNEUSDT        | PF_RUNEUSD    | 20×            | 5%             |
| AVAXUSDT        | PF_AVAXUSD    | 50×            | 2%             |
| APTUSDT         | PF_APTUSD     | 50×            | 2%             |
| DOTUSDT         | PF_DOTUSD     | 50×            | 2%             |
| FILUSDT         | PF_FILUSD     | 50×            | 2%             |

Tier-1 max leverage = instrument maximum. Actual EEA-retail limits may be lower
post-KYC; verify and record before STAGE_1 (admin checklist step 4).

---

## 4. Kraken-specific deltas vs Binance architecture rule

The Binance architecture rule (`real_money_executor_architecture_decision_rule_
2026-05-08.md`) specifies the full component decomposition (BinanceLive /
OrderRouter / PositionReconciler / SafetyGates / KillSwitch), failure-mode
taxonomy (F1–F5), journal schema, and migration mechanism. All of that applies to
`KrakenLive` by reference. Only the following five deltas are Kraken-specific:

### Delta 1 — API signing

Kraken Futures v3 authentication: HMAC-SHA512 over SHA256(postData + nonce +
endpoint-path), with the path stripped of the `/derivatives` prefix. Already
implemented and tested in `scripts/kraken_demo_smoke.py`. The implementation
MUST use this exact scheme — do not adapt the Binance HMAC-SHA256 signing.

Credentials: stored in `~/.kraken-futures.env` (production read-only + trade
key pairs, created per admin checklist step 6). Never in the repo.

### Delta 2 — Symbol mapping

Binance `XYZUSDT` → Kraken `PF_XYZUSD` for all symbols except:

> **1000SHIBUSDT → PF_SHIBUSD with qty ×1000 and price ÷1000.**

Kraken trades raw SHIB (1 unit = 1 SHIB); Binance trades the 1000-unit contract.
The port executor MUST apply this conversion on every order and reconciliation
call for this symbol. All other 15 symbols map directly (strip `USDT`, prepend
`PF_`, append `USD`).

### Delta 3 — Order types available

Kraken Futures supports MARKET and LIMIT (IOC/GTC) on all deployed symbols.
MARKET at STAGE_1–2 (same as Binance architecture rule). Limit-IOC consideration
deferred to STAGE_3+ per the locked rule. No change from the Binance plan.

### Delta 4 — Funding cadence

Kraken perps use 8h funding (same cadence as Binance USDT-M). The funding-CSV
accrual model (`pkg/funding/`) carries over unchanged; absolute rates will differ
from Binance historical CSVs. **The funding-net-zero finding is NOT revalidated
in this port milestone** — it is a post-STAGE_1 audit item. If STAGE_1 data shows
meaningful funding-cost divergence from the modeled zero-net, open a fresh pre-reg.

### Delta 5 — Fee model

Kraken base-tier taker = 5bp/side = 10bp round-trip. Exact match to `--fee-bps 10`
backtest assumption. No fee-model recalibration required unless the operator's
account lands on a non-base tier. **Record the actual fee tier from the Kraken
account dashboard before STAGE_1** (admin checklist step 4) and confirm it matches.
If the tier differs, the port pre-reg requires an addendum before STAGE_1 proceeds.

---

## 5. Validation gates (Layer 2/3-equivalent)

Both gates must pass before STAGE_1 real money. Either gate failure = investigation
before proceeding (not auto-kill; the executor is new and some failures are
expected and fixable).

### Layer 2 — Demo fills (primary) / prod micro-orders (sanctioned fallback)

**Primary path — demo shadow:**

`KrakenLive` configured to target `demo-futures.kraken.com`. Runs as a shadow
alongside the paper-live engine on the same tick stream (same mechanism as the
existing Layer 3 Binance testnet shadow). Pass criteria:

- ≥5 signals reach the demo shadow AND complete (fill + journal close event);
  higher bar than Layer 3 because demo fills are less reliable (synthetic exchange)
- PnL parity on those signals within 0.5% (same threshold as `cmd/journal_diff`)
- No unexpected gate rejections (expected: Gate A/B on deliberate test inputs;
  unexpected: signing errors, symbol-mapping failures, fill timeouts)
- Window: 7 days minimum from first signal reaching the shadow

**Sanctioned fallback — prod micro-orders:**

Activates if `demo-futures.kraken.com` is still unreliable at port time, defined
as: ≥3 consecutive calendar days where the public endpoint
`GET /derivatives/api/v3/instruments` returns non-200 responses. Operator makes
the call and logs it in the port session doc. No fresh pre-reg needed; this
fallback is pre-sanctioned here.

Fallback procedure:
1. Place ≥10 real prod orders at €10/trade stake each: MARKET entry on a deployed
   symbol, followed immediately by a MARKET close. No strategy signals — these
   are execution-path smoke tests only.
2. Verify: signing accepted, fills confirmed, margin debited/credited correctly,
   journal events written with correct schema.
3. Budget: ≤€50 total (2× taker fee per round-trip = ~€1/trade at €10 stake;
   remainder is noise slippage). These are planned losses; do not count toward
   forward-paper or STAGE_1 trade count.
4. Pass criteria: all 10 orders fill and close without errors; margin behavior
   matches expectation; journal schema matches Stub output.

### Layer 3 — Same-tick shadow parity

`KrakenLive` (Layer 2 validated) runs as a named shadow alongside paper-live on
the same tick stream for 7 days. Uses the existing `cmd/journal_diff` comparator
and `layer3_verdict.sh`-equivalent logic adapted for Kraken journal paths.

Pass criteria:
- ≥3 signals reach the Kraken shadow during the 7-day window; lower bar than
  Layer 2 because these are real-exchange fills on the production tick stream
- All signals: entry/stop/target match the paper-live signal (same tick source)
- Fill prices within 1% of the signal price (market-order slippage tolerance)
- No unexpected gate rejections during the window
- `cmd/journal_diff` exits 0 on the paired journals

If the 7-day window produces <3 signals (trade rate ~1.18/day means ~8 expected;
<3 signals implies a shadow-config or signal-routing bug, not a low-signal-rate
issue — investigate before extending the window).

---

## 6. Venue fallback ladder

If Kraken access fails at port time — appropriateness retake fails again, KYC
rejected, or API access blocked — proceed in order:

| Priority | Venue | Blocker to verify | Book gap |
|:--------:|:------|:------------------|:---------|
| 1 | **OKX EU** (MiCA+MiFID II) | crypto-perp retail access for Romanian client; 16-symbol coverage (API geo-403'd at scouting) | unknown — verify at port time |
| 2 | **Bybit EU** (MiCAR Austria/FMA) | perps live for EEA retail (was "rolling out" at scouting); per-state leverage caps | 16/16 on Binance; check Bybit |
| 3 | **Hyperliquid** (DEX) | hourly funding (model change); MiCA July-2026 boundary; bridge/self-custody ops | 12/16 — missing ROSE, GRT, 1INCH, KAVA |

Fallback activation: operator decision, logged in the port session. Each fallback
requires:
- Fresh instrument/fee/funding verification pass
- Fee-model confirmation (does `--fee-bps 10` still hold?)
- Funding-cadence check (hourly vs 8h for Hyperliquid = model change requiring
  a fresh pre-reg)
- A new Layer 2/3-equivalent validation cycle on the chosen venue

**This pre-reg does NOT pre-sanction code work for fallback venues.** If a
fallback activates, open a new pre-reg scoped to that venue before any
implementation begins.

---

## 7. What opens at PROMOTE

When the forward-paper verdict fires PROMOTE (or OPERATOR_REVIEW resolved as
promote), the port milestone opens with the following ordered sequence:

1. **Confirm Kraken access** — appropriateness passed, KYC complete, futures
   activated, fee tier recorded (admin checklist steps 1–6 complete). If not,
   escalate to fallback ladder (§6).
2. **Confirm fee tier matches** — actual tier = base (10bp RT). If not, addendum
   required before proceeding.
3. **Build `KrakenLive`** — implement the executor per the Binance architecture
   rule + deltas in §4. Symbol mapping table in §3 is the canonical reference.
4. **Layer 2 validation** — demo shadow (primary) or prod micro-orders (fallback)
   per §5.
5. **Layer 3 validation** — same-tick shadow parity per §5.
6. **STAGE_1 deploy** — $100/trade stake on Kraken prod. All other staging
   protocol criteria from `real_money_protocol_decision_rule_2026-05-08.md`
   apply from this point.

No step may be skipped. Steps 4 and 5 are sequential (Layer 3 requires Layer 2
to pass first). Steps 1–3 may proceed in parallel where practical.

---

## 8. References

- `results/real_money_executor_architecture_decision_rule_2026-05-08.md` — locked
  component decomposition, failure-mode taxonomy, safety gates, journal schema.
  Inherited by reference; §4 specifies deltas only.
- `results/real_money_protocol_decision_rule_2026-05-08.md` — staging thresholds,
  promotion criteria, kill triggers. §2 of this doc supersedes the venue clause only.
- `results/venue_scouting_2026-06-10.md` — EEA venue comparison; Kraken selection
  rationale; full-57-symbol economics; leverage addendum (2026-07-04).
- `docs/venue_port_admin_checklist.md` — operator-side account/KYC/API prep.
  Steps 1–6 must be complete before Layer 2 validation begins.
- `scripts/kraken_demo_smoke.py` — Kraken v3 signing implementation (reference for
  Delta 1); smoke-tests the demo and prod endpoints.
