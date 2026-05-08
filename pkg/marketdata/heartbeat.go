package marketdata

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/cristianmanoliu/trading-engine/pkg/models"
	"github.com/cristianmanoliu/trading-engine/pkg/notify"
)

// Heartbeat periodically emits a liveness log line showing the last tick timestamp
// and tick count since the previous heartbeat. A gap exceeding heartbeatStaleThreshold
// between ticks is escalated to Warn level so log-based alerting can detect a stalled
// feed. The Warn line is what scripts/post_deploy_check.sh parses for tick-freshness
// audits — the threshold and the escalation behavior are operational contract.
//
// Notifier is opt-in (nil = no Telegram alerts, just slog as before). When set,
// Warn-level heartbeats also emit a SeverityWarn alert per the locked telegram
// alert design rule. The Notifier itself enforces rate limiting + mute hours,
// so this code does not gate the call.
type Heartbeat struct {
	lastTick  atomic.Pointer[time.Time] // pointer so it's nullable before first tick
	tickCount atomic.Int64
	symbol    string

	Notifier *notify.Notifier // optional; nil disables Telegram alerts
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
// When Notifier is set, Warn-level heartbeats also fire a SeverityWarn
// Telegram alert (the Notifier handles its own rate limiting and mute hours).
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
				if h.Notifier != nil {
					_ = h.Notifier.SendStructured(ctx, notify.SeverityWarn, formatHeartbeatBody(msg, h.symbol, args))
				}
			} else {
				slog.Info(msg, args...)
			}
		}
	}
}

// formatHeartbeatBody renders the heartbeat slog args into a multi-line body
// suitable for Telegram. Skips the "symbol" key (already in the prefix line)
// and renders the remaining key-value pairs as `key: value` lines.
func formatHeartbeatBody(msg string, symbol string, args []any) string {
	body := fmt.Sprintf("%s\nsymbol: %s", msg, symbol)
	for i := 0; i+1 < len(args); i += 2 {
		k, ok := args[i].(string)
		if !ok || k == "symbol" {
			continue
		}
		body += fmt.Sprintf("\n%s: %v", k, args[i+1])
	}
	return body
}
