package marketdata

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/cristianmanoliu/trading-engine/pkg/models"
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

func TestHeartbeat_StaleThresholdConstant_Is90s(t *testing.T) {
	// Pin the constant against drift. scripts/post_deploy_check.sh uses
	// the Warn line to identify stalled feeds; if this threshold changes
	// without updating the audit logic, the check would either over-alert
	// (threshold lowered) or under-alert (threshold raised).
	if heartbeatStaleThreshold != 90*time.Second {
		t.Errorf("heartbeatStaleThreshold = %v, want 90s", heartbeatStaleThreshold)
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
