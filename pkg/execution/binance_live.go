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
			PollInterval:       60 * time.Second,
			APIBaseURL:         "https://fapi.binance.com",
			APIKey:             apiKey,
			APISecret:          apiSecret,
			HTTPClient:         &http.Client{Timeout: 10 * time.Second},
			RecvWindow:         5000,
			QtyTolerance:       0.01, // locked: drift fires at |Δqty| ≥ 0.01 contracts
			EntryPriceBpsLimit: 10,   // locked: drift fires at |Δentry|/entry ≥ 10 bps
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
// compares it to the local view. On mismatch, raises an alert and BLOCKS
// new orders on the affected symbol until an operator clears the drift
// state via ClearDrift (after manual root-cause investigation per the
// locked per_symbol_pause_decision_rule).
//
// Drift detection (locked thresholds):
//   - |position-size mismatch| ≥ QtyTolerance (default 0.01 contracts)
//   - OR side mismatch (one side LONG, the other SHORT or none)
//   - OR |entry-price drift| ≥ EntryPriceBpsLimit (default 10 bps)
//
// Reconciliation does NOT auto-correct; it ALERTS. The operator decides
// whether to fix local-state-to-match-exchange or
// fix-exchange-state-to-match-local based on root-cause investigation.
type PositionReconciler struct {
	PollInterval time.Duration

	// API config — same shape as OrderRouter; populated by NewBinanceLive.
	APIBaseURL string
	APIKey     string
	APISecret  string
	HTTPClient *http.Client
	RecvWindow int64 // ms; default 5000 if zero

	// Drift detection thresholds. NewBinanceLive sets locked defaults
	// (0.01 contracts, 10 bps); production may tune via configuration but
	// re-locking is an explicit decision.
	QtyTolerance       float64
	EntryPriceBpsLimit float64

	Notifier *notify.Notifier

	// Local position view + drift map per symbol. Populated by
	// SetLocalPosition / ClearLocalPosition (the BinanceLive-side write
	// path) and by ReconcileSymbol (the drift-set/clear path).
	mu        sync.Mutex
	positions map[string]reconcilerPosition
	drifted   map[string]string // symbol → drift reason; absent or "" = clean
}

type reconcilerPosition struct {
	side     models.Direction
	qty      float64
	avgEntry float64
	openedAt time.Time
}

// ExchangePosition is the parsed result of a /fapi/v2/positionRisk query.
// Qty is signed: positive=Long, negative=Short, zero=flat. Side is derived
// from the sign for callers that prefer the enum.
type ExchangePosition struct {
	Qty      float64
	AvgEntry float64
	Side     models.Direction
}

// DriftReport is the per-symbol output of a single ReconcileSymbol pass.
type DriftReport struct {
	Symbol           string
	Drifted          bool
	Reason           string // empty when Drifted=false
	HasLocal         bool
	LocalSide        models.Direction
	LocalQty         float64
	LocalAvgEntry    float64
	HasExchange      bool
	ExchangeSide     models.Direction
	ExchangeQty      float64 // absolute (always non-negative)
	ExchangeAvgEntry float64
}

// SetLocalPosition records BinanceLive's local view of a position. Called
// by OnSignal after an open is journaled, and by close-event handling.
func (r *PositionReconciler) SetLocalPosition(symbol string, side models.Direction, qty, avgEntry float64, openedAt time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.positions == nil {
		r.positions = make(map[string]reconcilerPosition)
	}
	r.positions[symbol] = reconcilerPosition{
		side:     side,
		qty:      qty,
		avgEntry: avgEntry,
		openedAt: openedAt,
	}
}

// ClearLocalPosition removes the local record. Called after a close is
// journaled (the position is gone — the next reconcile should see flat-on-both
// = clean).
func (r *PositionReconciler) ClearLocalPosition(symbol string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.positions, symbol)
}

// LocalPosition returns the locally-tracked position; ok=false when nothing
// is recorded for the symbol.
func (r *PositionReconciler) LocalPosition(symbol string) (side models.Direction, qty, avgEntry float64, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	pos, found := r.positions[symbol]
	if !found {
		return models.Neutral, 0, 0, false
	}
	return pos.side, pos.qty, pos.avgEntry, true
}

// IsDrifted reports whether the symbol is currently in drift. BinanceLive.OnSignal
// MUST gate on this BEFORE SafetyGates — sending orders on a drifted symbol
// risks compounding the discrepancy.
func (r *PositionReconciler) IsDrifted(symbol string) (bool, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	reason, ok := r.drifted[symbol]
	if !ok || reason == "" {
		return false, ""
	}
	return true, reason
}

// ClearDrift unsets the drift flag. Operator-invoked after manual
// reconciliation (e.g., closing the orphan position via Binance UI, or
// updating local journal state). NOT auto-called — drift is sticky until
// human intervention OR until a subsequent reconcile naturally finds the
// state has converged.
func (r *PositionReconciler) ClearDrift(symbol string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.drifted, symbol)
}

// FetchExchangePosition queries Binance USDT-M Futures /fapi/v2/positionRisk
// for the symbol's current position. Returns ExchangePosition with signed Qty
// (positive=Long, negative=Short). On hedge-mode accounts the response can
// contain LONG and SHORT entries separately; we sum them so net exposure is
// reported coherently regardless of position-mode.
func (r *PositionReconciler) FetchExchangePosition(ctx context.Context, symbol string) (ExchangePosition, error) {
	if r.APIKey == "" || r.APISecret == "" {
		return ExchangePosition{}, fmt.Errorf("PositionReconciler not configured: APIKey/APISecret missing")
	}
	if r.HTTPClient == nil {
		return ExchangePosition{}, fmt.Errorf("PositionReconciler not configured: HTTPClient nil")
	}
	recvWin := r.RecvWindow
	if recvWin == 0 {
		recvWin = 5000
	}

	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("recvWindow", strconv.FormatInt(recvWin, 10))
	params.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))
	queryStr := params.Encode()
	signature := hmacSHA256(queryStr, r.APISecret)
	fullURL := r.APIBaseURL + "/fapi/v2/positionRisk?" + queryStr + "&signature=" + signature

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return ExchangePosition{}, err
	}
	req.Header.Set("X-MBX-APIKEY", r.APIKey)

	resp, err := r.HTTPClient.Do(req)
	if err != nil {
		return ExchangePosition{}, fmt.Errorf("network: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return ExchangePosition{}, fmt.Errorf("positionRisk %d: %s", resp.StatusCode, body)
	}

	var entries []struct {
		Symbol       string `json:"symbol"`
		PositionAmt  string `json:"positionAmt"`
		EntryPrice   string `json:"entryPrice"`
		PositionSide string `json:"positionSide"`
	}
	if err := json.Unmarshal(body, &entries); err != nil {
		return ExchangePosition{}, fmt.Errorf("parse positionRisk: %w", err)
	}

	// Sum signed positionAmt across entries (one in one-way mode; up to two
	// in hedge mode). Average entry is notional-weighted.
	var netQty, weightedNotional float64
	for _, e := range entries {
		amt, _ := strconv.ParseFloat(e.PositionAmt, 64)
		entry, _ := strconv.ParseFloat(e.EntryPrice, 64)
		netQty += amt
		weightedNotional += math.Abs(amt) * entry
	}

	pos := ExchangePosition{Qty: netQty}
	if math.Abs(netQty) > 0 {
		pos.AvgEntry = weightedNotional / math.Abs(netQty)
	}
	switch {
	case netQty > 0:
		pos.Side = models.Long
	case netQty < 0:
		pos.Side = models.Short
	default:
		pos.Side = models.Neutral
	}
	return pos, nil
}

// ReconcileSymbol runs one reconciliation pass: fetch exchange state, compare
// to local, set/clear drift, alert on the leading edge of a drift episode.
//
// "Leading edge" = drift just transitioned from clean → drifted. Repeated
// drift on consecutive passes does NOT re-alert (locked: prevent operator
// alert-fatigue from a single unresolved drift).
func (r *PositionReconciler) ReconcileSymbol(ctx context.Context, symbol string) (DriftReport, error) {
	exch, err := r.FetchExchangePosition(ctx, symbol)
	if err != nil {
		return DriftReport{Symbol: symbol}, err
	}

	r.mu.Lock()
	local, hasLocal := r.positions[symbol]
	wasDrifted := r.drifted[symbol] != ""
	r.mu.Unlock()

	report := DriftReport{
		Symbol:      symbol,
		HasLocal:    hasLocal,
		HasExchange: math.Abs(exch.Qty) >= r.QtyTolerance,
	}
	if hasLocal {
		report.LocalSide = local.side
		report.LocalQty = local.qty
		report.LocalAvgEntry = local.avgEntry
	}
	if report.HasExchange {
		report.ExchangeSide = exch.Side
		report.ExchangeQty = math.Abs(exch.Qty)
		report.ExchangeAvgEntry = exch.AvgEntry
	}

	report.Drifted, report.Reason = detectDrift(hasLocal, local, report.HasExchange, exch, r.QtyTolerance, r.EntryPriceBpsLimit)

	r.mu.Lock()
	if report.Drifted {
		if r.drifted == nil {
			r.drifted = make(map[string]string)
		}
		r.drifted[symbol] = report.Reason
	} else if wasDrifted {
		delete(r.drifted, symbol)
	}
	r.mu.Unlock()

	switch {
	case report.Drifted && !wasDrifted:
		slog.Error("position drift detected",
			"symbol", symbol,
			"reason", report.Reason,
			"local_side", report.LocalSide, "local_qty", report.LocalQty, "local_entry", report.LocalAvgEntry,
			"exchange_side", report.ExchangeSide, "exchange_qty", report.ExchangeQty, "exchange_entry", report.ExchangeAvgEntry)
		if r.Notifier != nil {
			_ = r.Notifier.SendStructured(ctx, notify.SeverityCritical,
				fmt.Sprintf("Position drift on %s — engine BLOCKED for new orders\nreason: %s\nlocal: side=%v qty=%v entry=%v\nexchange: side=%v qty=%v entry=%v\nOperator must reconcile + ClearDrift",
					symbol, report.Reason,
					report.LocalSide, report.LocalQty, report.LocalAvgEntry,
					report.ExchangeSide, report.ExchangeQty, report.ExchangeAvgEntry))
		}
	case !report.Drifted && wasDrifted:
		slog.Info("position drift cleared", "symbol", symbol)
		if r.Notifier != nil {
			_ = r.Notifier.SendStructured(ctx, notify.SeverityInfo,
				fmt.Sprintf("Position drift cleared on %s — local + exchange now agree", symbol))
		}
	}

	return report, nil
}

// detectDrift is the pure-arithmetic decision: given local + exchange state
// and the locked thresholds, produce drifted-bool + human-readable reason.
// Extracted so the rule is testable without HTTP plumbing.
func detectDrift(hasLocal bool, local reconcilerPosition, hasExch bool, exch ExchangePosition, qtyTol, entryBpsLimit float64) (bool, string) {
	switch {
	case !hasLocal && !hasExch:
		return false, ""
	case !hasLocal && hasExch:
		return true, fmt.Sprintf("orphan exchange position: side=%v qty=%v entry=%v (local empty)",
			exch.Side, math.Abs(exch.Qty), exch.AvgEntry)
	case hasLocal && !hasExch:
		return true, fmt.Sprintf("externally-closed: local side=%v qty=%v entry=%v (exchange flat)",
			local.side, local.qty, local.avgEntry)
	}

	if local.side != exch.Side {
		return true, fmt.Sprintf("side mismatch: local=%v exchange=%v", local.side, exch.Side)
	}

	absExchQty := math.Abs(exch.Qty)
	qtyDelta := math.Abs(local.qty - absExchQty)
	if qtyDelta >= qtyTol {
		return true, fmt.Sprintf("qty drift: local=%v exchange=%v (Δ=%v ≥ tol=%v)",
			local.qty, absExchQty, qtyDelta, qtyTol)
	}

	if local.avgEntry > 0 {
		bpsDiff := math.Abs(local.avgEntry-exch.AvgEntry) / local.avgEntry * 10000
		if bpsDiff >= entryBpsLimit {
			return true, fmt.Sprintf("entry-price drift: local=%v exchange=%v (%.2f bps ≥ %.2f bps)",
				local.avgEntry, exch.AvgEntry, bpsDiff, entryBpsLimit)
		}
	}

	return false, ""
}

// effectivePollInterval clamps PollInterval to the locked [30s, 300s] band
// and substitutes the 60s default when unset. Exposed (lowercase) to permit
// direct unit tests of the clamp without spinning the loop.
func (r *PositionReconciler) effectivePollInterval() time.Duration {
	const (
		floor   = 30 * time.Second
		ceiling = 5 * time.Minute
		def     = 60 * time.Second
	)
	switch {
	case r.PollInterval == 0:
		return def
	case r.PollInterval < floor:
		return floor
	case r.PollInterval > ceiling:
		return ceiling
	default:
		return r.PollInterval
	}
}

// Run starts the periodic reconciliation loop for a single symbol. Errors
// loudly at entry if API config is missing — this is a real-money component;
// silent skeleton-mode would be dangerous. Otherwise it issues an initial
// reconcile (so operator visibility on engine restart doesn't lag the tick
// interval) then enters a ticker loop until ctx is canceled.
func (r *PositionReconciler) Run(ctx context.Context, symbol string) error {
	if r.APIKey == "" || r.APISecret == "" {
		return fmt.Errorf("PositionReconciler not configured: APIKey/APISecret missing")
	}
	if r.HTTPClient == nil {
		return fmt.Errorf("PositionReconciler not configured: HTTPClient nil")
	}

	// Initial reconcile — surface pre-existing exchange-side state immediately
	// (caller of Run is typically the engine startup goroutine).
	if _, err := r.ReconcileSymbol(ctx, symbol); err != nil {
		slog.Warn("reconcile error (initial)", "symbol", symbol, "err", err)
	}

	ticker := time.NewTicker(r.effectivePollInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if _, err := r.ReconcileSymbol(ctx, symbol); err != nil {
				slog.Warn("reconcile error", "symbol", symbol, "err", err)
			}
		}
	}
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
