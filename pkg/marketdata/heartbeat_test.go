package marketdata

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cristianmanoliu/fin-trading-engine/pkg/models"
	"github.com/cristianmanoliu/fin-trading-engine/pkg/notify"
)

// Heartbeat is the operational liveness signal that scripts/post_deploy_check.sh
// parses for tick-freshness audits. The Warn-level escalation at the staleness
// threshold IS the contract — log-based alerting depends on it firing exactly
// when a feed has actually stalled, and not firing under normal lag.
//
// Tests cover:
//   - NewHeartbeat zero state (no tick, count=0)
//   - Observe stores tick + atomically increments count
//   - snapshotForLog branches: no-ticks → Warn, fresh → Info, stale → Warn
//   - boundary at heartbeatStaleThreshold uses strict > (not >=)
//   - heartbeatStaleThreshold = 90s pinned against accidental drift
//   - Run() honors context cancellation cleanly

func TestHeartbeat_NewIsZeroState(t *testing.T) {
	h := NewHeartbeat("BTCUSDT")
	if h == nil {
		t.Fatal("NewHeartbeat returned nil")
	}
	if h.lastTick.Load() != nil {
		t.Error("new heartbeat: lastTick should be nil before any Observe")
	}
	if h.tickCount.Load() != 0 {
		t.Errorf("new heartbeat: tickCount = %d, want 0", h.tickCount.Load())
	}
	if h.symbol != "BTCUSDT" {
		t.Errorf("symbol = %q, want BTCUSDT", h.symbol)
	}
}

func TestHeartbeat_Observe_StoresTickAndIncrementsCount(t *testing.T) {
	h := NewHeartbeat("BTCUSDT")
	ts := time.Date(2026, 5, 8, 10, 0, 0, 0, time.UTC)
	h.Observe(models.Tick{Symbol: "BTCUSDT", Timestamp: ts, Price: 100, Volume: 1.0})

	last := h.lastTick.Load()
	if last == nil {
		t.Fatal("Observe didn't store tick timestamp")
	}
	if !last.Equal(ts) {
		t.Errorf("stored timestamp = %v, want %v", *last, ts)
	}
	if h.tickCount.Load() != 1 {
		t.Errorf("tickCount = %d, want 1 after one Observe", h.tickCount.Load())
	}
}

func TestHeartbeat_Observe_MultipleAccumulates(t *testing.T) {
	// 5 sequential observes should land tickCount at exactly 5 and lastTick
	// at the LATEST timestamp (each Store overwrites the previous).
	h := NewHeartbeat("BTCUSDT")
	var lastTS time.Time
	for i := 0; i < 5; i++ {
		ts := time.Now().Add(time.Duration(i) * time.Millisecond)
		h.Observe(models.Tick{Timestamp: ts})
		lastTS = ts
	}
	if h.tickCount.Load() != 5 {
		t.Errorf("after 5 observes: tickCount = %d, want 5", h.tickCount.Load())
	}
	last := h.lastTick.Load()
	if last == nil || !last.Equal(lastTS) {
		t.Errorf("lastTick = %v, want %v (latest)", last, lastTS)
	}
}

func TestHeartbeat_SnapshotForLog_NoTicks_Warn(t *testing.T) {
	h := NewHeartbeat("BTCUSDT")
	level, msg, args, count := h.snapshotForLog(0)
	if level != slog.LevelWarn {
		t.Errorf("no-ticks state: level = %v, want Warn", level)
	}
	if msg != "heartbeat: no ticks received yet" {
		t.Errorf("no-ticks msg = %q", msg)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
	// args should at least include the symbol.
	foundSymbol := false
	for i := 0; i+1 < len(args); i += 2 {
		if k, ok := args[i].(string); ok && k == "symbol" {
			if v, ok := args[i+1].(string); ok && v == "BTCUSDT" {
				foundSymbol = true
				break
			}
		}
	}
	if !foundSymbol {
		t.Errorf("args missing symbol=BTCUSDT: %v", args)
	}
}

func TestHeartbeat_SnapshotForLog_FreshTicks_Info(t *testing.T) {
	// Observe a recent tick → stale threshold not reached → Info level.
	h := NewHeartbeat("BTCUSDT")
	h.Observe(models.Tick{Timestamp: time.Now()})

	level, msg, args, count := h.snapshotForLog(0)
	if level != slog.LevelInfo {
		t.Errorf("fresh-tick state: level = %v, want Info", level)
	}
	if msg != "heartbeat" {
		t.Errorf("fresh msg = %q, want \"heartbeat\"", msg)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
	// args must include ticks_since_last delta and last_tick_age.
	hasDelta := false
	hasAge := false
	for i := 0; i+1 < len(args); i += 2 {
		if k, _ := args[i].(string); k == "ticks_since_last" {
			if d, ok := args[i+1].(int64); ok && d == 1 {
				hasDelta = true
			}
		}
		if k, _ := args[i].(string); k == "last_tick_age" {
			if _, ok := args[i+1].(time.Duration); ok {
				hasAge = true
			}
		}
	}
	if !hasDelta {
		t.Errorf("args missing ticks_since_last=1: %v", args)
	}
	if !hasAge {
		t.Errorf("args missing last_tick_age duration: %v", args)
	}
}

func TestHeartbeat_SnapshotForLog_StaleTicks_Warn(t *testing.T) {
	// Tick from before the stale threshold → escalates to Warn.
	h := NewHeartbeat("BTCUSDT")
	staleTS := time.Now().Add(-2 * heartbeatStaleThreshold)
	h.Observe(models.Tick{Timestamp: staleTS})

	level, msg, _, _ := h.snapshotForLog(0)
	if level != slog.LevelWarn {
		t.Errorf("stale state: level = %v, want Warn", level)
	}
	if msg != "heartbeat: feed appears stalled" {
		t.Errorf("stale msg = %q", msg)
	}
}

func TestHeartbeat_SnapshotForLog_BoundaryStrictGreater(t *testing.T) {
	// Inequality is strict: age > heartbeatStaleThreshold escalates to Warn.
	// A tick exactly at the threshold should stay Info, not flip to Warn —
	// otherwise normal-lag closes would be alerted on.
	//
	// time.Since rounds the duration to seconds in the helper, but the
	// comparison uses the raw duration. We use a margin of 100ms inside the
	// threshold to land safely under it (accounting for test clock drift).
	h := NewHeartbeat("BTCUSDT")
	h.Observe(models.Tick{Timestamp: time.Now().Add(-heartbeatStaleThreshold + 100*time.Millisecond)})

	level, _, _, _ := h.snapshotForLog(0)
	if level != slog.LevelInfo {
		t.Errorf("at-threshold (~%v old): level = %v, want Info (gate uses strict >, not >=)", heartbeatStaleThreshold, level)
	}

	// And just past the threshold: must be Warn.
	h2 := NewHeartbeat("BTCUSDT")
	h2.Observe(models.Tick{Timestamp: time.Now().Add(-heartbeatStaleThreshold - time.Second)})
	level2, _, _, _ := h2.snapshotForLog(0)
	if level2 != slog.LevelWarn {
		t.Errorf("just past threshold: level = %v, want Warn", level2)
	}
}

func TestHeartbeat_StaleThresholdConstant_Is180s(t *testing.T) {
	// Pin the constant against drift. scripts/post_deploy_check.sh uses
	// the Warn line to identify stalled feeds; if this threshold changes
	// without updating the audit logic, the check would either over-alert
	// (threshold lowered) or under-alert (threshold raised).
	//
	// 180s aligns with the WS→REST fallback boundary (wsReadDeadline ×
	// wsMaxStalls = 90s × 2) so WARN fires only after the engine's own
	// recovery window has elapsed — eliminating the alert-fatigue pattern
	// observed on thin-liquidity altcoins (KAVAUSDT/IMXUSDT) where natural
	// inter-trade gaps routinely cross 90s in quiet sessions.
	if heartbeatStaleThreshold != 180*time.Second {
		t.Errorf("heartbeatStaleThreshold = %v, want 180s", heartbeatStaleThreshold)
	}
}

func TestHeartbeat_SnapshotForLog_DeltaAdvances(t *testing.T) {
	// snapshotForLog returns the new count so the caller can update
	// prevCount. After one Observe and one snapshot at prev=0, count=1;
	// after another Observe and snapshot at prev=1, delta=1.
	h := NewHeartbeat("BTCUSDT")
	h.Observe(models.Tick{Timestamp: time.Now()})

	_, _, _, c1 := h.snapshotForLog(0)
	if c1 != 1 {
		t.Errorf("after 1 observe: count = %d, want 1", c1)
	}

	h.Observe(models.Tick{Timestamp: time.Now()})
	_, _, args, c2 := h.snapshotForLog(c1)
	if c2 != 2 {
		t.Errorf("after 2 observes: count = %d, want 2", c2)
	}
	// delta should be 1 (2 - prev=1).
	for i := 0; i+1 < len(args); i += 2 {
		if k, _ := args[i].(string); k == "ticks_since_last" {
			if d, ok := args[i+1].(int64); ok {
				if d != 1 {
					t.Errorf("delta after second snapshot = %d, want 1", d)
				}
				return
			}
		}
	}
	t.Error("ticks_since_last arg missing on second snapshot")
}

func TestHeartbeat_Run_ContextCancel_ExitsCleanly(t *testing.T) {
	// Run should exit promptly when ctx is cancelled. A bug that misses
	// ctx.Done() (e.g. only checking ticker.C) would leak the goroutine.
	h := NewHeartbeat("BTCUSDT")
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		h.Run(ctx, 50*time.Millisecond)
		close(done)
	}()

	cancel()
	select {
	case <-done:
		// success — goroutine exited
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Run did not exit within 200ms after ctx cancel — goroutine leak suspected")
	}
}

// ── Telegram alert wiring tests ──────────────────────────────────────────────

func TestFormatHeartbeatBody_SkipsSymbolKey(t *testing.T) {
	// formatHeartbeatBody renders args as multi-line key:value but skips
	// "symbol" since the symbol is already on its own line.
	got := formatHeartbeatBody(
		"heartbeat: feed appears stalled",
		"BTCUSDT",
		[]any{
			"symbol", "BTCUSDT", // should be skipped
			"ticks_since_last", int64(0),
			"last_tick_age", 120 * time.Second,
		},
	)
	want := "heartbeat: feed appears stalled\nsymbol: BTCUSDT\nticks_since_last: 0\nlast_tick_age: 2m0s"
	if got != want {
		t.Errorf("formatHeartbeatBody mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestFormatHeartbeatBody_HandlesOddArgsLength(t *testing.T) {
	// Defensive: if args has an odd length (key without value at the end),
	// the formatter must not panic.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("formatHeartbeatBody panicked on odd args: %v", r)
		}
	}()
	_ = formatHeartbeatBody("msg", "BTC", []any{"orphan_key"})
}

func TestHeartbeat_Run_NoNotifier_NoPanicOnWarn(t *testing.T) {
	// Heartbeat with Notifier=nil and no Observe calls hits the Warn path
	// in snapshotForLog ("no ticks received yet"). Must not panic.
	h := NewHeartbeat("BTCUSDT") // Notifier defaults nil
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.Run(ctx, 10*time.Millisecond)
	}()

	// Wait long enough for at least one ticker fire.
	time.Sleep(30 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// success
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Run hung after panic with nil Notifier (regression of nil-safety guard)")
	}
}

func TestHeartbeat_Run_WithNotifier_FiresWarnAlert(t *testing.T) {
	// With Notifier set + Warn-level snapshot (no ticks observed), Run
	// should fire a SendStructured alert. Verified by intercepting the
	// HTTP POST to a test server.
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var msg map[string]string
		_ = json.Unmarshal(b, &msg)
		mu.Lock()
		bodies = append(bodies, msg["text"])
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	// Build a Notifier that posts to the test server. Use sendTo via
	// the public Send wrapper would point at telegram.org; instead we
	// construct manually with the test URL embedded via the client's
	// Transport — but the simplest path is to use the existing escape
	// hatch: a Notifier whose HTTPClient redirects all requests through
	// a custom RoundTripper that rewrites the URL to the test server.
	n := &notify.Notifier{
		BotToken: "tok",
		ChatID:   "chat",
		HTTPClient: &http.Client{
			Timeout: 5 * time.Second,
			Transport: rewriteTransport{base: http.DefaultTransport, target: srv.URL},
		},
	}

	h := NewHeartbeat("BTCUSDT")
	h.Notifier = n
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.Run(ctx, 20*time.Millisecond)
	}()

	// Wait for at least one ticker fire.
	time.Sleep(80 * time.Millisecond)
	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) == 0 {
		t.Fatal("expected ≥1 Telegram alert from Warn-level heartbeat, got 0")
	}
	if !strings.Contains(bodies[0], "no ticks received yet") {
		t.Errorf("first alert missing expected msg: %q", bodies[0])
	}
	if !strings.Contains(bodies[0], "BTCUSDT") {
		t.Errorf("first alert missing symbol: %q", bodies[0])
	}
	// Severity prefix from SendStructured.
	if !strings.HasPrefix(bodies[0], "⚠") {
		t.Errorf("first alert missing ⚠ severity prefix: %q", bodies[0])
	}
}

// ── StartupGrace tests ───────────────────────────────────────────────────────
//
// The grace window suppresses Warn outcomes for the first StartupGrace
// duration after Run() begins (or after startupAt is set in tests). It exists
// because the Binance source can take ~3 minutes to deliver its first live
// tick after restart (backfill → WS read deadline × wsMaxStalls → REST
// fallback). Without grace, every redeploy floods Telegram with stale-feed
// WARN alerts that auto-clear within 3 minutes — pure noise.
//
// The contract is: during grace, Warn outcomes (no-ticks-yet, stale) become
// Info "warming up" lines. After grace, the original level rules apply.

func TestHeartbeat_StartupGrace_NoTicks_DowngradedToInfo(t *testing.T) {
	// During grace, snapshotForLog with no observed ticks must return Info
	// "warming up", not the legacy Warn "no ticks received yet".
	h := NewHeartbeat("BTCUSDT")
	h.StartupGrace = 3 * time.Minute
	h.startupAt = time.Now() // just started
	level, msg, _, _ := h.snapshotForLog(0)
	if level != slog.LevelInfo {
		t.Errorf("in-grace + no-ticks: level = %v, want Info (Warn alerts must be suppressed)", level)
	}
	if !strings.Contains(msg, "warming up") {
		t.Errorf("in-grace msg = %q, want contains 'warming up'", msg)
	}
}

func TestHeartbeat_StartupGrace_StaleTicks_DowngradedToInfo(t *testing.T) {
	// During grace, a stale tick (legitimate stale by the 90s threshold)
	// must also be suppressed — this is the actual XLMUSDT/etc post-restart
	// failure mode the grace is designed for.
	h := NewHeartbeat("BTCUSDT")
	h.StartupGrace = 3 * time.Minute
	h.startupAt = time.Now()
	staleTS := time.Now().Add(-2 * heartbeatStaleThreshold)
	h.Observe(models.Tick{Timestamp: staleTS})

	level, msg, _, _ := h.snapshotForLog(0)
	if level != slog.LevelInfo {
		t.Errorf("in-grace + stale-tick: level = %v, want Info (Warn alerts must be suppressed)", level)
	}
	if !strings.Contains(msg, "warming up") {
		t.Errorf("in-grace stale msg = %q, want contains 'warming up'", msg)
	}
}

func TestHeartbeat_StartupGrace_FreshTicks_StillInfo(t *testing.T) {
	// In grace + fresh tick: same as legacy fresh path — Info "heartbeat",
	// no warming-up phrasing. The grace only downgrades Warn outcomes; it
	// must not perturb the normal fresh-tick log line shape that
	// post_deploy_check.sh's section 4 parses.
	h := NewHeartbeat("BTCUSDT")
	h.StartupGrace = 3 * time.Minute
	h.startupAt = time.Now()
	h.Observe(models.Tick{Timestamp: time.Now()})

	level, msg, _, _ := h.snapshotForLog(0)
	if level != slog.LevelInfo {
		t.Errorf("in-grace + fresh-tick: level = %v, want Info", level)
	}
	if msg != "heartbeat" {
		t.Errorf("in-grace fresh msg = %q, want bare \"heartbeat\" (post_deploy_check.sh greps this exact string)", msg)
	}
}

func TestHeartbeat_StartupGrace_Expired_StaleTicks_BecomeWarn(t *testing.T) {
	// After grace expires, stale ticks must escalate to Warn as the legacy
	// code did. This is the safety property: the grace must not permanently
	// suppress real stalled-feed alerts.
	h := NewHeartbeat("BTCUSDT")
	h.StartupGrace = 100 * time.Millisecond
	h.startupAt = time.Now().Add(-200 * time.Millisecond) // grace already expired
	staleTS := time.Now().Add(-2 * heartbeatStaleThreshold)
	h.Observe(models.Tick{Timestamp: staleTS})

	level, msg, _, _ := h.snapshotForLog(0)
	if level != slog.LevelWarn {
		t.Errorf("post-grace + stale-tick: level = %v, want Warn (grace must not permanently mask stale-feed)", level)
	}
	if msg != "heartbeat: feed appears stalled" {
		t.Errorf("post-grace stale msg = %q, want \"heartbeat: feed appears stalled\"", msg)
	}
}

func TestHeartbeat_StartupGrace_Disabled_ZeroValue_LegacyBehavior(t *testing.T) {
	// StartupGrace=0 (zero value) preserves legacy alert-immediately
	// behavior. This guards every existing test + any caller that hasn't
	// opted in to the grace window — they must observe identical behavior.
	h := NewHeartbeat("BTCUSDT")
	// StartupGrace left at zero
	h.startupAt = time.Now() // should not matter

	level, _, _, _ := h.snapshotForLog(0)
	if level != slog.LevelWarn {
		t.Errorf("StartupGrace=0 + no-ticks: level = %v, want Warn (legacy behavior)", level)
	}
}

func TestHeartbeat_StartupGrace_DefaultConstant_Is3Min(t *testing.T) {
	// DefaultStartupGrace must equal the WS→REST fallback boundary
	// (wsReadDeadline * wsMaxStalls = 90s * 2 = 180s = 3min). Pinning this
	// here so changing the binance.go fallback parameters cannot silently
	// re-enable the post-restart noise floor.
	if DefaultStartupGrace != 3*time.Minute {
		t.Errorf("DefaultStartupGrace = %v, want 3m (= wsReadDeadline × wsMaxStalls)", DefaultStartupGrace)
	}
}

func TestHeartbeat_Run_InGrace_NoTelegramAlertOnStaleStartup(t *testing.T) {
	// End-to-end: Run() with StartupGrace set should NOT fire any Telegram
	// alert during the grace window even when there are no observed ticks.
	// This is the operationally observable change — the failure mode that
	// produced 16+ stale-feed alerts on every redeploy.
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var msg map[string]string
		_ = json.Unmarshal(b, &msg)
		mu.Lock()
		bodies = append(bodies, msg["text"])
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	n := &notify.Notifier{
		BotToken: "tok",
		ChatID:   "chat",
		HTTPClient: &http.Client{
			Timeout:   5 * time.Second,
			Transport: rewriteTransport{base: http.DefaultTransport, target: srv.URL},
		},
	}
	h := NewHeartbeat("BTCUSDT")
	h.Notifier = n
	h.StartupGrace = 1 * time.Second // long enough to cover several ticker fires below
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.Run(ctx, 20*time.Millisecond) // 20ms ticker → ~25 fires in 500ms
	}()

	// Run for half the grace window — every snapshot should be Info, no Telegram.
	time.Sleep(500 * time.Millisecond)
	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 0 {
		t.Errorf("expected zero Telegram alerts during grace window, got %d: %v", len(bodies), bodies)
	}
}

func TestHeartbeat_Run_GraceExpired_TelegramAlertFires(t *testing.T) {
	// Inverse of the above: once the grace window expires, the next stale
	// snapshot must escalate to Warn and fire a Telegram alert. This proves
	// the grace is a window, not a permanent mute.
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var msg map[string]string
		_ = json.Unmarshal(b, &msg)
		mu.Lock()
		bodies = append(bodies, msg["text"])
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	n := &notify.Notifier{
		BotToken: "tok",
		ChatID:   "chat",
		HTTPClient: &http.Client{
			Timeout:   5 * time.Second,
			Transport: rewriteTransport{base: http.DefaultTransport, target: srv.URL},
		},
	}
	h := NewHeartbeat("BTCUSDT")
	h.Notifier = n
	h.StartupGrace = 50 * time.Millisecond // expires almost immediately
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.Run(ctx, 20*time.Millisecond)
	}()

	time.Sleep(300 * time.Millisecond) // well past grace; multiple ticker fires happen post-grace
	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) == 0 {
		t.Fatal("expected ≥1 Telegram alert post-grace, got 0 (grace must not permanently suppress)")
	}
	if !strings.Contains(bodies[0], "no ticks received yet") {
		t.Errorf("post-grace alert msg = %q, want contains 'no ticks received yet'", bodies[0])
	}
}

// rewriteTransport intercepts outbound HTTP requests and rewrites the URL
// to point at the test server. This is the cleanest way to redirect a
// production-shaped Notifier to a test server without modifying the
// production code's URL resolution.
type rewriteTransport struct {
	base   http.RoundTripper
	target string // e.g. "http://127.0.0.1:54321"
}

func (rt rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Replace scheme + host with target's; keep the path.
	target, err := http.NewRequest(req.Method, rt.target+req.URL.Path, req.Body)
	if err != nil {
		return nil, err
	}
	target.Header = req.Header
	return rt.base.RoundTrip(target)
}

// ── Lag tracking (REST-poll-lag instrumentation) ───────────────────────────

func TestHeartbeat_Observe_NoLagWhenLocalReceiptUnset(t *testing.T) {
	// CSV replay path: tick has no LocalReceiptTS. Observe should not
	// pollute the lag ring with bogus zero-time measurements.
	h := NewHeartbeat("BTCUSDT")
	for i := 0; i < 10; i++ {
		h.Observe(models.Tick{
			Symbol:    "BTCUSDT",
			Timestamp: time.Date(2026, 5, 8, 10, i, 0, 0, time.UTC),
			Price:     50000,
			// LocalReceiptTS deliberately unset (zero value)
		})
	}
	_, _, _, n := h.lagSnapshot()
	if n != 0 {
		t.Errorf("CSV-replay path: lagSnapshot n = %d, want 0", n)
	}
}

func TestHeartbeat_Observe_RecordsLagWhenLocalReceiptSet(t *testing.T) {
	// BinanceFutures path: each tick carries LocalReceiptTS. Observe
	// captures lag = LocalReceiptTS - Timestamp into the ring.
	h := NewHeartbeat("BTCUSDT")
	exchange := time.Date(2026, 5, 8, 10, 0, 0, 0, time.UTC)
	for i, ms := range []int64{50, 100, 200, 500, 1000} {
		h.Observe(models.Tick{
			Symbol:         "BTCUSDT",
			Timestamp:      exchange.Add(time.Duration(i) * time.Millisecond),
			LocalReceiptTS: exchange.Add(time.Duration(i)*time.Millisecond + time.Duration(ms)*time.Millisecond),
			Price:          50000,
		})
	}
	p50, p99, maxLag, n := h.lagSnapshot()
	if n != 5 {
		t.Errorf("n = %d, want 5", n)
	}
	if p50 != 200*time.Millisecond {
		t.Errorf("p50 = %v, want 200ms (median of {50, 100, 200, 500, 1000})", p50)
	}
	if maxLag != 1000*time.Millisecond {
		t.Errorf("max = %v, want 1000ms", maxLag)
	}
	// p99 of n=5: index = (5*99)/100 = 4 → samples[4] = 1000ms
	if p99 != 1000*time.Millisecond {
		t.Errorf("p99 = %v, want 1000ms (small-n: clamps to last sample)", p99)
	}
}

func TestHeartbeat_LagRing_WrapsAtCapacity(t *testing.T) {
	// Once we've pushed > lagRingSize samples, the oldest get overwritten.
	// Verify lagFilled flips and the snapshot uses the full ring.
	h := NewHeartbeat("BTCUSDT")
	for i := 0; i < lagRingSize+50; i++ {
		exchange := time.Date(2026, 5, 8, 10, 0, 0, i, time.UTC)
		h.Observe(models.Tick{
			Symbol:         "BTCUSDT",
			Timestamp:      exchange,
			LocalReceiptTS: exchange.Add(time.Duration(i) * time.Millisecond),
		})
	}
	_, _, _, n := h.lagSnapshot()
	if n != lagRingSize {
		t.Errorf("after wrap, n = %d, want %d (lagRingSize)", n, lagRingSize)
	}
}

func TestHeartbeat_LagSnapshot_EmptyRing(t *testing.T) {
	h := NewHeartbeat("BTCUSDT")
	p50, p99, maxLag, n := h.lagSnapshot()
	if n != 0 {
		t.Errorf("empty ring: n = %d, want 0", n)
	}
	if p50 != 0 || p99 != 0 || maxLag != 0 {
		t.Errorf("empty ring: percentiles = (%v, %v, %v), want all zero", p50, p99, maxLag)
	}
}

func TestSnapshotForLog_IncludesLagArgsWhenSamplesPresent(t *testing.T) {
	// Lag args must be appended when the ring has samples — operator
	// reads lag_p99_ms from heartbeat output to detect API degradation.
	h := NewHeartbeat("BTCUSDT")
	now := time.Now()
	tick := models.Tick{
		Symbol:         "BTCUSDT",
		Timestamp:      now.Add(-50 * time.Millisecond),
		LocalReceiptTS: now,
	}
	for i := 0; i < 10; i++ {
		h.Observe(tick)
	}
	_, _, args, _ := h.snapshotForLog(0)
	hasLagP50 := false
	hasLagSamples := false
	for i := 0; i+1 < len(args); i += 2 {
		k, _ := args[i].(string)
		if k == "lag_p50_ms" {
			hasLagP50 = true
		}
		if k == "lag_samples" {
			hasLagSamples = true
		}
	}
	if !hasLagP50 {
		t.Errorf("snapshotForLog should include lag_p50_ms when samples present, got args: %v", args)
	}
	if !hasLagSamples {
		t.Errorf("snapshotForLog should include lag_samples when samples present, got args: %v", args)
	}
}

func TestSnapshotForLog_OmitsLagArgsWhenNoSamples(t *testing.T) {
	// CSV replay or pre-first-tick state: no lag samples, lag args
	// should NOT appear in heartbeat output (don't pollute schema with
	// "lag_p50_ms": 0 misleading the operator).
	h := NewHeartbeat("BTCUSDT")
	// Observe with no LocalReceiptTS — increments tickCount but does
	// NOT populate the lag ring.
	h.Observe(models.Tick{
		Symbol:    "BTCUSDT",
		Timestamp: time.Now(),
	})
	_, _, args, _ := h.snapshotForLog(0)
	for i := 0; i+1 < len(args); i += 2 {
		k, _ := args[i].(string)
		if strings.HasPrefix(k, "lag_") {
			t.Errorf("snapshotForLog should NOT include lag_* args without samples, got: %s = %v", k, args[i+1])
		}
	}
}
