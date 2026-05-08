package indicators

import (
	"math"
	"testing"
)

// EMA priming uses the standard SMA-seed pattern: accumulate the first `period`
// samples, divide by `period` once at the boundary to produce the seed, then
// switch to the canonical EMA formula `value = price*k + value*(1-k)` where
// `k = 2/(period+1)`. These tests pin both the priming boundary and the
// post-prime arithmetic against hand-computed reference values, so a future
// refactor that subtly changes either path (off-by-one in the priming count,
// wrong k, divide-by-period+1, etc.) cannot land silently — every backtest
// claim transitively depends on EMA being numerically correct.

const emaTol = 1e-9

func approxEMA(a, b, tol float64) bool {
	return math.Abs(a-b) <= tol
}

func TestEMA_NotPrimedBeforePeriod(t *testing.T) {
	ema := NewEMA(10)
	for i := 0; i < 9; i++ {
		ema.Update(float64(i + 1))
		if ema.Primed() {
			t.Fatalf("primed too early at sample %d/%d", i+1, 10)
		}
		if ema.Value() != 0 {
			t.Errorf("sample %d: Value()=%v before primed, want 0", i+1, ema.Value())
		}
	}
}

func TestEMA_PrimedExactlyAtPeriod(t *testing.T) {
	ema := NewEMA(5)
	for i := 0; i < 4; i++ {
		ema.Update(float64(i + 1))
	}
	if ema.Primed() {
		t.Fatalf("primed at sample 4/5 — should require 5 samples")
	}
	ema.Update(5)
	if !ema.Primed() {
		t.Fatalf("not primed at sample 5/5 — boundary off-by-one")
	}
}

func TestEMA_SeedEqualsSMA(t *testing.T) {
	// Seed at sample period == SMA of first `period` samples.
	// EMA(10) over [1..10] → seed = (1+2+...+10)/10 = 55/10 = 5.5
	ema := NewEMA(10)
	for i := 1; i <= 10; i++ {
		ema.Update(float64(i))
	}
	want := 5.5
	if !approxEMA(ema.Value(), want, emaTol) {
		t.Errorf("seed value = %v, want %v (SMA of 1..10)", ema.Value(), want)
	}
}

func TestEMA_PostPrimeFormula(t *testing.T) {
	// After seeding, next update applies the EMA formula.
	// EMA(10): k = 2/11, seed = 5.5, next price = 11.
	// Expected: 11*(2/11) + 5.5*(9/11) = 2.0 + 4.5 = 6.5
	ema := NewEMA(10)
	for i := 1; i <= 10; i++ {
		ema.Update(float64(i))
	}
	ema.Update(11)
	want := 6.5
	if !approxEMA(ema.Value(), want, emaTol) {
		t.Errorf("post-prime value = %v, want %v", ema.Value(), want)
	}
}

func TestEMA_SmoothingFactor_K(t *testing.T) {
	// k = 2/(period+1). Verify by checking the post-prime delta.
	// EMA(4): k = 2/5 = 0.4. Seed = (1+2+3+4)/4 = 2.5. Update 5 → 5*0.4 + 2.5*0.6 = 3.5
	ema := NewEMA(4)
	for i := 1; i <= 4; i++ {
		ema.Update(float64(i))
	}
	ema.Update(5)
	want := 3.5
	if !approxEMA(ema.Value(), want, emaTol) {
		t.Errorf("k smoothing: got %v, want %v (k should be 2/5=0.4 for period=4)", ema.Value(), want)
	}
}

func TestEMA_LongRunMatchesReference(t *testing.T) {
	// EMA(3) over [10, 20, 30, 40, 50]:
	//   sample 1 (10): not primed, sum=10
	//   sample 2 (20): not primed, sum=30
	//   sample 3 (30): primed, seed = 60/3 = 20
	//   sample 4 (40): k=2/4=0.5, value = 40*0.5 + 20*0.5 = 30
	//   sample 5 (50): value = 50*0.5 + 30*0.5 = 40
	ema := NewEMA(3)
	prices := []float64{10, 20, 30, 40, 50}
	wantValues := []float64{0, 0, 20, 30, 40}
	wantPrimed := []bool{false, false, true, true, true}
	for i, p := range prices {
		ema.Update(p)
		if ema.Primed() != wantPrimed[i] {
			t.Errorf("sample %d: primed=%v want %v", i+1, ema.Primed(), wantPrimed[i])
		}
		if !approxEMA(ema.Value(), wantValues[i], emaTol) {
			t.Errorf("sample %d: value=%v want %v", i+1, ema.Value(), wantValues[i])
		}
	}
}

func TestEMA_StableValueWithFlatInput(t *testing.T) {
	// Feeding a constant price converges to that price and stays exactly there:
	// p*k + p*(1-k) = p for any k. Sanity check that no numerical drift
	// accumulates over many updates.
	ema := NewEMA(5)
	const price = 100.0
	for i := 0; i < 50; i++ {
		ema.Update(price)
	}
	if !approxEMA(ema.Value(), price, emaTol) {
		t.Errorf("flat-input EMA = %v, want exactly %v", ema.Value(), price)
	}
}

func TestEMA_NegativeAndZeroValues(t *testing.T) {
	// EMA accepts any float; no input validation. Verify mixed signs work.
	// Seed = (-10 + 0 + 10) / 3 = 0
	ema := NewEMA(3)
	ema.Update(-10)
	ema.Update(0)
	ema.Update(10)
	want := 0.0
	if !approxEMA(ema.Value(), want, emaTol) {
		t.Errorf("mixed-sign seed: got %v, want %v", ema.Value(), want)
	}
}

func TestEMA_PrimedStaysTrueAfterMoreUpdates(t *testing.T) {
	// Once primed, Primed() never flips back to false.
	ema := NewEMA(2)
	ema.Update(1)
	ema.Update(2)
	if !ema.Primed() {
		t.Fatalf("not primed at sample 2/2")
	}
	for i := 0; i < 10; i++ {
		ema.Update(float64(i))
		if !ema.Primed() {
			t.Errorf("primed flipped to false after %d post-prime update(s)", i+1)
		}
	}
}

func TestEMA_Period1_DegenerateCase(t *testing.T) {
	// Period=1: k = 2/(1+1) = 1.0. Each update sets value = latest price exactly.
	// Seed at sample 1: sum=p, divide by 1 → value=p.
	// Each subsequent: p*1 + prev*0 = p.
	ema := NewEMA(1)
	prices := []float64{5, 10, 3, 7, 100}
	for i, p := range prices {
		ema.Update(p)
		if !ema.Primed() {
			t.Errorf("sample %d: period=1 should prime on first update", i+1)
		}
		if !approxEMA(ema.Value(), p, emaTol) {
			t.Errorf("sample %d: period=1 EMA = %v, want %v (k=1 means latest price wins)", i+1, ema.Value(), p)
		}
	}
}

func TestEMA_LiveConfigPeriod_NumericStability(t *testing.T) {
	// Live config uses EMA(9) and EMA(21). Pin behavior at period=21 across
	// a moderately long input sequence — the kind a 4H-signal-tf engine sees
	// over a few months — to catch any future drift in priming or formula.
	ema := NewEMA(21)
	// Walk price from 100 to 200 over 100 samples (linear ramp).
	for i := 0; i < 21; i++ {
		ema.Update(100 + float64(i)) // 100..120 inclusive
	}
	if !ema.Primed() {
		t.Fatalf("EMA(21) not primed at sample 21")
	}
	// Seed = mean of 100..120 = 110
	wantSeed := 110.0
	if !approxEMA(ema.Value(), wantSeed, emaTol) {
		t.Errorf("EMA(21) seed = %v, want %v", ema.Value(), wantSeed)
	}
	// Continue the ramp another 79 samples (price 121..199).
	for i := 21; i < 100; i++ {
		ema.Update(100 + float64(i))
	}
	// On a perfectly linear ramp, EMA lags the input by ~(period-1)/2.
	// Last input was 199. EMA should be below 199 but well above the seed.
	v := ema.Value()
	if v >= 199 || v <= 110 {
		t.Errorf("EMA(21) on linear ramp out of expected range: got %v, want strictly between 110 and 199", v)
	}
}
