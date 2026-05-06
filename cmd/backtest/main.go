package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/cristianmanoliu/trading-engine/config"
	"github.com/cristianmanoliu/trading-engine/pkg/aggregator"
	"github.com/cristianmanoliu/trading-engine/pkg/execution"
	"github.com/cristianmanoliu/trading-engine/pkg/funding"
	"github.com/cristianmanoliu/trading-engine/pkg/marketdata"
	"github.com/cristianmanoliu/trading-engine/pkg/models"
	"github.com/cristianmanoliu/trading-engine/pkg/strategy"
)

func main() {
	cfgPath              := flag.String("config", "configs/default.yaml", "path to config file")
	symbol               := flag.String("symbol", "", "override symbol from config (e.g. ETHUSDT)")
	year                 := flag.String("year", "", "year for CSV path (e.g. 2024)")
	month                := flag.String("month", "", "month for CSV path, zero-padded (e.g. 01)")
	exactFills           := flag.Bool("exact-fills", false, "audit mode: exit at exact stop/target price (no wick overshoot)")
	includeBoundary      := flag.Bool("include-boundary", false, "audit mode: fold boundary tick into closing candle OHLCV (matches live close semantics)")
	pessimisticAmbiguous := flag.Bool("pessimistic-ambiguous", false, "audit mode: force STOP for LONG wins on 1m bars that also breach the stop (corrects synthetic-tick same-bar bias)")
	feeBps               := flag.Float64("fee-bps", 0, "round-trip taker fee in basis points charged on entry notional (e.g. 10 = 0.10% Binance Futures Regular)")
	stopSlippageBps      := flag.Float64("stop-slippage-bps", 0, "additional adverse slippage on losing trades in basis points (e.g. 5 = 0.05% on stop-market fills past trigger)")
	fundingBpsPerDay     := flag.Float64("funding-bps-per-day", 0, "average daily funding rate drag in bps on entry notional (e.g. 3 = 0.03%/day average Binance perpetual funding)")
	taxRatePct           := flag.Float64("tax-rate-pct", 0, "effective tax rate applied to portfolio-level net positive PnL at end of run (e.g. 30 = 30%)")
	signalTF             := flag.String("signal-tf", "5m", "timeframe driving signal evaluation: 5m | 30m | 1H | 2H | 4H | 1D")
	atrStopMult          := flag.Float64("atr-stop-mult", 0, "EMA-mode stop placement: when > 0, stop = entry ± ATR(period) × mult; 0 = legacy wick-based stop")
	atrPeriod            := flag.Int("atr-period", 14, "ATR period in candles (only used when --atr-stop-mult > 0)")
	sideFilter           := flag.String("side-filter", "both", "filter signals by direction: both | long | short")
	maxHoldHours         := flag.Float64("max-hold-hours", 0, "force-close any open position older than this many hours; 0 = no cap (default). Useful to trim funding-eaten tails.")
	fundingCSVDir        := flag.String("funding-csv-dir", "", "directory of per-symbol funding CSVs (e.g. data/funding/). When set, replaces --funding-bps-per-day with actual historical Binance funding rates accrued per 8h event with correct per-side sign.")
	fundingFilterMaxBpsPerDay := flag.Float64("funding-filter-max-bps-per-day", 0, "skip SHORT signals when current funding rate × 3 (per-day in bps) exceeds this threshold; 0 = disabled. Requires --funding-csv-dir. e.g. 5 = exclude only extreme bull regimes; 0.1 = exclude all positive funding.")
	emaFastPeriod := flag.Int("ema-fast-period", 0, "fast EMA period for EMA-cross signal (default 9 when EMAMode is true)")
	emaSlowPeriod := flag.Int("ema-slow-period", 0, "slow EMA period for EMA-cross signal (default 21 when EMAMode is true)")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})))

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		slog.Error("failed to load config", "err", err)
		os.Exit(1)
	}

	if *symbol != "" {
		cfg.Symbol = *symbol
	}
	if *year != "" && *month != "" {
		cfg.Backtest.CSVPath = fmt.Sprintf("./data/%s-1m-%s-%s.csv", cfg.Symbol, *year, *month)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	slog.Info("starting backtest",
		"csv", cfg.Backtest.CSVPath,
		"symbol", cfg.Symbol)

	src := marketdata.NewCSVReplay(cfg.Backtest.CSVPath, cfg.Symbol)
	defer src.Close()

	ticks, err := src.Subscribe(ctx)
	if err != nil {
		slog.Error("failed to subscribe to csv replay", "err", err)
		os.Exit(1)
	}

	// Synchronous backtest pipeline — deterministic, single-goroutine event loop.
	// Each tick is processed in order: indicators → candle aggregation → entry evaluation.
	// This avoids the goroutine scheduling non-determinism that affects the live engine.
	agg := aggregator.New(nil)
	agg.IncludeBoundaryTick = *includeBoundary

	tf := models.Timeframe(*signalTF)
	switch tf {
	case models.Timeframe5m, models.Timeframe30m, models.Timeframe1H, models.Timeframe2H, models.Timeframe4H, models.Timeframe1D:
	default:
		slog.Error("invalid --signal-tf; must be 5m | 30m | 1H | 2H | 4H | 1D", "got", *signalTF)
		os.Exit(1)
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
		ATRStopMult:       *atrStopMult,
		ATRPeriod:         *atrPeriod,
		SignalTimeframe:   tf,
		SideFilter:        sideDir,
	}

	exec := &execution.Stub{
		StakeUSDT:            cfg.Strategy.StakeUSDT,
		ExactFills:           *exactFills,
		PessimisticAmbiguous: *pessimisticAmbiguous,
		FeeBps:               *feeBps,
		StopSlippageBps:      *stopSlippageBps,
		FundingBpsPerDay:     *fundingBpsPerDay,
		TaxRatePct:           *taxRatePct,
		MaxHoldHours:         *maxHoldHours,
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
		nil, nil, nil, nil, nil, nil, nil,
		cfg.ModelZones(),
		entryCfg,
		exec,
	)

	// Funding filter (optional): gate SHORT signals by current funding regime.
	// Requires Historical funding provider — Constant rate has no time variation.
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

loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case tick, ok := <-ticks:
			if !ok {
				break loop
			}
			runner.HandleTick(tick)
			for _, c := range agg.ProcessTick(tick) {
				runner.HandleCandle(c)
			}
		}
	}

	runner.Summarize()
	slog.Info("backtest complete")
}
