package marketdata

import (
	"encoding/json"
	"strings"
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
