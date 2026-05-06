package marketdata

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cristianmanoliu/trading-engine/pkg/models"
	"github.com/gorilla/websocket"
)

// combinedEnvelope wraps every payload on the Binance combined-streams endpoint.
// Format: {"stream":"<symbol>@aggTrade","data":{...}}
type combinedEnvelope struct {
	Stream string          `json:"stream"`
	Data   json.RawMessage `json:"data"`
}

// buildCombinedURL constructs the Binance combined-streams WS URL.
// Symbols are normalised to lowercase and suffixed with @aggTrade.
// Up to 1024 streams per Binance limit; we never approach that.
func buildCombinedURL(base string, symbols []string) string {
	parts := make([]string, 0, len(symbols))
	for _, s := range symbols {
		parts = append(parts, strings.ToLower(s)+"@aggTrade")
	}
	return base + "/stream?streams=" + strings.Join(parts, "/")
}

// routeEnvelope parses one combined-streams message and pushes the resulting
// tick into the channel for that symbol. Unknown symbols are silently dropped
// (we should never subscribe to a symbol we don't have a channel for, but be
// defensive — Binance can occasionally echo extras around connection lifecycle).
//
// Returns an error only on unparseable JSON; routing decisions and parse
// failures on individual fields are skipped (logging deferred to Task 6's readLoop).
func routeEnvelope(raw []byte, channels map[string]chan<- models.Tick) error {
	var env combinedEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("envelope unmarshal: %w", err)
	}

	// Stream name format: "<symbol-lowercase>@aggTrade". Extract symbol.
	at := strings.IndexByte(env.Stream, '@')
	if at <= 0 {
		return nil // malformed stream name — skip
	}
	symUpper := strings.ToUpper(env.Stream[:at])

	ch, ok := channels[symUpper]
	if !ok {
		return nil // not subscribed — drop
	}

	var trade aggTradeMsg
	if err := json.Unmarshal(env.Data, &trade); err != nil {
		return nil // data malformed — skip silently
	}
	if trade.EventType != "aggTrade" {
		return nil
	}

	price, err := strconv.ParseFloat(trade.Price, 64)
	if err != nil {
		return nil
	}
	qty, err := strconv.ParseFloat(trade.Qty, 64)
	if err != nil {
		return nil
	}

	tick := models.Tick{
		Symbol:    symUpper,
		Timestamp: time.UnixMilli(trade.TradeTime).UTC(),
		Price:     price,
		Volume:    qty,
	}

	select {
	case ch <- tick:
	default:
		// TODO(task-6): channel full — surface drop counter via slog in readLoop layer.
	}
	return nil
}

// BinanceFuturesMulti opens a single combined-streams WebSocket for N symbols
// and demuxes ticks into per-symbol channels. Replaces N separate
// BinanceFutures instances with one connection.
//
// Unlike BinanceFutures, this type does NOT fall back to per-symbol REST
// aggTrade polling on stall — that fallback is the cause of the 2026-05-06
// IP ban (synchronized REST cap overshoot). Stalls trigger WS reconnect only;
// data gaps during outages are accepted.
type BinanceFuturesMulti struct {
	wsURL         string
	restURL       string
	symbols       []string
	backfillHours int
	conn          *websocket.Conn
	connMu        sync.Mutex
}

func NewBinanceFuturesMulti(wsURL, restURL string, symbols []string, backfillHours int) *BinanceFuturesMulti {
	return &BinanceFuturesMulti{
		wsURL:         wsURL,
		restURL:       restURL,
		symbols:       append([]string(nil), symbols...),
		backfillHours: backfillHours,
	}
}

// SubscribeMulti opens the combined-streams WebSocket, performs a per-symbol
// REST kline backfill (sequential), and returns a read-only tick channel per
// symbol. All channels are closed when ctx is cancelled or the upstream
// connection terminates permanently.
//
// Symbol keys in the returned map are uppercase (matching the input format).
// Buffer size per channel: 8192 — same as single-symbol BinanceFutures, sized
// to hold full backfill (1500 klines × 4 ticks = 6000) with headroom.
func (b *BinanceFuturesMulti) SubscribeMulti(ctx context.Context) (map[string]<-chan models.Tick, error) {
	// Implementation in Task 5 + Task 6.
	return nil, fmt.Errorf("not implemented")
}

// Close shuts down the underlying WebSocket connection. Safe to call multiple
// times.
func (b *BinanceFuturesMulti) Close() error {
	b.connMu.Lock()
	defer b.connMu.Unlock()
	if b.conn != nil {
		err := b.conn.Close()
		b.conn = nil
		return err
	}
	return nil
}
