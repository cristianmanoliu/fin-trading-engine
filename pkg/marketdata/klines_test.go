package marketdata

import (
	"testing"
	"time"
)

func TestExpandKlineToTicks(t *testing.T) {
	openMs := int64(1700000000000)
	closeMs := int64(1700000060000) // 60s window
	ticks := expandKlineToTicks(openMs, closeMs, 100.0, 110.0, 90.0, 105.0, 4.0, "BTCUSDT")

	if len(ticks) != 4 {
		t.Fatalf("expected 4 ticks, got %d", len(ticks))
	}

	prices := []float64{100.0, 110.0, 90.0, 105.0}
	for i, tick := range ticks {
		if tick.Symbol != "BTCUSDT" {
			t.Errorf("tick %d: wrong symbol %q", i, tick.Symbol)
		}
		if tick.Price != prices[i] {
			t.Errorf("tick %d: expected price %.1f, got %.1f", i, prices[i], tick.Price)
		}
		if tick.Volume != 1.0 {
			t.Errorf("tick %d: expected volume 1.0, got %.2f", i, tick.Volume)
		}
	}

	// Timestamps must be monotonically non-decreasing.
	for i := 1; i < len(ticks); i++ {
		if ticks[i].Timestamp.Before(ticks[i-1].Timestamp) {
			t.Errorf("tick %d timestamp %v is before tick %d timestamp %v",
				i, ticks[i].Timestamp, i-1, ticks[i-1].Timestamp)
		}
	}

	// First tick at openMs, last tick at closeMs.
	expectedOpen := time.UnixMilli(openMs).UTC()
	expectedClose := time.UnixMilli(closeMs).UTC()
	if !ticks[0].Timestamp.Equal(expectedOpen) {
		t.Errorf("first tick: expected %v, got %v", expectedOpen, ticks[0].Timestamp)
	}
	if !ticks[3].Timestamp.Equal(expectedClose) {
		t.Errorf("last tick: expected %v, got %v", expectedClose, ticks[3].Timestamp)
	}
}
