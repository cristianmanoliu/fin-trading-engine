package marketdata

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/cristianmanoliu/trading-engine/pkg/models"
)

const (
	wsReadDeadline   = 90 * time.Second
	wsMaxStalls      = 2 // consecutive i/o timeouts before falling back to REST polling
	// 16 symbols × (60/10) polls/min × 20 weight/call = 1920 weight/min (Binance limit: 2400/min).
	// Do NOT lower this without recomputing: at 6s × 16 the fleet sat in 50% rate-limit backoff.
	restPollInterval = 10 * time.Second
	restPollLimit    = 500
)

// BinanceFutures streams aggTrade events from Binance USDT-M Futures.
// On Subscribe it first performs a REST kline backfill so that DailyLevels
// is primed before the first live tick arrives (eliminating the 24h cold-start blind period).
// If the WebSocket stalls for more than wsMaxStalls consecutive timeouts, it automatically
// switches to REST aggTrade polling (fromId-based, zero gaps) as a fallback.
type BinanceFutures struct {
	wsURL         string
	restURL       string
	symbol        string
	backfillHours int
	conn          *websocket.Conn
}

func NewBinanceFutures(wsURL, restURL, symbol string, backfillHours int) *BinanceFutures {
	return &BinanceFutures{
		wsURL:         wsURL,
		restURL:       restURL,
		symbol:        symbol,
		backfillHours: backfillHours,
	}
}

func (b *BinanceFutures) Subscribe(ctx context.Context) (<-chan models.Tick, error) {
	// Buffer must hold all backfill ticks before consumers start. Backfill is
	// synchronous in Subscribe and blocks until done; if the buffer is too small,
	// backfill blocks on send and Subscribe never returns.
	// Sized for backfillHours × 60 klines × 4 ticks/kline + 2048 headroom.
	bufSize := b.backfillHours*60*4 + 2048
	if bufSize < 8192 {
		bufSize = 8192 // floor for tiny / zero-backfill configs
	}
	ch := make(chan models.Tick, bufSize)

	// Backfill before opening the WebSocket so the aggregator and DailyLevels
	// are primed from the first live tick. Failure is non-fatal: log a warning
	// and proceed — engine works, just blind for the first UTC day.
	if err := b.backfill(ctx, ch); err != nil {
		slog.Warn("REST kline backfill failed, proceeding without historical data",
			"symbol", b.symbol, "err", err)
	}

	stream := strings.ToLower(b.symbol) + "@aggTrade"
	url := fmt.Sprintf("%s/ws/%s", b.wsURL, stream)

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, url, nil)
	if err != nil {
		close(ch)
		return nil, fmt.Errorf("dial binance ws: %w", err)
	}
	b.conn = conn

	go b.readLoop(ctx, conn, ch)
	return ch, nil
}

// backfill fetches the last backfillHours of 1m klines from the Binance Futures REST API
// and expands them into synthetic ticks, pushing them synchronously into ch before the
// WebSocket is opened.
//
// Pagination: Binance's /fapi/v1/klines caps at 1500 klines per call (= 25h of 1m data).
// To support backfillHours > 25 we walk forward in chained startTime calls. This is
// load-bearing for cold-start indicator priming — a 4H-signal strategy with EMA21 needs
// 22 closed 4H candles ≈ 88h of 1m history before the cross detector can fire.
func (b *BinanceFutures) backfill(ctx context.Context, ch chan<- models.Tick) error {
	totalKlines := b.backfillHours * 60
	if totalKlines <= 0 {
		return nil
	}

	const perCallLimit = 1500
	client := &http.Client{Timeout: 30 * time.Second}

	nowMs := time.Now().UTC().UnixMilli()
	startMs := time.Now().UTC().Add(-time.Duration(b.backfillHours) * time.Hour).UnixMilli()

	fetched := 0
	pages := 0
	ticksEmitted := 0

	for fetched < totalKlines {
		n := perCallLimit
		if remaining := totalKlines - fetched; remaining < n {
			n = remaining
		}

		url := fmt.Sprintf("%s/fapi/v1/klines?symbol=%s&interval=1m&startTime=%d&limit=%d",
			b.restURL, b.symbol, startMs, n)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}

		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("REST klines page %d returned HTTP %d", pages+1, resp.StatusCode)
		}

		// Binance returns a JSON array of arrays:
		// [openMs, open, high, low, close, volume, closeMs, ...]
		var raw [][]json.RawMessage
		if err := json.Unmarshal(body, &raw); err != nil {
			return fmt.Errorf("decode klines page %d: %w", pages+1, err)
		}
		if len(raw) == 0 {
			break
		}

		var lastOpenMs int64
		var skippedMalformed int
		for _, row := range raw {
			if len(row) < 7 {
				skippedMalformed++
				continue
			}
			openMs, err := parseRawInt64(row[0])
			if err != nil {
				skippedMalformed++
				continue
			}
			closeMs, err := parseRawInt64(row[6])
			if err != nil {
				skippedMalformed++
				continue
			}
			// Audit-pattern fix 2026-05-10: previously these parse errors were
			// swallowed via `o, _ := parseRawFloat(row[1])`. A malformed price
			// field from Binance would produce a kline at price=0, which then
			// expands into 0-priced ticks that poison EMA / BB / ATR priming.
			// Skip-on-error matches the same-row int64 handling above; gaps
			// are recoverable from subsequent klines + live ticks, but
			// 0-priced indicators are not.
			o, oerr := parseRawFloat(row[1])
			h, herr := parseRawFloat(row[2])
			l, lerr := parseRawFloat(row[3])
			c, cerr := parseRawFloat(row[4])
			v, verr := parseRawFloat(row[5])
			if oerr != nil || herr != nil || lerr != nil || cerr != nil || verr != nil {
				skippedMalformed++
				continue
			}

			for _, tick := range expandKlineToTicks(openMs, closeMs, o, h, l, c, v, b.symbol) {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case ch <- tick:
					ticksEmitted++
				}
			}
			lastOpenMs = openMs
		}

		fetched += len(raw)
		pages++

		// Surface malformed-row count if any. A non-zero count means Binance
		// returned data the parser couldn't handle — could be transient
		// (single bad row) or signal a schema change (every row failing).
		// Either way the operator should know rather than silently lose
		// klines from indicator priming.
		if skippedMalformed > 0 {
			slog.Warn("kline backfill: skipped malformed rows",
				"symbol", b.symbol,
				"page", pages,
				"skipped", skippedMalformed,
				"page_size", len(raw))
		}

		// Advance cursor past the last 1m bar we just received.
		nextStart := lastOpenMs + 60_000
		if nextStart >= nowMs {
			break // caught up to the current minute
		}
		startMs = nextStart

		// Server returned fewer rows than asked for → end of available history.
		if len(raw) < n {
			break
		}
	}

	slog.Info("kline backfill complete",
		"symbol", b.symbol,
		"hours", b.backfillHours,
		"klines", fetched,
		"pages", pages,
		"ticks", ticksEmitted)
	return nil
}

func parseRawInt64(r json.RawMessage) (int64, error) {
	// Binance timestamps arrive as JSON numbers (not strings).
	var n int64
	if err := json.Unmarshal(r, &n); err != nil {
		// Fallback: might be quoted.
		var s string
		if err2 := json.Unmarshal(r, &s); err2 != nil {
			return 0, err
		}
		return strconv.ParseInt(s, 10, 64)
	}
	return n, nil
}

func parseRawFloat(r json.RawMessage) (float64, error) {
	// Binance price/volume fields arrive as quoted strings.
	var s string
	if err := json.Unmarshal(r, &s); err != nil {
		// Fallback: might be an unquoted number.
		var f float64
		if err2 := json.Unmarshal(r, &f); err2 != nil {
			return 0, err
		}
		return f, nil
	}
	return strconv.ParseFloat(s, 64)
}

// aggTradeMsg is the Binance aggTrade WebSocket message shape.
//
// EventTime is non-obvious but load-bearing. Binance includes BOTH "e":"aggTrade"
// and "E":<timestamp> in every payload. Without an explicit field for "E", Go's
// encoding/json case-insensitive fallback tries to assign the numeric "E" value
// into the string-typed EventType field tagged "e", returning a non-nil error
// from Unmarshal AND silently dropping the tick at readLoop's `if err != nil
// { continue }` (line 254). Discovered 2026-05-06 via the multi-engine refactor —
// the per-symbol live engines had been silently relying on REST aggTrade fallback
// for live ticks because every WS message was being dropped. Adding this field
// fixes the WS path with zero behavior change for callers.
type aggTradeMsg struct {
	EventType string `json:"e"`
	EventTime int64  `json:"E"` // see type comment — required to prevent case-insensitive json collision with "e"
	TradeTime int64  `json:"T"`
	Price     string `json:"p"`
	Qty       string `json:"q"`
}

// aggTradeREST is the shape returned by GET /fapi/v1/aggTrades.
type aggTradeREST struct {
	ID        int64  `json:"a"`
	Price     string `json:"p"`
	Qty       string `json:"q"`
	Timestamp int64  `json:"T"`
}

func (b *BinanceFutures) readLoop(ctx context.Context, conn *websocket.Conn, ch chan<- models.Tick) {
	defer close(ch)

	backoff := time.Second
	maxBackoff := 30 * time.Second
	consecutiveStalls := 0

	for {
		if ctx.Err() != nil {
			return
		}

		conn.SetReadDeadline(time.Now().Add(wsReadDeadline))
		_, msg, err := conn.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return
			}

			consecutiveStalls++
			slog.Warn("binance ws read error, reconnecting",
				"err", err, "backoff", backoff, "stalls", consecutiveStalls)

			// After wsMaxStalls consecutive timeouts the WebSocket cluster is not
			// delivering data. Switch to REST aggTrade polling as a fallback.
			if consecutiveStalls >= wsMaxStalls {
				slog.Warn("WebSocket stalled repeatedly — switching to REST aggTrade polling",
					"symbol", b.symbol, "stalls", consecutiveStalls)
				conn.Close()
				b.aggTradeLoop(ctx, ch)
				return
			}

			gapStart := time.Now()
			time.Sleep(backoff)
			backoff = time.Duration(math.Min(float64(backoff*2), float64(maxBackoff)))

			stream := strings.ToLower(b.symbol) + "@aggTrade"
			wsURL := fmt.Sprintf("%s/ws/%s", b.wsURL, stream)
			newConn, _, dialErr := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
			if dialErr != nil {
				slog.Error("binance ws reconnect failed", "err", dialErr)
				continue
			}
			slog.Info("ws gap closed",
				"symbol", b.symbol,
				"duration", time.Since(gapStart).Round(time.Second))
			conn = newConn
			backoff = time.Second
			continue
		}

		consecutiveStalls = 0
		backoff = time.Second

		var trade aggTradeMsg
		if err := json.Unmarshal(msg, &trade); err != nil {
			continue
		}
		if trade.EventType != "aggTrade" {
			continue
		}

		price, err := strconv.ParseFloat(trade.Price, 64)
		if err != nil {
			continue
		}
		qty, err := strconv.ParseFloat(trade.Qty, 64)
		if err != nil {
			continue
		}

		tick := models.Tick{
			Symbol:    b.symbol,
			Timestamp: time.UnixMilli(trade.TradeTime).UTC(),
			Price:     price,
			Volume:    qty,
		}

		select {
		case <-ctx.Done():
			return
		case ch <- tick:
		}
	}
}

// aggTradeLoop polls GET /fapi/v1/aggTrades using fromId pagination to deliver
// every trade with zero gaps. Called automatically when WebSocket stalls.
func (b *BinanceFutures) aggTradeLoop(ctx context.Context, ch chan<- models.Tick) {
	// Retry initial ID fetch with backoff — 418/429 rate-limit bans must not exit the loop.
	var lastID int64
	for {
		id, err := b.latestAggTradeID(ctx)
		if err == nil {
			lastID = id
			break
		}
		if ctx.Err() != nil {
			return
		}
		slog.Warn("aggTrade fallback: initial ID fetch failed, retrying in 30s",
			"symbol", b.symbol, "err", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}
	}

	slog.Info("REST aggTrade polling active", "symbol", b.symbol, "fromID", lastID)

	ticker := time.NewTicker(restPollInterval)
	defer ticker.Stop()

	client := &http.Client{Timeout: 10 * time.Second}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		url := fmt.Sprintf("%s/fapi/v1/aggTrades?symbol=%s&fromId=%d&limit=%d",
			b.restURL, b.symbol, lastID+1, restPollLimit)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			slog.Warn("aggTrade poll: request build error", "symbol", b.symbol, "err", err)
			continue
		}

		resp, err := client.Do(req)
		if err != nil {
			slog.Warn("aggTrade poll: HTTP error", "symbol", b.symbol, "err", err)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			// Audit-pattern fix 2026-05-10: previously this `continue` was
			// silent — a body-read failure mid-response (network blip after
			// the headers but before the full body) would silently miss the
			// batch's trades with zero operator visibility. The polling
			// loop's other failure paths all slog.Warn; this one was the
			// outlier. Symmetric logging keeps the operator's mental model
			// of "every recoverable failure is visible somewhere."
			slog.Warn("aggTrade poll: body read error", "symbol", b.symbol, "err", err)
			continue
		}

		// 418 = IP banned; 429 = rate limit exceeded. Back off 60s — do not exit.
		if resp.StatusCode == 418 || resp.StatusCode == 429 {
			slog.Warn("aggTrade poll: rate limited, backing off 60s",
				"symbol", b.symbol, "status", resp.StatusCode)
			select {
			case <-ctx.Done():
				return
			case <-time.After(60 * time.Second):
			}
			continue
		}

		if resp.StatusCode != http.StatusOK {
			slog.Warn("aggTrade poll: non-200 response", "symbol", b.symbol, "status", resp.StatusCode)
			continue
		}

		var trades []aggTradeREST
		if err := json.Unmarshal(body, &trades); err != nil {
			slog.Warn("aggTrade poll: decode error", "symbol", b.symbol, "err", err)
			continue
		}

		for _, t := range trades {
			price, err := strconv.ParseFloat(t.Price, 64)
			if err != nil {
				continue
			}
			qty, err := strconv.ParseFloat(t.Qty, 64)
			if err != nil {
				continue
			}

			tick := models.Tick{
				Symbol:    b.symbol,
				Timestamp: time.UnixMilli(t.Timestamp).UTC(),
				Price:     price,
				Volume:    qty,
			}

			select {
			case <-ctx.Done():
				return
			case ch <- tick:
			}

			if t.ID > lastID {
				lastID = t.ID
			}
		}

		if len(trades) > 0 {
			slog.Info("aggTrade poll", "symbol", b.symbol, "count", len(trades), "lastID", lastID)
		}
	}
}

// latestAggTradeID fetches the most recent aggTrade ID to use as the starting
// point for REST polling.
func (b *BinanceFutures) latestAggTradeID(ctx context.Context) (int64, error) {
	url := fmt.Sprintf("%s/fapi/v1/aggTrades?symbol=%s&limit=1", b.restURL, b.symbol)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	if resp.StatusCode == 418 || resp.StatusCode == 429 {
		return 0, fmt.Errorf("rate limited (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var trades []aggTradeREST
	if err := json.Unmarshal(body, &trades); err != nil {
		return 0, fmt.Errorf("decode aggTrades: %w", err)
	}
	if len(trades) == 0 {
		return 0, fmt.Errorf("empty aggTrades response")
	}
	return trades[0].ID, nil
}

func (b *BinanceFutures) Close() error {
	if b.conn != nil {
		return b.conn.Close()
	}
	return nil
}
