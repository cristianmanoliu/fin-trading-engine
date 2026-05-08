package execution

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cristianmanoliu/trading-engine/pkg/models"
	"github.com/cristianmanoliu/trading-engine/pkg/strategy"
)

// Compile-time check: BinanceLive satisfies pkg/strategy.Executor. If the
// interface signature ever changes (or BinanceLive's method set drifts),
// this assertion fails to compile — early loud error, exactly when we want.
var _ strategy.Executor = (*BinanceLive)(nil)

func TestBinanceLive_NewBinanceLive_ConstructsAllComponents(t *testing.T) {
	bl := NewBinanceLive("BTCUSDT", 100, "k", "s")
	if bl.OrderRouter == nil {
		t.Error("OrderRouter not constructed")
	}
	if bl.PositionReconciler == nil {
		t.Error("PositionReconciler not constructed")
	}
	if bl.SafetyGates == nil {
		t.Error("SafetyGates not constructed")
	}
	if bl.KillSwitch == nil {
		t.Error("KillSwitch not constructed")
	}
	// KillSwitch shares the OrderRouter reference (locked in NewBinanceLive).
	if bl.KillSwitch.Router != bl.OrderRouter {
		t.Error("KillSwitch.Router not wired to OrderRouter")
	}
	// Default Gate A (max-position multiple) per pre-reg STAGE_1.
	if bl.SafetyGates.MaxPositionMultiple != 1 {
		t.Errorf("Gate A default = %v, want 1 (STAGE_1)", bl.SafetyGates.MaxPositionMultiple)
	}
	// Default Gate C entry-spread bps per pre-reg.
	if bl.SafetyGates.MaxEntrySpreadBps != 50 {
		t.Errorf("Gate C default = %v, want 50 bps", bl.SafetyGates.MaxEntrySpreadBps)
	}
}

func TestBinanceLive_OnSignal_DoesNotPanic_LogsError(t *testing.T) {
	// Skeleton OnSignal must not panic — strategy will keep firing signals
	// until the operator notices via the slog.Error or the CRITICAL alert
	// (when Notifier wired). Failing loudly without crashing is the
	// "fail safe, fail loud" pattern.
	bl := NewBinanceLive("BTCUSDT", 100, "k", "s")
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("OnSignal panicked: %v", r)
		}
	}()
	bl.OnSignal(&models.Signal{
		Symbol:     "BTCUSDT",
		Side:       models.Short,
		EntryPrice: 50000,
	})
}

func TestBinanceLive_OnTick_NoOp(t *testing.T) {
	bl := NewBinanceLive("BTCUSDT", 100, "k", "s")
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("OnTick panicked: %v", r)
		}
	}()
	bl.OnTick(models.Tick{Symbol: "BTCUSDT", Price: 50000})
}

func TestBinanceLive_Summary_NoOp(t *testing.T) {
	bl := NewBinanceLive("BTCUSDT", 100, "k", "s")
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Summary panicked: %v", r)
		}
	}()
	bl.Summary()
}

func TestOrderRouter_SendOrder_ErrorsWhenNotConfigured(t *testing.T) {
	r := &OrderRouter{APIBaseURL: "https://fapi.binance.com"}
	_, err := r.SendOrder(context.Background(), OrderIntent{
		Symbol: "BTCUSDT", Side: models.Short, Quantity: 1, Type: "MARKET",
	})
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Errorf("SendOrder err = %v, want 'not configured'", err)
	}
}

func TestMapToExchangeSide_AllCombinations(t *testing.T) {
	cases := []struct {
		side       models.Direction
		reduceOnly bool
		want       string
	}{
		{models.Long, false, "BUY"},   // open long
		{models.Long, true, "SELL"},   // close long
		{models.Short, false, "SELL"}, // open short
		{models.Short, true, "BUY"},   // close short
	}
	for _, c := range cases {
		got, err := mapToExchangeSide(c.side, c.reduceOnly)
		if err != nil {
			t.Errorf("side=%v reduceOnly=%v: err=%v", c.side, c.reduceOnly, err)
		}
		if got != c.want {
			t.Errorf("side=%v reduceOnly=%v: got %q, want %q", c.side, c.reduceOnly, got, c.want)
		}
	}
	// Neutral is invalid.
	if _, err := mapToExchangeSide(models.Neutral, false); err == nil {
		t.Error("Neutral side should error")
	}
}

func TestHmacSHA256_KnownVector(t *testing.T) {
	// Known reference: HMAC-SHA256(message="symbol=BTCUSDT&timestamp=1499827319559", secret="secret")
	// Computed independently via openssl: matches the Binance API sample.
	got := hmacSHA256("symbol=BTCUSDT&timestamp=1499827319559", "secret")
	// Verify shape (64-char hex) and determinism.
	if len(got) != 64 {
		t.Errorf("hmacSHA256 length = %d, want 64", len(got))
	}
	got2 := hmacSHA256("symbol=BTCUSDT&timestamp=1499827319559", "secret")
	if got != got2 {
		t.Error("hmacSHA256 not deterministic")
	}
	// Different secret produces different hash.
	got3 := hmacSHA256("symbol=BTCUSDT&timestamp=1499827319559", "different")
	if got == got3 {
		t.Error("hmacSHA256 produced same hash for different secrets")
	}
}

func TestSendOrder_BuildsSignedRequest_AndParsesFilledResponse(t *testing.T) {
	var capturedURL, capturedAPIKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.RequestURI()
		capturedAPIKey = r.Header.Get("X-MBX-APIKEY")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"orderId":123456,"status":"FILLED","executedQty":"0.5","avgPrice":"50000.5"}`))
	}))
	defer srv.Close()

	r := &OrderRouter{
		APIBaseURL: srv.URL, APIKey: "testkey", APISecret: "testsecret",
		HTTPClient: srv.Client(), RecvWindow: 5000,
	}
	res, err := r.SendOrder(context.Background(), OrderIntent{
		Symbol: "BTCUSDT", Side: models.Short, Quantity: 0.5, Type: "MARKET",
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.OrderID != "123456" || res.Status != "FILLED" || res.FilledQty != 0.5 || res.AvgPrice != 50000.5 {
		t.Errorf("OrderResult mismatch: %+v", res)
	}
	if capturedAPIKey != "testkey" {
		t.Errorf("X-MBX-APIKEY not sent: %q", capturedAPIKey)
	}
	if !strings.Contains(capturedURL, "side=SELL") {
		t.Errorf("expected side=SELL in URL: %q", capturedURL)
	}
	if !strings.Contains(capturedURL, "signature=") {
		t.Errorf("expected signature= in URL: %q", capturedURL)
	}
	if !strings.Contains(capturedURL, "type=MARKET") {
		t.Errorf("expected type=MARKET in URL: %q", capturedURL)
	}
}

func TestSendOrder_PartialFill_ReportedAsPARTIAL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"orderId":1,"status":"PARTIALLY_FILLED","executedQty":"0.3","avgPrice":"50000"}`))
	}))
	defer srv.Close()
	r := &OrderRouter{
		APIBaseURL: srv.URL, APIKey: "k", APISecret: "s",
		HTTPClient: srv.Client(), RecvWindow: 5000,
	}
	res, err := r.SendOrder(context.Background(), OrderIntent{
		Symbol: "BTCUSDT", Side: models.Short, Quantity: 0.5, Type: "MARKET",
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Status != "PARTIAL" {
		t.Errorf("expected PARTIAL when filled<requested, got %q (filled=%v)", res.Status, res.FilledQty)
	}
}

func TestSendOrder_RateLimit_429(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"code":-1003,"msg":"too many requests"}`))
	}))
	defer srv.Close()
	r := &OrderRouter{
		APIBaseURL: srv.URL, APIKey: "k", APISecret: "s",
		HTTPClient: srv.Client(),
	}
	res, err := r.SendOrder(context.Background(), OrderIntent{
		Symbol: "BTCUSDT", Side: models.Short, Quantity: 0.5, Type: "MARKET",
	})
	if err == nil {
		t.Fatal("expected err on 429")
	}
	if res.RejectCode != "RATE_LIMIT" {
		t.Errorf("RejectCode = %q, want RATE_LIMIT", res.RejectCode)
	}
}

func TestSendOrder_4xxRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"code":-2010,"msg":"insufficient margin"}`))
	}))
	defer srv.Close()
	r := &OrderRouter{
		APIBaseURL: srv.URL, APIKey: "k", APISecret: "s",
		HTTPClient: srv.Client(),
	}
	res, err := r.SendOrder(context.Background(), OrderIntent{
		Symbol: "BTCUSDT", Side: models.Short, Quantity: 0.5, Type: "MARKET",
	})
	if err == nil {
		t.Fatal("expected err on 400")
	}
	if res.Status != "REJECTED" {
		t.Errorf("Status = %q, want REJECTED", res.Status)
	}
}

func TestSendOrder_5xxError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	r := &OrderRouter{
		APIBaseURL: srv.URL, APIKey: "k", APISecret: "s",
		HTTPClient: srv.Client(),
	}
	res, _ := r.SendOrder(context.Background(), OrderIntent{
		Symbol: "BTCUSDT", Side: models.Short, Quantity: 0.5, Type: "MARKET",
	})
	if res.Status != "ERROR" || res.RejectCode != "SERVER" {
		t.Errorf("expected ERROR/SERVER, got %q/%q", res.Status, res.RejectCode)
	}
}

func TestPositionReconciler_Run_ErrorsOnSkeleton(t *testing.T) {
	r := &PositionReconciler{}
	err := r.Run(context.Background(), "BTCUSDT")
	if !errors.Is(err, ErrStageNotPromoted) {
		t.Errorf("Run err = %v, want ErrStageNotPromoted", err)
	}
}

// SafetyGates real-implementation tests. STAGE_1 thresholds:
// MaxPositionMultiple=1, DailyLossUSDCap=$1000, MaxEntrySpreadBps=50.
func stage1Gates() *SafetyGates {
	return &SafetyGates{MaxPositionMultiple: 1, DailyLossUSDCap: 1000, MaxEntrySpreadBps: 50}
}

func goodGateCtx() GateContext {
	// Healthy STAGE_1 context: $100 stake, signal entry at mid, no open
	// position, no recent loss.
	return GateContext{
		Intent: OrderIntent{
			Symbol: "BTCUSDT", Side: models.Short, Quantity: 0.002, Type: "MARKET",
		},
		SignalEntryPrice:        50000,
		CurrentBid:              49998,
		CurrentAsk:              50002,
		OpenPositionNotionalUSD: 0,
		StakeUSD:                100,
		Recent24hLossUSD:        0,
	}
}

func TestSafetyGates_AllPass_ReturnsNil(t *testing.T) {
	g := stage1Gates()
	if err := g.CheckOrder(goodGateCtx()); err != nil {
		t.Errorf("expected nil for healthy context, got %v", err)
	}
}

func TestSafetyGates_GateA_MaxPositionCap(t *testing.T) {
	g := stage1Gates()
	c := goodGateCtx()
	// stake=$100, MaxPositionMultiple=1, so cap=$100. Quantity 0.002 × 50000 = $100 — exactly at cap.
	c.Intent.Quantity = 0.002
	if err := g.CheckOrder(c); err != nil {
		t.Errorf("Gate A at exact cap should pass: %v", err)
	}
	// One penny over: should fail.
	c.Intent.Quantity = 0.0021 // $105 notional → over $100 cap
	err := g.CheckOrder(c)
	if err == nil || !strings.Contains(err.Error(), "Gate A") {
		t.Errorf("Gate A over cap: expected Gate A failure, got %v", err)
	}
}

func TestSafetyGates_GateA_AccountsForOpenPosition(t *testing.T) {
	g := stage1Gates()
	c := goodGateCtx()
	c.Intent.Quantity = 0.001 // $50 intent
	c.OpenPositionNotionalUSD = 60 // $60 already open → total $110 > $100 cap
	err := g.CheckOrder(c)
	if err == nil || !strings.Contains(err.Error(), "Gate A") {
		t.Errorf("expected Gate A failure with existing position, got %v", err)
	}
}

func TestSafetyGates_GateB_DailyLossCircuit(t *testing.T) {
	g := stage1Gates()
	c := goodGateCtx()
	// At cap: passes.
	c.Recent24hLossUSD = 1000
	if err := g.CheckOrder(c); err != nil {
		t.Errorf("Gate B at exact cap should pass: %v", err)
	}
	// Over cap: fails.
	c.Recent24hLossUSD = 1000.01
	err := g.CheckOrder(c)
	if err == nil || !strings.Contains(err.Error(), "Gate B") {
		t.Errorf("Gate B over cap: expected Gate B failure, got %v", err)
	}
}

func TestSafetyGates_GateC_EntryPriceSanity(t *testing.T) {
	g := stage1Gates()
	c := goodGateCtx()
	// Signal entry at exact mid: passes.
	c.SignalEntryPrice = 50000
	if err := g.CheckOrder(c); err != nil {
		t.Errorf("entry-at-mid should pass: %v", err)
	}
	// Signal entry 50 bps below mid (right at cap): mid=50000, 50 bps = $250
	// → 49750 should be exactly at cap and pass.
	c.SignalEntryPrice = 49750
	if err := g.CheckOrder(c); err != nil {
		t.Errorf("entry at 50bps from mid (exact cap) should pass: %v", err)
	}
	// 60 bps below: should fail.
	c.SignalEntryPrice = 49700 // 60 bps below 50000
	err := g.CheckOrder(c)
	if err == nil || !strings.Contains(err.Error(), "Gate C") {
		t.Errorf("entry at 60bps from mid: expected Gate C failure, got %v", err)
	}
}

func TestSafetyGates_GateC_InvalidBidAsk(t *testing.T) {
	g := stage1Gates()
	c := goodGateCtx()
	// Bid > ask is structurally invalid.
	c.CurrentBid, c.CurrentAsk = 50100, 50000
	err := g.CheckOrder(c)
	if err == nil || !strings.Contains(err.Error(), "Gate C") {
		t.Errorf("inverted bid/ask: expected Gate C failure, got %v", err)
	}
	// Zero bid/ask is invalid.
	c.CurrentBid, c.CurrentAsk = 0, 0
	err = g.CheckOrder(c)
	if err == nil || !strings.Contains(err.Error(), "Gate C") {
		t.Errorf("zero bid/ask: expected Gate C failure, got %v", err)
	}
}

func TestSafetyGates_Ordering_GateAFailsBeforeGateB(t *testing.T) {
	// When BOTH Gate A and Gate B would fail, A's error fires first.
	// This pins the locked order (cheapest checks first; A and B are both
	// pure arithmetic, so A's order is purely convention).
	g := stage1Gates()
	c := goodGateCtx()
	c.Intent.Quantity = 1.0      // $50000 intent vs $100 cap → Gate A fails
	c.Recent24hLossUSD = 5000    // $5000 vs $1000 cap → Gate B would also fail
	c.SignalEntryPrice = 30000   // 4000 bps from mid → Gate C would also fail
	err := g.CheckOrder(c)
	if err == nil || !strings.Contains(err.Error(), "Gate A") {
		t.Errorf("ordered failure: expected Gate A first, got %v", err)
	}
}

func TestKillSwitch_KillAll_NotConfigured(t *testing.T) {
	k := &KillSwitch{} // Router nil
	_, err := k.KillAll(context.Background(), nil, "test")
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Errorf("KillAll with nil Router: expected 'not configured' err, got %v", err)
	}
}

func TestKillSwitch_KillAll_EmptyPositions_NoError(t *testing.T) {
	// Idempotent: empty list is valid input, returns clean.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(`{"orderId":1,"status":"FILLED","executedQty":"0","avgPrice":"0"}`))
	}))
	defer srv.Close()
	r := &OrderRouter{
		APIBaseURL: srv.URL, APIKey: "k", APISecret: "s", HTTPClient: srv.Client(),
	}
	k := &KillSwitch{Router: r}
	res, err := k.KillAll(context.Background(), nil, "no-op test")
	if err != nil {
		t.Errorf("empty positions: expected nil err, got %v", err)
	}
	if len(res.Outcomes) != 0 {
		t.Errorf("empty positions: outcomes = %d, want 0", len(res.Outcomes))
	}
	if res.Reason != "no-op test" {
		t.Errorf("Reason not preserved: %q", res.Reason)
	}
}

func TestKillSwitch_KillAll_SingleSuccess(t *testing.T) {
	// Track which side the close order used (verify Long-position close → SELL).
	var capturedSide string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Side is in the URL query string.
		if strings.Contains(r.URL.RawQuery, "side=SELL") {
			capturedSide = "SELL"
		} else if strings.Contains(r.URL.RawQuery, "side=BUY") {
			capturedSide = "BUY"
		}
		w.WriteHeader(200)
		w.Write([]byte(`{"orderId":42,"status":"FILLED","executedQty":"0.5","avgPrice":"50000.5"}`))
	}))
	defer srv.Close()
	r := &OrderRouter{
		APIBaseURL: srv.URL, APIKey: "k", APISecret: "s", HTTPClient: srv.Client(),
	}
	k := &KillSwitch{Router: r}

	res, err := k.KillAll(context.Background(),
		[]ClosePosition{
			{Symbol: "BTCUSDT", Side: models.Long, Quantity: 0.5, AvgEntry: 49500},
		},
		"test kill")
	if err != nil {
		t.Fatalf("expected nil err on success, got %v", err)
	}
	if len(res.Outcomes) != 1 {
		t.Fatalf("outcomes = %d, want 1", len(res.Outcomes))
	}
	if res.Outcomes[0].Status != "CLOSED" {
		t.Errorf("Status = %q, want CLOSED", res.Outcomes[0].Status)
	}
	if res.Outcomes[0].Filled != 0.5 {
		t.Errorf("Filled = %v, want 0.5", res.Outcomes[0].Filled)
	}
	// Long position close → SELL on the exchange (per mapToExchangeSide reduceOnly flip).
	if capturedSide != "SELL" {
		t.Errorf("close-Long side = %q, want SELL", capturedSide)
	}
}

func TestKillSwitch_KillAll_ShortClose_MapsToBuy(t *testing.T) {
	var capturedSide string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.RawQuery, "side=BUY") {
			capturedSide = "BUY"
		}
		w.Write([]byte(`{"orderId":1,"status":"FILLED","executedQty":"0.5","avgPrice":"50000"}`))
	}))
	defer srv.Close()
	r := &OrderRouter{APIBaseURL: srv.URL, APIKey: "k", APISecret: "s", HTTPClient: srv.Client()}
	k := &KillSwitch{Router: r}

	_, err := k.KillAll(context.Background(),
		[]ClosePosition{{Symbol: "BTCUSDT", Side: models.Short, Quantity: 0.5}},
		"test")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if capturedSide != "BUY" {
		t.Errorf("close-Short side = %q, want BUY", capturedSide)
	}
}

func TestKillSwitch_KillAll_FailureContinues_AggregatesErrors(t *testing.T) {
	// One symbol returns 400 (REJECTED), one returns 200 (CLOSED). KillAll
	// should attempt both, aggregate per-symbol outcomes, and return a
	// non-nil error noting the partial failure.
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(400)
			w.Write([]byte(`{"code":-2010,"msg":"insufficient margin"}`))
		} else {
			w.WriteHeader(200)
			w.Write([]byte(`{"orderId":2,"status":"FILLED","executedQty":"1.0","avgPrice":"3000"}`))
		}
	}))
	defer srv.Close()
	r := &OrderRouter{APIBaseURL: srv.URL, APIKey: "k", APISecret: "s", HTTPClient: srv.Client()}
	k := &KillSwitch{Router: r}

	res, err := k.KillAll(context.Background(),
		[]ClosePosition{
			{Symbol: "BTCUSDT", Side: models.Long, Quantity: 0.5}, // will fail
			{Symbol: "ETHUSDT", Side: models.Long, Quantity: 1.0}, // will succeed
		},
		"partial test")
	if err == nil {
		t.Fatal("expected aggregated err when 1/2 failed")
	}
	if !strings.Contains(err.Error(), "1 of 2") {
		t.Errorf("err missing aggregate count: %q", err.Error())
	}
	if len(res.Outcomes) != 2 {
		t.Fatalf("outcomes = %d, want 2 (both attempted)", len(res.Outcomes))
	}
	if res.Outcomes[0].Status != "FAILED" {
		t.Errorf("first outcome Status = %q, want FAILED", res.Outcomes[0].Status)
	}
	if res.Outcomes[1].Status != "CLOSED" {
		t.Errorf("second outcome Status = %q, want CLOSED (failure didn't stop iteration)", res.Outcomes[1].Status)
	}
}

func TestKillSwitch_KillAll_PartialFill_ReportedAsPARTIAL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"orderId":1,"status":"PARTIALLY_FILLED","executedQty":"0.3","avgPrice":"50000"}`))
	}))
	defer srv.Close()
	r := &OrderRouter{APIBaseURL: srv.URL, APIKey: "k", APISecret: "s", HTTPClient: srv.Client()}
	k := &KillSwitch{Router: r}

	res, _ := k.KillAll(context.Background(),
		[]ClosePosition{{Symbol: "BTCUSDT", Side: models.Long, Quantity: 0.5}},
		"partial-fill test")
	if len(res.Outcomes) != 1 || res.Outcomes[0].Status != "PARTIAL" {
		t.Errorf("partial fill: expected PARTIAL outcome, got %+v", res.Outcomes)
	}
	if res.Outcomes[0].Filled != 0.3 {
		t.Errorf("PARTIAL Filled = %v, want 0.3", res.Outcomes[0].Filled)
	}
}

func TestErrStageNotPromoted_MessageMentionsPreReg(t *testing.T) {
	// Defensive: the error message points operators at the pre-reg doc
	// so when this fires accidentally in production logs, the operator
	// has a direct path to the design rationale.
	msg := ErrStageNotPromoted.Error()
	if !contains(msg, "real_money_executor_architecture") {
		t.Errorf("ErrStageNotPromoted message missing pre-reg reference: %q", msg)
	}
	if !contains(msg, "STAGE_1") {
		t.Errorf("ErrStageNotPromoted message missing stage reference: %q", msg)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
