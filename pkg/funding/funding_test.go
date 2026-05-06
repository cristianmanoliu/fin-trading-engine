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
