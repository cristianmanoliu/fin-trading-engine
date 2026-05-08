package execution

import (
	"context"
	"errors"
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

func TestOrderRouter_SendOrder_ErrorsOnSkeleton(t *testing.T) {
	r := &OrderRouter{APIBaseURL: "https://fapi.binance.com"}
	_, err := r.SendOrder(context.Background(), OrderIntent{
		Symbol: "BTCUSDT", Side: models.Short, Quantity: 1, Type: "MARKET",
	})
	if !errors.Is(err, ErrStageNotPromoted) {
		t.Errorf("SendOrder err = %v, want ErrStageNotPromoted", err)
	}
}

func TestPositionReconciler_Run_ErrorsOnSkeleton(t *testing.T) {
	r := &PositionReconciler{}
	err := r.Run(context.Background(), "BTCUSDT")
	if !errors.Is(err, ErrStageNotPromoted) {
		t.Errorf("Run err = %v, want ErrStageNotPromoted", err)
	}
}

func TestSafetyGates_CheckOrder_ErrorsOnSkeleton(t *testing.T) {
	g := &SafetyGates{MaxPositionMultiple: 1, DailyLossUSDCap: 1000, MaxEntrySpreadBps: 50}
	err := g.CheckOrder(OrderIntent{}, 0, 0, 0)
	if !errors.Is(err, ErrStageNotPromoted) {
		t.Errorf("CheckOrder err = %v, want ErrStageNotPromoted", err)
	}
}

func TestKillSwitch_KillAll_ErrorsOnSkeleton(t *testing.T) {
	k := &KillSwitch{}
	err := k.KillAll(context.Background(), "manual test")
	if !errors.Is(err, ErrStageNotPromoted) {
		t.Errorf("KillAll err = %v, want ErrStageNotPromoted", err)
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
