package funding

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cristianmanoliu/trading-engine/pkg/models"
)

func TestConstantProvider(t *testing.T) {
	p := &Constant{BpsPerDay: 3}
	open := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	close := open.Add(48 * time.Hour) // 2 days
	cost := p.ChargeFor(models.Long, 100000, open, close)
	want := 3.0 / 10000.0 * 100000 * 2.0 // 60
	if math.Abs(cost-want) > 1e-9 {
		t.Errorf("cost=%v want=%v", cost, want)
	}
	// Constant is side-agnostic — same charge for SHORT.
	costS := p.ChargeFor(models.Short, 100000, open, close)
	if costS != cost {
		t.Errorf("expected same charge regardless of side, got long=%v short=%v", cost, costS)
	}
}

func writeCSV(t *testing.T, rows [][2]string) string {
	dir := t.TempDir()
	path := filepath.Join(dir, "TEST.csv")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString("funding_time_ms,funding_rate\n"); err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if _, err := f.WriteString(r[0] + "," + r[1] + "\n"); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func TestHistoricalProvider_PerSideSign(t *testing.T) {
	// Three funding events: +0.0001, +0.0001, -0.0002 (signed decimal, per 8h period)
	t1 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	t2 := time.Date(2024, 1, 1, 8, 0, 0, 0, time.UTC).UnixMilli()
	t3 := time.Date(2024, 1, 1, 16, 0, 0, 0, time.UTC).UnixMilli()
	path := writeCSV(t, [][2]string{
		{itoa(t1), "0.0001"},
		{itoa(t2), "0.0001"},
		{itoa(t3), "-0.0002"},
	})
	h, err := NewHistorical("TEST", path)
	if err != nil {
		t.Fatal(err)
	}

	// Position open from 2023-12-31 23:00 UTC to 2024-01-01 23:00 UTC (24h, spans all three events).
	open := time.Date(2023, 12, 31, 23, 0, 0, 0, time.UTC)
	close := time.Date(2024, 1, 1, 23, 0, 0, 0, time.UTC)
	notional := 100000.0

	// LONG: pays 0.0001 + 0.0001 + (-0.0002) = 0.0 net rate × 100k = $0
	costLong := h.ChargeFor(models.Long, notional, open, close)
	if math.Abs(costLong-0.0) > 1e-6 {
		t.Errorf("LONG cost: want 0, got %v", costLong)
	}

	// Different test: only first two events (positive funding)
	close2 := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC) // includes events at t1 and t2 but not t3
	costLong2 := h.ChargeFor(models.Long, notional, open, close2)
	want2 := 0.0001 * 100000 * 2 // $20
	if math.Abs(costLong2-want2) > 1e-6 {
		t.Errorf("LONG cost (2 events): want %v, got %v", want2, costLong2)
	}
	// SHORT receives the same in opposite sign.
	costShort2 := h.ChargeFor(models.Short, notional, open, close2)
	if math.Abs(costShort2-(-want2)) > 1e-6 {
		t.Errorf("SHORT cost (2 events): want %v, got %v", -want2, costShort2)
	}
}

func TestHistoricalProvider_BoundaryExclusion(t *testing.T) {
	// Verify funding events strictly between (open, close) are counted; equal to boundary not.
	t1 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	path := writeCSV(t, [][2]string{{itoa(t1), "0.0001"}})
	h, err := NewHistorical("TEST", path)
	if err != nil {
		t.Fatal(err)
	}
	notional := 100000.0
	rate := 0.0001

	// Position spans the event: should charge.
	open := time.Date(2023, 12, 31, 23, 0, 0, 0, time.UTC)
	close := time.Date(2024, 1, 1, 1, 0, 0, 0, time.UTC)
	cost := h.ChargeFor(models.Long, notional, open, close)
	if math.Abs(cost-rate*notional) > 1e-6 {
		t.Errorf("spanning event: want %v, got %v", rate*notional, cost)
	}

	// Position ends BEFORE the event: should NOT charge.
	close2 := time.Date(2023, 12, 31, 23, 30, 0, 0, time.UTC)
	cost2 := h.ChargeFor(models.Long, notional, open, close2)
	if cost2 != 0 {
		t.Errorf("ending before event: want 0, got %v", cost2)
	}
}

// quotedRateCSV writes a fixture using Binance's actual export format where
// the funding_rate field is double-quoted (e.g. `"-0.00012359"`). The pre-fix
// loader's strconv.ParseFloat call rejected these as invalid syntax and silently
// skipped every row, leaving the table empty and producing $0 funding for all
// trades regardless of regime. This regression test pins the behaviour: the
// loader must strip surrounding quotes before parsing.
func quotedRateCSV(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "TESTUSDT.csv")
	body := "funding_time_ms,funding_rate\n" +
		`1577836800000,"-0.00012359"` + "\n" +
		`1577865600000,"0.00010000"` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func TestNewHistorical_ParsesQuotedRates(t *testing.T) {
	h, err := NewHistorical("TESTUSDT", quotedRateCSV(t))
	if err != nil {
		t.Fatalf("NewHistorical: %v", err)
	}
	if got := len(h.times); got != 2 {
		t.Fatalf("expected 2 funding events parsed, got %d (regression: quoted-rate skip)", got)
	}
	if h.rates[0] != -0.00012359 {
		t.Errorf("rate[0]: got %v, want -0.00012359", h.rates[0])
	}
	if h.rates[1] != 0.00010000 {
		t.Errorf("rate[1]: got %v, want 0.00010000", h.rates[1])
	}
}

func TestHistorical_RateAt(t *testing.T) {
	h, err := NewHistorical("TESTUSDT", quotedRateCSV(t))
	if err != nil {
		t.Fatalf("NewHistorical: %v", err)
	}
	// Event 1 at 2020-01-01 00:00:00 UTC, rate -0.00012359
	// Event 2 at 2020-01-01 08:00:00 UTC, rate +0.00010000
	cases := []struct {
		name string
		ts   time.Time
		want float64
	}{
		{"before any event returns 0", time.Date(2019, 12, 31, 23, 0, 0, 0, time.UTC), 0},
		{"between events returns first rate", time.Date(2020, 1, 1, 4, 0, 0, 0, time.UTC), -0.00012359},
		{"after second event returns second rate", time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC), 0.00010000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := h.RateAt(tc.ts); got != tc.want {
				t.Errorf("RateAt(%v): got %v, want %v", tc.ts, got, tc.want)
			}
		})
	}
}

// TestHistorical_QuotedCSV_EndToEnd is the end-to-end check: a position spanning
// a funding event in a quoted-rate CSV must produce non-zero funding cost.
// Before the fix this returned 0 silently, masking the bug across the codebase.
func TestHistorical_QuotedCSV_EndToEnd(t *testing.T) {
	h, err := NewHistorical("TESTUSDT", quotedRateCSV(t))
	if err != nil {
		t.Fatalf("NewHistorical: %v", err)
	}
	openT := time.Date(2020, 1, 1, 0, 0, 1, 0, time.UTC) // strictly after event 1
	closeT := time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC) // covers event 2 only
	notional := 100000.0
	cost := h.ChargeFor(models.Long, notional, openT, closeT)
	if cost == 0 {
		t.Fatalf("ChargeFor returned 0 for span covering an event — quoted CSV not parsed (regression)")
	}
	want := 0.00010000 * notional // single event between (openT, closeT]
	if math.Abs(cost-want) > 1e-6 {
		t.Errorf("ChargeFor: got %v, want %v", cost, want)
	}
}

func itoa(n int64) string {
	const digits = "0123456789"
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = digits[n%10]
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
