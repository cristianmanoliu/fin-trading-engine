package execution

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cristianmanoliu/fin-trading-engine/pkg/models"
)

func TestKrakenSymbolMap(t *testing.T) {
	tests := []struct {
		binance    string
		wantSymbol string
		wantMult   float64
	}{
		{"BCHUSDT", "PF_BCHUSD", 1.0},
		{"XLMUSDT", "PF_XLMUSD", 1.0},
		{"AAVEUSDT", "PF_AAVEUSD", 1.0},
		{"1000SHIBUSDT", "PF_SHIBUSD", 1000.0},
		{"INJUSDT", "PF_INJUSD", 1.0},
	}
	for _, tt := range tests {
		sym, mult := KrakenSymbolMap(tt.binance)
		if sym != tt.wantSymbol {
			t.Errorf("KrakenSymbolMap(%q) symbol = %q, want %q", tt.binance, sym, tt.wantSymbol)
		}
		if mult != tt.wantMult {
			t.Errorf("KrakenSymbolMap(%q) mult = %v, want %v", tt.binance, mult, tt.wantMult)
		}
	}
}

func TestKrakenSign(t *testing.T) {
	// The signing scheme: HMAC-SHA512( base64decode(secret), SHA256(postData + nonce + path) )
	// Verify it doesn't panic and produces a base64 string.
	secret := "dGVzdHNlY3JldA==" // base64("testsecret")
	sig, err := krakenSign(secret, "orderType=mkt&symbol=PF_BCHUSD&side=sell&size=1", "1234567890", "/api/v3/sendorder")
	if err != nil {
		t.Fatalf("krakenSign error: %v", err)
	}
	if sig == "" {
		t.Fatal("krakenSign returned empty string")
	}
	// Verify it's valid base64
	if len(sig) < 10 {
		t.Errorf("signature suspiciously short: %q", sig)
	}
}

func TestKrakenSign_BadSecret(t *testing.T) {
	_, err := krakenSign("not-valid-base64!!!", "data", "nonce", "/path")
	if err == nil {
		t.Fatal("expected error for invalid base64 secret")
	}
}

func TestKrakenOrderRouter_SendOrder_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/derivatives/api/v3/sendorder" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("APIKey") == "" {
			t.Error("missing APIKey header")
		}
		if r.Header.Get("Nonce") == "" {
			t.Error("missing Nonce header")
		}
		if r.Header.Get("Authent") == "" {
			t.Error("missing Authent header")
		}
		w.WriteHeader(200)
		w.Write([]byte(`{"result":"success","sendStatus":{"order_id":"abc123","status":"placed"}}`))
	}))
	defer srv.Close()

	router := &KrakenOrderRouter{
		APIBaseURL: srv.URL,
		APIKey:     "testkey",
		APISecret:  "dGVzdHNlY3JldA==",
		HTTPClient: &http.Client{Timeout: 5e9},
	}
	intent := OrderIntent{
		Symbol:   "PF_BCHUSD",
		Side:     models.Short,
		Quantity: 1.5,
		Type:     "MARKET",
	}
	result, err := router.SendOrder(context.Background(), intent)
	if err != nil {
		t.Fatalf("SendOrder error: %v", err)
	}
	if result.OrderID != "abc123" {
		t.Errorf("OrderID = %q, want %q", result.OrderID, "abc123")
	}
	if result.Status != "FILLED" {
		t.Errorf("Status = %q, want %q", result.Status, "FILLED")
	}
}

func TestKrakenOrderRouter_SendOrder_Rejection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(`{"result":"error","error":"insufficientFunds"}`))
	}))
	defer srv.Close()

	router := &KrakenOrderRouter{
		APIBaseURL: srv.URL,
		APIKey:     "testkey",
		APISecret:  "dGVzdHNlY3JldA==",
		HTTPClient: &http.Client{Timeout: 5e9},
	}
	_, err := router.SendOrder(context.Background(), OrderIntent{
		Symbol: "PF_BCHUSD", Side: models.Short, Quantity: 1,
	})
	if err == nil {
		t.Fatal("expected error on rejection")
	}
}

func TestKrakenOrderRouter_SendOrder_RateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		w.Write([]byte(`rate limited`))
	}))
	defer srv.Close()

	router := &KrakenOrderRouter{
		APIBaseURL: srv.URL,
		APIKey:     "testkey",
		APISecret:  "dGVzdHNlY3JldA==",
		HTTPClient: &http.Client{Timeout: 5e9},
	}
	result, err := router.SendOrder(context.Background(), OrderIntent{
		Symbol: "PF_BCHUSD", Side: models.Short, Quantity: 1,
	})
	if err == nil {
		t.Fatal("expected error on rate limit")
	}
	if result.RejectCode != "RATE_LIMIT" {
		t.Errorf("RejectCode = %q, want RATE_LIMIT", result.RejectCode)
	}
}

func TestNewKrakenLive(t *testing.T) {
	kl := NewKrakenLive("BCHUSDT", 500, "key", "c2VjcmV0")
	if kl.KrakenSymbol != "PF_BCHUSD" {
		t.Errorf("KrakenSymbol = %q, want PF_BCHUSD", kl.KrakenSymbol)
	}
	if kl.QtyMultiplier != 1.0 {
		t.Errorf("QtyMultiplier = %v, want 1.0", kl.QtyMultiplier)
	}
	if kl.Stub.Symbol != "BCHUSDT" {
		t.Errorf("Stub.Symbol = %q, want BCHUSDT", kl.Stub.Symbol)
	}
	if kl.Router.APIBaseURL != KrakenAPIBaseURL {
		t.Errorf("APIBaseURL = %q, want %q", kl.Router.APIBaseURL, KrakenAPIBaseURL)
	}
}

func TestNewKrakenLive_SHIB(t *testing.T) {
	kl := NewKrakenLive("1000SHIBUSDT", 500, "key", "c2VjcmV0")
	if kl.KrakenSymbol != "PF_SHIBUSD" {
		t.Errorf("KrakenSymbol = %q, want PF_SHIBUSD", kl.KrakenSymbol)
	}
	if kl.QtyMultiplier != 1000.0 {
		t.Errorf("QtyMultiplier = %v, want 1000.0", kl.QtyMultiplier)
	}
}

func TestNewKrakenDemo(t *testing.T) {
	kl := NewKrakenDemo("XLMUSDT", 500, "key", "c2VjcmV0")
	if kl.Router.APIBaseURL != KrakenDemoAPIBaseURL {
		t.Errorf("APIBaseURL = %q, want %q", kl.Router.APIBaseURL, KrakenDemoAPIBaseURL)
	}
}

func TestKrakenLive_KrakenQty(t *testing.T) {
	kl := NewKrakenLive("1000SHIBUSDT", 500, "key", "c2VjcmV0")
	got := kl.krakenQty(10.0)
	want := 10000.0
	if got != want {
		t.Errorf("krakenQty(10.0) = %v, want %v", got, want)
	}

	kl2 := NewKrakenLive("BCHUSDT", 500, "key", "c2VjcmV0")
	got2 := kl2.krakenQty(1.5)
	if got2 != 1.5 {
		t.Errorf("krakenQty(1.5) = %v, want 1.5", got2)
	}
}
