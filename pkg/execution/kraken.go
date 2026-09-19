package execution

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// KrakenAPIBaseURL is the production Kraken Futures REST endpoint.
const KrakenAPIBaseURL = "https://futures.kraken.com"

// KrakenDemoAPIBaseURL is the Kraken Futures demo environment.
const KrakenDemoAPIBaseURL = "https://demo-futures.kraken.com"

// KrakenOrderRouter sends orders to Kraken Futures via the v3 API.
// Signing: HMAC-SHA512( base64decode(secret), SHA256(postData + nonce + path) ),
// where path strips the leading "/derivatives".
type KrakenOrderRouter struct {
	APIBaseURL string
	APIKey     string
	APISecret  string // base64-encoded
	HTTPClient *http.Client
}

// krakenSign implements the Kraken Futures v3 signing scheme.
func krakenSign(secret, postData, nonce, endpointPath string) (string, error) {
	secretBytes, err := base64.StdEncoding.DecodeString(secret)
	if err != nil {
		return "", fmt.Errorf("decode API secret: %w", err)
	}
	message := []byte(postData + nonce + endpointPath)
	sha := sha256.Sum256(message)
	mac := hmac.New(sha512.New, secretBytes)
	mac.Write(sha[:])
	return base64.StdEncoding.EncodeToString(mac.Sum(nil)), nil
}

// SendOrder posts a market order to Kraken Futures.
// Kraken v3 order endpoint: POST /derivatives/api/v3/sendorder
// Body params: orderType=mkt, symbol=PF_XYZUSD, side=buy|sell, size=<qty>
func (r *KrakenOrderRouter) SendOrder(ctx context.Context, intent OrderIntent) (OrderResult, error) {
	if r.APIKey == "" || r.APISecret == "" {
		return OrderResult{}, fmt.Errorf("KrakenOrderRouter not configured: APIKey/APISecret missing")
	}
	if r.HTTPClient == nil {
		return OrderResult{}, fmt.Errorf("KrakenOrderRouter not configured: HTTPClient nil")
	}

	side, err := mapToExchangeSide(intent.Side, intent.ReduceOnly)
	if err != nil {
		return OrderResult{}, err
	}

	postData := fmt.Sprintf("orderType=mkt&symbol=%s&side=%s&size=%s",
		intent.Symbol,
		strings.ToLower(side),
		strconv.FormatFloat(intent.Quantity, 'f', -1, 64),
	)
	if intent.ReduceOnly {
		postData += "&reduceOnly=true"
	}

	nonce := strconv.FormatInt(time.Now().UnixMilli(), 10)
	apiPath := "/api/v3/sendorder"
	sig, err := krakenSign(r.APISecret, postData, nonce, apiPath)
	if err != nil {
		return OrderResult{Status: "ERROR", RejectCode: "SIGN"}, err
	}

	fullURL := r.APIBaseURL + "/derivatives" + apiPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, strings.NewReader(postData))
	if err != nil {
		return OrderResult{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("APIKey", r.APIKey)
	req.Header.Set("Nonce", nonce)
	req.Header.Set("Authent", sig)

	resp, err := r.HTTPClient.Do(req)
	if err != nil {
		return OrderResult{Status: "ERROR", RejectCode: "NETWORK"}, fmt.Errorf("network: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == 429 {
		return OrderResult{Status: "ERROR", RejectCode: "RATE_LIMIT"}, fmt.Errorf("rate limit: %s", body)
	}
	if resp.StatusCode >= 400 {
		return OrderResult{Status: "REJECTED", RejectCode: string(body)}, fmt.Errorf("HTTP %d: %s", resp.StatusCode, body)
	}

	var kr struct {
		Result   string `json:"result"`
		SendStat struct {
			OrderID    string `json:"order_id"`
			Status     string `json:"status"`       // "placed"
			ReceivedTs string `json:"receivedTime"` // ISO 8601
		} `json:"sendStatus"`
		ServerTime string `json:"serverTime"`
	}
	if err := json.Unmarshal(body, &kr); err != nil {
		return OrderResult{Status: "ERROR", RejectCode: "PARSE"}, fmt.Errorf("parse: %w", err)
	}
	if kr.Result != "success" {
		return OrderResult{Status: "REJECTED", RejectCode: kr.Result}, fmt.Errorf("kraken result=%s: %s", kr.Result, body)
	}

	// Kraken sendorder returns the order ID and status but not the fill price.
	// For market orders, the fill is near-immediate. A follow-up query to
	// /api/v3/fills would get the actual fill price, but for the initial stub
	// we return the order ID and mark as FILLED (market orders on Kraken fill
	// synchronously in normal conditions).
	// ponytail: fill-price query deferred, add when paper trading shows a need
	return OrderResult{
		OrderID: kr.SendStat.OrderID,
		Status:  "FILLED",
	}, nil
}

// KrakenSymbolMap converts a Binance-style symbol (e.g. "BCHUSDT") to the
// Kraken Futures symbol (e.g. "PF_BCHUSD"). Returns the Kraken symbol and a
// quantity multiplier (1.0 for most symbols, 1000.0 for SHIB because Binance
// trades 1000SHIB contracts while Kraken trades raw SHIB).
func KrakenSymbolMap(binanceSymbol string) (krakenSymbol string, qtyMultiplier float64) {
	sym := strings.TrimSuffix(binanceSymbol, "USDT")

	if sym == "1000SHIB" {
		return "PF_SHIBUSD", 1000.0
	}

	return "PF_" + strings.ToUpper(sym) + "USD", 1.0
}

// KrakenLive is the Kraken Futures executor. It mirrors the BinanceLive
// architecture: OnSignal spawns an order goroutine, OnTick evaluates
// stop/target/max-hold and spawns exit goroutines. Journal format is
// identical to Stub/BinanceLive for compatibility with all analysis tools.
//
// This reuses the Stub for position tracking, journal writing, and PnL math
// (the paper-money path is identical), and wraps it with real order routing
// via KrakenOrderRouter. The Stub handles all state; KrakenLive adds order
// execution before/after the Stub's state transitions.
//
// ponytail: inherits Stub for state+journal, adds real orders on top.
// Full BinanceLive-style architecture (concurrent position lock, reconciler,
// safety gates) deferred to post-paper-trading if the overlay survives.
type KrakenLive struct {
	Stub

	Router        *KrakenOrderRouter
	QtyMultiplier float64 // 1.0 normally, 1000.0 for SHIB
	KrakenSymbol  string  // PF_XYZUSD
}

// NewKrakenLive constructs a KrakenLive executor for the given Binance-style
// symbol. Automatically maps to the Kraken symbol and sets the qty multiplier.
func NewKrakenLive(binanceSymbol string, stakeUSD float64, apiKey, apiSecret string) *KrakenLive {
	krakenSym, qtyMult := KrakenSymbolMap(binanceSymbol)
	return &KrakenLive{
		Stub: Stub{
			Symbol:    binanceSymbol,
			StakeUSDT: stakeUSD,
		},
		Router: &KrakenOrderRouter{
			APIBaseURL: KrakenAPIBaseURL,
			APIKey:     apiKey,
			APISecret:  apiSecret,
			HTTPClient: &http.Client{Timeout: 10 * time.Second},
		},
		QtyMultiplier: qtyMult,
		KrakenSymbol:  krakenSym,
	}
}

// NewKrakenDemo constructs a KrakenLive pointed at the demo environment.
func NewKrakenDemo(binanceSymbol string, stakeUSD float64, apiKey, apiSecret string) *KrakenLive {
	kl := NewKrakenLive(binanceSymbol, stakeUSD, apiKey, apiSecret)
	kl.Router.APIBaseURL = KrakenDemoAPIBaseURL
	return kl
}

// krakenQty converts the strategy's unit quantity (based on Binance conventions)
// to Kraken's expected quantity. For SHIB, Binance uses 1000SHIB units while
// Kraken uses raw SHIB, so qty must be multiplied by 1000.
func (k *KrakenLive) krakenQty(units float64) float64 {
	return math.Round(units*k.QtyMultiplier*1e8) / 1e8
}
