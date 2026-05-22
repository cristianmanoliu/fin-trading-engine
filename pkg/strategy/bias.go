package strategy

import "github.com/cristianmanoliu/fin-trading-engine/pkg/models"

// BiasTracker determines macro directional bias from 4H candles.
// Bias gates breakout entries: only signals aligned with bias are emitted.
// Absorption (reversal) entries are allowed regardless of bias.
type BiasTracker struct {
	current  models.Direction
	prevClose float64
}

// Update recalculates bias from a newly closed 4H candle.
func (b *BiasTracker) Update(c models.Candle) {
	bullish := c.Close > c.Open && (b.prevClose == 0 || c.Close > b.prevClose)
	bearish := c.Close < c.Open && (b.prevClose == 0 || c.Close < b.prevClose)

	switch {
	case bullish:
		b.current = models.Long
	case bearish:
		b.current = models.Short
	default:
		b.current = models.Neutral
	}

	b.prevClose = c.Close
}

// Direction returns the current macro bias.
func (b *BiasTracker) Direction() models.Direction {
	return b.current
}

// Allows reports whether a trade in the given direction is consistent with macro bias.
// Breakout trades are filtered; absorption trades pass through unconditionally.
func (b *BiasTracker) Allows(tradeDir models.Direction, isBreakout bool) bool {
	if !isBreakout {
		return true // absorption reversal: always allow
	}
	if b.current == models.Neutral {
		return false // no breakout trades when bias is unclear
	}
	return b.current == tradeDir
}
