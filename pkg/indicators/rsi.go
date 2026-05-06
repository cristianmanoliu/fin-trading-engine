package indicators

import "math"

// RSI is the Relative Strength Index indicator (Wilder, 1978).
// Computes the ratio of average gains to average losses over the trailing
// `period` candles, expressed as a 0-100 oscillator.
//
// Standard interpretation:
//   - RSI > 70: overbought (potential reversal down)
//   - RSI < 30: oversold (potential reversal up)
//   - RSI crossing 50: momentum direction shift
//
// Not goroutine-safe — owned exclusively by the strategy runner goroutine.
// Mirrors the EMA struct's lifecycle: Primed() reports readiness; Value()
// returns 0 until enough data has been observed.
type RSI struct {
	period   int
	prevPrice float64
	avgGain  float64 // Wilder-smoothed average gain
	avgLoss  float64 // Wilder-smoothed average loss
	count    int
	primed   bool
}

// NewRSI creates an RSI with the given period (typically 14).
func NewRSI(period int) *RSI {
	if period < 2 {
		period = 14
	}
	return &RSI{period: period}
}

// Update adds a new price sample. The RSI primes after `period+1` samples
// (one extra to compute the first delta) using simple averaging, then
// transitions to Wilder's exponential smoothing for subsequent updates.
func (r *RSI) Update(price float64) {
	if r.count == 0 {
		r.prevPrice = price
		r.count++
		return
	}
	delta := price - r.prevPrice
	r.prevPrice = price
	gain := math.Max(delta, 0)
	loss := math.Max(-delta, 0)

	if !r.primed {
		// Accumulate first `period` deltas as a simple sum, then transition.
		r.avgGain += gain
		r.avgLoss += loss
		r.count++
		if r.count >= r.period+1 {
			r.avgGain /= float64(r.period)
			r.avgLoss /= float64(r.period)
			r.primed = true
		}
		return
	}
	// Wilder smoothing: EMA-like with k = 1/period
	w := 1.0 / float64(r.period)
	r.avgGain = gain*w + r.avgGain*(1-w)
	r.avgLoss = loss*w + r.avgLoss*(1-w)
}

// Value returns the current RSI in [0, 100], or 0 if not yet primed.
// Returns 100 when avgLoss is zero (all gains; max bullishness).
func (r *RSI) Value() float64 {
	if !r.primed {
		return 0
	}
	if r.avgLoss == 0 {
		return 100
	}
	rs := r.avgGain / r.avgLoss
	return 100 - (100 / (1 + rs))
}

// Primed reports whether the RSI has accumulated enough data to produce a value.
func (r *RSI) Primed() bool {
	return r.primed
}
