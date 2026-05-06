package indicators

import (
	"math"
	"testing"
)

func TestBollinger_NotPrimedInitially(t *testing.T) {
	b := NewBollinger(20, 2.0)
	for i := 0; i < 19; i++ {
		b.Update(float64(i))
	}
	if b.Primed() {
		t.Errorf("Bollinger primed at 19 samples (period=20)")
	}
	b.Update(20)
	if !b.Primed() {
		t.Errorf("Bollinger not primed at 20 samples")
	}
}

func TestBollinger_ConstantValueProducesZeroBandWidth(t *testing.T) {
	b := NewBollinger(20, 2.0)
	for i := 0; i < 30; i++ {
		b.Update(100)
	}
	lower, middle, upper := b.Value()
	if math.Abs(middle-100) > 0.001 {
		t.Errorf("middle band on constant input: want 100, got %v", middle)
	}
	if math.Abs(upper-lower) > 0.001 {
		t.Errorf("constant input → zero band width: got upper-lower = %v", upper-lower)
	}
}

func TestBollinger_BandsBracketTheMean(t *testing.T) {
	// Variable input — bands should always be: lower < middle < upper
	b := NewBollinger(20, 2.0)
	for i := 0; i < 30; i++ {
		b.Update(100 + float64(i%5)) // small variation
	}
	lower, middle, upper := b.Value()
	if !(lower < middle && middle < upper) {
		t.Errorf("bands not ordered: lower=%v middle=%v upper=%v", lower, middle, upper)
	}
	// Symmetry: upper-middle ≈ middle-lower
	if math.Abs((upper-middle)-(middle-lower)) > 0.001 {
		t.Errorf("bands not symmetric: upper-mid=%v mid-lower=%v",
			upper-middle, middle-lower)
	}
}

func TestBollinger_SlidingWindowDropsOldValues(t *testing.T) {
	// Confirm window slides — feeding 30 samples then checking value uses last 20
	b := NewBollinger(20, 2.0)
	// First 10: constant 100
	for i := 0; i < 10; i++ {
		b.Update(100)
	}
	// Next 20: constant 200 (these are the last 20 → window content)
	for i := 0; i < 20; i++ {
		b.Update(200)
	}
	_, middle, _ := b.Value()
	if math.Abs(middle-200) > 0.001 {
		t.Errorf("window did not slide: middle=%v want ~200", middle)
	}
}

func TestBollinger_DefaultsApply(t *testing.T) {
	b := NewBollinger(0, 0)
	if b.period != 20 {
		t.Errorf("zero period should default to 20, got %v", b.period)
	}
	if b.stdMult != 2.0 {
		t.Errorf("zero stdMult should default to 2.0, got %v", b.stdMult)
	}
}
