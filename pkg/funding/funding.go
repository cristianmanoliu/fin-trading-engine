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

	"github.com/cristianmanoliu/fin-trading-engine/pkg/models"
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
	// Track non-empty, non-header rows seen vs successfully parsed rows.
	// The 2026-05-06 funding-loader bug had every row silently fail
	// ParseFloat (Binance quoted the rate, our loader didn't strip quotes),
	// returning an empty table and producing $0 funding for ALL trades.
	// A future CSV-schema change (new column, different separator, etc.)
	// could re-trip the same shape: every row skipped silently, empty
	// table returned with no error. Distinguish "file had 0 data rows
	// (legitimately empty)" from "file had N rows but every one failed
	// to parse (something changed)".
	dataRows := 0
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
		dataRows++
		parts := strings.SplitN(line, ",", 2)
		if len(parts) != 2 {
			continue
		}
		ms, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
		if err != nil {
			continue
		}
		// Binance CSV exports quote the funding_rate value (e.g. "-0.00012359").
		// Strip surrounding double quotes before parsing — without this fix every
		// row silently fails ParseFloat and the loader returns an empty table,
		// producing $0 funding for all trades.
		rateStr := strings.Trim(strings.TrimSpace(parts[1]), `"`)
		rate, err := strconv.ParseFloat(rateStr, 64)
		if err != nil {
			continue
		}
		h.times = append(h.times, time.UnixMilli(ms).UTC())
		h.rates = append(h.rates, rate)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read funding csv: %w", err)
	}
	if dataRows > 0 && len(h.times) == 0 {
		// File had data rows but none parsed — schema change or corruption.
		// Refuse rather than silently return an empty table. This is the
		// load-time analog of the regression that hid the funding-loader
		// bug for months.
		return nil, fmt.Errorf("funding csv %s: %d data rows but 0 parsed — likely schema change",
			csvPath, dataRows)
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

// RateAt returns the per-8h funding rate of the most recent funding event
// at or before t. Returns 0 if t precedes the first event in the table.
// Used by strategy-side funding filters that need to gate entries on the
// prevailing funding regime at signal time.
func (h *Historical) RateAt(t time.Time) float64 {
	if len(h.times) == 0 {
		return 0
	}
	i := sort.Search(len(h.times), func(i int) bool { return h.times[i].After(t) })
	if i == 0 {
		return 0
	}
	return h.rates[i-1]
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
// Historical provider. Returns (nil, nil) if the dir exists but the per-symbol
// file is missing — legit "no funding history for this symbol, caller may fall
// back to a default rate."
//
// FD-2: previously `os.IsNotExist` fired on both "dir doesn't exist" (operator
// typo'd the --funding-csv-dir flag) AND "symbol file missing in valid dir"
// (legit fallback case), returning (nil, nil) for both. cmd/backtest's
// `if fp != nil` then routed both to a slog.Warn + constant-rate fallback —
// operator who typo'd a path got a silent run with WRONG funding model,
// and locked the resulting verdict into results/. Same shape as the
// 022098b silent-zero bug, just at a different layer.
//
// Fix: validate dir existence explicitly. A missing dir is operator
// misconfig → return error (caller exits 1). A missing symbol file
// inside a valid dir stays as the legitimate fallback signal.
func LoadFromDir(dir, symbol string) (*Historical, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("funding dir %q: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("funding dir %q: not a directory", dir)
	}
	path := filepath.Join(dir, symbol+".csv")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	}
	return NewHistorical(symbol, path)
}

// LastTS returns the timestamp of the most recent funding event in the table,
// or zero time if the table is empty. Used by callers (cmd/engine) to detect
// stale CSVs at engine startup — an operator-missed weekly refresh leaves
// trades held past the last entry accruing $0 funding silently.
func (h *Historical) LastTS() time.Time {
	if len(h.times) == 0 {
		return time.Time{}
	}
	return h.times[len(h.times)-1]
}
