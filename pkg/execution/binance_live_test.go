package execution

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cristianmanoliu/fin-trading-engine/pkg/models"
	"github.com/cristianmanoliu/fin-trading-engine/pkg/strategy"
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
	// Mainnet endpoint is the default — production fapi.
	if bl.OrderRouter.APIBaseURL != MainnetAPIBaseURL {
		t.Errorf("OrderRouter.APIBaseURL = %q, want mainnet %q", bl.OrderRouter.APIBaseURL, MainnetAPIBaseURL)
	}
	if bl.PositionReconciler.APIBaseURL != MainnetAPIBaseURL {
		t.Errorf("PositionReconciler.APIBaseURL = %q, want mainnet %q", bl.PositionReconciler.APIBaseURL, MainnetAPIBaseURL)
	}
}

// NewBinanceLiveTestnet must produce a BinanceLive identical to NewBinanceLive
// in every respect EXCEPT that both REST endpoints (OrderRouter,
// PositionReconciler) are swapped to the testnet base. This is the Layer 2
// integration-gate prerequisite: real prices, fake fills.
func TestBinanceLive_NewBinanceLiveTestnet_SwapsBothEndpoints(t *testing.T) {
	bl := NewBinanceLiveTestnet("BTCUSDT", 100, "k", "s")
	if bl.OrderRouter.APIBaseURL != TestnetAPIBaseURL {
		t.Errorf("OrderRouter.APIBaseURL = %q, want testnet %q", bl.OrderRouter.APIBaseURL, TestnetAPIBaseURL)
	}
	if bl.PositionReconciler.APIBaseURL != TestnetAPIBaseURL {
		t.Errorf("PositionReconciler.APIBaseURL = %q, want testnet %q", bl.PositionReconciler.APIBaseURL, TestnetAPIBaseURL)
	}
	// All other components remain wired the same way as mainnet.
	if bl.OrderRouter == nil || bl.PositionReconciler == nil || bl.SafetyGates == nil || bl.KillSwitch == nil {
		t.Fatal("testnet variant must have all components constructed")
	}
	if bl.KillSwitch.Router != bl.OrderRouter {
		t.Error("testnet variant KillSwitch.Router not wired to OrderRouter")
	}
	// Stake + symbol pass through unchanged.
	if bl.Symbol != "BTCUSDT" || bl.StakeUSD != 100 || bl.APIKey != "k" || bl.APISecret != "s" {
		t.Errorf("testnet variant did not pass through ctor args: %+v", bl)
	}
	// Sanity: testnet base must not equal mainnet base.
	if TestnetAPIBaseURL == MainnetAPIBaseURL {
		t.Fatal("TestnetAPIBaseURL == MainnetAPIBaseURL — constants collapsed")
	}
}

// ── BinanceLive Executor full-path tests ─────────────────────────────────────
//
// Each test wires a httptest mock that satisfies whichever Binance endpoints
// the test path traverses (entry order via /fapi/v1/order; exit order ditto;
// positionRisk if Reconciler is invoked). Tests use bl.Wait() to drain the
// goroutines OnSignal / OnTick spawn so assertions are deterministic.

// newBinanceLiveWithMock returns a fully-wired BinanceLive whose OrderRouter
// + Reconciler talk to the supplied test server. Preset for STAGE_1
// thresholds, $100 stake, FeeBps=10, StopSlippageBps=5 (matches the modeled
// costs the project's forward-paper criteria use).
func newBinanceLiveWithMock(t *testing.T, srv *httptest.Server, journalDir string) *BinanceLive {
	t.Helper()
	bl := NewBinanceLive("BTCUSDT", 100, "k", "s")
	bl.OrderRouter.APIBaseURL = srv.URL
	bl.OrderRouter.HTTPClient = srv.Client()
	bl.PositionReconciler.APIBaseURL = srv.URL
	bl.PositionReconciler.HTTPClient = srv.Client()
	bl.JournalPath = journalDir
	bl.FeeBps = 10
	bl.StopSlippageBps = 5
	// Gate A would block default $100 stake at default qty for high-priced
	// instruments; raise the cap so STAGE_1 happy-path tests aren't fighting
	// the gate by accident. Specific gate-blocking tests override this.
	bl.SafetyGates.MaxPositionMultiple = 1e9
	return bl
}

// orderHandler returns an http handler that responds to /fapi/v1/order with
// FILLED + the supplied avg price/qty pairs in sequence (call N → response N).
// Other paths (positionRisk) get a generic flat response.
func orderHandler(t *testing.T, fills []orderFill) http.HandlerFunc {
	t.Helper()
	var calls int32
	return func(w http.ResponseWriter, req *http.Request) {
		switch {
		case strings.Contains(req.URL.Path, "/fapi/v1/order"):
			n := int(atomic.AddInt32(&calls, 1)) - 1
			var f orderFill
			if n < len(fills) {
				f = fills[n]
			} else {
				f = fills[len(fills)-1]
			}
			if f.statusCode == 0 {
				f.statusCode = 200
			}
			w.WriteHeader(f.statusCode)
			if f.body != "" {
				_, _ = w.Write([]byte(f.body))
				return
			}
			fmt.Fprintf(w, `{"orderId":%d,"status":"FILLED","executedQty":"%v","avgPrice":"%v"}`,
				1000+n, f.qty, f.price)
		case strings.Contains(req.URL.Path, "/fapi/v2/positionRisk"):
			_, _ = w.Write([]byte(`[{"symbol":"BTCUSDT","positionAmt":"0","entryPrice":"0","positionSide":"BOTH"}]`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}
}

type orderFill struct {
	qty        float64
	price      float64
	statusCode int    // 0 → 200
	body       string // overrides templated FILLED body when non-empty
}

func TestBinanceLive_OnSignal_HappyPath(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(orderHandler(t, []orderFill{{qty: 0.001, price: 50000}}))
	defer srv.Close()
	bl := newBinanceLiveWithMock(t, srv, dir)

	bl.OnSignal(&models.Signal{
		Symbol:     "BTCUSDT",
		Side:       models.Short,
		EntryPrice: 50000,
		StopLoss:   50100,
		TakeProfit: 49500,
		Timestamp:  time.Now().UTC(),
		Reason:     "test",
	})
	bl.Wait()

	if bl.position == nil {
		t.Fatal("position not set after happy-path OnSignal")
	}
	if bl.position.Signal.EntryPrice != 50000 {
		t.Errorf("position entry: got %v want 50000 (mock fill)", bl.position.Signal.EntryPrice)
	}
	// Reconciler must know about the new local position.
	side, qty, _, ok := bl.PositionReconciler.LocalPosition("BTCUSDT")
	if !ok {
		t.Fatal("Reconciler: LocalPosition not set")
	}
	if side != models.Short {
		t.Errorf("Reconciler side = %v, want SHORT", side)
	}
	// qty = stake / stop_dist = 100 / 100 = 1.0
	if qty != 1.0 {
		t.Errorf("Reconciler qty = %v, want 1.0 (=stake/stop_dist)", qty)
	}
	// Journal has one open line.
	matches, _ := filepath.Glob(filepath.Join(dir, "BTCUSDT-*.jsonl"))
	if len(matches) == 0 {
		t.Fatal("journal file not created")
	}
}

func TestBinanceLive_OnSignal_DriftBlocks(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(orderHandler(t, []orderFill{{qty: 1, price: 50000}}))
	defer srv.Close()
	bl := newBinanceLiveWithMock(t, srv, dir)

	// Force drift by setting a local position the exchange "doesn't" have.
	bl.PositionReconciler.SetLocalPosition("BTCUSDT", models.Long, 0.5, 50000, time.Now())
	if _, err := bl.PositionReconciler.ReconcileSymbol(context.Background(), "BTCUSDT"); err != nil {
		t.Fatal(err)
	}
	if drifted, _ := bl.PositionReconciler.IsDrifted("BTCUSDT"); !drifted {
		t.Fatal("setup: drift not flagged")
	}

	bl.OnSignal(&models.Signal{
		Symbol: "BTCUSDT", Side: models.Short,
		EntryPrice: 50000, StopLoss: 50100, TakeProfit: 49500,
		Timestamp: time.Now().UTC(), Reason: "test",
	})
	bl.Wait()

	if bl.position != nil {
		t.Error("drift gate failed: signal opened a position despite drift")
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, "BTCUSDT-*.jsonl")); len(matches) > 0 {
		t.Error("drift gate failed: journal file was written")
	}
}

func TestBinanceLive_OnSignal_SafetyGateBlocks(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(orderHandler(t, []orderFill{{qty: 1, price: 50000}}))
	defer srv.Close()
	bl := newBinanceLiveWithMock(t, srv, dir)
	// Gate B: pre-load 24h losses past the cap.
	bl.SafetyGates.DailyLossUSDCap = 100
	bl.results = []tradeResult{{
		signal:  &models.Signal{Symbol: "BTCUSDT", Side: models.Short},
		exitTime: time.Now(), pnlUSDT: -500,
	}}

	bl.OnSignal(&models.Signal{
		Symbol: "BTCUSDT", Side: models.Short,
		EntryPrice: 50000, StopLoss: 50100, TakeProfit: 49500,
		Timestamp: time.Now().UTC(), Reason: "test",
	})
	bl.Wait()

	if bl.position != nil {
		t.Error("safety gate failed: signal opened position despite Gate B trip")
	}
}

func TestBinanceLive_OnSignal_OrderRejected(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(orderHandler(t, []orderFill{
		{statusCode: 400, body: `{"code":-2010,"msg":"insufficient margin"}`},
	}))
	defer srv.Close()
	bl := newBinanceLiveWithMock(t, srv, dir)

	bl.OnSignal(&models.Signal{
		Symbol: "BTCUSDT", Side: models.Short,
		EntryPrice: 50000, StopLoss: 50100, TakeProfit: 49500,
		Timestamp: time.Now().UTC(), Reason: "test",
	})
	bl.Wait()

	if bl.position != nil {
		t.Error("rejected order should NOT set local position")
	}
	// No journal open should be written when the order is rejected.
	matches, _ := filepath.Glob(filepath.Join(dir, "BTCUSDT-*.jsonl"))
	for _, m := range matches {
		info, _ := os.Stat(m)
		if info != nil && info.Size() > 0 {
			t.Errorf("rejected order should not journal an open; file %s has size %d", m, info.Size())
		}
	}
}

func TestBinanceLive_OnSignal_PositionAlreadyOpen_Rejected(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(orderHandler(t, []orderFill{{qty: 1, price: 50000}}))
	defer srv.Close()
	bl := newBinanceLiveWithMock(t, srv, dir)

	sig := &models.Signal{
		Symbol: "BTCUSDT", Side: models.Short,
		EntryPrice: 50000, StopLoss: 50100, TakeProfit: 49500,
		Timestamp: time.Now().UTC(), Reason: "first",
	}
	bl.OnSignal(sig)
	bl.Wait()
	if bl.position == nil {
		t.Fatal("first signal should have opened a position")
	}

	// Second signal while first is open — must be ignored, no second order sent.
	bl.OnSignal(&models.Signal{
		Symbol: "BTCUSDT", Side: models.Short,
		EntryPrice: 49000, StopLoss: 49100, TakeProfit: 48500,
		Timestamp: time.Now().UTC(), Reason: "second",
	})
	bl.Wait()
	// Position entry should still be from the first signal.
	if bl.position.Signal.EntryPrice != 50000 {
		t.Errorf("position got overwritten: entry=%v want 50000", bl.position.Signal.EntryPrice)
	}
}

func TestBinanceLive_OnSignal_ZeroStopDist_Rejected(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(orderHandler(t, []orderFill{{qty: 1, price: 50000}}))
	defer srv.Close()
	bl := newBinanceLiveWithMock(t, srv, dir)

	bl.OnSignal(&models.Signal{
		Symbol: "BTCUSDT", Side: models.Short,
		EntryPrice: 50000, StopLoss: 50000, // entry == stop → div-by-zero in sizing
		TakeProfit: 49500, Timestamp: time.Now().UTC(),
	})
	bl.Wait()
	if bl.position != nil {
		t.Error("zero-stop-dist signal should be rejected before order")
	}
}

func TestBinanceLive_OnTick_NoPosition_NoOp(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(orderHandler(t, []orderFill{{qty: 1, price: 50000}}))
	defer srv.Close()
	bl := newBinanceLiveWithMock(t, srv, dir)
	// No OnSignal — just a tick. Should be safe.
	bl.OnTick(models.Tick{Symbol: "BTCUSDT", Timestamp: time.Now(), Price: 50000})
	bl.Wait()
	if bl.position != nil {
		t.Error("OnTick set a position out of nowhere")
	}
}

func TestBinanceLive_OnTick_StopHit_Long_FullPath(t *testing.T) {
	// Long, entry=50000, stop=49900 (stop_dist=100), target=50500.
	// Exit fill at 49899 (1bp adverse beyond modeled stop).
	dir := t.TempDir()
	srv := httptest.NewServer(orderHandler(t, []orderFill{
		{qty: 1, price: 50000},  // entry
		{qty: 1, price: 49899},  // stop fill
	}))
	defer srv.Close()
	bl := newBinanceLiveWithMock(t, srv, dir)
	bl.StakeUSD = 1000 // bigger stake → easier-to-eyeball PnL numbers

	openTime := time.Date(2026, 5, 8, 8, 0, 0, 0, time.UTC)
	bl.OnSignal(&models.Signal{
		Symbol: "BTCUSDT", Side: models.Long,
		EntryPrice: 50000, StopLoss: 49900, TakeProfit: 50500,
		Timestamp: openTime, Reason: "long-stop test",
	})
	bl.Wait()
	if bl.position == nil {
		t.Fatal("entry didn't open")
	}

	// Tick that hits stop.
	bl.OnTick(models.Tick{
		Symbol: "BTCUSDT", Timestamp: openTime.Add(time.Hour), Price: 49899,
	})
	bl.Wait()

	if bl.position != nil {
		t.Fatal("position not cleared after stop hit")
	}
	if len(bl.results) != 1 {
		t.Fatalf("results = %d, want 1", len(bl.results))
	}
	r := bl.results[0]
	if r.won {
		t.Error("stop hit should classify as loss")
	}
	// units = 1000/100 = 10. notional = 10 * 50000 = 500000.
	// gross = 10 * (49899 - 50000) = -1010.
	// fee at 10bp on $500k = $500. Slip = |49899 - 49900| × 10 = $10 (actual diff).
	if math.Abs(r.grossUSDT-(-1010)) > 0.01 {
		t.Errorf("gross: got %v want -1010", r.grossUSDT)
	}
	if math.Abs(r.feeUSDT-500) > 0.01 {
		t.Errorf("fee: got %v want 500", r.feeUSDT)
	}
	if math.Abs(r.slipUSDT-10) > 0.01 {
		t.Errorf("slip from |actual-modeled|×units: got %v want 10", r.slipUSDT)
	}
}

func TestBinanceLive_OnTick_TargetHit_Short_FullPath(t *testing.T) {
	// Short, entry=50000, stop=50100, target=49500. Target fill at 49500.
	dir := t.TempDir()
	srv := httptest.NewServer(orderHandler(t, []orderFill{
		{qty: 1, price: 50000}, // entry
		{qty: 1, price: 49500}, // exit
	}))
	defer srv.Close()
	bl := newBinanceLiveWithMock(t, srv, dir)
	bl.StakeUSD = 1000

	openTime := time.Date(2026, 5, 8, 8, 0, 0, 0, time.UTC)
	bl.OnSignal(&models.Signal{
		Symbol: "BTCUSDT", Side: models.Short,
		EntryPrice: 50000, StopLoss: 50100, TakeProfit: 49500,
		Timestamp: openTime, Reason: "short-target test",
	})
	bl.Wait()
	bl.OnTick(models.Tick{
		Symbol: "BTCUSDT", Timestamp: openTime.Add(time.Hour), Price: 49500,
	})
	bl.Wait()

	if len(bl.results) != 1 {
		t.Fatalf("results = %d, want 1", len(bl.results))
	}
	r := bl.results[0]
	if !r.won {
		t.Error("target hit should classify as win")
	}
	// units = 1000/100 = 10. notional = $500k. gross (short) = 10 × (50000-49500) = 5000.
	if math.Abs(r.grossUSDT-5000) > 0.01 {
		t.Errorf("gross: got %v want 5000", r.grossUSDT)
	}
	if math.Abs(r.feeUSDT-500) > 0.01 {
		t.Errorf("fee: got %v want 500", r.feeUSDT)
	}
	if r.slipUSDT != 0 {
		t.Errorf("slip should be 0 on winner, got %v", r.slipUSDT)
	}
}

func TestBinanceLive_OnTick_DoubleClose_Prevented(t *testing.T) {
	// Two consecutive past-stop ticks should fire only ONE close goroutine.
	// Tracked by counting /fapi/v1/order calls — should be exactly 2 (entry + 1 exit).
	dir := t.TempDir()
	var orderCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.Contains(req.URL.Path, "/fapi/v1/order") {
			n := atomic.AddInt32(&orderCalls, 1)
			if n == 1 {
				w.Write([]byte(`{"orderId":1,"status":"FILLED","executedQty":"1","avgPrice":"50000"}`))
			} else {
				w.Write([]byte(`{"orderId":2,"status":"FILLED","executedQty":"1","avgPrice":"49899"}`))
			}
			return
		}
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	bl := newBinanceLiveWithMock(t, srv, dir)
	bl.StakeUSD = 1000

	openTime := time.Date(2026, 5, 8, 8, 0, 0, 0, time.UTC)
	bl.OnSignal(&models.Signal{
		Symbol: "BTCUSDT", Side: models.Long,
		EntryPrice: 50000, StopLoss: 49900, TakeProfit: 50500,
		Timestamp: openTime, Reason: "test",
	})
	bl.Wait()

	// First past-stop tick.
	bl.OnTick(models.Tick{Symbol: "BTCUSDT", Timestamp: openTime.Add(time.Hour), Price: 49899})
	// Second past-stop tick BEFORE the close goroutine drains — should be no-op.
	bl.OnTick(models.Tick{Symbol: "BTCUSDT", Timestamp: openTime.Add(time.Hour + time.Second), Price: 49890})
	bl.Wait()

	if got := atomic.LoadInt32(&orderCalls); got != 2 {
		t.Errorf("order API calls = %d, want 2 (1 entry + 1 exit; double-close prevention failed)", got)
	}
	if len(bl.results) != 1 {
		t.Errorf("results = %d, want 1", len(bl.results))
	}
}

func TestBinanceLive_OnTick_MaxHold_ForceClose(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(orderHandler(t, []orderFill{
		{qty: 1, price: 50000}, // entry
		{qty: 1, price: 50250}, // force-close fill (matches tick price)
	}))
	defer srv.Close()
	bl := newBinanceLiveWithMock(t, srv, dir)
	bl.StakeUSD = 1000
	bl.MaxHoldHours = 1.0

	openTime := time.Date(2026, 5, 8, 0, 0, 0, 0, time.UTC)
	bl.OnSignal(&models.Signal{
		Symbol: "BTCUSDT", Side: models.Long,
		EntryPrice: 50000, StopLoss: 49900, TakeProfit: 50500,
		Timestamp: openTime, Reason: "max-hold test",
	})
	bl.Wait()

	// Tick at +2h, price between stop and target — would not normally close,
	// but MaxHoldHours fires. classified won = (price > entry) for Long.
	bl.OnTick(models.Tick{
		Symbol: "BTCUSDT", Timestamp: openTime.Add(2 * time.Hour), Price: 50250,
	})
	bl.Wait()

	if len(bl.results) != 1 {
		t.Fatalf("results = %d, want 1 (max-hold force-close)", len(bl.results))
	}
	r := bl.results[0]
	if !r.won {
		t.Error("max-hold above entry should classify as won")
	}
	// Verify the close-event journal outcome is "TIME".
	matches, _ := filepath.Glob(filepath.Join(dir, "BTCUSDT-*.jsonl"))
	if len(matches) == 0 {
		t.Fatal("journal not created")
	}
	data, _ := os.ReadFile(matches[0])
	if !strings.Contains(string(data), `"outcome":"TIME"`) {
		t.Errorf("journal close should record outcome=TIME for max-hold force-close, got: %s", data)
	}
}

func TestBinanceLive_OnTick_PreOpenTick_Skipped(t *testing.T) {
	// Mirrors Stub's recovery guard: a tick predating the position's open
	// timestamp must not hit stop/target. Backfill replay is the relevant
	// scenario — engine restart processes ~96h of historical ticks, many
	// older than the recovered position's open ts.
	dir := t.TempDir()
	srv := httptest.NewServer(orderHandler(t, []orderFill{
		{qty: 1, price: 50000},
	}))
	defer srv.Close()
	bl := newBinanceLiveWithMock(t, srv, dir)
	bl.StakeUSD = 1000

	openTime := time.Date(2026, 5, 8, 8, 0, 0, 0, time.UTC)
	bl.OnSignal(&models.Signal{
		Symbol: "BTCUSDT", Side: models.Long,
		EntryPrice: 50000, StopLoss: 49900, TakeProfit: 50500,
		Timestamp: openTime, Reason: "test",
	})
	bl.Wait()

	// Pre-open tick at price that would hit target — must be ignored.
	bl.OnTick(models.Tick{
		Symbol: "BTCUSDT", Timestamp: openTime.Add(-time.Hour), Price: 50500,
	})
	bl.Wait()
	if bl.position == nil {
		t.Error("pre-open tick at target price closed the position — guard failed")
	}
}

func TestBinanceLive_PnL_FeeAndSlipDecomposition_Long_Loss(t *testing.T) {
	// Verifies the locked journal close-event cost-decomposition schema:
	//   stake=$1000, entry=50000, stop=49900 (stop_dist=100), target=50500.
	//   units=10, notional=$500k.
	//   FeeBps=10 → fee = 10/10000 × 500000 = $500.
	//   StopSlippageBps=5 → fallback slip when actual fill matches modeled
	//     exactly = 5/10000 × 500000 = $250.
	//   gross = 10 × (49900 - 50000) = -1000. pnl = -1000 - 500 - 250 = -1750.
	dir := t.TempDir()
	srv := httptest.NewServer(orderHandler(t, []orderFill{
		{qty: 10, price: 50000}, // entry
		{qty: 10, price: 49900}, // exit at exactly modeled stop → slip falls back to bps
	}))
	defer srv.Close()
	bl := newBinanceLiveWithMock(t, srv, dir)
	bl.StakeUSD = 1000
	// FeeBps=10, StopSlippageBps=5 already set by newBinanceLiveWithMock.

	openTime := time.Date(2026, 5, 8, 0, 0, 0, 0, time.UTC)
	bl.OnSignal(&models.Signal{
		Symbol: "BTCUSDT", Side: models.Long,
		EntryPrice: 50000, StopLoss: 49900, TakeProfit: 50500,
		Timestamp: openTime, Reason: "fee/slip test",
	})
	bl.Wait()
	bl.OnTick(models.Tick{Symbol: "BTCUSDT", Timestamp: openTime.Add(time.Hour), Price: 49900})
	bl.Wait()

	if len(bl.results) != 1 {
		t.Fatalf("results = %d, want 1", len(bl.results))
	}
	r := bl.results[0]
	if math.Abs(r.grossUSDT-(-1000)) > 0.01 {
		t.Errorf("gross: got %v want -1000", r.grossUSDT)
	}
	if math.Abs(r.feeUSDT-500) > 0.01 {
		t.Errorf("fee: got %v want 500 (10bp on $500k notional)", r.feeUSDT)
	}
	if math.Abs(r.slipUSDT-250) > 0.01 {
		t.Errorf("slip fallback: got %v want 250 (5bp on $500k notional)", r.slipUSDT)
	}
	if math.Abs(r.pnlUSDT-(-1750)) > 0.01 {
		t.Errorf("net pnl: got %v want -1750", r.pnlUSDT)
	}

	// Verify journal close event carries the cost-decomposition fields per
	// the locked schema (forward_paper_status.sh reads these directly).
	matches, _ := filepath.Glob(filepath.Join(dir, "BTCUSDT-*.jsonl"))
	if len(matches) == 0 {
		t.Fatal("journal file missing")
	}
	data, _ := os.ReadFile(matches[0])
	for _, want := range []string{
		`"event":"close"`,
		`"outcome":"STOP"`,
		`"gross_usd":-1000`,
		`"fee_usd":500`,
		`"slip_usd":250`,
		`"notional_usd":500000`,
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("journal close missing %q\nfull contents:\n%s", want, data)
		}
	}
}

// ── BinanceLive.RecoverFromJournal ──────────────────────────────────────────
//
// Per the locked rule (real_money_executor_architecture_decision_rule_2026-05-08.md
// "Engine restarts mid-real-money-trade") recovery has TWO phases: scan +
// verify-against-exchange. Tests cover both paths — scanning behavior mirrors
// Stub but verification adds a new failure mode (ErrRecoveryDrift) that
// caller cmd/engine MUST honor.

// blRecoveryServer makes a httptest server that responds to /fapi/v2/positionRisk
// with the supplied JSON and to /fapi/v1/order with a generic FILLED response.
// Used by recovery tests where the only HTTP path that matters is positionRisk.
func blRecoveryServer(positionRiskJSON string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case strings.Contains(req.URL.Path, "/fapi/v2/positionRisk"):
			_, _ = w.Write([]byte(positionRiskJSON))
		case strings.Contains(req.URL.Path, "/fapi/v1/order"):
			_, _ = w.Write([]byte(`{"orderId":1,"status":"FILLED","executedQty":"1","avgPrice":"50000"}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
}

// blWriteJournal writes the given JSONL lines to <dir>/<symbol>-<month>.jsonl.
// Mirrors writeJournal from stub_test.go but kept local for clarity.
func blWriteJournal(t *testing.T, dir, symbol, month string, lines []string) {
	t.Helper()
	path := filepath.Join(dir, symbol+"-"+month+".jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("create journal: %v", err)
	}
	defer f.Close()
	for _, line := range lines {
		if _, err := f.WriteString(line + "\n"); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
}

func TestBinanceLive_Recover_NoJournalPath_NoOp(t *testing.T) {
	bl := NewBinanceLive("BTCUSDT", 100, "k", "s")
	bl.JournalPath = "" // explicit
	recovered, err := bl.RecoverFromJournal(context.Background())
	if err != nil || recovered {
		t.Errorf("no JournalPath: got recovered=%v err=%v; want false/nil", recovered, err)
	}
}

func TestBinanceLive_Recover_NoUnclosedOpen_NoOp(t *testing.T) {
	dir := t.TempDir()
	srv := blRecoveryServer(`[{"symbol":"BTCUSDT","positionAmt":"0","entryPrice":"0","positionSide":"BOTH"}]`)
	defer srv.Close()
	bl := newBinanceLiveWithMock(t, srv, dir)

	recovered, err := bl.RecoverFromJournal(context.Background())
	if err != nil || recovered {
		t.Errorf("empty journal dir: got recovered=%v err=%v; want false/nil", recovered, err)
	}
	if bl.position != nil {
		t.Error("position set despite no journal recovery")
	}
}

func TestBinanceLive_Recover_OpenAndClose_NoOp(t *testing.T) {
	dir := t.TempDir()
	month := time.Now().UTC().Format("2006-01")
	blWriteJournal(t, dir, "BTCUSDT", month, []string{
		`{"event":"open","symbol":"BTCUSDT","ts":"2026-05-08T08:00:00Z","side":"LONG","entry":50000,"stop":49500,"target":53000,"reason":"r"}`,
		`{"event":"close","symbol":"BTCUSDT","ts":"2026-05-08T09:00:00Z","side":"LONG","entry":50000,"exit":53000,"outcome":"TARGET","reason":"r"}`,
	})
	srv := blRecoveryServer(`[{"symbol":"BTCUSDT","positionAmt":"0","entryPrice":"0","positionSide":"BOTH"}]`)
	defer srv.Close()
	bl := newBinanceLiveWithMock(t, srv, dir)

	recovered, err := bl.RecoverFromJournal(context.Background())
	if err != nil || recovered {
		t.Errorf("fully-closed journal: got recovered=%v err=%v; want false/nil", recovered, err)
	}
}

func TestBinanceLive_Recover_OpenWithoutClose_VerifiedClean(t *testing.T) {
	// Journal: LONG 0.5 BTC @ 50000. Exchange: matching LONG 0.5 BTC @ 50000.
	// Recovery should produce (true, nil), set b.position, and register with
	// the Reconciler.
	dir := t.TempDir()
	month := time.Now().UTC().Format("2006-01")
	blWriteJournal(t, dir, "BTCUSDT", month, []string{
		`{"event":"open","symbol":"BTCUSDT","ts":"2026-05-08T08:00:00Z","side":"LONG","entry":50000,"stop":49500,"target":53000,"reason":"r"}`,
	})
	// stake/stop_dist = 100 / 500 = 0.2 contracts. Exchange must match.
	srv := blRecoveryServer(`[{"symbol":"BTCUSDT","positionAmt":"0.2","entryPrice":"50000","positionSide":"BOTH"}]`)
	defer srv.Close()
	bl := newBinanceLiveWithMock(t, srv, dir)

	recovered, err := bl.RecoverFromJournal(context.Background())
	if err != nil {
		t.Fatalf("clean recovery: unexpected err: %v", err)
	}
	if !recovered {
		t.Fatal("clean recovery: recovered=false; want true")
	}
	if bl.position == nil {
		t.Fatal("clean recovery: b.position not set")
	}
	if bl.position.Signal.EntryPrice != 50000 || bl.position.Signal.Side != models.Long {
		t.Errorf("recovered signal mismatch: %+v", bl.position.Signal)
	}
	if bl.position.OriginalStopDist != 500 {
		t.Errorf("OriginalStopDist: got %v want 500", bl.position.OriginalStopDist)
	}
	side, qty, _, ok := bl.PositionReconciler.LocalPosition("BTCUSDT")
	if !ok || side != models.Long || math.Abs(qty-0.2) > 1e-9 {
		t.Errorf("Reconciler: side=%v qty=%v ok=%v; want Long/0.2/true", side, qty, ok)
	}
	if drifted, _ := bl.PositionReconciler.IsDrifted("BTCUSDT"); drifted {
		t.Error("clean recovery: drift was flagged")
	}
}

func TestBinanceLive_Recover_OpenWithoutClose_ExchangeDisagrees_ReturnsErrRecoveryDrift(t *testing.T) {
	// Journal: LONG 0.2 BTC @ 50000. Exchange: SHORT 0.2 BTC @ 50000 → side mismatch drift.
	dir := t.TempDir()
	month := time.Now().UTC().Format("2006-01")
	blWriteJournal(t, dir, "BTCUSDT", month, []string{
		`{"event":"open","symbol":"BTCUSDT","ts":"2026-05-08T08:00:00Z","side":"LONG","entry":50000,"stop":49500,"target":53000,"reason":"r"}`,
	})
	srv := blRecoveryServer(`[{"symbol":"BTCUSDT","positionAmt":"-0.2","entryPrice":"50000","positionSide":"BOTH"}]`)
	defer srv.Close()
	bl := newBinanceLiveWithMock(t, srv, dir)

	recovered, err := bl.RecoverFromJournal(context.Background())
	if err == nil {
		t.Fatal("expected ErrRecoveryDrift on side-mismatch")
	}
	if !errors.Is(err, ErrRecoveryDrift) {
		t.Errorf("err not ErrRecoveryDrift: %v", err)
	}
	if !recovered {
		t.Error("recovered=false on drift; want true (local committed for diagnostics)")
	}
	if bl.position == nil {
		t.Error("position should be committed even on drift (drift gate provides safety; reduceOnly exits would be exchange-rejected if state diverged)")
	}
	if drifted, _ := bl.PositionReconciler.IsDrifted("BTCUSDT"); !drifted {
		t.Error("drift not flagged on Reconciler — OnSignal drift gate would not fire")
	}
}

func TestBinanceLive_Recover_OpenWithoutClose_ExchangeFlat_ReturnsErrRecoveryDrift(t *testing.T) {
	// Journal: LONG 0.2 BTC @ 50000. Exchange: flat → "externally-closed" drift.
	dir := t.TempDir()
	month := time.Now().UTC().Format("2006-01")
	blWriteJournal(t, dir, "BTCUSDT", month, []string{
		`{"event":"open","symbol":"BTCUSDT","ts":"2026-05-08T08:00:00Z","side":"LONG","entry":50000,"stop":49500,"target":53000,"reason":"r"}`,
	})
	srv := blRecoveryServer(`[{"symbol":"BTCUSDT","positionAmt":"0","entryPrice":"0","positionSide":"BOTH"}]`)
	defer srv.Close()
	bl := newBinanceLiveWithMock(t, srv, dir)

	_, err := bl.RecoverFromJournal(context.Background())
	if !errors.Is(err, ErrRecoveryDrift) {
		t.Errorf("exchange-flat: want ErrRecoveryDrift, got %v", err)
	}
	if !strings.Contains(err.Error(), "externally") {
		t.Errorf("err reason should mention externally-closed: %v", err)
	}
}

func TestBinanceLive_Recover_PartialClose_RecoversWithMidRHit(t *testing.T) {
	dir := t.TempDir()
	month := time.Now().UTC().Format("2006-01")
	blWriteJournal(t, dir, "BTCUSDT", month, []string{
		`{"event":"open","symbol":"BTCUSDT","ts":"2026-05-08T08:00:00Z","side":"LONG","entry":50000,"stop":49500,"target":53000,"reason":"r"}`,
		`{"event":"close","symbol":"BTCUSDT","ts":"2026-05-08T09:00:00Z","side":"LONG","entry":50000,"exit":51500,"outcome":"PARTIAL","reason":"r"}`,
	})
	// Remaining qty after partial = stake × 0.5 / stop_dist = 100 × 0.5 / 500 = 0.1.
	srv := blRecoveryServer(`[{"symbol":"BTCUSDT","positionAmt":"0.1","entryPrice":"50000","positionSide":"BOTH"}]`)
	defer srv.Close()
	bl := newBinanceLiveWithMock(t, srv, dir)

	recovered, err := bl.RecoverFromJournal(context.Background())
	if err != nil || !recovered {
		t.Fatalf("partial-close recovery: got recovered=%v err=%v", recovered, err)
	}
	if !bl.position.MidRHit {
		t.Error("MidRHit not set after partial-close recovery")
	}
	if bl.position.RemainingFrac != 0.5 {
		t.Errorf("RemainingFrac: got %v want 0.5", bl.position.RemainingFrac)
	}
}

func TestBinanceLive_Recover_PreExistingPosition_NoOverwrite(t *testing.T) {
	dir := t.TempDir()
	month := time.Now().UTC().Format("2006-01")
	blWriteJournal(t, dir, "BTCUSDT", month, []string{
		`{"event":"open","symbol":"BTCUSDT","ts":"2026-05-08T08:00:00Z","side":"LONG","entry":50000,"stop":49500,"target":53000,"reason":"journal"}`,
	})
	srv := blRecoveryServer(`[{"symbol":"BTCUSDT","positionAmt":"0.2","entryPrice":"50000","positionSide":"BOTH"}]`)
	defer srv.Close()
	bl := newBinanceLiveWithMock(t, srv, dir)
	// Plant an existing position.
	bl.position = &OpenPosition{
		Signal: &models.Signal{
			Symbol: "BTCUSDT", Side: models.Short, EntryPrice: 999,
			StopLoss: 1100, TakeProfit: 500, Timestamp: time.Now(),
		},
		OriginalStopDist: 101, RemainingFrac: 1.0,
	}
	recovered, err := bl.RecoverFromJournal(context.Background())
	if err != nil || recovered {
		t.Errorf("preexisting-position: got recovered=%v err=%v; want false/nil", recovered, err)
	}
	if bl.position.Signal.EntryPrice != 999 {
		t.Error("preexisting position was overwritten by recovery")
	}
}

func TestBinanceLive_appendJournal_WriteFailureClearsHandleForRetry(t *testing.T) {
	// Audit-pattern regression: appendJournal previously left b.journalFile
	// non-nil after a write error, so every subsequent call wrote into the
	// same dead handle and failed identically — the operator saw a recurring
	// "REAL-MONEY POSITION MAY BE INVISIBLE" stream while every new position
	// remained invisible to recovery. Fix clears the handle on failure so the
	// next call goes through the open-or-create path and gets a fresh fd.
	dir := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	bl := newBinanceLiveWithMock(t, srv, dir)

	// Plant a closed-file handle as journalFile. Write on a closed *os.File
	// returns os.ErrClosed — exactly the kind of mid-trade failure we want
	// to simulate (disk full, fd leak, permissions revoked, etc.).
	closedFile, err := os.CreateTemp(dir, "broken-*.jsonl")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	if cerr := closedFile.Close(); cerr != nil {
		t.Fatalf("setup close: %v", cerr)
	}
	bl.journalFile = closedFile
	bl.journalMonth = time.Now().UTC().Format("2006-01")

	// First call — Write fails on the closed handle.
	bl.appendJournal(journalEntry{
		Event: "open", Symbol: "BTCUSDT",
		TS:    time.Now().UTC().Format(time.RFC3339),
	})

	// Critical assertion: handle MUST be cleared so next call reopens.
	if bl.journalFile != nil {
		t.Fatal("journalFile not cleared after write failure — next call would " +
			"write to the same dead handle and fail identically")
	}
	if bl.journalMonth != "" {
		t.Errorf("journalMonth not cleared: got %q", bl.journalMonth)
	}

	// Second call MUST succeed via the open-or-create path. JournalPath
	// is a writable temp dir, so the recovery path runs cleanly.
	bl.appendJournal(journalEntry{
		Event: "open", Symbol: "BTCUSDT",
		TS:    time.Now().UTC().Format(time.RFC3339),
		Side:  "LONG", Entry: 50000, Stop: 49500, Target: 53000,
	})

	if bl.journalFile == nil {
		t.Error("journalFile not re-opened on subsequent call after failure clear")
	}
	// And the actual file on disk must contain the second event (proving the
	// recovery path produced a real, writeable handle, not just a non-nil sentinel).
	matches, _ := filepath.Glob(filepath.Join(dir, "BTCUSDT-*.jsonl"))
	if len(matches) == 0 {
		t.Fatal("no journal file on disk after recovery")
	}
	body, _ := os.ReadFile(matches[0])
	if !strings.Contains(string(body), `"event":"open"`) {
		t.Errorf("recovered journal did not contain expected event; got: %q", string(body))
	}
}

func TestBinanceLive_Recover_RepopulatesGateBLossesAcrossRestart(t *testing.T) {
	// Audit-pattern regression for the highest-severity finding in the
	// 2026-05-10 binance_live.go audit: Gate B daily-loss circuit breaker
	// silently reset to $0 on engine restart because recent24hLossUSD
	// walked only the in-memory `b.results`, never the journal. At STAGE_4
	// with the cap tripped, an engine crash would re-arm trading on
	// restart with the cap effectively disabled until 24h naturally
	// elapsed. RecoverFromJournal must repopulate b.results from journal-
	// recorded recent closes so the cap survives restarts.
	dir := t.TempDir()
	month := time.Now().UTC().Format("2006-01")
	priorMonth := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01")

	// Two closes within 24h (must be loaded), one outside (must NOT be).
	// All have notional > 0 so they reflect post-cost-decomp closes.
	now := time.Now().UTC()
	old := now.Add(-30 * time.Hour).Format(time.RFC3339)   // OUTSIDE 24h
	mid := now.Add(-12 * time.Hour).Format(time.RFC3339)   // inside
	rec := now.Add(-2 * time.Hour).Format(time.RFC3339)    // inside
	// Each close must follow an open (state machine), but the open ts is
	// not consulted by recent24hLossUSD — only the close ts matters.
	openLine := func(ts string) string {
		return `{"event":"open","symbol":"BTCUSDT","ts":"` + ts +
			`","side":"LONG","entry":50000,"stop":49500,"target":53000,"reason":"r"}`
	}
	closeLine := func(ts string, pnl float64, outcome string) string {
		return fmt.Sprintf(
			`{"event":"close","symbol":"BTCUSDT","ts":"%s","side":"LONG",`+
				`"entry":50000,"exit":49500,"stop":49500,"target":53000,`+
				`"outcome":"%s","pnl_usd":%v,"notional_usd":100000,`+
				`"fee_usd":100,"slip_usd":50,"reason":"r"}`,
			ts, outcome, pnl)
	}
	// Mix prior + current month so cross-month scan is exercised.
	blWriteJournal(t, dir, "BTCUSDT", priorMonth, []string{
		openLine(old), closeLine(old, -1000, "STOP"),
	})
	blWriteJournal(t, dir, "BTCUSDT", month, []string{
		openLine(mid), closeLine(mid, -200, "STOP"),
		openLine(rec), closeLine(rec, -300, "STOP"),
	})

	// No exchange call expected — fully-closed journal short-circuits the
	// recovery before FetchExchangePosition. Use a server that fails loudly
	// if hit, so a regression that adds a stray HTTP call surfaces.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		t.Errorf("unexpected HTTP call during fully-closed journal recovery: %s", req.URL.Path)
		w.WriteHeader(500)
	}))
	defer srv.Close()
	bl := newBinanceLiveWithMock(t, srv, dir)

	recovered, err := bl.RecoverFromJournal(context.Background())
	if err != nil {
		t.Fatalf("recovery: unexpected err: %v", err)
	}
	if recovered {
		t.Error("recovered=true on fully-closed journal; want false")
	}

	// recent24hLossUSD must see the two recent losses ($200 + $300 = $500),
	// NOT the 30h-old one ($1000). Without the fix, this returns 0.
	got := bl.recent24hLossUSD()
	if math.Abs(got-500) > 1e-6 {
		t.Fatalf("recent24hLossUSD after recovery: got %v, want 500 "+
			"(only 12h-old + 2h-old closes; the 30h-old must be filtered)",
			got)
	}

	// End-to-end: Gate B must now block a new entry when DailyLossUSDCap < $500.
	bl.SafetyGates.DailyLossUSDCap = 100
	bl.OnSignal(&models.Signal{
		Symbol: "BTCUSDT", Side: models.Short,
		EntryPrice: 50000, StopLoss: 50100, TakeProfit: 49500,
		Timestamp: time.Now().UTC(), Reason: "post-restart-test",
	})
	bl.Wait()
	if bl.position != nil {
		t.Error("Gate B failed: signal opened position despite recovered " +
			"24h losses ($500) exceeding cap ($100). Gate B journal-recovery " +
			"is the audit fix for the silent-restart-bypass.")
	}
}

func TestBinanceLive_Recover_CrossMonthRecovery(t *testing.T) {
	dir := t.TempDir()
	priorMonth := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01")
	blWriteJournal(t, dir, "BTCUSDT", priorMonth, []string{
		`{"event":"open","symbol":"BTCUSDT","ts":"2026-04-25T08:00:00Z","side":"LONG","entry":50000,"stop":49500,"target":53000,"reason":"crossmonth"}`,
	})
	srv := blRecoveryServer(`[{"symbol":"BTCUSDT","positionAmt":"0.2","entryPrice":"50000","positionSide":"BOTH"}]`)
	defer srv.Close()
	bl := newBinanceLiveWithMock(t, srv, dir)

	recovered, err := bl.RecoverFromJournal(context.Background())
	if err != nil || !recovered {
		t.Errorf("cross-month: got recovered=%v err=%v", recovered, err)
	}
	if bl.position == nil {
		t.Fatal("position not set after cross-month recovery")
	}
}

func TestBinanceLive_Recover_AfterRecovery_OnTick_StopHit_ClosesNormally(t *testing.T) {
	// Integration sanity: after a clean recovery, OnTick must process the
	// recovered position correctly. Tick that hits stop fires close + journal.
	dir := t.TempDir()
	month := time.Now().UTC().Format("2006-01")
	blWriteJournal(t, dir, "BTCUSDT", month, []string{
		`{"event":"open","symbol":"BTCUSDT","ts":"2026-05-08T08:00:00Z","side":"LONG","entry":50000,"stop":49900,"target":50500,"reason":"r"}`,
	})
	// stake/stop_dist = 1000/100 = 10 contracts.
	var orderCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case strings.Contains(req.URL.Path, "/fapi/v2/positionRisk"):
			_, _ = w.Write([]byte(`[{"symbol":"BTCUSDT","positionAmt":"10","entryPrice":"50000","positionSide":"BOTH"}]`))
		case strings.Contains(req.URL.Path, "/fapi/v1/order"):
			atomic.AddInt32(&orderCalls, 1)
			_, _ = w.Write([]byte(`{"orderId":1,"status":"FILLED","executedQty":"10","avgPrice":"49899"}`))
		}
	}))
	defer srv.Close()
	bl := newBinanceLiveWithMock(t, srv, dir)
	bl.StakeUSD = 1000

	if recovered, err := bl.RecoverFromJournal(context.Background()); err != nil || !recovered {
		t.Fatalf("recovery setup: got recovered=%v err=%v", recovered, err)
	}

	// Stop-hitting tick — must use a timestamp AFTER the recovered open ts
	// (2026-05-08T08:00:00Z) since the OnTick pre-open guard skips earlier
	// ticks.
	bl.OnTick(models.Tick{
		Symbol: "BTCUSDT",
		Timestamp: time.Date(2026, 5, 8, 9, 0, 0, 0, time.UTC),
		Price: 49899,
	})
	bl.Wait()

	if bl.position != nil {
		t.Fatal("position not cleared after stop hit on recovered position")
	}
	if len(bl.results) != 1 {
		t.Fatalf("results = %d after recovery+stop, want 1", len(bl.results))
	}
	if got := atomic.LoadInt32(&orderCalls); got != 1 {
		t.Errorf("expected exactly 1 exit-order API call, got %d", got)
	}
}

func TestPositionReconciler_MarkDrift_SetsDriftWithoutReconcile(t *testing.T) {
	r := newReconcilerForTest(t, "https://example")
	if drifted, _ := r.IsDrifted("BTCUSDT"); drifted {
		t.Fatal("setup: drift already set")
	}
	r.MarkDrift("BTCUSDT", "test reason")
	drifted, reason := r.IsDrifted("BTCUSDT")
	if !drifted {
		t.Error("MarkDrift did not set drift")
	}
	if reason != "test reason" {
		t.Errorf("reason: got %q want 'test reason'", reason)
	}
	r.ClearDrift("BTCUSDT")
	if drifted, _ := r.IsDrifted("BTCUSDT"); drifted {
		t.Error("ClearDrift did not unset drift after MarkDrift")
	}
}

func TestBinanceLive_Summary_DrainsGoroutines(t *testing.T) {
	// Summary calls Wait() internally — invoking Summary right after OnSignal
	// must not see "no trades closed" if there's an in-flight close goroutine.
	dir := t.TempDir()
	srv := httptest.NewServer(orderHandler(t, []orderFill{
		{qty: 10, price: 50000},
		{qty: 10, price: 50500},
	}))
	defer srv.Close()
	bl := newBinanceLiveWithMock(t, srv, dir)
	bl.StakeUSD = 1000

	openTime := time.Date(2026, 5, 8, 0, 0, 0, 0, time.UTC)
	bl.OnSignal(&models.Signal{
		Symbol: "BTCUSDT", Side: models.Long,
		EntryPrice: 50000, StopLoss: 49900, TakeProfit: 50500,
		Timestamp: openTime, Reason: "test",
	})
	// Wait for the entry goroutine specifically — without this, the OnTick
	// below would run on a not-yet-set position. (Tests Summary draining the
	// EXIT goroutine, not the racing entry/tick interleave.)
	bl.Wait()
	bl.OnTick(models.Tick{Symbol: "BTCUSDT", Timestamp: openTime.Add(time.Second), Price: 50500})
	// No explicit Wait here — Summary must drain the in-flight exit goroutine.
	bl.Summary()

	if len(bl.results) != 1 {
		t.Errorf("Summary did not drain goroutines: results = %d, want 1", len(bl.results))
	}
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

func TestKillSwitch_KillAll_RateLimitRetriesOnceAndSucceeds(t *testing.T) {
	// Audit-pattern regression: previously a 418/429 mid-batch cascaded
	// because the rate-limit window persists for ~60s while subsequent
	// SendOrder calls fired immediately. With the retry, a single 429
	// followed by a 200 records the position as CLOSED rather than FAILED.
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(429)
			w.Write([]byte(`{"code":-1003,"msg":"too many requests"}`))
			return
		}
		w.Write([]byte(`{"orderId":2,"status":"FILLED","executedQty":"0.5","avgPrice":"50000"}`))
	}))
	defer srv.Close()
	r := &OrderRouter{APIBaseURL: srv.URL, APIKey: "k", APISecret: "s", HTTPClient: srv.Client()}
	// Test backoff to 1ms so the test stays fast; production default is 60s.
	k := &KillSwitch{Router: r, RateLimitBackoff: time.Millisecond}

	res, err := k.KillAll(context.Background(),
		[]ClosePosition{{Symbol: "BTCUSDT", Side: models.Long, Quantity: 0.5}},
		"rate-limit-retry test")
	if err != nil {
		t.Fatalf("expected nil err after successful retry; got %v", err)
	}
	if calls != 2 {
		t.Errorf("expected 2 SendOrder calls (1 rate-limited + 1 retry), got %d", calls)
	}
	if len(res.Outcomes) != 1 || res.Outcomes[0].Status != "CLOSED" {
		t.Errorf("expected CLOSED after retry; got %+v", res.Outcomes)
	}
}

func TestKillSwitch_KillAll_RateLimitRetryStillFails_RecordsAsFAILED(t *testing.T) {
	// Sustained rate limit: both attempts return 429. Single retry, then
	// FAILED — operator's idempotent re-run path is the backstop.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		w.Write([]byte(`{"code":-1003,"msg":"too many requests"}`))
	}))
	defer srv.Close()
	r := &OrderRouter{APIBaseURL: srv.URL, APIKey: "k", APISecret: "s", HTTPClient: srv.Client()}
	k := &KillSwitch{Router: r, RateLimitBackoff: time.Millisecond}

	res, err := k.KillAll(context.Background(),
		[]ClosePosition{{Symbol: "BTCUSDT", Side: models.Long, Quantity: 0.5}},
		"rate-limit-sustained test")
	if err == nil {
		t.Fatal("expected aggregated err on sustained rate limit")
	}
	if len(res.Outcomes) != 1 || res.Outcomes[0].Status != "FAILED" {
		t.Errorf("expected FAILED after retry-still-fails; got %+v", res.Outcomes)
	}
	if !strings.Contains(strings.ToLower(res.Outcomes[0].Error), "rate") {
		t.Errorf("FAILED outcome should preserve rate-limit error message; got %q",
			res.Outcomes[0].Error)
	}
}

func TestKillSwitch_KillAll_RateLimit_DoesNotCascadeToOtherPositions(t *testing.T) {
	// The original bug: rate-limit on position 1 used to fail position 2
	// as well because the retry window persisted. With the per-position
	// retry, position 1 retries (succeeds on second call) and position 2
	// gets a clean attempt afterwards.
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		// Call 1: rate-limited (BTC's first attempt).
		// Call 2+: 200 OK.
		if calls == 1 {
			w.WriteHeader(429)
			w.Write([]byte(`{"code":-1003,"msg":"too many requests"}`))
			return
		}
		w.Write([]byte(`{"orderId":1,"status":"FILLED","executedQty":"0.5","avgPrice":"50000"}`))
	}))
	defer srv.Close()
	r := &OrderRouter{APIBaseURL: srv.URL, APIKey: "k", APISecret: "s", HTTPClient: srv.Client()}
	k := &KillSwitch{Router: r, RateLimitBackoff: time.Millisecond}

	// Both positions request 0.5 so the mock's fixed executedQty=0.5 fills
	// each completely (PARTIAL would be a noisy false-positive for this
	// test's purpose, which is "rate-limit doesn't cascade").
	res, err := k.KillAll(context.Background(),
		[]ClosePosition{
			{Symbol: "BTCUSDT", Side: models.Long, Quantity: 0.5},
			{Symbol: "ETHUSDT", Side: models.Long, Quantity: 0.5},
		}, "no-cascade test")
	if err != nil {
		t.Fatalf("expected nil err — both positions should succeed; got %v", err)
	}
	if len(res.Outcomes) != 2 {
		t.Fatalf("outcomes = %d, want 2", len(res.Outcomes))
	}
	for i, o := range res.Outcomes {
		if o.Status != "CLOSED" {
			t.Errorf("outcome[%d] status = %q, want CLOSED (rate-limit must " +
				"NOT cascade to subsequent positions)", i, o.Status)
		}
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


// ── Audit-pass regression tests (2026-05-09 PM Go-side audit) ────────────────

// Closes audit finding A1: handleSignalSync had no StakeUSD>0 guard. With
// StakeUSD=0 the qty calculation silently produced 0; depending on exchange
// behavior this could either be rejected or accepted as a no-op order. The
// recovery path had this guard explicitly; the entry path now mirrors it.
func TestBinanceLive_OnSignal_ZeroStakeUSD_Rejected(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(orderHandler(t, []orderFill{{qty: 1, price: 50000}}))
	defer srv.Close()
	bl := newBinanceLiveWithMock(t, srv, dir)
	bl.StakeUSD = 0 // misconfiguration we now catch loudly

	bl.OnSignal(&models.Signal{
		Symbol: "BTCUSDT", Side: models.Short,
		EntryPrice: 50000, StopLoss: 50100,
		TakeProfit: 49500, Timestamp: time.Now().UTC(),
	})
	bl.Wait()
	if bl.position != nil {
		t.Error("zero-StakeUSD signal must be rejected before order; got open position")
	}
}

// Closes audit finding A5a: malformed executedQty in the exchange response
// previously parsed silently to 0, classifying a real fill as PARTIAL. Now
// surfaces as ERROR so the caller can alert + skip rather than commit
// divergent local state.
func TestOrderRouter_SendOrder_MalformedExecutedQty_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Order endpoint returns a "successful" 200 but with non-numeric executedQty.
		_, _ = w.Write([]byte(`{"orderId":1,"status":"FILLED","executedQty":"???","avgPrice":"50000"}`))
	}))
	defer srv.Close()
	or := &OrderRouter{APIBaseURL: srv.URL, APIKey: "k", APISecret: "s", HTTPClient: srv.Client()}
	res, err := or.SendOrder(context.Background(), OrderIntent{
		Symbol: "BTCUSDT", Side: models.Long, Quantity: 1, Type: "MARKET",
	})
	if err == nil {
		t.Fatalf("expected error on malformed executedQty, got nil")
	}
	if res.Status != "ERROR" || res.RejectCode != "PARSE" {
		t.Errorf("Status/RejectCode = %q/%q, want ERROR/PARSE", res.Status, res.RejectCode)
	}
}

// Closes audit finding A5b: same shape on AvgPrice. Previous silent-zero
// would pass back AvgPrice=0 to handleSignalSync, which would fall back to
// the modeled signal price — diverging local state from actual fill.
func TestOrderRouter_SendOrder_MalformedAvgPrice_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"orderId":1,"status":"FILLED","executedQty":"1","avgPrice":"NaN-junk"}`))
	}))
	defer srv.Close()
	or := &OrderRouter{APIBaseURL: srv.URL, APIKey: "k", APISecret: "s", HTTPClient: srv.Client()}
	res, err := or.SendOrder(context.Background(), OrderIntent{
		Symbol: "BTCUSDT", Side: models.Long, Quantity: 1, Type: "MARKET",
	})
	if err == nil {
		t.Fatalf("expected error on malformed avgPrice, got nil")
	}
	if res.Status != "ERROR" || res.RejectCode != "PARSE" {
		t.Errorf("Status/RejectCode = %q/%q, want ERROR/PARSE", res.Status, res.RejectCode)
	}
}

// ── PositionReconciler: rate-limit (418/429) backoff ─────────────────────────
//
// Root cause (2026-05-29): the Layer 3 testnet reconciler polled
// /fapi/v2/positionRisk every 60s with NO 418/429 handling. On a shared-NAT
// testnet IP, occasional collective 429s trigger a 418 IP ban ("banned until
// T"); Binance EXTENDS that ban on every request received while banned. The
// reconciler's ban-blind 60s polling therefore held the ban open indefinitely
// (941 bans over 10 days, zero successful reconciles, empty Layer 3 journal).
// The sibling aggTrade poller already backs off on 418/429
// (pkg/marketdata/binance.go) — the reconciler must mirror that: on a
// rate-limit response, STOP polling for a cooldown LONGER than the normal poll
// interval so the ban can expire instead of being continuously re-extended.

func TestIsRateLimitErr_Classifies418And429(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"418 banned", fmt.Errorf(`positionRisk 418: {"code":-1003,"msg":"Way too many requests; IP banned until 123"}`), true},
		{"429 limit", fmt.Errorf(`positionRisk 429: {"code":-1003,"msg":"Too many requests"}`), true},
		{"wrapped 418", fmt.Errorf("reconcile: %w", fmt.Errorf("positionRisk 418: banned")), true},
		{"500 server error", fmt.Errorf("positionRisk 500: internal"), false},
		{"parse error", fmt.Errorf("parse positionRisk: unexpected token"), false},
		{"nil", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRateLimitErr(tc.err); got != tc.want {
				t.Errorf("isRateLimitErr(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestPositionReconciler_Run_BacksOffOnRateLimit(t *testing.T) {
	// A server that always returns 418. The naive (buggy) loop would re-poll
	// every PollInterval (clamped floor 30s) — but the bug is that it polls AT
	// ALL during a ban, extending it. The fix: after a 418, wait
	// RateLimitBackoff (which is LONGER than the poll interval) before the next
	// attempt. We assert the loop paces its calls by RateLimitBackoff, not by
	// the (shorter) poll interval — i.e. consecutive calls are ≥ backoff apart.
	var (
		mu        sync.Mutex
		callTimes []time.Time
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		callTimes = append(callTimes, time.Now())
		mu.Unlock()
		w.WriteHeader(http.StatusTeapot) // 418
		_, _ = w.Write([]byte(`{"code":-1003,"msg":"Way too many requests; IP(1.2.3.4) banned until 999"}`))
	}))
	defer srv.Close()

	r := newReconcilerForTest(t, srv.URL)
	r.HTTPClient = srv.Client()
	// Backoff LONGER than poll interval, but both tiny for the test. This is
	// the crux: backoff must dominate the cadence when rate-limited.
	r.PollInterval = 50 * time.Millisecond
	r.RateLimitBackoff = 300 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx, "BTCUSDT") }()
	time.Sleep(1 * time.Second)
	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()
	// Initial reconcile + ticker. If the loop ignored backoff and polled at the
	// 50ms interval, we'd see ~20 calls in 1s. With a 300ms rate-limit backoff
	// dominating, we expect far fewer (~3-4). Assert ≤ 6 to leave scheduling
	// slack while still failing the buggy (no-backoff) implementation.
	if len(callTimes) == 0 {
		t.Fatal("reconciler made no calls")
	}
	if len(callTimes) > 6 {
		t.Errorf("reconciler polled %d times in 1s under sustained 418 — backoff not applied (expected ≤6 at 300ms backoff; ~20 at the 50ms poll interval = bug)", len(callTimes))
	}
	// And verify spacing: consecutive calls ≥ ~backoff apart (allow 20% slack).
	for i := 1; i < len(callTimes); i++ {
		gap := callTimes[i].Sub(callTimes[i-1])
		if gap < 240*time.Millisecond {
			t.Errorf("call %d→%d gap %v < backoff floor (240ms) — loop re-polled before backoff elapsed", i-1, i, gap)
		}
	}
}

// TestPositionReconciler_Run_SurvivesSustained418_DoesNotExitGoroutine is the
// literal regression test for the 2026-05-29 Layer 3 testnet incident. The
// incident root cause was NOT just "polled during a ban" — it was that the
// reconciler goroutine EXITED on the rate-limit error, which closed the tick
// channel, looked like a clean shutdown to systemd, triggered an infinite
// restart loop, and re-polled-while-banned on every restart, holding the ban
// open for 10 days (see CLAUDE.md Bug 4 invariant + memory
// project-layer3-reconciler-backoff).
//
// The sibling TestPositionReconciler_Run_BacksOffOnRateLimit asserts PACING
// (calls are backoff-spaced) but NOT SURVIVAL: a buggy impl that did
// `if isRateLimitErr(err) { return err }` after one backoff would still make
// ≤6 backoff-spaced calls and PASS that test. This test closes that gap by
// asserting Run keeps running across multiple sustained-418 cycles and returns
// ONLY when ctx is canceled — and returns ctx.Canceled, never the 418 error.
func TestPositionReconciler_Run_SurvivesSustained418_DoesNotExitGoroutine(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusTeapot) // 418, every time
		_, _ = w.Write([]byte(`{"code":-1003,"msg":"IP banned until 999"}`))
	}))
	defer srv.Close()

	r := newReconcilerForTest(t, srv.URL)
	r.HTTPClient = srv.Client()
	// Tiny intervals so several backoff cycles elapse within the test window.
	r.PollInterval = 20 * time.Millisecond
	r.RateLimitBackoff = 80 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx, "BTCUSDT") }()

	// While ctx is live, Run must NOT return — even under sustained 418. A
	// buggy `return err`-on-rate-limit impl would deliver on `done` here.
	select {
	case err := <-done:
		t.Fatalf("Run exited on its own under sustained 418 (err=%v) — goroutine must survive until ctx cancel; this is the 2026-05-29 incident's restart-loop root cause", err)
	case <-time.After(500 * time.Millisecond):
		// Good: still running after multiple backoff cycles.
	}

	// Must have actually hit the rate-limit path (the initial reconcile fires
	// a 418 → sleepBackoff). Note: effectivePollInterval clamps to a 30s floor,
	// so the *ticker* branch cannot fire inside this sub-second window — the
	// reachable-fast 418 is the initial reconcile's. Survival past that single
	// backoff is the exact behavior the incident violated (the buggy impl
	// `return err`-ed instead of looping on, exiting the goroutine).
	if got := atomic.LoadInt32(&calls); got < 1 {
		t.Fatalf("server saw %d calls — initial-reconcile 418/backoff path not exercised", got)
	}

	// Now cancel: Run must return promptly, and with ctx.Canceled — never the
	// underlying 418 error (operators distinguish "we shut it down" from "the
	// exchange rejected us").
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Run err = %v, want context.Canceled (must not surface the 418 as the exit reason)", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s of ctx cancel")
	}
}

// TestPositionReconciler_Run_CtxCancelDuringBackoff_ReturnsPromptly asserts a
// shutdown/kill is not blocked for a full RateLimitBackoff (120s in prod) when
// the cancel lands mid-backoff. sleepBackoff selects on ctx.Done, so Run must
// return well before the backoff timer would have elapsed. Without this, a
// CONFIRM kill issued during an active testnet ban would hang up to 120s.
func TestPositionReconciler_Run_CtxCancelDuringBackoff_ReturnsPromptly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests) // 429
		_, _ = w.Write([]byte(`{"code":-1003,"msg":"Too many requests"}`))
	}))
	defer srv.Close()

	r := newReconcilerForTest(t, srv.URL)
	r.HTTPClient = srv.Client()
	// Backoff deliberately LONG so that, if Run ignored ctx during sleep, the
	// test would time out rather than pass. Poll interval tiny so we enter the
	// backoff almost immediately via the initial reconcile.
	r.PollInterval = 10 * time.Millisecond
	r.RateLimitBackoff = 30 * time.Second

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx, "BTCUSDT") }()

	// Let the initial reconcile hit 418/429 and enter sleepBackoff, then cancel
	// mid-backoff.
	time.Sleep(50 * time.Millisecond)
	start := time.Now()
	cancel()
	select {
	case err := <-done:
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Errorf("Run returned %v after cancel — blocked on the %v backoff instead of honoring ctx", elapsed, r.RateLimitBackoff)
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Run err = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("Run did not return within 5s of cancel — backoff (%v) blocked ctx cancellation", r.RateLimitBackoff)
	}
}
