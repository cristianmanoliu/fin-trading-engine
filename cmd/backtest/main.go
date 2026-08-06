package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/cristianmanoliu/fin-trading-engine/config"
	"github.com/cristianmanoliu/fin-trading-engine/pkg/aggregator"
	"github.com/cristianmanoliu/fin-trading-engine/pkg/execution"
	"github.com/cristianmanoliu/fin-trading-engine/pkg/funding"
	"github.com/cristianmanoliu/fin-trading-engine/pkg/marketdata"
	"github.com/cristianmanoliu/fin-trading-engine/pkg/models"
	"github.com/cristianmanoliu/fin-trading-engine/pkg/strategy"
)

func main() {
	cfgPath := flag.String("config", "configs/default.yaml", "path to config file")
	symbol := flag.String("symbol", "", "override symbol from config (e.g. ETHUSDT)")
	year := flag.String("year", "", "year for CSV path (e.g. 2024)")
	month := flag.String("month", "", "month for CSV path, zero-padded (e.g. 01)")
	exactFills := flag.Bool("exact-fills", false, "audit mode: exit at exact stop/target price (no wick overshoot)")
	includeBoundary := flag.Bool("include-boundary", false, "audit mode: fold boundary tick into closing candle OHLCV (matches live close semantics)")
	pessimisticAmbiguous := flag.Bool("pessimistic-ambiguous", false, "audit mode: force STOP for LONG wins on 1m bars that also breach the stop (corrects synthetic-tick same-bar bias)")
	feeBps := flag.Float64("fee-bps", 0, "round-trip taker fee in basis points charged on entry notional (e.g. 10 = 0.10% Binance Futures Regular)")
	stopSlippageBps := flag.Float64("stop-slippage-bps", 0, "additional adverse slippage on losing trades in basis points (e.g. 5 = 0.05% on stop-market fills past trigger)")
	fundingBpsPerDay := flag.Float64("funding-bps-per-day", 0, "average daily funding rate drag in bps on entry notional (e.g. 3 = 0.03%/day average Binance perpetual funding)")
	taxRatePct := flag.Float64("tax-rate-pct", 0, "effective tax rate applied to portfolio-level net positive PnL at end of run (e.g. 30 = 30%)")
	signalTF := flag.String("signal-tf", "5m", "timeframe driving signal evaluation: 5m | 30m | 1H | 2H | 4H | 1D")
	atrStopMult := flag.Float64("atr-stop-mult", 0, "EMA-mode stop placement: when > 0, stop = entry ± ATR(period) × mult; 0 = legacy wick-based stop")
	atrPeriod := flag.Int("atr-period", 14, "ATR period in candles (only used when --atr-stop-mult > 0)")
	sideFilter := flag.String("side-filter", "both", "filter signals by direction: both | long | short")
	maxHoldHours := flag.Float64("max-hold-hours", 0, "force-close any open position older than this many hours; 0 = no cap (default). Useful to trim funding-eaten tails.")
	fundingCSVDir := flag.String("funding-csv-dir", "", "directory of per-symbol funding CSVs (e.g. data/funding/). When set, replaces --funding-bps-per-day with actual historical Binance funding rates accrued per 8h event with correct per-side sign.")
	fundingFilterMaxBpsPerDay := flag.Float64("funding-filter-max-bps-per-day", 0, "skip SHORT signals when current funding rate × 3 (per-day in bps) exceeds this threshold; 0 = disabled. Requires --funding-csv-dir. e.g. 5 = exclude only extreme bull regimes; 0.1 = exclude all positive funding.")
	emaFastPeriod := flag.Int("ema-fast-period", 0, "fast EMA period for EMA-cross signal (default 9 when EMAMode is true)")
	emaSlowPeriod := flag.Int("ema-slow-period", 0, "slow EMA period for EMA-cross signal (default 21 when EMAMode is true)")
	momentumMode := flag.Bool("momentum-mode", false, "enable 5m-momentum-candle strategy (enter on momentum candles aligned with 4H bias). Overrides EMA mode when true.")
	purgatoryMode := flag.Bool("purgatory-mode", false, "enable 'Purgatory Method': 5/9 EMA cross gated by price on same side of BOTH VWAP and EMA30. Overrides EMA mode. Research-only. Use --ema-fast-period 5 --ema-slow-period 9.")
	vwapDevMode := flag.Bool("vwap-deviation-mode", false, "enable VWAP deviation fade strategy (mean-reversion). Overrides --ema-mode in YAML when true.")
	vwapDevPct := flag.Float64("vwap-deviation-pct", 0, "fractional distance from VWAP to trigger fade entry (e.g. 0.02 = 2%). Required when --vwap-deviation-mode.")
	rsiMode := flag.Bool("rsi-mode", false, "enable RSI-cross-50 strategy (momentum). Overrides EMA mode when true.")
	rsiPeriod := flag.Int("rsi-period", 0, "RSI period (default 14)")
	pdhPdlMode := flag.Bool("pdh-pdl-break-mode", false, "enable PDH/PDL breakdown strategy (momentum, fixed-RR variant). Overrides EMA mode when true.")
	macdMode := flag.Bool("macd-mode", false, "enable MACD line/signal cross strategy (momentum). Overrides EMA mode when true.")
	macdFast := flag.Int("macd-fast", 0, "MACD fast EMA period (default 12)")
	macdSlow := flag.Int("macd-slow", 0, "MACD slow EMA period (default 26)")
	macdSignal := flag.Int("macd-signal", 0, "MACD signal-line EMA period (default 9)")
	bollingerMode := flag.Bool("bollinger-mode", false, "enable Bollinger band breakdown/breakout strategy (momentum). Overrides EMA mode when true.")
	bollingerPeriod := flag.Int("bollinger-period", 0, "Bollinger band SMA period (default 20)")
	bollingerStdMult := flag.Float64("bollinger-std-mult", 0, "Bollinger band stdev multiplier (default 2.0)")
	trailingStopMode := flag.Bool("trailing-stop-mode", false, "Cat B1: ratchet stop favorable as price moves (lock (N-1)R after NR favorable). Coexists with target-rr backstop.")
	trailIntervalR := flag.Float64("trail-interval-r", 0, "Cat B1: trail step in R-multiples (default 1.0)")
	multiLevelTPMode := flag.Bool("multi-level-tp-mode", false, "Cat B2: scale-out at mid-R; close MidFrac of position; raise stop to BE on remainder.")
	midR := flag.Float64("mid-r", 0, "Cat B2: mid-R multiple at which to partial-close (default 3.0)")
	midFrac := flag.Float64("mid-frac", 0, "Cat B2: fraction of position closed at mid-R (default 0.5)")
	confluence1DMode := flag.Bool("confluence-1d-mode", false, "Cat D1: gate signals by 1D EMA bias agreement. Sample 4H closes at UTC hour=0 into 1D EMA pair.")
	confluenceFastPeriod := flag.Int("confluence-fast", 0, "Cat D1: 1D EMA fast period (default 9)")
	confluenceSlowPeriod := flag.Int("confluence-slow", 0, "Cat D1: 1D EMA slow period (default 21)")
	volFilterMode := flag.Bool("vol-filter-mode", false, "Cat E1: skip entries when realized 30d annualized vol > MaxVolAnnualized.")
	maxVolAnnualized := flag.Float64("max-vol-annualized", 0, "Cat E1: max 30d realized vol as fraction (default 1.20 = 120%)")
	fundingCrossMode := flag.Bool("funding-cross-mode", false, "Cat F1: standalone entry on extreme 8h funding rate (mean-reversion on position crowding). Independent of EMA/RSI/MACD. Requires --funding-csv-dir.")
	fundingThresholdBpsPerDay := flag.Float64("funding-threshold-bps", 0, "Cat F1: |funding × 3 × 10000| threshold in bps/day (default 30, locked by pre-registered decision rule)")
	journalDir := flag.String("journal-dir", "", "when set, write per-trade JSONL journals to this directory (same schema as paper-live). Off by default — backtest runs are journal-silent unless explicitly opted in.")
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
	// BD-1: partial --year/--month previously silently ignored — operator
	// passing only one expected the override and got the YAML-default
	// CSV path. Audit-pattern fail-open: ambiguous flag → wrong data
	// source → wrong backtest verdict locked into results/. Now: refuse
	// asymmetric flags loudly.
	if (*year != "") != (*month != "") {
		slog.Error("--year and --month must be set together (or both omitted); partial override is ambiguous",
			"year", *year, "month", *month)
		os.Exit(1)
	}
	if *year != "" && *month != "" {
		cfg.Backtest.CSVPath = fmt.Sprintf("./data/%s-1m-%s-%s.csv", cfg.Symbol, *year, *month)
	}

	// Validate enum-style flags BEFORE opening the CSV. With a bogus
	// signal-tf or side-filter, the operator should see the explicit
	// validation error immediately, not a downstream CSV-open error
	// (which masks the real misconfig). Same ordering principle applied
	// to cmd/engine (validateExecutorArgs runs before Subscribe).
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

	// BD-3 (pre-flight): if the funding-filter flag is set without
	// --funding-csv-dir, the post-load type assertion will fail and
	// silently disable the filter. Pre-flight catches the obvious
	// flag-pair misuse before we spend CSV-open time. Mirrors the
	// cmd/engine validateExecutorArgs ordering principle. The later
	// post-LoadFromDir check still fires when the dir IS provided but
	// the per-symbol file is missing — a separate shape.
	if *fundingFilterMaxBpsPerDay > 0 && *fundingCSVDir == "" {
		slog.Error("--funding-filter-max-bps-per-day requires --funding-csv-dir to load Historical provider — refusing silent disable",
			"max_bps_per_day", *fundingFilterMaxBpsPerDay)
		os.Exit(1)
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

	entryCfg := strategy.EntryConfig{
		ProximityPct:      cfg.Strategy.ProximityPct,
		WickRatio:         cfg.Strategy.WickRatio,
		BreakoutBodyRatio: cfg.Strategy.BreakoutBodyRatio,
		AbsorptionCandles: cfg.Strategy.AbsorptionCandles,
		StopBufferPct:     cfg.Strategy.StopBufferPct,
		MinRR:             cfg.Strategy.MinRR,
		TargetRR:          cfg.Strategy.TargetRR,
		MomentumMode:      cfg.Strategy.MomentumMode || *momentumMode,
		PurgatoryMode:     *purgatoryMode,
		VWAPDeviationMode: cfg.Strategy.VWAPDeviationMode || *vwapDevMode,
		VWAPDeviationPct: func() float64 {
			if *vwapDevPct > 0 {
				return *vwapDevPct
			}
			return cfg.Strategy.VWAPDeviationPct
		}(),
		EMAMode:                   cfg.Strategy.EMAMode && !(*momentumMode) && !(*purgatoryMode) && !(*vwapDevMode) && !(*rsiMode) && !(*pdhPdlMode) && !(*macdMode) && !(*bollingerMode),
		EMAFastPeriod:             *emaFastPeriod,
		EMASlowPeriod:             *emaSlowPeriod,
		RSIMode:                   *rsiMode,
		RSIPeriod:                 *rsiPeriod,
		PDHPDLBreakMode:           *pdhPdlMode,
		MACDMode:                  *macdMode,
		MACDFast:                  *macdFast,
		MACDSlow:                  *macdSlow,
		MACDSignal:                *macdSignal,
		BollingerMode:             *bollingerMode,
		BollingerPeriod:           *bollingerPeriod,
		BollingerStdMult:          *bollingerStdMult,
		Confluence1DMode:          *confluence1DMode,
		ConfluenceFastPeriod:      *confluenceFastPeriod,
		ConfluenceSlowPeriod:      *confluenceSlowPeriod,
		VolFilterMode:             *volFilterMode,
		MaxVolAnnualized:          *maxVolAnnualized,
		FundingCrossMode:          *fundingCrossMode,
		FundingThresholdBpsPerDay: *fundingThresholdBpsPerDay,
		ATRStopMult:               *atrStopMult,
		ATRPeriod:                 *atrPeriod,
		SignalTimeframe:           tf,
		SideFilter:                sideDir,
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
		TrailingStopMode:     *trailingStopMode,
		TrailIntervalR:       *trailIntervalR,
		MultiLevelTPMode:     *multiLevelTPMode,
		MidRMult:             *midR,
		MidFrac:              *midFrac,
		JournalPath:          *journalDir,
		Symbol:               cfg.Symbol,
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

	// Cat F1 funding-cross signal needs RateAt access at signal-evaluation time.
	// Wire from the same Historical provider that exec uses for funding accrual.
	if *fundingCrossMode {
		if hist, ok := exec.FundingProvider.(*funding.Historical); ok {
			runner.SetFundingRateReader(hist.RateAt)
		} else {
			slog.Error("--funding-cross-mode requires --funding-csv-dir to load Historical provider")
			os.Exit(1)
		}
	}

	// Funding filter (optional): gate SHORT signals by current funding regime.
	// Requires Historical funding provider — Constant rate has no time variation.
	//
	// BD-3: previously slog.Warn + continue when the type assertion failed —
	// operator enabled the filter flag expecting shorts to be gated; the
	// backtest silently ran WITHOUT the filter, producing strategy results
	// that don't match operator intent. Same audit-pattern shape as the
	// cmd/engine 2nd-pass fix at fe4bf21 (--funding-filter-max-bps-per-day
	// silently disabled when provider isn't Historical). Now: exit 1.
	if *fundingFilterMaxBpsPerDay > 0 {
		hist, ok := exec.FundingProvider.(*funding.Historical)
		if !ok {
			slog.Error("--funding-filter-max-bps-per-day requires --funding-csv-dir to load Historical provider — refusing silent disable",
				"max_bps_per_day", *fundingFilterMaxBpsPerDay,
				"funding_csv_dir", *fundingCSVDir,
				"symbol", cfg.Symbol)
			os.Exit(1)
		}
		runner.SetFundingFilter(&strategy.FundingFilter{
			Reader:       hist,
			MaxBpsPerDay: *fundingFilterMaxBpsPerDay,
		})
		slog.Info("funding filter enabled", "max_bps_per_day", *fundingFilterMaxBpsPerDay)
	}

	// BD-4: track whether the loop exited via SIGINT/SIGTERM cancellation
	// vs natural CSV-exhausted completion. Without this distinction,
	// "backtest complete" fires identically in both cases — a Ctrl-C'd
	// run looks the same as a fully-replayed one. A partial sweep with
	// matching log message could be locked into results/ as if it were
	// a clean run.
	cancelled := false
loop:
	for {
		select {
		case <-ctx.Done():
			cancelled = true
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
	if cancelled {
		slog.Warn("backtest CANCELLED (interrupted before CSV exhaustion) — summary is PARTIAL, do not lock as a verdict")
		os.Exit(130) // 128 + SIGINT(2) — conventional shell exit code for interrupted process
	}
	slog.Info("backtest complete")
}
