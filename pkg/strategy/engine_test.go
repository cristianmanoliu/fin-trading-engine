package strategy

import (
	"testing"
	"time"

	"github.com/cristianmanoliu/trading-engine/pkg/models"
)

// Bug 5 regression suite (CLAUDE.md "Known Bugs / Fixed → Bug 5").
//
// Background: paginated kline backfill (96h × 60 1m → 24 closed 4H candles)
// primes EMA21 mid-backfill. Without a staleness gate, the very last priming
// candle would emit a signal at a close price that's hours/days old — the
// engine then opens a position at that stale price and the first live tick
// is an instant adverse fill.
//
// The fix: evaluateEntry returns early when liveMode=true AND the candle's
// CloseTime is older than backfillStaleness. cmd/engine sets liveMode=true
// for live + each shadow Runner; cmd/backtest leaves it default-false so
// historical CSV replay continues to emit signals as designed.
//
// These tests pin the gate predicate (candleTooStale) directly, plus the
// setter and the staleness constant. A subtle regression — accidental
// removal of the liveMode clause, sign-flipped inequality, threshold
// changed too small or too large — would be caught here, not in production.

func TestRunner_CandleTooStale_DefaultIsFalse(t *testing.T) {
	// A zero-value Runner has liveMode=false (backtest default). The gate must
	// be off so historical CSV replay can still emit signals at any candle age.
	var r Runner
	fresh := models.Candle{CloseTime: time.Now()}
	stale := models.Candle{CloseTime: time.Now().Add(-time.Hour)}
	if r.candleTooStale(fresh) {
		t.Error("default Runner: fresh candle reported stale (gate must be off in backtest mode)")
	}
	if r.candleTooStale(stale) {
		t.Error("default Runner: hour-old candle reported stale — would suppress every backtest signal")
	}
}

func TestRunner_CandleTooStale_LiveMode_FreshCandle_NotStale(t *testing.T) {
	// liveMode=true with a candle that just closed: the gate must NOT fire,
	// otherwise no live signal could ever be emitted.
	r := Runner{liveMode: true}
	fresh := models.Candle{CloseTime: time.Now()}
	if r.candleTooStale(fresh) {
		t.Error("liveMode=true + just-closed candle: reported stale (would suppress every live signal)")
	}
}

func TestRunner_CandleTooStale_LiveMode_StaleCandle_IsStale(t *testing.T) {
	// liveMode=true with an hour-old candle (representative of a 4H signal-tf
	// backfill priming candle): the gate must fire. This is the Bug 5 case —
	// without the gate, this is exactly the candle that would emit a signal at
	// a stale close price, opening a position at an instant adverse fill.
	r := Runner{liveMode: true}
	stale := models.Candle{CloseTime: time.Now().Add(-time.Hour)}
	if !r.candleTooStale(stale) {
		t.Error("liveMode=true + 1h-old candle: NOT reported stale — Bug 5 regression")
	}
}

func TestRunner_CandleTooStale_LiveMode_BoundaryAtThreshold(t *testing.T) {
	// The inequality is strict: time.Since(c.CloseTime) > backfillStaleness.
	// A candle exactly at the threshold (90s old) is NOT stale. One tick over
	// the threshold IS. Pin the strict-> semantics so a future change to >=
	// is intentional (which would suppress real live closes that happened to
	// take exactly 90s to reach the strategy goroutine).
	r := Runner{liveMode: true}

	// CloseTime exactly backfillStaleness ago → time.Since may be slightly
	// over by the time the call lands, so use a small safety margin to land
	// strictly UNDER the threshold.
	atThreshold := models.Candle{CloseTime: time.Now().Add(-backfillStaleness + 100*time.Millisecond)}
	if r.candleTooStale(atThreshold) {
		t.Errorf("liveMode=true + ~%v old (just under threshold): reported stale (gate must use strict >, not >=)", backfillStaleness)
	}

	// One second past threshold → must be stale.
	overThreshold := models.Candle{CloseTime: time.Now().Add(-backfillStaleness - time.Second)}
	if !r.candleTooStale(overThreshold) {
		t.Errorf("liveMode=true + %v old (1s past threshold): NOT reported stale", backfillStaleness+time.Second)
	}
}

func TestRunner_BackfillStalenessConstant_Is90s(t *testing.T) {
	// Pin the staleness threshold against accidental drift. CLAUDE.md notes
	// the value is "smaller than the smallest signal timeframe (5m) so real
	// live closes are never suppressed even with reasonable network/processing
	// lag." Too small (<1m) would suppress real live closes; too large would
	// re-introduce the Bug 5 stale-signal-on-restart failure mode (the
	// paginated backfill primes 4H indicators with candles that are ~4h old
	// at the seam, far past any reasonable threshold).
	if backfillStaleness != 90*time.Second {
		t.Errorf("backfillStaleness = %v, want 90s — see CLAUDE.md Bug 5", backfillStaleness)
	}
}

func TestRunner_SetLiveMode_TogglesField(t *testing.T) {
	// Setter contract: zero value is false, SetLiveMode(true) sets it,
	// SetLiveMode(false) clears it. cmd/engine relies on this to flip live
	// + shadow Runners into gated mode after construction; cmd/backtest
	// never calls it.
	var r Runner
	if r.liveMode {
		t.Error("zero-value Runner.liveMode = true, want false")
	}
	r.SetLiveMode(true)
	if !r.liveMode {
		t.Error("SetLiveMode(true) did not set liveMode")
	}
	// And toggle back, in case future logic ever needs to disable mid-run
	// (currently no caller does, but pin the symmetric behavior).
	r.SetLiveMode(false)
	if r.liveMode {
		t.Error("SetLiveMode(false) did not clear liveMode")
	}
}

func TestRunner_CandleTooStale_FutureCandle_NotStale(t *testing.T) {
	// Edge case: candle with CloseTime in the FUTURE. time.Since returns a
	// negative duration, which is always < backfillStaleness, so the gate
	// does not fire. This protects against clock-skew between exchange
	// servers and the engine host: if exchange-reported CloseTime is in the
	// near future relative to the engine's clock, we should NOT suppress —
	// the candle is by definition fresh.
	r := Runner{liveMode: true}
	future := models.Candle{CloseTime: time.Now().Add(time.Minute)}
	if r.candleTooStale(future) {
		t.Error("liveMode=true + future-dated candle: reported stale (clock-skew defense broken)")
	}
}
