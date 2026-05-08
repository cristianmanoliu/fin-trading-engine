package execution

import (
	"testing"

	"github.com/cristianmanoliu/trading-engine/pkg/models"
)

// mockTeeExec records every call for assertion. Captures both signals
// and ticks to verify TeeExecutor fans out to both Primary and Shadow.
type mockTeeExec struct {
	signals  []*models.Signal
	ticks    []models.Tick
	summary  int
	panicMsg string // when set, panic on any method call
}

func (m *mockTeeExec) OnSignal(s *models.Signal) {
	if m.panicMsg != "" {
		panic(m.panicMsg)
	}
	m.signals = append(m.signals, s)
}
func (m *mockTeeExec) OnTick(t models.Tick) {
	if m.panicMsg != "" {
		panic(m.panicMsg)
	}
	m.ticks = append(m.ticks, t)
}
func (m *mockTeeExec) Summary() {
	if m.panicMsg != "" {
		panic(m.panicMsg)
	}
	m.summary++
}

func TestTeeExecutor_OnSignal_FansToBoth(t *testing.T) {
	primary := &mockTeeExec{}
	shadow := &mockTeeExec{}
	tee := &TeeExecutor{Primary: primary, Shadow: shadow}

	sig := &models.Signal{Side: models.Long, EntryPrice: 100, StopLoss: 95, TakeProfit: 130}
	tee.OnSignal(sig)

	if len(primary.signals) != 1 || primary.signals[0] != sig {
		t.Errorf("primary did not receive signal: %+v", primary.signals)
	}
	if len(shadow.signals) != 1 || shadow.signals[0] != sig {
		t.Errorf("shadow did not receive signal: %+v", shadow.signals)
	}
}

func TestTeeExecutor_OnTick_FansToBoth(t *testing.T) {
	primary := &mockTeeExec{}
	shadow := &mockTeeExec{}
	tee := &TeeExecutor{Primary: primary, Shadow: shadow}

	tick := models.Tick{Price: 101, Volume: 0.5}
	tee.OnTick(tick)

	if len(primary.ticks) != 1 || primary.ticks[0] != tick {
		t.Errorf("primary did not receive tick: %+v", primary.ticks)
	}
	if len(shadow.ticks) != 1 || shadow.ticks[0] != tick {
		t.Errorf("shadow did not receive tick: %+v", shadow.ticks)
	}
}

func TestTeeExecutor_Summary_FansToBoth(t *testing.T) {
	primary := &mockTeeExec{}
	shadow := &mockTeeExec{}
	tee := &TeeExecutor{Primary: primary, Shadow: shadow}
	tee.Summary()
	if primary.summary != 1 || shadow.summary != 1 {
		t.Errorf("Summary not fanned out: primary=%d shadow=%d", primary.summary, shadow.summary)
	}
}

func TestTeeExecutor_SequentialOrder_PrimaryBeforeShadow(t *testing.T) {
	// Sequential semantics: if Primary panics, Shadow MUST NOT have run
	// (sequential fan-out aborts on first panic). This is the design
	// contract — Primary's state is unchanged on panic in Shadow, but
	// Primary side-effects MUST complete before Shadow begins.
	primary := &mockTeeExec{}
	shadow := &mockTeeExec{panicMsg: "shadow boom"}
	tee := &TeeExecutor{Primary: primary, Shadow: shadow}

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected shadow panic to propagate, got none")
		}
		// Primary completed its OnSignal (synchronous side effect) before
		// shadow ran and panicked.
		if len(primary.signals) != 1 {
			t.Errorf("primary should have received signal before shadow panic: %+v", primary.signals)
		}
	}()

	tee.OnSignal(&models.Signal{Side: models.Long, EntryPrice: 100})
}

func TestTeeExecutor_PrimaryPanic_ShadowDoesNotRun(t *testing.T) {
	// Conversely: if Primary panics, Shadow MUST NOT run. Sequential
	// semantics — failure short-circuits.
	primary := &mockTeeExec{panicMsg: "primary boom"}
	shadow := &mockTeeExec{}
	tee := &TeeExecutor{Primary: primary, Shadow: shadow}

	defer func() {
		_ = recover()
		if len(shadow.signals) != 0 {
			t.Errorf("shadow should not have run after primary panic: %+v", shadow.signals)
		}
	}()
	tee.OnSignal(&models.Signal{Side: models.Long})
}
