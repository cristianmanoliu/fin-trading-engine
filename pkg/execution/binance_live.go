package execution

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/cristianmanoliu/trading-engine/pkg/models"
	"github.com/cristianmanoliu/trading-engine/pkg/notify"
)

// ErrStageNotPromoted is returned by every BinanceLive method that would
// otherwise interact with the exchange. The skeleton makes accidental
// activation safe — a misconfigured engine that wires BinanceLive instead
// of Stub will fail loudly on the first signal rather than send a real
// order.
//
// Real implementation lands at STAGE_1 promotion per the locked design
// in `results/real_money_executor_architecture_decision_rule_2026-05-08.md`.
var ErrStageNotPromoted = errors.New("BinanceLive skeleton: real implementation not yet built; engine must run with Stub until STAGE_1 promotion (see results/real_money_executor_architecture_decision_rule_2026-05-08.md)")

// BinanceLive is the real-money executor that replaces Stub at STAGE_1
// promotion. SKELETON — every exchange-interacting method returns
// ErrStageNotPromoted. The skeleton's purpose is to lock the package
// structure + Executor interface satisfaction in compilable code so
// future-self at STAGE_1 has an unambiguous extension point.
//
// Five-component decomposition per the locked design:
//   - BinanceLive itself: implements pkg/strategy.Executor
//   - OrderRouter:        sends orders, reports results, stateless
//   - PositionReconciler: periodically syncs local vs exchange state
//   - SafetyGates:        pre-order checks (max-position, daily-loss, entry-price)
//   - KillSwitch:         immediate market-close-all entry point
//
// All five components are constructed by NewBinanceLive but their methods
// error until real implementation lands.
type BinanceLive struct {
	Symbol    string
	StakeUSD  float64
	APIKey    string
	APISecret string

	// Notifier is opt-in. When set, BinanceLive emits SeverityCritical
	// alerts on every accidental method call (helps catch misconfiguration
	// on real-money systems where silent skeleton-mode would be dangerous).
	Notifier *notify.Notifier

	// Components. Constructed but not functional in skeleton mode.
	OrderRouter        *OrderRouter
	PositionReconciler *PositionReconciler
	SafetyGates        *SafetyGates
	KillSwitch         *KillSwitch
}

// NewBinanceLive constructs a BinanceLive with default-initialized components.
// Construction is safe — only method calls error.
func NewBinanceLive(symbol string, stakeUSD float64, apiKey, apiSecret string) *BinanceLive {
	bl := &BinanceLive{
		Symbol:    symbol,
		StakeUSD:  stakeUSD,
		APIKey:    apiKey,
		APISecret: apiSecret,
		OrderRouter: &OrderRouter{
			APIBaseURL: "https://fapi.binance.com",
		},
		PositionReconciler: &PositionReconciler{
			PollInterval: 60 * time.Second,
		},
		SafetyGates: &SafetyGates{
			// Defaults sized for STAGE_1; real activation overrides per stage.
			MaxPositionMultiple: 1, // one position per symbol — matches paper
			DailyLossUSDCap:     1000,
			MaxEntrySpreadBps:   50,
		},
		KillSwitch: &KillSwitch{},
	}
	bl.KillSwitch.Router = bl.OrderRouter
	return bl
}

// OnSignal is called by the strategy runner when a new signal is generated.
// Real implementation: pre-order check via SafetyGates, then SendOrder via
// OrderRouter, then journal the open. Skeleton: log + alert + drop the signal.
func (b *BinanceLive) OnSignal(sig *models.Signal) {
	slog.Error("BinanceLive.OnSignal on skeleton — signal dropped (engine must use Stub until STAGE_1)",
		"symbol", b.Symbol,
		"side", sig.Side,
		"entry", sig.EntryPrice,
		"reason", sig.Reason)
	if b.Notifier != nil {
		_ = b.Notifier.SendStructured(context.Background(), notify.SeverityCritical,
			fmt.Sprintf("BinanceLive skeleton called OnSignal — engine misconfigured\nsymbol: %s\nside: %v\nentry: %v\n%s",
				b.Symbol, sig.Side, sig.EntryPrice, ErrStageNotPromoted))
	}
}

// OnTick is called for every tick. Real implementation: feed PositionReconciler
// for periodic exchange-state sync. Skeleton: no-op (logging at tick rate would
// be too noisy; the OnSignal alert covers misconfiguration).
func (b *BinanceLive) OnTick(_ models.Tick) {
	// No-op in skeleton.
}

// Summary writes the end-of-run report. Real implementation: aggregate
// realized fills from journal + reconciler reports. Skeleton: log only.
func (b *BinanceLive) Summary() {
	slog.Info("BinanceLive.Summary on skeleton — no real-money trades to report",
		"symbol", b.Symbol)
}

// ── Component skeletons ─────────────────────────────────────────────────────

// OrderRouter sends orders to Binance USDT-M Futures. Stateless — every
// SendOrder is independent. Real implementation will use a signed HTTP
// client; skeleton has placeholder fields.
type OrderRouter struct {
	APIBaseURL string
	// HTTPClient *http.Client (deferred to implementation per pre-reg)
	// signer for HMAC request signing (deferred)
}

// OrderIntent is the input to SendOrder. Captures what the strategy wants
// without prescribing the wire format (deferred to implementation).
type OrderIntent struct {
	Symbol      string
	Side        models.Direction
	Quantity    float64 // contracts
	Type        string  // "MARKET" at STAGE_1-2; "LIMIT_IOC" considered at STAGE_3+
	LimitPrice  float64 // only used when Type == "LIMIT_IOC"
	ReduceOnly  bool    // true for closing orders
}

// OrderResult is the outcome of SendOrder. Always populated even on partial
// fills or rejections; the FilledQty field disambiguates.
type OrderResult struct {
	OrderID    string
	Status     string  // "FILLED" | "PARTIAL" | "REJECTED" | "ERROR"
	FilledQty  float64
	AvgPrice   float64
	FeeUSD     float64 // exchange-reported actual fee
	RejectCode string  // populated when Status == "REJECTED" or "ERROR"
}

// SendOrder posts an order to the exchange. Skeleton: errors.
func (r *OrderRouter) SendOrder(_ context.Context, _ OrderIntent) (OrderResult, error) {
	return OrderResult{}, ErrStageNotPromoted
}

// PositionReconciler runs a background loop that periodically queries the
// exchange for the current position state on each deployed symbol and
// compares it to the local view. On mismatch, raises an alert and
// triggers SOFT pause via the locked per_symbol_pause rule.
type PositionReconciler struct {
	PollInterval time.Duration

	// Local position view per symbol. Populated by BinanceLive's OnSignal
	// (open) and close-event handler. Mutex-protected because Run() reads
	// it from a separate goroutine.
	mu        sync.Mutex
	positions map[string]reconcilerPosition
}

type reconcilerPosition struct {
	side       models.Direction
	qty        float64
	avgEntry   float64
	openedAt   time.Time
}

// Run starts the reconciliation loop. Skeleton: errors immediately rather
// than starting a real polling loop.
func (r *PositionReconciler) Run(_ context.Context, _ string) error {
	return ErrStageNotPromoted
}

// SafetyGates enforces pre-order checks per the locked design. Each gate
// blocks an order before it reaches the exchange.
//
// Gate A: total $-notional per symbol must be ≤ MaxPositionMultiple × stake.
// Gate B: rolling 24h realized losses must be ≤ DailyLossUSDCap.
// Gate C: signal entry price must be within MaxEntrySpreadBps of current
//         best-bid/best-ask at order time.
type SafetyGates struct {
	MaxPositionMultiple float64 // Gate A: 1 at STAGE_1-4 per pre-reg
	DailyLossUSDCap     float64 // Gate B: $1000 STAGE_1, $3600 STAGE_2, etc.
	MaxEntrySpreadBps   float64 // Gate C: 50 bps default
}

// CheckOrder runs all three gates in order. Returns the first failing gate
// or nil if all pass. Skeleton: errors with ErrStageNotPromoted.
func (g *SafetyGates) CheckOrder(_ OrderIntent, _, _, _ float64) error {
	return ErrStageNotPromoted
}

// KillSwitch is the immediate market-close-all entry point. Used by:
//   - results/auto_kill_execution_decision_rule_2026-05-08.md Phase 2 Path C
//   - TRIAGE-C investigation when escalating to active kill
//   - operator emergency: scripts/kill_switch.sh CONFIRM
//
// Bypasses SafetyGates by design — KillSwitch fires under conditions where
// gates would interfere (e.g., post-Gate-B trip the kill is exactly what
// should happen).
type KillSwitch struct {
	Router *OrderRouter
}

// KillAll synchronously closes every open position via market orders.
// Best-effort: if one symbol's close fails, the others still close;
// the failure is reported in the result. Idempotent — re-running is
// safe (closes already-closed = no-op).
//
// Skeleton: errors immediately. Real implementation will iterate the
// reconciler's positions map, send a market-close OrderIntent per
// position, and return the aggregated result.
func (k *KillSwitch) KillAll(_ context.Context, _ string) error {
	return ErrStageNotPromoted
}
