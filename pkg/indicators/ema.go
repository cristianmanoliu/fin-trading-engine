package indicators

// EMA is an exponential moving average calculator.
// Not goroutine-safe — owned exclusively by the strategy runner goroutine.
type EMA struct {
	period  int
	k       float64 // smoothing factor = 2/(period+1)
	value   float64
	count   int
	primed  bool
}

// NewEMA creates an EMA with the given period.
func NewEMA(period int) *EMA {
	return &EMA{
		period: period,
		k:      2.0 / float64(period+1),
	}
}

// Update adds a new price sample and updates the EMA.
func (e *EMA) Update(price float64) {
	if !e.primed {
		e.count++
		e.value += price
		if e.count == e.period {
			e.value /= float64(e.period)
			e.primed = true
		}
		return
	}
	e.value = price*e.k + e.value*(1-e.k)
}

// Value returns the current EMA, or 0 if not yet primed.
func (e *EMA) Value() float64 {
	if !e.primed {
		return 0
	}
	return e.value
}

// Primed returns true once the EMA has enough data points.
func (e *EMA) Primed() bool {
	return e.primed
}
