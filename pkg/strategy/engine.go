package strategy

import (
	"context"
	"log/slog"
	"time"

	"github.com/cristianmanoliu/trading-engine/pkg/indicators"
	"github.com/cristianmanoliu/trading-engine/pkg/models"
)

// Executor receives trade signals and manages paper positions.
type Executor interface {
	OnSignal(sig *models.Signal)
	OnTick(tick models.Tick)
	Summary()
}

// Runner is the single-goroutine strategy event loop.
// It owns all mutable state: bias, VWAP, levels, entry detector.
// No mutexes are needed because only this goroutine reads/writes these fields.
type Runner struct {
	// inputs
	candle4H  <-chan models.Candle
	candle30m <-chan models.Candle
	candle5m  <-chan models.Candle
	candle1H  <-chan models.Candle
	candle2H  <-chan models.Candle
	candle1D  <-chan models.Candle
	ticks     <-chan models.Tick

	// state
	bias     *BiasTracker
	vwap     *indicators.VWAP
	levels   *indicators.DailyLevels
	detector *EntryDetector
	zones    []models.Zone

	// signalTF is the timeframe whose closed candles drive entry evaluation.
	// Default Timeframe5m (legacy). Setting to Timeframe30m or Timeframe4H feeds the
	// detector with slower candles, producing structurally wider stops.
	signalTF models.Timeframe

	// fundingFilter (optional) gates SHORT signals by funding regime — see
	// pkg/strategy/funding_filter.go. nil = disabled (default).
	fundingFilter *FundingFilter

	// liveMode gates evaluateEntry to suppress signals from candles whose CloseTime
	// is more than backfillStaleness in the past. Set true via SetLiveMode in
	// cmd/engine; cmd/backtest leaves it false so historical CSV replay produces signals.
	// Without this gate, paginated backfill (96h × 60 1m klines) would prime EMA21
	// mid-backfill and emit signals at stale historical close prices.
	liveMode bool

	// output
	executor Executor
}

// backfillStaleness is the age beyond which evaluateEntry treats a candle as
// historical (from REST kline backfill) rather than live in liveMode. Smaller
// than the smallest signal timeframe (5m) so real live closes are never
// suppressed even with reasonable network/processing lag.
const backfillStaleness = 90 * time.Second

// SetFundingFilter installs (or clears, if nil) a funding-regime filter on
// SHORT signals. Safe to call before Run starts. Concurrent use during Run is
// not supported (caller must set up before calling Run).
func (r *Runner) SetFundingFilter(f *FundingFilter) {
	r.fundingFilter = f
}

// SetLiveMode toggles the backfill-staleness gate in evaluateEntry. cmd/engine
// must call this with true before Run; cmd/backtest leaves it default-false so
// historical replay continues to emit signals. Safe to call before Run starts;
// concurrent use during Run is not supported.
func (r *Runner) SetLiveMode(live bool) {
	r.liveMode = live
}

// NewRunner wires up the strategy runner.
func NewRunner(
	candle4H, candle30m, candle5m <-chan models.Candle,
	candle1H, candle2H, candle1D <-chan models.Candle,
	ticks <-chan models.Tick,
	zones []models.Zone,
	cfg EntryConfig,
	executor Executor,
) *Runner {
	tf := cfg.SignalTimeframe
	if tf == "" {
		tf = models.Timeframe5m
	}
	return &Runner{
		candle4H:  candle4H,
		candle30m: candle30m,
		candle5m:  candle5m,
		candle1H:  candle1H,
		candle2H:  candle2H,
		candle1D:  candle1D,
		ticks:     ticks,
		bias:      &BiasTracker{},
		vwap:      &indicators.VWAP{},
		levels:    &indicators.DailyLevels{},
		detector:  NewEntryDetector(cfg),
		zones:     zones,
		signalTF:  tf,
		executor:  executor,
	}
}

// Run is the main event loop. Blocks until ctx is cancelled or all input channels close.
func (r *Runner) Run(ctx context.Context) {
	slog.Info("strategy runner started")

	open4H := true
	open30m := true
	open5m := true
	open1H := true
	open2H := true
	open1D := true
	openTicks := true

	for open4H || open30m || open5m || open1H || open2H || open1D || openTicks {
		select {
		case <-ctx.Done():
			return

		case c, ok := <-r.candle4H:
			if !ok {
				open4H = false
				r.candle4H = nil
				continue
			}
			r.bias.Update(c)
			slog.Info("4H candle closed",
				"open", c.Open, "close", c.Close,
				"bias", r.bias.Direction())
			if r.signalTF == models.Timeframe4H {
				r.detector.AddCandle(c)
				r.evaluateEntry(c)
			}

		case c, ok := <-r.candle30m:
			if !ok {
				open30m = false
				r.candle30m = nil
				continue
			}
			if r.signalTF == models.Timeframe30m {
				r.detector.AddCandle(c)
				r.evaluateEntry(c)
			} else {
				slog.Debug("30m candle closed",
					"open", c.Open, "high", c.High, "low", c.Low, "close", c.Close)
			}

		case c, ok := <-r.candle5m:
			if !ok {
				open5m = false
				r.candle5m = nil
				continue
			}
			if r.signalTF == models.Timeframe5m {
				r.detector.AddCandle(c)
				r.evaluateEntry(c)
			}

		case c, ok := <-r.candle1H:
			if !ok {
				open1H = false
				r.candle1H = nil
				continue
			}
			if r.signalTF == models.Timeframe1H {
				r.detector.AddCandle(c)
				r.evaluateEntry(c)
			}

		case c, ok := <-r.candle2H:
			if !ok {
				open2H = false
				r.candle2H = nil
				continue
			}
			if r.signalTF == models.Timeframe2H {
				r.detector.AddCandle(c)
				r.evaluateEntry(c)
			}

		case c, ok := <-r.candle1D:
			if !ok {
				open1D = false
				r.candle1D = nil
				continue
			}
			if r.signalTF == models.Timeframe1D {
				r.detector.AddCandle(c)
				r.evaluateEntry(c)
			}

		case tick, ok := <-r.ticks:
			if !ok {
				openTicks = false
				r.ticks = nil
				continue
			}
			r.vwap.Update(tick)
			r.levels.Update(tick)
			r.executor.OnTick(tick)
		}
	}

	r.executor.Summary()
	slog.Info("strategy runner finished")
}

// HandleTick processes a single tick: updates indicators and checks exit conditions.
// Used by the deterministic backtest pipeline.
func (r *Runner) HandleTick(tick models.Tick) {
	r.vwap.Update(tick)
	r.levels.Update(tick)
	r.executor.OnTick(tick)
}

// HandleCandle processes a single closed candle: updates bias and (when timeframe matches
// signalTF) feeds the entry detector. Used by the deterministic backtest pipeline.
func (r *Runner) HandleCandle(c models.Candle) {
	if c.Timeframe == models.Timeframe4H {
		r.bias.Update(c)
		slog.Info("4H candle closed",
			"open", c.Open, "close", c.Close,
			"bias", r.bias.Direction())
	}
	if c.Timeframe == r.signalTF {
		r.detector.AddCandle(c)
		r.evaluateEntry(c)
	} else if c.Timeframe == models.Timeframe30m {
		slog.Debug("30m candle closed",
			"open", c.Open, "high", c.High, "low", c.Low, "close", c.Close)
	}
}

// Summarize prints the PnL report. Called after all data is processed.
func (r *Runner) Summarize() {
	r.executor.Summary()
}

func (r *Runner) evaluateEntry(c models.Candle) {
	if r.liveMode && time.Since(c.CloseTime) > backfillStaleness {
		// Historical candle from REST kline backfill — firing a signal here would
		// open a position at a stale close price → instant adverse fill on the next
		// live tick. Indicators have already been updated by AddCandle (warmup is
		// the whole point of running backfill through the pipeline); we only
		// suppress signal emission, not state mutation.
		return
	}

	if !r.levels.HasData() {
		return // need at least one full day before PDH/PDL are valid
	}

	keyLevels := r.levels.KeyLevels(r.zones)
	vwap := r.vwap.Value()

	sig := r.detector.Evaluate(keyLevels, vwap, r.bias)
	if sig == nil {
		return
	}

	if r.fundingFilter != nil && !r.fundingFilter.Allows(sig.Side, sig.Timestamp) {
		slog.Info("signal filtered by funding rate",
			"side", sig.Side,
			"time", sig.Timestamp,
			"rate_8h", r.fundingFilter.Reader.RateAt(sig.Timestamp),
			"threshold_bps_per_day", r.fundingFilter.MaxBpsPerDay)
		return
	}

	slog.Info("signal generated",
		"side", sig.Side,
		"entry", sig.EntryPrice,
		"stop", sig.StopLoss,
		"target", sig.TakeProfit,
		"reason", sig.Reason,
		"time", sig.Timestamp)

	r.executor.OnSignal(sig)
}
