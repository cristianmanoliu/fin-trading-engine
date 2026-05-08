// Command kill_switch is the operator-facing emergency real-money market-close-all
// tool. It implements Path C (real-money market close) of the locked auto-kill
// execution rule (results/auto_kill_execution_decision_rule_2026-05-08.md
// Phase 2) by sending reduceOnly MARKET orders for each operator-supplied
// position via execution.KillSwitch.KillAll.
//
// Usage:
//
//	kill_switch --reason <slug> --positions "<sym>,<side>,<qty>;..."         # dry-run
//	kill_switch --reason <slug> --positions "<sym>,<side>,<qty>;..." CONFIRM # live
//
// The positional CONFIRM gate prevents accidental fire — without it, the tool
// prints what it would close and exits 0 without contacting Binance.
//
// Environment (CONFIRM mode):
//
//	BINANCE_API_KEY     required
//	BINANCE_API_SECRET  required
//	TELEGRAM_BOT_TOKEN  optional — alerts on fire (CRITICAL severity)
//	TELEGRAM_CHAT_ID    optional
//	BINANCE_API_BASE    optional override (default https://fapi.binance.com;
//	                    set to https://testnet.binancefuture.com for testnet)
//
// Per the locked rule's "Phase 2: OPEN POSITIONS" section, the operator
// is responsible for enumerating the open positions to close (typically by
// running scripts/forward_paper_status.sh and reading the Open positions
// summary). This tool deliberately does NOT auto-discover via positionRisk —
// the operator's explicit list IS the human-in-the-loop safety gate that the
// locked rule requires (the "Auto-execute via cron" alternative was rejected).
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/cristianmanoliu/trading-engine/pkg/execution"
	"github.com/cristianmanoliu/trading-engine/pkg/notify"
)

const usageText = `kill_switch — emergency real-money market-close-all (Path C of auto-kill rule)

USAGE
  kill_switch --reason <slug> --positions "<spec>"            # dry-run
  kill_switch --reason <slug> --positions "<spec>" CONFIRM    # live

POSITION SPEC
  <sym>,<side>,<qty>;<sym>,<side>,<qty>;...
  side = LONG | SHORT (case-insensitive)
  qty  = positive float in contracts (NOT $-notional)

EXAMPLE
  kill_switch --reason drift_kill_2026-05-08 \
              --positions "BTCUSDT,LONG,0.5;ETHUSDT,SHORT,2.0" CONFIRM

ENV (CONFIRM mode only)
  BINANCE_API_KEY      required
  BINANCE_API_SECRET   required
  TELEGRAM_BOT_TOKEN   optional (alerts on fire)
  TELEGRAM_CHAT_ID     optional
  BINANCE_API_BASE     optional override (testnet: https://testnet.binancefuture.com)

The CONFIRM positional gate prevents accidental fire — without it, this tool
prints what it would close and exits without contacting Binance.

Operator responsibility (per auto_kill_execution_decision_rule Phase 2):
  - Enumerate open positions (e.g., scripts/forward_paper_status.sh)
  - Decide kill scope (live cohort? all cohorts including shadow?)
  - Run this tool with the explicit list — auto-discovery is intentionally
    NOT supported (human-in-the-loop is the locked safety gate).
`

func main() {
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, usageText)
		fmt.Fprintln(os.Stderr)
		flag.PrintDefaults()
	}

	var (
		reason    = flag.String("reason", "", "kill reason slug for the artifact (e.g., drift_kill_2026-05-08)")
		positions = flag.String("positions", "", "semicolon-separated <sym>,<side>,<qty> tuples to close")
	)
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	if *reason == "" {
		fmt.Fprintln(os.Stderr, "ERROR: --reason is required (kill artifact slug)")
		os.Exit(1)
	}
	if *positions == "" {
		fmt.Fprintln(os.Stderr, "ERROR: --positions is required (use --help for spec format)")
		os.Exit(1)
	}

	closes, err := execution.ParseClosePositionSpec(*positions)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: --positions parse failed: %v\n", err)
		os.Exit(1)
	}

	confirm := flag.Arg(0) == "CONFIRM"

	if !confirm {
		fmt.Println("DRY RUN — no orders will be sent. Append 'CONFIRM' to fire.")
		fmt.Printf("reason: %s\n", *reason)
		fmt.Printf("positions to close (%d):\n", len(closes))
		for _, c := range closes {
			fmt.Printf("  %-12s %-5s qty=%v\n", c.Symbol, c.Side, c.Quantity)
		}
		return
	}

	apiKey := os.Getenv("BINANCE_API_KEY")
	apiSecret := os.Getenv("BINANCE_API_SECRET")
	if apiKey == "" || apiSecret == "" {
		fmt.Fprintln(os.Stderr, "ERROR: CONFIRM mode requires BINANCE_API_KEY and BINANCE_API_SECRET env vars")
		os.Exit(1)
	}
	apiBase := os.Getenv("BINANCE_API_BASE")
	if apiBase == "" {
		apiBase = "https://fapi.binance.com"
	}

	notifier := notify.FromEnv()
	hostname, _ := os.Hostname()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Pre-fire alert: announce intent + scope BEFORE any order goes out.
	// CRITICAL severity — never muted, no rate limit per the locked telegram
	// alert design rule.
	preFireMsg := fmt.Sprintf("KILL SWITCH FIRING\nreason: %s\nhost: %s\npositions: %d\nbase: %s",
		*reason, hostname, len(closes), apiBase)
	for _, c := range closes {
		preFireMsg += fmt.Sprintf("\n  %s %s qty=%v", c.Symbol, c.Side, c.Quantity)
	}
	if err := notifier.SendStructured(ctx, notify.SeverityCritical, preFireMsg); err != nil {
		slog.Warn("pre-fire alert failed; proceeding anyway", "err", err)
	}

	router := &execution.OrderRouter{
		APIBaseURL: apiBase,
		APIKey:     apiKey,
		APISecret:  apiSecret,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
		RecvWindow: 5000,
	}
	ks := &execution.KillSwitch{Router: router}

	slog.Warn("KILL SWITCH FIRING — sending reduceOnly MARKET orders to Binance",
		"reason", *reason, "positions", len(closes), "api_base", apiBase)

	result, killErr := ks.KillAll(ctx, closes, *reason)

	// Per-symbol outcome — printed as a structured operator artifact and
	// also written to the kill notification.
	fmt.Printf("\n=== KILL RESULT — reason: %s ===\n", result.Reason)
	postFireMsg := fmt.Sprintf("KILL RESULT — %s", result.Reason)
	failures := 0
	for _, o := range result.Outcomes {
		line := fmt.Sprintf("%-12s %-7s requested=%v filled=%v avg_price=%v",
			o.Symbol, o.Status, o.Requested, o.Filled, o.AvgPrice)
		if o.Error != "" {
			line += "  ERR: " + o.Error
			failures++
		}
		fmt.Println("  " + line)
		postFireMsg += "\n  " + line
	}

	// Post-fire alert with full per-symbol detail.
	if err := notifier.SendStructured(ctx, notify.SeverityCritical, postFireMsg); err != nil {
		slog.Warn("post-fire alert failed", "err", err)
	}

	if killErr != nil {
		fmt.Fprintf(os.Stderr, "\nKILL had %d failures: %v\n", failures, killErr)
		fmt.Fprintln(os.Stderr, "Operator must investigate failed symbols on Binance UI before considering the kill complete.")
		os.Exit(1)
	}
	fmt.Println("\nAll positions closed cleanly. Record this output in results/kill_<date>_<reason>.md per Phase 4.")
}
