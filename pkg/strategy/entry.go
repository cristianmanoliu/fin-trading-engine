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

	// RSI state (used only when RSIMode=true)
	rsi     *indicators.RSI
	prevRSI float64

	// MACD state (used only when MACDMode=true)
	macd       *indicators.MACD
	prevMACD   float64
	prevSignal float64

	// Bollinger state (used only when BollingerMode=true). Zone flags edge-trigger
	// the entry: fire only on the FIRST close that enters the band region. Without
	// this gate, sustained moves below the lower band would re-fire on every candle.
	bollinger      *indicators.Bollinger
	inBBLowerZone  bool // last evaluation: close was below lower band
	inBBUpperZone  bool // last evaluation: close was above upper band

	// D1 confluence state (used only when Confluence1DMode=true).
	// Sample 4H closes at CloseTime hour=0 UTC into a 1D EMA pair. Used as
	// a gating filter inside checkEMACrossover — only emit signals whose direction
	// agrees with the 1D bias. Cat D1: 2026-05-07.
	confluenceEMAFast *indicators.EMA
	confluenceEMASlow *indicators.EMA

	// E1 vol-regime state (used only when VolFilterMode=true). Rolling window of
	// 4H log returns; realized vol annualized via sqrt(6×365). Updated every AddCandle.
	// Used as a gating filter inside checkEMACrossover. Cat E1: 2026-05-07.
	logReturns      []float64
	realizedVol30d  float64 // annualized; 0 until window has ≥30 returns
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

	// PDHPDLBreakMode: when true, enter on PDH/PDL breakdown using the same
	// fixed-TargetRR exit framework as EMA mode (wick stop, 6:1 target by default,
	// max-hold cap). Independent of the legacy absorption+breakout strategy which
	// uses VWAP as target. Cat A: PDH/PDL break test (2026-05-06 EOS).
	PDHPDLBreakMode bool

	// RSIMode: enter on RSI crossing 50 from above (short) or below (long).
	// Same exit framework as EMA mode. Cat A: RSI test (2026-05-06 EOS).
	RSIMode   bool
	RSIPeriod int // 0 → defaults to 14

	// MACDMode: enter on MACD line crossing signal line. Bullish cross (MACD up
	// through signal) → long; bearish cross → short. Same exit framework as EMA.
	// Cat A: MACD test (2026-05-07).
	MACDMode     bool
	MACDFast     int // 0 → defaults to 12
	MACDSlow     int // 0 → defaults to 26
	MACDSignal   int // 0 → defaults to 9

	// BollingerMode: enter on close breaking outside Bollinger bands. Close <
	// lower band → bearish breakdown (short); close > upper band → bullish
	// breakout (long). Edge-triggered (fires only on band entry). Same exit
	// framework as EMA. Cat A: Bollinger test (2026-05-07).
	BollingerMode    bool
	BollingerPeriod  int     // 0 → defaults to 20
	BollingerStdMult float64 // 0 → defaults to 2.0

	// Confluence1DMode (D1): when true and EMAMode is active, gate signals by 1D EMA
	// bias agreement. 1D EMA pair is sampled at 4H candle CloseTime hour=0 UTC (Binance
	// canonical daily close). Only emit when 1D EMA bias agrees with signal direction.
	// Cat D1: multi-TF confluence test (2026-05-07).
	Confluence1DMode     bool
	ConfluenceFastPeriod int // 0 → defaults to 9 (1D EMA fast)
	ConfluenceSlowPeriod int // 0 → defaults to 21 (1D EMA slow)

	// VolFilterMode (E1): when true and EMAMode is active, gate signals by realized
	// volatility regime. Skip entries when 30-day annualized realized vol exceeds
	// MaxVolAnnualized. Computed on rolling 180 4H log returns × sqrt(6×365).
	// Cat E1: vol-regime filter test (2026-05-07).
	VolFilterMode    bool
	MaxVolAnnualized float64 // 0 → defaults to 1.20 (120% annualized)

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
	if cfg.RSIMode {
		period := cfg.RSIPeriod
		if period <= 0 {
			period = 14
		}
		d.rsi = indicators.NewRSI(period)
	}
	if cfg.MACDMode {
		d.macd = indicators.NewMACD(cfg.MACDFast, cfg.MACDSlow, cfg.MACDSignal)
	}
	if cfg.BollingerMode {
		d.bollinger = indicators.NewBollinger(cfg.BollingerPeriod, cfg.BollingerStdMult)
	}
	if cfg.Confluence1DMode {
		fast := cfg.ConfluenceFastPeriod
		if fast <= 0 {
			fast = 9
		}
		slow := cfg.ConfluenceSlowPeriod
		if slow <= 0 {
			slow = 21
		}
		d.confluenceEMAFast = indicators.NewEMA(fast)
		d.confluenceEMASlow = indicators.NewEMA(slow)
	}
	return d
}

// Snapshot returns the indicator values for a SignalContext record. Zero
// values mean "indicator not configured / not primed" — consumers should
// rely on omitempty in the JSON encoding rather than sentinel values.
func (e *EntryDetector) Snapshot() IndicatorSnapshot {
	s := IndicatorSnapshot{}
	if e.cfg.EMAMode {
		if e.ema9 != nil && e.ema9.Primed() {
			s.EMA9 = e.ema9.Value()
		}
		if e.ema21 != nil && e.ema21.Primed() {
			s.EMA21 = e.ema21.Value()
		}
	}
	if e.atr != nil && e.atr.Primed() {
		s.ATR = e.atr.Value()
	}
	if e.cfg.VolFilterMode {
		s.RealizedVol30dAnn = e.realizedVol30d
	}
	if e.cfg.BollingerMode && e.bollinger != nil && e.bollinger.Primed() {
		lower, mid, upper := e.bollinger.Value()
		s.BBLower = lower
		s.BBMid = mid
		s.BBUpper = upper
	}
	return s
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
	if e.cfg.RSIMode && e.rsi != nil {
		e.prevRSI = e.rsi.Value()
		e.rsi.Update(c.Close)
	}
	if e.cfg.MACDMode && e.macd != nil {
		e.prevMACD, e.prevSignal = e.macd.Value()
		e.macd.Update(c.Close)
	}
	if e.cfg.BollingerMode && e.bollinger != nil {
		e.bollinger.Update(c.Close)
	}

	// D1 confluence: sample 4H close into 1D EMA pair at UTC day boundary.
	// CloseTime hour=0 UTC marks the close of the 20-24 UTC 4H candle (Binance
	// canonical daily close). One sample per calendar day.
	if e.cfg.Confluence1DMode && e.confluenceEMAFast != nil && e.confluenceEMASlow != nil {
		if c.CloseTime.UTC().Hour() == 0 {
			e.confluenceEMAFast.Update(c.Close)
			e.confluenceEMASlow.Update(c.Close)
		}
	}

	// E1 vol filter: roll log returns and recompute realized vol when window has ≥30 obs.
	if e.cfg.VolFilterMode && len(e.window) >= 1 {
		prev := e.window[len(e.window)-1].Close
		if prev > 0 && c.Close > 0 {
			ret := math.Log(c.Close / prev)
			e.logReturns = append(e.logReturns, ret)
			if len(e.logReturns) > 180 {
				e.logReturns = e.logReturns[len(e.logReturns)-180:]
			}
			if len(e.logReturns) >= 30 {
				var sum float64
				for _, r := range e.logReturns {
					sum += r
				}
				mean := sum / float64(len(e.logReturns))
				var sumSq float64
				for _, r := range e.logReturns {
					d := r - mean
					sumSq += d * d
				}
				stdev := math.Sqrt(sumSq / float64(len(e.logReturns)))
				e.realizedVol30d = stdev * math.Sqrt(6.0*365.0)
			}
		}
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

	if e.cfg.RSIMode {
		return e.checkRSIBreakdown(last, bias)
	}

	if e.cfg.MACDMode {
		return e.checkMACDCross(last, bias)
	}

	if e.cfg.BollingerMode {
		return e.checkBollingerBreakdown(last, bias)
	}

	if e.cfg.PDHPDLBreakMode {
		// Iterate levels to find the most recently breached one and emit a
		// fixed-RR signal. Different from legacy checkBreakout which uses VWAP target.
		for _, level := range levels {
			if level == 0 {
				continue
			}
			if sig := e.checkPDHPDLBreakFixedRR(last, level, bias); sig != nil {
				return sig
			}
		}
		return nil
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

// checkRSIBreakdown fires when RSI crosses 50 from above (bearish — short)
// or from below (bullish — long). Uses the same fixed-TargetRR exit framework
// as EMAMode: wick stop, target = TargetRR × stop distance, side filter applied.
// Cat A: RSI test 2026-05-06 EOS.
func (e *EntryDetector) checkRSIBreakdown(last models.Candle, bias *BiasTracker) *models.Signal {
	if e.rsi == nil || !e.rsi.Primed() {
		return nil
	}
	if e.prevRSI == 0 {
		return nil // need previous value
	}
	cur := e.rsi.Value()

	bullishCross := e.prevRSI <= 50 && cur > 50
	bearishCross := e.prevRSI >= 50 && cur < 50
	if !bullishCross && !bearishCross {
		return nil
	}

	var side models.Direction
	if bullishCross {
		side = models.Long
	} else {
		side = models.Short
	}
	if !bias.Allows(side, true) {
		return nil
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
	rr := e.cfg.TargetRR
	if rr <= 0 {
		rr = 6.0
	}
	var takeProfit float64
	if side == models.Long {
		takeProfit = last.Close + risk*rr
	} else {
		takeProfit = last.Close - risk*rr
	}

	return &models.Signal{
		Symbol:     last.Symbol,
		Side:       side,
		EntryPrice: last.Close,
		StopLoss:   stopLoss,
		TakeProfit: takeProfit,
		Timestamp:  last.CloseTime,
		Reason: fmt.Sprintf("rsi_cross_50 %s | prev=%.1f cur=%.1f | rr=%.1f",
			side, e.prevRSI, cur, rr),
	}
}

// checkMACDCross fires when MACD line crosses the signal line. Bullish cross
// (MACD up through signal) → long; bearish cross → short. Uses the same fixed-
// TargetRR exit framework as EMAMode/RSIMode (wick stop, side filter).
// Cat A: MACD test 2026-05-07.
func (e *EntryDetector) checkMACDCross(last models.Candle, bias *BiasTracker) *models.Signal {
	if e.macd == nil || !e.macd.Primed() {
		return nil
	}
	if e.prevMACD == 0 && e.prevSignal == 0 {
		return nil // need a previous reading
	}
	curMACD, curSignal := e.macd.Value()

	bullishCross := e.prevMACD <= e.prevSignal && curMACD > curSignal
	bearishCross := e.prevMACD >= e.prevSignal && curMACD < curSignal
	if !bullishCross && !bearishCross {
		return nil
	}

	var side models.Direction
	if bullishCross {
		side = models.Long
	} else {
		side = models.Short
	}
	if !bias.Allows(side, true) {
		return nil
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
	rr := e.cfg.TargetRR
	if rr <= 0 {
		rr = 6.0
	}
	var takeProfit float64
	if side == models.Long {
		takeProfit = last.Close + risk*rr
	} else {
		takeProfit = last.Close - risk*rr
	}

	return &models.Signal{
		Symbol:     last.Symbol,
		Side:       side,
		EntryPrice: last.Close,
		StopLoss:   stopLoss,
		TakeProfit: takeProfit,
		Timestamp:  last.CloseTime,
		Reason: fmt.Sprintf("macd_cross %s | macd=%.4f signal=%.4f | rr=%.1f",
			side, curMACD, curSignal, rr),
	}
}

// checkBollingerBreakdown fires when close breaks outside Bollinger bands.
// Close < lower band → bearish breakdown (short); close > upper band → bullish
// breakout (long). Edge-triggered via inBB*Zone flags so sustained moves below
// the band don't re-fire each candle. Same exit framework as EMA/RSI.
// Cat A: Bollinger test 2026-05-07.
func (e *EntryDetector) checkBollingerBreakdown(last models.Candle, bias *BiasTracker) *models.Signal {
	if e.bollinger == nil || !e.bollinger.Primed() {
		return nil
	}
	lower, _, upper := e.bollinger.Value()

	nowBelowLower := last.Close < lower
	nowAboveUpper := last.Close > upper

	bearishBreakdown := !e.inBBLowerZone && nowBelowLower
	bullishBreakout := !e.inBBUpperZone && nowAboveUpper

	// Update zone flags for the next evaluation, regardless of whether we fire.
	e.inBBLowerZone = nowBelowLower
	e.inBBUpperZone = nowAboveUpper

	if !bearishBreakdown && !bullishBreakout {
		return nil
	}

	var side models.Direction
	var band float64
	if bullishBreakout {
		side = models.Long
		band = upper
	} else {
		side = models.Short
		band = lower
	}
	if !bias.Allows(side, true) {
		return nil
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
	rr := e.cfg.TargetRR
	if rr <= 0 {
		rr = 6.0
	}
	var takeProfit float64
	if side == models.Long {
		takeProfit = last.Close + risk*rr
	} else {
		takeProfit = last.Close - risk*rr
	}

	return &models.Signal{
		Symbol:     last.Symbol,
		Side:       side,
		EntryPrice: last.Close,
		StopLoss:   stopLoss,
		TakeProfit: takeProfit,
		Timestamp:  last.CloseTime,
		Reason: fmt.Sprintf("bollinger_break %s | close=%.4f band=%.4f | rr=%.1f",
			side, last.Close, band, rr),
	}
}

// checkPDHPDLBreakFixedRR fires when the candle breaks through PDH/PDL on a
// strong-body close. Same exit framework as EMA mode (wick stop, fixed TargetRR
// target). Independent of legacy checkBreakout which uses VWAP target.
// Cat A: PDH/PDL break test 2026-05-06 EOS.
func (e *EntryDetector) checkPDHPDLBreakFixedRR(last models.Candle, level float64, bias *BiasTracker) *models.Signal {
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

	risk := math.Abs(last.Close - stopLoss)
	if risk == 0 {
		return nil
	}
	rr := e.cfg.TargetRR
	if rr <= 0 {
		rr = 6.0
	}
	var takeProfit float64
	if side == models.Long {
		takeProfit = last.Close + risk*rr
	} else {
		takeProfit = last.Close - risk*rr
	}

	return &models.Signal{
		Symbol:     last.Symbol,
		Side:       side,
		EntryPrice: last.Close,
		StopLoss:   stopLoss,
		TakeProfit: takeProfit,
		Timestamp:  last.CloseTime,
		Reason: fmt.Sprintf("pdh_pdl_break %s | level=%.4f | body_ratio=%.2f | rr=%.1f",
			side, level, bodySize/candleRange, rr),
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

	// D1 confluence: gate by 1D EMA bias agreement. Only emit when 1D bias
	// matches signal direction. Skips signal when 1D EMAs not yet primed
	// (conservative — no false positives during warmup).
	if e.cfg.Confluence1DMode && e.confluenceEMAFast != nil && e.confluenceEMASlow != nil {
		if !e.confluenceEMAFast.Primed() || !e.confluenceEMASlow.Primed() {
			return nil
		}
		biasFast := e.confluenceEMAFast.Value()
		biasSlow := e.confluenceEMASlow.Value()
		// 1D bias DOWN (fast < slow) → only allow shorts. 1D bias UP → only allow longs.
		if side == models.Short && biasFast >= biasSlow {
			return nil
		}
		if side == models.Long && biasFast <= biasSlow {
			return nil
		}
	}

	// E1 vol filter: skip when realized 30d annualized vol exceeds threshold.
	// realizedVol30d is 0 until the rolling window has ≥30 observations — during
	// warmup the filter is a no-op (signals pass through).
	if e.cfg.VolFilterMode && e.realizedVol30d > 0 {
		threshold := e.cfg.MaxVolAnnualized
		if threshold <= 0 {
			threshold = 1.20
		}
		if e.realizedVol30d > threshold {
			return nil
		}
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
