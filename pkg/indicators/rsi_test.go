package indicators

import (
	"math"
	"testing"
)

func TestRSI_NotPrimedInitially(t *testing.T) {
	r := NewRSI(14)
	r.Update(100)
	r.Update(101)
	if r.Primed() {
		t.Error("RSI primed too early with only 2 samples (need period+1=15)")
	}
	if r.Value() != 0 {
		t.Errorf("Value before primed should be 0, got %v", r.Value())
	}
}

func TestRSI_PrimingThreshold(t *testing.T) {
	r := NewRSI(14)
	// Need 15 samples to prime (1 to set prevPrice, 14 deltas)
	for i := 0; i < 14; i++ {
		r.Update(100 + float64(i)) // pure uptrend
	}
	if r.Primed() {
		t.Errorf("primed at sample 14, expected to need 15")
	}
	r.Update(115) // 15th sample
	if !r.Primed() {
		t.Errorf("not primed at sample 15")
	}
}

func TestRSI_ConstantUptrendIs100(t *testing.T) {
	r := NewRSI(14)
	for i := 0; i < 20; i++ {
		r.Update(100 + float64(i))
	}
	v := r.Value()
	if math.Abs(v-100) > 0.01 {
		t.Errorf("constant uptrend RSI: want 100, got %v", v)
	}
}

func TestRSI_ConstantDowntrendIsZero(t *testing.T) {
	r := NewRSI(14)
	for i := 0; i < 20; i++ {
		r.Update(100 - float64(i))
	}
	v := r.Value()
	if math.Abs(v) > 0.01 {
		t.Errorf("constant downtrend RSI: want 0, got %v", v)
	}
}

func TestRSI_AlternatingPriceIs50ish(t *testing.T) {
	// Alternating +1/-1 should average to ~50 (equal gains and losses)
	r := NewRSI(14)
	for i := 0; i < 30; i++ {
		if i%2 == 0 {
			r.Update(100)
		} else {
			r.Update(101)
		}
	}
	v := r.Value()
	// Tolerate a wide band — alternating pattern with Wilder smoothing
	// drifts away from exactly 50 but stays in the middle range.
	if v < 30 || v > 70 {
		t.Errorf("alternating RSI: want ~50 (30-70), got %v", v)
	}
}

func TestRSI_ZeroPeriodDefaultsTo14(t *testing.T) {
	r := NewRSI(0)
	if r.period != 14 {
		t.Errorf("period 0 should default to 14, got %v", r.period)
	}
}
