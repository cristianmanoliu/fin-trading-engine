package marketdata

import (
	"context"

	"github.com/cristianmanoliu/fin-trading-engine/pkg/models"
)

// DataSource is the single abstraction over live exchange feeds and historical replays.
// Swapping implementations is the only change needed to move between backtest and live.
type DataSource interface {
	// Subscribe starts the data feed and returns a read-only tick channel.
	// The channel is closed when the context is cancelled or the source exhausts its data.
	Subscribe(ctx context.Context) (<-chan models.Tick, error)
	// Close shuts down the underlying connection or file handle.
	Close() error
}
