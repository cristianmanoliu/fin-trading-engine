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
