# Multi-Symbol Engine Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the per-symbol `cmd/engine` deployment (16-32 systemd units, each opening its own Binance WS) with a single multi-symbol engine process that opens ONE combined-stream WebSocket and fans ticks out to per-symbol strategy goroutines.

**Architecture:** Add a sibling `BinanceFuturesMulti` type in `pkg/marketdata` that subscribes to Binance's combined-streams endpoint (`wss://fstream.binance.com/stream?streams=...`), parses the `{stream, data}` envelope, and demuxes into a `map[string]<-chan models.Tick`. Extend `cmd/engine` with a `--symbols` CLI flag that switches it into multi-mode: load each symbol's YAML config, start one BinanceFuturesMulti, then spawn per-symbol goroutine groups (aggregator + strategy runner + Stub executor + heartbeat) that consume from their dedicated channel. Drop the per-symbol REST-aggTrade fallback (the failure mode that caused the 2026-05-06 IP ban); rely on aggressive WS reconnect with explicit IP rotation to dodge the Tokyo zero-frame backends.

**Tech Stack:** Go 1.26, `github.com/gorilla/websocket`, existing `pkg/marketdata` / `pkg/aggregator` / `pkg/strategy` / `pkg/execution`, `golang.org/x/sync/errgroup` for goroutine lifecycle.

**Out of scope (deferred):**
- Kline catch-up REST call after long WS gaps (multi-hour outages currently produce silent gaps)
- Per-symbol REST-aggTrade fallback (intentionally removed — was the cause of the 2026-05-06 ban)
- Reusing the multi-stream client for the backtest path (`cmd/backtest` continues to use `CSVReplay` unchanged)

---

## File Structure

| Path | Status | Responsibility |
|---|---|---|
| `pkg/marketdata/binance_multi.go` | **CREATE** | Multi-stream WS client. `BinanceFuturesMulti` type + `SubscribeMulti` returning per-symbol channels. ~250 lines. |
| `pkg/marketdata/binance_multi_test.go` | **CREATE** | URL builder, envelope parser, demux routing, reconnect path. Pure-Go tests, no live network. ~200 lines. |
| `pkg/marketdata/binance.go` | **UNCHANGED** | Existing single-symbol client kept for `cmd/engine` single-mode and any direct callers. No edits. |
| `pkg/marketdata/source.go` | **UNCHANGED** | `DataSource` interface unchanged. Multi client doesn't implement it (different return shape). |
| `cmd/engine/main.go` | **MODIFY** | Add `--symbols` flag. When set, branch into multi-symbol setup that loads N configs, opens 1 BinanceFuturesMulti, runs N goroutine groups. Single-symbol path unchanged. |
| `cmd/engine/multi.go` | **CREATE** | New file with multi-symbol setup logic (keeps `main.go` readable). Function: `runMulti(ctx, symbols []string, flags struct{...}) error`. ~150 lines. |
| `deploy/systemd/paper-live-multi.service` | **CREATE** | New systemd unit, single ExecStart with comma-separated symbol list. Replaces the 16 `paper-live@*.service` units in production. |
| `CLAUDE.md` | **MODIFY** | Add `## Multi-symbol engine (2026-05-06)` section documenting the new architecture, deploy procedure, and the rationale for dropping REST fallback. Update `## Current state` once the multi engine is deployed. |

---

## Task 1: Add multi-stream message envelope types

**Files:**
- Create: `pkg/marketdata/binance_multi.go`
- Test: `pkg/marketdata/binance_multi_test.go`

**Goal of task:** Define the wire types Binance sends on the combined-streams endpoint. These are pure data structures with no behavior — first task to ground subsequent code.

- [ ] **Step 0: Fix a latent JSON-collision bug in `aggTradeMsg`**

The existing `aggTradeMsg` in `pkg/marketdata/binance.go` lines 182-188 has tag `json:"e"` for `EventType string`. Binance aggTrade payloads include BOTH `"e":"aggTrade"` AND `"E":1748128765432` (event time). Go's `encoding/json` falls back to case-insensitive matching when an exact match isn't found — it tries to assign the number `1748128765432` from `"E"` into the `string` field tagged `e`, returning a non-nil error from `Unmarshal`. The struct gets partially populated, but the error causes `binance.go:253-256` to `continue` and drop the tick. Discovered while writing Task 1's test — same JSON shape fails identically.

Fix: add the missing field so the exact-match path succeeds. In `pkg/marketdata/binance.go`:

```go
// aggTradeMsg is the Binance aggTrade WebSocket message shape.
type aggTradeMsg struct {
	EventType string `json:"e"`
	EventTime int64  `json:"E"` // ← NEW: fixes case-insensitive collision with `e`
	TradeTime int64  `json:"T"`
	Price     string `json:"p"`
	Qty       string `json:"q"`
}
```

This is a non-breaking change (struct grows; existing field reads unchanged). It silently fixes the single-symbol live engine's WS path AND lets Task 1's test pass with realistic Binance JSON.

- [ ] **Step 1: Write the failing test for envelope unmarshalling**

Create `pkg/marketdata/binance_multi_test.go`:

```go
package marketdata

import (
	"encoding/json"
	"testing"
)

func TestCombinedEnvelopeUnmarshal(t *testing.T) {
	// Sample from Binance docs for combined streams:
	// https://binance-docs.github.io/apidocs/futures/en/#websocket-market-streams
	raw := []byte(`{"stream":"btcusdt@aggTrade","data":{"e":"aggTrade","E":1748128765432,"s":"BTCUSDT","a":1234,"p":"80123.45","q":"0.012","T":1748128765400,"m":false}}`)

	var env combinedEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Stream != "btcusdt@aggTrade" {
		t.Errorf("Stream: got %q want %q", env.Stream, "btcusdt@aggTrade")
	}

	var trade aggTradeMsg
	if err := json.Unmarshal(env.Data, &trade); err != nil {
		t.Fatalf("data unmarshal: %v", err)
	}
	if trade.EventType != "aggTrade" || trade.Price != "80123.45" || trade.TradeTime != 1748128765400 {
		t.Errorf("unexpected trade: %+v", trade)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/marketdata/ -run TestCombinedEnvelopeUnmarshal -v`
Expected: FAIL — `combinedEnvelope` undefined.

- [ ] **Step 3: Write the type definition**

Create `pkg/marketdata/binance_multi.go`:

```go
package marketdata

import (
	"encoding/json"
)

// combinedEnvelope wraps every payload on the Binance combined-streams endpoint.
// Format: {"stream":"<symbol>@aggTrade","data":{...}}
type combinedEnvelope struct {
	Stream string          `json:"stream"`
	Data   json.RawMessage `json:"data"`
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./pkg/marketdata/ -run TestCombinedEnvelopeUnmarshal -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/marketdata/binance_multi.go pkg/marketdata/binance_multi_test.go
git commit -m "marketdata: add combined-streams envelope type"
```

---

## Task 2: Multi-stream URL builder

**Files:**
- Modify: `pkg/marketdata/binance_multi.go`
- Modify: `pkg/marketdata/binance_multi_test.go`

**Goal of task:** Build the combined-streams URL from a list of symbols. Pure function.

- [ ] **Step 1: Write the failing test**

Append to `pkg/marketdata/binance_multi_test.go`:

```go
func TestBuildCombinedURL(t *testing.T) {
	tests := []struct {
		name    string
		base    string
		symbols []string
		want    string
	}{
		{
			name:    "single symbol",
			base:    "wss://fstream.binance.com",
			symbols: []string{"BTCUSDT"},
			want:    "wss://fstream.binance.com/stream?streams=btcusdt@aggTrade",
		},
		{
			name:    "three symbols, mixed case",
			base:    "wss://fstream.binance.com",
			symbols: []string{"BTCUSDT", "ethusdt", "SolUsdT"},
			want:    "wss://fstream.binance.com/stream?streams=btcusdt@aggTrade/ethusdt@aggTrade/solusdt@aggTrade",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildCombinedURL(tt.base, tt.symbols)
			if got != tt.want {
				t.Errorf("got %q want %q", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/marketdata/ -run TestBuildCombinedURL -v`
Expected: FAIL — `buildCombinedURL` undefined.

- [ ] **Step 3: Add the builder function**

Append to `pkg/marketdata/binance_multi.go`:

```go
import (
	"strings"
)

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
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./pkg/marketdata/ -run TestBuildCombinedURL -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/marketdata/binance_multi.go pkg/marketdata/binance_multi_test.go
git commit -m "marketdata: add combined-streams URL builder"
```

---

## Task 3: Per-symbol channel router (pure logic)

**Files:**
- Modify: `pkg/marketdata/binance_multi.go`
- Modify: `pkg/marketdata/binance_multi_test.go`

**Goal of task:** Given the parsed envelope and a map of per-symbol channels, route the tick to the right channel. Pure function so we can test without WebSocket.

- [ ] **Step 1: Write the failing test**

Append to `pkg/marketdata/binance_multi_test.go`:

```go
import (
	"testing"
	"time"

	"github.com/cristianmanoliu/trading-engine/pkg/models"
)

func TestRouteEnvelope(t *testing.T) {
	chBTC := make(chan models.Tick, 1)
	chETH := make(chan models.Tick, 1)
	channels := map[string]chan<- models.Tick{
		"BTCUSDT": chBTC,
		"ETHUSDT": chETH,
	}

	raw := []byte(`{"stream":"ethusdt@aggTrade","data":{"e":"aggTrade","T":1748128765400,"p":"3500.50","q":"0.5"}}`)
	if err := routeEnvelope(raw, channels); err != nil {
		t.Fatalf("route: %v", err)
	}

	select {
	case tick := <-chETH:
		if tick.Symbol != "ETHUSDT" {
			t.Errorf("symbol: got %q want ETHUSDT", tick.Symbol)
		}
		if tick.Price != 3500.50 {
			t.Errorf("price: got %v want 3500.50", tick.Price)
		}
		want := time.UnixMilli(1748128765400).UTC()
		if !tick.Timestamp.Equal(want) {
			t.Errorf("timestamp: got %v want %v", tick.Timestamp, want)
		}
	case <-time.After(time.Second):
		t.Fatal("no tick on ETH channel")
	}

	select {
	case <-chBTC:
		t.Fatal("unexpected tick on BTC channel")
	default:
	}
}

func TestRouteEnvelopeUnknownSymbolDropped(t *testing.T) {
	channels := map[string]chan<- models.Tick{}
	raw := []byte(`{"stream":"xrpusdt@aggTrade","data":{"e":"aggTrade","T":1,"p":"1.0","q":"1"}}`)
	// Should NOT error — just drop. We may receive ticks for symbols we
	// don't subscribe to (we shouldn't, but be defensive).
	if err := routeEnvelope(raw, channels); err != nil {
		t.Errorf("expected nil error for unknown symbol, got %v", err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/marketdata/ -run TestRouteEnvelope -v`
Expected: FAIL — `routeEnvelope` undefined.

- [ ] **Step 3: Implement routeEnvelope**

Append to `pkg/marketdata/binance_multi.go`:

```go
import (
	"fmt"
	"strconv"
	"time"

	"github.com/cristianmanoliu/trading-engine/pkg/models"
)

// routeEnvelope parses one combined-streams message and pushes the resulting
// tick into the channel for that symbol. Unknown symbols are silently dropped
// (we should never subscribe to a symbol we don't have a channel for, but be
// defensive — Binance can occasionally echo extras around connection lifecycle).
//
// Returns an error only on unparseable JSON; routing decisions and parse
// failures on individual fields are logged and skipped.
func routeEnvelope(raw []byte, channels map[string]chan<- models.Tick) error {
	var env combinedEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("envelope unmarshal: %w", err)
	}

	// Stream name format: "<symbol-lowercase>@aggTrade". Extract symbol.
	at := -1
	for i, c := range env.Stream {
		if c == '@' {
			at = i
			break
		}
	}
	if at <= 0 {
		return nil // malformed stream name — skip
	}
	sym := env.Stream[:at]
	symUpper := ""
	for _, c := range sym {
		if c >= 'a' && c <= 'z' {
			c = c - 'a' + 'A'
		}
		symUpper += string(c)
	}

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
		// channel full — drop tick rather than block. Log via slog at higher level.
	}
	return nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./pkg/marketdata/ -run TestRouteEnvelope -v`
Expected: PASS for both cases.

- [ ] **Step 5: Commit**

```bash
git add pkg/marketdata/binance_multi.go pkg/marketdata/binance_multi_test.go
git commit -m "marketdata: route combined-stream envelopes to per-symbol channels"
```

---

## Task 4: BinanceFuturesMulti type and SubscribeMulti skeleton

**Files:**
- Modify: `pkg/marketdata/binance_multi.go`

**Goal of task:** Add the public type and constructor; `SubscribeMulti` returns a stub map of channels (nothing arrives yet — that's the next task). This is the API surface that `cmd/engine` will consume.

- [ ] **Step 1: Add the type and constructor**

Append to `pkg/marketdata/binance_multi.go`:

```go
import (
	"context"
	"fmt"
	"sync"

	"github.com/gorilla/websocket"
)

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
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./pkg/marketdata/`
Expected: builds with no errors. (No tests for the stub — that comes in Task 5+6.)

- [ ] **Step 3: Commit**

```bash
git add pkg/marketdata/binance_multi.go
git commit -m "marketdata: add BinanceFuturesMulti type and SubscribeMulti stub"
```

---

## Task 5: Multi-symbol backfill (sequential per-symbol REST klines)

**Files:**
- Modify: `pkg/marketdata/binance_multi.go`

**Goal of task:** On Subscribe, push the last `backfillHours` of synthetic ticks into each per-symbol channel BEFORE opening the WebSocket. Sequential to keep aggregate REST load bounded (1500 weight × N = ~24k weight for 16 symbols, takes ~5s; under any rate cap if spaced).

- [ ] **Step 1: Add backfillAll helper**

Insert into `pkg/marketdata/binance_multi.go`, before `SubscribeMulti`:

```go
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
```

Add the missing imports at the top of `pkg/marketdata/binance_multi.go`:

```go
import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/cristianmanoliu/trading-engine/pkg/models"
)
```

(De-duplicate any imports you already added in earlier tasks; the final import block should match the list above.)

- [ ] **Step 2: Verify it compiles**

Run: `go build ./pkg/marketdata/`
Expected: builds. Existing tests still pass: `go test ./pkg/marketdata/ -v`.

- [ ] **Step 3: Commit**

```bash
git add pkg/marketdata/binance_multi.go
git commit -m "marketdata: add sequential per-symbol backfill helper"
```

---

## Task 6: Multi-stream WS read loop with demux and reconnect

**Files:**
- Modify: `pkg/marketdata/binance_multi.go`

**Goal of task:** Wire `SubscribeMulti` to actually open the combined WS, run a read loop that calls `routeEnvelope` per message, reconnect on stall (no REST fallback), and close all channels cleanly on context cancel.

- [ ] **Step 1: Implement SubscribeMulti**

Replace the stub `SubscribeMulti` in `pkg/marketdata/binance_multi.go`:

```go
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
	const maxStalls = 5 // higher than single-symbol's 2 — combined stream represents
	// 16 symbols, so giving up means everyone goes blind. Reconnect more aggressively.
	backoff := time.Second
	const maxBackoff = 30 * time.Second

	for {
		if ctx.Err() != nil {
			return
		}

		b.connMu.Lock()
		conn := b.conn
		b.connMu.Unlock()
		if conn == nil {
			// Closed externally — reconnect.
			if !b.reconnect(ctx) {
				return
			}
			continue
		}

		conn.SetReadDeadline(time.Now().Add(wsReadDeadline))
		_, msg, err := conn.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			consecutiveStalls++
			slog.Warn("multi: ws read error",
				"err", err, "stalls", consecutiveStalls, "max", maxStalls)
			if consecutiveStalls >= maxStalls {
				slog.Error("multi: max stalls reached, terminating",
					"stalls", consecutiveStalls)
				return
			}
			// Reconnect.
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
			slog.Info("multi: ws gap closed",
				"duration", time.Since(gapStart).Round(time.Second))
			continue
		}

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
```

- [ ] **Step 2: Verify it builds and existing tests pass**

Run: `go build ./pkg/marketdata/ && go test ./pkg/marketdata/ -v`
Expected: builds, all unit tests pass (Tasks 1-3 tests still green).

- [ ] **Step 3: Commit**

```bash
git add pkg/marketdata/binance_multi.go
git commit -m "marketdata: implement BinanceFuturesMulti subscribe + read loop"
```

---

## Task 7: Zero-frame detection (force reconnect if no data for 60s after connect)

**Files:**
- Modify: `pkg/marketdata/binance_multi.go`

**Goal of task:** The Tokyo zero-frame WS bug means a connection can succeed but never deliver data. Detect this on initial connect and force reconnect (which re-rolls DNS).

- [ ] **Step 1: Add a first-message watchdog**

In `pkg/marketdata/binance_multi.go`, replace the `readLoop` function's outer block to include a "received any message yet" check after the initial dial. Modify the existing `readLoop` to track `firstMessageReceived bool`, and add a goroutine that fires a 60s timer after each (re)connect — if no message has been received by then, log and force reconnect.

Replace `readLoop` with:

```go
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
			// If we never received first msg, this might be the zero-frame
			// case — let the top-of-loop check handle reconnect.
			if !gotFirstMsg {
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
```

- [ ] **Step 2: Verify it builds**

Run: `go build ./pkg/marketdata/ && go test ./pkg/marketdata/ -v`
Expected: builds, tests pass.

- [ ] **Step 3: Commit**

```bash
git add pkg/marketdata/binance_multi.go
git commit -m "marketdata: detect zero-frame WS backends and force reconnect"
```

---

## Task 8: Add --symbols CLI flag to cmd/engine

**Files:**
- Modify: `cmd/engine/main.go`

**Goal of task:** Add the flag. When set, branch to a new `runMulti` function (skeleton in Task 9). Keep single-symbol path identical.

- [ ] **Step 1: Add the flag and branching**

Modify `cmd/engine/main.go`. The multi-mode branch must fire **after** `slog.SetDefault(...)` (currently lines 37-39) but **before** any single-symbol setup (`config.Load`, sideFilter parsing, etc.). After the existing flag declarations (around line 34), add the new flag:

```go
	symbolsCSV := flag.String("symbols", "", "comma-separated list of symbols for multi-symbol mode (e.g. ROSEUSDT,MKRUSDT,GRTUSDT). When set, --config is interpreted as a directory and per-symbol configs are loaded as <symbol-lowercase>.yaml from it.")
```

**After** `slog.SetDefault(...)` and **before** `notifier := notify.FromEnv()`, add the multi-symbol branch:

```go
	if *symbolsCSV != "" {
		symbols := splitCSV(*symbolsCSV)
		if len(symbols) == 0 {
			slog.Error("--symbols is empty")
			os.Exit(1)
		}
		multiFlags := multiFlagSet{
			configDir:        *cfgPath,
			feeBps:           *feeBps,
			stopSlippageBps:  *stopSlippageBps,
			fundingBpsPerDay: *fundingBpsPerDay,
			sideFilter:       *sideFilter,
			maxHoldHours:     *maxHoldHours,
			fundingCSVDir:    *fundingCSVDir,
			targetRROverride: *targetRROverride,
			signalTFOverride: *signalTFOverride,
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		if err := runMulti(ctx, symbols, multiFlags); err != nil {
			slog.Error("multi engine error", "err", err)
			os.Exit(1)
		}
		slog.Info("multi engine stopped")
		return
	}

	// existing single-symbol path follows below, unchanged
```

Add the helper at the bottom of `main.go`:

```go
func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, strings.ToUpper(p))
		}
	}
	return out
}
```

Add `"strings"` to the imports if not already present.

- [ ] **Step 2: Verify it builds**

Run: `go build ./cmd/engine/`
Expected: build fails — `runMulti` and `multiFlagSet` undefined. That is expected; we add them in Task 9.

- [ ] **Step 3: Commit**

(Skip the failing-build commit — Task 9 makes it green. Alternatively, stash these changes until Task 9 is done. For simplicity, do not commit yet; Task 9 will commit both together.)

---

## Task 9: cmd/engine multi-mode setup

**Files:**
- Create: `cmd/engine/multi.go`

**Goal of task:** Implement `runMulti` which loads N configs, opens one BinanceFuturesMulti, fans out to per-symbol goroutine groups (aggregator + strategy + executor + heartbeat). Mirrors the existing single-symbol setup in `main.go` lines 100-210.

- [ ] **Step 1: Create the file**

Create `cmd/engine/multi.go`:

```go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/cristianmanoliu/trading-engine/config"
	"github.com/cristianmanoliu/trading-engine/pkg/aggregator"
	"github.com/cristianmanoliu/trading-engine/pkg/execution"
	"github.com/cristianmanoliu/trading-engine/pkg/funding"
	"github.com/cristianmanoliu/trading-engine/pkg/marketdata"
	"github.com/cristianmanoliu/trading-engine/pkg/models"
	"github.com/cristianmanoliu/trading-engine/pkg/notify"
	"github.com/cristianmanoliu/trading-engine/pkg/strategy"
)

// multiFlagSet bundles the CLI-flag values that runMulti needs. Mirrors the
// flags consumed by the single-symbol path in main.go.
type multiFlagSet struct {
	configDir        string
	feeBps           float64
	stopSlippageBps  float64
	fundingBpsPerDay float64
	sideFilter       string
	maxHoldHours     float64
	fundingCSVDir    string
	targetRROverride float64
	signalTFOverride string
}

// runMulti loads per-symbol configs, opens one combined-streams marketdata
// source, and runs N goroutine groups (aggregator + runner + executor + heartbeat),
// one per symbol, sharing the single WebSocket connection.
//
// Returns when ctx is cancelled or any goroutine group returns an error.
func runMulti(ctx context.Context, symbols []string, flags multiFlagSet) error {
	notifier := notify.FromEnv()

	sideDir, err := parseSideFilter(flags.sideFilter)
	if err != nil {
		return err
	}

	// Load per-symbol configs.
	type symbolSetup struct {
		symbol  string
		cfg     *config.Config
		entry   strategy.EntryConfig
		exec    *execution.Stub
	}

	setups := make([]symbolSetup, 0, len(symbols))
	var firstWSURL, firstRESTURL string
	var firstBackfillHours int

	for _, sym := range symbols {
		cfgPath := filepath.Join(flags.configDir, strings.ToLower(sym)+".yaml")
		cfg, err := config.Load(cfgPath)
		if err != nil {
			return fmt.Errorf("load %s: %w", cfgPath, err)
		}
		if flags.targetRROverride > 0 {
			cfg.Strategy.TargetRR = flags.targetRROverride
		}
		if flags.signalTFOverride != "" {
			cfg.Strategy.SignalTimeframe = flags.signalTFOverride
		}
		if firstWSURL == "" {
			firstWSURL = cfg.Exchange.WSURL
			firstRESTURL = cfg.Exchange.RESTURL
			firstBackfillHours = cfg.Strategy.BackfillHours
		}

		entry := strategy.EntryConfig{
			ProximityPct:      cfg.Strategy.ProximityPct,
			WickRatio:         cfg.Strategy.WickRatio,
			BreakoutBodyRatio: cfg.Strategy.BreakoutBodyRatio,
			AbsorptionCandles: cfg.Strategy.AbsorptionCandles,
			StopBufferPct:     cfg.Strategy.StopBufferPct,
			MinRR:             cfg.Strategy.MinRR,
			TargetRR:          cfg.Strategy.TargetRR,
			MomentumMode:      cfg.Strategy.MomentumMode,
			VWAPDeviationMode: cfg.Strategy.VWAPDeviationMode,
			VWAPDeviationPct:  cfg.Strategy.VWAPDeviationPct,
			EMAMode:           cfg.Strategy.EMAMode,
			ATRStopMult:       cfg.Strategy.ATRStopMult,
			ATRPeriod:         cfg.Strategy.ATRPeriod,
			SignalTimeframe:   models.Timeframe(cfg.Strategy.SignalTimeframe),
			SideFilter:        sideDir,
		}

		journalDir := os.Getenv("PAPER_LIVE_JOURNAL_DIR")
		if journalDir == "" {
			journalDir = "./logs/journal"
		}

		exec := &execution.Stub{
			StakeUSDT:        cfg.Strategy.StakeUSDT,
			JournalPath:      journalDir,
			Symbol:           cfg.Symbol,
			FeeBps:           flags.feeBps,
			StopSlippageBps:  flags.stopSlippageBps,
			FundingBpsPerDay: flags.fundingBpsPerDay,
			MaxHoldHours:     flags.maxHoldHours,
		}
		if flags.fundingCSVDir != "" {
			fp, err := funding.LoadFromDir(flags.fundingCSVDir, cfg.Symbol)
			if err != nil {
				return fmt.Errorf("funding %s: %w", cfg.Symbol, err)
			}
			if fp != nil {
				exec.FundingProvider = fp
				exec.FundingBpsPerDay = 0
				slog.Info("loaded historical funding", "symbol", cfg.Symbol)
			} else {
				slog.Warn("no historical funding for symbol — falling back to constant rate",
					"symbol", cfg.Symbol)
			}
		}

		setups = append(setups, symbolSetup{
			symbol: cfg.Symbol,
			cfg:    cfg,
			entry:  entry,
			exec:   exec,
		})
	}

	hostname, _ := os.Hostname()
	notifier.Send(ctx, fmt.Sprintf("🟢 *multi-engine* started on `%s` with %d symbols", hostname, len(setups))) //nolint:errcheck

	slog.Info("multi engine starting",
		"symbols", len(setups),
		"ws_url", firstWSURL,
		"backfill_hours", firstBackfillHours)

	src := marketdata.NewBinanceFuturesMulti(firstWSURL, firstRESTURL, symbols, firstBackfillHours)
	defer src.Close()

	tickChannels, err := src.SubscribeMulti(ctx)
	if err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}

	g, gctx := errgroup.WithContext(ctx)

	for i := range setups {
		setup := setups[i] // capture
		ticks, ok := tickChannels[setup.symbol]
		if !ok {
			return fmt.Errorf("no tick channel for %s", setup.symbol)
		}

		aggTicks := make(chan models.Tick, 1000)
		stratTicks := make(chan models.Tick, 1000)
		agg := aggregator.New(aggTicks)
		runner := strategy.NewRunner(
			agg.Chan4H(), agg.Chan30m(), agg.Chan5m(),
			agg.Chan1H(), agg.Chan2H(), agg.Chan1D(),
			stratTicks,
			setup.cfg.ModelZones(),
			setup.entry,
			setup.exec,
		)
		hb := marketdata.NewHeartbeat(setup.symbol)

		// Tick fan-out for this symbol.
		g.Go(func() error {
			defer close(aggTicks)
			defer close(stratTicks)
			for {
				select {
				case <-gctx.Done():
					return nil
				case tick, ok := <-ticks:
					if !ok {
						return nil
					}
					hb.Observe(tick)
					select {
					case aggTicks <- tick:
					case <-gctx.Done():
						return nil
					}
					select {
					case stratTicks <- tick:
					case <-gctx.Done():
						return nil
					}
				}
			}
		})

		g.Go(func() error { agg.Run(gctx); return nil })
		g.Go(func() error { runner.Run(gctx); return nil })
		g.Go(func() error { hb.Run(gctx, 60*time.Second); return nil })
	}

	if err := g.Wait(); err != nil {
		return err
	}

	notifier.Send(context.Background(), fmt.Sprintf("🔴 *multi-engine* stopped (clean) on `%s`", hostname)) //nolint:errcheck
	return nil
}

// parseSideFilter validates and converts the --side-filter flag value.
func parseSideFilter(s string) (models.Direction, error) {
	switch s {
	case "both", "":
		return models.Neutral, nil
	case "long":
		return models.Long, nil
	case "short":
		return models.Short, nil
	default:
		return models.Neutral, fmt.Errorf("invalid --side-filter %q (want both | long | short)", s)
	}
}
```

- [ ] **Step 2: Verify it builds**

Run: `go build ./cmd/engine/ && go test ./...`
Expected: builds, all tests pass.

- [ ] **Step 3: Commit (combined with Task 8)**

```bash
git add cmd/engine/main.go cmd/engine/multi.go
git commit -m "engine: add --symbols flag and runMulti for multi-symbol mode"
```

---

## Task 10: Local smoke test of multi engine

**Files:**
- None (manual verification)

**Goal of task:** Run the multi engine locally for ~2 minutes against 3 symbols. Confirm WS connects, ticks arrive for all 3, no panics.

- [ ] **Step 1: Run with 3 symbols, 2-minute timeout**

Run from the repo root:

```bash
go build -o /tmp/engine-multi ./cmd/engine/
PAPER_LIVE_JOURNAL_DIR=/tmp/journal-test \
  timeout 120 /tmp/engine-multi \
  --config configs/ \
  --symbols btcusdt,ethusdt,solusdt \
  --signal-tf 4H --target-rr 6.0 --side-filter short \
  --fee-bps 10 --stop-slippage-bps 5 --max-hold-hours 336 \
  --funding-csv-dir data/funding/ 2>&1 | tee /tmp/multi-smoke.log
```

Expected within 2 minutes:
- Log line `multi engine starting symbols=3`
- 3 × `kline backfill complete` (one per symbol)
- Log line `multi: ws reconnected symbols=3` OR direct successful first message
- 3 × `heartbeat` lines (every 60s) with `ticks_since_last > 0`
- No `panic` or `multi: max stalls reached`

- [ ] **Step 2: Verify each symbol got ticks**

Run:
```bash
for s in BTCUSDT ETHUSDT SOLUSDT; do
  echo "=== $s ==="
  grep -c "\"symbol\":\"$s\"" /tmp/multi-smoke.log
done
```

Expected: each shows ≥ 1 line (heartbeat at minimum).

- [ ] **Step 3: Commit smoke-test log if useful**

If the smoke log is useful as a regression reference, save it:

```bash
cp /tmp/multi-smoke.log docs/plans/2026-05-06-multi-symbol-smoke.log
git add docs/plans/2026-05-06-multi-symbol-smoke.log
git commit -m "engine: smoke-test log for multi-symbol mode (3 symbols, 2 min)"
```

(Skip if log is uninteresting — the plan does not require it.)

---

## Task 11: Production systemd unit

**Files:**
- Create: `deploy/systemd/paper-live-multi.service`

**Goal of task:** Replace the 16 `paper-live@*.service` units with a single `paper-live-multi.service` that runs the new multi engine.

- [ ] **Step 1: Write the unit file**

Create `deploy/systemd/paper-live-multi.service`:

```ini
[Unit]
Description=Paper-live multi-symbol trading engine
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=paperlive
WorkingDirectory=/opt/trading-engine
EnvironmentFile=/etc/paper-live/env
Environment=PAPER_LIVE_JOURNAL_DIR=/var/log/paper-live/journal
# Strategy A (P4-Combined, OOS-validated 2026-05-05).
# 16 honest train-only top-K from deployed-32 (see CLAUDE.md ## Forward-paper go/no-go criteria).
ExecStart=/opt/trading-engine/bin/engine \
    --config /opt/trading-engine/configs/ \
    --symbols ROSEUSDT,MKRUSDT,GRTUSDT,1INCHUSDT,ADAUSDT,KAVAUSDT,1000SHIBUSDT,ENSUSDT,XLMUSDT,IMXUSDT,ETCUSDT,RUNEUSDT,AVAXUSDT,FTMUSDT,DOTUSDT,FILUSDT \
    --signal-tf 4H --target-rr 6.0 \
    --side-filter short --max-hold-hours 336 \
    --funding-csv-dir /opt/trading-engine/data/funding \
    --fee-bps 10 --stop-slippage-bps 5 --funding-bps-per-day 0
StandardOutput=append:/var/log/paper-live/multi.log
StandardError=append:/var/log/paper-live/multi.log
Restart=always
RestartSec=30

[Install]
WantedBy=multi-user.target
```

- [ ] **Step 2: Lint the unit locally**

Run:

```bash
systemd-analyze verify deploy/systemd/paper-live-multi.service 2>&1
```

Expected: no errors. (Warnings about file paths not existing on the local machine are OK — the unit is deployed to the VPS.)

- [ ] **Step 3: Commit**

```bash
git add deploy/systemd/paper-live-multi.service
git commit -m "deploy: add paper-live-multi.service systemd unit"
```

---

## Task 12: Migration script — stop 16 units, deploy multi unit

**Files:**
- Create: `deploy/migrate-to-multi.sh`

**Goal of task:** A safe, idempotent script that switches the VPS from 16 per-symbol units to 1 multi unit.

- [ ] **Step 1: Write the migration script**

Create `deploy/migrate-to-multi.sh`:

```bash
#!/usr/bin/env bash
# migrate-to-multi.sh — switch VPS from 16 paper-live@*.service units to
# the single paper-live-multi.service.
#
# Idempotent: safe to re-run. Verifies each step.
#
# Usage: ./deploy/migrate-to-multi.sh [HOST]
#   HOST defaults to root@178.105.24.230
set -euo pipefail

HOST="${1:-root@178.105.24.230}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"

echo "=== 1. Sync code + new unit file to $HOST ==="
rsync -avz --delete \
  --exclude='data/' --exclude='results/' --exclude='logs/' --exclude='.git/' \
  "$ROOT/" "$HOST:/opt/trading-engine/"
rsync -avz "$ROOT/data/funding/" "$HOST:/opt/trading-engine/data/funding/"

echo ""
echo "=== 2. Rebuild engine binary on VPS ==="
ssh "$HOST" 'cd /opt/trading-engine && go build -o bin/engine ./cmd/engine/'

echo ""
echo "=== 3. Stop old per-symbol units + timers ==="
ssh "$HOST" '
  systemctl stop "paper-live@*.service" || true
  systemctl disable "paper-live@*.service" 2>/dev/null || true
  systemctl stop paper-live-watchdog.timer paper-live-digest.timer || true
'

echo ""
echo "=== 4. Install multi unit ==="
ssh "$HOST" '
  cp /opt/trading-engine/deploy/systemd/paper-live-multi.service /etc/systemd/system/
  systemctl daemon-reload
  systemd-analyze verify /etc/systemd/system/paper-live-multi.service
'

echo ""
echo "=== 5. Start multi engine ==="
ssh "$HOST" 'systemctl enable --now paper-live-multi.service'

echo ""
echo "=== 6. Wait 90s for backfill + first ticks ==="
sleep 90

echo ""
echo "=== 7. Health check ==="
ssh "$HOST" '
  echo "--- service state ---"
  systemctl is-active paper-live-multi.service
  echo ""
  echo "--- recent log (last 30 lines) ---"
  tail -30 /var/log/paper-live/multi.log
  echo ""
  echo "--- ticks per symbol (heartbeat sample) ---"
  for s in ROSEUSDT MKRUSDT GRTUSDT 1INCHUSDT ADAUSDT KAVAUSDT 1000SHIBUSDT ENSUSDT XLMUSDT IMXUSDT ETCUSDT RUNEUSDT AVAXUSDT FTMUSDT DOTUSDT FILUSDT; do
    last_hb=$(grep "\"symbol\":\"$s\"" /var/log/paper-live/multi.log | grep "heartbeat" | tail -1)
    if [ -n "$last_hb" ]; then
      echo "  $s: $(echo $last_hb | grep -oE "\"ticks_since_last\":[0-9]+" | head -1)"
    else
      echo "  $s: NO HEARTBEAT YET"
    fi
  done
'

echo ""
echo "=== 8. Re-enable watchdog/digest timers (point them at multi log) ==="
ssh "$HOST" 'systemctl enable --now paper-live-watchdog.timer paper-live-digest.timer || true'

echo ""
echo "Migration complete."
```

- [ ] **Step 2: Make it executable**

Run: `chmod +x deploy/migrate-to-multi.sh`

- [ ] **Step 3: DO NOT RUN YET** — this is destructive. Operator decides when to execute.

- [ ] **Step 4: Commit**

```bash
git add deploy/migrate-to-multi.sh
git commit -m "deploy: add multi-engine migration script (does not auto-run)"
```

---

## Task 13: Update CLAUDE.md

**Files:**
- Modify: `CLAUDE.md`

**Goal of task:** Document the new architecture, deploy procedure, and updated `## Current state` table.

- [ ] **Step 1: Add a new "## Multi-symbol engine (2026-05-06)" section**

In `CLAUDE.md`, after the `## Forward-paper go/no-go criteria` section, insert:

```markdown
## Multi-symbol engine (2026-05-06)

Replaced the per-symbol `paper-live@*.service` deployment with a single `paper-live-multi.service` running `cmd/engine` in multi-symbol mode.

**Why:** The per-symbol architecture caused the 2026-05-06 9-hour IP ban. Each engine opened its own WebSocket; on Tokyo zero-frame backends, multiple engines fell back to REST aggTrade simultaneously, exceeding the 2400-weight/min cap and triggering 418. Single-WS architecture eliminates the synchronized-fallback failure mode entirely.

**How it works:** `cmd/engine --symbols ROSEUSDT,MKRUSDT,...` opens ONE combined-streams WebSocket (`wss://fstream.binance.com/stream?streams=roseusdt@aggTrade/mkrusdt@aggTrade/...`), demuxes by stream name in `pkg/marketdata/binance_multi.go`, and fans ticks into per-symbol goroutine groups (aggregator + strategy runner + Stub executor + heartbeat). All 16 engines share one TCP connection, one DNS resolution, one IP.

**REST fallback removed.** The single-symbol `BinanceFutures` falls back to per-symbol REST aggTrade polling on stall. The multi version does NOT — it reconnects WS on stall (with explicit zero-frame detection: 60s no-message timeout forces reconnect to re-roll DNS). During sustained Binance WS outages, the multi engine accepts data gaps. The 4H signal timeframe is robust to brief gaps.

**Deploy:**

```bash
./deploy/migrate-to-multi.sh root@178.105.24.230
```

The script: (1) syncs code, (2) rebuilds the binary on the VPS, (3) stops + disables the 16 `paper-live@*.service` units, (4) installs `paper-live-multi.service`, (5) starts it, (6) waits 90s, (7) prints a per-symbol heartbeat health check.

**Rollback:** Re-enable any subset of `paper-live@*.service` units after stopping `paper-live-multi.service`. The single-symbol code path in `cmd/engine/main.go` is unchanged.

**Files added:**
- `pkg/marketdata/binance_multi.go` — `BinanceFuturesMulti` type, combined-streams client
- `pkg/marketdata/binance_multi_test.go` — pure-Go unit tests for envelope, URL, demux
- `cmd/engine/multi.go` — `runMulti` setup
- `deploy/systemd/paper-live-multi.service` — production unit
- `deploy/migrate-to-multi.sh` — migration script
```

- [ ] **Step 2: Update `## Current state` table once the multi engine is deployed**

After the migration runs and the multi engine has been alive for ≥ 1 hour, edit the `## Current state` table's "Live engines (VPS)" row to:

```markdown
| **Live engines (VPS)** | **RUNNING — 1 multi-engine on 16 symbols since YYYY-MM-DD HH:MM UTC.** Single `paper-live-multi.service`. ExecStart per `deploy/systemd/paper-live-multi.service`. Replaced the prior 16 per-symbol units after the 2026-05-06 IP-ban incident — see `## Multi-symbol engine (2026-05-06)`. | journal: `/var/log/paper-live/journal/*.jsonl`, log: `/var/log/paper-live/multi.log` |
```

(Leave the timestamp blank until Task 12's migration completes — fill it in based on actual deploy time.)

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: document multi-symbol engine architecture and deploy"
```

---

## Self-review checklist (run AFTER all tasks above are written)

**1. Spec coverage:**

| Spec requirement | Task |
|---|---|
| Multi-stream WS subscription | Task 4-7 |
| Per-symbol channel demux | Task 3, Task 6 |
| Backfill (per-symbol, sequential) | Task 5 |
| Reconnect on stall | Task 6 |
| Zero-frame detection | Task 7 |
| Drop REST fallback | (architectural — implicit in Tasks 6-7, no per-symbol REST loop spawned) |
| `--symbols` CLI flag | Task 8 |
| Multi-mode engine setup | Task 9 |
| Local smoke test | Task 10 |
| Production systemd unit | Task 11 |
| Migration script | Task 12 |
| CLAUDE.md update | Task 13 |

✓ All spec items have a task.

**2. Placeholder scan:** No `TBD`, `TODO`, "fill in details", or "similar to Task N" in any task body. Every code step shows complete code.

**3. Type consistency:**
- `BinanceFuturesMulti` (Task 4) → used in Task 5, 6, 7, 9 ✓
- `SubscribeMulti` (Task 4) → called in Task 9 ✓
- `routeEnvelope(raw, channels)` (Task 3) → called from `readLoop` in Task 6 with `routeMap` argument that matches the `map[string]chan<- models.Tick` signature ✓
- `combinedEnvelope` (Task 1) → used in `routeEnvelope` (Task 3) ✓
- `buildCombinedURL(base, symbols)` (Task 2) → called from `SubscribeMulti` and `reconnect` in Task 6 ✓
- `multiFlagSet` struct (Task 8) → defined in `cmd/engine/multi.go` (Task 9) ✓
- `runMulti(ctx, symbols, flags)` (Task 8) → defined in Task 9 ✓
- `splitCSV` helper (Task 8) → defined in `cmd/engine/main.go` ✓

✓ Names and signatures consistent across tasks.

---

## Note for executors: import handling

Each task shows a focused snippet of the imports it needs. After every code-writing step, run `goimports -w <file>` (or `go build ./...` and let the compiler tell you what's missing) to ensure the final import block is correct. Do NOT delete imports the previous task added unless you are certain they are unused — the cumulative import block grows across Tasks 1-7.

## Estimated effort

| Phase | Tasks | Hours |
|---|---|---|
| Marketdata multi-stream client | 1-7 | ~6h |
| cmd/engine multi mode | 8-10 | ~3h |
| Deploy + migration | 11-12 | ~2h |
| Documentation | 13 | ~1h |
| Buffer (debugging, smoke-test fixes) | — | ~4h |
| **Total** | | **~16h (2 days)** |
