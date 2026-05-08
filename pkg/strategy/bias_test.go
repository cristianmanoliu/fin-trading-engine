package strategy

import (
	"testing"

	"github.com/cristianmanoliu/trading-engine/pkg/models"
)

// BiasTracker is the macro-trend gate that every breakout entry passes
// through (called from EntryDetector.checkBreakout via bias.Allows). The
// state machine has subtle invariants:
//
//   - bullish requires both Close>Open AND Close>prev_close (strict)
//   - bearish requires both Close<Open AND Close<prev_close (strict)
//   - prev_close updates on EVERY Update, including ones that resolve to
//     Neutral — a bug that froze prev_close on Neutral would cascade
//     wrong bias on subsequent candles
//   - prev_close==0 is the "first candle" sentinel; any non-zero close
//     becomes the comparison anchor going forward
//
// These tests pin all four invariants with hand-traced sequences. A subtle
// regression (off-by-one, equality vs strict, prev_close-stuck-on-neutral)
// would silently misroute every entry signal — and EntryDetector currently
// tests bias only via fixture-mocked state, so the state machine itself
// has no direct guard.

func mkBiasCandle(open, close float64) models.Candle {
	return models.Candle{Open: open, Close: close}
}

func TestBiasTracker_ZeroValueIsNeutral(t *testing.T) {
	var b BiasTracker
	if b.Direction() != models.Neutral {
		t.Errorf("zero-value tracker direction = %v, want Neutral (=0)", b.Direction())
	}
}

func TestBiasTracker_FirstCandle_GreenIsLong(t *testing.T) {
	var b BiasTracker
	b.Update(mkBiasCandle(100, 105))
	if b.Direction() != models.Long {
		t.Errorf("first green candle (100→105): direction = %v, want Long", b.Direction())
	}
}

func TestBiasTracker_FirstCandle_RedIsShort(t *testing.T) {
	var b BiasTracker
	b.Update(mkBiasCandle(100, 95))
	if b.Direction() != models.Short {
		t.Errorf("first red candle (100→95): direction = %v, want Short", b.Direction())
	}
}

func TestBiasTracker_FirstCandle_DojiIsNeutral(t *testing.T) {
	// Open == Close: neither bullish nor bearish.
	var b BiasTracker
	b.Update(mkBiasCandle(100, 100))
	if b.Direction() != models.Neutral {
		t.Errorf("first doji (100→100): direction = %v, want Neutral", b.Direction())
	}
}

func TestBiasTracker_SubsequentGreen_RequiresHigherClose(t *testing.T) {
	// Green candle alone is insufficient — close must also exceed prev_close.
	var b BiasTracker
	b.Update(mkBiasCandle(100, 110)) // Long; prev_close=110
	b.Update(mkBiasCandle(105, 108)) // green (105→108) but 108<110 → Neutral
	if b.Direction() != models.Neutral {
		t.Errorf("green candle with Close<prev_close: direction = %v, want Neutral", b.Direction())
	}
}

func TestBiasTracker_SubsequentRed_RequiresLowerClose(t *testing.T) {
	// Red candle alone is insufficient — close must also fall below prev_close.
	var b BiasTracker
	b.Update(mkBiasCandle(100, 90)) // Short; prev_close=90
	b.Update(mkBiasCandle(95, 92))  // red (95→92) but 92>90 → Neutral
	if b.Direction() != models.Neutral {
		t.Errorf("red candle with Close>prev_close: direction = %v, want Neutral", b.Direction())
	}
}

func TestBiasTracker_EqualCloseBlocksBullContinuation(t *testing.T) {
	// Strict comparison: Close == prev_close → Neutral, even with green body.
	var b BiasTracker
	b.Update(mkBiasCandle(100, 110)) // Long; prev_close=110
	b.Update(mkBiasCandle(105, 110)) // green (105→110) but 110==prev_close → Neutral
	if b.Direction() != models.Neutral {
		t.Errorf("Close==prev_close (bull side): direction = %v, want Neutral", b.Direction())
	}
}

func TestBiasTracker_EqualCloseBlocksBearContinuation(t *testing.T) {
	var b BiasTracker
	b.Update(mkBiasCandle(100, 90)) // Short; prev_close=90
	b.Update(mkBiasCandle(95, 90))  // red (95→90) but 90==prev_close → Neutral
	if b.Direction() != models.Neutral {
		t.Errorf("Close==prev_close (bear side): direction = %v, want Neutral", b.Direction())
	}
}

func TestBiasTracker_DojiAfterTrend_ResetsToNeutral(t *testing.T) {
	// Doji on subsequent candle: Close>Open and Close<Open both false → Neutral
	// regardless of relation to prev_close.
	var b BiasTracker
	b.Update(mkBiasCandle(100, 110)) // Long
	b.Update(mkBiasCandle(106, 106)) // doji → Neutral
	if b.Direction() != models.Neutral {
		t.Errorf("doji after Long: direction = %v, want Neutral", b.Direction())
	}
}

func TestBiasTracker_PrevCloseUpdatesOnNeutralCandle(t *testing.T) {
	// CRITICAL: prev_close must update on EVERY candle, including Neutral.
	// A bug that froze prev_close on Neutral would cascade incorrect bias
	// on the next candle. This test distinguishes the two behaviors.
	var b BiasTracker
	b.Update(mkBiasCandle(100, 110)) // Long; prev_close=110
	b.Update(mkBiasCandle(108, 108)) // doji → Neutral; prev_close MUST update to 108
	// Next candle: green close at 109. If prev_close stuck at 110: 109<110 → Neutral.
	// With correct prev_close=108: 109>108 → Long.
	b.Update(mkBiasCandle(105, 109))
	if b.Direction() != models.Long {
		t.Errorf("after Neutral candle: prev_close should track every update; got direction = %v, want Long", b.Direction())
	}
}

func TestBiasTracker_TransitionsAcrossSequence(t *testing.T) {
	// Pin the full state-machine response to a representative multi-bar sequence
	// covering every transition path: Long→Long, Long→Short, Short→Short,
	// Short→Neutral (doji), Neutral→Long.
	var b BiasTracker
	steps := []struct {
		open, close float64
		want        models.Direction
		desc        string
	}{
		{100, 110, models.Long, "first green: Long, prev=110"},
		{105, 115, models.Long, "green close 115>prev 110: Long, prev=115"},
		{120, 112, models.Short, "red close 112<prev 115: Short, prev=112"},
		{110, 105, models.Short, "red close 105<prev 112: Short, prev=105"},
		{106, 106, models.Neutral, "doji: Neutral, prev=106"},
		{105, 115, models.Long, "green close 115>prev 106: Long, prev=115"},
	}
	for i, s := range steps {
		b.Update(mkBiasCandle(s.open, s.close))
		if b.Direction() != s.want {
			t.Errorf("step %d (%s): direction = %v, want %v", i+1, s.desc, b.Direction(), s.want)
		}
	}
}

func TestBiasTracker_Allows_AbsorptionAlwaysTrue(t *testing.T) {
	// isBreakout=false (absorption mode): unconditional pass, regardless of
	// bias state or tradeDir. CLAUDE.md flags this branch as dead code in
	// the live path (checkAbsorption never actually filters on bias), but
	// the API contract still asserts unconditional true here.
	cases := []struct {
		bias    models.Direction
		dir     models.Direction
		biasStr string
	}{
		{models.Neutral, models.Long, "Neutral"},
		{models.Neutral, models.Short, "Neutral"},
		{models.Long, models.Long, "Long"},
		{models.Long, models.Short, "Long"},
		{models.Short, models.Long, "Short"},
		{models.Short, models.Short, "Short"},
	}
	for _, c := range cases {
		b := &BiasTracker{current: c.bias}
		if !b.Allows(c.dir, false) {
			t.Errorf("Allows(dir=%v, breakout=false, bias=%s) = false, want true (absorption is unconditional)",
				c.dir, c.biasStr)
		}
	}
}

func TestBiasTracker_Allows_BreakoutNeutralBlocks(t *testing.T) {
	// isBreakout=true with Neutral bias: blocked for ANY tradeDir.
	b := &BiasTracker{current: models.Neutral}
	for _, dir := range []models.Direction{models.Long, models.Short, models.Neutral} {
		if b.Allows(dir, true) {
			t.Errorf("Allows(dir=%v, breakout=true, bias=Neutral) = true, want false", dir)
		}
	}
}

func TestBiasTracker_Allows_BreakoutMatchingBias(t *testing.T) {
	// Breakout aligned with bias is allowed.
	bLong := &BiasTracker{current: models.Long}
	if !bLong.Allows(models.Long, true) {
		t.Error("Allows(Long, breakout=true, bias=Long) = false, want true")
	}
	bShort := &BiasTracker{current: models.Short}
	if !bShort.Allows(models.Short, true) {
		t.Error("Allows(Short, breakout=true, bias=Short) = false, want true")
	}
}

func TestBiasTracker_Allows_BreakoutMismatchedBias(t *testing.T) {
	// Breakout opposing bias is blocked.
	bLong := &BiasTracker{current: models.Long}
	if bLong.Allows(models.Short, true) {
		t.Error("Allows(Short, breakout=true, bias=Long) = true, want false")
	}
	bShort := &BiasTracker{current: models.Short}
	if bShort.Allows(models.Long, true) {
		t.Error("Allows(Long, breakout=true, bias=Short) = true, want false")
	}
}
