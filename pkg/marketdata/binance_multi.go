package marketdata

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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

// backfillAll runs backfill sequentially for each symbol. Sequential rather
// than parallel to avoid the 48,000-weight burst that triggered the
// 2026-05-06 IP ban (32 engines × 1500-kline backfill simultaneously).
//
// At ~300ms per symbol × 16 symbols = ~5 seconds total. Acceptable startup cost.
// Backfill failure for one symbol is logged but does not abort — that symbol
// runs blind for ~24h until DailyLevels populates from live ticks.
func (b *BinanceFuturesMulti) backfillAll(ctx context.Context, channels map[string]chan models.Tick) {
	for _, sym := range b.symbols {
		ch, ok := channels[sym]
		if !ok {
			continue
		}
		// Reuse the single-symbol backfill from binance.go via a temporary
		// BinanceFutures instance. Avoids duplicating REST-kline logic.
		single := &BinanceFutures{
			restURL:       b.restURL,
			symbol:        sym,
			backfillHours: b.backfillHours,
		}
		if err := single.backfill(ctx, ch); err != nil {
			slog.Warn("multi: backfill failed for symbol — proceeding without history",
				"symbol", sym, "err", err)
		}
		// Yield between symbols so any 429 has time to clear.
		select {
		case <-ctx.Done():
			return
		case <-time.After(50 * time.Millisecond):
		}
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
	if len(b.symbols) == 0 {
		return nil, fmt.Errorf("multi: no symbols")
	}

	// Owned (writable) channels for backfill + readLoop. Returned to caller as
	// receive-only.
	owned := make(map[string]chan models.Tick, len(b.symbols))
	exposed := make(map[string]<-chan models.Tick, len(b.symbols))
	for _, sym := range b.symbols {
		ch := make(chan models.Tick, 8192)
		owned[sym] = ch
		exposed[sym] = ch
	}

	// Backfill BEFORE opening WS so DailyLevels is primed when first live tick
	// arrives. Sequential — see backfillAll comment.
	b.backfillAll(ctx, owned)

	url := buildCombinedURL(b.wsURL, b.symbols)
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, url, nil)
	if err != nil {
		for _, ch := range owned {
			close(ch)
		}
		return nil, fmt.Errorf("dial combined ws: %w", err)
	}
	b.connMu.Lock()
	b.conn = conn
	b.connMu.Unlock()

	// readLoop owns all channel writes and closes after this point.
	// Build a routing map matching routeEnvelope's signature.
	routeMap := make(map[string]chan<- models.Tick, len(owned))
	for k, v := range owned {
		routeMap[k] = v
	}

	go b.readLoop(ctx, routeMap, owned)
	return exposed, nil
}

// readLoop drains the combined WS and routes ticks until ctx is cancelled or
// reconnect attempts give up. On exit, all per-symbol channels are closed so
// downstream consumers terminate cleanly.
func (b *BinanceFuturesMulti) readLoop(
	ctx context.Context,
	routeMap map[string]chan<- models.Tick,
	owned map[string]chan models.Tick,
) {
	defer func() {
		for _, ch := range owned {
			close(ch)
		}
	}()

	consecutiveStalls := 0
	const maxStalls = 5
	backoff := time.Second
	const maxBackoff = 30 * time.Second

	// firstMsgGuard: after each (re)connect, if no message arrives within 60s,
	// the connection landed on a zero-frame Tokyo backend. Force reconnect to
	// re-roll DNS.
	const zeroFrameTimeout = 60 * time.Second
	connectTime := time.Now()
	gotFirstMsg := false

	for {
		if ctx.Err() != nil {
			return
		}

		// Zero-frame check.
		if !gotFirstMsg && time.Since(connectTime) > zeroFrameTimeout {
			slog.Warn("multi: zero-frame backend detected, forcing reconnect",
				"connect_age", time.Since(connectTime).Round(time.Second))
			b.connMu.Lock()
			if b.conn != nil {
				b.conn.Close()
				b.conn = nil
			}
			b.connMu.Unlock()
			if !b.reconnect(ctx) {
				return
			}
			connectTime = time.Now()
			gotFirstMsg = false
			consecutiveStalls = 0
			continue
		}

		b.connMu.Lock()
		conn := b.conn
		b.connMu.Unlock()
		if conn == nil {
			if !b.reconnect(ctx) {
				return
			}
			connectTime = time.Now()
			gotFirstMsg = false
			continue
		}

		// Use a short read deadline while waiting for first message so we
		// re-check the zero-frame timer.
		readTimeout := wsReadDeadline
		if !gotFirstMsg {
			readTimeout = 10 * time.Second
		}
		conn.SetReadDeadline(time.Now().Add(readTimeout))
		_, msg, err := conn.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			// If we never received first msg, close the conn so the top-of-loop
			// conn-nil check (or zero-frame check) triggers reconnect.
			// CRITICAL: gorilla/websocket panics with "repeated read on failed websocket
			// connection" if we call ReadMessage twice on a failed conn without closing.
			if !gotFirstMsg {
				b.connMu.Lock()
				if b.conn != nil {
					b.conn.Close()
					b.conn = nil
				}
				b.connMu.Unlock()
				continue
			}
			consecutiveStalls++
			slog.Warn("multi: ws read error",
				"err", err, "stalls", consecutiveStalls, "max", maxStalls)
			if consecutiveStalls >= maxStalls {
				slog.Error("multi: max stalls reached, terminating",
					"stalls", consecutiveStalls)
				return
			}
			gapStart := time.Now()
			b.connMu.Lock()
			if b.conn != nil {
				b.conn.Close()
				b.conn = nil
			}
			b.connMu.Unlock()
			time.Sleep(backoff)
			backoff = time.Duration(float64(backoff) * 2)
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			if !b.reconnect(ctx) {
				return
			}
			connectTime = time.Now()
			gotFirstMsg = false
			slog.Info("multi: ws gap closed",
				"duration", time.Since(gapStart).Round(time.Second))
			continue
		}

		gotFirstMsg = true
		consecutiveStalls = 0
		backoff = time.Second

		if err := routeEnvelope(msg, routeMap); err != nil {
			slog.Warn("multi: route error", "err", err)
		}
	}
}

// reconnect attempts to re-establish the combined WS. Returns false if ctx is
// cancelled during the attempt.
func (b *BinanceFuturesMulti) reconnect(ctx context.Context) bool {
	url := buildCombinedURL(b.wsURL, b.symbols)
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, url, nil)
	if err != nil {
		slog.Error("multi: reconnect failed", "err", err)
		// Caller's outer loop will retry after backoff.
		return ctx.Err() == nil
	}
	b.connMu.Lock()
	b.conn = conn
	b.connMu.Unlock()
	slog.Info("multi: ws reconnected", "symbols", len(b.symbols))
	return true
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
