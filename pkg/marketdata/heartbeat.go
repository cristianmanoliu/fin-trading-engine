package marketdata

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/cristianmanoliu/trading-engine/pkg/models"
)

// Heartbeat periodically emits a liveness log line showing the last tick timestamp
// and tick count since the previous heartbeat. A gap of > 90s between ticks is
// escalated to Warn level so log-based alerting can detect a stalled feed.
type Heartbeat struct {
	lastTick  atomic.Pointer[time.Time] // pointer so it's nullable before first tick
	tickCount atomic.Int64
	symbol    string
}

func NewHeartbeat(symbol string) *Heartbeat {
	return &Heartbeat{symbol: symbol}
}

// Observe records a tick for heartbeat tracking. Safe to call from one goroutine.
func (h *Heartbeat) Observe(tick models.Tick) {
	t := tick.Timestamp
	h.lastTick.Store(&t)
	h.tickCount.Add(1)
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
			count := h.tickCount.Load()
			delta := count - prevCount
			prevCount = count

			last := h.lastTick.Load()
			if last == nil {
				slog.Warn("heartbeat: no ticks received yet", "symbol", h.symbol)
				continue
			}

			age := time.Since(*last).Round(time.Second)
			args := []any{
				"symbol", h.symbol,
				"ticks_since_last", delta,
				"last_tick_age", age,
			}
			if age > 90*time.Second {
				slog.Warn("heartbeat: feed appears stalled", args...)
			} else {
				slog.Info("heartbeat", args...)
			}
		}
	}
}
