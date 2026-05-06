package marketdata

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cristianmanoliu/trading-engine/pkg/models"
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
