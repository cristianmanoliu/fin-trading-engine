package marketdata

import (
	"time"

	"github.com/cristianmanoliu/fin-trading-engine/pkg/models"
)

// expandKlineToTicks converts a single 1m kline into 4 synthetic ticks:
// open → high(33%) → low(66%) → close(100%) with timestamps interpolated
// across the kline window and volume split equally.
// Used by both CSVReplay and the REST backfill path so behaviour is consistent.
func expandKlineToTicks(openMs, closeMs int64, o, h, l, c, v float64, symbol string) []models.Tick {
	openTime := time.UnixMilli(openMs).UTC()
	closeTime := time.UnixMilli(closeMs).UTC()
	duration := closeTime.Sub(openTime)
	tickVol := v / 4

	points := [4]struct {
		price float64
		frac  float64
	}{
		{o, 0.0},
		{h, 0.33},
		{l, 0.66},
		{c, 1.0},
	}

	ticks := make([]models.Tick, 4)
	for i, p := range points {
		ticks[i] = models.Tick{
			Symbol:    symbol,
			Timestamp: openTime.Add(time.Duration(float64(duration) * p.frac)),
			Price:     p.price,
			Volume:    tickVol,
		}
	}
	return ticks
}
