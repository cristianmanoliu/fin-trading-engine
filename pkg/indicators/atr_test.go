package indicators

import (
	"math"
	"testing"

	"github.com/cristianmanoliu/trading-engine/pkg/models"
)

func TestATR_PrimingAndSmoothing(t *testing.T) {
	atr := NewATR(3)

	// Hand-computed example.
	// Bar 1: H=10 L=8  C=9                  TR1 = H-L = 2
	// Bar 2: H=12 L=9  C=11   prev=9        TR2 = max(3, |12-9|, |9-9|) = 3
	// Bar 3: H=11 L=7  C=10   prev=11       TR3 = max(4, |11-11|, |7-11|) = 4
	// SMA prime: ATR3 = (2+3+4)/3 = 3
	// Bar 4: H=13 L=10 C=12   prev=10       TR4 = max(3, |13-10|, |10-10|) = 3
	// Wilder: ATR4 = (3*2 + 3)/3 = 3
	// Bar 5: H=15 L=11 C=14   prev=12       TR5 = max(4, |15-12|, |11-12|) = 4
	// Wilder: ATR5 = (3*2 + 4)/3 ≈ 3.333

	bars := []struct {
		h, l, c float64
	}{
		{10, 8, 9},
		{12, 9, 11},
		{11, 7, 10},
		{13, 10, 12},
		{15, 11, 14},
	}

	wantValues := []float64{0, 0, 3.0, 3.0, 10.0 / 3.0}
	wantPrimed := []bool{false, false, true, true, true}

	for i, b := range bars {
		atr.Update(models.Candle{High: b.h, Low: b.l, Close: b.c})
		if atr.Primed() != wantPrimed[i] {
			t.Errorf("bar %d: primed=%v want %v", i+1, atr.Primed(), wantPrimed[i])
		}
		if math.Abs(atr.Value()-wantValues[i]) > 1e-9 {
			t.Errorf("bar %d: ATR=%v want %v", i+1, atr.Value(), wantValues[i])
		}
	}
}

func TestATR_FirstBarUsesHighLow(t *testing.T) {
	atr := NewATR(1)
	atr.Update(models.Candle{High: 100, Low: 90, Close: 95})
	if !atr.Primed() {
		t.Fatal("ATR(1) should be primed after one bar")
	}
	if atr.Value() != 10 {
		t.Errorf("ATR=%v want 10 (high-low on first bar)", atr.Value())
	}
}
