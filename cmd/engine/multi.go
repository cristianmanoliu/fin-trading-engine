package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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

// multiFlagSet bundles the CLI-flag values that runMulti needs. Mirrors the
// flags consumed by the single-symbol path in main.go.
type multiFlagSet struct {
	configDir        string
	feeBps           float64
	stopSlippageBps  float64
	fundingBpsPerDay float64
	sideFilter       string
	maxHoldHours     float64
	fundingCSVDir    string
	targetRROverride float64
	signalTFOverride string
}

// runMulti loads per-symbol configs, opens one combined-streams marketdata
// source, and runs N goroutine groups (aggregator + runner + executor + heartbeat),
// one per symbol, sharing the single WebSocket connection.
//
// Returns when ctx is cancelled or any goroutine group returns an error.
func runMulti(ctx context.Context, symbols []string, flags multiFlagSet) error {
	notifier := notify.FromEnv()

	sideDir, err := parseSideFilter(flags.sideFilter)
	if err != nil {
		return err
	}

	// Load per-symbol configs.
	type symbolSetup struct {
		symbol string
		cfg    *config.Config
		entry  strategy.EntryConfig
		exec   *execution.Stub
	}

	setups := make([]symbolSetup, 0, len(symbols))
	var firstWSURL, firstRESTURL string
	var firstBackfillHours int

	for _, sym := range symbols {
		cfgPath := filepath.Join(flags.configDir, strings.ToLower(sym)+".yaml")
		cfg, err := config.Load(cfgPath)
		if err != nil {
			return fmt.Errorf("load %s: %w", cfgPath, err)
		}
		if flags.targetRROverride > 0 {
			cfg.Strategy.TargetRR = flags.targetRROverride
		}
		if flags.signalTFOverride != "" {
			cfg.Strategy.SignalTimeframe = flags.signalTFOverride
		}
		if firstWSURL == "" {
			firstWSURL = cfg.Exchange.WSURL
			firstRESTURL = cfg.Exchange.RESTURL
			firstBackfillHours = cfg.Strategy.BackfillHours
		}

		entry := strategy.EntryConfig{
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
			FeeBps:           flags.feeBps,
			StopSlippageBps:  flags.stopSlippageBps,
			FundingBpsPerDay: flags.fundingBpsPerDay,
			MaxHoldHours:     flags.maxHoldHours,
		}
		if flags.fundingCSVDir != "" {
			fp, err := funding.LoadFromDir(flags.fundingCSVDir, cfg.Symbol)
			if err != nil {
				return fmt.Errorf("funding %s: %w", cfg.Symbol, err)
			}
			if fp != nil {
				exec.FundingProvider = fp
				exec.FundingBpsPerDay = 0
				slog.Info("loaded historical funding", "symbol", cfg.Symbol)
			} else {
				slog.Warn("no historical funding for symbol — falling back to constant rate",
					"symbol", cfg.Symbol)
			}
		}

		setups = append(setups, symbolSetup{
			symbol: cfg.Symbol,
			cfg:    cfg,
			entry:  entry,
			exec:   exec,
		})
	}

	hostname, _ := os.Hostname()
	notifier.Send(ctx, fmt.Sprintf("🟢 *multi-engine* started on `%s` with %d symbols", hostname, len(setups))) //nolint:errcheck

	slog.Info("multi engine starting",
		"symbols", len(setups),
		"ws_url", firstWSURL,
		"backfill_hours", firstBackfillHours)

	src := marketdata.NewBinanceFuturesMulti(firstWSURL, firstRESTURL, symbols, firstBackfillHours)
	defer src.Close()

	tickChannels, err := src.SubscribeMulti(ctx)
	if err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}

	g, gctx := errgroup.WithContext(ctx)

	for i := range setups {
		setup := setups[i] // capture
		ticks, ok := tickChannels[setup.symbol]
		if !ok {
			return fmt.Errorf("no tick channel for %s", setup.symbol)
		}

		aggTicks := make(chan models.Tick, 1000)
		stratTicks := make(chan models.Tick, 1000)
		agg := aggregator.New(aggTicks)
		runner := strategy.NewRunner(
			agg.Chan4H(), agg.Chan30m(), agg.Chan5m(),
			agg.Chan1H(), agg.Chan2H(), agg.Chan1D(),
			stratTicks,
			setup.cfg.ModelZones(),
			setup.entry,
			setup.exec,
		)
		hb := marketdata.NewHeartbeat(setup.symbol)

		// Tick fan-out for this symbol.
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

		g.Go(func() error { agg.Run(gctx); return nil })
		g.Go(func() error { runner.Run(gctx); return nil })
		g.Go(func() error { hb.Run(gctx, 60*time.Second); return nil })
	}

	if err := g.Wait(); err != nil {
		return err
	}

	notifier.Send(context.Background(), fmt.Sprintf("🔴 *multi-engine* stopped (clean) on `%s`", hostname)) //nolint:errcheck
	return nil
}

// parseSideFilter validates and converts the --side-filter flag value.
func parseSideFilter(s string) (models.Direction, error) {
	switch s {
	case "both", "":
		return models.Neutral, nil
	case "long":
		return models.Long, nil
	case "short":
		return models.Short, nil
	default:
		return models.Neutral, fmt.Errorf("invalid --side-filter %q (want both | long | short)", s)
	}
}
