package main

import (
	"context"
	"errors"
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
	executorMode := flag.String("executor", "stub", "executor mode: stub (paper-money default — current paper-live deploy) | binance_live_testnet (Layer 2 integration gate; orders go to testnet.binancefuture.com, prices stay on production fapi) | binance_live (real money, STAGE_1+ promotion). binance_live and binance_live_testnet both require BINANCE_API_KEY and BINANCE_API_SECRET env vars (testnet uses SEPARATE credentials from mainnet) and only govern the LIVE runner — shadow runners always use stub by design.")
	layer3TestnetJournalDir := flag.String("layer3-binance-testnet-journal-dir", "", "Layer 3 shadow-parity gate (per real_money_executor_architecture_decision_rule_2026-05-08.md): when set, the LIVE runner is wrapped in a TeeExecutor that fans signals/ticks to BOTH the configured Stub primary AND a BinanceLiveTestnet shadow whose journals land in this directory. Both executors see identical ticks (single-engine, single-subscription) — exactly the 'same input ticks' the locked rule requires. Diff via cmd/journal_diff after ≥7d. Requires --executor=stub (real-money primary forbidden) and BINANCE_API_KEY/BINANCE_API_SECRET (testnet credentials).")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	notifier := notify.FromEnv()

	// Panic-recovery → CRITICAL Telegram alert. Without this, an engine
	// panic would log to stderr (visible only on log review) but the
	// operator wouldn't know the engine died until the watchdog or the
	// next forward-paper status check noticed the missing heartbeat.
	// CRITICAL severity bypasses rate limit + mute hours per the locked
	// telegram alert design rule.
	defer func() {
		if r := recover(); r != nil {
			body := fmt.Sprintf("engine panic\nsymbol: %s\nerror: %v", os.Getenv("PAPER_LIVE_SYMBOL"), r)
			_ = notifier.SendStructured(context.Background(), notify.SeverityCritical, body)
			panic(r) // re-panic so the process actually dies + stack-trace surfaces
		}
	}()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		slog.Error("failed to load config", "err", err, "path", *cfgPath)
		// Telegram CRITICAL — without this, an operator's first deploy
		// after a malformed YAML edit silently fails (engine never starts,
		// systemd watchdog will catch the restart loop eventually but the
		// primary signal is dashboard-blind). Best-effort: notifier may be
		// a no-op when TELEGRAM env is unset, in which case this returns
		// nil silently. Same pattern as the existing executor-validation
		// alert at line ~119.
		_ = notify.FromEnv().SendStructured(context.Background(), notify.SeverityCritical,
			fmt.Sprintf("STARTUP FAILED — config load: %s\nerror: %v", *cfgPath, err))
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
			_ = notifier.SendStructured(context.Background(), notify.SeverityCritical,
				fmt.Sprintf("STARTUP FAILED on %s — invalid --signal-tf=%q (must be 5m | 30m | 1H | 2H | 4H | 1D)",
					cfg.Symbol, *signalTFOverride))
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
		_ = notifier.SendStructured(context.Background(), notify.SeverityCritical,
			fmt.Sprintf("STARTUP FAILED on %s — invalid --side-filter=%q (must be both | long | short)",
				cfg.Symbol, *sideFilter))
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	hostname, _ := os.Hostname()

	// Early validation of executor + Layer 3 args. Fails fast BEFORE
	// marketdata.Subscribe (which spends ~30s on the 96h backfill) and
	// BEFORE the "engine started" INFO Telegram alert. Without this, an
	// operator misconfig (e.g., binance_live without API creds) wastes
	// 30s of backfill, fires a misleading "engine started" alert, and
	// only then fails — followed immediately by a CRITICAL STARTUP FAILED
	// from the executor switch. Now: one CRITICAL on misconfig, no INFO,
	// no wasted backfill. The downstream executor switch keeps its own
	// validation as defense-in-depth (resists refactoring drift).
	if err := validateExecutorArgs(*executorMode, *layer3TestnetJournalDir); err != nil {
		slog.Error(err.Error(),
			"executor", *executorMode,
			"layer3_dir", *layer3TestnetJournalDir,
			"symbol", cfg.Symbol)
		_ = notifier.SendStructured(ctx, notify.SeverityCritical,
			fmt.Sprintf("STARTUP FAILED on %s — %v", cfg.Symbol, err))
		os.Exit(1)
	}

	_ = notifier.SendStructured(ctx, notify.SeverityInfo,
		fmt.Sprintf("engine started\nsymbol: %s\nhost: %s", cfg.Symbol, hostname))

	slog.Info("starting live engine",
		"symbol", cfg.Symbol,
		"executor", *executorMode,
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
		slog.Error("failed to subscribe to binance ws", "err", err, "ws_url", cfg.Exchange.WSURL)
		// Telegram CRITICAL — engine started alert fired at line ~123,
		// so the operator expects a healthy engine; subscribe failure is
		// a startup failure that contradicts the earlier alert. Without
		// this, the only signal is "engine started" → silence (engine is
		// dead). The watchdog catches the dead engine eventually but
		// Telegram is the primary operational channel.
		_ = notifier.SendStructured(context.Background(), notify.SeverityCritical,
			fmt.Sprintf("STARTUP FAILED on %s — Binance WebSocket subscribe error\n%v\nws_url: %s",
				cfg.Symbol, err, cfg.Exchange.WSURL))
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

	// Load funding provider once, before the executor branch — both Stub
	// (legacy paper-live path) and the always-Stub shadow runners reference
	// it. BinanceLive doesn't model funding locally; the locked design
	// "Exchange-reported funding cost differs from local model" specifies
	// that real-money runs use exchange-reported funding only.
	var fundingProvider funding.Provider
	if *fundingCSVDir != "" {
		fp, err := funding.LoadFromDir(*fundingCSVDir, cfg.Symbol)
		if err != nil {
			slog.Error("failed to load historical funding", "err", err, "symbol", cfg.Symbol)
			os.Exit(1)
		}
		if fp != nil {
			fundingProvider = fp
			slog.Info("loaded historical funding", "symbol", cfg.Symbol, "dir", *fundingCSVDir)
			// Staleness warning: an operator-missed weekly refresh leaves trades
			// held past the last entry accruing $0 funding silently. Bounded in
			// steady state (refresh + restart caps drift at ~7d) — but if it
			// ever exceeds a week, surface it loudly so the operator can run
			// scripts/refresh_funding.sh + redeploy.
			if last := fp.LastTS(); !last.IsZero() {
				age := time.Since(last)
				if age > 7*24*time.Hour {
					slog.Warn("funding CSV is stale; trades held past last entry will accrue $0 funding — run scripts/refresh_funding.sh + redeploy",
						"symbol", cfg.Symbol,
						"last_funding_ts", last.UTC().Format(time.RFC3339),
						"stale_days", age.Hours()/24,
					)
				}
			}
		} else {
			slog.Warn("no historical funding file for symbol — falling back to constant rate", "symbol", cfg.Symbol)
		}
	}

	// Branch on --executor: paper-money Stub (default, today's deployed
	// behavior) vs real-money BinanceLive (STAGE_1+ promotion only).
	// Shadow runners are ALWAYS Stub regardless of this flag — real money
	// goes to one strategy at a time per the locked stage-promotion rule.
	var exec strategy.Executor
	var liveBinance *execution.BinanceLive

	switch *executorMode {
	case "stub", "":
		stub := &execution.Stub{
			StakeUSDT:        cfg.Strategy.StakeUSDT,
			JournalPath:      journalDir,
			Symbol:           cfg.Symbol,
			FeeBps:           *feeBps,
			StopSlippageBps:  *stopSlippageBps,
			FundingBpsPerDay: *fundingBpsPerDay,
			MaxHoldHours:     *maxHoldHours,
			FundingProvider:  fundingProvider,
		}
		if fundingProvider != nil {
			stub.FundingBpsPerDay = 0 // historical replaces constant
		}

		// Recover any unclosed paper position from the journal before any tick
		// flow starts. Bridges the orphan-open gap that previously existed
		// when an engine restart wiped Stub.position while the journal still
		// had the corresponding open event with no matching close.
		if recovered, err := stub.RecoverFromJournal(); err != nil {
			slog.Warn("live position recovery failed", "err", err, "symbol", cfg.Symbol)
			_ = notifier.SendStructured(ctx, notify.SeverityWarn,
				fmt.Sprintf("live recovery FAILED\nsymbol: %s\nerror: %v", cfg.Symbol, err))
		} else if recovered {
			slog.Info("live position recovery: in-flight trade restored", "symbol", cfg.Symbol)
			_ = notifier.SendStructured(ctx, notify.SeverityWarn,
				fmt.Sprintf("live position recovered from journal\nsymbol: %s\ncohort: live", cfg.Symbol))
		}
		exec = stub

	case "binance_live":
		apiKey := os.Getenv("BINANCE_API_KEY")
		apiSecret := os.Getenv("BINANCE_API_SECRET")
		if apiKey == "" || apiSecret == "" {
			slog.Error("--executor=binance_live requires BINANCE_API_KEY and BINANCE_API_SECRET env vars",
				"symbol", cfg.Symbol)
			_ = notifier.SendStructured(ctx, notify.SeverityCritical,
				fmt.Sprintf("STARTUP FAILED on %s — --executor=binance_live without BINANCE_API_KEY/BINANCE_API_SECRET env vars",
					cfg.Symbol))
			os.Exit(1)
		}
		bl := execution.NewBinanceLive(cfg.Symbol, cfg.Strategy.StakeUSDT, apiKey, apiSecret)
		bl.JournalPath = journalDir
		bl.FeeBps = *feeBps
		bl.StopSlippageBps = *stopSlippageBps
		bl.MaxHoldHours = *maxHoldHours
		bl.Notifier = notifier
		bl.PositionReconciler.Notifier = notifier

		// Recover + verify against exchange before any tick flow starts.
		// ErrRecoveryDrift maps to "BLOCK startup" per the locked rule
		// (real_money_executor_architecture_decision_rule_2026-05-08.md
		// "Engine restarts mid-real-money-trade"). os.Exit(2) is used so
		// systemd watchdog logs distinguish a recovery-drift exit from a
		// generic os.Exit(1).
		if recovered, err := bl.RecoverFromJournal(ctx); err != nil {
			if errors.Is(err, execution.ErrRecoveryDrift) {
				slog.Error("STARTUP BLOCKED: real-money recovery drift — operator must reconcile + restart",
					"symbol", cfg.Symbol, "err", err)
				_ = notifier.SendStructured(ctx, notify.SeverityCritical,
					fmt.Sprintf("STARTUP BLOCKED on %s — real-money recovery drift\n%v\nOperator must investigate the divergence (close exchange position OR adjust journal) and restart the engine.",
						cfg.Symbol, err))
				os.Exit(2)
			}
			slog.Error("real-money recovery failed (non-drift)", "err", err, "symbol", cfg.Symbol)
			_ = notifier.SendStructured(ctx, notify.SeverityCritical,
				fmt.Sprintf("STARTUP FAILED on %s — real-money recovery error\n%v\nOperator must investigate before restarting.",
					cfg.Symbol, err))
			os.Exit(2)
		} else if recovered {
			slog.Info("real-money position recovered + verified clean", "symbol", cfg.Symbol)
			_ = notifier.SendStructured(ctx, notify.SeverityWarn,
				fmt.Sprintf("real-money position recovered + verified on %s", cfg.Symbol))
		}

		liveBinance = bl
		exec = bl

		slog.Warn("REAL-MONEY EXECUTOR ACTIVE — orders will be sent to Binance",
			"symbol", cfg.Symbol, "stake_usd", cfg.Strategy.StakeUSDT)
		_ = notifier.SendStructured(ctx, notify.SeverityCritical,
			fmt.Sprintf("REAL-MONEY engine started on %s\nstake: $%.0f / trade\nhost: %s",
				cfg.Symbol, cfg.Strategy.StakeUSDT, hostname))

	case "binance_live_testnet":
		// Layer 2 integration gate per real_money_executor_architecture_decision_rule_2026-05-08.md.
		// Identical wiring to binance_live but orders go to testnet.binancefuture.com
		// (SEPARATE credentials). Tick/price feeds stay on production fapi —
		// Layer 2 contract is "real prices, fake fills". Severity-Warn instead
		// of Critical because no real capital is at risk.
		apiKey := os.Getenv("BINANCE_API_KEY")
		apiSecret := os.Getenv("BINANCE_API_SECRET")
		if apiKey == "" || apiSecret == "" {
			slog.Error("--executor=binance_live_testnet requires BINANCE_API_KEY and BINANCE_API_SECRET env vars (testnet credentials)",
				"symbol", cfg.Symbol)
			_ = notifier.SendStructured(ctx, notify.SeverityWarn,
				fmt.Sprintf("STARTUP FAILED on %s — --executor=binance_live_testnet without BINANCE_API_KEY/BINANCE_API_SECRET env vars",
					cfg.Symbol))
			os.Exit(1)
		}
		bl := execution.NewBinanceLiveTestnet(cfg.Symbol, cfg.Strategy.StakeUSDT, apiKey, apiSecret)
		bl.JournalPath = journalDir
		bl.FeeBps = *feeBps
		bl.StopSlippageBps = *stopSlippageBps
		bl.MaxHoldHours = *maxHoldHours
		bl.Notifier = notifier
		bl.PositionReconciler.Notifier = notifier

		// Same recovery + drift-block flow as mainnet — exercising it on testnet
		// is precisely what Layer 2 is for. Use exit code 2 on drift so the
		// distinct-exit-code contract holds in both modes.
		if recovered, err := bl.RecoverFromJournal(ctx); err != nil {
			if errors.Is(err, execution.ErrRecoveryDrift) {
				slog.Error("STARTUP BLOCKED: testnet recovery drift — operator must reconcile + restart",
					"symbol", cfg.Symbol, "err", err)
				_ = notifier.SendStructured(ctx, notify.SeverityWarn,
					fmt.Sprintf("STARTUP BLOCKED on %s (TESTNET) — recovery drift\n%v\nOperator must investigate the divergence (close exchange position OR adjust journal) and restart the engine.",
						cfg.Symbol, err))
				os.Exit(2)
			}
			slog.Error("testnet recovery failed (non-drift)", "err", err, "symbol", cfg.Symbol)
			_ = notifier.SendStructured(ctx, notify.SeverityWarn,
				fmt.Sprintf("STARTUP FAILED on %s (TESTNET) — recovery error\n%v",
					cfg.Symbol, err))
			os.Exit(2)
		} else if recovered {
			slog.Info("testnet position recovered + verified clean", "symbol", cfg.Symbol)
			_ = notifier.SendStructured(ctx, notify.SeverityInfo,
				fmt.Sprintf("testnet position recovered + verified on %s", cfg.Symbol))
		}

		liveBinance = bl
		exec = bl

		slog.Warn("TESTNET EXECUTOR ACTIVE — orders will be sent to Binance TESTNET (no real capital)",
			"symbol", cfg.Symbol, "stake_usd", cfg.Strategy.StakeUSDT, "api_base", execution.TestnetAPIBaseURL)
		_ = notifier.SendStructured(ctx, notify.SeverityWarn,
			fmt.Sprintf("TESTNET engine started on %s\nstake: $%.0f / trade (paper)\nhost: %s",
				cfg.Symbol, cfg.Strategy.StakeUSDT, hostname))

	default:
		slog.Error("invalid --executor; must be 'stub', 'binance_live_testnet', or 'binance_live'", "got", *executorMode)
		os.Exit(1)
	}

	// Layer 3 shadow-parity wrap. When --layer3-binance-testnet-journal-dir
	// is set, fan the live runner's signals/ticks to BOTH the existing Stub
	// primary AND a BinanceLiveTestnet shadow that journals separately.
	// Both executors see identical ticks (this is the locked-rule "same
	// input ticks" semantics — single engine, single subscription). After
	// ≥7d, operator runs cmd/journal_diff against the two journals to
	// validate the 0.5%-pnl tolerance. Constraint: live executor must be
	// stub (real-money primary forbidden — wrapping that would silently
	// double real-money exposure).
	if *layer3TestnetJournalDir != "" {
		if *executorMode != "stub" && *executorMode != "" {
			slog.Error("--layer3-binance-testnet-journal-dir requires --executor=stub (cannot wrap a real-money primary)",
				"executor", *executorMode)
			os.Exit(1)
		}
		apiKey := os.Getenv("BINANCE_API_KEY")
		apiSecret := os.Getenv("BINANCE_API_SECRET")
		if apiKey == "" || apiSecret == "" {
			slog.Error("--layer3-binance-testnet-journal-dir requires BINANCE_API_KEY and BINANCE_API_SECRET env vars (testnet credentials)",
				"symbol", cfg.Symbol)
			_ = notifier.SendStructured(ctx, notify.SeverityWarn,
				fmt.Sprintf("STARTUP FAILED on %s — --layer3-binance-testnet-journal-dir without BINANCE_API_KEY/BINANCE_API_SECRET env vars",
					cfg.Symbol))
			os.Exit(1)
		}
		blShadow := execution.NewBinanceLiveTestnet(cfg.Symbol, cfg.Strategy.StakeUSDT, apiKey, apiSecret)
		blShadow.JournalPath = *layer3TestnetJournalDir
		blShadow.FeeBps = *feeBps
		blShadow.StopSlippageBps = *stopSlippageBps
		blShadow.MaxHoldHours = *maxHoldHours
		blShadow.Notifier = notifier
		blShadow.PositionReconciler.Notifier = notifier

		// Same recovery + drift-block flow as the testnet executor mode.
		if recovered, err := blShadow.RecoverFromJournal(ctx); err != nil {
			if errors.Is(err, execution.ErrRecoveryDrift) {
				slog.Error("STARTUP BLOCKED: layer3 shadow recovery drift — operator must reconcile + restart",
					"symbol", cfg.Symbol, "err", err)
				_ = notifier.SendStructured(ctx, notify.SeverityWarn,
					fmt.Sprintf("STARTUP BLOCKED on %s (LAYER 3) — shadow recovery drift\n%v",
						cfg.Symbol, err))
				os.Exit(2)
			}
			slog.Error("layer3 shadow recovery failed (non-drift)", "err", err, "symbol", cfg.Symbol)
			_ = notifier.SendStructured(ctx, notify.SeverityWarn,
				fmt.Sprintf("STARTUP FAILED on %s (LAYER 3) — shadow recovery error\n%v", cfg.Symbol, err))
			os.Exit(2)
		} else if recovered {
			slog.Info("layer3 shadow position recovered + verified clean", "symbol", cfg.Symbol)
		}

		exec = &execution.TeeExecutor{Primary: exec, Shadow: blShadow}
		liveBinance = blShadow // ensures the PositionReconciler goroutine starts for the testnet shadow

		slog.Warn("LAYER 3 SHADOW MODE ACTIVE — paper Stub primary + BinanceLive(testnet) shadow on same ticks",
			"symbol", cfg.Symbol, "shadow_journal_dir", *layer3TestnetJournalDir, "api_base", execution.TestnetAPIBaseURL)
		_ = notifier.SendStructured(ctx, notify.SeverityWarn,
			fmt.Sprintf("LAYER 3 shadow mode started on %s\nstub primary + testnet shadow on identical ticks\nshadow_journal_dir: %s\nhost: %s",
				cfg.Symbol, *layer3TestnetJournalDir, hostname))
	}

	// Parse shadow specs first so we know how many runners to fan-out to.
	shadowSpecs, err := strategy.ParseShadowSpecs(*shadowFlag)
	if err != nil {
		slog.Error("invalid --shadow spec", "err", err, "shadow", *shadowFlag)
		// Telegram CRITICAL — operator passed a malformed --shadow spec;
		// engine has already announced "engine started" and is mid-startup.
		// Silent failure here means the operator might think the shadow
		// got registered when it didn't.
		_ = notifier.SendStructured(context.Background(), notify.SeverityCritical,
			fmt.Sprintf("STARTUP FAILED on %s — invalid --shadow spec\nspec: %q\nerror: %v",
				cfg.Symbol, *shadowFlag, err))
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
		if hist, ok := fundingProvider.(*funding.Historical); ok {
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
			FundingProvider:  fundingProvider, // share — provider is read-only
		}
		if fundingProvider != nil {
			shadowExec.FundingBpsPerDay = 0
		}

		// Same recovery as live — each shadow has its own journal at
		// shadow/<label>/, so its own orphan-open class.
		if recovered, err := shadowExec.RecoverFromJournal(); err != nil {
			slog.Warn("shadow position recovery failed",
				"err", err, "symbol", cfg.Symbol, "label", spec.Label)
			_ = notifier.SendStructured(ctx, notify.SeverityWarn,
				fmt.Sprintf("shadow recovery FAILED\nsymbol: %s\ncohort: %s\nerror: %v",
					cfg.Symbol, spec.Label, err))
		} else if recovered {
			slog.Info("shadow position recovery: in-flight trade restored",
				"symbol", cfg.Symbol, "label", spec.Label)
			_ = notifier.SendStructured(ctx, notify.SeverityWarn,
				fmt.Sprintf("shadow position recovered from journal\nsymbol: %s\ncohort: %s",
					cfg.Symbol, spec.Label))
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
	hb.Notifier = notifier                              // wire Telegram WARN alerts on stale-feed detection
	hb.StartupGrace = marketdata.DefaultStartupGrace    // suppress Warn alerts during the post-restart WS→REST fallback gap

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

	// PositionReconciler periodic loop — only when real-money executor is
	// active. Reconciler.Run does an initial reconcile then ticks at 60s,
	// alerting CRITICAL on the leading edge of any local-vs-exchange
	// divergence and gating future OnSignal calls via IsDrifted.
	if liveBinance != nil {
		g.Go(func() error {
			_ = liveBinance.PositionReconciler.Run(gctx, cfg.Symbol)
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		slog.Error("engine error", "err", err, "symbol", cfg.Symbol)
		// Telegram CRITICAL — engine has been running and a goroutine
		// returned a non-nil error. Currently this path is mostly
		// reached only via panic propagation through errgroup (the
		// individual goroutines either return nil or context.Canceled
		// which is filtered by errgroup), but the gap-coverage of the
		// alert is still important: an undocumented future error path
		// that surfaces here would otherwise log to /var/log only.
		_ = notifier.SendStructured(context.Background(), notify.SeverityCritical,
			fmt.Sprintf("ENGINE CRASHED on %s\nerror: %v\nThis is the errgroup error path; investigate logs for which goroutine surfaced the error.",
				cfg.Symbol, err))
		os.Exit(1)
	}

	slog.Info("engine stopped")
	_ = notifier.SendStructured(context.Background(), notify.SeverityInfo,
		fmt.Sprintf("engine stopped (clean)\nsymbol: %s", cfg.Symbol))
}

// validateExecutorArgs is the early-fail validator for --executor and
// --layer3-binance-testnet-journal-dir. Returns nil if the combo is
// valid, error otherwise. Caller is expected to slog.Error + Telegram-
// alert + os.Exit(1) on non-nil. Pure function so the cmd/engine CLI
// tests can exercise the flag-validation surface without wiring up
// marketdata, notifier, or config.
func validateExecutorArgs(executorMode, layer3JournalDir string) error {
	switch executorMode {
	case "stub", "":
		// no creds required for paper-money default
	case "binance_live", "binance_live_testnet":
		if os.Getenv("BINANCE_API_KEY") == "" || os.Getenv("BINANCE_API_SECRET") == "" {
			return fmt.Errorf("--executor=%s requires BINANCE_API_KEY and BINANCE_API_SECRET env vars (testnet mode uses SEPARATE credentials from mainnet)", executorMode)
		}
	default:
		return fmt.Errorf("invalid --executor=%q; must be 'stub', 'binance_live_testnet', or 'binance_live'", executorMode)
	}

	if layer3JournalDir != "" {
		if executorMode != "stub" && executorMode != "" {
			return fmt.Errorf("--layer3-binance-testnet-journal-dir requires --executor=stub (cannot wrap a real-money primary in TeeExecutor)")
		}
		if os.Getenv("BINANCE_API_KEY") == "" || os.Getenv("BINANCE_API_SECRET") == "" {
			return fmt.Errorf("--layer3-binance-testnet-journal-dir requires BINANCE_API_KEY and BINANCE_API_SECRET env vars (testnet credentials)")
		}
	}
	return nil
}
