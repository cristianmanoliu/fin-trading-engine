package main

import (
	"os"
	"path/filepath"
	"testing"
)

// writeJSONL writes lines to a temp file and returns its path.
func writeJSONL(t *testing.T, dir, name string, lines []string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	for _, l := range lines {
		f.WriteString(l + "\n")
	}
	f.Close()
	return path
}

func TestLoadCohort_Empty(t *testing.T) {
	dir := t.TempDir()
	c, err := loadCohort("live", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.TotalTrades != 0 {
		t.Errorf("expected 0 trades, got %d", c.TotalTrades)
	}
	if len(c.Opens) != 0 {
		t.Errorf("expected 0 opens, got %d", len(c.Opens))
	}
}

func TestLoadCohort_MalformedLines(t *testing.T) {
	dir := t.TempDir()
	writeJSONL(t, dir, "BTCUSDT-2026-05.jsonl", []string{
		`{"event":"open","symbol":"BTCUSDT","ts":"2026-05-10T12:00:00Z","side":"SHORT","entry":60000,"stop":60300,"target":58200,"reason":"test"}`,
		`not json at all`,
		`{"broken":`,
		`{"event":"close","symbol":"BTCUSDT","ts":"2026-05-11T08:00:00Z","side":"SHORT","entry":60000,"exit":58200,"stop":60300,"target":58200,"pnl_usd":500,"outcome":"TARGET","reason":"test","notional_usd":200000,"fee_usd":20}`,
	})
	c, err := loadCohort("live", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Malformed lines must be skipped; 1 terminal close should be counted.
	if c.TotalTrades != 1 {
		t.Errorf("expected 1 trade, got %d", c.TotalTrades)
	}
	if c.Wins != 1 {
		t.Errorf("expected 1 win, got %d", c.Wins)
	}
}

func TestLoadCohort_OpenPosition(t *testing.T) {
	dir := t.TempDir()
	writeJSONL(t, dir, "ETHUSDT-2026-05.jsonl", []string{
		`{"event":"open","symbol":"ETHUSDT","ts":"2026-05-15T10:00:00Z","side":"SHORT","entry":3000,"stop":3030,"target":2820,"reason":"test"}`,
	})
	c, err := loadCohort("live", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(c.Opens) != 1 {
		t.Fatalf("expected 1 open position, got %d", len(c.Opens))
	}
	p := c.Opens[0]
	if p.Symbol != "ETHUSDT" {
		t.Errorf("symbol: got %q, want ETHUSDT", p.Symbol)
	}
	if p.Side != "SHORT" {
		t.Errorf("side: got %q, want SHORT", p.Side)
	}
}

func TestLoadCohort_CrossMonthRecovery(t *testing.T) {
	// Open in April, close in May — must not show as open.
	dir := t.TempDir()
	writeJSONL(t, dir, "SOLUSDT-2026-04.jsonl", []string{
		`{"event":"open","symbol":"SOLUSDT","ts":"2026-04-28T08:00:00Z","side":"SHORT","entry":150,"stop":151.5,"target":141,"reason":"test"}`,
	})
	writeJSONL(t, dir, "SOLUSDT-2026-05.jsonl", []string{
		`{"event":"close","symbol":"SOLUSDT","ts":"2026-05-02T14:00:00Z","side":"SHORT","entry":150,"exit":141,"stop":151.5,"target":141,"pnl_usd":300,"outcome":"TARGET","reason":"test","notional_usd":100000,"fee_usd":10}`,
	})
	c, err := loadCohort("live", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(c.Opens) != 0 {
		t.Errorf("expected 0 open positions after cross-month close, got %d", len(c.Opens))
	}
	if c.TotalTrades != 1 {
		t.Errorf("expected 1 terminal trade, got %d", c.TotalTrades)
	}
}

func TestLoadCohort_MetricsAggregation(t *testing.T) {
	dir := t.TempDir()
	writeJSONL(t, dir, "BNBUSDT-2026-05.jsonl", []string{
		`{"event":"open","symbol":"BNBUSDT","ts":"2026-05-01T00:00:00Z","side":"SHORT","entry":400,"stop":404,"target":376,"reason":"test"}`,
		`{"event":"close","symbol":"BNBUSDT","ts":"2026-05-02T00:00:00Z","outcome":"TARGET","pnl_usd":600,"fee_usd":10,"slip_usd":0,"notional_usd":100000,"symbol":"BNBUSDT","ts":"2026-05-02T00:00:00Z","side":"SHORT","entry":400,"exit":376,"stop":404,"target":376,"reason":"test"}`,
		`{"event":"open","symbol":"BNBUSDT","ts":"2026-05-03T00:00:00Z","side":"SHORT","entry":410,"stop":414.1,"target":385.4,"reason":"test"}`,
		`{"event":"close","symbol":"BNBUSDT","ts":"2026-05-04T00:00:00Z","outcome":"STOP","pnl_usd":-100,"fee_usd":10,"slip_usd":5,"notional_usd":100000,"symbol":"BNBUSDT","ts":"2026-05-04T00:00:00Z","side":"SHORT","entry":410,"exit":414.1,"stop":414.1,"target":385.4,"reason":"test"}`,
	})
	c, err := loadCohort("live", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.TotalTrades != 2 {
		t.Errorf("total trades: got %d, want 2", c.TotalTrades)
	}
	if c.Wins != 1 {
		t.Errorf("wins: got %d, want 1", c.Wins)
	}
	want := 600.0 - 100.0
	if c.NetPnL != want {
		t.Errorf("net pnl: got %.2f, want %.2f", c.NetPnL, want)
	}
	if c.NotionalLosers != 100000 {
		t.Errorf("notional losers: got %.0f, want 100000", c.NotionalLosers)
	}
	if c.SlipUSDLosers != 5 {
		t.Errorf("slip losers: got %.2f, want 5", c.SlipUSDLosers)
	}
}

func TestLoadCohort_EquityCurve(t *testing.T) {
	dir := t.TempDir()
	writeJSONL(t, dir, "ADAUSDT-2026-05.jsonl", []string{
		`{"event":"open","symbol":"ADAUSDT","ts":"2026-05-01T00:00:00Z","side":"SHORT","entry":0.4,"stop":0.404,"target":0.376,"reason":"test"}`,
		`{"event":"close","symbol":"ADAUSDT","ts":"2026-05-02T00:00:00Z","outcome":"TARGET","pnl_usd":200,"symbol":"ADAUSDT","side":"SHORT","entry":0.4,"exit":0.376,"stop":0.404,"target":0.376,"reason":"test"}`,
		`{"event":"open","symbol":"ADAUSDT","ts":"2026-05-03T00:00:00Z","side":"SHORT","entry":0.41,"stop":0.414,"target":0.386,"reason":"test"}`,
		`{"event":"close","symbol":"ADAUSDT","ts":"2026-05-04T00:00:00Z","outcome":"STOP","pnl_usd":-50,"symbol":"ADAUSDT","side":"SHORT","entry":0.41,"exit":0.414,"stop":0.414,"target":0.386,"reason":"test"}`,
	})
	c, err := loadCohort("live", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(c.EquityCurve) != 2 {
		t.Fatalf("equity curve length: got %d, want 2", len(c.EquityCurve))
	}
	if c.EquityCurve[0] != 200 {
		t.Errorf("curve[0]: got %.0f, want 200", c.EquityCurve[0])
	}
	if c.EquityCurve[1] != 150 {
		t.Errorf("curve[1]: got %.0f, want 150", c.EquityCurve[1])
	}
}

func TestGates_WaitingBeforeFloor(t *testing.T) {
	c := &Cohort{
		TotalTrades:  5,
		Wins:         1,
		SymbolPnL:    map[string]float64{"BTC": -100},
		SymbolTrades: map[string]int{"BTC": 5},
	}
	gates := c.Gates()
	// With only 5 trades + 0 days, most gates should be PENDING (insufficient data).
	tradeGate := gates[0]
	if tradeGate.Pass {
		t.Error("trades gate should not pass at n=5")
	}
}

func TestLoadState_ShadowSubdirs(t *testing.T) {
	dir := t.TempDir()
	shadowDir := filepath.Join(dir, "shadow", "alt5-15-336")
	os.MkdirAll(shadowDir, 0755)
	writeJSONL(t, shadowDir, "BTCUSDT-2026-05.jsonl", []string{
		`{"event":"open","symbol":"BTCUSDT","ts":"2026-05-10T00:00:00Z","side":"SHORT","entry":60000,"stop":60300,"target":58200,"reason":"test"}`,
		`{"event":"close","symbol":"BTCUSDT","ts":"2026-05-11T00:00:00Z","outcome":"TARGET","pnl_usd":1200,"symbol":"BTCUSDT","side":"SHORT","entry":60000,"exit":58200,"stop":60300,"target":58200,"reason":"test"}`,
	})
	st, err := LoadState(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(st.Shadows) != 1 {
		t.Fatalf("expected 1 shadow, got %d", len(st.Shadows))
	}
	if st.Shadows[0].TotalTrades != 1 {
		t.Errorf("shadow trades: got %d, want 1", st.Shadows[0].TotalTrades)
	}
}
