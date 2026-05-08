package execution

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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

func TestPositionReconciler_Run_ErrorsWhenNotConfigured(t *testing.T) {
	// An unconfigured Reconciler (no API creds) must error LOUDLY at Run-entry
	// rather than silently spin a periodic loop that fails every fetch.
	// Misconfiguration on a real-money system should fail fast.
	r := &PositionReconciler{}
	err := r.Run(context.Background(), "BTCUSDT")
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Errorf("Run err = %v, want 'not configured'", err)
	}
}

// ── PositionReconciler: local state CRUD ─────────────────────────────────────

func newReconcilerForTest(t *testing.T, apiURL string) *PositionReconciler {
	t.Helper()
	return &PositionReconciler{
		PollInterval:       60 * time.Second,
		APIBaseURL:         apiURL,
		APIKey:             "k",
		APISecret:          "s",
		HTTPClient:         &http.Client{Timeout: 5 * time.Second},
		RecvWindow:         5000,
		QtyTolerance:       0.01,
		EntryPriceBpsLimit: 10,
	}
}

func TestPositionReconciler_SetAndLocalPosition(t *testing.T) {
	r := newReconcilerForTest(t, "https://example")
	openedAt := time.Date(2026, 5, 8, 8, 0, 0, 0, time.UTC)
	r.SetLocalPosition("BTCUSDT", models.Long, 0.5, 50000, openedAt)
	side, qty, entry, ok := r.LocalPosition("BTCUSDT")
	if !ok {
		t.Fatal("LocalPosition: ok=false after SetLocalPosition")
	}
	if side != models.Long || qty != 0.5 || entry != 50000 {
		t.Errorf("local pos = side=%v qty=%v entry=%v; want Long/0.5/50000", side, qty, entry)
	}
}

func TestPositionReconciler_LocalPosition_NotSet_ReturnsFalse(t *testing.T) {
	r := newReconcilerForTest(t, "https://example")
	_, _, _, ok := r.LocalPosition("BTCUSDT")
	if ok {
		t.Error("LocalPosition: ok=true on never-set symbol, want false")
	}
}

func TestPositionReconciler_ClearLocalPosition(t *testing.T) {
	r := newReconcilerForTest(t, "https://example")
	r.SetLocalPosition("BTCUSDT", models.Long, 0.5, 50000, time.Now())
	r.ClearLocalPosition("BTCUSDT")
	if _, _, _, ok := r.LocalPosition("BTCUSDT"); ok {
		t.Error("LocalPosition still present after Clear")
	}
}

func TestPositionReconciler_IsDrifted_DefaultsClean(t *testing.T) {
	r := newReconcilerForTest(t, "https://example")
	drifted, reason := r.IsDrifted("BTCUSDT")
	if drifted || reason != "" {
		t.Errorf("IsDrifted default: %v / %q; want false / \"\"", drifted, reason)
	}
}

// ── PositionReconciler: FetchExchangePosition (HTTP path) ───────────────────

func TestPositionReconciler_FetchExchangePosition_ParsesLong(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte(`[{"symbol":"BTCUSDT","positionAmt":"0.500","entryPrice":"50000.0","positionSide":"BOTH"}]`))
	}))
	defer srv.Close()
	r := newReconcilerForTest(t, srv.URL)
	r.HTTPClient = srv.Client()
	pos, err := r.FetchExchangePosition(context.Background(), "BTCUSDT")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if pos.Side != models.Long || pos.Qty != 0.5 || pos.AvgEntry != 50000 {
		t.Errorf("Long pos parsed wrong: %+v", pos)
	}
}

func TestPositionReconciler_FetchExchangePosition_ParsesShort(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// SHORT in one-way mode: positionAmt is NEGATIVE.
		w.Write([]byte(`[{"symbol":"BTCUSDT","positionAmt":"-0.500","entryPrice":"50000.0","positionSide":"BOTH"}]`))
	}))
	defer srv.Close()
	r := newReconcilerForTest(t, srv.URL)
	r.HTTPClient = srv.Client()
	pos, err := r.FetchExchangePosition(context.Background(), "BTCUSDT")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if pos.Side != models.Short || pos.Qty != -0.5 || pos.AvgEntry != 50000 {
		t.Errorf("Short pos parsed wrong: %+v", pos)
	}
}

func TestPositionReconciler_FetchExchangePosition_ParsesFlat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// Flat: positionAmt zero. entryPrice may be "0.0".
		w.Write([]byte(`[{"symbol":"BTCUSDT","positionAmt":"0","entryPrice":"0.0","positionSide":"BOTH"}]`))
	}))
	defer srv.Close()
	r := newReconcilerForTest(t, srv.URL)
	r.HTTPClient = srv.Client()
	pos, err := r.FetchExchangePosition(context.Background(), "BTCUSDT")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if pos.Side != models.Neutral || pos.Qty != 0 {
		t.Errorf("Flat pos parsed wrong: %+v", pos)
	}
}

func TestPositionReconciler_FetchExchangePosition_HedgeModeSumsLongAndShort(t *testing.T) {
	// Hedge mode: separate LONG and SHORT entries. Net position = sum.
	// Defensive parsing — STAGE_1 is one-way mode, but if the account ever
	// flips to hedge mode the reconciler still gives a coherent answer.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte(`[
			{"symbol":"BTCUSDT","positionAmt":"0.700","entryPrice":"50000.0","positionSide":"LONG"},
			{"symbol":"BTCUSDT","positionAmt":"-0.200","entryPrice":"51000.0","positionSide":"SHORT"}
		]`))
	}))
	defer srv.Close()
	r := newReconcilerForTest(t, srv.URL)
	r.HTTPClient = srv.Client()
	pos, err := r.FetchExchangePosition(context.Background(), "BTCUSDT")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// Net qty = 0.7 - 0.2 = 0.5 (LONG bias).
	if pos.Side != models.Long || math.Abs(pos.Qty-0.5) > 1e-9 {
		t.Errorf("hedge-mode net: %+v; want Long qty=0.5", pos)
	}
}

func TestPositionReconciler_FetchExchangePosition_SignedRequest(t *testing.T) {
	var capturedQuery, capturedAPIKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		capturedQuery = req.URL.RawQuery
		capturedAPIKey = req.Header.Get("X-MBX-APIKEY")
		w.Write([]byte(`[{"symbol":"BTCUSDT","positionAmt":"0","entryPrice":"0","positionSide":"BOTH"}]`))
	}))
	defer srv.Close()
	r := newReconcilerForTest(t, srv.URL)
	r.HTTPClient = srv.Client()
	if _, err := r.FetchExchangePosition(context.Background(), "BTCUSDT"); err != nil {
		t.Fatalf("err: %v", err)
	}
	if capturedAPIKey != "k" {
		t.Errorf("X-MBX-APIKEY missing: %q", capturedAPIKey)
	}
	if !strings.Contains(capturedQuery, "signature=") {
		t.Errorf("query missing signature: %q", capturedQuery)
	}
	if !strings.Contains(capturedQuery, "symbol=BTCUSDT") {
		t.Errorf("query missing symbol: %q", capturedQuery)
	}
	if !strings.Contains(capturedQuery, "timestamp=") {
		t.Errorf("query missing timestamp: %q", capturedQuery)
	}
}

func TestPositionReconciler_FetchExchangePosition_5xxError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	r := newReconcilerForTest(t, srv.URL)
	r.HTTPClient = srv.Client()
	if _, err := r.FetchExchangePosition(context.Background(), "BTCUSDT"); err == nil {
		t.Fatal("expected err on 5xx")
	}
}

// ── PositionReconciler: ReconcileSymbol drift detection ──────────────────────
//
// The locked rule defines drift as: |qty mismatch| ≥ 0.01 contracts, OR side
// mismatch, OR entry price differs by ≥10 bps. The cases below tabulate every
// branch + boundary.

// reconcileWithExchange wires up an httptest server with a canned positionRisk
// response and runs ReconcileSymbol once. Returns the DriftReport for inspection.
func reconcileWithExchange(t *testing.T, r *PositionReconciler, exchangeJSON string) DriftReport {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte(exchangeJSON))
	}))
	t.Cleanup(srv.Close)
	r.APIBaseURL = srv.URL
	r.HTTPClient = srv.Client()
	report, err := r.ReconcileSymbol(context.Background(), "BTCUSDT")
	if err != nil {
		t.Fatalf("ReconcileSymbol: %v", err)
	}
	return report
}

func TestReconcileSymbol_BothFlat_Clean(t *testing.T) {
	r := newReconcilerForTest(t, "")
	report := reconcileWithExchange(t, r,
		`[{"symbol":"BTCUSDT","positionAmt":"0","entryPrice":"0","positionSide":"BOTH"}]`)
	if report.Drifted {
		t.Errorf("both-flat: drifted=true (%q); want false", report.Reason)
	}
}

func TestReconcileSymbol_OrphanExchange_Drifts(t *testing.T) {
	r := newReconcilerForTest(t, "")
	// Local has nothing; exchange has a position → orphan-exchange drift.
	report := reconcileWithExchange(t, r,
		`[{"symbol":"BTCUSDT","positionAmt":"0.5","entryPrice":"50000","positionSide":"BOTH"}]`)
	if !report.Drifted {
		t.Errorf("orphan-exchange: drifted=false; want true")
	}
	if !strings.Contains(report.Reason, "orphan") {
		t.Errorf("reason missing 'orphan': %q", report.Reason)
	}
	if drifted, _ := r.IsDrifted("BTCUSDT"); !drifted {
		t.Error("IsDrifted not set after drift detection")
	}
}

func TestReconcileSymbol_ExternallyClosed_Drifts(t *testing.T) {
	r := newReconcilerForTest(t, "")
	// Local has position; exchange shows flat → externally-closed drift.
	r.SetLocalPosition("BTCUSDT", models.Long, 0.5, 50000, time.Now())
	report := reconcileWithExchange(t, r,
		`[{"symbol":"BTCUSDT","positionAmt":"0","entryPrice":"0","positionSide":"BOTH"}]`)
	if !report.Drifted {
		t.Errorf("externally-closed: drifted=false; want true")
	}
	if !strings.Contains(report.Reason, "externally") {
		t.Errorf("reason missing 'externally': %q", report.Reason)
	}
}

func TestReconcileSymbol_SideMismatch_Drifts(t *testing.T) {
	r := newReconcilerForTest(t, "")
	r.SetLocalPosition("BTCUSDT", models.Long, 0.5, 50000, time.Now())
	report := reconcileWithExchange(t, r,
		`[{"symbol":"BTCUSDT","positionAmt":"-0.5","entryPrice":"50000","positionSide":"BOTH"}]`)
	if !report.Drifted {
		t.Error("side mismatch: drifted=false; want true")
	}
	if !strings.Contains(report.Reason, "side") {
		t.Errorf("reason missing 'side': %q", report.Reason)
	}
}

func TestReconcileSymbol_QtyDrift_AtThreshold_Drifts(t *testing.T) {
	r := newReconcilerForTest(t, "")
	// Local 0.50, exchange 0.51 → Δ=0.01 = locked threshold (>= fires).
	r.SetLocalPosition("BTCUSDT", models.Long, 0.50, 50000, time.Now())
	report := reconcileWithExchange(t, r,
		`[{"symbol":"BTCUSDT","positionAmt":"0.51","entryPrice":"50000","positionSide":"BOTH"}]`)
	if !report.Drifted {
		t.Error("qty drift at exact threshold: want drift")
	}
	if !strings.Contains(report.Reason, "qty") {
		t.Errorf("reason missing 'qty': %q", report.Reason)
	}
}

func TestReconcileSymbol_QtyDrift_BelowThreshold_Clean(t *testing.T) {
	r := newReconcilerForTest(t, "")
	// Local 0.500, exchange 0.505 → Δ=0.005 < 0.01 threshold.
	r.SetLocalPosition("BTCUSDT", models.Long, 0.500, 50000, time.Now())
	report := reconcileWithExchange(t, r,
		`[{"symbol":"BTCUSDT","positionAmt":"0.505","entryPrice":"50000","positionSide":"BOTH"}]`)
	if report.Drifted {
		t.Errorf("qty drift below threshold: drifted=true (%q); want false", report.Reason)
	}
}

func TestReconcileSymbol_EntryPriceDrift_AtThreshold_Drifts(t *testing.T) {
	r := newReconcilerForTest(t, "")
	// Local entry 50000, exchange 50050 → 10.0 bps = locked threshold.
	r.SetLocalPosition("BTCUSDT", models.Long, 0.5, 50000, time.Now())
	report := reconcileWithExchange(t, r,
		`[{"symbol":"BTCUSDT","positionAmt":"0.5","entryPrice":"50050","positionSide":"BOTH"}]`)
	if !report.Drifted {
		t.Error("entry-price drift at exact threshold: want drift")
	}
	if !strings.Contains(report.Reason, "entry-price") {
		t.Errorf("reason missing 'entry-price': %q", report.Reason)
	}
}

func TestReconcileSymbol_EntryPriceDrift_BelowThreshold_Clean(t *testing.T) {
	r := newReconcilerForTest(t, "")
	// Local 50000, exchange 50049 → 9.8 bps < 10 bps.
	r.SetLocalPosition("BTCUSDT", models.Long, 0.5, 50000, time.Now())
	report := reconcileWithExchange(t, r,
		`[{"symbol":"BTCUSDT","positionAmt":"0.5","entryPrice":"50049","positionSide":"BOTH"}]`)
	if report.Drifted {
		t.Errorf("entry-price drift below threshold: drifted=true (%q); want false", report.Reason)
	}
}

func TestReconcileSymbol_HappyPath_Clean(t *testing.T) {
	r := newReconcilerForTest(t, "")
	r.SetLocalPosition("BTCUSDT", models.Long, 0.5, 50000, time.Now())
	report := reconcileWithExchange(t, r,
		`[{"symbol":"BTCUSDT","positionAmt":"0.5","entryPrice":"50000","positionSide":"BOTH"}]`)
	if report.Drifted {
		t.Errorf("happy path: drifted=true (%q); want false", report.Reason)
	}
	if drifted, _ := r.IsDrifted("BTCUSDT"); drifted {
		t.Error("IsDrifted set on clean reconciliation")
	}
}

func TestReconcileSymbol_DriftClearsAfterReconciliation(t *testing.T) {
	r := newReconcilerForTest(t, "")
	r.SetLocalPosition("BTCUSDT", models.Long, 0.5, 50000, time.Now())

	// First pass: simulate side-mismatch drift.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte(`[{"symbol":"BTCUSDT","positionAmt":"-0.5","entryPrice":"50000","positionSide":"BOTH"}]`))
	}))
	r.APIBaseURL = srv.URL
	r.HTTPClient = srv.Client()
	if _, err := r.ReconcileSymbol(context.Background(), "BTCUSDT"); err != nil {
		t.Fatal(err)
	}
	if drifted, _ := r.IsDrifted("BTCUSDT"); !drifted {
		t.Fatal("drift not flagged after side-mismatch")
	}
	srv.Close()

	// Second pass: exchange now matches local → drift should clear automatically.
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte(`[{"symbol":"BTCUSDT","positionAmt":"0.5","entryPrice":"50000","positionSide":"BOTH"}]`))
	}))
	defer srv2.Close()
	r.APIBaseURL = srv2.URL
	r.HTTPClient = srv2.Client()
	if _, err := r.ReconcileSymbol(context.Background(), "BTCUSDT"); err != nil {
		t.Fatal(err)
	}
	if drifted, _ := r.IsDrifted("BTCUSDT"); drifted {
		t.Error("drift not cleared after subsequent clean reconciliation")
	}
}

func TestPositionReconciler_ClearDrift_Resets(t *testing.T) {
	r := newReconcilerForTest(t, "")
	// Force drift via the orphan-exchange path.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte(`[{"symbol":"BTCUSDT","positionAmt":"0.5","entryPrice":"50000","positionSide":"BOTH"}]`))
	}))
	defer srv.Close()
	r.APIBaseURL = srv.URL
	r.HTTPClient = srv.Client()
	if _, err := r.ReconcileSymbol(context.Background(), "BTCUSDT"); err != nil {
		t.Fatal(err)
	}
	if drifted, _ := r.IsDrifted("BTCUSDT"); !drifted {
		t.Fatal("drift not set")
	}
	r.ClearDrift("BTCUSDT")
	if drifted, _ := r.IsDrifted("BTCUSDT"); drifted {
		t.Error("drift not cleared by ClearDrift")
	}
}

// ── PositionReconciler: poll-interval clamping ───────────────────────────────

func TestPositionReconciler_EffectivePollInterval_DefaultsTo60s(t *testing.T) {
	r := &PositionReconciler{}
	got := r.effectivePollInterval()
	if got != 60*time.Second {
		t.Errorf("default = %v, want 60s", got)
	}
}

func TestPositionReconciler_EffectivePollInterval_ClampsLow(t *testing.T) {
	r := &PositionReconciler{PollInterval: 1 * time.Second}
	got := r.effectivePollInterval()
	if got != 30*time.Second {
		t.Errorf("clamped low = %v, want 30s (locked floor)", got)
	}
}

func TestPositionReconciler_EffectivePollInterval_ClampsHigh(t *testing.T) {
	r := &PositionReconciler{PollInterval: 10 * time.Minute}
	got := r.effectivePollInterval()
	if got != 5*time.Minute {
		t.Errorf("clamped high = %v, want 5m (locked ceiling)", got)
	}
}

func TestPositionReconciler_EffectivePollInterval_PassesThroughInRange(t *testing.T) {
	r := &PositionReconciler{PollInterval: 90 * time.Second}
	got := r.effectivePollInterval()
	if got != 90*time.Second {
		t.Errorf("in-range = %v, want passthrough 90s", got)
	}
}

// ── PositionReconciler: Run loop ─────────────────────────────────────────────

func TestPositionReconciler_Run_RespectsContext(t *testing.T) {
	// The Run loop must return promptly when ctx is canceled, even though
	// the clamped tick interval is 30s+. The initial reconcile shouldn't
	// block ctx-cancel forever either.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte(`[{"symbol":"BTCUSDT","positionAmt":"0","entryPrice":"0","positionSide":"BOTH"}]`))
	}))
	defer srv.Close()
	r := newReconcilerForTest(t, srv.URL)
	r.HTTPClient = srv.Client()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx, "BTCUSDT") }()
	// Give the initial reconcile time to fire, then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Run err = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s of ctx cancel")
	}
}

func TestPositionReconciler_Run_InitialReconcileFires(t *testing.T) {
	// Run must do an initial reconcile pass BEFORE waiting for the first
	// tick — otherwise an engine restart leaves the operator blind for up
	// to 60s about pre-existing exchange positions.
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Write([]byte(`[{"symbol":"BTCUSDT","positionAmt":"0","entryPrice":"0","positionSide":"BOTH"}]`))
	}))
	defer srv.Close()
	r := newReconcilerForTest(t, srv.URL)
	r.HTTPClient = srv.Client()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx, "BTCUSDT") }()
	// Wait long enough for initial reconcile to land but well short of the
	// 30s clamped tick.
	time.Sleep(200 * time.Millisecond)
	cancel()
	<-done
	if got := atomic.LoadInt32(&calls); got < 1 {
		t.Errorf("initial reconcile calls = %d, want ≥1", got)
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
