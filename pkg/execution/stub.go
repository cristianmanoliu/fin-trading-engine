package execution

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/cristianmanoliu/trading-engine/pkg/funding"
	"github.com/cristianmanoliu/trading-engine/pkg/models"
)

// OpenPosition tracks an active paper trade.
type OpenPosition struct {
	Signal     *models.Signal
	MaxAdverse float64   // worst price seen against the trade (for MAE tracking)
	LastPrice  float64   // most recent tick price
	LastTime   time.Time // most recent tick timestamp (used for force-close at end of data)

	// OriginalStopDist: |entry - signalStopLoss| at OnSignal time. Used by closePosition
	// for sizing (units = StakeUSDT / OriginalStopDist). Cached because B1 (trailing
	// stop) and B2 (BE-stop after partial) mutate Signal.StopLoss after entry, which
	// would otherwise produce stopDist=0 (divide-by-zero) at exit.
	OriginalStopDist float64

	// MaxFavorableR: peak favorable price excursion in R-multiples since entry.
	// Used by B1 trailing stop. Updated each tick. Initial value 0.
	MaxFavorableR float64

	// MidRHit: set true after the B2 mid-R partial close has fired. Prevents repeat firing.
	MidRHit bool

	// RemainingFrac: fraction of original position size still open. Starts at 1.0.
	// After a B2 partial close, drops to 1 - MidFrac. closePosition uses this to
	// scale units = StakeUSDT × RemainingFrac / OriginalStopDist.
	RemainingFrac float64
}

// Stub is a paper-trading execution engine.
// It receives signals, opens paper positions, and closes them when stop or target is hit.
// Not goroutine-safe — driven exclusively by the StrategyRunner goroutine via OnTick/OnSignal.
type Stub struct {
	position  *OpenPosition
	results   []tradeResult
	StakeUSDT float64 // fixed dollar amount risked per trade; 0 = points-only mode

	// ExactFills: when true, exits are recorded at the exact stop/target price rather than
	// at tick.Price. This models limit-order fills (reality) vs the optimistic wick-price
	// fills of the default mode. Use for audit/validation runs only; leave false for live.
	ExactFills bool

	// PessimisticAmbiguous: when true, any LONG win that fires on the high tick of a 1m kline
	// is deferred until the next tick. If that tick (the kline's low) crosses the stop, the
	// trade is reclassified to a STOP. This corrects the synthetic open→high→low→close ordering
	// bias on ambiguous bars where both stop and target lie within the same 1m kline range.
	// Only applies to LONG positions; SHORT stops already fire first under synthetic ordering.
	PessimisticAmbiguous bool

	// FeeBps: round-trip taker fee in basis points, charged on entry notional for every
	// closed trade. 8 bps = 0.08% = Binance USDT-M Futures taker × 2 (entry + exit).
	// Default 0 reproduces fee-free historical behavior. Position notional = units × entry,
	// where units = StakeUSDT / stopDist. The leverage is implicit in `units`, so for tight
	// stops the per-trade fee scales up dramatically (e.g., $1000 stake at 0.18% stop →
	// $555k notional → $444 fee at 8 bps). This is the dominant realistic execution cost.
	FeeBps float64

	// StopSlippageBps: additional adverse slippage in bps on losing trades only.
	// Models stop-market orders filling past the trigger during fast moves.
	// 5 bps = 0.05% extra loss on the entry notional. Applied to losers only —
	// take-profit fills are assumed to be limit orders and slip neither way.
	StopSlippageBps float64

	// FundingBpsPerDay: average daily funding-rate drag in basis points on entry notional.
	// Binance USDT-M perpetuals charge funding every 8h; ≈0.01%/8h normal regime ≈ 3 bps/day.
	// Used as the simplification when no FundingProvider is configured.
	// 0 disables. Replaced by FundingProvider when set (see below).
	FundingBpsPerDay float64

	// FundingProvider: when non-nil, replaces the FundingBpsPerDay constant with a per-side
	// per-event accrual. The Historical provider in pkg/funding sums actual Binance funding
	// rates between the position's open and close times, signed correctly: longs pay positive
	// funding (cost > 0), shorts receive positive funding (cost < 0 = benefit).
	// Set FundingBpsPerDay to 0 when using a provider — the binary is responsible for picking
	// one path or the other.
	FundingProvider funding.Provider

	// TaxRatePct: effective tax rate applied to portfolio-level net positive PnL at end of run.
	// 0 disables. Example: 30 = 30% on net gains. Loss years assumed not to generate refunds
	// (conservative — most jurisdictions limit loss carry-back). Applied to total NET PnL only
	// when positive; trade-level metrics remain pre-tax for diagnostic purposes.
	TaxRatePct float64

	// MaxHoldHours: when > 0, any open position older than this is force-closed at the
	// current tick price. 0 disables. Useful to trim funding-eaten tails on signal-sparse
	// symbols (e.g., FILUSDT had 64-day average hold under target_rr=6.0). Force-close
	// classification: won = (pnl > 0) — same accounting as natural target/stop hits.
	MaxHoldHours float64

	// TrailingStopMode (B1): when true, ratchet the stop favorable as price moves.
	// At maxFavorableR ≥ 1.0, lock the stop at entry + (floor(maxFavorableR) − 1) × originalStopDist
	// (LONG; mirror for SHORT). Stop only ratchets in the favorable direction. Take-profit
	// still acts as a backstop. Cat B: trailing stop test (2026-05-07). See `results/cat_b_decision_rule_2026-05-07.md`.
	TrailingStopMode bool
	TrailIntervalR   float64 // 0 → defaults to 1.0

	// MultiLevelTPMode (B2): when true, scale out at mid-R. At entry ± MidRMult × originalStopDist,
	// close MidFrac of the position; emit a separate tradeResult with outcome=PARTIAL; raise
	// stop on the remainder to entry (BE). Remainder runs to either 6R target or BE-stop.
	// Cat B: multi-level TP test (2026-05-07). See pre-registered rule above.
	MultiLevelTPMode bool
	MidRMult         float64 // 0 → defaults to 3.0
	MidFrac          float64 // 0 → defaults to 0.5

	// timeStopCount: audit counter for positions force-closed by MaxHoldHours.
	timeStopCount int

	// partialCloseCount: audit counter for B2 mid-R partial closes.
	partialCloseCount int

	// JournalPath, when non-empty, is a directory where per-symbol JSONL trade journals
	// are written (one file per calendar month). Set by cmd/engine only — backtest
	// leaves this empty so no files are produced during historical runs.
	JournalPath string
	Symbol      string // required when JournalPath is set

	journalFile  *os.File
	journalMonth string // "YYYY-MM" of the open journal file

	// deferred-win state (PessimisticAmbiguous only)
	deferredWinActive bool
	deferredWinPrice  float64
	deferredWinMinute time.Time

	// audit counter: LONG wins reclassified to STOP due to same-bar ambiguity
	ambiguousOverrideCount int
}

type journalEntry struct {
	Event   string  `json:"event"` // "open" or "close"
	Symbol  string  `json:"symbol"`
	TS      string  `json:"ts"` // RFC3339 UTC
	Side    string  `json:"side"`
	Entry   float64 `json:"entry"`
	Exit    float64 `json:"exit,omitempty"`
	Stop    float64 `json:"stop"`
	Target  float64 `json:"target"`
	PnlPts  float64 `json:"pnl_pts,omitempty"`
	PnlUSD  float64 `json:"pnl_usd,omitempty"`
	Outcome string  `json:"outcome,omitempty"` // "TARGET" or "STOP" or "PARTIAL"
	Reason  string  `json:"reason"`
	// MFE/MAE in R-multiples — peak favorable / adverse excursion since entry.
	// Captured in close events only (open=0). Used for exit-policy counterfactual
	// analysis: any trade with MFE_R ≥ T would have triggered a partial-TP at T.
	MFER float64 `json:"mfe_r,omitempty"`
	MAER float64 `json:"mae_r,omitempty"`
}

func (s *Stub) appendJournal(entry journalEntry) {
	if s.JournalPath == "" {
		return
	}
	month := time.Now().UTC().Format("2006-01")
	if s.journalFile == nil || month != s.journalMonth {
		if s.journalFile != nil {
			_ = s.journalFile.Close()
		}
		if err := os.MkdirAll(s.JournalPath, 0755); err != nil {
			slog.Warn("journal mkdir failed", "err", err)
			return
		}
		name := filepath.Join(s.JournalPath, fmt.Sprintf("%s-%s.jsonl", s.Symbol, month))
		f, err := os.OpenFile(name, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			slog.Warn("journal open failed", "err", err)
			return
		}
		s.journalFile = f
		s.journalMonth = month
	}
	line, _ := json.Marshal(entry)
	line = append(line, '\n')
	_, _ = s.journalFile.Write(line)
}

type tradeResult struct {
	signal      *models.Signal
	exitPrice   float64
	exitTime    time.Time
	won         bool
	pnlPts      float64 // PnL in price points
	pnlUSDT     float64 // NET PnL in USD after fees, slippage, funding; 0 if StakeUSDT not set
	grossUSDT   float64 // gross PnL in USD before costs
	feeUSDT     float64 // round-trip fee deducted (always non-negative)
	slipUSDT    float64 // adverse stop slippage deducted (losers only, non-negative)
	fundingUSDT float64 // funding-rate cost deducted; signed when using Historical provider (positive = paid, negative = received), non-negative when using constant-rate Provider
	holdSeconds float64 // exit_time - entry_time, seconds (for hold-time distribution)
	stopDistPct float64 // stop distance as % of entry (for size-dispersion audit)
}

// OnSignal opens a new paper position. Rejects if a position is already open.
func (s *Stub) OnSignal(sig *models.Signal) {
	if s.position != nil {
		slog.Debug("signal rejected: position already open",
			"existing_entry", s.position.Signal.EntryPrice,
			"new_signal", sig.Reason)
		return
	}
	s.position = &OpenPosition{
		Signal:           sig,
		MaxAdverse:       sig.EntryPrice,
		LastPrice:        sig.EntryPrice,
		LastTime:         sig.Timestamp,
		OriginalStopDist: math.Abs(sig.EntryPrice - sig.StopLoss),
		RemainingFrac:    1.0,
	}
	slog.Info("position opened",
		"side", sig.Side,
		"entry", sig.EntryPrice,
		"stop", sig.StopLoss,
		"target", sig.TakeProfit,
		"reason", sig.Reason)
	s.appendJournal(journalEntry{
		Event:  "open",
		Symbol: s.Symbol,
		TS:     sig.Timestamp.UTC().Format(time.RFC3339),
		Side:   sig.Side.String(),
		Entry:  sig.EntryPrice,
		Stop:   sig.StopLoss,
		Target: sig.TakeProfit,
		Reason: sig.Reason,
	})
}

// OnTick checks whether the current price has hit stop or target for the open position.
func (s *Stub) OnTick(tick models.Tick) {
	// Resolve a deferred LONG win: the previous tick hit the target on a high tick;
	// we wait one more tick in the same 1m kline to check if the low crosses the stop.
	if s.deferredWinActive {
		sameKline := tick.Timestamp.Truncate(time.Minute).Equal(s.deferredWinMinute)
		if sameKline {
			sig := s.position.Signal
			if sig.Side == models.Long && tick.Price <= sig.StopLoss {
				// Ambiguous bar: low crossed stop in same kline as target — force STOP.
				s.ambiguousOverrideCount++
				exitPrice := tick.Price
				if s.ExactFills {
					exitPrice = sig.StopLoss
				}
				s.deferredWinActive = false
				s.closePosition(exitPrice, tick.Timestamp, false)
				return
			}
		}
		// No ambiguity (or different kline): commit the win.
		s.deferredWinActive = false
		s.closePosition(s.deferredWinPrice, tick.Timestamp, true)
		return
	}

	if s.position == nil {
		return
	}
	sig := s.position.Signal

	s.position.LastPrice = tick.Price
	s.position.LastTime = tick.Timestamp

	// MaxHoldHours: if the position has been open longer than the cap, force-close at
	// current tick price. Classification follows pnl sign — won = (pnl > 0).
	if s.MaxHoldHours > 0 && !sig.Timestamp.IsZero() {
		ageHours := tick.Timestamp.Sub(sig.Timestamp).Hours()
		if ageHours >= s.MaxHoldHours {
			won := false
			if sig.Side == models.Long {
				won = tick.Price > sig.EntryPrice
			} else {
				won = tick.Price < sig.EntryPrice
			}
			s.timeStopCount++
			s.closePosition(tick.Price, tick.Timestamp, won)
			return
		}
	}

	// Track max adverse excursion.
	if sig.Side == models.Long && tick.Price < s.position.MaxAdverse {
		s.position.MaxAdverse = tick.Price
	} else if sig.Side == models.Short && tick.Price > s.position.MaxAdverse {
		s.position.MaxAdverse = tick.Price
	}

	// Track max favorable excursion (always — needed for MFE/MAE journal records
	// regardless of TrailingStopMode). Pre-fix MFE was only updated when
	// trailing-stop was on, so MFE was silently 0 in normal-exit journals.
	if s.position.OriginalStopDist > 0 {
		var favR float64
		if sig.Side == models.Long {
			favR = (tick.Price - sig.EntryPrice) / s.position.OriginalStopDist
		} else {
			favR = (sig.EntryPrice - tick.Price) / s.position.OriginalStopDist
		}
		if favR > s.position.MaxFavorableR {
			s.position.MaxFavorableR = favR
		}
	}

	// B1 trailing stop: ratchet stop based on the MFE watermark we just updated.
	if s.TrailingStopMode && s.position.OriginalStopDist > 0 {
		interval := s.TrailIntervalR
		if interval <= 0 {
			interval = 1.0
		}
		// Lock (floor(maxFav/interval) × interval - interval)R favorable when ≥ interval.
		// At interval=1: maxFav 1.x → locked 0 (BE); 2.x → locked 1; etc.
		if s.position.MaxFavorableR >= interval {
			lockedR := math.Floor(s.position.MaxFavorableR/interval)*interval - interval
			if lockedR < 0 {
				lockedR = 0
			}
			var newStop float64
			if sig.Side == models.Long {
				newStop = sig.EntryPrice + lockedR*s.position.OriginalStopDist
				if newStop > sig.StopLoss { // only ratchet favorable
					sig.StopLoss = newStop
				}
			} else {
				newStop = sig.EntryPrice - lockedR*s.position.OriginalStopDist
				if newStop < sig.StopLoss {
					sig.StopLoss = newStop
				}
			}
		}
	}

	stopHit := (sig.Side == models.Long && tick.Price <= sig.StopLoss) ||
		(sig.Side == models.Short && tick.Price >= sig.StopLoss)

	targetHit := (sig.Side == models.Long && tick.Price >= sig.TakeProfit) ||
		(sig.Side == models.Short && tick.Price <= sig.TakeProfit)

	if stopHit {
		exitPrice := tick.Price
		if s.ExactFills {
			exitPrice = sig.StopLoss
		}
		// Under TrailingStopMode (or after a B2 partial that raised stop to BE),
		// the stop can fire at or above entry — classify as won by PnL sign.
		// Baseline behavior preserved when neither mode is active: stop = loss.
		won := false
		if s.TrailingStopMode || (s.MultiLevelTPMode && s.position.MidRHit) {
			if sig.Side == models.Long {
				won = exitPrice > sig.EntryPrice
			} else {
				won = exitPrice < sig.EntryPrice
			}
		}
		s.closePosition(exitPrice, tick.Timestamp, won)
		return
	} else if targetHit {
		exitPrice := tick.Price
		if s.ExactFills {
			exitPrice = sig.TakeProfit
		}
		// PessimisticAmbiguous: defer LONG wins so the next tick (kline low) can
		// override to STOP if the same 1m kline covers both stop and target.
		if s.PessimisticAmbiguous && sig.Side == models.Long {
			s.deferredWinActive = true
			s.deferredWinPrice = exitPrice
			s.deferredWinMinute = tick.Timestamp.Truncate(time.Minute)
			return
		}
		s.closePosition(exitPrice, tick.Timestamp, true)
		return
	}

	// B2 multi-level TP: at mid-R, close MidFrac of position; raise remainder stop to BE.
	// Fires at most once per position (MidRHit gate).
	if s.MultiLevelTPMode && !s.position.MidRHit && s.position.OriginalStopDist > 0 {
		midRMult := s.MidRMult
		if midRMult <= 0 {
			midRMult = 3.0
		}
		midFrac := s.MidFrac
		if midFrac <= 0 {
			midFrac = 0.5
		}
		var midRPrice float64
		var midRHit bool
		if sig.Side == models.Long {
			midRPrice = sig.EntryPrice + midRMult*s.position.OriginalStopDist
			midRHit = tick.Price >= midRPrice
		} else {
			midRPrice = sig.EntryPrice - midRMult*s.position.OriginalStopDist
			midRHit = tick.Price <= midRPrice
		}
		if midRHit {
			exitPrice := tick.Price
			if s.ExactFills {
				exitPrice = midRPrice
			}
			s.recordPartialClose(exitPrice, tick.Timestamp, midFrac)
			s.position.RemainingFrac -= midFrac
			s.position.MidRHit = true
			sig.StopLoss = sig.EntryPrice // raise stop to breakeven on remainder
		}
	}
}

// recordPartialClose emits a tradeResult for a partial close at midFrac of the original
// position size. Position remains open with reduced RemainingFrac. Used by B2 multi-level TP.
func (s *Stub) recordPartialClose(exitPrice float64, exitTime time.Time, frac float64) {
	sig := s.position.Signal
	stopDist := s.position.OriginalStopDist
	if stopDist == 0 {
		return
	}

	var pnlPts float64
	if sig.Side == models.Long {
		pnlPts = exitPrice - sig.EntryPrice
	} else {
		pnlPts = sig.EntryPrice - exitPrice
	}

	holdSeconds := 0.0
	if !exitTime.IsZero() && !sig.Timestamp.IsZero() {
		holdSeconds = exitTime.Sub(sig.Timestamp).Seconds()
		if holdSeconds < 0 {
			holdSeconds = 0
		}
	}

	var grossUSDT, feeUSDT, fundingUSDT, pnlUSDT float64
	if s.StakeUSDT > 0 {
		units := s.StakeUSDT * frac / stopDist
		grossUSDT = units * pnlPts
		notional := units * sig.EntryPrice
		if s.FeeBps > 0 {
			feeUSDT = s.FeeBps / 10000.0 * notional
		}
		// Partial close is always a winner (mid-R is favorable) — no slippage.
		if s.FundingProvider != nil && holdSeconds > 0 {
			fundingUSDT = s.FundingProvider.ChargeFor(sig.Side, notional, sig.Timestamp, exitTime)
		} else if s.FundingBpsPerDay > 0 && holdSeconds > 0 {
			holdDays := holdSeconds / 86400.0
			fundingUSDT = s.FundingBpsPerDay / 10000.0 * notional * holdDays
		}
		pnlUSDT = grossUSDT - feeUSDT - fundingUSDT
	}

	s.partialCloseCount++

	stopDistPct := 0.0
	if sig.EntryPrice > 0 {
		stopDistPct = stopDist / sig.EntryPrice * 100
	}
	s.results = append(s.results, tradeResult{
		signal:      sig,
		exitPrice:   exitPrice,
		exitTime:    exitTime,
		won:         true,
		pnlPts:      pnlPts,
		pnlUSDT:     pnlUSDT,
		grossUSDT:   grossUSDT,
		feeUSDT:     feeUSDT,
		slipUSDT:    0,
		fundingUSDT: fundingUSDT,
		holdSeconds: holdSeconds,
		stopDistPct: stopDistPct,
	})
	maeR := 0.0
	maeStopDist := math.Abs(sig.EntryPrice - sig.StopLoss)
	if maeStopDist > 0 {
		maeR = math.Abs(sig.EntryPrice-s.position.MaxAdverse) / maeStopDist
	}
	s.appendJournal(journalEntry{
		Event:   "close",
		Symbol:  s.Symbol,
		TS:      time.Now().UTC().Format(time.RFC3339),
		Side:    sig.Side.String(),
		Entry:   sig.EntryPrice,
		Exit:    exitPrice,
		Stop:    sig.StopLoss,
		Target:  sig.TakeProfit,
		PnlPts:  math.Round(pnlPts*100) / 100,
		PnlUSD:  math.Round(pnlUSDT*100) / 100,
		Outcome: "PARTIAL",
		Reason:  sig.Reason,
		MFER:    math.Round(s.position.MaxFavorableR*1000) / 1000,
		MAER:    math.Round(maeR*1000) / 1000,
	})
}

func (s *Stub) closePosition(exitPrice float64, exitTime time.Time, won bool) {
	sig := s.position.Signal

	var pnlPts float64
	if sig.Side == models.Long {
		pnlPts = exitPrice - sig.EntryPrice
	} else {
		pnlPts = sig.EntryPrice - exitPrice
	}

	// Hold time = exit - entry; used by funding-rate accrual and reporting.
	holdSeconds := 0.0
	if !exitTime.IsZero() && !sig.Timestamp.IsZero() {
		holdSeconds = exitTime.Sub(sig.Timestamp).Seconds()
		if holdSeconds < 0 {
			holdSeconds = 0
		}
	}

	// Fixed-stake dollar PnL: size the trade so that the stop distance = StakeUSDT.
	// units = (StakeUSDT × remainingFrac) / |entry - originalStop|; gross = units × pnlPts;
	// net = gross − fee − slip − funding. Uses OriginalStopDist (cached at OnSignal) so that
	// B1/B2 stop mutations don't break sizing. RemainingFrac scales for B2 partial closes.
	var grossUSDT, feeUSDT, slipUSDT, fundingUSDT, pnlUSDT float64
	if s.StakeUSDT > 0 {
		stopDist := s.position.OriginalStopDist
		if stopDist == 0 {
			stopDist = math.Abs(sig.EntryPrice - sig.StopLoss) // fallback for legacy callers
		}
		remainingFrac := s.position.RemainingFrac
		if remainingFrac == 0 {
			remainingFrac = 1.0 // legacy callers that didn't go through OnSignal
		}
		if stopDist > 0 {
			units := s.StakeUSDT * remainingFrac / stopDist
			grossUSDT = units * pnlPts
			notional := units * sig.EntryPrice
			if s.FeeBps > 0 {
				feeUSDT = s.FeeBps / 10000.0 * notional
			}
			if !won && s.StopSlippageBps > 0 {
				slipUSDT = s.StopSlippageBps / 10000.0 * notional
			}
			if s.FundingProvider != nil && holdSeconds > 0 {
				fundingUSDT = s.FundingProvider.ChargeFor(sig.Side, notional, sig.Timestamp, exitTime)
			} else if s.FundingBpsPerDay > 0 && holdSeconds > 0 {
				holdDays := holdSeconds / 86400.0
				fundingUSDT = s.FundingBpsPerDay / 10000.0 * notional * holdDays
			}
			pnlUSDT = grossUSDT - feeUSDT - slipUSDT - fundingUSDT
		}
	}

	outcome := "STOP"
	if won {
		outcome = "TARGET"
	}

	logArgs := []any{
		"outcome", outcome,
		"side", sig.Side,
		"entry", sig.EntryPrice,
		"exit", exitPrice,
		"pnl_pts", pnlPts,
	}
	if s.StakeUSDT > 0 {
		logArgs = append(logArgs, "pnl_usd", math.Round(pnlUSDT*100)/100)
	}
	logArgs = append(logArgs, "reason", sig.Reason)
	slog.Info("position closed", logArgs...)

	stopDistPct := 0.0
	if sig.EntryPrice > 0 {
		stopDistPct = math.Abs(sig.EntryPrice-sig.StopLoss) / sig.EntryPrice * 100
	}
	s.results = append(s.results, tradeResult{
		signal:      sig,
		exitPrice:   exitPrice,
		exitTime:    exitTime,
		won:         won,
		pnlPts:      pnlPts,
		pnlUSDT:     pnlUSDT,
		grossUSDT:   grossUSDT,
		feeUSDT:     feeUSDT,
		slipUSDT:    slipUSDT,
		fundingUSDT: fundingUSDT,
		holdSeconds: holdSeconds,
		stopDistPct: stopDistPct,
	})
	maeR := 0.0
	maeStopDist := math.Abs(sig.EntryPrice - sig.StopLoss)
	if maeStopDist > 0 {
		maeR = math.Abs(sig.EntryPrice-s.position.MaxAdverse) / maeStopDist
	}
	s.appendJournal(journalEntry{
		Event:   "close",
		Symbol:  s.Symbol,
		TS:      time.Now().UTC().Format(time.RFC3339),
		Side:    sig.Side.String(),
		Entry:   sig.EntryPrice,
		Exit:    exitPrice,
		Stop:    sig.StopLoss,
		Target:  sig.TakeProfit,
		PnlPts:  math.Round(pnlPts*100) / 100,
		PnlUSD:  math.Round(pnlUSDT*100) / 100,
		Outcome: outcome,
		Reason:  sig.Reason,
		MFER:    math.Round(s.position.MaxFavorableR*1000) / 1000,
		MAER:    math.Round(maeR*1000) / 1000,
	})
	s.position = nil
}

// Summary prints a structured PnL report at the end of a backtest run.
// Any position still open at end-of-data is force-closed at its last known price.
func (s *Stub) Summary() {
	// Commit any pending deferred win (data ended before next kline low arrived).
	if s.deferredWinActive {
		s.deferredWinActive = false
		s.closePosition(s.deferredWinPrice, s.deferredWinMinute, true)
	}
	if s.position != nil {
		slog.Warn("force-closing open position at end of data",
			"side", s.position.Signal.Side,
			"entry", s.position.Signal.EntryPrice,
			"last_price", s.position.LastPrice)
		s.closePosition(s.position.LastPrice, s.position.LastTime, false)
	}

	total := len(s.results)
	if total == 0 {
		slog.Info("backtest summary: no trades taken")
		return
	}

	wins := 0
	var totalPts, totalUSD float64
	var totalGrossUSD, totalFeeUSD, totalSlipUSD, totalFundingUSD float64
	var maxDD, maxProfit float64
	var longCount, shortCount, longWins, shortWins int
	var longNetUSD, shortNetUSD float64
	var totalHoldSeconds float64

	for _, r := range s.results {
		totalPts += r.pnlPts
		totalUSD += r.pnlUSDT
		totalGrossUSD += r.grossUSDT
		totalFeeUSD += r.feeUSDT
		totalSlipUSD += r.slipUSDT
		totalFundingUSD += r.fundingUSDT
		totalHoldSeconds += r.holdSeconds
		if r.won {
			wins++
		}
		if r.pnlPts > maxProfit {
			maxProfit = r.pnlPts
		}
		if r.pnlPts < maxDD {
			maxDD = r.pnlPts
		}
		if r.signal.Side == models.Long {
			longCount++
			longNetUSD += r.pnlUSDT
			if r.won {
				longWins++
			}
		} else {
			shortCount++
			shortNetUSD += r.pnlUSDT
			if r.won {
				shortWins++
			}
		}
	}

	// Tax model: applied to portfolio-level total NET if positive.
	// Loss runs assumed not to generate refunds (no carry-back). Conservative.
	var taxUSD, afterTaxUSD float64
	afterTaxUSD = totalUSD
	if s.TaxRatePct > 0 && totalUSD > 0 {
		taxUSD = totalUSD * s.TaxRatePct / 100.0
		afterTaxUSD = totalUSD - taxUSD
	}

	winRate := float64(wins) / float64(total) * 100

	var sumWinPts, sumLossPts float64
	var sumWinUSD, sumLossUSD float64
	var nWin, nLoss int
	for _, r := range s.results {
		if r.won {
			sumWinPts += r.pnlPts
			sumWinUSD += r.pnlUSDT
			nWin++
		} else {
			sumLossPts += math.Abs(r.pnlPts)
			sumLossUSD += math.Abs(r.pnlUSDT)
			nLoss++
		}
	}

	var avgWin, avgLoss float64
	if nWin > 0 {
		avgWin = sumWinPts / float64(nWin)
	}
	if nLoss > 0 {
		avgLoss = sumLossPts / float64(nLoss)
	}
	expectancyPts := (winRate/100)*avgWin - (1-winRate/100)*avgLoss

	var breakevenWinRate float64
	if avgWin+avgLoss > 0 {
		breakevenWinRate = avgLoss / (avgWin + avgLoss) * 100
	}

	logArgs := []any{
		"total_trades", total,
		"wins", wins,
		"losses", total - wins,
		"win_rate_pct", winRate,
		"breakeven_win_rate_pct", breakevenWinRate,
		"total_pnl_pts", totalPts,
		"avg_win_pts", avgWin,
		"avg_loss_pts", avgLoss,
		"win_loss_ratio", avgWin / avgLoss,
		"expectancy_pts", expectancyPts,
		"best_trade_pts", maxProfit,
		"worst_trade_pts", maxDD,
	}

	if s.StakeUSDT > 0 {
		var avgWinUSD, avgLossUSD float64
		if nWin > 0 {
			avgWinUSD = sumWinUSD / float64(nWin)
		}
		if nLoss > 0 {
			avgLossUSD = sumLossUSD / float64(nLoss)
		}
		expectancyUSD := (winRate/100)*avgWinUSD - (1-winRate/100)*avgLossUSD
		logArgs = append(logArgs,
			"stake_usd", s.StakeUSDT,
			"total_pnl_usd", math.Round(totalUSD*100)/100,
			"avg_win_usd", math.Round(avgWinUSD*100)/100,
			"avg_loss_usd", math.Round(avgLossUSD*100)/100,
			"expectancy_usd", math.Round(expectancyUSD*100)/100,
		)
		if s.FeeBps > 0 || s.StopSlippageBps > 0 || s.FundingBpsPerDay > 0 || s.FundingProvider != nil {
			fundingMode := "none"
			if s.FundingProvider != nil {
				fundingMode = "historical"
			} else if s.FundingBpsPerDay > 0 {
				fundingMode = "constant"
			}
			logArgs = append(logArgs,
				"fee_bps", s.FeeBps,
				"stop_slippage_bps", s.StopSlippageBps,
				"funding_mode", fundingMode,
				"funding_bps_per_day", s.FundingBpsPerDay,
				"gross_pnl_usd", math.Round(totalGrossUSD*100)/100,
				"total_fees_usd", math.Round(totalFeeUSD*100)/100,
				"total_slippage_usd", math.Round(totalSlipUSD*100)/100,
				"total_funding_usd", math.Round(totalFundingUSD*100)/100,
				"avg_fee_per_trade_usd", math.Round(totalFeeUSD/float64(total)*100)/100,
				"avg_hold_hours", math.Round(totalHoldSeconds/float64(total)/3600.0*10)/10,
			)
		}
		// Long/short split — diagnostic for drift-vs-signal.
		var longWR, shortWR float64
		if longCount > 0 {
			longWR = float64(longWins) / float64(longCount) * 100
		}
		if shortCount > 0 {
			shortWR = float64(shortWins) / float64(shortCount) * 100
		}
		logArgs = append(logArgs,
			"long_trades", longCount,
			"long_wins", longWins,
			"long_win_rate_pct", math.Round(longWR*10)/10,
			"long_net_usd", math.Round(longNetUSD*100)/100,
			"short_trades", shortCount,
			"short_wins", shortWins,
			"short_win_rate_pct", math.Round(shortWR*10)/10,
			"short_net_usd", math.Round(shortNetUSD*100)/100,
		)
		if s.TaxRatePct > 0 {
			logArgs = append(logArgs,
				"tax_rate_pct", s.TaxRatePct,
				"tax_usd", math.Round(taxUSD*100)/100,
				"after_tax_pnl_usd", math.Round(afterTaxUSD*100)/100,
			)
		}
		if s.MaxHoldHours > 0 {
			logArgs = append(logArgs,
				"max_hold_hours", s.MaxHoldHours,
				"time_stops", s.timeStopCount,
			)
		}
	}

	if s.PessimisticAmbiguous {
		logArgs = append(logArgs,
			"ambiguous_overrides", s.ambiguousOverrideCount,
			"ambiguous_override_pct_of_wins", math.Round(float64(s.ambiguousOverrideCount)/math.Max(1, float64(wins))*10000)/100,
		)
	}

	slog.Info("=== BACKTEST SUMMARY ===", logArgs...)

	// Stop-distance percentile audit (Test γ): % of entry that the stop sits below entry.
	// Tail blowups indicate signals on tiny-stop candles where units = stake/stopDist explodes.
	stopDists := make([]float64, len(s.results))
	for i, r := range s.results {
		stopDists[i] = r.stopDistPct
	}
	sort.Float64s(stopDists)
	pct := func(p float64) float64 {
		idx := int(p * float64(len(stopDists)))
		if idx >= len(stopDists) {
			idx = len(stopDists) - 1
		}
		if idx < 0 {
			idx = 0
		}
		return math.Round(stopDists[idx]*10000) / 10000
	}
	slog.Info("=== STOPDIST PERCENTILES (% of entry) ===",
		"p10", pct(0.10),
		"p50", pct(0.50),
		"p90", pct(0.90),
		"p99", pct(0.99),
		"p999", pct(0.999),
	)
}
