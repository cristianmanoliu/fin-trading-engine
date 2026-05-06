package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
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

	runner := strategy.NewRunner(
		agg.Chan4H(),
		agg.Chan30m(),
		agg.Chan5m(),
		agg.Chan1H(),
		agg.Chan2H(),
		agg.Chan1D(),
		stratTicks,
		cfg.ModelZones(),
		entryCfg,
		exec,
	)

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
