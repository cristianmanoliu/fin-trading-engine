package aggregator

import (
	"context"
	"time"

	"github.com/cristianmanoliu/fin-trading-engine/pkg/models"
)

// timeframeDuration maps each supported timeframe to its duration.
var timeframeDuration = map[models.Timeframe]time.Duration{
	models.Timeframe5m:  5 * time.Minute,
	models.Timeframe30m: 30 * time.Minute,
	models.Timeframe1H:  1 * time.Hour,
	models.Timeframe2H:  2 * time.Hour,
	models.Timeframe4H:  4 * time.Hour,
	models.Timeframe1D:  24 * time.Hour,
}

// Aggregator consumes a tick stream and emits closed candles on per-timeframe channels.
// A single goroutine owns all state — no mutexes required.
type Aggregator struct {
	ticks   <-chan models.Tick
	candles map[models.Timeframe]chan models.Candle
	open    map[models.Timeframe]*models.Candle

	// IncludeBoundaryTick: when true, the boundary tick (timestamp >= CloseTime) is folded
	// into the closing candle's OHLCV before being used to open the next candle. Default
	// behavior excludes the boundary tick from the closing candle, which under synthetic
	// 4-tick-per-1m-kline expansion makes the 5m Close field equal to the LOW tick of the
	// last 1m subkline (rather than the actual close). Used for backtest-vs-live divergence
	// audits; do not enable in live mode where the live tick stream has its own close-tick
	// semantics.
	IncludeBoundaryTick bool
}

// New creates an Aggregator. Call Run to start it.
func New(ticks <-chan models.Tick) *Aggregator {
	candles := make(map[models.Timeframe]chan models.Candle, len(models.Timeframes))
	for _, tf := range models.Timeframes {
		candles[tf] = make(chan models.Candle, 64)
	}
	return &Aggregator{
		ticks:   ticks,
		candles: candles,
		open:    make(map[models.Timeframe]*models.Candle),
	}
}

// Chan5m returns the channel of closed 5-minute candles.
func (a *Aggregator) Chan5m() <-chan models.Candle { return a.candles[models.Timeframe5m] }

// Chan30m returns the channel of closed 30-minute candles.
func (a *Aggregator) Chan30m() <-chan models.Candle { return a.candles[models.Timeframe30m] }

// Chan1H returns the channel of closed 1-hour candles.
func (a *Aggregator) Chan1H() <-chan models.Candle { return a.candles[models.Timeframe1H] }

// Chan2H returns the channel of closed 2-hour candles.
func (a *Aggregator) Chan2H() <-chan models.Candle { return a.candles[models.Timeframe2H] }

// Chan4H returns the channel of closed 4-hour candles.
func (a *Aggregator) Chan4H() <-chan models.Candle { return a.candles[models.Timeframe4H] }

// Chan1D returns the channel of closed 1-day candles.
func (a *Aggregator) Chan1D() <-chan models.Candle { return a.candles[models.Timeframe1D] }

// Run processes ticks until ctx is cancelled or the tick channel is closed.
// It must be called in its own goroutine.
func (a *Aggregator) Run(ctx context.Context) {
	defer func() {
		for _, ch := range a.candles {
			close(ch)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case tick, ok := <-a.ticks:
			if !ok {
				return
			}
			for _, tf := range models.Timeframes {
				if c := a.update(tf, tick); c != nil {
					a.candles[tf] <- *c
				}
			}
		}
	}
}

// ProcessTick aggregates one tick synchronously and returns any candles that closed.
// Used by the deterministic backtest pipeline — never call Run when using this.
func (a *Aggregator) ProcessTick(tick models.Tick) []models.Candle {
	var closed []models.Candle
	for _, tf := range models.Timeframes {
		if c := a.update(tf, tick); c != nil {
			closed = append(closed, *c)
		}
	}
	return closed
}

// update returns the closed candle (if the tick completed one), or nil.
func (a *Aggregator) update(tf models.Timeframe, tick models.Tick) *models.Candle {
	dur := timeframeDuration[tf]
	current := a.open[tf]

	if current == nil {
		a.open[tf] = openCandle(tf, tick, dur)
		return nil
	}

	// Close and emit if this tick falls at or past the candle's close time.
	if !tick.Timestamp.Before(current.CloseTime) {
		if a.IncludeBoundaryTick {
			if tick.Price > current.High {
				current.High = tick.Price
			}
			if tick.Price < current.Low {
				current.Low = tick.Price
			}
			current.Close = tick.Price
			current.Volume += tick.Volume
		}
		current.Closed = true
		closed := *current
		a.open[tf] = openCandle(tf, tick, dur)
		return &closed
	}

	// Update OHLCV of the open candle.
	if tick.Price > current.High {
		current.High = tick.Price
	}
	if tick.Price < current.Low {
		current.Low = tick.Price
	}
	current.Close = tick.Price
	current.Volume += tick.Volume
	return nil
}

// openCandle creates a new open candle aligned to the timeframe boundary.
func openCandle(tf models.Timeframe, tick models.Tick, dur time.Duration) *models.Candle {
	// Align open time to the timeframe boundary using exchange timestamp.
	openTime := tick.Timestamp.Truncate(dur)
	closeTime := openTime.Add(dur)
	return &models.Candle{
		Symbol:    tick.Symbol,
		Timeframe: tf,
		OpenTime:  openTime,
		CloseTime: closeTime,
		Open:      tick.Price,
		High:      tick.Price,
		Low:       tick.Price,
		Close:     tick.Price,
		Volume:    tick.Volume,
	}
}
