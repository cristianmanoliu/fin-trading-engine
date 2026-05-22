package indicators

import (
	"github.com/cristianmanoliu/fin-trading-engine/pkg/models"
)

// DailyLevels tracks the Previous Day High (PDH) and Previous Day Low (PDL),
// and maintains the current day's running high/low so they can be rolled at midnight.
// Not goroutine-safe — owned exclusively by the StrategyRunner goroutine.
type DailyLevels struct {
	PDH float64
	PDL float64

	currentHigh float64
	currentLow  float64
	sessionDay  int
	initialized bool
}

// Update processes a tick, rolling PDH/PDL when a new UTC day starts.
func (d *DailyLevels) Update(tick models.Tick) {
	day := julianDay(tick.Timestamp)

	if !d.initialized {
		d.currentHigh = tick.Price
		d.currentLow = tick.Price
		d.sessionDay = day
		d.initialized = true
		return
	}

	if day != d.sessionDay {
		// Roll current day into previous.
		d.PDH = d.currentHigh
		d.PDL = d.currentLow
		d.currentHigh = tick.Price
		d.currentLow = tick.Price
		d.sessionDay = day
		return
	}

	if tick.Price > d.currentHigh {
		d.currentHigh = tick.Price
	}
	if tick.Price < d.currentLow {
		d.currentLow = tick.Price
	}
}

// KeyLevels returns the active key price levels: PDH, PDL, and any zone edges.
// Returns nil if PDH/PDL have not yet been established (less than one full day of data).
func (d *DailyLevels) KeyLevels(zones []models.Zone) []float64 {
	var levels []float64
	if d.PDH > 0 {
		levels = append(levels, d.PDH)
	}
	if d.PDL > 0 {
		levels = append(levels, d.PDL)
	}
	for _, z := range zones {
		levels = append(levels, z.Low, z.High)
	}
	return levels
}

// HasData reports whether at least one full day of data has been seen
// (i.e., PDH/PDL are populated).
func (d *DailyLevels) HasData() bool {
	return d.PDH > 0 && d.PDL > 0
}

