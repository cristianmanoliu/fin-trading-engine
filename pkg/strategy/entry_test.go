package strategy

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/cristianmanoliu/trading-engine/pkg/models"
)

// approxEqual reports whether a and b are within 1e-9 of each other.
// Used because float arithmetic in the stop/target math produces values
// like 100.60049999999998 when the algebraic answer is 100.6005.
func approxEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

// emaTestSetup primes an EntryDetector + BiasTracker with `numFlat` candles all
// having the same close price (defaults to 100). After this, both EMA9 and
// EMA21 are primed at the flat value, prevEma9 and prevEma21 equal that value,
// so the next AddCandle with a different close cleanly triggers a cross.
//
// `bias` controls the BiasTracker direction by feeding 4H candles whose
// open/close pattern produces the desired bias.
func emaTestSetup(t *testing.T, biasDir models.Direction) (*EntryDetector, *BiasTracker) {
	t.Helper()

	d := NewEntryDetector(EntryConfig{
		EMAMode:       true,
		TargetRR:      2.0,
		StopBufferPct: 0.001,
	})

	// Prime EMAs: 22 flat candles ensure both EMAs are primed AND prev values are non-zero.
	base := time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 22; i++ {
		d.AddCandle(models.Candle{
			Symbol:    "BTCUSDT",
			Open:      100,
			High:      100.5,
			Low:       99.5,
			Close:     100,
			CloseTime: base.Add(time.Duration(i) * 5 * time.Minute),
		})
	}

	// Build bias from a single 4H candle whose direction matches.
	bias := &BiasTracker{}
	switch biasDir {
	case models.Long:
		// Bullish: close > open, close > prevClose (prevClose=0 first time, just close > open).
		bias.Update(models.Candle{Open: 100, Close: 110})
	case models.Short:
		bias.Update(models.Candle{Open: 110, Close: 100})
	case models.Neutral:
		bias.Update(models.Candle{Open: 100, Close: 100}) // doji
	}
	if bias.Direction() != biasDir {
		t.Fatalf("setup: bias direction %v, want %v", bias.Direction(), biasDir)
	}

	return d, bias
}

func TestEMACrossoverNotPrimed(t *testing.T) {
	d := NewEntryDetector(EntryConfig{EMAMode: true, TargetRR: 2.0})
	bias := &BiasTracker{}
	bias.Update(models.Candle{Open: 100, Close: 110})

	// Only 5 candles — neither EMA primed.
	for i := 0; i < 5; i++ {
		d.AddCandle(models.Candle{
			Symbol: "BTCUSDT", Open: 100, High: 101, Low: 99, Close: 100,
			CloseTime: time.Now(),
		})
	}
	sig := d.Evaluate(nil, 0, bias)
	if sig != nil {
		t.Errorf("expected no signal before EMAs primed, got %+v", sig)
	}
}

func TestEMABullishCrossover_LongBias_EmitsLongSignal(t *testing.T) {
	d, bias := emaTestSetup(t, models.Long)

	// 23rd candle: close jumps up → ema9 rises faster than ema21 → bullish cross.
	upCandle := models.Candle{
		Symbol:    "BTCUSDT",
		Open:      100,
		High:      111,
		Low:       99.5,
		Close:     110,
		CloseTime: time.Date(2026, 5, 6, 1, 55, 0, 0, time.UTC),
	}
	d.AddCandle(upCandle)
	sig := d.Evaluate(nil, 0, bias)

	if sig == nil {
		t.Fatal("expected Long signal on bullish cross with Long bias, got nil")
	}
	if sig.Side != models.Long {
		t.Errorf("Side: got %v want Long", sig.Side)
	}
	if sig.EntryPrice != 110 {
		t.Errorf("EntryPrice: got %v want 110 (last close)", sig.EntryPrice)
	}
	// Stop = low * (1 - 0.001) = 99.5 * 0.999 = 99.4005
	expectedStop := 99.5 * (1 - 0.001)
	if !approxEqual(sig.StopLoss, expectedStop) {
		t.Errorf("StopLoss: got %v want %v (low - buffer)", sig.StopLoss, expectedStop)
	}
	// Target = entry + (entry - stop) * RR = 110 + (110-99.4005)*2 = 131.199
	expectedTarget := 110 + (110-expectedStop)*2.0
	if !approxEqual(sig.TakeProfit, expectedTarget) {
		t.Errorf("TakeProfit: got %v want %v", sig.TakeProfit, expectedTarget)
	}
	if !strings.Contains(sig.Reason, "ema_cross") {
		t.Errorf("Reason should describe EMA cross, got %q", sig.Reason)
	}
}

func TestEMABearishCrossover_ShortBias_EmitsShortSignal(t *testing.T) {
	d, bias := emaTestSetup(t, models.Short)

	downCandle := models.Candle{
		Symbol:    "BTCUSDT",
		Open:      100,
		High:      100.5,
		Low:       89,
		Close:     90,
		CloseTime: time.Date(2026, 5, 6, 1, 55, 0, 0, time.UTC),
	}
	d.AddCandle(downCandle)
	sig := d.Evaluate(nil, 0, bias)

	if sig == nil {
		t.Fatal("expected Short signal on bearish cross with Short bias, got nil")
	}
	if sig.Side != models.Short {
		t.Errorf("Side: got %v want Short", sig.Side)
	}
	if sig.EntryPrice != 90 {
		t.Errorf("EntryPrice: got %v want 90", sig.EntryPrice)
	}
	// Stop above the wick high: 100.5 * (1 + 0.001)
	expectedStop := 100.5 * (1 + 0.001)
	if !approxEqual(sig.StopLoss, expectedStop) {
		t.Errorf("StopLoss: got %v want %v", sig.StopLoss, expectedStop)
	}
	// Short target: entry - stop_dist*RR
	expectedTarget := 90 - (expectedStop-90)*2.0
	if !approxEqual(sig.TakeProfit, expectedTarget) {
		t.Errorf("TakeProfit: got %v want %v", sig.TakeProfit, expectedTarget)
	}
}

func TestEMABullishCrossover_ShortBias_NoSignal(t *testing.T) {
	d, bias := emaTestSetup(t, models.Short)

	upCandle := models.Candle{Symbol: "BTCUSDT", Open: 100, High: 111, Low: 99.5, Close: 110}
	d.AddCandle(upCandle)
	sig := d.Evaluate(nil, 0, bias)

	if sig != nil {
		t.Errorf("expected no signal (bullish cross + Short bias), got %+v", sig)
	}
}

func TestEMABearishCrossover_LongBias_NoSignal(t *testing.T) {
	d, bias := emaTestSetup(t, models.Long)

	downCandle := models.Candle{Symbol: "BTCUSDT", Open: 100, High: 100.5, Low: 89, Close: 90}
	d.AddCandle(downCandle)
	sig := d.Evaluate(nil, 0, bias)

	if sig != nil {
		t.Errorf("expected no signal (bearish cross + Long bias), got %+v", sig)
	}
}

func TestEMANoCross_FlatPrice_NoSignal(t *testing.T) {
	d, bias := emaTestSetup(t, models.Long)

	// 23rd candle: close stays at 100 → no cross.
	d.AddCandle(models.Candle{Symbol: "BTCUSDT", Open: 100, High: 100.5, Low: 99.5, Close: 100})
	sig := d.Evaluate(nil, 0, bias)

	if sig != nil {
		t.Errorf("expected no signal on flat price (no cross), got %+v", sig)
	}
}

func TestEMASideFilter_BlocksMatchingCross(t *testing.T) {
	// A bullish cross + Long bias would normally emit Long. With SideFilter=Short,
	// it must be blocked.
	d := NewEntryDetector(EntryConfig{
		EMAMode:       true,
		TargetRR:      2.0,
		StopBufferPct: 0.001,
		SideFilter:    models.Short,
	})

	base := time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 22; i++ {
		d.AddCandle(models.Candle{
			Symbol: "BTCUSDT", Open: 100, High: 100.5, Low: 99.5, Close: 100,
			CloseTime: base.Add(time.Duration(i) * 5 * time.Minute),
		})
	}
	bias := &BiasTracker{}
	bias.Update(models.Candle{Open: 100, Close: 110}) // Long bias

	d.AddCandle(models.Candle{Symbol: "BTCUSDT", Open: 100, High: 111, Low: 99.5, Close: 110})
	sig := d.Evaluate(nil, 0, bias)

	if sig != nil {
		t.Errorf("expected SideFilter=Short to block Long signal, got %+v", sig)
	}
}

func TestEMACrossoverWithFixedRR_TargetMatchesMultiplier(t *testing.T) {
	// Verify that TargetRR=6.0 (production setting) produces 6× stop_dist target.
	d := NewEntryDetector(EntryConfig{
		EMAMode:       true,
		TargetRR:      6.0,
		StopBufferPct: 0.001,
	})

	base := time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 22; i++ {
		d.AddCandle(models.Candle{
			Symbol: "BTCUSDT", Open: 100, High: 100.5, Low: 99.5, Close: 100,
			CloseTime: base.Add(time.Duration(i) * 5 * time.Minute),
		})
	}
	bias := &BiasTracker{}
	bias.Update(models.Candle{Open: 100, Close: 110})

	d.AddCandle(models.Candle{Symbol: "BTCUSDT", Open: 100, High: 111, Low: 99.5, Close: 110})
	sig := d.Evaluate(nil, 0, bias)
	if sig == nil {
		t.Fatal("expected signal")
	}
	stopDist := sig.EntryPrice - sig.StopLoss
	gotMultiplier := (sig.TakeProfit - sig.EntryPrice) / stopDist
	if gotMultiplier < 5.99 || gotMultiplier > 6.01 {
		t.Errorf("TargetRR multiplier: got %v want 6.0 (±0.01)", gotMultiplier)
	}
}
