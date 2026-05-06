package indicators

import "math"

// Bollinger computes Bollinger Bands: SMA(period) ± stdMult × stdev(period).
// Standard parameters: period=20, stdMult=2.0.
//
// Trading interpretation (breakdown variant — the one used by our strategy
// framework as a momentum trigger, NOT mean-reversion):
//   - Close > Upper band: bullish breakout
//   - Close < Lower band: bearish breakdown (short signal)
//
// Not goroutine-safe — owned exclusively by the strategy runner goroutine.
// Lifecycle: Primed() once `period` samples observed.
type Bollinger struct {
	period  int
	stdMult float64
	values  []float64 // rolling window of last `period` values
	primed  bool
}

// NewBollinger creates a Bollinger band calculator. Defaults: period=20, stdMult=2.0.
func NewBollinger(period int, stdMult float64) *Bollinger {
	if period <= 1 {
		period = 20
	}
	if stdMult <= 0 {
		stdMult = 2.0
	}
	return &Bollinger{
		period:  period,
		stdMult: stdMult,
		values:  make([]float64, 0, period),
	}
}

// Update adds a new price sample. Maintains a sliding window of size period.
func (b *Bollinger) Update(price float64) {
	b.values = append(b.values, price)
	if len(b.values) > b.period {
		b.values = b.values[len(b.values)-b.period:]
	}
	if len(b.values) >= b.period {
		b.primed = true
	}
}

// Value returns (lower, middle, upper) bands. Returns (0,0,0) if not primed.
// middle = SMA, upper = middle + stdMult*stdev, lower = middle - stdMult*stdev.
func (b *Bollinger) Value() (lower, middle, upper float64) {
	if !b.primed {
		return 0, 0, 0
	}
	// SMA
	var sum float64
	for _, v := range b.values {
		sum += v
	}
	middle = sum / float64(len(b.values))
	// Stdev (population, divisor N — matches typical TA convention)
	var sqDiff float64
	for _, v := range b.values {
		d := v - middle
		sqDiff += d * d
	}
	stdev := math.Sqrt(sqDiff / float64(len(b.values)))
	upper = middle + b.stdMult*stdev
	lower = middle - b.stdMult*stdev
	return lower, middle, upper
}

// Primed reports whether enough samples have been observed.
func (b *Bollinger) Primed() bool {
	return b.primed
}
