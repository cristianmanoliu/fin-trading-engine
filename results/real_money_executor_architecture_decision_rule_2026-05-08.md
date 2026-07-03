# Real-money executor architecture — locked design rule (2026-05-08)

**Status:** LOCKED 2026-05-08, design-only. Activates at STAGE_1 promotion. The interface contract and component decomposition below MUST be respected when the implementation actually lands; the implementation details (HTTP client, concurrency primitives, error-handling specifics) are deliberately NOT locked here so they can evolve under real-world feedback.

## Question

The current engine writes paper trades to a `Stub` Executor that synthesizes fills at internal prices. At STAGE_1 promotion ($100/trade real money) the engine MUST send actual orders to Binance USDT-M Futures. **The architecture isn't designed yet.** The `auto_kill_execution_decision_rule_2026-05-08.md` references "Path C — real-money market close" but the underlying mechanism doesn't exist.

Locking the architectural seam now — when there's no time pressure — prevents two failure modes:
- **Implementation-under-pressure:** when STAGE_1 fires, building the executor while watching the deploy gate fire is exactly when bad design crystallizes (e.g., synchronous HTTP calls that block the strategy goroutine, position state stored in memory only, etc.).
- **Drift between paper and real:** if the real executor's contract doesn't match the Stub's contract, every paper-trading invariant the project has built up could silently differ in real-money mode.

## Scope of this lock

**What this lock DECIDES:**
- The interface contract (matches the existing `Executor` interface)
- Component decomposition (which packages, which responsibilities)
- The failure-mode taxonomy (what kinds of errors and how each must be handled)
- Safety gates (which gates exist and what thresholds they enforce)
- Migration mechanism from Stub to BinanceLive
- Testing strategy

**What this lock DOES NOT decide:**
- Specific HTTP client library (defer to implementation)
- Specific concurrency primitives (defer)
- Specific retry/backoff parameters (calibrate during testnet integration)
- Specific Telegram message content (defer to operational tuning)

The discipline: lock decisions that are hard to change later (architecture is forever); leave decisions that can evolve (implementation details) to when there's real feedback.

## Component decomposition

The real-money executor is a stack of five packages, each with a narrow responsibility:

```
                ┌────────────────────────────────────────────────┐
                │ pkg/strategy/Runner                            │
                │   .executor.OnSignal(sig) / .OnTick(tick)      │
                └──────────────────────┬─────────────────────────┘
                                       │ Executor interface
                                       ▼
                ┌────────────────────────────────────────────────┐
                │ pkg/execution/BinanceLive  (replaces Stub)     │
                │   - implements Executor exactly                │
                │   - delegates to OrderRouter, Reconciler,      │
                │     SafetyGates, KillSwitch                    │
                │   - writes journal events identical schema     │
                │     to Stub                                    │
                └─────┬────────────┬───────────┬──────────┬──────┘
                      │            │           │          │
                      ▼            ▼           ▼          ▼
                  ┌────────┐ ┌──────────┐ ┌────────┐ ┌────────┐
                  │ Order  │ │ Position │ │ Safety │ │ Kill   │
                  │ Router │ │Reconciler│ │ Gates  │ │ Switch │
                  └────────┘ └──────────┘ └────────┘ └────────┘
```

### 1. `pkg/execution/BinanceLive` — Executor implementation

Implements the existing `Executor` interface from `pkg/strategy/engine.go` (`OnSignal`, `OnTick`, `Summary`). MUST match Stub's contract bit-for-bit on the JOURNAL OUTPUT schema — same fields, same units, same semantics — so downstream tooling (`forward_paper_status.sh`, drift detector, MFE/MAE analysis) works identically across paper and real-money runs.

**Locked fields in journal close events:** `ts, symbol, side, entry, exit, pnl_usd, outcome, fee_usd, slip_usd, notional_usd` (matches current Stub schema). Real-money close events MUST use the SAME field names and units. `fee_usd` and `slip_usd` come from Binance's reported fee/slippage on the actual fill, NOT from the modeled bps.

### 2. `pkg/execution/OrderRouter` — sends orders to Binance

Single responsibility: take a structured order intent (symbol, side, quantity, type, price), produce an exchange-state result (filled / rejected / partial / error). Stateless — no position tracking. Calls the exchange API and returns whatever the exchange said.

Locked behaviors:
- **Order types used:** MARKET only at STAGE_1-2 (simplicity); LIMIT-via-IOC may be added at STAGE_3+ if slippage analysis warrants.
- **No state cached:** every call is independent; recovery comes from PositionReconciler, not from OrderRouter memory.

### 3. `pkg/execution/PositionReconciler` — sync local with exchange

Periodically (every 60s by default; configurable, but ≥30s and ≤300s) queries Binance for the current position state on every deployed symbol. Compares against BinanceLive's local view. On mismatch, emits an operator alert and BLOCKS new orders on the affected symbol until the mismatch is resolved.

Locked behaviors:
- **Drift triggers SOFT pause** of the affected engine per `per_symbol_pause_decision_rule_2026-05-08.md` (cause type: operational/data integrity).
- **Drift detection mode:** absolute position size mismatch ≥ 0.01 contracts, OR side mismatch (engine thinks LONG, exchange reports SHORT or none), OR entry price differs by ≥10 bps from exchange-reported avg entry.
- **Reconciliation does NOT auto-correct** — it alerts. Operator decides between "fix local state to match exchange" and "fix exchange state to match local" based on root-cause investigation.

### 4. `pkg/execution/SafetyGates` — pre-order checks

Every order intent passes through SafetyGates BEFORE OrderRouter. Gates short-circuit a bad order BEFORE it hits the exchange. Three gates, applied in order:

**Gate A — Max-position cap:** total $-notional per symbol must be ≤ N × stake. N is per-stage:
- STAGE_1: N=1 (one position max per symbol; matches paper-trading)
- STAGE_2: N=1
- STAGE_3: N=1
- STAGE_4: N=1
- (No multi-position-per-symbol logic until a later milestone explicitly designs it.)

**Gate B — Daily-loss circuit breaker:** running total of realized losses over the prior 24h (rolling) must be ≤ M × stake. M is per-stage:
- STAGE_1: M=10 (cap: $1000 max daily loss before auto-pause)
- STAGE_2: M=12 (cap: $3600)
- STAGE_3: M=12 (cap: $6000)
- STAGE_4: M=15 (cap: $15000)

When tripped: SafetyGates returns rejection; engine SOFT-pauses for 24h (operational trigger #5 in `per_symbol_pause_decision_rule`); operator reviews.

**Gate C — Entry-price sanity:** the signal's entry price must be within K bps of the current best-bid/best-ask at order time. K=50 bps default. If the signal's entry has gone stale (signal fired but engine took >5s to reach OrderRouter), the order is REJECTED. Better to skip the trade than fill at a price the strategy didn't intend.

All three gates are bypassable ONLY by the KillSwitch (which is supposed to fire under conditions where safety gates would interfere).

### 5. `pkg/execution/KillSwitch` — immediate market-close-all

Single entry point: `KillAll(reason string) error`. Synchronously closes every open position on every deployed symbol via market orders, bypassing SafetyGates. Used by:
- `auto_kill_execution_decision_rule_2026-05-08.md` Phase 2 Path C
- TRIAGE-C investigation when investigation upgrades to active kill
- Operator emergency: `./scripts/kill_switch.sh CONFIRM` (confirmation gate prevents accidental fire)

Locked behaviors:
- **Synchronous:** caller blocks until all positions closed or error.
- **Best-effort:** if one symbol fails to close (e.g., exchange halted on it), the others still close; the failure is reported in the result and triggers per-symbol HARD-pause for that one.
- **Idempotent:** re-running KillAll is safe (closes already-closed positions = no-op).

## Failure-mode taxonomy

Five classes of failure the executor must handle:

### F1 — Network error (transient)

Examples: TCP timeout, TLS handshake fail, ECONNREFUSED.
**Handling:** retry with exponential backoff (3 attempts, base 1s, max 8s); if all retries fail, escalate to F2 treatment.

### F2 — Network error (sustained)

Examples: 60s+ of failed retries; DNS resolution failure.
**Handling:** PositionReconciler can't sync; new orders are blocked; existing positions remain in their last-known state. Operator alert. Does NOT auto-kill — exchange might recover.

### F3 — Exchange API error (bad request)

Examples: HTTP 400, 401, 403, malformed payload, signature mismatch.
**Handling:** TERMINAL for that order — do not retry. Log full request/response. Operator alert. Engine continues for other signals (this is a code-bug class).

### F4 — Exchange API error (rate limit)

Examples: HTTP 418, 429.
**Handling:** back off 60s (matches existing aggTrade backoff in `pkg/marketdata/binance.go`). Subsequent orders blocked during backoff. After backoff, resume; if rate-limit fires again within 5 min, escalate to operator alert (sustained pattern).

### F5 — Partial fill / position drift

Examples: order sent for 100 contracts, exchange filled 80; reconciler detects mismatch.
**Handling:** PositionReconciler raises drift; engine SOFT-pauses on the affected symbol; operator investigates and reconciles manually.

## Migration from Stub to BinanceLive

The migration is at STAGE_1 promotion time — atomic per engine.

**Pre-promotion checklist (mandatory, locked):**
1. BinanceLive code merged and code-reviewed.
2. Unit tests passing (mocked HTTP server, no real Binance calls).
3. Integration tests passing against **Binance USDT-M Futures TESTNET** (testnet API base: `testnet.binancefuture.com`). Tests must include: open + close round trip, partial fill, rate-limit handling, kill-switch firing.
4. Shadow-mode run for ≥7 days: BinanceLive runs in PARALLEL to Stub on the same input, sending orders to TESTNET; the two journals are diff'd; differences must be ≤0.5% on `pnl_usd` per closed trade (accounting for testnet vs synthetic-fill divergence) AND zero on signal generation.
5. Operator runbook for STAGE_1 promotion (separate doc, not in this lock) is reviewed.

**Promotion mechanism:**
1. Operator updates the engine config to specify executor = "binance_live" instead of "stub".
2. `deploy/redeploy.sh <symbol>` per the existing deploy flow.
3. PositionReconciler runs ONCE on startup to confirm exchange has no pre-existing position on this symbol; if it does, BLOCK startup with explicit error.
4. Engine fires its first real-money signal on the next 4H close after promotion.
5. Within 24h post-promotion: operator manually verifies the first 1-2 trades' execution quality (fill price vs signal price, realized fee vs modeled, no drift).

**Rollback mechanism:**
- Per `per_symbol_pause_decision_rule_2026-05-08.md` SOFT trigger #3 (operational config), the operator can revert config to executor = "stub" and `deploy/redeploy.sh <symbol>` to roll back to paper mode.
- Open real-money positions remain at the exchange; operator manages them via Binance UI until natural close.

## Testing strategy

Three layers, each gating the next:

### Layer 1 — Unit tests with mocked HTTP

Standard Go testing patterns. The OrderRouter takes an `http.Client` interface; tests inject a mock that returns canned responses. Test scenarios: success path, every F1-F5 failure mode, signature generation correctness, payload structure correctness.

### Layer 2 — Integration against Binance TESTNET

The testnet provides a faithful replica of the production API with paper money. Tests are SLOWER (real network) and require testnet API credentials (separate from prod). Test scenarios:
- Full round trip (signal → order → fill → close → realized P&L)
- KillSwitch fires across multiple symbols
- PositionReconciler detects synthetic drift (manually open a position via testnet UI; verify reconciler raises alert)
- Rate limit triggers backoff (run a flood of orders)

### Layer 3 — Production shadow mode

Before flipping any single engine to BinanceLive in production, run BinanceLive in PARALLEL on testnet alongside the existing Stub on the same input ticks. Run for ≥7 days. Diff the journals. Acceptance: pnl_usd per closed trade differs by ≤ 0.5%, no signal-generation divergence.

This isn't a test in the strict sense — it's a final validation that the contract is preserved across paper and real.

## Edge cases — pre-locked

### Engine restarts mid-real-money-trade

Recovery uses the same `Stub.RecoverFromJournal` mechanism — BinanceLive inherits this. The reconstructed position state is then VERIFIED against the exchange via PositionReconciler immediately on restart. If exchange disagrees with the recovered local state, BLOCK startup with explicit alert; operator manually reconciles.

### Exchange-reported funding cost differs from local model

Binance reports actual funding per trade. The journal MUST use the actual reported value, NOT the local model's estimate. The journal field is `funding_usd` (already in schema). Drift between modeled and actual is informative for milestone-2 calibration.

### Slippage exceeds the modeled 5 bp on losers

The journal field `slip_usd` records ACTUAL slip, computed as (modeled_stop_price − actual_fill_price) × notional in adverse direction. The locked kill criterion (slip > 25 bp on losers) uses actual; modeled-vs-actual divergence is data, not a kill trigger by itself.

### Order intent for a symbol that exchange has halted

OrderRouter returns F3 (bad request). The engine logs and SOFT-pauses on the affected symbol per `per_symbol_pause_decision_rule_2026-05-08.md` operational trigger #1 (exchange halt).

### Telegram bot for alerts is unreachable

Alerts are best-effort. Reconciliation drift, F2 sustained, etc., still LOG locally even if Telegram fails. Operator who reviews logs manually catches it on the next pass.

### Two-engine race for the same symbol's exchange position

Should NEVER happen — each symbol has exactly one engine, and PositionReconciler runs in BinanceLive's process. But defensively: the Reconciler emits an alert if it detects another order on the symbol that BinanceLive didn't send. This catches operator-initiated orders via Binance UI (e.g., during a kill).

## Alternatives considered (rejected)

### Use a third-party SDK (e.g., `go-binance`)

**Considered:** vendored library reduces boilerplate.

**Rejected because:** Binance API surface is small for our use (place order, query position, query account); the SDK adds dependency surface (auth flow assumptions, error type mapping, transitive deps) we don't need. Direct HTTP with our own signing keeps the code base self-contained and makes the executor's behavior fully under our control. Re-evaluate if the API surface grows (e.g., listening for fill events via user data stream).

### WebSocket-based order entry instead of REST

**Considered:** WebSocket lower latency for order entry.

**Rejected because:** at 4H signal-tf, order latency budget is hours, not milliseconds. REST is simpler, more debuggable, and the existing engine already uses both REST (aggTrade fallback) and WebSocket (price feed). No reason to add WS for orders specifically. Re-evaluate if STAGE_X+ ever moves to a sub-minute signal-tf.

### Strict synchronous execution (block strategy goroutine on each order)

**Considered:** simplest design, no concurrency.

**Rejected because:** the strategy goroutine MUST process ticks; blocking it on a 500ms HTTP round-trip per signal would back up the tick channel and could cause data loss. BinanceLive's `OnSignal` MUST return quickly; order execution happens in a goroutine spawned per-signal, with results journaled async. The journal write is the synchronization point.

### Combined OrderRouter + PositionReconciler in a single component

**Considered:** simpler component count.

**Rejected because:** they have orthogonal concerns. OrderRouter is stateless (each call independent); PositionReconciler is stateful (background loop comparing local vs exchange). Combining them muddles their lifecycles and complicates testing (you'd need to mock the reconciler to test the router and vice versa).

## Migration triggers

The rule re-opens for design when:

1. **STAGE_1 promotion** — when the implementation actually lands. Validate that the locked decomposition + failure modes hold up; recalibrate any threshold (Gate A/B/C parameters) based on testnet integration data.
2. **First F2/F5 incident in production** — review whether the locked handling was correct or needs refinement.
3. **Multi-strategy fleet** — currently one strategy + 3 shadows. If multiple strategies become live (real money on more than one), the executor decomposition may need a strategy-id-aware reconciler.
4. **Move to LIMIT-IOC orders** — if STAGE_3+ slippage analysis indicates LIMIT-with-fallback would reduce realized slip materially, OrderRouter's order-type set expands; gate parameters may need recalibration.
5. **Cross-exchange execution** — if Cat X (Bybit) replication ever becomes live, executor decomposition must abstract over exchange identity.

## Cross-references

- `pkg/execution/stub.go` — the contract BinanceLive must match
- `pkg/strategy/engine.go` — defines the Executor interface
- `pkg/notify/telegram.go` — alerting infrastructure for operator notifications
- `pkg/marketdata/binance.go` — reference for HTTP client patterns + rate-limit backoff
- `results/auto_kill_execution_decision_rule_2026-05-08.md` — Path C (real-money market close) is implemented by KillSwitch
- `results/per_symbol_pause_decision_rule_2026-05-08.md` — operational triggers that BinanceLive's Reconciler can fire
- `results/real_money_protocol_decision_rule_2026-05-08.md` — STAGE thresholds that parameterize Gate A/B
- `CLAUDE.md ## Strategy status` — the line that flips from "Live: paper" to "Live: real-money STAGE_1" when this lock activates

## Addendum 2026-06-11 — Gate A/B recalibration (migration trigger #1, testnet integration data)

Sanctioned by migration trigger #1 ("recalibrate any threshold (Gate A/B/C
parameters) based on testnet integration data"). The triggering data: the
first-ever live signal to reach the Layer 3 testnet shadow (KAVAUSDT
2026-06-10 04:00 UTC) was blocked by Gate A, and inspection showed the block
rate is 100% by construction.

**Gate A basis corrected: notional → per-trade risk.** The locked text read
"total $-notional per symbol must be ≤ N × stake" with intent "one position
max per symbol; matches paper-trading". The intent conflated notional with
stake: the strategy risk-sizes entries (qty = stake / stop-distance), so
notional runs 20–50× stake by construction (KAVAUSDT instance: $1,000 stake →
$21,405 notional, 4.67% stop). A notional cap at 1× stake therefore blocks
every realistic order at every stage — including STAGE_1 real money. Gate A
now checks per-trade risk: qty × |entry − stop| ≤ N × stake (N unchanged, 1
at STAGE_1–4). Position concurrency (the actual intent) is enforced upstream
by the position-already-open guard in handleSignalSync. A missing/zero stop
fails closed. Regression: `TestSafetyGates_GateA_RealisticNotionalPasses`
(pins the 2026-06-10 KAVAUSDT geometry).

**Gate B cap re-expressed as 10× stake.** The locked $1,000 figure was sized
for the STAGE_1 $100 stake (10×). Layer 3 shadows run the $1,000 paper stake,
where a flat $1,000 trips on a single typical stop loss (observed paper
losses $1,061–$1,247 incl. fees/slip) and would censor 24h of parity window
per loss. Constructor default is now `10 × stake`; stage activation still
overrides explicitly per the protocol table (STAGE_2 remains $3,600 as
locked — the override, not the default, governs at promotion).

**Discovery context + alert-loss bug** (Telegram parse_mode=Markdown 400 on
underscores, fixed same day): `docs/findings/2026-06-11.md`. Test fixtures
that had been calibrated around the broken gate (`MaxPositionMultiple = 1e9`
workaround, $100-notional fantasy geometry) were replaced with
production-config fixtures — writer-equals-fixture pattern lock.

## Addendum 2026-07-04 — order-quantity quantization (migration trigger #1, Layer 3 testnet data)

Sanctioned by the same migration trigger as the 2026-06-11 addendum: Layer 3
doing exactly its job — surfacing execution-layer defects on play money that
would otherwise have surfaced at STAGE_1.

**Defect.** All three live signals that reached the Layer 3 shadow after the
Gate A fix (ENSUSDT 2026-06-19 08:00, KAVAUSDT 2026-06-20 04:00 and
2026-06-23 08:00 UTC) were rejected by the testnet with -1111 "Precision is
over the maximum defined for this asset": `qty = stake / stop_distance` was
serialized at full float precision (e.g. `828088.2810…`), never aligned to
the symbol's LOT_SIZE step (KAVA/ENS: 0.1). Every prior gate passed — the
order died at the exchange filter layer. CRITICAL Telegram alerts fired per
design on all three rejections. The parity clock is therefore still at zero.

**Fix (fix commit d08ee30).** `BinanceLive` lazily fetches LOT_SIZE +
MARKET_LOT_SIZE from `/fapi/v1/exchangeInfo` (cached per process; coarser
step / tighter maxQty of the two governs, entry orders being MARKET), floors
qty to the step through a fixed-decimal round-trip so it serializes cleanly,
and rejects loudly (CRITICAL) when the quantized qty falls outside
[minQty, maxQty]. exchangeInfo failure is fail-CLOSED (WARN, no order) — the
unquantized order was a guaranteed rejection anyway.

**Known residual.** Testnet KAVAUSDT/ENSUSDT `maxQty = 1,000,000`: the
$1,000-stake shadow on the tightest stops (the 06-23 KAVA signal sized to
qty 1.76M) still cannot be placed — now rejected pre-flight by us, loudly,
instead of by the venue. This is a stake-geometry limit, not a defect:
STAGE_1's $100 stake sits 10× under the cap. Parity dispositions should note
any such skipped signal.

**No locked thresholds, stages, gates, or parity tolerances change.**
