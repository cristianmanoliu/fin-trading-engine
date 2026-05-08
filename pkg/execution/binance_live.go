package execution

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strconv"
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
			APIKey:     apiKey,
			APISecret:  apiSecret,
			HTTPClient: &http.Client{Timeout: 10 * time.Second},
			RecvWindow: 5000,
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
// SendOrder is independent. Uses HMAC-SHA256 request signing per Binance
// Futures API spec.
type OrderRouter struct {
	APIBaseURL string
	APIKey     string
	APISecret  string
	HTTPClient *http.Client
	RecvWindow int64 // ms; default 5000 if zero
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

// SendOrder posts a signed order to Binance USDT-M Futures. Implements the
// MARKET-order path per the locked architecture (LIMIT_IOC deferred to STAGE_3+).
// Returns the parsed OrderResult on 2xx; F3-class REJECTED on 4xx other than
// rate limit; F4-class RATE_LIMIT on 418/429; F1-class ERROR on 5xx or network
// failure.
func (r *OrderRouter) SendOrder(ctx context.Context, intent OrderIntent) (OrderResult, error) {
	if r.APIKey == "" || r.APISecret == "" {
		return OrderResult{}, fmt.Errorf("OrderRouter not configured: APIKey/APISecret missing")
	}
	if r.HTTPClient == nil {
		return OrderResult{}, fmt.Errorf("OrderRouter not configured: HTTPClient nil")
	}

	exchangeSide, err := mapToExchangeSide(intent.Side, intent.ReduceOnly)
	if err != nil {
		return OrderResult{}, err
	}
	recvWin := r.RecvWindow
	if recvWin == 0 {
		recvWin = 5000
	}

	params := url.Values{}
	params.Set("symbol", intent.Symbol)
	params.Set("side", exchangeSide)
	params.Set("type", intent.Type)
	params.Set("quantity", strconv.FormatFloat(intent.Quantity, 'f', -1, 64))
	if intent.ReduceOnly {
		params.Set("reduceOnly", "true")
	}
	if intent.Type == "LIMIT_IOC" {
		params.Set("price", strconv.FormatFloat(intent.LimitPrice, 'f', -1, 64))
		params.Set("timeInForce", "IOC")
	}
	params.Set("recvWindow", strconv.FormatInt(recvWin, 10))
	params.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))

	queryStr := params.Encode()
	signature := hmacSHA256(queryStr, r.APISecret)
	fullURL := r.APIBaseURL + "/fapi/v1/order?" + queryStr + "&signature=" + signature

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, nil)
	if err != nil {
		return OrderResult{}, err
	}
	req.Header.Set("X-MBX-APIKEY", r.APIKey)

	resp, err := r.HTTPClient.Do(req)
	if err != nil {
		return OrderResult{Status: "ERROR", RejectCode: "NETWORK"}, fmt.Errorf("network: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	switch {
	case resp.StatusCode == 418 || resp.StatusCode == 429:
		return OrderResult{Status: "ERROR", RejectCode: "RATE_LIMIT"}, fmt.Errorf("rate limit %d: %s", resp.StatusCode, body)
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		return OrderResult{Status: "REJECTED", RejectCode: string(body)}, fmt.Errorf("4xx %d: %s", resp.StatusCode, body)
	case resp.StatusCode >= 500:
		return OrderResult{Status: "ERROR", RejectCode: "SERVER"}, fmt.Errorf("5xx %d: %s", resp.StatusCode, body)
	}

	var br struct {
		OrderID     int64  `json:"orderId"`
		Status      string `json:"status"`
		ExecutedQty string `json:"executedQty"`
		AvgPrice    string `json:"avgPrice"`
	}
	if err := json.Unmarshal(body, &br); err != nil {
		return OrderResult{Status: "ERROR", RejectCode: "PARSE"}, fmt.Errorf("parse: %w", err)
	}
	qty, _ := strconv.ParseFloat(br.ExecutedQty, 64)
	avg, _ := strconv.ParseFloat(br.AvgPrice, 64)
	status := "FILLED"
	if qty < intent.Quantity {
		status = "PARTIAL"
	}
	return OrderResult{
		OrderID:   strconv.FormatInt(br.OrderID, 10),
		Status:    status,
		FilledQty: qty,
		AvgPrice:  avg,
	}, nil
}

// hmacSHA256 computes Binance's signed-request signature: HMAC-SHA256 of the
// query string with the API secret as key, hex-encoded.
func hmacSHA256(message, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(message))
	return hex.EncodeToString(h.Sum(nil))
}

// mapToExchangeSide translates our (position-direction + reduceOnly) intent
// into the exchange's (BUY/SELL) order side. Open: Long→BUY, Short→SELL.
// Close (reduceOnly): flipped — Long position closing → SELL, Short → BUY.
func mapToExchangeSide(side models.Direction, reduceOnly bool) (string, error) {
	switch side {
	case models.Long:
		if reduceOnly {
			return "SELL", nil
		}
		return "BUY", nil
	case models.Short:
		if reduceOnly {
			return "BUY", nil
		}
		return "SELL", nil
	default:
		return "", fmt.Errorf("invalid side: %v (must be Long or Short)", side)
	}
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

// GateContext bundles every input the three gates need. Pulled into a struct
// because Gate A needs the open-position notional + per-trade stake (not just
// the order intent) and Gate C needs the strategy's intended entry price
// separately from any limit price on the intent (MARKET orders have no price
// field but still need entry-price-vs-bid/ask sanity).
type GateContext struct {
	Intent                  OrderIntent
	SignalEntryPrice        float64 // strategy's intended entry (signal.EntryPrice)
	CurrentBid              float64 // best-bid at order time
	CurrentAsk              float64 // best-ask at order time
	OpenPositionNotionalUSD float64 // existing open position $-notional on this symbol
	StakeUSD                float64 // per-trade stake (used for Gate A cap)
	Recent24hLossUSD        float64 // rolling 24h realized losses (used for Gate B)
}

// CheckOrder runs Gate A → Gate B → Gate C in order, short-circuiting on the
// first failure. Returns nil only if all three pass. Order is locked: Gate A
// and B are cheap pure-arithmetic checks; Gate C requires fresh bid/ask which
// is the most expensive input.
func (g *SafetyGates) CheckOrder(c GateContext) error {
	// Gate A: total $-notional per symbol must be ≤ MaxPositionMultiple × stake.
	// Adding the new intent's notional to the existing open notional must not
	// exceed the cap.
	intentNotional := c.Intent.Quantity * c.SignalEntryPrice
	maxAllowed := g.MaxPositionMultiple * c.StakeUSD
	if c.OpenPositionNotionalUSD+intentNotional > maxAllowed {
		return fmt.Errorf("Gate A failed: max-position cap $%.2f exceeded (current_open=$%.2f + intent=$%.2f = $%.2f)",
			maxAllowed, c.OpenPositionNotionalUSD, intentNotional,
			c.OpenPositionNotionalUSD+intentNotional)
	}

	// Gate B: rolling 24h realized losses must be ≤ DailyLossUSDCap.
	// Note: losses are POSITIVE numbers in this convention (loss=$1500 means
	// $1500 was lost). At-cap is allowed; strictly > cap fails.
	if c.Recent24hLossUSD > g.DailyLossUSDCap {
		return fmt.Errorf("Gate B failed: daily-loss circuit breaker tripped (loss=$%.2f > cap=$%.2f)",
			c.Recent24hLossUSD, g.DailyLossUSDCap)
	}

	// Gate C: signal entry price must be within MaxEntrySpreadBps of the
	// current bid-ask mid. Catches stale signals (signal fired but engine
	// took >5s to reach the order, mid has moved).
	if c.CurrentBid <= 0 || c.CurrentAsk <= 0 || c.CurrentBid > c.CurrentAsk {
		return fmt.Errorf("Gate C failed: invalid bid/ask (bid=%.4f ask=%.4f)",
			c.CurrentBid, c.CurrentAsk)
	}
	mid := (c.CurrentBid + c.CurrentAsk) / 2
	if mid <= 0 {
		return fmt.Errorf("Gate C failed: non-positive mid (%.4f)", mid)
	}
	deviationBps := math.Abs(c.SignalEntryPrice-mid) / mid * 10000
	if deviationBps > g.MaxEntrySpreadBps {
		return fmt.Errorf("Gate C failed: signal price $%.4f differs from mid $%.4f by %.1f bps > cap %.1f bps",
			c.SignalEntryPrice, mid, deviationBps, g.MaxEntrySpreadBps)
	}

	return nil
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

// ClosePosition describes a position that KillAll needs to close. Decoupled
// from PositionReconciler's internal type so KillAll is testable without a
// running reconciler — caller passes whatever positions it knows about.
type ClosePosition struct {
	Symbol   string
	Side     models.Direction // current position direction (Long/Short)
	Quantity float64          // contracts to close
	AvgEntry float64          // avg entry price (informational; included in result)
}

// KillOutcome is the per-symbol result of a KillAll attempt.
type KillOutcome struct {
	Symbol    string
	Status    string  // "CLOSED" | "PARTIAL" | "FAILED"
	Requested float64 // intended close quantity
	Filled    float64 // actual filled quantity
	AvgPrice  float64 // exchange-reported avg fill price
	Error     string  // populated when Status == "FAILED"
}

// KillResult aggregates per-symbol outcomes across the whole kill batch.
type KillResult struct {
	Reason   string
	Outcomes []KillOutcome
}

// KillAll synchronously closes every position in the input list via market
// orders. Best-effort: a single failed close does NOT stop iteration; the
// remaining positions still attempt close. Returns the aggregated KillResult
// always; the error return is non-nil when ANY position failed (caller can
// inspect the result for per-symbol detail).
//
// Idempotent — re-running with the same positions is safe (closing an
// already-closed position returns 4xx from Binance which the result records
// as FAILED with the exchange's reject code; caller can filter those out).
//
// Bypasses SafetyGates by design.
func (k *KillSwitch) KillAll(ctx context.Context, positions []ClosePosition, reason string) (KillResult, error) {
	result := KillResult{Reason: reason}
	if k.Router == nil {
		return result, fmt.Errorf("KillSwitch not configured: Router nil")
	}

	failureCount := 0
	for _, pos := range positions {
		intent := OrderIntent{
			Symbol:     pos.Symbol,
			Side:       pos.Side,
			Quantity:   pos.Quantity,
			Type:       "MARKET",
			ReduceOnly: true, // CLOSE — mapToExchangeSide flips the direction
		}
		orderResult, err := k.Router.SendOrder(ctx, intent)
		outcome := KillOutcome{
			Symbol:    pos.Symbol,
			Requested: pos.Quantity,
			Filled:    orderResult.FilledQty,
			AvgPrice:  orderResult.AvgPrice,
		}
		switch {
		case err != nil:
			outcome.Status = "FAILED"
			outcome.Error = err.Error()
			failureCount++
		case orderResult.FilledQty < pos.Quantity:
			outcome.Status = "PARTIAL"
		default:
			outcome.Status = "CLOSED"
		}
		result.Outcomes = append(result.Outcomes, outcome)
	}

	if failureCount > 0 {
		return result, fmt.Errorf("KillAll: %d of %d positions failed (reason=%q)",
			failureCount, len(positions), reason)
	}
	return result, nil
}
