package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/cristianmanoliu/trading-engine/config"
	"github.com/cristianmanoliu/trading-engine/pkg/aggregator"
	"github.com/cristianmanoliu/trading-engine/pkg/execution"
	"github.com/cristianmanoliu/trading-engine/pkg/funding"
	"github.com/cristianmanoliu/trading-engine/pkg/marketdata"
	"github.com/cristianmanoliu/trading-engine/pkg/models"
	"github.com/cristianmanoliu/trading-engine/pkg/notify"
	"github.com/cristianmanoliu/trading-engine/pkg/strategy"
)

func main() {
	cfgPath          := flag.String("config", "configs/default.yaml", "path to config file")
	feeBps           := flag.Float64("fee-bps", 0, "round-trip taker fee in basis points (e.g. 10 = 0.10% Binance Futures Regular)")
	stopSlippageBps  := flag.Float64("stop-slippage-bps", 0, "additional adverse slippage on losing trades in basis points (e.g. 5 = 0.05% on stop-market fills past trigger)")
	fundingBpsPerDay := flag.Float64("funding-bps-per-day", 0, "constant daily funding rate drag in bps (overridden when --funding-csv-dir is set)")
	sideFilter       := flag.String("side-filter", "both", "filter signals by direction: both | long | short")
	maxHoldHours     := flag.Float64("max-hold-hours", 0, "force-close any open position older than this many hours; 0 = no cap")
	fundingCSVDir    := flag.String("funding-csv-dir", "", "directory of per-symbol funding CSVs (e.g. data/funding/); replaces --funding-bps-per-day with historical Binance rates")
	targetRROverride := flag.Float64("target-rr", 0, "override YAML target_rr when > 0")
	signalTFOverride := flag.String("signal-tf", "", "override YAML signal_tf when set (5m | 30m | 4H)")
	fundingFilterMaxBpsPerDay := flag.Float64("funding-filter-max-bps-per-day", 0, "skip SHORT signals when current funding rate × 3 (per-day in bps) exceeds this threshold; 0 = disabled. Requires --funding-csv-dir. e.g. 5 = exclude only extreme bull regimes; 0.1 = exclude all positive funding.")
	shadowFlag := flag.String("shadow", "", "comma-separated shadow strategy specs to run alongside live: 'label1:ema_fast-ema_slow-max_hold,label2:...'. Shadow strategies see identical market data but write to /var/log/paper-live/journal/shadow/<label>/. Used to test parameter variants forward without changing the live config.")
	emaFastPeriod := flag.Int("ema-fast-period", 0, "fast EMA period for live strategy (default 9 when EMAMode is true)")
	emaSlowPeriod := flag.Int("ema-slow-period", 0, "slow EMA period for live strategy (default 21 when EMAMode is true)")
	signalContextDir := flag.String("signal-context-dir", "", "directory for signal-context JSONL sidecars; written per-runner under <dir>/<label>/<symbol>-<month>.jsonl. Off by default; when unset and PAPER_LIVE_SIGNAL_CONTEXT_DIR env is set, that env value is used.")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	notifier := notify.FromEnv()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		slog.Error("failed to load config", "err", err)
		os.Exit(1)
	}

	if *targetRROverride > 0 {
		cfg.Strategy.TargetRR = *targetRROverride
	}
	if *signalTFOverride != "" {
		switch *signalTFOverride {
		case "5m", "30m", "1H", "2H", "4H", "1D":
			cfg.Strategy.SignalTimeframe = *signalTFOverride
		default:
			slog.Error("invalid --signal-tf; must be 5m | 30m | 1H | 2H | 4H | 1D", "got", *signalTFOverride)
			os.Exit(1)
		}
	}

	var sideDir models.Direction
	switch *sideFilter {
	case "both", "":
		sideDir = models.Neutral
	case "long":
		sideDir = models.Long
	case "short":
		sideDir = models.Short
	default:
		slog.Error("invalid --side-filter; must be both | long | short", "got", *sideFilter)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	hostname, _ := os.Hostname()
	notifier.Send(ctx, fmt.Sprintf("🟢 *%s* engine started on `%s`", cfg.Symbol, hostname)) //nolint:errcheck

	slog.Info("starting live engine",
		"symbol", cfg.Symbol,
		"ws_url", cfg.Exchange.WSURL,
		"backfill_hours", cfg.Strategy.BackfillHours)

	src := marketdata.NewBinanceFutures(
		cfg.Exchange.WSURL,
		cfg.Exchange.RESTURL,
		cfg.Symbol,
		cfg.Strategy.BackfillHours,
	)
	defer src.Close()

	ticks, err := src.Subscribe(ctx)
	if err != nil {
		slog.Error("failed to subscribe to binance ws", "err", err)
		os.Exit(1)
	}

	aggTicks := make(chan models.Tick, 1000)
	stratTicks := make(chan models.Tick, 1000)

	agg := aggregator.New(aggTicks)

	entryCfg := strategy.EntryConfig{
		ProximityPct:      cfg.Strategy.ProximityPct,
		WickRatio:         cfg.Strategy.WickRatio,
		BreakoutBodyRatio: cfg.Strategy.BreakoutBodyRatio,
		AbsorptionCandles: cfg.Strategy.AbsorptionCandles,
		StopBufferPct:     cfg.Strategy.StopBufferPct,
		MinRR:             cfg.Strategy.MinRR,
		TargetRR:          cfg.Strategy.TargetRR,
		MomentumMode:      cfg.Strategy.MomentumMode,
		VWAPDeviationMode: cfg.Strategy.VWAPDeviationMode,
		VWAPDeviationPct:  cfg.Strategy.VWAPDeviationPct,
		EMAMode:           cfg.Strategy.EMAMode,
		EMAFastPeriod:     *emaFastPeriod,
		EMASlowPeriod:     *emaSlowPeriod,
		ATRStopMult:       cfg.Strategy.ATRStopMult,
		ATRPeriod:         cfg.Strategy.ATRPeriod,
		SignalTimeframe:   models.Timeframe(cfg.Strategy.SignalTimeframe),
		SideFilter:        sideDir,
	}

	journalDir := os.Getenv("PAPER_LIVE_JOURNAL_DIR")
	if journalDir == "" {
		journalDir = "./logs/journal"
	}

	exec := &execution.Stub{
		StakeUSDT:        cfg.Strategy.StakeUSDT,
		JournalPath:      journalDir,
		Symbol:           cfg.Symbol,
		FeeBps:           *feeBps,
		StopSlippageBps:  *stopSlippageBps,
		FundingBpsPerDay: *fundingBpsPerDay,
		MaxHoldHours:     *maxHoldHours,
	}

	// Per-symbol historical funding overrides the constant rate when the file is present.
	if *fundingCSVDir != "" {
		fp, err := funding.LoadFromDir(*fundingCSVDir, cfg.Symbol)
		if err != nil {
			slog.Error("failed to load historical funding", "err", err, "symbol", cfg.Symbol)
			os.Exit(1)
		}
		if fp != nil {
			exec.FundingProvider = fp
			exec.FundingBpsPerDay = 0 // historical replaces constant
			slog.Info("loaded historical funding", "symbol", cfg.Symbol, "dir", *fundingCSVDir)
		} else {
			slog.Warn("no historical funding file for symbol — falling back to constant rate", "symbol", cfg.Symbol)
		}
	}

	// Parse shadow specs first so we know how many runners to fan-out to.
	shadowSpecs, err := strategy.ParseShadowSpecs(*shadowFlag)
	if err != nil {
		slog.Error("invalid --shadow spec", "err", err)
		os.Exit(1)
	}

	// Fan-out: live runner = index 0, shadow runners = indices 1..N.
	// When no shadows are configured, fan-out is a no-op that just rebroadcasts
	// 1→1, adding negligible overhead vs the prior direct subscription.
	totalRunners := 1 + len(shadowSpecs)
	c4hChans := marketdata.FanOutCandles(ctx, agg.Chan4H(), totalRunners)
	c30mChans := marketdata.FanOutCandles(ctx, agg.Chan30m(), totalRunners)
	c5mChans := marketdata.FanOutCandles(ctx, agg.Chan5m(), totalRunners)
	c1hChans := marketdata.FanOutCandles(ctx, agg.Chan1H(), totalRunners)
	c2hChans := marketdata.FanOutCandles(ctx, agg.Chan2H(), totalRunners)
	c1dChans := marketdata.FanOutCandles(ctx, agg.Chan1D(), totalRunners)
	tickChans := marketdata.FanOutTicks(ctx, stratTicks, totalRunners)

	runner := strategy.NewRunner(
		c4hChans[0], c30mChans[0], c5mChans[0],
		c1hChans[0], c2hChans[0], c1dChans[0],
		tickChans[0],
		cfg.ModelZones(),
		entryCfg,
		exec,
	)
	runner.SetLiveMode(true)

	// Resolve signal-context dir: CLI flag wins, then env var, else off.
	signalContextRoot := *signalContextDir
	if signalContextRoot == "" {
		signalContextRoot = os.Getenv("PAPER_LIVE_SIGNAL_CONTEXT_DIR")
	}
	if signalContextRoot != "" {
		liveCtxWriter := &strategy.SignalContextWriter{
			Dir:    filepath.Join(signalContextRoot, "live"),
			Symbol: cfg.Symbol,
		}
		runner.SetSignalContextWriter(liveCtxWriter, "live")
		slog.Info("signal-context writer enabled",
			"label", "live",
			"dir", liveCtxWriter.Dir)
	}

	// Funding filter (optional): gate SHORT signals by current funding regime.
	// Requires Historical funding provider — Constant rate has no time variation
	// so the filter would be trivially uniform. Mirrors cmd/backtest wiring.
	if *fundingFilterMaxBpsPerDay > 0 {
		if hist, ok := exec.FundingProvider.(*funding.Historical); ok {
			runner.SetFundingFilter(&strategy.FundingFilter{
				Reader:       hist,
				MaxBpsPerDay: *fundingFilterMaxBpsPerDay,
			})
			slog.Info("funding filter enabled", "max_bps_per_day", *fundingFilterMaxBpsPerDay)
		} else {
			slog.Warn("--funding-filter-max-bps-per-day requires --funding-csv-dir to load Historical provider; filter ignored")
		}
	}

	// Build shadow runners: each gets its own EntryConfig (per-type), its own Stub
	// (with shadow-prefixed JournalPath and per-spec MaxHoldHours), and its own slot
	// in the fan-out arrays.
	//
	// Two shadow types supported:
	//   - "ema": override EMAFastPeriod/EMASlowPeriod on the live entry config.
	//   - "bb":  switch to Bollinger entry mode (EMAMode=false, BollingerMode=true).
	shadowRunners := make([]*strategy.Runner, len(shadowSpecs))
	for i, spec := range shadowSpecs {
		shadowEntryCfg := entryCfg
		switch spec.Type {
		case "bb":
			// Disable EMA dispatch; use Bollinger breakdown trigger.
			shadowEntryCfg.EMAMode = false
			shadowEntryCfg.BollingerMode = true
			shadowEntryCfg.BollingerPeriod = spec.BollingerPeriod
			shadowEntryCfg.BollingerStdMult = spec.BollingerStdMult
			// Force Cat D/E filters off for BB shadow — they were tuned for EMA entry.
			shadowEntryCfg.Confluence1DMode = false
			shadowEntryCfg.VolFilterMode = false
		default: // "ema" or empty (legacy)
			shadowEntryCfg.EMAFastPeriod = spec.EMAFastPeriod
			shadowEntryCfg.EMASlowPeriod = spec.EMASlowPeriod
		}

		shadowExec := &execution.Stub{
			StakeUSDT:        cfg.Strategy.StakeUSDT,
			JournalPath:      filepath.Join(journalDir, "shadow", spec.Label),
			Symbol:           cfg.Symbol,
			FeeBps:           *feeBps,
			StopSlippageBps:  *stopSlippageBps,
			FundingBpsPerDay: *fundingBpsPerDay,
			MaxHoldHours:     spec.MaxHoldHours,
			FundingProvider:  exec.FundingProvider, // share — provider is read-only
		}

		idx := i + 1 // live = 0, shadows = 1..N
		shadowRunners[i] = strategy.NewRunner(
			c4hChans[idx], c30mChans[idx], c5mChans[idx],
			c1hChans[idx], c2hChans[idx], c1dChans[idx],
			tickChans[idx],
			cfg.ModelZones(),
			shadowEntryCfg,
			shadowExec,
		)
		shadowRunners[i].SetLiveMode(true)
		if signalContextRoot != "" {
			shadowCtxWriter := &strategy.SignalContextWriter{
				Dir:    filepath.Join(signalContextRoot, spec.Label),
				Symbol: cfg.Symbol,
			}
			shadowRunners[i].SetSignalContextWriter(shadowCtxWriter, spec.Label)
		}
		switch spec.Type {
		case "bb":
			slog.Info("shadow strategy registered",
				"label", spec.Label,
				"type", "bb",
				"bollinger_period", spec.BollingerPeriod,
				"bollinger_std_mult", spec.BollingerStdMult,
				"max_hold_hours", spec.MaxHoldHours,
				"journal_path", shadowExec.JournalPath)
		default:
			slog.Info("shadow strategy registered",
				"label", spec.Label,
				"type", "ema",
				"ema_fast", spec.EMAFastPeriod,
				"ema_slow", spec.EMASlowPeriod,
				"max_hold_hours", spec.MaxHoldHours,
				"journal_path", shadowExec.JournalPath)
		}
	}

	hb := marketdata.NewHeartbeat(cfg.Symbol)

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		defer close(aggTicks)
		defer close(stratTicks)
		for {
			select {
			case <-gctx.Done():
				return nil
			case tick, ok := <-ticks:
				if !ok {
					return nil
				}
				hb.Observe(tick)
				select {
				case aggTicks <- tick:
				case <-gctx.Done():
					return nil
				}
				select {
				case stratTicks <- tick:
				case <-gctx.Done():
					return nil
				}
			}
		}
	})

	g.Go(func() error {
		agg.Run(gctx)
		return nil
	})

	g.Go(func() error {
		runner.Run(gctx)
		return nil
	})

	for i := range shadowRunners {
		sr := shadowRunners[i] // capture for closure
		g.Go(func() error {
			sr.Run(gctx)
			return nil
		})
	}

	g.Go(func() error {
		hb.Run(gctx, 60*time.Second)
		return nil
	})

	if err := g.Wait(); err != nil {
		slog.Error("engine error", "err", err)
		os.Exit(1)
	}

	slog.Info("engine stopped")
	notifier.Send(context.Background(), fmt.Sprintf("🔴 *%s* engine stopped (clean)", cfg.Symbol)) //nolint:errcheck
}
