package indicators

import (
	"math"
	"testing"
)

func TestMACD_NotPrimedInitially(t *testing.T) {
	m := NewMACD(12, 26, 9)
	for i := 0; i < 20; i++ {
		m.Update(100 + float64(i))
	}
	if m.Primed() {
		t.Errorf("MACD primed too early (slow=26 needs ≥26 samples)")
	}
}

func TestMACD_PrimesAfterFullWindow(t *testing.T) {
	m := NewMACD(12, 26, 9)
	// Need slow EMA primed (26 samples), then signal EMA primed (9 more after MACD line starts producing values)
	for i := 0; i < 50; i++ {
		m.Update(100 + float64(i))
	}
	if !m.Primed() {
		t.Errorf("MACD not primed after 50 samples (should be — slow=26, signal=9)")
	}
}

func TestMACD_SteadyTrendProducesPositiveMACD(t *testing.T) {
	// Persistent uptrend: fast EMA > slow EMA → MACD > 0
	m := NewMACD(12, 26, 9)
	for i := 0; i < 100; i++ {
		m.Update(100 + float64(i))
	}
	macd, signal := m.Value()
	if macd <= 0 {
		t.Errorf("uptrend MACD line should be positive, got %v", macd)
	}
	// Signal converges with MACD on a STEADY linear trend (the difference flattens).
	// Just verify both are positive and signal also tracks upward.
	if signal <= 0 {
		t.Errorf("uptrend signal line should be positive, got %v", signal)
	}
	_ = math.Abs // keep import in case test grows
}

func TestMACD_DefaultsApply(t *testing.T) {
	m := NewMACD(0, 0, 0)
	if m.emaFast == nil || m.emaSlow == nil || m.emaSignal == nil {
		t.Fatal("zero params should default; got nil EMA")
	}
	// Cannot directly verify period from outside, but Primed should still work after enough samples
	for i := 0; i < 50; i++ {
		m.Update(float64(i))
	}
	if !m.Primed() {
		t.Errorf("default-period MACD did not prime after 50 samples")
	}
}
