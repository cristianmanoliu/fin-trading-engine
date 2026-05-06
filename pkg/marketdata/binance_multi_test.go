package marketdata

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/cristianmanoliu/trading-engine/pkg/models"
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
