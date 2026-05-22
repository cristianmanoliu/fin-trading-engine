package indicators

import (
	"time"

	"github.com/cristianmanoliu/fin-trading-engine/pkg/models"
)

// VWAP calculates the session Volume-Weighted Average Price.
// It resets at 00:00 UTC daily.
// Not goroutine-safe — owned exclusively by the StrategyRunner goroutine.
type VWAP struct {
	cumPV    float64 // Σ(price × volume)
	cumVol   float64 // Σ(volume)
	sessionDay int   // UTC day of the current session
}

// Update incorporates a new tick into the VWAP calculation.
func (v *VWAP) Update(tick models.Tick) {
	day := julianDay(tick.Timestamp)
	if v.sessionDay != day {
		v.cumPV = 0
		v.cumVol = 0
		v.sessionDay = day
	}
	v.cumPV += tick.Price * tick.Volume
	v.cumVol += tick.Volume
}

// Value returns the current VWAP. Returns 0 if no ticks have been processed.
func (v *VWAP) Value() float64 {
	if v.cumVol == 0 {
		return 0
	}
	return v.cumPV / v.cumVol
}

// julianDay returns a monotonically increasing day number for a UTC time,
// used to detect session boundaries without string formatting.
func julianDay(t time.Time) int {
	t = t.UTC()
	return t.Year()*1000 + t.YearDay()
}
