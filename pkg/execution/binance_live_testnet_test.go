package execution

// Layer 2 — Binance USDT-M Futures TESTNET integration tests.
//
// Per the locked rule
// (results/real_money_executor_architecture_decision_rule_2026-05-08.md
// "Layer 2 — Integration against Binance TESTNET"), these exercise the
// real wire format + signing + parsing against
// https://testnet.binancefuture.com so the unit tests' httptest mocks
// can't drift silently from the actual API.
//
// Two-stage opt-in gating:
//
//   1. Read-only tests  → gated on BINANCE_TESTNET_API_KEY +
//                          BINANCE_TESTNET_API_SECRET being set.
//   2. Destructive tests → ADDITIONALLY require
//                          BINANCE_TESTNET_RUN_DESTRUCTIVE=1.
//
// Without the env vars set the tests t.Skip cleanly so normal CI passes.
//
// Operator workflow:
//
//   # Read-only smoke (signed-request format works, account is reachable)
//   BINANCE_TESTNET_API_KEY=... BINANCE_TESTNET_API_SECRET=... \
//     go test ./pkg/execution -run Testnet_ReadOnly -v
//
//   # Full round-trip (opens + closes positions on testnet — consumes
//   # testnet balance; requires confirming the testnet account has
//   # enough balance + is in one-way mode)
//   BINANCE_TESTNET_API_KEY=... BINANCE_TESTNET_API_SECRET=... \
//   BINANCE_TESTNET_RUN_DESTRUCTIVE=1 \
//     go test ./pkg/execution -run Testnet -v -timeout 5m
//
// Pre-conditions for destructive tests (operator-set, not asserted):
//   - Testnet account is in ONE-WAY position mode (not hedge mode).
//   - Testnet account has ≥$100 USDT-M balance.
//   - No other client is actively trading testnetSymbol on this account
//     (concurrent activity will cause spurious drift in PositionReconciler
//     test).

import (
	"context"
	"math"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/cristianmanoliu/fin-trading-engine/pkg/models"
)

const (
	testnetAPIBase = "https://testnet.binancefuture.com"

	// testnetSymbol is the symbol used for destructive integration tests.
	// BTCUSDT is the canonical Binance Futures pair, has stable testnet
	// liquidity, and a low min-notional. Update if Binance retires it on
	// testnet (unlikely).
	testnetSymbol = "BTCUSDT"

	// testnetQty is the contracts size used for round-trip tests. 0.001 BTC
	// at ~$50k = ~$50 notional — comfortably over Binance Futures' min
	// notional ($5) but small enough that 100s of test runs don't deplete
	// a default testnet balance.
	testnetQty = 0.001

	// testnetHTTPTimeout matches the production HTTPClient timeout so we
	// catch network-config issues that would surface in real-money mode.
	testnetHTTPTimeout = 30 * time.Second
)

// skipIfNoTestnetCreds returns the testnet credentials or skips the test.
// Used by every test in this file — never invoke testnet endpoints without
// going through this gate.
func skipIfNoTestnetCreds(t *testing.T) (apiKey, apiSecret string) {
	t.Helper()
	apiKey = os.Getenv("BINANCE_TESTNET_API_KEY")
	apiSecret = os.Getenv("BINANCE_TESTNET_API_SECRET")
	if apiKey == "" || apiSecret == "" {
		t.Skip("Layer 2 testnet integration: set BINANCE_TESTNET_API_KEY + BINANCE_TESTNET_API_SECRET to run")
	}
	return
}

// skipIfNotDestructive blocks tests that would open positions / consume
// testnet balance unless the operator explicitly opts in.
func skipIfNotDestructive(t *testing.T) {
	t.Helper()
	if os.Getenv("BINANCE_TESTNET_RUN_DESTRUCTIVE") != "1" {
		t.Skip("Layer 2 destructive tests: set BINANCE_TESTNET_RUN_DESTRUCTIVE=1 to run (will open + close real testnet positions)")
	}
}

// testnetRouter constructs an OrderRouter wired to testnet credentials.
func testnetRouter(t *testing.T) *OrderRouter {
	t.Helper()
	apiKey, apiSecret := skipIfNoTestnetCreds(t)
	return &OrderRouter{
		APIBaseURL: testnetAPIBase,
		APIKey:     apiKey,
		APISecret:  apiSecret,
		HTTPClient: &http.Client{Timeout: testnetHTTPTimeout},
		RecvWindow: 5000,
	}
}

// testnetReconciler constructs a PositionReconciler wired to testnet
// credentials with locked production thresholds.
func testnetReconciler(t *testing.T) *PositionReconciler {
	t.Helper()
	apiKey, apiSecret := skipIfNoTestnetCreds(t)
	return &PositionReconciler{
		APIBaseURL:         testnetAPIBase,
		APIKey:             apiKey,
		APISecret:          apiSecret,
		HTTPClient:         &http.Client{Timeout: testnetHTTPTimeout},
		RecvWindow:         5000,
		QtyTolerance:       0.01,
		EntryPriceBpsLimit: 10,
	}
}

// ── Read-only tests ─────────────────────────────────────────────────────────
//
// These don't open positions. They prove the signed-request format is
// correct + the account is reachable.

func TestTestnet_ReadOnly_PositionReconciler_FetchExchangePosition(t *testing.T) {
	r := testnetReconciler(t)
	ctx, cancel := context.WithTimeout(context.Background(), testnetHTTPTimeout)
	defer cancel()

	pos, err := r.FetchExchangePosition(ctx, testnetSymbol)
	if err != nil {
		t.Fatalf("FetchExchangePosition: %v (signature wrong? account inaccessible?)", err)
	}
	// Side must be a valid Direction. Don't assert on Qty value — depends on
	// account state which is operator-controlled.
	switch pos.Side {
	case models.Long, models.Short, models.Neutral:
	default:
		t.Errorf("invalid Side: %v", pos.Side)
	}
	t.Logf("testnet position: side=%v qty=%v entry=%v", pos.Side, pos.Qty, pos.AvgEntry)
}

// ── Destructive tests ───────────────────────────────────────────────────────
//
// These open + close real testnet positions. Each test MUST install a
// t.Cleanup that closes any leaked position so a panic mid-test doesn't
// leave the account dirty for the next run.

func TestTestnet_OrderRouter_SendOrder_MarketOpenAndClose_RoundTrip(t *testing.T) {
	skipIfNotDestructive(t)
	router := testnetRouter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// Defensive cleanup: try a reduceOnly close on test exit. Idempotent —
	// closing an already-flat position returns 4xx which is fine on cleanup.
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_, _ = router.SendOrder(cleanupCtx, OrderIntent{
			Symbol: testnetSymbol, Side: models.Short, Quantity: testnetQty,
			Type: "MARKET", ReduceOnly: true,
		})
	})

	// Open a SHORT (entry side = SELL on the exchange).
	openResult, err := router.SendOrder(ctx, OrderIntent{
		Symbol:   testnetSymbol,
		Side:     models.Short,
		Quantity: testnetQty,
		Type:     "MARKET",
	})
	if err != nil {
		t.Fatalf("open order failed: %v (status=%q reject=%q)",
			err, openResult.Status, openResult.RejectCode)
	}
	if openResult.Status != "FILLED" && openResult.Status != "PARTIAL" {
		t.Errorf("open status = %q, want FILLED or PARTIAL", openResult.Status)
	}
	if openResult.FilledQty <= 0 {
		t.Fatalf("open FilledQty = %v, want > 0", openResult.FilledQty)
	}
	if openResult.AvgPrice <= 0 {
		t.Errorf("open AvgPrice = %v, want > 0", openResult.AvgPrice)
	}
	t.Logf("open: orderID=%s filled=%v avg=%v", openResult.OrderID, openResult.FilledQty, openResult.AvgPrice)

	// Close via reduceOnly (close side = BUY for SHORT — mapToExchangeSide
	// flips). reduceOnly guarantees we won't accidentally open a counter
	// position if the size mismatches.
	closeResult, err := router.SendOrder(ctx, OrderIntent{
		Symbol:     testnetSymbol,
		Side:       models.Short,
		Quantity:   openResult.FilledQty,
		Type:       "MARKET",
		ReduceOnly: true,
	})
	if err != nil {
		t.Fatalf("close order failed: %v", err)
	}
	if closeResult.Status != "FILLED" && closeResult.Status != "PARTIAL" {
		t.Errorf("close status = %q, want FILLED or PARTIAL", closeResult.Status)
	}
	if closeResult.FilledQty < openResult.FilledQty-0.0001 {
		t.Errorf("close FilledQty %v < open FilledQty %v — partial close left residual",
			closeResult.FilledQty, openResult.FilledQty)
	}

	// Round-trip P&L sanity check. SHORT: pnl points = entry - exit.
	pnlPts := openResult.AvgPrice - closeResult.AvgPrice
	t.Logf("round-trip pnl = %v (entry=%v exit=%v)", pnlPts, openResult.AvgPrice, closeResult.AvgPrice)

	// BTC-USDT shouldn't move >5% in <90s under any normal condition. If it
	// did, log a warning — not a test failure (the round-trip mechanics still
	// work) but worth visibility on an unusual run.
	if openResult.AvgPrice > 0 {
		movePct := math.Abs(pnlPts) / openResult.AvgPrice * 100
		if movePct > 5 {
			t.Logf("WARNING: round-trip price moved %.2f%% in <90s — testnet liquidity unusual today", movePct)
		}
	}
}

func TestTestnet_PositionReconciler_DetectsLiveOpenPosition(t *testing.T) {
	// Opens a real testnet position via the same router used by BinanceLive,
	// then queries via PositionReconciler.FetchExchangePosition and verifies
	// the round-trip view matches. Catches signed-request + JSON-parsing
	// drift between order responses and positionRisk responses.
	skipIfNotDestructive(t)
	router := testnetRouter(t)
	reconciler := testnetReconciler(t)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// Defensive cleanup: close any opened position on test exit.
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_, _ = router.SendOrder(cleanupCtx, OrderIntent{
			Symbol: testnetSymbol, Side: models.Short, Quantity: testnetQty,
			Type: "MARKET", ReduceOnly: true,
		})
	})

	openResult, err := router.SendOrder(ctx, OrderIntent{
		Symbol:   testnetSymbol,
		Side:     models.Short,
		Quantity: testnetQty,
		Type:     "MARKET",
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	// Brief delay to let the exchange propagate state across the order endpoint
	// and the positionRisk endpoint. 1s is generous; in practice <100ms.
	time.Sleep(1 * time.Second)

	pos, err := reconciler.FetchExchangePosition(ctx, testnetSymbol)
	if err != nil {
		t.Fatalf("FetchExchangePosition: %v", err)
	}
	if pos.Side != models.Short {
		t.Errorf("Side = %v, want SHORT (exchange should reflect the opened position)", pos.Side)
	}
	if math.Abs(math.Abs(pos.Qty)-openResult.FilledQty) > 0.0001 {
		t.Errorf("Qty mismatch: reconciler |%v| vs router %v", pos.Qty, openResult.FilledQty)
	}
	if math.Abs(pos.AvgEntry-openResult.AvgPrice)/openResult.AvgPrice > 0.001 {
		// Allow up to 0.1% drift between order-fill avg and position avg —
		// rounding differences. Anything bigger suggests a parse bug.
		t.Errorf("AvgEntry drift > 0.1%%: reconciler=%v router=%v", pos.AvgEntry, openResult.AvgPrice)
	}
}

func TestTestnet_KillSwitch_KillAll_SinglePosition(t *testing.T) {
	// Verifies KillSwitch.KillAll routes through OrderRouter against testnet
	// without compounding errors when reduceOnly hits an existing position.
	skipIfNotDestructive(t)
	router := testnetRouter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_, _ = router.SendOrder(cleanupCtx, OrderIntent{
			Symbol: testnetSymbol, Side: models.Short, Quantity: testnetQty,
			Type: "MARKET", ReduceOnly: true,
		})
	})

	// Open a SHORT to give the kill switch something to close.
	openResult, err := router.SendOrder(ctx, OrderIntent{
		Symbol: testnetSymbol, Side: models.Short, Quantity: testnetQty, Type: "MARKET",
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	// Fire kill switch.
	ks := &KillSwitch{Router: router}
	result, err := ks.KillAll(ctx,
		[]ClosePosition{{
			Symbol: testnetSymbol, Side: models.Short, Quantity: openResult.FilledQty,
		}},
		"layer2_test")
	if err != nil {
		t.Fatalf("KillAll: %v", err)
	}
	if len(result.Outcomes) != 1 {
		t.Fatalf("outcomes = %d, want 1", len(result.Outcomes))
	}
	o := result.Outcomes[0]
	if o.Status != "CLOSED" && o.Status != "PARTIAL" {
		t.Errorf("Status = %q, want CLOSED or PARTIAL", o.Status)
	}
	if o.Filled <= 0 {
		t.Errorf("Filled = %v, want > 0", o.Filled)
	}
	t.Logf("kill outcome: status=%q filled=%v avg=%v", o.Status, o.Filled, o.AvgPrice)
}
