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
//
// StartupGrace suppresses Warn alerts during the first N seconds after Run()
// starts. The Binance source can take up to ~3 minutes to deliver its first
// live tick after restart (backfill ~30s + WebSocket read deadline 90s ×
// wsMaxStalls 2 = 180s before the REST aggTrade fallback engages). Without
// grace, every redeploy fires ~16 stale-feed Telegram alerts that auto-clear
// — pure noise that desensitizes the operator. Default 0 preserves the legacy
// alert-immediately behavior for tests and any caller that does not set it.
type Heartbeat struct {
	lastTick  atomic.Pointer[time.Time] // pointer so it's nullable before first tick
	tickCount atomic.Int64
	symbol    string

	Notifier     *notify.Notifier // optional; nil disables Telegram alerts
	StartupGrace time.Duration    // optional; 0 disables the grace window
	startupAt    time.Time        // set by Run; consulted by snapshotForLog
}

// heartbeatStaleThreshold is the age beyond which a heartbeat log line is
// escalated from Info to Warn. Operational scripts and log-based alerting
// rely on this contract — changing it (or the > vs >= comparison) silently
// alters which intervals trigger the post-deploy check's STALE warning.
//
// Aligned with the WS→REST fallback boundary (wsReadDeadline × wsMaxStalls =
// 90s × 2 = 180s = DefaultStartupGrace). Before alignment, WARN could fire at
// 90s while the engine was still on its first WS read stall — auto-clearing
// noise during normal internal recovery. After alignment, WARN fires only
// once the engine's own recovery window has elapsed, separating genuine feed
// stalls from thin-liquidity quiet periods on low-volume altcoins (KAVAUSDT,
// IMXUSDT, ROSEUSDT) where natural inter-trade gaps routinely cross 90s.
const heartbeatStaleThreshold = 180 * time.Second

// DefaultStartupGrace covers backfill (~30s) + WebSocket read deadline
// (90s) × wsMaxStalls (2) = the boundary at which BinanceFutures.readLoop
// gives up on the WebSocket and switches to REST aggTrade polling. Tracked
// here as a package constant so the heartbeat grace and the binance.go
// fallback boundary can drift in lockstep if they ever change.
const DefaultStartupGrace = 3 * time.Minute

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
//
// During the startup grace window (StartupGrace > 0 and time.Since(startupAt)
// within grace), Warn outcomes are downgraded to Info "warming up" so the
// transient WS→REST fallback gap on engine startup does not flood Telegram.
// Once grace expires the level rules return to their normal contract — a
// real stalled feed will fire Warn at the next interval after grace ends.
func (h *Heartbeat) snapshotForLog(prevCount int64) (level slog.Level, msg string, args []any, count int64) {
	count = h.tickCount.Load()
	delta := count - prevCount
	last := h.lastTick.Load()

	inGrace := h.StartupGrace > 0 && !h.startupAt.IsZero() &&
		time.Since(h.startupAt) < h.StartupGrace

	if last == nil {
		if inGrace {
			return slog.LevelInfo, "heartbeat: warming up (no ticks yet)",
				[]any{"symbol", h.symbol, "uptime", time.Since(h.startupAt).Round(time.Second)}, count
		}
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
		if inGrace {
			args = append(args, "uptime", time.Since(h.startupAt).Round(time.Second))
			return slog.LevelInfo, "heartbeat: warming up (feed not yet established)", args, count
		}
		return slog.LevelWarn, "heartbeat: feed appears stalled", args, count
	}
	return slog.LevelInfo, "heartbeat", args, count
}

// Run emits a heartbeat log every interval until ctx is cancelled.
// When Notifier is set, Warn-level heartbeats also fire a SeverityWarn
// Telegram alert (the Notifier handles its own rate limiting and mute hours).
//
// Run records the wall-clock start time for the StartupGrace check —
// snapshotForLog consults it to suppress Warn alerts during the post-restart
// window. Direct callers of snapshotForLog in tests can set startupAt via
// the test helper if they want to exercise the grace path.
func (h *Heartbeat) Run(ctx context.Context, interval time.Duration) {
	h.startupAt = time.Now()
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
