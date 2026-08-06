package execution

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cristianmanoliu/fin-trading-engine/pkg/models"
	"github.com/cristianmanoliu/fin-trading-engine/pkg/notify"
)

// ErrRecoveryDrift is returned by BinanceLive.RecoverFromJournal when the
// journal-recovered local state disagrees with the exchange. Per the locked
// rule "Engine restarts mid-real-money-trade" in
// results/real_money_executor_architecture_decision_rule_2026-05-08.md, the
// engine MUST NOT proceed with tick processing in this case — the operator
// must investigate, reconcile (close exchange position OR adjust journal),
// then restart. Use errors.Is(err, ErrRecoveryDrift) to detect.
var ErrRecoveryDrift = errors.New("recovery drift: exchange disagrees with journal-recovered state")

// BinanceLive is the real-money executor that replaces Stub at STAGE_1
// promotion. Implements pkg/strategy.Executor (OnSignal / OnTick / Summary)
// with bit-for-bit journal-schema parity to Stub so downstream tooling
// (forward_paper_status.sh, drift detector, MFE/MAE analysis) is identical
// across paper and real-money runs.
//
// Five-component decomposition per the locked design:
//   - BinanceLive itself: implements pkg/strategy.Executor
//   - OrderRouter:        sends orders, reports results, stateless
//   - PositionReconciler: periodically syncs local vs exchange state
//   - SafetyGates:        pre-order checks (max-risk, daily-loss, entry-price)
//   - KillSwitch:         immediate market-close-all entry point
//
// Concurrency model (locked): OnSignal MUST return quickly so the strategy
// goroutine doesn't back up on HTTP latency. The order-send + journal-write
// happens in a goroutine spawned per signal; same pattern for stop/target
// exits. Wait() drains pending goroutines (used by Summary + tests).
type BinanceLive struct {
	Symbol    string
	StakeUSD  float64
	APIKey    string
	APISecret string

	// JournalPath: directory where per-symbol JSONL trade journals are written
	// (one file per calendar month). Mirrors Stub.JournalPath exactly.
	JournalPath string

	// FeeBps: round-trip taker fee in basis points; fallback when the exchange
	// does not report fee on the fill. Mirrors Stub.FeeBps semantics.
	FeeBps float64

	// StopSlippageBps: adverse stop slippage in bps (losers only) when the
	// actual fill matches the modeled stop exactly (i.e., no measurable real
	// slip). Mirrors Stub.StopSlippageBps semantics.
	StopSlippageBps float64

	// MaxHoldHours: when > 0, any open position older than this is force-closed
	// at the current tick price. 0 disables. Mirrors Stub.MaxHoldHours.
	MaxHoldHours float64

	// Notifier is opt-in. When set, BinanceLive emits SeverityCritical alerts
	// on hard failures (drift, order-rejection, kill-switch firing).
	Notifier *notify.Notifier

	// Components. Constructed and ready by NewBinanceLive.
	OrderRouter        *OrderRouter
	PositionReconciler *PositionReconciler
	SafetyGates        *SafetyGates
	KillSwitch         *KillSwitch

	// State (mutex-guarded — accessed from strategy goroutine via OnTick AND
	// from spawned order-handling goroutines via handleSignalSync /
	// handleExitSync).
	mu           sync.Mutex
	position     *OpenPosition
	exitPending  bool
	lastTick     models.Tick
	results      []tradeResult
	journalFile  *os.File
	journalMonth string

	// wg tracks in-flight order-handling goroutines so Summary + tests can
	// drain them deterministically.
	wg sync.WaitGroup

	// Symbol quantity filters, fetched lazily from /fapi/v1/exchangeInfo
	// before the first order and cached for the process lifetime
	// (mu-guarded). A quantity sent with more precision than the symbol's
	// LOT_SIZE stepSize is rejected -1111 "Precision is over the maximum"
	// — which silently blocked all three Layer 3 testnet entries
	// 2026-06-19..23 (qty = stake/stop_dist is essentially never
	// step-aligned). Entry sizing floors qty to qtyStep; fetch failure is
	// fail-CLOSED (no order) so a filters outage can't reintroduce -1111.
	filtersOK   bool
	qtyStep     float64
	qtyMin      float64
	qtyMax      float64
	qtyDecimals int
}

// MainnetAPIBaseURL is the production Binance USDT-M Futures REST endpoint.
// Used by NewBinanceLive (the default real-money executor).
const MainnetAPIBaseURL = "https://fapi.binance.com"

// TestnetAPIBaseURL is the Binance USDT-M Futures TESTNET REST endpoint —
// faithful replica of mainnet with paper-money fills, separate API credentials.
// Used by NewBinanceLiveTestnet to satisfy the Layer 2 integration gate from
// real_money_executor_architecture_decision_rule_2026-05-08.md before any
// engine is flipped to mainnet for STAGE_1.
const TestnetAPIBaseURL = "https://testnet.binancefuture.com"

// NewBinanceLive constructs a BinanceLive with default-initialized components.
// Construction is safe — only method calls error.
func NewBinanceLive(symbol string, stakeUSD float64, apiKey, apiSecret string) *BinanceLive {
	bl := &BinanceLive{
		Symbol:    symbol,
		StakeUSD:  stakeUSD,
		APIKey:    apiKey,
		APISecret: apiSecret,
		OrderRouter: &OrderRouter{
			APIBaseURL: MainnetAPIBaseURL,
			APIKey:     apiKey,
			APISecret:  apiSecret,
			HTTPClient: &http.Client{Timeout: 10 * time.Second},
			RecvWindow: 5000,
		},
		PositionReconciler: &PositionReconciler{
			PollInterval:       60 * time.Second,
			APIBaseURL:         "https://fapi.binance.com",
			APIKey:             apiKey,
			APISecret:          apiSecret,
			HTTPClient:         &http.Client{Timeout: 10 * time.Second},
			RecvWindow:         5000,
			QtyTolerance:       0.01, // locked: drift fires at |Δqty| ≥ 0.01 contracts
			EntryPriceBpsLimit: 10,   // locked: drift fires at |Δentry|/entry ≥ 10 bps
		},
		SafetyGates: &SafetyGates{
			// Gate A multiple is stage-invariant (1 at STAGE_1-4 per pre-reg).
			// Gate B scales with stake: 10× preserves the pre-reg geometry
			// ($1,000 cap at the $100 STAGE_1 stake) at any stake this executor
			// is constructed with — Layer 3 shadows run the $1,000 paper stake,
			// where a flat $1,000 cap trips on a single typical stop loss.
			// Stage activation still overrides these explicitly per the
			// pre-reg table (real_money_protocol_decision_rule_2026-05-08.md).
			MaxPositionMultiple: 1,
			DailyLossUSDCap:     10 * stakeUSD,
			MaxEntrySpreadBps:   50,
		},
		KillSwitch: &KillSwitch{},
	}
	bl.KillSwitch.Router = bl.OrderRouter
	return bl
}

// NewBinanceLiveTestnet constructs a BinanceLive pointed at the Binance USDT-M
// Futures TESTNET. Identical to NewBinanceLive in every other respect — same
// safety gates, same recovery semantics, same kill-switch wiring — but order
// + position-reconciler REST calls go to testnet.binancefuture.com instead of
// fapi.binance.com. Tick/price feeds are unaffected (those flow through the
// marketdata package and continue to use production data, which is the
// correct Layer 2 contract: real prices, fake fills).
//
// Required by the Layer 2 integration gate before any STAGE_1 mainnet flip
// (real_money_executor_architecture_decision_rule_2026-05-08.md §"Layer 2 —
// Integration against Binance TESTNET"). Testnet API credentials are SEPARATE
// from production — generate them at testnet.binancefuture.com.
func NewBinanceLiveTestnet(symbol string, stakeUSD float64, apiKey, apiSecret string) *BinanceLive {
	bl := NewBinanceLive(symbol, stakeUSD, apiKey, apiSecret)
	bl.OrderRouter.APIBaseURL = TestnetAPIBaseURL
	bl.PositionReconciler.APIBaseURL = TestnetAPIBaseURL
	return bl
}

// OnSignal is the strategy runner's entry to send an order. Per the locked
// concurrency model, OnSignal returns immediately — the actual SafetyGates
// check + SendOrder + journal write run in a spawned goroutine so the
// strategy tick loop never blocks on HTTP latency.
func (b *BinanceLive) OnSignal(sig *models.Signal) {
	b.wg.Add(1)
	go b.handleSignalSync(sig)
}

// handleSignalSync is the synchronous body of OnSignal — drift gate →
// position-already-open guard → safety gates → SendOrder → journal open →
// register local position with the reconciler. Exposed (lowercase) so unit
// tests can drive the entry path deterministically without goroutine timing.
func (b *BinanceLive) handleSignalSync(sig *models.Signal) {
	defer b.wg.Done()

	// Drift gate (locked: BLOCK new orders on a drifted symbol).
	if b.PositionReconciler != nil {
		if drifted, reason := b.PositionReconciler.IsDrifted(b.Symbol); drifted {
			slog.Warn("signal dropped: position drift",
				"symbol", b.Symbol, "reason", reason, "signal_reason", sig.Reason)
			return
		}
	}

	// Mirror the recovery-path StakeUSD guard. Without this check, qty would
	// silently compute to 0; SendOrder would forward a 0-quantity order which
	// most exchanges reject but some venues might accept as no-op. Either way,
	// the local position state would NOT update (the entry path requires a
	// successful order), so this is fail-CLOSED in practice — but explicit
	// rejection here surfaces the misconfiguration loudly via slog.Error
	// rather than getting buried under a cryptic exchange rejection.
	if b.StakeUSD <= 0 {
		slog.Error("signal rejected: BinanceLive.StakeUSD not configured (≤0)",
			"symbol", b.Symbol, "stake_usd", b.StakeUSD)
		return
	}
	stopDist := math.Abs(sig.EntryPrice - sig.StopLoss)
	if stopDist == 0 {
		slog.Error("signal rejected: zero stop distance",
			"symbol", b.Symbol, "entry", sig.EntryPrice, "stop", sig.StopLoss)
		return
	}
	qty := b.StakeUSD / stopDist

	// Quantize to the symbol's exchange filters. Fail-CLOSED when filters are
	// unavailable: sending the raw float is a guaranteed -1111 rejection
	// anyway, and a loud skip is more honest than a cryptic exchange error.
	if err := b.ensureSymbolFilters(); err != nil {
		slog.Error("signal rejected: symbol filters unavailable",
			"symbol", b.Symbol, "err", err)
		if b.Notifier != nil {
			_ = b.Notifier.SendStructured(context.Background(), notify.SeverityWarn,
				fmt.Sprintf("Entry skipped on %s: exchangeInfo filters unavailable: %v", b.Symbol, err))
		}
		return
	}
	b.mu.Lock()
	step, minQty, maxQty, decimals := b.qtyStep, b.qtyMin, b.qtyMax, b.qtyDecimals
	b.mu.Unlock()
	qty = quantizeQty(qty, step, decimals)
	if qty <= 0 || (minQty > 0 && qty < minQty) || (maxQty > 0 && qty > maxQty) {
		slog.Error("signal rejected: quantized qty outside symbol bounds",
			"symbol", b.Symbol, "qty", qty, "min_qty", minQty, "max_qty", maxQty,
			"step", step, "stake_usd", b.StakeUSD, "stop_dist", stopDist)
		if b.Notifier != nil {
			_ = b.Notifier.SendStructured(context.Background(), notify.SeverityCritical,
				fmt.Sprintf("Entry REJECTED on %s: quantized qty %v outside bounds [%v, %v] (step %v)",
					b.Symbol, qty, minQty, maxQty, step))
		}
		return
	}

	// Phase 1 (under lock): check position-already-open + snapshot last tick
	// for Gate C bid/ask substitute.
	b.mu.Lock()
	if b.position != nil {
		b.mu.Unlock()
		slog.Debug("signal rejected: position already open",
			"existing_entry", b.position.Signal.EntryPrice,
			"new_signal", sig.Reason)
		return
	}
	bid, ask := b.lastTick.Price, b.lastTick.Price
	b.mu.Unlock()

	// Gate C needs a current bid/ask. Until a real bid/ask feed is wired (a
	// later commit), we substitute the most recent tick price as both. This
	// degenerates the gate to "is the signal price within K bps of the latest
	// tick", which is the relevant staleness check anyway. If no tick has
	// arrived (cold start), fall back to the signal's own entry — the gate is
	// effectively a no-op in that boundary case.
	if bid == 0 || ask == 0 {
		bid, ask = sig.EntryPrice, sig.EntryPrice
	}

	// Phase 2 (no lock): safety gates + HTTP call.
	intent := OrderIntent{
		Symbol:   b.Symbol,
		Side:     sig.Side,
		Quantity: qty,
		Type:     "MARKET",
	}
	gateCtx := GateContext{
		Intent:           intent,
		SignalEntryPrice: sig.EntryPrice,
		SignalStopPrice:  sig.StopLoss,
		CurrentBid:       bid,
		CurrentAsk:       ask,
		StakeUSD:         b.StakeUSD,
		Recent24hLossUSD: b.recent24hLossUSD(),
	}
	if b.SafetyGates != nil {
		if err := b.SafetyGates.CheckOrder(gateCtx); err != nil {
			slog.Error("signal blocked by safety gate",
				"symbol", b.Symbol, "err", err, "signal_reason", sig.Reason)
			if b.Notifier != nil {
				_ = b.Notifier.SendStructured(context.Background(), notify.SeverityWarn,
					fmt.Sprintf("Safety gate blocked %s entry: %v", b.Symbol, err))
			}
			return
		}
	}

	if b.OrderRouter == nil {
		slog.Error("OrderRouter not configured", "symbol", b.Symbol)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := b.OrderRouter.SendOrder(ctx, intent)
	if err != nil {
		slog.Error("entry order failed",
			"symbol", b.Symbol, "err", err, "result_status", result.Status, "reject_code", result.RejectCode)
		if b.Notifier != nil {
			_ = b.Notifier.SendStructured(context.Background(), notify.SeverityCritical,
				fmt.Sprintf("Entry order REJECTED on %s\nstatus: %s\ncode: %s\nerr: %v",
					b.Symbol, result.Status, result.RejectCode, err))
		}
		return
	}

	// Use the actual exchange-reported fill price; fall back to the modeled
	// signal price only if the exchange returned 0 (rare/error path).
	actualEntry := result.AvgPrice
	if actualEntry == 0 {
		actualEntry = sig.EntryPrice
	}

	// Phase 3 (under lock): set position + journal open.
	posSignal := *sig
	posSignal.EntryPrice = actualEntry
	now := time.Now().UTC()
	openTS := sig.Timestamp
	if openTS.IsZero() {
		openTS = now
	}

	b.mu.Lock()
	b.position = &OpenPosition{
		Signal:           &posSignal,
		MaxAdverse:       actualEntry,
		LastPrice:        actualEntry,
		LastTime:         openTS,
		OriginalStopDist: stopDist,
		RemainingFrac:    1.0,
	}
	b.mu.Unlock()

	b.appendJournal(journalEntry{
		Event:  "open",
		Symbol: b.Symbol,
		TS:     openTS.Format(time.RFC3339),
		Side:   sig.Side.String(),
		Entry:  actualEntry,
		Stop:   sig.StopLoss,
		Target: sig.TakeProfit,
		Reason: sig.Reason,
	})

	if b.PositionReconciler != nil {
		b.PositionReconciler.SetLocalPosition(b.Symbol, sig.Side, qty, actualEntry, openTS)
	}

	slog.Info("position opened",
		"symbol", b.Symbol,
		"side", sig.Side,
		"entry", actualEntry,
		"stop", sig.StopLoss,
		"target", sig.TakeProfit,
		"qty", qty,
		"order_id", result.OrderID,
		"reason", sig.Reason)
}

// OnTick is called for every tick on the strategy goroutine. Per the locked
// concurrency model, OnTick MUST return quickly — exit-order HTTP work is
// shipped to a spawned goroutine when stop/target/MaxHoldHours fires. The
// exitPending flag prevents double-fires while the close goroutine is in
// flight.
func (b *BinanceLive) OnTick(tick models.Tick) {
	b.mu.Lock()
	b.lastTick = tick

	if b.position == nil || b.exitPending {
		b.mu.Unlock()
		return
	}
	pos := b.position
	sig := pos.Signal

	// Skip ticks predating the position's open timestamp. Mirrors Stub's
	// recovery guard — backfill replay can deliver pre-open ticks that
	// must not be evaluated against the current position.
	if !sig.Timestamp.IsZero() && tick.Timestamp.Before(sig.Timestamp) {
		b.mu.Unlock()
		return
	}

	pos.LastPrice = tick.Price
	pos.LastTime = tick.Timestamp

	// MaxHoldHours: force-close at current tick price.
	if b.MaxHoldHours > 0 && !sig.Timestamp.IsZero() {
		ageHours := tick.Timestamp.Sub(sig.Timestamp).Hours()
		if ageHours >= b.MaxHoldHours {
			won := false
			if sig.Side == models.Long {
				won = tick.Price > sig.EntryPrice
			} else {
				won = tick.Price < sig.EntryPrice
			}
			b.exitPending = true
			posCopy := *pos
			b.mu.Unlock()
			b.wg.Add(1)
			go b.handleExitSync(posCopy, tick.Price, tick.Timestamp, "TIME", won)
			return
		}
	}

	// Track MaxAdverse and MaxFavorableR (mirrors Stub).
	if sig.Side == models.Long && tick.Price < pos.MaxAdverse {
		pos.MaxAdverse = tick.Price
	} else if sig.Side == models.Short && tick.Price > pos.MaxAdverse {
		pos.MaxAdverse = tick.Price
	}
	if pos.OriginalStopDist > 0 {
		var favR float64
		if sig.Side == models.Long {
			favR = (tick.Price - sig.EntryPrice) / pos.OriginalStopDist
		} else {
			favR = (sig.EntryPrice - tick.Price) / pos.OriginalStopDist
		}
		if favR > pos.MaxFavorableR {
			pos.MaxFavorableR = favR
		}
	}

	stopHit := (sig.Side == models.Long && tick.Price <= sig.StopLoss) ||
		(sig.Side == models.Short && tick.Price >= sig.StopLoss)
	targetHit := (sig.Side == models.Long && tick.Price >= sig.TakeProfit) ||
		(sig.Side == models.Short && tick.Price <= sig.TakeProfit)
	if !stopHit && !targetHit {
		b.mu.Unlock()
		return
	}

	var modeledExit float64
	var outcome string
	var won bool
	if stopHit {
		modeledExit = sig.StopLoss
		outcome = "STOP"
		won = false
	} else {
		modeledExit = sig.TakeProfit
		outcome = "TARGET"
		won = true
	}
	b.exitPending = true
	posCopy := *pos
	b.mu.Unlock()

	b.wg.Add(1)
	go b.handleExitSync(posCopy, modeledExit, tick.Timestamp, outcome, won)
}

// handleExitSync issues the reduceOnly close order and records the result.
// Mirrors Stub.closePosition's PnL math precisely so the journal close-event
// schema is identical across paper and real-money runs. Differences vs Stub:
//   - exit price is the EXCHANGE-REPORTED fill (result.AvgPrice), not the
//     modeled stop/target.
//   - fee_usd is exchange-reported when result.FeeUSD > 0; falls back to FeeBps
//     × notional (× 2 for round-trip — exchange reports per-side).
//   - slip_usd is computed as |actual − modeled| × units on losers; falls back
//     to StopSlippageBps × notional when the exchange fill matches modeled
//     exactly.
//
// On exit-order failure: log + alert, reset exitPending so the next tick can
// retry. Position remains open locally; the reconciler will detect drift if
// the exchange has actually closed the position behind us.
func (b *BinanceLive) handleExitSync(pos OpenPosition, modeledExit float64, exitTS time.Time, outcome string, won bool) {
	defer b.wg.Done()

	sig := pos.Signal
	stopDist := pos.OriginalStopDist
	if stopDist == 0 {
		stopDist = math.Abs(sig.EntryPrice - sig.StopLoss)
	}
	remainingFrac := pos.RemainingFrac
	if remainingFrac == 0 {
		remainingFrac = 1.0
	}
	qty := b.StakeUSD * remainingFrac / stopDist

	intent := OrderIntent{
		Symbol:     b.Symbol,
		Side:       sig.Side,
		Quantity:   qty,
		Type:       "MARKET",
		ReduceOnly: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := b.OrderRouter.SendOrder(ctx, intent)
	if err != nil {
		slog.Error("exit order failed — position remains open locally; reconciler will detect drift if exchange diverged",
			"symbol", b.Symbol, "outcome", outcome, "err", err, "reject_code", result.RejectCode)
		if b.Notifier != nil {
			_ = b.Notifier.SendStructured(context.Background(), notify.SeverityCritical,
				fmt.Sprintf("Exit order FAILED on %s (%s)\nerr: %v\nposition remains open locally — verify exchange + reconcile",
					b.Symbol, outcome, err))
		}
		b.mu.Lock()
		b.exitPending = false
		b.mu.Unlock()
		return
	}
	actualExit := result.AvgPrice
	if actualExit == 0 {
		actualExit = modeledExit
	}

	// PnL math — mirrors Stub.closePosition.
	var pnlPts float64
	if sig.Side == models.Long {
		pnlPts = actualExit - sig.EntryPrice
	} else {
		pnlPts = sig.EntryPrice - actualExit
	}
	holdSeconds := 0.0
	if !exitTS.IsZero() && !sig.Timestamp.IsZero() {
		holdSeconds = exitTS.Sub(sig.Timestamp).Seconds()
		if holdSeconds < 0 {
			holdSeconds = 0
		}
	}

	var grossUSD, feeUSD, slipUSD, pnlUSD, notional float64
	if b.StakeUSD > 0 && stopDist > 0 {
		units := b.StakeUSD * remainingFrac / stopDist
		grossUSD = units * pnlPts
		notional = units * sig.EntryPrice
		// Fee: exchange-reported (per-side) × 2 if available; otherwise FeeBps fallback.
		switch {
		case result.FeeUSD > 0:
			feeUSD = result.FeeUSD * 2
		case b.FeeBps > 0:
			feeUSD = b.FeeBps / 10000.0 * notional
		}
		// Slip on losers only.
		if !won {
			slipPts := math.Abs(actualExit - modeledExit)
			slipUSD = units * slipPts
			if slipUSD == 0 && b.StopSlippageBps > 0 {
				slipUSD = b.StopSlippageBps / 10000.0 * notional
			}
		}
		pnlUSD = grossUSD - feeUSD - slipUSD
	}

	maeR := 0.0
	maeStopDist := math.Abs(sig.EntryPrice - sig.StopLoss)
	if maeStopDist > 0 {
		maeR = math.Abs(sig.EntryPrice-pos.MaxAdverse) / maeStopDist
	}
	stopDistPct := 0.0
	if sig.EntryPrice > 0 {
		stopDistPct = math.Abs(sig.EntryPrice-sig.StopLoss) / sig.EntryPrice * 100
	}

	res := tradeResult{
		signal:      sig,
		exitPrice:   actualExit,
		exitTime:    exitTS,
		won:         won,
		pnlPts:      pnlPts,
		pnlUSDT:     pnlUSD,
		grossUSDT:   grossUSD,
		feeUSDT:     feeUSD,
		slipUSDT:    slipUSD,
		fundingUSDT: 0,
		holdSeconds: holdSeconds,
		stopDistPct: stopDistPct,
	}
	entry := journalEntry{
		Event:    "close",
		Symbol:   b.Symbol,
		TS:       time.Now().UTC().Format(time.RFC3339),
		Side:     sig.Side.String(),
		Entry:    sig.EntryPrice,
		Exit:     actualExit,
		Stop:     sig.StopLoss,
		Target:   sig.TakeProfit,
		PnlPts:   math.Round(pnlPts*100) / 100,
		PnlUSD:   math.Round(pnlUSD*100) / 100,
		Outcome:  outcome,
		Reason:   sig.Reason,
		MFER:     math.Round(pos.MaxFavorableR*1000) / 1000,
		MAER:     math.Round(maeR*1000) / 1000,
		GrossUSD: math.Round(grossUSD*100) / 100,
		FeeUSD:   math.Round(feeUSD*100) / 100,
		SlipUSD:  math.Round(slipUSD*100) / 100,
		Notional: math.Round(notional*100) / 100,
	}

	b.mu.Lock()
	b.position = nil
	b.exitPending = false
	b.results = append(b.results, res)
	b.mu.Unlock()

	b.appendJournal(entry)
	if b.PositionReconciler != nil {
		b.PositionReconciler.ClearLocalPosition(b.Symbol)
	}

	slog.Info("position closed",
		"symbol", b.Symbol,
		"outcome", outcome,
		"side", sig.Side,
		"entry", sig.EntryPrice,
		"exit", actualExit,
		"pnl_usd", math.Round(pnlUSD*100)/100,
		"reason", sig.Reason)
}

// Wait drains all in-flight order-handling goroutines. Called by Summary
// before aggregating; tests use it to ensure deterministic state after
// OnSignal / OnTick.
func (b *BinanceLive) Wait() { b.wg.Wait() }

// Summary writes the end-of-run report. Drains any in-flight goroutines
// first so all closed trades are accounted for.
func (b *BinanceLive) Summary() {
	b.Wait()

	b.mu.Lock()
	total := len(b.results)
	var wins int
	var totalUSD float64
	for _, r := range b.results {
		totalUSD += r.pnlUSDT
		if r.won {
			wins++
		}
	}
	hasOpen := b.position != nil
	b.mu.Unlock()

	if total == 0 {
		slog.Info("BinanceLive summary: no real-money trades closed", "symbol", b.Symbol, "open_at_summary", hasOpen)
		return
	}
	winRate := float64(wins) / float64(total) * 100
	slog.Info("BinanceLive summary",
		"symbol", b.Symbol,
		"trades", total,
		"wins", wins,
		"win_rate_pct", math.Round(winRate*10)/10,
		"net_pnl_usd", math.Round(totalUSD*100)/100,
		"open_at_summary", hasOpen)
}

// recent24hLossWindow is the rolling-loss horizon for Gate B. Centralized
// here so RecoverFromJournal's journal-scan cutoff matches recent24hLossUSD's
// summation cutoff exactly — drift between the two would create a window
// where journal-recovered losses appear in `b.results` but get filtered out
// by `recent24hLossUSD`.
const recent24hLossWindow = 24 * time.Hour

// recent24hLossUSD scans the in-memory results window for closes within the
// last recent24hLossWindow and sums the negative pnl as positive loss. Used
// by SafetyGates Gate B (daily-loss circuit breaker).
//
// Cross-restart correctness depends on RecoverFromJournal having repopulated
// `b.results` with recent close events from the journal. Without that hook,
// every restart resets Gate B to $0 regardless of realized losses — which
// silently disables the cap at exactly the moment (post-crash) when running
// it most matters.
func (b *BinanceLive) recent24hLossUSD() float64 {
	cutoff := time.Now().Add(-recent24hLossWindow)
	var loss float64
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, r := range b.results {
		if r.exitTime.After(cutoff) && r.pnlUSDT < 0 {
			loss += -r.pnlUSDT
		}
	}
	return loss
}

// RecoverFromJournal scans the symbol's journal files (current + prior month)
// for an unclosed position from a prior engine session, reconstructs local
// state to match, and verifies that state against the exchange via
// PositionReconciler.FetchExchangePosition.
//
// Return semantics (caller MUST inspect both):
//
//	(false, nil) — nothing to recover (no JournalPath/Symbol; or no unclosed
//	               open in journal; or b.position already non-nil — no
//	               overwrite). Engine starts normally.
//	(true,  nil) — position recovered AND verified clean (or verification
//	               skipped due to a transient network error — logged at WARN;
//	               the standard Reconciler loop will catch any drift on its
//	               next tick). Engine starts; recovered position participates
//	               in OnTick stop/target/MaxHold evaluation.
//	(true,  err) where errors.Is(err, ErrRecoveryDrift) — position recovered
//	               locally AND exchange divergence confirmed. Per the locked
//	               rule, caller MUST NOT start tick processing. The drift is
//	               flagged on the Reconciler so any future ReconcileSymbol +
//	               IsDrifted gate observe the same divergence; an operator
//	               must investigate, reconcile (close exchange position OR
//	               adjust journal), and restart the engine.
//	(false, err) — catastrophic recovery failure (corrupt timestamp / unknown
//	               side / zero stop distance in the journal open event).
//	               Caller MUST NOT start.
//
// Designed for one-shot invocation on engine startup, AFTER NewBinanceLive
// but BEFORE the first OnTick.
func (b *BinanceLive) RecoverFromJournal(ctx context.Context) (bool, error) {
	if b.JournalPath == "" || b.Symbol == "" {
		return false, nil
	}

	b.mu.Lock()
	if b.position != nil {
		b.mu.Unlock()
		return false, nil
	}
	b.mu.Unlock()

	lastOpen, midRHit, remainingFrac, recentCloses, err := b.scanJournalForUnclosedOpen()
	if err != nil {
		return false, err
	}

	// Repopulate b.results from journal-recorded recent closes BEFORE the
	// no-open short-circuit — Gate B's daily-loss circuit breaker depends
	// on this state being present even when there's no open position to
	// recover. Without this, an engine that crashed AFTER a losing trade
	// closed but BEFORE the next entry would silently re-arm with $0 in
	// `b.results` regardless of realized losses. Real-money exposure window.
	if len(recentCloses) > 0 {
		recovered := make([]tradeResult, 0, len(recentCloses))
		for _, c := range recentCloses {
			ts, terr := time.Parse(time.RFC3339, c.TS)
			if terr != nil {
				// Already filtered to parseable TSes by scanJournalForUnclosedOpen,
				// but defensive against future schema drift. Skip individual
				// bad lines rather than failing the whole recovery.
				continue
			}
			recovered = append(recovered, tradeResult{
				exitTime: ts,
				pnlUSDT:  c.PnlUSD,
				won:      c.Outcome == "TARGET" || c.Outcome == "PARTIAL",
				// Other fields stay zero — only exitTime + pnlUSDT + won
				// are read by recent24hLossUSD and Summary's wins counter.
				// Full hydration would require parsing fee/slip/notional
				// per close; deferred unless a future caller needs it.
			})
		}
		b.mu.Lock()
		b.results = append(b.results, recovered...)
		b.mu.Unlock()
		slog.Info("recovered recent closes from journal for Gate B",
			"symbol", b.Symbol, "count", len(recovered))
	}

	if lastOpen == nil {
		return false, nil
	}

	// Parse the recovered open into typed fields.
	var side models.Direction
	switch lastOpen.Side {
	case "LONG":
		side = models.Long
	case "SHORT":
		side = models.Short
	default:
		return false, fmt.Errorf("recovery: unknown side %q in journal open", lastOpen.Side)
	}
	ts, terr := time.Parse(time.RFC3339, lastOpen.TS)
	if terr != nil {
		return false, fmt.Errorf("recovery: invalid timestamp %q in journal open: %w", lastOpen.TS, terr)
	}
	stopDist := math.Abs(lastOpen.Entry - lastOpen.Stop)
	if stopDist == 0 {
		return false, fmt.Errorf("recovery: zero stop distance (entry==stop) in journal open")
	}
	if b.StakeUSD <= 0 {
		return false, fmt.Errorf("recovery: BinanceLive.StakeUSD not set — cannot derive contracts qty")
	}
	qty := b.StakeUSD * remainingFrac / stopDist

	// Verify against the exchange. Net failure is logged but does NOT block
	// recovery — the Reconciler loop will catch any divergence on its next
	// tick. Drift IS detected here only when the query succeeds.
	driftReason := ""
	if b.PositionReconciler != nil {
		exch, ferr := b.PositionReconciler.FetchExchangePosition(ctx, b.Symbol)
		if ferr != nil {
			slog.Warn("recovery verification skipped due to exchange-query error; periodic Reconciler loop will catch any divergence",
				"symbol", b.Symbol, "err", ferr)
		} else {
			tentative := reconcilerPosition{
				side:     side,
				qty:      qty,
				avgEntry: lastOpen.Entry,
				openedAt: ts,
			}
			hasExch := math.Abs(exch.Qty) >= b.PositionReconciler.QtyTolerance
			drifted, reason := detectDrift(true, tentative, hasExch, exch,
				b.PositionReconciler.QtyTolerance, b.PositionReconciler.EntryPriceBpsLimit)
			if drifted {
				driftReason = "recovery: " + reason
				b.PositionReconciler.MarkDrift(b.Symbol, driftReason)
				slog.Error("position recovery: exchange disagrees with journal — TRADING BLOCKED",
					"symbol", b.Symbol,
					"reason", reason,
					"local_side", side, "local_qty", qty, "local_entry", lastOpen.Entry,
					"exchange_side", exch.Side, "exchange_qty", math.Abs(exch.Qty), "exchange_entry", exch.AvgEntry)
				if b.Notifier != nil {
					_ = b.Notifier.SendStructured(ctx, notify.SeverityCritical,
						fmt.Sprintf("Recovery drift on %s — TRADING BLOCKED until operator reconciles + restarts\nreason: %s\nrecovered: side=%v qty=%v entry=%v\nexchange: side=%v qty=%v entry=%v",
							b.Symbol, reason, side, qty, lastOpen.Entry,
							exch.Side, math.Abs(exch.Qty), exch.AvgEntry))
				}
			}
		}
	}

	// Commit local state. We commit even on drift so diagnostic tooling and
	// the Reconciler have a coherent view; the caller's drift-error check is
	// what enforces "BLOCK startup". The drift gate also blocks new OnSignal
	// orders, and reduceOnly OnTick exits would be rejected by the exchange
	// if state is truly diverged — defense in depth.
	sig := &models.Signal{
		Symbol:     lastOpen.Symbol,
		Side:       side,
		EntryPrice: lastOpen.Entry,
		StopLoss:   lastOpen.Stop,
		TakeProfit: lastOpen.Target,
		Timestamp:  ts,
		Reason:     lastOpen.Reason,
	}
	b.mu.Lock()
	b.position = &OpenPosition{
		Signal:           sig,
		MaxAdverse:       sig.EntryPrice,
		LastPrice:        sig.EntryPrice,
		LastTime:         sig.Timestamp,
		OriginalStopDist: stopDist,
		MidRHit:          midRHit,
		RemainingFrac:    remainingFrac,
	}
	b.mu.Unlock()

	if b.PositionReconciler != nil {
		b.PositionReconciler.SetLocalPosition(b.Symbol, side, qty, lastOpen.Entry, ts)
	}

	if driftReason != "" {
		return true, fmt.Errorf("%w: %s", ErrRecoveryDrift, driftReason)
	}

	slog.Info("position recovered + verified",
		"symbol", b.Symbol,
		"side", side,
		"entry", lastOpen.Entry,
		"stop", lastOpen.Stop,
		"target", lastOpen.Target,
		"qty", qty,
		"midRHit", midRHit,
		"remainingFrac", remainingFrac,
		"ts", lastOpen.TS,
	)
	return true, nil
}

// scanJournalForUnclosedOpen walks current + prior month's journal files
// for the symbol and returns:
//   - the most recent unclosed-open entry along with partial-close state
//     (MidRHit + RemainingFrac) — drives RecoverFromJournal's open recovery
//   - all close events within the recent24hLossWindow — drives
//     RecoverFromJournal's repopulation of b.results so Gate B (daily-loss
//     circuit breaker) survives engine restarts. Without this, an engine
//     that crashes mid-day with $X realized losses re-arms with $0 in
//     memory on restart, silently disabling the cap until 24h naturally
//     elapses. At STAGE_4 this is a real-money exposure window.
//
// Mirrors Stub.RecoverFromJournal's scanning loop exactly — same
// chronological order (prior month first, then current), same
// open/close/PARTIAL state machine, same corrupt-line tolerance. Duplicated
// rather than shared to keep the journal-schema parity contract explicit.
func (b *BinanceLive) scanJournalForUnclosedOpen() (*journalEntry, bool, float64, []journalEntry, error) {
	now := time.Now().UTC()
	currentMonth := now.Format("2006-01")
	priorMonth := now.AddDate(0, -1, 0).Format("2006-01")
	files := []string{
		filepath.Join(b.JournalPath, fmt.Sprintf("%s-%s.jsonl", b.Symbol, priorMonth)),
		filepath.Join(b.JournalPath, fmt.Sprintf("%s-%s.jsonl", b.Symbol, currentMonth)),
	}

	cutoff24h := now.Add(-recent24hLossWindow)
	var lastOpen *journalEntry
	var recentCloses []journalEntry
	midRHit := false
	remainingFrac := 1.0

	for _, fname := range files {
		f, openErr := os.Open(fname)
		if openErr != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Bytes()
			if len(line) == 0 {
				continue
			}
			var e journalEntry
			if jerr := json.Unmarshal(line, &e); jerr != nil {
				// Tolerated: a partial trailing line from an engine killed
				// mid-flush. Skip and keep scanning.
				continue
			}
			switch e.Event {
			case "open":
				eCopy := e
				lastOpen = &eCopy
				midRHit = false
				remainingFrac = 1.0
			case "close":
				// Collect for Gate B recovery if the close is within the
				// 24h loss window. Parse failure or pre-cutoff = skip.
				if ts, terr := time.Parse(time.RFC3339, e.TS); terr == nil &&
					ts.After(cutoff24h) {
					recentCloses = append(recentCloses, e)
				}
				if e.Outcome == "PARTIAL" {
					midRHit = true
					if remainingFrac > 0.5 {
						remainingFrac -= 0.5
					}
				} else {
					lastOpen = nil
					midRHit = false
					remainingFrac = 1.0
				}
			}
		}
		_ = f.Close()
	}
	return lastOpen, midRHit, remainingFrac, recentCloses, nil
}

// appendJournal writes a single JSONL line to the per-symbol monthly journal.
// Mirrors Stub.appendJournal exactly — same path scheme (<JournalPath>/<Symbol>-<YYYY-MM>.jsonl)
// and same JSON schema. Duplicated here rather than shared because the locked
// architecture rule treats journal-schema parity as a HARD invariant: any
// schema change must be a deliberate, coordinated update across both writers.
func (b *BinanceLive) appendJournal(entry journalEntry) {
	if b.JournalPath == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	month := time.Now().UTC().Format("2006-01")
	if b.journalFile == nil || month != b.journalMonth {
		if b.journalFile != nil {
			_ = b.journalFile.Close()
		}
		if err := os.MkdirAll(b.JournalPath, 0755); err != nil {
			// Elevated from slog.Warn to slog.Error: BinanceLive is the real-money
			// executor — a missed journal write means a position exists on the
			// exchange with no local record. RecoverFromJournal would not find
			// the orphan on next restart; the 60s reconciler poll would catch
			// the divergence eventually but there's a real-money window where
			// the local engine is blind. Surface loudly so post_deploy_check §5
			// (ERROR-level events) catches it.
			slog.Error("journal mkdir failed — REAL-MONEY POSITION MAY BE INVISIBLE TO LOCAL STATE",
				"symbol", b.Symbol, "path", b.JournalPath, "event", entry.Event, "err", err)
			return
		}
		name := filepath.Join(b.JournalPath, fmt.Sprintf("%s-%s.jsonl", b.Symbol, month))
		f, err := os.OpenFile(name, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			slog.Error("journal open failed — REAL-MONEY POSITION MAY BE INVISIBLE TO LOCAL STATE",
				"symbol", b.Symbol, "path", name, "event", entry.Event, "err", err)
			return
		}
		b.journalFile = f
		b.journalMonth = month
	}
	line, _ := json.Marshal(entry)
	line = append(line, '\n')
	if _, werr := b.journalFile.Write(line); werr != nil {
		// Same severity as mkdir/open failure: a half-written journal line
		// or no-line-at-all means the orphan is invisible to recovery.
		slog.Error("journal write failed — REAL-MONEY POSITION MAY BE INVISIBLE TO LOCAL STATE",
			"symbol", b.Symbol, "event", entry.Event, "err", werr)
		// CRITICAL: close + clear the handle so the NEXT appendJournal call
		// goes through the open-or-create path and gets a fresh fd. Without
		// this, every subsequent call writes to the same dead handle, fails
		// the same way, and burns the operator's attention with identical
		// error lines while every new real-money position remains invisible
		// to recovery. Audit-pattern shape: failure path must reset state
		// rather than persist into "permanently broken" silent failure.
		_ = b.journalFile.Close()
		b.journalFile = nil
		b.journalMonth = ""
		if b.Notifier != nil {
			// Spawn the alert (no Lock held in here) — alert is fire-and-forget;
			// we don't want notifier latency holding the journal mutex.
			go func(sym, ev string, e error) {
				_ = b.Notifier.SendStructured(context.Background(),
					notify.SeverityCritical,
					fmt.Sprintf("Journal write FAILED on %s (%s): %v\n"+
						"Real-money position may be invisible to recovery. "+
						"Engine will retry on next event; if errors persist, "+
						"investigate disk + permissions.",
						sym, ev, e))
			}(b.Symbol, entry.Event, werr)
		}
	}
}

// ── Component skeletons ─────────────────────────────────────────────────────

// OrderRouter sends orders to Binance USDT-M Futures. Stateless — every
// SendOrder is independent. Uses HMAC-SHA256 request signing per Binance
// Futures API spec.
type OrderRouter struct {
	APIBaseURL string
	APIKey     string
	APISecret  string
	HTTPClient *http.Client
	RecvWindow int64 // ms; default 5000 if zero
}

// OrderIntent is the input to SendOrder. Captures what the strategy wants
// without prescribing the wire format (deferred to implementation).
type OrderIntent struct {
	Symbol     string
	Side       models.Direction
	Quantity   float64 // contracts
	Type       string  // "MARKET" at STAGE_1-2; "LIMIT_IOC" considered at STAGE_3+
	LimitPrice float64 // only used when Type == "LIMIT_IOC"
	ReduceOnly bool    // true for closing orders
}

// OrderResult is the outcome of SendOrder. Always populated even on partial
// fills or rejections; the FilledQty field disambiguates.
type OrderResult struct {
	OrderID    string
	Status     string // "FILLED" | "PARTIAL" | "REJECTED" | "ERROR"
	FilledQty  float64
	AvgPrice   float64
	FeeUSD     float64 // exchange-reported actual fee
	RejectCode string  // populated when Status == "REJECTED" or "ERROR"
}

// SendOrder posts a signed order to Binance USDT-M Futures. Implements the
// MARKET-order path per the locked architecture (LIMIT_IOC deferred to STAGE_3+).
// Returns the parsed OrderResult on 2xx; F3-class REJECTED on 4xx other than
// rate limit; F4-class RATE_LIMIT on 418/429; F1-class ERROR on 5xx or network
// failure.
func (r *OrderRouter) SendOrder(ctx context.Context, intent OrderIntent) (OrderResult, error) {
	if r.APIKey == "" || r.APISecret == "" {
		return OrderResult{}, fmt.Errorf("OrderRouter not configured: APIKey/APISecret missing")
	}
	if r.HTTPClient == nil {
		return OrderResult{}, fmt.Errorf("OrderRouter not configured: HTTPClient nil")
	}

	exchangeSide, err := mapToExchangeSide(intent.Side, intent.ReduceOnly)
	if err != nil {
		return OrderResult{}, err
	}
	recvWin := r.RecvWindow
	if recvWin == 0 {
		recvWin = 5000
	}

	params := url.Values{}
	params.Set("symbol", intent.Symbol)
	params.Set("side", exchangeSide)
	params.Set("type", intent.Type)
	params.Set("quantity", strconv.FormatFloat(intent.Quantity, 'f', -1, 64))
	if intent.ReduceOnly {
		params.Set("reduceOnly", "true")
	}
	if intent.Type == "LIMIT_IOC" {
		params.Set("price", strconv.FormatFloat(intent.LimitPrice, 'f', -1, 64))
		params.Set("timeInForce", "IOC")
	}
	params.Set("newOrderRespType", "RESULT")
	params.Set("recvWindow", strconv.FormatInt(recvWin, 10))
	params.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))

	queryStr := params.Encode()
	signature := hmacSHA256(queryStr, r.APISecret)
	fullURL := r.APIBaseURL + "/fapi/v1/order?" + queryStr + "&signature=" + signature

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, nil)
	if err != nil {
		return OrderResult{}, err
	}
	req.Header.Set("X-MBX-APIKEY", r.APIKey)

	resp, err := r.HTTPClient.Do(req)
	if err != nil {
		return OrderResult{Status: "ERROR", RejectCode: "NETWORK"}, fmt.Errorf("network: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	switch {
	case resp.StatusCode == 418 || resp.StatusCode == 429:
		return OrderResult{Status: "ERROR", RejectCode: "RATE_LIMIT"}, fmt.Errorf("rate limit %d: %s", resp.StatusCode, body)
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		return OrderResult{Status: "REJECTED", RejectCode: string(body)}, fmt.Errorf("4xx %d: %s", resp.StatusCode, body)
	case resp.StatusCode >= 500:
		return OrderResult{Status: "ERROR", RejectCode: "SERVER"}, fmt.Errorf("5xx %d: %s", resp.StatusCode, body)
	}

	var br struct {
		OrderID     int64  `json:"orderId"`
		Status      string `json:"status"`
		ExecutedQty string `json:"executedQty"`
		AvgPrice    string `json:"avgPrice"`
	}
	if err := json.Unmarshal(body, &br); err != nil {
		return OrderResult{Status: "ERROR", RejectCode: "PARSE"}, fmt.Errorf("parse: %w", err)
	}
	// Capture parse errors on the numeric fields rather than the prior
	// silent-zero pattern (`qty, _ := strconv.ParseFloat(...)`). A degenerate
	// AvgPrice/ExecutedQty would default to 0, causing two downstream
	// fail-opens: (1) qty=0 would falsely classify a FILLED order as PARTIAL;
	// (2) avg=0 would trigger handleSignalSync's fallback to the modeled
	// signal price, divorcing the local position from the actual fill — a
	// divergence the reconciler would catch eventually but the immediate
	// state would be wrong. Better to surface as ERROR and let the caller
	// decide (current callers alert + skip).
	qty, qerr := strconv.ParseFloat(br.ExecutedQty, 64)
	if qerr != nil {
		return OrderResult{Status: "ERROR", RejectCode: "PARSE"},
			fmt.Errorf("parse executedQty %q: %w", br.ExecutedQty, qerr)
	}
	avg, aerr := strconv.ParseFloat(br.AvgPrice, 64)
	if aerr != nil {
		return OrderResult{Status: "ERROR", RejectCode: "PARSE"},
			fmt.Errorf("parse avgPrice %q: %w", br.AvgPrice, aerr)
	}
	status := "FILLED"
	if qty < intent.Quantity {
		status = "PARTIAL"
	}
	return OrderResult{
		OrderID:   strconv.FormatInt(br.OrderID, 10),
		Status:    status,
		FilledQty: qty,
		AvgPrice:  avg,
	}, nil
}

// hmacSHA256 computes Binance's signed-request signature: HMAC-SHA256 of the
// query string with the API secret as key, hex-encoded.
func hmacSHA256(message, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(message))
	return hex.EncodeToString(h.Sum(nil))
}

// mapToExchangeSide translates our (position-direction + reduceOnly) intent
// into the exchange's (BUY/SELL) order side. Open: Long→BUY, Short→SELL.
// Close (reduceOnly): flipped — Long position closing → SELL, Short → BUY.
func mapToExchangeSide(side models.Direction, reduceOnly bool) (string, error) {
	switch side {
	case models.Long:
		if reduceOnly {
			return "SELL", nil
		}
		return "BUY", nil
	case models.Short:
		if reduceOnly {
			return "BUY", nil
		}
		return "SELL", nil
	default:
		return "", fmt.Errorf("invalid side: %v (must be Long or Short)", side)
	}
}

// PositionReconciler runs a background loop that periodically queries the
// exchange for the current position state on each deployed symbol and
// compares it to the local view. On mismatch, raises an alert and BLOCKS
// new orders on the affected symbol until an operator clears the drift
// state via ClearDrift (after manual root-cause investigation per the
// locked per_symbol_pause_decision_rule).
//
// Drift detection (locked thresholds):
//   - |position-size mismatch| ≥ QtyTolerance (default 0.01 contracts)
//   - OR side mismatch (one side LONG, the other SHORT or none)
//   - OR |entry-price drift| ≥ EntryPriceBpsLimit (default 10 bps)
//
// Reconciliation does NOT auto-correct; it ALERTS. The operator decides
// whether to fix local-state-to-match-exchange or
// fix-exchange-state-to-match-local based on root-cause investigation.
type PositionReconciler struct {
	PollInterval time.Duration

	// API config — same shape as OrderRouter; populated by NewBinanceLive.
	APIBaseURL string
	APIKey     string
	APISecret  string
	HTTPClient *http.Client
	RecvWindow int64 // ms; default 5000 if zero

	// Drift detection thresholds. NewBinanceLive sets locked defaults
	// (0.01 contracts, 10 bps); production may tune via configuration but
	// re-locking is an explicit decision.
	QtyTolerance       float64
	EntryPriceBpsLimit float64

	// RateLimitBackoff is the cooldown the Run loop waits after a 418/429
	// from the exchange before polling again. It MUST exceed PollInterval —
	// otherwise polling continues at tick cadence during an IP ban, which
	// Binance extends on every request-while-banned, holding the ban open
	// indefinitely (see the 2026-05-29 Layer 3 testnet reconciler incident).
	// Zero → defaultRateLimitBackoff. Mirrors the aggTrade poller's 60s
	// backoff in pkg/marketdata/binance.go.
	RateLimitBackoff time.Duration

	Notifier *notify.Notifier

	// Local position view + drift map per symbol. Populated by
	// SetLocalPosition / ClearLocalPosition (the BinanceLive-side write
	// path) and by ReconcileSymbol (the drift-set/clear path).
	mu        sync.Mutex
	positions map[string]reconcilerPosition
	drifted   map[string]string // symbol → drift reason; absent or "" = clean
}

type reconcilerPosition struct {
	side     models.Direction
	qty      float64
	avgEntry float64
	openedAt time.Time
}

// ExchangePosition is the parsed result of a /fapi/v2/positionRisk query.
// Qty is signed: positive=Long, negative=Short, zero=flat. Side is derived
// from the sign for callers that prefer the enum.
type ExchangePosition struct {
	Qty      float64
	AvgEntry float64
	Side     models.Direction
}

// DriftReport is the per-symbol output of a single ReconcileSymbol pass.
type DriftReport struct {
	Symbol           string
	Drifted          bool
	Reason           string // empty when Drifted=false
	HasLocal         bool
	LocalSide        models.Direction
	LocalQty         float64
	LocalAvgEntry    float64
	HasExchange      bool
	ExchangeSide     models.Direction
	ExchangeQty      float64 // absolute (always non-negative)
	ExchangeAvgEntry float64
}

// SetLocalPosition records BinanceLive's local view of a position. Called
// by OnSignal after an open is journaled, and by close-event handling.
func (r *PositionReconciler) SetLocalPosition(symbol string, side models.Direction, qty, avgEntry float64, openedAt time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.positions == nil {
		r.positions = make(map[string]reconcilerPosition)
	}
	r.positions[symbol] = reconcilerPosition{
		side:     side,
		qty:      qty,
		avgEntry: avgEntry,
		openedAt: openedAt,
	}
}

// ClearLocalPosition removes the local record. Called after a close is
// journaled (the position is gone — the next reconcile should see flat-on-both
// = clean).
func (r *PositionReconciler) ClearLocalPosition(symbol string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.positions, symbol)
}

// LocalPosition returns the locally-tracked position; ok=false when nothing
// is recorded for the symbol.
func (r *PositionReconciler) LocalPosition(symbol string) (side models.Direction, qty, avgEntry float64, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	pos, found := r.positions[symbol]
	if !found {
		return models.Neutral, 0, 0, false
	}
	return pos.side, pos.qty, pos.avgEntry, true
}

// IsDrifted reports whether the symbol is currently in drift. BinanceLive.OnSignal
// MUST gate on this BEFORE SafetyGates — sending orders on a drifted symbol
// risks compounding the discrepancy.
func (r *PositionReconciler) IsDrifted(symbol string) (bool, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	reason, ok := r.drifted[symbol]
	if !ok || reason == "" {
		return false, ""
	}
	return true, reason
}

// ClearDrift unsets the drift flag. Operator-invoked after manual
// reconciliation (e.g., closing the orphan position via Binance UI, or
// updating local journal state). NOT auto-called — drift is sticky until
// human intervention OR until a subsequent reconcile naturally finds the
// state has converged.
func (r *PositionReconciler) ClearDrift(symbol string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.drifted, symbol)
}

// MarkDrift sets the drift flag for a symbol without going through the
// full ReconcileSymbol pass. Used by BinanceLive.RecoverFromJournal when
// the recovery-time exchange query already proved divergence — calling
// ReconcileSymbol again would just repeat the same comparison.
//
// The reason should be prefixed (e.g., "recovery: ...") so the operator
// log distinguishes recovery-time drift from steady-state drift.
func (r *PositionReconciler) MarkDrift(symbol, reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.drifted == nil {
		r.drifted = make(map[string]string)
	}
	r.drifted[symbol] = reason
}

// FetchExchangePosition queries Binance USDT-M Futures /fapi/v2/positionRisk
// for the symbol's current position. Returns ExchangePosition with signed Qty
// (positive=Long, negative=Short). On hedge-mode accounts the response can
// contain LONG and SHORT entries separately; we sum them so net exposure is
// reported coherently regardless of position-mode.
func (r *PositionReconciler) FetchExchangePosition(ctx context.Context, symbol string) (ExchangePosition, error) {
	if r.APIKey == "" || r.APISecret == "" {
		return ExchangePosition{}, fmt.Errorf("PositionReconciler not configured: APIKey/APISecret missing")
	}
	if r.HTTPClient == nil {
		return ExchangePosition{}, fmt.Errorf("PositionReconciler not configured: HTTPClient nil")
	}
	recvWin := r.RecvWindow
	if recvWin == 0 {
		recvWin = 5000
	}

	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("recvWindow", strconv.FormatInt(recvWin, 10))
	params.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))
	queryStr := params.Encode()
	signature := hmacSHA256(queryStr, r.APISecret)
	fullURL := r.APIBaseURL + "/fapi/v2/positionRisk?" + queryStr + "&signature=" + signature

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return ExchangePosition{}, err
	}
	req.Header.Set("X-MBX-APIKEY", r.APIKey)

	resp, err := r.HTTPClient.Do(req)
	if err != nil {
		return ExchangePosition{}, fmt.Errorf("network: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return ExchangePosition{}, fmt.Errorf("positionRisk %d: %s", resp.StatusCode, body)
	}

	var entries []struct {
		Symbol       string `json:"symbol"`
		PositionAmt  string `json:"positionAmt"`
		EntryPrice   string `json:"entryPrice"`
		PositionSide string `json:"positionSide"`
	}
	if err := json.Unmarshal(body, &entries); err != nil {
		return ExchangePosition{}, fmt.Errorf("parse positionRisk: %w", err)
	}

	// Sum signed positionAmt across entries (one in one-way mode; up to two
	// in hedge mode). Average entry is notional-weighted.
	var netQty, weightedNotional float64
	for _, e := range entries {
		amt, _ := strconv.ParseFloat(e.PositionAmt, 64)
		entry, _ := strconv.ParseFloat(e.EntryPrice, 64)
		netQty += amt
		weightedNotional += math.Abs(amt) * entry
	}

	pos := ExchangePosition{Qty: netQty}
	if math.Abs(netQty) > 0 {
		pos.AvgEntry = weightedNotional / math.Abs(netQty)
	}
	switch {
	case netQty > 0:
		pos.Side = models.Long
	case netQty < 0:
		pos.Side = models.Short
	default:
		pos.Side = models.Neutral
	}
	return pos, nil
}

// ReconcileSymbol runs one reconciliation pass: fetch exchange state, compare
// to local, set/clear drift, alert on the leading edge of a drift episode.
//
// "Leading edge" = drift just transitioned from clean → drifted. Repeated
// drift on consecutive passes does NOT re-alert (locked: prevent operator
// alert-fatigue from a single unresolved drift).
func (r *PositionReconciler) ReconcileSymbol(ctx context.Context, symbol string) (DriftReport, error) {
	exch, err := r.FetchExchangePosition(ctx, symbol)
	if err != nil {
		return DriftReport{Symbol: symbol}, err
	}

	r.mu.Lock()
	local, hasLocal := r.positions[symbol]
	wasDrifted := r.drifted[symbol] != ""
	r.mu.Unlock()

	report := DriftReport{
		Symbol:      symbol,
		HasLocal:    hasLocal,
		HasExchange: math.Abs(exch.Qty) >= r.QtyTolerance,
	}
	if hasLocal {
		report.LocalSide = local.side
		report.LocalQty = local.qty
		report.LocalAvgEntry = local.avgEntry
	}
	if report.HasExchange {
		report.ExchangeSide = exch.Side
		report.ExchangeQty = math.Abs(exch.Qty)
		report.ExchangeAvgEntry = exch.AvgEntry
	}

	report.Drifted, report.Reason = detectDrift(hasLocal, local, report.HasExchange, exch, r.QtyTolerance, r.EntryPriceBpsLimit)

	r.mu.Lock()
	if report.Drifted {
		if r.drifted == nil {
			r.drifted = make(map[string]string)
		}
		r.drifted[symbol] = report.Reason
	} else if wasDrifted {
		delete(r.drifted, symbol)
	}
	r.mu.Unlock()

	switch {
	case report.Drifted && !wasDrifted:
		slog.Error("position drift detected",
			"symbol", symbol,
			"reason", report.Reason,
			"local_side", report.LocalSide, "local_qty", report.LocalQty, "local_entry", report.LocalAvgEntry,
			"exchange_side", report.ExchangeSide, "exchange_qty", report.ExchangeQty, "exchange_entry", report.ExchangeAvgEntry)
		if r.Notifier != nil {
			_ = r.Notifier.SendStructured(ctx, notify.SeverityCritical,
				fmt.Sprintf("Position drift on %s — engine BLOCKED for new orders\nreason: %s\nlocal: side=%v qty=%v entry=%v\nexchange: side=%v qty=%v entry=%v\nOperator must reconcile + ClearDrift",
					symbol, report.Reason,
					report.LocalSide, report.LocalQty, report.LocalAvgEntry,
					report.ExchangeSide, report.ExchangeQty, report.ExchangeAvgEntry))
		}
	case !report.Drifted && wasDrifted:
		slog.Info("position drift cleared", "symbol", symbol)
		if r.Notifier != nil {
			_ = r.Notifier.SendStructured(ctx, notify.SeverityInfo,
				fmt.Sprintf("Position drift cleared on %s — local + exchange now agree", symbol))
		}
	}

	return report, nil
}

// detectDrift is the pure-arithmetic decision: given local + exchange state
// and the locked thresholds, produce drifted-bool + human-readable reason.
// Extracted so the rule is testable without HTTP plumbing.
func detectDrift(hasLocal bool, local reconcilerPosition, hasExch bool, exch ExchangePosition, qtyTol, entryBpsLimit float64) (bool, string) {
	switch {
	case !hasLocal && !hasExch:
		return false, ""
	case !hasLocal && hasExch:
		return true, fmt.Sprintf("orphan exchange position: side=%v qty=%v entry=%v (local empty)",
			exch.Side, math.Abs(exch.Qty), exch.AvgEntry)
	case hasLocal && !hasExch:
		return true, fmt.Sprintf("externally-closed: local side=%v qty=%v entry=%v (exchange flat)",
			local.side, local.qty, local.avgEntry)
	}

	if local.side != exch.Side {
		return true, fmt.Sprintf("side mismatch: local=%v exchange=%v", local.side, exch.Side)
	}

	absExchQty := math.Abs(exch.Qty)
	qtyDelta := math.Abs(local.qty - absExchQty)
	if qtyDelta >= qtyTol {
		return true, fmt.Sprintf("qty drift: local=%v exchange=%v (Δ=%v ≥ tol=%v)",
			local.qty, absExchQty, qtyDelta, qtyTol)
	}

	if local.avgEntry > 0 {
		bpsDiff := math.Abs(local.avgEntry-exch.AvgEntry) / local.avgEntry * 10000
		if bpsDiff >= entryBpsLimit {
			return true, fmt.Sprintf("entry-price drift: local=%v exchange=%v (%.2f bps ≥ %.2f bps)",
				local.avgEntry, exch.AvgEntry, bpsDiff, entryBpsLimit)
		}
	}

	return false, ""
}

// defaultRateLimitBackoff is the cooldown after a 418/429 before the
// reconciler polls again. 120s deliberately exceeds both the poll interval
// and a typical short Binance IP ban so the ban can expire instead of being
// re-extended by a poll-while-banned. (The aggTrade poller uses 60s; the
// reconciler uses longer because its calls are rarer and the cost of a missed
// reconcile cycle is low — staleness alerts, not data loss.)
const defaultRateLimitBackoff = 120 * time.Second

// isRateLimitErr reports whether err is a Binance 418 (IP banned) or 429
// (rate limit exceeded) response from the positionRisk endpoint. Matches the
// error format produced by FetchExchangePosition ("positionRisk <code>: ...").
// Used by the Run loop to switch from poll-interval cadence to a longer
// rate-limit cooldown — continuing to poll during a 418 ban extends it.
func isRateLimitErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "positionRisk 418") || strings.Contains(s, "positionRisk 429")
}

// effectiveRateLimitBackoff returns RateLimitBackoff or the default when unset.
func (r *PositionReconciler) effectiveRateLimitBackoff() time.Duration {
	if r.RateLimitBackoff <= 0 {
		return defaultRateLimitBackoff
	}
	return r.RateLimitBackoff
}

// effectivePollInterval clamps PollInterval to the locked [30s, 300s] band
// and substitutes the 60s default when unset. Exposed (lowercase) to permit
// direct unit tests of the clamp without spinning the loop.
func (r *PositionReconciler) effectivePollInterval() time.Duration {
	const (
		floor   = 30 * time.Second
		ceiling = 5 * time.Minute
		def     = 60 * time.Second
	)
	switch {
	case r.PollInterval == 0:
		return def
	case r.PollInterval < floor:
		return floor
	case r.PollInterval > ceiling:
		return ceiling
	default:
		return r.PollInterval
	}
}

// Run starts the periodic reconciliation loop for a single symbol. Errors
// loudly at entry if API config is missing — this is a real-money component;
// silent skeleton-mode would be dangerous. Otherwise it issues an initial
// reconcile (so operator visibility on engine restart doesn't lag the tick
// interval) then enters a ticker loop until ctx is canceled.
func (r *PositionReconciler) Run(ctx context.Context, symbol string) error {
	if r.APIKey == "" || r.APISecret == "" {
		return fmt.Errorf("PositionReconciler not configured: APIKey/APISecret missing")
	}
	if r.HTTPClient == nil {
		return fmt.Errorf("PositionReconciler not configured: HTTPClient nil")
	}

	// Initial reconcile — surface pre-existing exchange-side state immediately
	// (caller of Run is typically the engine startup goroutine).
	if _, err := r.ReconcileSymbol(ctx, symbol); err != nil {
		slog.Warn("reconcile error (initial)", "symbol", symbol, "err", err)
		if isRateLimitErr(err) {
			if !r.sleepBackoff(ctx, symbol) {
				return ctx.Err()
			}
		}
	}

	ticker := time.NewTicker(r.effectivePollInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if _, err := r.ReconcileSymbol(ctx, symbol); err != nil {
				slog.Warn("reconcile error", "symbol", symbol, "err", err)
				// On a 418/429, STOP polling for a cooldown longer than the
				// tick interval. Continuing to poll at tick cadence during an
				// IP ban extends the ban (Binance pushes "banned until" forward
				// on every request-while-banned) — see the 2026-05-29 Layer 3
				// testnet incident. Mirrors pkg/marketdata/binance.go's
				// aggTrade backoff.
				if isRateLimitErr(err) {
					if !r.sleepBackoff(ctx, symbol) {
						return ctx.Err()
					}
				}
			}
		}
	}
}

// sleepBackoff waits effectiveRateLimitBackoff() or until ctx is canceled.
// Returns true if the backoff elapsed normally, false if ctx was canceled
// (caller should return). Logged at Warn so the operator sees the reconciler
// has paused for a rate-limit cooldown rather than silently going quiet.
func (r *PositionReconciler) sleepBackoff(ctx context.Context, symbol string) bool {
	d := r.effectiveRateLimitBackoff()
	slog.Warn("reconcile rate-limited, backing off",
		"symbol", symbol, "backoff", d)
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// SafetyGates enforces pre-order checks per the locked design. Each gate
// blocks an order before it reaches the exchange.
//
// Gate A: per-trade $-risk (qty × |entry − stop|) must be ≤ MaxPositionMultiple
//
//	× stake. Risk — not notional — is the basis: qty is sized as
//	stake/stop-distance, so notional runs 20-50× stake by construction
//	and a notional cap at 1× stake blocks every realistic order (the
//	2026-06-10 KAVAUSDT Layer 3 block). Position CONCURRENCY (one open
//	position per symbol) is enforced upstream by handleSignalSync's
//	position-already-open guard; Gate A bounds the dollar risk a single
//	order can carry, catching sizing-math blowups before the exchange.
//
// Gate B: rolling 24h realized losses must be ≤ DailyLossUSDCap.
// Gate C: signal entry price must be within MaxEntrySpreadBps of current
//
//	best-bid/best-ask at order time.
type SafetyGates struct {
	MaxPositionMultiple float64 // Gate A: 1 at STAGE_1-4 per pre-reg
	DailyLossUSDCap     float64 // Gate B: 10× stake (= $1000 at STAGE_1's $100 stake)
	MaxEntrySpreadBps   float64 // Gate C: 50 bps default
}

// GateContext bundles every input the three gates need. Pulled into a struct
// because Gate A needs the signal's stop price + per-trade stake (not just
// the order intent) and Gate C needs the strategy's intended entry price
// separately from any limit price on the intent (MARKET orders have no price
// field but still need entry-price-vs-bid/ask sanity).
type GateContext struct {
	Intent           OrderIntent
	SignalEntryPrice float64 // strategy's intended entry (signal.EntryPrice)
	SignalStopPrice  float64 // strategy's stop (signal.StopLoss; used for Gate A risk)
	CurrentBid       float64 // best-bid at order time
	CurrentAsk       float64 // best-ask at order time
	StakeUSD         float64 // per-trade stake (used for Gate A cap)
	Recent24hLossUSD float64 // rolling 24h realized losses (used for Gate B)
}

// CheckOrder runs Gate A → Gate B → Gate C in order, short-circuiting on the
// first failure. Returns nil only if all three pass. Order is locked: Gate A
// and B are cheap pure-arithmetic checks; Gate C requires fresh bid/ask which
// is the most expensive input.
func (g *SafetyGates) CheckOrder(c GateContext) error {
	// Gate A: per-trade $-risk (qty × |entry − stop|) must be ≤
	// MaxPositionMultiple × stake. A zero/unset stop would degenerate risk to
	// notional-scale or zero — guard it fail-closed so a missing stop can
	// never waive the gate.
	if c.SignalStopPrice <= 0 {
		return fmt.Errorf("Gate A failed: missing/invalid stop price (stop=%.8f)", c.SignalStopPrice)
	}
	stopDistance := math.Abs(c.SignalEntryPrice - c.SignalStopPrice)
	intentRisk := c.Intent.Quantity * stopDistance
	maxAllowed := g.MaxPositionMultiple * c.StakeUSD
	if intentRisk > maxAllowed {
		return fmt.Errorf("Gate A failed: max-risk cap $%.2f exceeded (intent risk=$%.2f = qty %.6f × stop-distance %.8f)",
			maxAllowed, intentRisk, c.Intent.Quantity, stopDistance)
	}

	// Gate B: rolling 24h realized losses must be ≤ DailyLossUSDCap.
	// Note: losses are POSITIVE numbers in this convention (loss=$1500 means
	// $1500 was lost). At-cap is allowed; strictly > cap fails.
	if c.Recent24hLossUSD > g.DailyLossUSDCap {
		return fmt.Errorf("Gate B failed: daily-loss circuit breaker tripped (loss=$%.2f > cap=$%.2f)",
			c.Recent24hLossUSD, g.DailyLossUSDCap)
	}

	// Gate C: signal entry price must be within MaxEntrySpreadBps of the
	// current bid-ask mid. Catches stale signals (signal fired but engine
	// took >5s to reach the order, mid has moved).
	if c.CurrentBid <= 0 || c.CurrentAsk <= 0 || c.CurrentBid > c.CurrentAsk {
		return fmt.Errorf("Gate C failed: invalid bid/ask (bid=%.4f ask=%.4f)",
			c.CurrentBid, c.CurrentAsk)
	}
	mid := (c.CurrentBid + c.CurrentAsk) / 2
	if mid <= 0 {
		return fmt.Errorf("Gate C failed: non-positive mid (%.4f)", mid)
	}
	deviationBps := math.Abs(c.SignalEntryPrice-mid) / mid * 10000
	if deviationBps > g.MaxEntrySpreadBps {
		return fmt.Errorf("Gate C failed: signal price $%.4f differs from mid $%.4f by %.1f bps > cap %.1f bps",
			c.SignalEntryPrice, mid, deviationBps, g.MaxEntrySpreadBps)
	}

	return nil
}

// KillSwitch is the immediate market-close-all entry point. Used by:
//   - results/auto_kill_execution_decision_rule_2026-05-08.md Phase 2 Path C
//   - TRIAGE-C investigation when escalating to active kill
//   - operator emergency: scripts/kill_switch.sh CONFIRM
//
// Bypasses SafetyGates by design — KillSwitch fires under conditions where
// gates would interfere (e.g., post-Gate-B trip the kill is exactly what
// should happen).
type KillSwitch struct {
	Router *OrderRouter

	// RateLimitBackoff is the per-position retry delay when SendOrder
	// returns RejectCode=RATE_LIMIT (HTTP 418/429). Without retry, a 16-
	// position kill batch that hits the rate limit on position 5 would
	// cascade to all remaining 12 positions failing as the rate-limit
	// window persists — a partial close in a panic kill is worst-case
	// dangerous. With one retry per position, each gets up to 60s of
	// natural rate-limit window recovery before recording FAILED.
	//
	// Zero falls back to defaultKillRateLimitBackoff (60s). Tests
	// override to a sub-second value to keep fast.
	RateLimitBackoff time.Duration
}

// defaultKillRateLimitBackoff is the production-default per-position retry
// delay on RATE_LIMIT. 60s aligns with Binance USDT-M Futures' 1-minute
// rate-limit window — long enough that the second attempt has a fresh
// budget, short enough that a 16-symbol kill completes in worst-case 16m
// rather than failing 12 positions instantly.
const defaultKillRateLimitBackoff = 60 * time.Second

// ClosePosition describes a position that KillAll needs to close. Decoupled
// from PositionReconciler's internal type so KillAll is testable without a
// running reconciler — caller passes whatever positions it knows about.
type ClosePosition struct {
	Symbol   string
	Side     models.Direction // current position direction (Long/Short)
	Quantity float64          // contracts to close
	AvgEntry float64          // avg entry price (informational; included in result)
}

// KillOutcome is the per-symbol result of a KillAll attempt.
type KillOutcome struct {
	Symbol    string
	Status    string  // "CLOSED" | "PARTIAL" | "FAILED"
	Requested float64 // intended close quantity
	Filled    float64 // actual filled quantity
	AvgPrice  float64 // exchange-reported avg fill price
	Error     string  // populated when Status == "FAILED"
}

// KillResult aggregates per-symbol outcomes across the whole kill batch.
type KillResult struct {
	Reason   string
	Outcomes []KillOutcome
}

// KillAll synchronously closes every position in the input list via market
// orders. Best-effort: a single failed close does NOT stop iteration; the
// remaining positions still attempt close. Returns the aggregated KillResult
// always; the error return is non-nil when ANY position failed (caller can
// inspect the result for per-symbol detail).
//
// Idempotent — re-running with the same positions is safe (closing an
// already-closed position returns 4xx from Binance which the result records
// as FAILED with the exchange's reject code; caller can filter those out).
//
// Bypasses SafetyGates by design.
func (k *KillSwitch) KillAll(ctx context.Context, positions []ClosePosition, reason string) (KillResult, error) {
	result := KillResult{Reason: reason}
	if k.Router == nil {
		return result, fmt.Errorf("KillSwitch not configured: Router nil")
	}

	backoff := k.RateLimitBackoff
	if backoff == 0 {
		backoff = defaultKillRateLimitBackoff
	}

	failureCount := 0
	for _, pos := range positions {
		intent := OrderIntent{
			Symbol:     pos.Symbol,
			Side:       pos.Side,
			Quantity:   pos.Quantity,
			Type:       "MARKET",
			ReduceOnly: true, // CLOSE — mapToExchangeSide flips the direction
		}
		orderResult, err := k.Router.SendOrder(ctx, intent)
		// Single retry on RATE_LIMIT. Without this, a 16-symbol kill batch
		// that hits 418/429 on position 5 cascades to all 12 remaining
		// positions failing as the rate-limit window persists — partial
		// close in a panic kill is worst-case dangerous. The retry adds
		// up to backoff per affected position; the operator's idempotent
		// re-run path is preserved as a backstop for true outages.
		if err != nil && orderResult.RejectCode == "RATE_LIMIT" {
			slog.Warn("kill-switch rate-limited, sleeping before retry",
				"symbol", pos.Symbol, "backoff", backoff)
			select {
			case <-ctx.Done():
				// Operator canceled (e.g., timeout on the kill_switch CLI).
				// Record the in-progress backoff state so the operator sees
				// what happened rather than treating it as a generic FAILED.
				result.Outcomes = append(result.Outcomes, KillOutcome{
					Symbol:    pos.Symbol,
					Status:    "FAILED",
					Requested: pos.Quantity,
					Error:     fmt.Sprintf("rate-limited; ctx canceled during %v backoff", backoff),
				})
				failureCount++
				continue
			case <-time.After(backoff):
			}
			orderResult, err = k.Router.SendOrder(ctx, intent)
		}
		outcome := KillOutcome{
			Symbol:    pos.Symbol,
			Requested: pos.Quantity,
			Filled:    orderResult.FilledQty,
			AvgPrice:  orderResult.AvgPrice,
		}
		switch {
		case err != nil:
			outcome.Status = "FAILED"
			outcome.Error = err.Error()
			failureCount++
		case orderResult.FilledQty < pos.Quantity:
			outcome.Status = "PARTIAL"
		default:
			outcome.Status = "CLOSED"
		}
		result.Outcomes = append(result.Outcomes, outcome)
	}

	if failureCount > 0 {
		return result, fmt.Errorf("KillAll: %d of %d positions failed (reason=%q)",
			failureCount, len(positions), reason)
	}
	return result, nil
}

// ── Symbol quantity filters (lazy exchangeInfo fetch + quantization) ─────────

// ensureSymbolFilters fetches LOT_SIZE + MARKET_LOT_SIZE for b.Symbol from
// /fapi/v1/exchangeInfo once and caches the result (mu-guarded). Entry orders
// are MARKET, so both filters apply: the effective step/minQty is the coarser
// of the two, the effective maxQty the tighter. Concurrent callers may
// double-fetch harmlessly; a failed fetch is retried on the next signal.
func (b *BinanceLive) ensureSymbolFilters() error {
	b.mu.Lock()
	if b.filtersOK {
		b.mu.Unlock()
		return nil
	}
	b.mu.Unlock()

	if b.OrderRouter == nil || b.OrderRouter.HTTPClient == nil {
		return fmt.Errorf("OrderRouter not configured for exchangeInfo fetch")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		b.OrderRouter.APIBaseURL+"/fapi/v1/exchangeInfo?symbol="+url.QueryEscape(b.Symbol), nil)
	if err != nil {
		return err
	}
	resp, err := b.OrderRouter.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("exchangeInfo: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("exchangeInfo %d: %s", resp.StatusCode, body)
	}

	var info struct {
		Symbols []struct {
			Symbol  string `json:"symbol"`
			Filters []struct {
				FilterType string `json:"filterType"`
				StepSize   string `json:"stepSize"`
				MinQty     string `json:"minQty"`
				MaxQty     string `json:"maxQty"`
			} `json:"filters"`
		} `json:"symbols"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return fmt.Errorf("exchangeInfo parse: %w", err)
	}

	var step, minQty, maxQty float64
	decimals := 0
	found := false
	for _, s := range info.Symbols {
		if s.Symbol != b.Symbol {
			continue
		}
		for _, f := range s.Filters {
			if f.FilterType != "LOT_SIZE" && f.FilterType != "MARKET_LOT_SIZE" {
				continue
			}
			fs, _ := strconv.ParseFloat(f.StepSize, 64)
			fmin, _ := strconv.ParseFloat(f.MinQty, 64)
			fmax, _ := strconv.ParseFloat(f.MaxQty, 64)
			if fs <= 0 {
				continue
			}
			found = true
			if fs > step {
				step = fs
				decimals = stepDecimals(f.StepSize)
			}
			if fmin > minQty {
				minQty = fmin
			}
			if fmax > 0 && (maxQty == 0 || fmax < maxQty) {
				maxQty = fmax
			}
		}
	}
	if !found {
		return fmt.Errorf("exchangeInfo: no LOT_SIZE filter for %s in response", b.Symbol)
	}

	b.mu.Lock()
	b.qtyStep, b.qtyMin, b.qtyMax, b.qtyDecimals = step, minQty, maxQty, decimals
	b.filtersOK = true
	b.mu.Unlock()
	slog.Info("symbol filters loaded", "symbol", b.Symbol,
		"qty_step", step, "min_qty", minQty, "max_qty", maxQty)
	return nil
}

// stepDecimals returns the number of significant decimal places in a Binance
// stepSize string ("0.00100000" → 3, "1" → 0).
func stepDecimals(step string) int {
	i := strings.IndexByte(step, '.')
	if i < 0 {
		return 0
	}
	frac := strings.TrimRight(step[i+1:], "0")
	return len(frac)
}

// quantizeQty floors qty to an exact multiple of step, re-parsed through a
// fixed-decimal string so the result serializes cleanly (3333*0.001 is
// 3.3330000000000002 in float64; the exchange wants "3.333").
func quantizeQty(qty, step float64, decimals int) float64 {
	if step <= 0 {
		return qty
	}
	q := math.Floor(qty/step+1e-9) * step
	f, err := strconv.ParseFloat(strconv.FormatFloat(q, 'f', decimals, 64), 64)
	if err != nil {
		return q
	}
	return f
}
