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
