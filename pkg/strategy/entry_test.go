package strategy

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/cristianmanoliu/fin-trading-engine/pkg/models"
)

// captureSlog redirects slog.Default into a buffer for assertion. Restored on
// test cleanup. Mirrors the helper in pkg/notify/telegram_test.go — kept local
// (not extracted to a shared testutil package) to avoid cross-package test
// dependencies; the surface is small and the two copies don't drift.
func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

// findSuppressionEvent scans captured slog output for the first
// "signal suppressed" record and returns its parsed fields. Returns nil if
// no such record was emitted. Used by the suppression-observability tests.
func findSuppressionEvent(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	for line := range strings.SplitSeq(buf.String(), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		if rec["msg"] == "signal suppressed" {
			return rec
		}
	}
	return nil
}

// approxEqual reports whether a and b are within 1e-9 of each other.
// Used because float arithmetic in the stop/target math produces values
// like 100.60049999999998 when the algebraic answer is 100.6005.
func approxEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

// TestTargetRRFallbackConsistency pins that every TargetRR <= 0 fallback
// in entry.go uses the same value (6.0). Pre-fix, checkMomentum (line
// ~418) and checkEMACrossover (line ~916) used 2.0 while the 5 sibling
// check_* functions used 6.0. The 2.0 sites were Option-C era defaults
// that never got updated when the locked spec moved to 6:1 RR.
//
// Risk path the inconsistency enables: operator deploys without
// --target-rr override AND YAML target_rr=0 → cfg.Strategy.TargetRR=0
// → fallback fires → live strategy silently runs at 2.0 RR instead of
// locked 6.0. Especially severe for checkEMACrossover (the live entry
// path on the deployed-16 fleet).
//
// This test scans entry.go source for the fallback pattern and asserts
// every site uses 6.0. A future change to a different fallback value
// MUST update this test in lockstep — preventing the silent drift from
// recurring.
func TestTargetRRFallbackConsistency(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not determine test file path")
	}
	entryGo := filepath.Join(filepath.Dir(thisFile), "entry.go")
	src, err := os.ReadFile(entryGo)
	if err != nil {
		t.Fatalf("read entry.go: %v", err)
	}

	// Match the pattern `(rr|targetMult) = <number>` immediately after
	// a `<= 0 {` test. Greedy-tolerant of intervening whitespace + blank
	// lines so the test doesn't break on formatting changes.
	pattern := regexp.MustCompile(`<=\s*0\s*\{\s*\n\s*(?:rr|targetMult)\s*=\s*([\d.]+)`)
	matches := pattern.FindAllSubmatch(src, -1)
	if len(matches) < 5 {
		t.Fatalf("found only %d TargetRR fallback sites in entry.go — expected at "+
			"least 5 (checkRSIBreakdown / checkMACDCross / checkEMACrossover plus "+
			"two others). Pattern may have changed.", len(matches))
	}

	for i, m := range matches {
		val := string(m[1])
		if val != "6.0" {
			t.Errorf("TargetRR fallback site #%d uses %s — must be 6.0 to match locked "+
				"CLAUDE.md spec + the other %d sites. Pre-fix this allowed the live "+
				"strategy to silently run at 2.0 RR if config didn't propagate.",
				i+1, val, len(matches)-1)
		}
	}
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

func TestEMABullishCrossover_NeutralBias_NoSignal(t *testing.T) {
	// Neutral bias should block both directions — EMA cross requires
	// direction agreement (bias == Long for bull cross, == Short for bear).
	d, bias := emaTestSetup(t, models.Neutral)

	upCandle := models.Candle{Symbol: "BTCUSDT", Open: 100, High: 111, Low: 99.5, Close: 110}
	d.AddCandle(upCandle)
	sig := d.Evaluate(nil, 0, bias)

	if sig != nil {
		t.Errorf("expected no signal (bullish cross + Neutral bias), got %+v", sig)
	}
}

func TestEMABearishCrossover_NeutralBias_NoSignal(t *testing.T) {
	d, bias := emaTestSetup(t, models.Neutral)

	downCandle := models.Candle{Symbol: "BTCUSDT", Open: 100, High: 100.5, Low: 89, Close: 90}
	d.AddCandle(downCandle)
	sig := d.Evaluate(nil, 0, bias)

	if sig != nil {
		t.Errorf("expected no signal (bearish cross + Neutral bias), got %+v", sig)
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

func TestBollingerSideFilter_BlocksLongBreakout(t *testing.T) {
	// REGRESSION (2026-06-09): non-EMA entry modes did NOT apply cfg.SideFilter —
	// only checkEMACrossover + checkFundingCross had the inline check. So a
	// `--side-filter short` Bollinger run still emitted LONG signals on a bullish
	// breakout (close > upper band). With a long target ABOVE entry, a rising price
	// hit that target and booked a PHANTOM "short win" (bb20 bull-year artifact:
	// ~$2.25M phantom profit 2020-2021). The fix enforces SideFilter centrally in
	// Evaluate for ALL modes. This test locks it for the Bollinger path: a bullish
	// breakout under SideFilter=Short must be blocked (no Long signal leaks).
	d := NewEntryDetector(EntryConfig{
		BollingerMode:    true,
		BollingerPeriod:  20,
		BollingerStdMult: 2.0,
		TargetRR:         6.0,
		StopBufferPct:    0.001,
		SideFilter:       models.Short,
	})

	base := time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC)
	// Prime the 20-period Bollinger with flat candles so the bands are tight.
	for i := 0; i < 22; i++ {
		d.AddCandle(models.Candle{
			Symbol: "BTCUSDT", Open: 100, High: 100.2, Low: 99.8, Close: 100,
			CloseTime: base.Add(time.Duration(i) * 5 * time.Minute),
		})
	}
	bias := &BiasTracker{}
	bias.Update(models.Candle{Open: 100, Close: 130}) // Long bias (allows long)

	// A candle closing far ABOVE the upper band = bullish breakout → would emit
	// LONG. SideFilter=Short must drop it.
	d.AddCandle(models.Candle{
		Symbol: "BTCUSDT", Open: 100, High: 121, Low: 99.8, Close: 120,
		CloseTime: base.Add(23 * 5 * time.Minute),
	})
	sig := d.Evaluate(nil, 0, bias)

	if sig != nil {
		t.Errorf("expected SideFilter=Short to block Long Bollinger breakout, got %+v", sig)
	}
}

func TestBollingerSideFilter_AllowsShortBreakdown(t *testing.T) {
	// Companion to the block test: a BEARISH breakdown (close < lower band) under
	// SideFilter=Short MUST still emit a valid Short (the filter blocks longs, not
	// shorts). Guards against an over-broad fix that drops everything.
	d := NewEntryDetector(EntryConfig{
		BollingerMode:    true,
		BollingerPeriod:  20,
		BollingerStdMult: 2.0,
		TargetRR:         6.0,
		StopBufferPct:    0.001,
		SideFilter:       models.Short,
	})

	base := time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 22; i++ {
		d.AddCandle(models.Candle{
			Symbol: "BTCUSDT", Open: 100, High: 100.2, Low: 99.8, Close: 100,
			CloseTime: base.Add(time.Duration(i) * 5 * time.Minute),
		})
	}
	bias := &BiasTracker{}
	bias.Update(models.Candle{Open: 100, Close: 70}) // Short bias (allows short)

	// A candle closing far BELOW the lower band = bearish breakdown → Short.
	d.AddCandle(models.Candle{
		Symbol: "BTCUSDT", Open: 100, High: 100.2, Low: 79, Close: 80,
		CloseTime: base.Add(23 * 5 * time.Minute),
	})
	sig := d.Evaluate(nil, 0, bias)

	if sig == nil {
		t.Fatal("expected a Short signal on bearish breakdown under SideFilter=Short, got nil")
	}
	if sig.Side != models.Short {
		t.Errorf("expected Short, got %v", sig.Side)
	}
	if sig.TakeProfit >= sig.EntryPrice {
		t.Errorf("short target must be BELOW entry, got target=%.2f entry=%.2f (the phantom-win bug)",
			sig.TakeProfit, sig.EntryPrice)
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

// ── EMA suppression observability — 2026-05-12 ──────────────────────────────
// Closes the gate-informationality gap that turned the 2026-05-12 trade-rate
// investigation into a 30-min drill. checkEMACrossover now emits a
// "signal suppressed" slog event at each silent return-nil site, tagged with
// the filter that blocked. Operators can now `grep -c "signal suppressed"` to
// compute the actual cross detection rate vs the bias/side/d1/vol filter rates
// — instead of retro-deriving via Binance EMA recomputation.
// Memo: memory/trade_rate_investigation_2026-05-12.md.

func TestEMASuppression_BiasMismatch_LogsReason(t *testing.T) {
	// Bull cross + Short bias → cross detected, bias misaligned → suppress.
	// Test mirrors TestEMABullishCrossover_ShortBias_NoSignal but additionally
	// asserts the slog event is emitted with filter=bias_mismatch.
	buf := captureSlog(t)
	d, bias := emaTestSetup(t, models.Short)

	d.AddCandle(models.Candle{Symbol: "BTCUSDT", Open: 100, High: 111, Low: 99.5, Close: 110})
	sig := d.Evaluate(nil, 0, bias)
	if sig != nil {
		t.Fatalf("expected suppressed signal, got %+v", sig)
	}

	rec := findSuppressionEvent(t, buf)
	if rec == nil {
		t.Fatalf("expected 'signal suppressed' slog event, got:\n%s", buf.String())
	}
	if rec["filter"] != "bias_mismatch" {
		t.Errorf("filter field: got %v want bias_mismatch", rec["filter"])
	}
	if rec["cross_side"] != "LONG" {
		t.Errorf("cross_side field: got %v want LONG (bull cross direction)", rec["cross_side"])
	}
	if rec["bias"] != "SHORT" {
		t.Errorf("bias field: got %v want SHORT", rec["bias"])
	}
}

func TestEMASuppression_SideFilter_LogsReason(t *testing.T) {
	// Bull cross + Long bias would normally emit Long. With SideFilter=Short
	// it must be suppressed AND logged with filter=side_filter.
	buf := captureSlog(t)
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
		t.Fatalf("expected suppressed signal, got %+v", sig)
	}

	rec := findSuppressionEvent(t, buf)
	if rec == nil {
		t.Fatalf("expected 'signal suppressed' slog event, got:\n%s", buf.String())
	}
	if rec["filter"] != "side_filter" {
		t.Errorf("filter field: got %v want side_filter", rec["filter"])
	}
	if rec["cross_side"] != "LONG" {
		t.Errorf("cross_side field: got %v want LONG (bias-aligned side before filter)", rec["cross_side"])
	}
	if rec["side_filter"] != "SHORT" {
		t.Errorf("side_filter field: got %v want SHORT", rec["side_filter"])
	}
}

func TestEMASuppression_NoCross_NoLog(t *testing.T) {
	// Flat price → no cross detected → MUST NOT emit suppression event. The
	// suppression slog is only for cross-detected-but-filtered cases, not for
	// every candle close. Without this guard, log volume explodes.
	buf := captureSlog(t)
	d, bias := emaTestSetup(t, models.Long)

	d.AddCandle(models.Candle{Symbol: "BTCUSDT", Open: 100, High: 100.5, Low: 99.5, Close: 100})
	_ = d.Evaluate(nil, 0, bias)

	if rec := findSuppressionEvent(t, buf); rec != nil {
		t.Errorf("unexpected suppression event on no-cross candle: %+v", rec)
	}
}

func TestEMASuppression_PassThrough_NoLog(t *testing.T) {
	// Cross + bias align + no side-filter → signal emitted → MUST NOT log
	// suppression. Counter-test for TestEMASuppression_NoCross_NoLog.
	buf := captureSlog(t)
	d, bias := emaTestSetup(t, models.Long)

	d.AddCandle(models.Candle{Symbol: "BTCUSDT", Open: 100, High: 111, Low: 99.5, Close: 110})
	sig := d.Evaluate(nil, 0, bias)
	if sig == nil {
		t.Fatal("setup error: expected signal emission, got nil")
	}
	if rec := findSuppressionEvent(t, buf); rec != nil {
		t.Errorf("unexpected suppression event on emitted signal: %+v", rec)
	}
}

// ── Cat F1 funding-cross standalone signal tests ─────────────────────────────
// Pre-registered 2026-05-07 — see results/cat_f1_funding_cross_decision_rule_2026-05-07.md.
// Trigger: funding × 3 × 10000 > +threshold → SHORT; < −threshold → LONG.

func newFundingCrossDetector(threshold float64, rateFn func(time.Time) float64) *EntryDetector {
	d := NewEntryDetector(EntryConfig{
		FundingCrossMode:          true,
		FundingThresholdBpsPerDay: threshold,
		StopBufferPct:             0.001,
		TargetRR:                  6.0,
	})
	d.SetFundingRateReader(rateFn)
	return d
}

func TestFundingCross_NoReader_NoSignal(t *testing.T) {
	d := NewEntryDetector(EntryConfig{
		FundingCrossMode:          true,
		FundingThresholdBpsPerDay: 30,
		StopBufferPct:             0.001,
		TargetRR:                  6.0,
	})
	// No SetFundingRateReader call → fundingRate is nil.
	d.AddCandle(models.Candle{Symbol: "BTCUSDT", Open: 100, High: 101, Low: 99, Close: 100,
		CloseTime: time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)})
	if sig := d.Evaluate(nil, 0, &BiasTracker{}); sig != nil {
		t.Errorf("expected nil signal when fundingRate reader unset, got %+v", sig)
	}
}

func TestFundingCross_BelowPositiveThreshold_NoSignal(t *testing.T) {
	// 29.9 bp/day = rate8h × 3 × 10000 < 30 → no entry
	rate := 29.9 / (3 * 10000)
	d := newFundingCrossDetector(30, func(time.Time) float64 { return rate })
	d.AddCandle(models.Candle{Symbol: "BTCUSDT", Open: 100, High: 101, Low: 99, Close: 100,
		CloseTime: time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)})
	if sig := d.Evaluate(nil, 0, &BiasTracker{}); sig != nil {
		t.Errorf("expected nil signal at 29.9bp/day (below 30 threshold), got %+v", sig)
	}
}

func TestFundingCross_AbovePositiveThreshold_FiresShort(t *testing.T) {
	// 35 bp/day positive → short entry (overcrowded longs revert)
	rate := 35.0 / (3 * 10000)
	d := newFundingCrossDetector(30, func(time.Time) float64 { return rate })
	d.AddCandle(models.Candle{Symbol: "BTCUSDT", Open: 100, High: 101, Low: 99, Close: 100,
		CloseTime: time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)})
	sig := d.Evaluate(nil, 0, &BiasTracker{})
	if sig == nil {
		t.Fatal("expected SHORT signal at 35bp/day funding (above 30 threshold)")
	}
	if sig.Side != models.Short {
		t.Errorf("expected Short side, got %v", sig.Side)
	}
	// Wick stop: high × (1 + 0.001) = 101.101
	if sig.StopLoss < 101.10 || sig.StopLoss > 101.11 {
		t.Errorf("expected stop ≈ 101.101, got %v", sig.StopLoss)
	}
	// 6:1 RR: target = entry − 6 × stop_dist
	stopDist := sig.StopLoss - sig.EntryPrice
	expectedTarget := sig.EntryPrice - 6*stopDist
	if sig.TakeProfit < expectedTarget-0.01 || sig.TakeProfit > expectedTarget+0.01 {
		t.Errorf("expected target ≈ %v (6:1 RR), got %v", expectedTarget, sig.TakeProfit)
	}
}

func TestFundingCross_BelowNegativeThreshold_FiresLong(t *testing.T) {
	// −35 bp/day → long entry (overcrowded shorts revert)
	rate := -35.0 / (3 * 10000)
	d := newFundingCrossDetector(30, func(time.Time) float64 { return rate })
	d.AddCandle(models.Candle{Symbol: "BTCUSDT", Open: 100, High: 101, Low: 99, Close: 100,
		CloseTime: time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)})
	sig := d.Evaluate(nil, 0, &BiasTracker{})
	if sig == nil {
		t.Fatal("expected LONG signal at −35bp/day funding (below −30 threshold)")
	}
	if sig.Side != models.Long {
		t.Errorf("expected Long side, got %v", sig.Side)
	}
	// Wick stop: low × (1 − 0.001) = 98.901
	if sig.StopLoss < 98.90 || sig.StopLoss > 98.91 {
		t.Errorf("expected stop ≈ 98.901, got %v", sig.StopLoss)
	}
}

func TestFundingCross_Neutral_NoSignal(t *testing.T) {
	// Funding within ±30 → no entry
	rate := 0.00005 // = 1.5 bp/day, well inside threshold
	d := newFundingCrossDetector(30, func(time.Time) float64 { return rate })
	d.AddCandle(models.Candle{Symbol: "BTCUSDT", Open: 100, High: 101, Low: 99, Close: 100,
		CloseTime: time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)})
	if sig := d.Evaluate(nil, 0, &BiasTracker{}); sig != nil {
		t.Errorf("expected nil signal at 1.5bp/day funding (well inside threshold), got %+v", sig)
	}
}

func TestFundingCross_SideFilterShort_LongSignalSuppressed(t *testing.T) {
	// SideFilter=Short + funding signal would fire LONG → suppressed
	rate := -50.0 / (3 * 10000)
	d := NewEntryDetector(EntryConfig{
		FundingCrossMode:          true,
		FundingThresholdBpsPerDay: 30,
		StopBufferPct:             0.001,
		TargetRR:                  6.0,
		SideFilter:                models.Short,
	})
	d.SetFundingRateReader(func(time.Time) float64 { return rate })
	d.AddCandle(models.Candle{Symbol: "BTCUSDT", Open: 100, High: 101, Low: 99, Close: 100,
		CloseTime: time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)})
	if sig := d.Evaluate(nil, 0, &BiasTracker{}); sig != nil {
		t.Errorf("expected nil signal (SideFilter=Short blocks LONG funding signal), got %+v", sig)
	}
}

// TestSnapshot_ObservabilityFieldsPopulatedWithoutModeFlags verifies the 2026-05-24
// schema-completeness fix: ATR, RealizedVol30dAnn, and BB are captured in Snapshot
// even when ATRStopMult==0, VolFilterMode==false, and BollingerMode==false.
// Pre-fix, these were absent from all 235 live/shadow signal-context records.
func TestSnapshot_ObservabilityFieldsPopulatedWithoutModeFlags(t *testing.T) {
	cfg := EntryConfig{
		EMAMode: true,
		// ATRStopMult == 0 (wick-based stop, production default)
		// VolFilterMode == false (not deployed)
		// BollingerMode == false (EMA mode, not BB mode)
	}
	d := NewEntryDetector(cfg)

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Feed enough candles to prime ATR(14), BB(20), and realizedVol (≥30).
	// Prices move: 100 → 110 in steps to give non-trivial ATR/vol/BB values.
	for i := 0; i < 40; i++ {
		price := 100.0 + float64(i)*0.25
		d.AddCandle(models.Candle{
			Symbol:    "TEST",
			Open:      price - 0.1,
			High:      price + 0.5,
			Low:       price - 0.5,
			Close:     price,
			CloseTime: base.Add(time.Duration(i) * 5 * time.Minute),
		})
	}

	snap := d.Snapshot()

	// ATR must be primed and non-zero (14 candles min; we fed 40).
	if snap.ATR == 0 {
		t.Error("ATR is 0 in Snapshot — unconditional construction fix not effective")
	}

	// RealizedVol must be primed (≥30 observations needed; we fed 40).
	if snap.RealizedVol30dAnn == 0 {
		t.Error("RealizedVol30dAnn is 0 in Snapshot — VolFilterMode guard removal not effective")
	}

	// BB must be primed (20 candles min; we fed 40).
	if snap.BBMid == 0 {
		t.Error("BBMid is 0 in Snapshot — unconditional Bollinger construction fix not effective")
	}
	if snap.BBUpper == 0 {
		t.Error("BBUpper is 0 in Snapshot")
	}
	if snap.BBLower == 0 {
		t.Error("BBLower is 0 in Snapshot")
	}
}
