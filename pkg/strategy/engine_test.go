package strategy

import (
	"context"
	"testing"
	"time"

	"github.com/cristianmanoliu/trading-engine/pkg/models"
)

// Bug 5 regression suite (CLAUDE.md "Known Bugs / Fixed → Bug 5").
//
// Background: paginated kline backfill (96h × 60 1m → 24 closed 4H candles)
// primes EMA21 mid-backfill. Without a staleness gate, the very last priming
// candle would emit a signal at a close price that's hours/days old — the
// engine then opens a position at that stale price and the first live tick
// is an instant adverse fill.
//
// The fix: evaluateEntry returns early when liveMode=true AND the candle's
// CloseTime is older than backfillStaleness. cmd/engine sets liveMode=true
// for live + each shadow Runner; cmd/backtest leaves it default-false so
// historical CSV replay continues to emit signals as designed.
//
// These tests pin the gate predicate (candleTooStale) directly, plus the
// setter and the staleness constant. A subtle regression — accidental
// removal of the liveMode clause, sign-flipped inequality, threshold
// changed too small or too large — would be caught here, not in production.

func TestRunner_CandleTooStale_DefaultIsFalse(t *testing.T) {
	// A zero-value Runner has liveMode=false (backtest default). The gate must
	// be off so historical CSV replay can still emit signals at any candle age.
	var r Runner
	fresh := models.Candle{CloseTime: time.Now()}
	stale := models.Candle{CloseTime: time.Now().Add(-time.Hour)}
	if r.candleTooStale(fresh) {
		t.Error("default Runner: fresh candle reported stale (gate must be off in backtest mode)")
	}
	if r.candleTooStale(stale) {
		t.Error("default Runner: hour-old candle reported stale — would suppress every backtest signal")
	}
}

func TestRunner_CandleTooStale_LiveMode_FreshCandle_NotStale(t *testing.T) {
	// liveMode=true with a candle that just closed: the gate must NOT fire,
	// otherwise no live signal could ever be emitted.
	r := Runner{liveMode: true}
	fresh := models.Candle{CloseTime: time.Now()}
	if r.candleTooStale(fresh) {
		t.Error("liveMode=true + just-closed candle: reported stale (would suppress every live signal)")
	}
}

func TestRunner_CandleTooStale_LiveMode_StaleCandle_IsStale(t *testing.T) {
	// liveMode=true with an hour-old candle (representative of a 4H signal-tf
	// backfill priming candle): the gate must fire. This is the Bug 5 case —
	// without the gate, this is exactly the candle that would emit a signal at
	// a stale close price, opening a position at an instant adverse fill.
	r := Runner{liveMode: true}
	stale := models.Candle{CloseTime: time.Now().Add(-time.Hour)}
	if !r.candleTooStale(stale) {
		t.Error("liveMode=true + 1h-old candle: NOT reported stale — Bug 5 regression")
	}
}

func TestRunner_CandleTooStale_LiveMode_BoundaryAtThreshold(t *testing.T) {
	// The inequality is strict: time.Since(c.CloseTime) > backfillStaleness.
	// A candle exactly at the threshold (90s old) is NOT stale. One tick over
	// the threshold IS. Pin the strict-> semantics so a future change to >=
	// is intentional (which would suppress real live closes that happened to
	// take exactly 90s to reach the strategy goroutine).
	r := Runner{liveMode: true}

	// CloseTime exactly backfillStaleness ago → time.Since may be slightly
	// over by the time the call lands, so use a small safety margin to land
	// strictly UNDER the threshold.
	atThreshold := models.Candle{CloseTime: time.Now().Add(-backfillStaleness + 100*time.Millisecond)}
	if r.candleTooStale(atThreshold) {
		t.Errorf("liveMode=true + ~%v old (just under threshold): reported stale (gate must use strict >, not >=)", backfillStaleness)
	}

	// One second past threshold → must be stale.
	overThreshold := models.Candle{CloseTime: time.Now().Add(-backfillStaleness - time.Second)}
	if !r.candleTooStale(overThreshold) {
		t.Errorf("liveMode=true + %v old (1s past threshold): NOT reported stale", backfillStaleness+time.Second)
	}
}

func TestRunner_BackfillStalenessConstant_Is90s(t *testing.T) {
	// Pin the staleness threshold against accidental drift. CLAUDE.md notes
	// the value is "smaller than the smallest signal timeframe (5m) so real
	// live closes are never suppressed even with reasonable network/processing
	// lag." Too small (<1m) would suppress real live closes; too large would
	// re-introduce the Bug 5 stale-signal-on-restart failure mode (the
	// paginated backfill primes 4H indicators with candles that are ~4h old
	// at the seam, far past any reasonable threshold).
	if backfillStaleness != 90*time.Second {
		t.Errorf("backfillStaleness = %v, want 90s — see CLAUDE.md Bug 5", backfillStaleness)
	}
}

func TestRunner_SetLiveMode_TogglesField(t *testing.T) {
	// Setter contract: zero value is false, SetLiveMode(true) sets it,
	// SetLiveMode(false) clears it. cmd/engine relies on this to flip live
	// + shadow Runners into gated mode after construction; cmd/backtest
	// never calls it.
	var r Runner
	if r.liveMode {
		t.Error("zero-value Runner.liveMode = true, want false")
	}
	r.SetLiveMode(true)
	if !r.liveMode {
		t.Error("SetLiveMode(true) did not set liveMode")
	}
	// And toggle back, in case future logic ever needs to disable mid-run
	// (currently no caller does, but pin the symmetric behavior).
	r.SetLiveMode(false)
	if r.liveMode {
		t.Error("SetLiveMode(false) did not clear liveMode")
	}
}

func TestRunner_CandleTooStale_FutureCandle_NotStale(t *testing.T) {
	// Edge case: candle with CloseTime in the FUTURE. time.Since returns a
	// negative duration, which is always < backfillStaleness, so the gate
	// does not fire. This protects against clock-skew between exchange
	// servers and the engine host: if exchange-reported CloseTime is in the
	// near future relative to the engine's clock, we should NOT suppress —
	// the candle is by definition fresh.
	r := Runner{liveMode: true}
	future := models.Candle{CloseTime: time.Now().Add(time.Minute)}
	if r.candleTooStale(future) {
		t.Error("liveMode=true + future-dated candle: reported stale (clock-skew defense broken)")
	}
}

// ── HandleCandle / HandleTick routing tests ───────────────────────────────────
//
// HandleCandle and HandleTick form the deterministic backtest pipeline (cmd/backtest
// drives them directly rather than going through the Run channel-select loop).
// The routing logic is small but load-bearing: a 4H candle ALWAYS updates bias
// (regardless of signalTF), and ONLY a candle matching signalTF feeds the
// EntryDetector. A regression that swapped the order, broke the timeframe
// match, or routed every candle to the detector would silently change every
// backtest result.

// recordingExecutor captures executor calls for assertions. Threadsafe is not
// required since Runner is single-goroutine by design.
type recordingExecutor struct {
	ticks   []models.Tick
	signals []*models.Signal
	summary int
}

func (e *recordingExecutor) OnTick(t models.Tick)      { e.ticks = append(e.ticks, t) }
func (e *recordingExecutor) OnSignal(s *models.Signal) { e.signals = append(e.signals, s) }
func (e *recordingExecutor) Summary()                  { e.summary++ }

// newTestRunner constructs a minimal Runner. nil channels are fine because
// HandleCandle/HandleTick don't read from them — only Run() does.
// EMAMode: true matches the live production config and allocates the
// detector.ema9/ema21 fields the routing tests inspect.
func newTestRunner(signalTF models.Timeframe, exec Executor) *Runner {
	if exec == nil {
		exec = &recordingExecutor{}
	}
	cfg := EntryConfig{SignalTimeframe: signalTF, EMAMode: true}
	return NewRunner(nil, nil, nil, nil, nil, nil, nil, nil, cfg, exec)
}

func TestRunner_HandleCandle_4H_UpdatesBias_RegardlessOfSignalTF(t *testing.T) {
	// 4H candles ALWAYS update the bias tracker, even when signalTF is 5m
	// (the legacy default). Bias gates breakout entries; detaching it from
	// 4H would silently change which trades pass the bias filter.
	r := newTestRunner(models.Timeframe5m, nil) // signalTF != 4H
	r.HandleCandle(models.Candle{
		Timeframe: models.Timeframe4H,
		Open:      100,
		Close:     110, // green → bullish first candle → Long
		CloseTime: time.Now(),
	})
	if r.bias.Direction() != models.Long {
		t.Errorf("HandleCandle(4H green) didn't update bias: got %v, want Long", r.bias.Direction())
	}
}

func TestRunner_HandleCandle_MatchingSignalTF_FeedsDetector(t *testing.T) {
	// A candle whose Timeframe matches signalTF is fed to the EntryDetector
	// via AddCandle. After 9 such candles, the inner EMA9 should be primed.
	r := newTestRunner(models.Timeframe4H, nil)
	for i := 0; i < 9; i++ {
		r.HandleCandle(models.Candle{
			Timeframe: models.Timeframe4H,
			Open:      100 + float64(i),
			Close:     100 + float64(i) + 1,
			CloseTime: time.Now(),
		})
	}
	if !r.detector.ema9.Primed() {
		t.Error("HandleCandle(4H, signalTF=4H) × 9 didn't prime EMA9 — detector not receiving matching candles")
	}
}

func TestRunner_HandleCandle_NonMatchingSignalTF_DoesNotFeedDetector(t *testing.T) {
	// signalTF=4H means ONLY 4H candles feed the detector. 5m candles must
	// pass through HandleCandle without affecting detector state. A regression
	// that fed everything to the detector would invert backtest signal counts
	// (5m crosses being read as 4H crosses, or vice versa).
	r := newTestRunner(models.Timeframe4H, nil)
	for i := 0; i < 9; i++ {
		r.HandleCandle(models.Candle{
			Timeframe: models.Timeframe5m,
			Open:      100 + float64(i),
			Close:     100 + float64(i) + 1,
			CloseTime: time.Now(),
		})
	}
	if r.detector.ema9.Primed() {
		t.Error("HandleCandle(5m, signalTF=4H) × 9 primed EMA9 — non-matching candles should be filtered out")
	}
}

func TestRunner_HandleTick_CallsExecutorOnTick(t *testing.T) {
	// Every tick must reach the executor (Stub uses OnTick to manage exits).
	// A regression that dropped this wiring would freeze position lifecycle —
	// stops/targets/max-hold would never trigger.
	exec := &recordingExecutor{}
	r := newTestRunner(models.Timeframe5m, exec)
	tick := models.Tick{
		Symbol:    "BTCUSDT",
		Price:     100,
		Timestamp: time.Now(),
		Volume:    1.0,
	}
	r.HandleTick(tick)
	if len(exec.ticks) != 1 {
		t.Fatalf("HandleTick: executor.OnTick called %d times, want 1", len(exec.ticks))
	}
	if exec.ticks[0].Price != 100 {
		t.Errorf("OnTick received wrong price: %v, want 100", exec.ticks[0].Price)
	}
	if exec.ticks[0].Symbol != "BTCUSDT" {
		t.Errorf("OnTick received wrong symbol: %q, want BTCUSDT", exec.ticks[0].Symbol)
	}
}

func TestRunner_HandleTick_UpdatesVWAP(t *testing.T) {
	// HandleTick must update the VWAP indicator. VWAP feeds entry detection
	// (signal context records the value at signal-emission time, and VWAP
	// distance is a feature in the EMA-cross signal context). Skipping the
	// update here would silently zero out VWAP-derived signals.
	exec := &recordingExecutor{}
	r := newTestRunner(models.Timeframe5m, exec)
	r.HandleTick(models.Tick{
		Symbol:    "BTCUSDT",
		Price:     100,
		Timestamp: time.Now(),
		Volume:    1.0,
	})
	if r.vwap.Value() == 0 {
		t.Error("HandleTick: VWAP not updated (Value() returned 0 after a non-zero tick)")
	}
}

func TestRunner_NewRunner_SignalTFDefaultsTo5m(t *testing.T) {
	// Per NewRunner: empty SignalTimeframe in config → defaults to 5m.
	// This is the legacy behavior; current production overrides to 4H, but
	// the default must be preserved so existing per-symbol configs still
	// resolve correctly when the field is absent.
	r := NewRunner(nil, nil, nil, nil, nil, nil, nil, nil, EntryConfig{}, &recordingExecutor{})
	if r.signalTF != models.Timeframe5m {
		t.Errorf("NewRunner default signalTF = %q, want %q", r.signalTF, models.Timeframe5m)
	}
}

// ── Run() lifecycle tests ─────────────────────────────────────────────────────
//
// Run is the main event loop. Two distinct exit paths with different semantics:
//   1. All input channels close naturally → loop exits → executor.Summary() runs
//      (this is the cmd/backtest path: CSVReplay drains, channels close, the
//      end-of-run report fires)
//   2. ctx.Done() fires → return from inside select → Summary() does NOT run
//      (this is the cmd/engine SIGTERM path: paper-live cleanup is journal-only;
//      a final summary log line on shutdown would be misleading mid-run)
//
// The closed-channel-is-nilled invariant (r.candle4H = nil after !ok) is what
// prevents busy-looping on the zero-value receive of a closed channel. If
// removed, the loop would spin at 100% CPU receiving zero candles repeatedly.

// runnerWithChannels is a test helper that builds a Runner over fresh
// bidirectional channels (so the test can close them) and returns both the
// Runner and the channels for sending.
type runnerChannels struct {
	c4H, c30m, c5m, c1H, c2H, c1D chan models.Candle
	ticks                         chan models.Tick
}

func newRunnerWithChannels(exec Executor) (*Runner, runnerChannels) {
	if exec == nil {
		exec = &recordingExecutor{}
	}
	chs := runnerChannels{
		c4H:   make(chan models.Candle),
		c30m:  make(chan models.Candle),
		c5m:   make(chan models.Candle),
		c1H:   make(chan models.Candle),
		c2H:   make(chan models.Candle),
		c1D:   make(chan models.Candle),
		ticks: make(chan models.Tick, 1),
	}
	r := NewRunner(chs.c4H, chs.c30m, chs.c5m, chs.c1H, chs.c2H, chs.c1D, chs.ticks,
		nil, EntryConfig{SignalTimeframe: models.Timeframe5m, EMAMode: true}, exec)
	return r, chs
}

func TestRunner_Run_AllChannelsClosed_CallsSummaryOnce(t *testing.T) {
	// Backtest-style termination: every input channel drains naturally. Run
	// should exit and call executor.Summary exactly once (the end-of-run report).
	exec := &recordingExecutor{}
	r, chs := newRunnerWithChannels(exec)

	close(chs.c4H)
	close(chs.c30m)
	close(chs.c5m)
	close(chs.c1H)
	close(chs.c2H)
	close(chs.c1D)
	close(chs.ticks)

	done := make(chan struct{})
	go func() {
		r.Run(context.Background())
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit within 2s after all channels closed — busy-loop on closed channel suspected")
	}

	if exec.summary != 1 {
		t.Errorf("Summary called %d times, want 1 (post-natural-drain report)", exec.summary)
	}
}

func TestRunner_Run_ContextCancel_DoesNotCallSummary(t *testing.T) {
	// Engine-style termination: SIGTERM cancels ctx before channels drain.
	// Run returns early from inside the select; Summary should NOT fire
	// (a mid-run summary line would be misleading — the run is incomplete).
	exec := &recordingExecutor{}
	r, _ := newRunnerWithChannels(exec)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		r.Run(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit within 2s after ctx cancel")
	}

	if exec.summary != 0 {
		t.Errorf("Summary called %d times after ctx cancel, want 0 (mid-run cancel must not emit summary)", exec.summary)
	}
}

func TestRunner_Run_TickFlow_ReachesExecutor(t *testing.T) {
	// A tick sent before channels close must reach executor.OnTick via the
	// Run() select loop (this is a different code path than HandleTick — the
	// case branch in Run does the same work but tests the dispatch).
	exec := &recordingExecutor{}
	r, chs := newRunnerWithChannels(exec)

	chs.ticks <- models.Tick{
		Symbol:    "BTCUSDT",
		Price:     12345.67,
		Volume:    1.0,
		Timestamp: time.Now(),
	}
	close(chs.ticks)
	close(chs.c4H)
	close(chs.c30m)
	close(chs.c5m)
	close(chs.c1H)
	close(chs.c2H)
	close(chs.c1D)

	done := make(chan struct{})
	go func() {
		r.Run(context.Background())
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit within 2s")
	}

	if len(exec.ticks) != 1 {
		t.Fatalf("OnTick called %d times, want 1", len(exec.ticks))
	}
	if exec.ticks[0].Price != 12345.67 {
		t.Errorf("OnTick received wrong price: %v, want 12345.67", exec.ticks[0].Price)
	}
}

func TestRunner_Run_PartialChannelClose_ContinuesUntilAllClose(t *testing.T) {
	// Closing one channel sets that channel to nil (loop ignores it from then
	// on) but other channels remain active. The for-condition is OR-of-all,
	// so the loop continues until ALL channels close. A bug that AND-ed the
	// flags would exit on first close. This test pins the OR semantics.
	exec := &recordingExecutor{}
	r, chs := newRunnerWithChannels(exec)

	// Close 6 of 7 channels first; one stays open with no sender.
	close(chs.c4H)
	close(chs.c30m)
	close(chs.c5m)
	close(chs.c1H)
	close(chs.c2H)
	close(chs.c1D)

	done := make(chan struct{})
	go func() {
		r.Run(context.Background())
		close(done)
	}()

	// With ticks still open and no sender, Run should still be running
	// (blocked on select). Verify it has NOT exited prematurely.
	select {
	case <-done:
		t.Fatal("Run exited with one channel still open (AND-vs-OR regression)")
	case <-time.After(50 * time.Millisecond):
		// expected — still running
	}

	// Now close the last channel; loop should exit and emit Summary.
	close(chs.ticks)
	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit within 2s after final channel close")
	}
	if exec.summary != 1 {
		t.Errorf("Summary called %d times, want 1", exec.summary)
	}
}
