package strategy

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// SignalContext captures rich state at signal-emission time so forward-paper
// trades can be retroactively pattern-matched to wins/losses. Produced by
// Runner.evaluateEntry just before executor.OnSignal — i.e. after all gates
// (Primed, prevEMA != 0, bias, side-filter, funding-filter, vol-filter,
// confluence) have passed. Filtered signals are NOT captured here.
//
// Schema mirrors the existing Stub journal style (event-tagged JSONL) so
// downstream tools can read both stream types with the same parser.
type SignalContext struct {
	Event  string `json:"event"` // always "signal_context"
	Symbol string `json:"symbol"`
	TS     string `json:"ts"`    // RFC3339 UTC, signal entry time (== sig.Timestamp)
	Label  string `json:"label"` // "live" | shadow label (e.g. "bb20", "alt5-15-504")

	// Trade fundamentals (mirrors what executor.OnSignal will see).
	Side   string  `json:"side"`
	Entry  float64 `json:"entry"`
	Stop   float64 `json:"stop"`
	Target float64 `json:"target"`
	RR     float64 `json:"rr"`
	Reason string  `json:"reason"`

	// Indicator state at signal time. Fields are omitted when not primed
	// (omitempty + zero default), so consumers can detect "indicator not
	// available" cleanly without sentinel values.
	EMA9              float64 `json:"ema9,omitempty"`
	EMA21             float64 `json:"ema21,omitempty"`
	EMASpreadPct      float64 `json:"ema_spread_pct,omitempty"`
	ATR               float64 `json:"atr,omitempty"`
	RealizedVol30dAnn float64 `json:"realized_vol_30d_ann,omitempty"`
	BBUpper           float64 `json:"bb_upper,omitempty"`
	BBMid             float64 `json:"bb_mid,omitempty"`
	BBLower           float64 `json:"bb_lower,omitempty"`

	// Market context — sourced from Runner state, not the detector.
	Bias       int     `json:"bias"` // -1 short, 0 neutral, +1 long
	VWAP       float64 `json:"vwap,omitempty"`
	PDH        float64 `json:"pdh,omitempty"`
	PDL        float64 `json:"pdl,omitempty"`
	DistPDHPct float64 `json:"dist_pdh_pct,omitempty"`
	DistPDLPct float64 `json:"dist_pdl_pct,omitempty"`

	// Funding context — populated only when a Historical funding provider
	// is wired (via FundingFilter). For live engines this is the rate at
	// the candle close time, not "now"; for backtest it's the historical rate.
	FundingRate8h    float64 `json:"funding_rate_8h,omitempty"`
	FundingBpsPerDay float64 `json:"funding_bps_per_day,omitempty"`

	// Strategy mode tags — useful when comparing signals across shadow variants.
	SignalTF   string `json:"signal_tf"`
	SideFilter string `json:"side_filter,omitempty"`
}

// IndicatorSnapshot is the EntryDetector-internal slice of state needed by
// Runner to build a SignalContext. Returned by EntryDetector.Snapshot().
type IndicatorSnapshot struct {
	EMA9              float64
	EMA21             float64
	ATR               float64
	RealizedVol30dAnn float64
	BBUpper           float64
	BBMid             float64
	BBLower           float64
}

// SignalContextWriter writes SignalContext records to monthly JSONL files.
// Same monthly-rotation pattern as Stub.appendJournal; intentionally separate
// so the signal-context stream is decoupled from trade execution (a filter
// could veto a signal AT the executor layer in future without losing the
// pre-execution context).
//
// One Writer per (Runner, symbol). Concurrent writes are guarded by an
// internal mutex; in practice the Runner is single-goroutine so the mutex
// is defensive against future use.
type SignalContextWriter struct {
	Dir    string
	Symbol string

	mu    sync.Mutex
	file  *os.File
	month string
}

// Write appends a SignalContext record. No-op when Writer is nil or Dir is
// empty (matches the journal-disabled-when-path-empty pattern).
func (w *SignalContextWriter) Write(ctx SignalContext) {
	if w == nil || w.Dir == "" {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	month := time.Now().UTC().Format("2006-01")
	if w.file == nil || month != w.month {
		if w.file != nil {
			_ = w.file.Close()
		}
		if err := os.MkdirAll(w.Dir, 0755); err != nil {
			// Audit-pattern fix 2026-05-10 (3rd paired-implementation):
			// elevated from slog.Warn to slog.Error. Signal-context records
			// feed retroactive forward-paper pattern-matching ("why did this
			// signal win/lose?") — silent loss compromises a key analysis
			// surface. Same shape as Stub.appendJournal (4154374) and
			// BinanceLive.appendJournal (028e6a2) — the audit pattern's
			// "always check the sibling" lesson manifests for the third time.
			slog.Error("signal context mkdir failed — SIGNAL CONTEXT MAY BE LOST",
				"err", err, "dir", w.Dir, "symbol", w.Symbol, "label", ctx.Label)
			return
		}
		name := filepath.Join(w.Dir, fmt.Sprintf("%s-%s.jsonl", w.Symbol, month))
		f, err := os.OpenFile(name, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			slog.Error("signal context open failed — SIGNAL CONTEXT MAY BE LOST",
				"err", err, "path", name, "symbol", w.Symbol, "label", ctx.Label)
			return
		}
		w.file = f
		w.month = month
	}
	line, err := json.Marshal(ctx)
	if err != nil {
		// json.Marshal of a struct with primitive fields rarely fails, but if
		// it does (NaN / Inf in a float field), silent return would lose the
		// record without operator visibility. Surface it.
		slog.Error("signal context marshal failed — SIGNAL CONTEXT LOST",
			"err", err, "symbol", w.Symbol, "label", ctx.Label)
		return
	}
	line = append(line, '\n')
	if _, err := w.file.Write(line); err != nil {
		// Mirrors Stub.appendJournal 4154374 + BinanceLive 028e6a2 fix:
		// reset file handle on Write failure so the next call goes through
		// open-or-create rather than persisting into "permanently broken"
		// silent failure. Without this, every subsequent write to the same
		// dead handle fails identically — the operator's signal-context
		// stream is silently dead while the engine keeps emitting signals.
		slog.Error("signal context write failed — SIGNAL CONTEXT MAY BE LOST",
			"err", err, "symbol", w.Symbol, "label", ctx.Label)
		_ = w.file.Close()
		w.file = nil
		w.month = ""
	}
}

// Close releases the journal file handle. Safe to call on nil receiver.
func (w *SignalContextWriter) Close() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
	}
}
