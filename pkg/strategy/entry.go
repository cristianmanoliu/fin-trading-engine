package strategy

import (
	"fmt"
	"math"

	"github.com/cristianmanoliu/trading-engine/pkg/indicators"
	"github.com/cristianmanoliu/trading-engine/pkg/models"
)

// EntryDetector inspects a rolling window of closed candles (timeframe set by the runner)
// and generates trade signals when absorption / breakout / EMA-cross patterns are confirmed.
type EntryDetector struct {
	cfg    EntryConfig
	window []models.Candle // rolling buffer of closed signal-TF candles (newest last)

	// EMA crossover state (used only when EMAMode=true)
	ema9      *indicators.EMA
	ema21     *indicators.EMA
	prevEma9  float64 // EMA9 value before last candle (to detect crossover direction)
	prevEma21 float64

	// ATR-based stop state (used only when ATRStopMult > 0)
	atr *indicators.ATR
}

// EntryConfig holds the tunable parameters for entry detection.
type EntryConfig struct {
	ProximityPct      float64 // fraction of price considered "near" a level
	WickRatio         float64 // minimum wick/body ratio for absorption candles
	BreakoutBodyRatio float64 // minimum body/range ratio for a breakout candle
	AbsorptionCandles int     // consecutive candles required to confirm absorption
	StopBufferPct     float64 // fraction added beyond wick/level for stop placement
	MinRR             float64 // minimum reward/risk ratio; 0 = no filter
	TargetRR          float64 // fixed R:R multiplier for take-profit; 0 = use VWAP
	MomentumMode      bool    // when true: ignore levels, enter only on 5m momentum candles in 4H bias direction
	VWAPDeviationMode bool    // when true: fade price stretched ≥ VWAPDeviationPct from session VWAP; target = VWAP
	VWAPDeviationPct  float64 // minimum fractional distance from VWAP to trigger a deviation entry
	EMAMode           bool    // when true: enter on EMA fast/slow crossover aligned with bias; fixed TargetRR

	// EMAFastPeriod / EMASlowPeriod: configurable EMA periods (only used when EMAMode is true).
	// Defaults preserve legacy behavior: 9 (fast) and 21 (slow). Field names retain "9/21" in
	// detector internals (prevEma9, ema21) for git-blame continuity but accept any periods.
	EMAFastPeriod int // 0 → defaults to 9
	EMASlowPeriod int // 0 → defaults to 21

	// ATRStopMult: when > 0 (and EMAMode), place stop at last.Close ± ATRStopMult × ATR(period).
	// 0 keeps the legacy wick-based stop (last.Low or last.High with StopBufferPct).
	// Wider ATR-scaled stops reduce implicit leverage and per-trade fees on tight-stop signals.
	ATRStopMult float64
	ATRPeriod   int // ATR period; defaults to 14 when ATRStopMult > 0

	// SignalTimeframe: which candle timeframe drives signal evaluation. Default Timeframe5m
	// preserves legacy behavior. Set to Timeframe30m or Timeframe4H to test slower signals
	// with structurally wider stops (the cost-geometry escape).
	SignalTimeframe models.Timeframe

	// SideFilter: when set, only signals matching this direction are emitted.
	// Neutral (zero value) = no filter (legacy behavior). Long = longs only. Short = shorts only.
	// Used to test directional bias of the strategy (e.g., 4H EMA cross was found to be
	// short-side dominant on 2026-05-05 — longs slightly negative, shorts strongly positive).
	SideFilter models.Direction
}

// NewEntryDetector creates a detector with the given configuration.
func NewEntryDetector(cfg EntryConfig) *EntryDetector {
	d := &EntryDetector{cfg: cfg}
	if cfg.EMAMode {
		fast := cfg.EMAFastPeriod
		if fast <= 0 {
			fast = 9
		}
		slow := cfg.EMASlowPeriod
		if slow <= 0 {
			slow = 21
		}
		d.ema9 = indicators.NewEMA(fast)
		d.ema21 = indicators.NewEMA(slow)
	}
	if cfg.ATRStopMult > 0 {
		period := cfg.ATRPeriod
		if period <= 0 {
			period = 14
		}
		d.atr = indicators.NewATR(period)
	}
	return d
}

// AddCandle appends a closed 5m candle to the rolling window, keeping the last
// max(absorptionCandles+1, 3) candles for pattern analysis.
func (e *EntryDetector) AddCandle(c models.Candle) {
	if e.cfg.EMAMode {
		e.prevEma9 = e.ema9.Value()
		e.prevEma21 = e.ema21.Value()
		e.ema9.Update(c.Close)
		e.ema21.Update(c.Close)
	}
	if e.atr != nil {
		e.atr.Update(c)
	}

	e.window = append(e.window, c)
	keep := e.cfg.AbsorptionCandles + 1
	if keep < 3 {
		keep = 3
	}
	if len(e.window) > keep {
		e.window = e.window[len(e.window)-keep:]
	}
}

// Evaluate checks the current window and returns a Signal if a pattern is confirmed, nil otherwise.
// In MomentumMode it ignores levels and VWAP, entering only on 5m momentum candles in 4H bias direction.
// In VWAPDeviationMode it fades price stretched from VWAP; target is always VWAP.
func (e *EntryDetector) Evaluate(levels []float64, vwap float64, bias *BiasTracker) *models.Signal {
	if len(e.window) < 1 {
		return nil
	}

	last := e.window[len(e.window)-1]

	if e.cfg.MomentumMode {
		return e.checkMomentum(last, bias)
	}

	if e.cfg.VWAPDeviationMode {
		return e.checkVWAPDeviation(last, vwap)
	}

	if e.cfg.EMAMode {
		return e.checkEMACrossover(last, bias)
	}

	if len(e.window) < e.cfg.AbsorptionCandles {
		return nil
	}
	if vwap == 0 {
		return nil // no target without VWAP
	}

	for _, level := range levels {
		if level == 0 {
			continue
		}
		if sig := e.checkAbsorption(last, level, vwap, bias); sig != nil {
			return sig
		}
		if sig := e.checkBreakout(last, level, vwap, bias); sig != nil {
			return sig
		}
	}
	return nil
}

// checkMomentum detects trend-following entries: a strong 5m candle aligned with the 4H bias.
// Stop is placed beyond the candle's wick; target is a fixed TargetRR multiple of stop distance.
func (e *EntryDetector) checkMomentum(last models.Candle, bias *BiasTracker) *models.Signal {
	if bias.Direction() == models.Neutral {
		return nil
	}

	candleRange := last.High - last.Low
	if candleRange == 0 {
		return nil
	}
	bodySize := math.Abs(last.Close - last.Open)
	if bodySize/candleRange < e.cfg.BreakoutBodyRatio {
		return nil
	}

	bullish := last.Close > last.Open
	var side models.Direction
	if bias.Direction() == models.Long && bullish {
		side = models.Long
	} else if bias.Direction() == models.Short && !bullish {
		side = models.Short
	} else {
		return nil
	}

	var stopLoss float64
	if side == models.Long {
		stopLoss = last.Low * (1 - e.cfg.StopBufferPct)
	} else {
		stopLoss = last.High * (1 + e.cfg.StopBufferPct)
	}

	stopDist := math.Abs(last.Close - stopLoss)
	if stopDist == 0 {
		return nil
	}

	targetMult := e.cfg.TargetRR
	if targetMult <= 0 {
		targetMult = 2.0
	}

	var takeProfit float64
	if side == models.Long {
		takeProfit = last.Close + stopDist*targetMult
	} else {
		takeProfit = last.Close - stopDist*targetMult
	}

	return &models.Signal{
		Symbol:     last.Symbol,
		Side:       side,
		EntryPrice: last.Close,
		StopLoss:   stopLoss,
		TakeProfit: takeProfit,
		Timestamp:  last.CloseTime,
		Reason: fmt.Sprintf("momentum %s | bias=%s | body=%.0f%% | rr=%.1f",
			side, bias.Direction(), bodySize/candleRange*100, targetMult),
	}
}

// checkVWAPDeviation fades price that is stretched significantly from the session VWAP.
// It enters a reversal trade when the 5m candle confirms the move is failing (close is back
// toward VWAP). Target is always VWAP; stop is beyond the candle's wick extreme.
func (e *EntryDetector) checkVWAPDeviation(last models.Candle, vwap float64) *models.Signal {
	if vwap == 0 {
		return nil
	}

	deviation := (last.Close - vwap) / vwap

	var side models.Direction
	if deviation >= e.cfg.VWAPDeviationPct {
		side = models.Short // stretched above VWAP — fade down to VWAP
	} else if deviation <= -e.cfg.VWAPDeviationPct {
		side = models.Long // stretched below VWAP — fade up to VWAP
	} else {
		return nil // not far enough from VWAP
	}

	// Candle must close back toward VWAP (reversal confirmation).
	bullish := last.Close > last.Open
	if side == models.Short && bullish {
		return nil // still trending up, not reversing
	}
	if side == models.Long && !bullish {
		return nil // still trending down, not reversing
	}

	var stopLoss float64
	if side == models.Long {
		stopLoss = last.Low * (1 - e.cfg.StopBufferPct)
	} else {
		stopLoss = last.High * (1 + e.cfg.StopBufferPct)
	}

	risk := math.Abs(last.Close - stopLoss)
	if risk == 0 {
		return nil
	}
	reward := math.Abs(vwap - last.Close)
	rr := reward / risk
	if e.cfg.MinRR > 0 && rr < e.cfg.MinRR {
		return nil
	}

	return &models.Signal{
		Symbol:     last.Symbol,
		Side:       side,
		EntryPrice: last.Close,
		StopLoss:   stopLoss,
		TakeProfit: vwap,
		Timestamp:  last.CloseTime,
		Reason: fmt.Sprintf("vwap_deviation %s | dev=%.3f%% | rr=%.2f | vwap=%.2f",
			side, deviation*100, rr, vwap),
	}
}

// checkEMACrossover detects EMA9/EMA21 crossovers on the 5m chart aligned with 4H bias.
// A bullish cross (EMA9 crosses above EMA21) triggers a long when bias is Long.
// A bearish cross (EMA9 crosses below EMA21) triggers a short when bias is Short.
// Stop is placed beyond the candle wick; target is a fixed TargetRR multiple.
func (e *EntryDetector) checkEMACrossover(last models.Candle, bias *BiasTracker) *models.Signal {
	if !e.ema9.Primed() || !e.ema21.Primed() {
		return nil // not enough candles to prime both EMAs
	}
	if e.prevEma9 == 0 || e.prevEma21 == 0 {
		return nil // no previous state yet
	}

	curEma9 := e.ema9.Value()
	curEma21 := e.ema21.Value()

	bullishCross := e.prevEma9 <= e.prevEma21 && curEma9 > curEma21
	bearishCross := e.prevEma9 >= e.prevEma21 && curEma9 < curEma21

	if !bullishCross && !bearishCross {
		return nil
	}

	var side models.Direction
	if bullishCross && bias.Direction() == models.Long {
		side = models.Long
	} else if bearishCross && bias.Direction() == models.Short {
		side = models.Short
	} else {
		return nil // not aligned with 4H bias
	}

	// Optional one-sided filter (longs-only or shorts-only experiments).
	if e.cfg.SideFilter != models.Neutral && side != e.cfg.SideFilter {
		return nil
	}

	var stopLoss float64
	stopMode := "wick"
	if e.cfg.ATRStopMult > 0 && e.atr != nil && e.atr.Primed() {
		atrDist := e.cfg.ATRStopMult * e.atr.Value()
		if side == models.Long {
			stopLoss = last.Close - atrDist
		} else {
			stopLoss = last.Close + atrDist
		}
		stopMode = fmt.Sprintf("atr×%.1f", e.cfg.ATRStopMult)
	} else if side == models.Long {
		stopLoss = last.Low * (1 - e.cfg.StopBufferPct)
	} else {
		stopLoss = last.High * (1 + e.cfg.StopBufferPct)
	}

	stopDist := math.Abs(last.Close - stopLoss)
	if stopDist == 0 {
		return nil
	}

	targetMult := e.cfg.TargetRR
	if targetMult <= 0 {
		targetMult = 2.0
	}

	var takeProfit float64
	if side == models.Long {
		takeProfit = last.Close + stopDist*targetMult
	} else {
		takeProfit = last.Close - stopDist*targetMult
	}

	return &models.Signal{
		Symbol:     last.Symbol,
		Side:       side,
		EntryPrice: last.Close,
		StopLoss:   stopLoss,
		TakeProfit: takeProfit,
		Timestamp:  last.CloseTime,
		Reason: fmt.Sprintf("ema_cross %s | ema9=%.2f ema21=%.2f | bias=%s | stop=%s | rr=%.1f",
			side, curEma9, curEma21, bias.Direction(), stopMode, targetMult),
	}
}

// checkAbsorption detects exhaustion / bounce setups at a level.
//
// Conditions:
//  1. Last candle close is within proximityPct of the level (price approaching but not breaking).
//  2. The required number of consecutive candles have wick/body ≥ wickRatio on the level side.
//  3. The last candle body closes away from the level (rejection confirmed).
func (e *EntryDetector) checkAbsorption(last models.Candle, level, vwap float64, bias *BiasTracker) *models.Signal {
	prox := last.Close * e.cfg.ProximityPct

	// Price must be near the level but NOT through it.
	dist := math.Abs(last.Close - level)
	if dist > prox {
		return nil
	}

	// Determine which side of the level we're on.
	approachingFromBelow := last.Close < level
	side := models.Short // approaching resistance from below → expect short bounce
	if !approachingFromBelow {
		side = models.Long // approaching support from above → expect long bounce
	}

	// Check that the last N candles show absorption wicks on the level side.
	n := e.cfg.AbsorptionCandles
	if len(e.window) < n {
		return nil
	}
	absCandles := e.window[len(e.window)-n:]

	for _, c := range absCandles {
		if !hasAbsorptionWick(c, level, e.cfg.WickRatio) {
			return nil
		}
	}

	// Confirm last candle body closes away from the level (rejection).
	bodyClose := last.Close
	if approachingFromBelow {
		// Body should close below the level (failed to break it).
		if bodyClose >= level {
			return nil
		}
	} else {
		// Body should close above the level.
		if bodyClose <= level {
			return nil
		}
	}

	if !bias.Allows(side, false) {
		return nil
	}

	// Target must be on the correct side of entry — no signal if VWAP is unreachable.
	if side == models.Long && vwap <= last.Close {
		return nil
	}
	if side == models.Short && vwap >= last.Close {
		return nil
	}

	// Stop beyond the worst wick in the absorption window.
	wickExtreme := worstWick(absCandles, approachingFromBelow)
	var stopLoss float64
	if approachingFromBelow {
		stopLoss = wickExtreme * (1 + e.cfg.StopBufferPct)
	} else {
		stopLoss = wickExtreme * (1 - e.cfg.StopBufferPct)
	}

	reward := math.Abs(vwap - last.Close)
	risk := math.Abs(last.Close - stopLoss)
	rr := reward / risk
	if e.cfg.MinRR > 0 && rr < e.cfg.MinRR {
		return nil
	}

	return &models.Signal{
		Symbol:     last.Symbol,
		Side:       side,
		EntryPrice: last.Close,
		StopLoss:   stopLoss,
		TakeProfit: vwap,
		Timestamp:  last.CloseTime,
		Reason: fmt.Sprintf("absorption at level %.2f | wick_ratio≥%.1f | %d candles | vwap=%.2f | rr=%.2f",
			level, e.cfg.WickRatio, n, vwap, rr),
	}
}

// checkBreakout detects momentum / continuation setups through a level.
//
// Conditions:
//  1. Last candle closes beyond the level (price broke through).
//  2. Candle body/range ≥ breakoutBodyRatio (momentum candle).
//  3. Aligned with 4H bias (breakouts only taken with the trend).
func (e *EntryDetector) checkBreakout(last models.Candle, level, vwap float64, bias *BiasTracker) *models.Signal {
	bullishBreak := last.Close > level && last.Open < level
	bearishBreak := last.Close < level && last.Open > level

	if !bullishBreak && !bearishBreak {
		return nil
	}

	candleRange := last.High - last.Low
	if candleRange == 0 {
		return nil
	}
	bodySize := math.Abs(last.Close - last.Open)
	if bodySize/candleRange < e.cfg.BreakoutBodyRatio {
		return nil
	}

	var side models.Direction
	var stopLoss float64

	if bullishBreak {
		side = models.Long
		stopLoss = level * (1 - e.cfg.StopBufferPct)
	} else {
		side = models.Short
		stopLoss = level * (1 + e.cfg.StopBufferPct)
	}

	if !bias.Allows(side, true) {
		return nil
	}

	// Target must be on the correct side of entry.
	if side == models.Long && vwap <= last.Close {
		return nil
	}
	if side == models.Short && vwap >= last.Close {
		return nil
	}

	reward := math.Abs(vwap - last.Close)
	risk := math.Abs(last.Close - stopLoss)
	rr := reward / risk
	if e.cfg.MinRR > 0 && rr < e.cfg.MinRR {
		return nil
	}

	return &models.Signal{
		Symbol:     last.Symbol,
		Side:       side,
		EntryPrice: last.Close,
		StopLoss:   stopLoss,
		TakeProfit: vwap,
		Timestamp:  last.CloseTime,
		Reason: fmt.Sprintf("breakout through level %.2f | body_ratio=%.2f | vwap=%.2f | rr=%.2f",
			level, bodySize/candleRange, vwap, rr),
	}
}

// hasAbsorptionWick returns true if the candle shows a long wick toward the level
// with a wick/body ratio ≥ threshold.
func hasAbsorptionWick(c models.Candle, level, threshold float64) bool {
	body := math.Abs(c.Close - c.Open)
	if body == 0 {
		body = 0.0001 // avoid divide-by-zero on doji
	}

	var wick float64
	if c.Close > level || c.Open > level {
		// Level is below — measure lower wick (support test).
		lowerBody := math.Min(c.Close, c.Open)
		wick = lowerBody - c.Low
	} else {
		// Level is above — measure upper wick (resistance test).
		upperBody := math.Max(c.Close, c.Open)
		wick = c.High - upperBody
	}

	if wick <= 0 {
		return false
	}
	return wick/body >= threshold
}

// worstWick returns the most extreme price reached toward the level across the candle slice.
func worstWick(candles []models.Candle, approachingFromBelow bool) float64 {
	if len(candles) == 0 {
		return 0
	}
	if approachingFromBelow {
		worst := candles[0].High
		for _, c := range candles[1:] {
			if c.High > worst {
				worst = c.High
			}
		}
		return worst
	}
	worst := candles[0].Low
	for _, c := range candles[1:] {
		if c.Low < worst {
			worst = c.Low
		}
	}
	return worst
}
