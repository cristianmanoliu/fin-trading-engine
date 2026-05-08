package marketdata

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/cristianmanoliu/trading-engine/pkg/models"
)

// Heartbeat periodically emits a liveness log line showing the last tick timestamp
// and tick count since the previous heartbeat. A gap exceeding heartbeatStaleThreshold
// between ticks is escalated to Warn level so log-based alerting can detect a stalled
// feed. The Warn line is what scripts/post_deploy_check.sh parses for tick-freshness
// audits — the threshold and the escalation behavior are operational contract.
type Heartbeat struct {
	lastTick  atomic.Pointer[time.Time] // pointer so it's nullable before first tick
	tickCount atomic.Int64
	symbol    string
}

// heartbeatStaleThreshold is the age beyond which a heartbeat log line is
// escalated from Info to Warn. Operational scripts and log-based alerting
// rely on this contract — changing it (or the > vs >= comparison) silently
// alters which intervals trigger the post-deploy check's STALE warning.
const heartbeatStaleThreshold = 90 * time.Second

func NewHeartbeat(symbol string) *Heartbeat {
	return &Heartbeat{symbol: symbol}
}

// Observe records a tick for heartbeat tracking. Safe to call from one goroutine.
func (h *Heartbeat) Observe(tick models.Tick) {
	t := tick.Timestamp
	h.lastTick.Store(&t)
	h.tickCount.Add(1)
}

// snapshotForLog computes the level, message, and structured args for the next
// heartbeat log line. Extracted from Run so the decision logic (no-ticks → Warn,
// stale → Warn, otherwise Info) is unit-testable without spinning up a ticker
// and capturing slog output. Returns the post-snapshot tickCount so the caller
// can advance prevCount for the next interval.
func (h *Heartbeat) snapshotForLog(prevCount int64) (level slog.Level, msg string, args []any, count int64) {
	count = h.tickCount.Load()
	delta := count - prevCount
	last := h.lastTick.Load()
	if last == nil {
		return slog.LevelWarn, "heartbeat: no ticks received yet",
			[]any{"symbol", h.symbol}, count
	}
	age := time.Since(*last).Round(time.Second)
	args = []any{
		"symbol", h.symbol,
		"ticks_since_last", delta,
		"last_tick_age", age,
	}
	if age > heartbeatStaleThreshold {
		return slog.LevelWarn, "heartbeat: feed appears stalled", args, count
	}
	return slog.LevelInfo, "heartbeat", args, count
}

// Run emits a heartbeat log every interval until ctx is cancelled.
func (h *Heartbeat) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var prevCount int64
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			level, msg, args, count := h.snapshotForLog(prevCount)
			prevCount = count
			if level == slog.LevelWarn {
				slog.Warn(msg, args...)
			} else {
				slog.Info(msg, args...)
			}
		}
	}
}
