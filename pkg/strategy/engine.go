package strategy

import (
	"context"
	"log/slog"
	"time"

	"github.com/cristianmanoliu/fin-trading-engine/pkg/indicators"
	"github.com/cristianmanoliu/fin-trading-engine/pkg/models"
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

	// signalContext (optional) writes rich state at signal-emission time to
	// JSONL sidecars for forward-paper pattern-matching analysis. Nil = disabled.
	// See SignalContext / SignalContextWriter in signal_context.go.
	signalContext        *SignalContextWriter
	contextLabel         string // "live" | shadow label, embedded in each record
	fundingContextReader FundingRateReader // for sidecar only; separate from fundingFilter

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

// SetSignalContextWriter wires a JSONL sidecar that captures rich state at
// signal-emission time (just before executor.OnSignal). Pass nil to disable.
// label is embedded in each record so multiple Runners (live + shadows) can
// share an inspection pipeline. Safe to call before Run starts; concurrent
// use during Run is not supported.
func (r *Runner) SetSignalContextWriter(w *SignalContextWriter, label string) {
	r.signalContext = w
	r.contextLabel = label
}

// SetFundingRateReader wires a funding-rate accessor for FundingCrossMode
// (Cat F1, see results/cat_f1_funding_cross_decision_rule_2026-05-07.md).
// Pass nil to clear. Safe to call before Run; not safe to reconfigure during.
// Delegates to the inner EntryDetector so funding-cross signals can fire.
func (r *Runner) SetFundingRateReader(f func(t time.Time) float64) {
	if r.detector != nil {
		r.detector.SetFundingRateReader(f)
	}
}

// SetFundingContextReader wires a funding-rate reader used exclusively for
// signal-context sidecar enrichment. Independent of the funding-filter gate
// (SetFundingFilter) — allows funding rate to be captured in sidecar records
// even when the funding-regime filter is disabled (the common production case:
// --funding-csv-dir set but --funding-filter-max-bps-per-day=0).
func (r *Runner) SetFundingContextReader(reader FundingRateReader) {
	r.fundingContextReader = reader
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

// candleTooStale reports whether evaluateEntry should suppress signal emission
// because the candle's CloseTime predates the backfill-staleness threshold and
// liveMode is on. Extracted from evaluateEntry so the Bug 5 regression gate
// (see CLAUDE.md "Known Bugs / Fixed → Bug 5") can be tested in isolation
// without priming the full indicator/levels/bias pipeline.
func (r *Runner) candleTooStale(c models.Candle) bool {
	return r.liveMode && time.Since(c.CloseTime) > backfillStaleness
}

func (r *Runner) evaluateEntry(c models.Candle) {
	if r.candleTooStale(c) {
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

	r.writeSignalContext(sig)

	r.executor.OnSignal(sig)
}

// writeSignalContext is a no-op when no SignalContextWriter is wired. When
// wired, it builds a SignalContext record from Runner state + EntryDetector
// snapshot and appends it to the per-symbol JSONL sidecar.
func (r *Runner) writeSignalContext(sig *models.Signal) {
	if r.signalContext == nil {
		return
	}
	snap := r.detector.Snapshot()
	emaSpreadPct := 0.0
	if snap.EMA21 != 0 {
		emaSpreadPct = (snap.EMA9 - snap.EMA21) / snap.EMA21 * 100
	}
	distPDHPct := 0.0
	distPDLPct := 0.0
	if r.levels.PDH > 0 && sig.EntryPrice > 0 {
		distPDHPct = (r.levels.PDH - sig.EntryPrice) / sig.EntryPrice * 100
	}
	if r.levels.PDL > 0 && sig.EntryPrice > 0 {
		distPDLPct = (sig.EntryPrice - r.levels.PDL) / sig.EntryPrice * 100
	}

	rr := 0.0
	if sig.EntryPrice != sig.StopLoss {
		risk := sig.EntryPrice - sig.StopLoss
		reward := sig.TakeProfit - sig.EntryPrice
		if risk != 0 {
			rr = reward / risk
			if rr < 0 {
				rr = -rr
			}
		}
	}

	sideFilter := ""
	switch r.detector.cfg.SideFilter {
	case models.Long:
		sideFilter = "long"
	case models.Short:
		sideFilter = "short"
	case models.Neutral:
		sideFilter = ""
	}

	ctx := SignalContext{
		Event:             "signal_context",
		Symbol:            sig.Symbol,
		TS:                sig.Timestamp.UTC().Format(time.RFC3339),
		Label:             r.contextLabel,
		Side:              sig.Side.String(),
		Entry:             sig.EntryPrice,
		Stop:              sig.StopLoss,
		Target:            sig.TakeProfit,
		RR:                rr,
		Reason:            sig.Reason,
		EMA9:              snap.EMA9,
		EMA21:             snap.EMA21,
		EMASpreadPct:      emaSpreadPct,
		ATR:               snap.ATR,
		RealizedVol30dAnn: snap.RealizedVol30dAnn,
		BBUpper:           snap.BBUpper,
		BBMid:             snap.BBMid,
		BBLower:           snap.BBLower,
		Bias:              int(r.bias.Direction()),
		VWAP:              r.vwap.Value(),
		PDH:               r.levels.PDH,
		PDL:               r.levels.PDL,
		DistPDHPct:        distPDHPct,
		DistPDLPct:        distPDLPct,
		SignalTF:          string(r.signalTF),
		SideFilter:        sideFilter,
	}
	// Prefer fundingFilter.Reader (already wired when filter is active); fall back
	// to fundingContextReader (wired by cmd/engine when --funding-csv-dir is set
	// but --funding-filter-max-bps-per-day=0 — the production case that previously
	// left funding fields absent from all sidecar records).
	var fundingReader FundingRateReader
	if r.fundingFilter != nil && r.fundingFilter.Reader != nil {
		fundingReader = r.fundingFilter.Reader
	} else if r.fundingContextReader != nil {
		fundingReader = r.fundingContextReader
	}
	if fundingReader != nil {
		rate8h := fundingReader.RateAt(sig.Timestamp)
		ctx.FundingRate8h = rate8h
		ctx.FundingBpsPerDay = rate8h * 3 * 10000 // 3 funding intervals per day, → bps
	}
	r.signalContext.Write(ctx)
}
