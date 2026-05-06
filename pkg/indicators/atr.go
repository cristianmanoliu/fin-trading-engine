package indicators

import (
	"math"

	"github.com/cristianmanoliu/trading-engine/pkg/models"
)

// ATR computes the Average True Range over a fixed period using Wilder's smoothing.
// True range for each bar:
//
//	TR = max(high-low, |high-prevClose|, |low-prevClose|)
//
// First `period` TRs are averaged (SMA priming), then Wilder's smoothing applies:
//
//	ATR_t = (ATR_{t-1} * (period-1) + TR_t) / period
//
// Not goroutine-safe — owned exclusively by the strategy runner goroutine.
type ATR struct {
	period int

	// SMA priming buffer; nil after the indicator is primed.
	trBuffer []float64

	value     float64
	primed    bool
	prevClose float64
	havePrev  bool
}

// NewATR creates an ATR with the given period.
func NewATR(period int) *ATR {
	return &ATR{period: period}
}

// Update incorporates a newly closed candle.
func (a *ATR) Update(c models.Candle) {
	var tr float64
	if !a.havePrev {
		tr = c.High - c.Low
		a.havePrev = true
	} else {
		hl := c.High - c.Low
		hpc := math.Abs(c.High - a.prevClose)
		lpc := math.Abs(c.Low - a.prevClose)
		tr = math.Max(hl, math.Max(hpc, lpc))
	}

	if !a.primed {
		a.trBuffer = append(a.trBuffer, tr)
		if len(a.trBuffer) >= a.period {
			sum := 0.0
			for _, x := range a.trBuffer {
				sum += x
			}
			a.value = sum / float64(a.period)
			a.primed = true
			a.trBuffer = nil
		}
	} else {
		a.value = (a.value*float64(a.period-1) + tr) / float64(a.period)
	}

	a.prevClose = c.Close
}

// Value returns the current ATR, or 0 if not yet primed.
func (a *ATR) Value() float64 {
	if !a.primed {
		return 0
	}
	return a.value
}

// Primed reports whether the ATR has enough samples to be meaningful.
func (a *ATR) Primed() bool {
	return a.primed
}
