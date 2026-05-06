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
