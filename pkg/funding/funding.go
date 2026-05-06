// Package funding provides historical-funding-rate accrual for paper-trading PnL.
//
// Binance USDT-M perpetuals charge funding every 8h on open notional. The signed
// fundingRate (decimal, e.g. +0.0001 = +0.01% per 8h) is paid by longs to shorts
// when positive, and vice-versa when negative.
//
// Per-side accounting (signed cost convention — positive cost = drag, negative = benefit):
//
//	LONG  cost  = +rate × notional   (pays when rate > 0, receives when rate < 0)
//	SHORT cost  = −rate × notional   (receives when rate > 0, pays when rate < 0)
package funding

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cristianmanoliu/trading-engine/pkg/models"
)

// Provider is the abstraction the executor uses to charge funding cost on a closed trade.
// Returns signed dollars: positive = drag (paid), negative = benefit (received).
type Provider interface {
	ChargeFor(side models.Direction, notional float64, openTime, closeTime time.Time) float64
}

// Constant applies a fixed bps-per-day drag uniformly, regardless of side or regime.
// Used as the simplification when no historical table is available.
type Constant struct {
	BpsPerDay float64
}

// ChargeFor implements Provider. Always-positive cost (drag); side-agnostic.
func (c *Constant) ChargeFor(_ models.Direction, notional float64, openTime, closeTime time.Time) float64 {
	if c.BpsPerDay <= 0 {
		return 0
	}
	hold := closeTime.Sub(openTime).Hours() / 24.0
	if hold <= 0 {
		return 0
	}
	return c.BpsPerDay / 10000.0 * notional * hold
}

// Historical applies actual per-symbol funding rates accrued at each 8h boundary
// the position spans. Per-side sign convention: longs pay positive funding (cost > 0),
// shorts receive positive funding (cost < 0).
type Historical struct {
	symbol string
	times  []time.Time // sorted ascending
	rates  []float64   // signed decimal per 8h period (e.g. 0.0001 = +1bp)
}

// NewHistorical builds a Historical provider from a CSV file with columns:
//
//	funding_time_ms, funding_rate
//
// (header line required). Returns an error if the file is missing or malformed.
func NewHistorical(symbol, csvPath string) (*Historical, error) {
	f, err := os.Open(csvPath)
	if err != nil {
		return nil, fmt.Errorf("open funding csv %s: %w", csvPath, err)
	}
	defer f.Close()

	h := &Historical{symbol: symbol}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	first := true
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if first {
			first = false
			if strings.HasPrefix(line, "funding_time_ms") {
				continue // header
			}
		}
		parts := strings.SplitN(line, ",", 2)
		if len(parts) != 2 {
			continue
		}
		ms, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
		if err != nil {
			continue
		}
		rate, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		if err != nil {
			continue
		}
		h.times = append(h.times, time.UnixMilli(ms).UTC())
		h.rates = append(h.rates, rate)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read funding csv: %w", err)
	}
	if !sort.SliceIsSorted(h.times, func(i, j int) bool { return h.times[i].Before(h.times[j]) }) {
		// Ensure ascending order; Binance sometimes interleaves on pagination boundaries.
		idx := make([]int, len(h.times))
		for i := range idx {
			idx[i] = i
		}
		sort.Slice(idx, func(a, b int) bool { return h.times[idx[a]].Before(h.times[idx[b]]) })
		newT := make([]time.Time, len(h.times))
		newR := make([]float64, len(h.rates))
		for k, i := range idx {
			newT[k] = h.times[i]
			newR[k] = h.rates[i]
		}
		h.times, h.rates = newT, newR
	}
	return h, nil
}

// ChargeFor implements Provider. Sums signed funding cost over events in (openTime, closeTime].
func (h *Historical) ChargeFor(side models.Direction, notional float64, openTime, closeTime time.Time) float64 {
	if len(h.times) == 0 || !openTime.Before(closeTime) {
		return 0
	}
	// First event strictly after openTime.
	i := sort.Search(len(h.times), func(i int) bool { return h.times[i].After(openTime) })
	cost := 0.0
	for ; i < len(h.times); i++ {
		if !h.times[i].Before(closeTime) {
			break
		}
		rate := h.rates[i]
		if side == models.Long {
			cost += rate * notional
		} else {
			cost -= rate * notional
		}
	}
	return cost
}

// LoadFromDir loads `data/funding/<SYMBOL>.csv` for the given symbol and returns a
// Historical provider. Returns nil if the file does not exist (caller may fall back).
func LoadFromDir(dir, symbol string) (*Historical, error) {
	path := filepath.Join(dir, symbol+".csv")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	}
	return NewHistorical(symbol, path)
}
