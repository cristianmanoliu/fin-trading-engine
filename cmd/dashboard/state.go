package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// journalEntry mirrors pkg/execution/stub.go:journalEntry exactly.
// Dashboard is read-only so we replicate rather than export.
type journalEntry struct {
	Event   string  `json:"event"`
	Symbol  string  `json:"symbol"`
	TS      string  `json:"ts"`
	Side    string  `json:"side"`
	Entry   float64 `json:"entry"`
	Exit    float64 `json:"exit,omitempty"`
	Stop    float64 `json:"stop"`
	Target  float64 `json:"target"`
	PnlPts  float64 `json:"pnl_pts,omitempty"`
	PnlUSD  float64 `json:"pnl_usd,omitempty"`
	Outcome string  `json:"outcome,omitempty"`
	Reason  string  `json:"reason"`
	MFER    float64 `json:"mfe_r,omitempty"`
	MAER    float64 `json:"mae_r,omitempty"`
	GrossUSD   float64 `json:"gross_usd,omitempty"`
	FeeUSD     float64 `json:"fee_usd,omitempty"`
	SlipUSD    float64 `json:"slip_usd,omitempty"`
	FundingUSD float64 `json:"funding_usd,omitempty"`
	Notional   float64 `json:"notional_usd,omitempty"`
}

// OpenPos is a reconstructed open position (from journal — no live price).
type OpenPos struct {
	Symbol  string
	Side    string
	Entry   float64
	Stop    float64
	Target  float64
	OpenTS  time.Time
}

func (p OpenPos) HoldHours() float64 {
	return time.Since(p.OpenTS).Hours()
}

// tradeRecord is a single closed trade.
type tradeRecord struct {
	Symbol  string
	TS      time.Time
	Outcome string
	PnlUSD  float64
	FeeUSD  float64
	SlipUSD float64
	Notional float64
	MFER    float64
	MAER    float64
}

// Cohort holds aggregated stats for one journal directory (live or shadow/label).
type Cohort struct {
	Label   string
	Trades  []tradeRecord
	Opens   []OpenPos

	// Cached aggregates (populated by aggregate()).
	TotalTrades    int
	Wins           int
	NetPnL         float64
	TotalFeeUSD    float64
	SlipUSDLosers  float64
	NotionalAll    float64
	NotionalLosers float64

	FirstTradeTS time.Time
	LastTradeTS  time.Time

	SymbolPnL    map[string]float64
	SymbolTrades map[string]int

	// Equity curve: cumulative PnL per trade, sorted by TS.
	EquityCurve []float64

	CacheFreshnessTS time.Time // mtime of newest .jsonl file read
}

func (c *Cohort) WinRate() float64 {
	if c.TotalTrades == 0 {
		return 0
	}
	return float64(c.Wins) / float64(c.TotalTrades) * 100
}

func (c *Cohort) FeeBps() float64 {
	if c.NotionalAll == 0 {
		return 0
	}
	return c.TotalFeeUSD / c.NotionalAll * 10000
}

func (c *Cohort) SlipBps() float64 {
	if c.NotionalLosers == 0 {
		return 0
	}
	return c.SlipUSDLosers / c.NotionalLosers * 10000
}

func (c *Cohort) DaysElapsed() int {
	if c.FirstTradeTS.IsZero() {
		return 0
	}
	return int(time.Since(c.FirstTradeTS).Hours() / 24)
}

// MaxDrawdown returns max drawdown in USD from the equity curve.
func (c *Cohort) MaxDrawdown() float64 {
	if len(c.EquityCurve) == 0 {
		return 0
	}
	peak := 0.0
	maxDD := 0.0
	for _, v := range c.EquityCurve {
		if v > peak {
			peak = v
		}
		dd := peak - v
		if dd > maxDD {
			maxDD = dd
		}
	}
	return maxDD
}

// TopSymbols returns up to n symbols sorted by absolute PnL contribution.
func (c *Cohort) TopSymbols(n int) []symStat {
	if len(c.SymbolPnL) == 0 {
		return nil
	}
	out := make([]symStat, 0, len(c.SymbolPnL))
	for sym, pnl := range c.SymbolPnL {
		out = append(out, symStat{Symbol: sym, PnL: pnl, Trades: c.SymbolTrades[sym]})
	}
	sort.Slice(out, func(i, j int) bool {
		return math.Abs(out[i].PnL) > math.Abs(out[j].PnL)
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// MaxSymbolPct returns the highest single-symbol % of absolute total PnL.
func (c *Cohort) MaxSymbolPct() (float64, string) {
	absTotal := math.Abs(c.NetPnL)
	if absTotal == 0 || len(c.SymbolPnL) == 0 {
		return 0, ""
	}
	max := 0.0
	maxSym := ""
	for sym, pnl := range c.SymbolPnL {
		pct := math.Abs(pnl) / absTotal * 100
		if pct > max {
			max = pct
			maxSym = sym
		}
	}
	return max, maxSym
}

type symStat struct {
	Symbol string
	PnL    float64
	Trades int
}

// State is the full dashboard state loaded from journal cache.
type State struct {
	Live    *Cohort
	Shadows []*Cohort // alt5-15-336, alt5-15-504, bb20 in directory order
	LoadedAt time.Time
	CacheDir string
}

// LoadState reads all JSONL files from cacheDir and builds the State.
func LoadState(cacheDir string) (*State, error) {
	st := &State{
		CacheDir: cacheDir,
		LoadedAt: time.Now(),
	}

	live, err := loadCohort("live", cacheDir)
	if err != nil {
		return nil, fmt.Errorf("live cohort: %w", err)
	}
	st.Live = live

	shadowDir := filepath.Join(cacheDir, "shadow")
	entries, _ := os.ReadDir(shadowDir)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		label := "shadow/" + e.Name()
		cohort, err := loadCohort(label, filepath.Join(shadowDir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("shadow %s: %w", e.Name(), err)
		}
		st.Shadows = append(st.Shadows, cohort)
	}

	return st, nil
}

// AllCohorts returns live + shadows in order.
func (s *State) AllCohorts() []*Cohort {
	out := make([]*Cohort, 0, 1+len(s.Shadows))
	out = append(out, s.Live)
	out = append(out, s.Shadows...)
	return out
}

// NewestCacheMtime returns the most recent mtime across all loaded journals.
func (s *State) NewestCacheMtime() time.Time {
	t := time.Time{}
	for _, c := range s.AllCohorts() {
		if c.CacheFreshnessTS.After(t) {
			t = c.CacheFreshnessTS
		}
	}
	return t
}

// loadCohort reads all *.jsonl files in dir, parses them, and builds a Cohort.
func loadCohort(label, dir string) (*Cohort, error) {
	c := &Cohort{
		Label:        label,
		SymbolPnL:    make(map[string]float64),
		SymbolTrades: make(map[string]int),
	}

	files, err := filepath.Glob(filepath.Join(dir, "*-*.jsonl"))
	if err != nil {
		return c, nil // glob failure = treat as empty
	}
	sort.Strings(files) // chronological: SYMBOL-YYYY-MM.jsonl alphabetic = time order

	// Track opens per symbol: latest open event per symbol.
	// An open without a subsequent close = currently open position.
	type openState struct {
		entry journalEntry
		openCount int
		closeCount int
	}
	symState := make(map[string]*openState)

	var freshest time.Time

	for _, f := range files {
		info, err := os.Stat(f)
		if err == nil && info.ModTime().After(freshest) {
			freshest = info.ModTime()
		}

		readJSONL(f, func(e journalEntry) {
			switch e.Event {
			case "open":
				st, ok := symState[e.Symbol]
				if !ok {
					st = &openState{}
					symState[e.Symbol] = st
				}
				st.entry = e
				st.openCount++
			case "close":
				st, ok := symState[e.Symbol]
				if !ok {
					st = &openState{}
					symState[e.Symbol] = st
				}
				st.closeCount++

				// Only terminal closes count toward trade stats (D3 resolution).
				isTerminal := e.Outcome != "PARTIAL"

				ts, _ := time.Parse(time.RFC3339, e.TS)
				rec := tradeRecord{
					Symbol:   e.Symbol,
					TS:       ts,
					Outcome:  e.Outcome,
					PnlUSD:   e.PnlUSD,
					FeeUSD:   e.FeeUSD,
					SlipUSD:  e.SlipUSD,
					Notional: e.Notional,
					MFER:     e.MFER,
					MAER:     e.MAER,
				}
				c.Trades = append(c.Trades, rec)

				if isTerminal {
					c.TotalTrades++
					if e.Outcome == "TARGET" {
						c.Wins++
					}
					if c.FirstTradeTS.IsZero() || ts.Before(c.FirstTradeTS) {
						c.FirstTradeTS = ts
					}
					if ts.After(c.LastTradeTS) {
						c.LastTradeTS = ts
					}
				}

				c.NetPnL += e.PnlUSD
				c.TotalFeeUSD += e.FeeUSD
				c.NotionalAll += e.Notional
				if e.Outcome == "STOP" {
					c.SlipUSDLosers += e.SlipUSD
					c.NotionalLosers += e.Notional
				}
				c.SymbolPnL[e.Symbol] += e.PnlUSD
				if isTerminal {
					c.SymbolTrades[e.Symbol]++
				}
			}
		})
	}

	// Build equity curve from all trades (including partials) sorted by TS.
	sort.Slice(c.Trades, func(i, j int) bool {
		return c.Trades[i].TS.Before(c.Trades[j].TS)
	})
	cum := 0.0
	c.EquityCurve = make([]float64, len(c.Trades))
	for i, t := range c.Trades {
		cum += t.PnlUSD
		c.EquityCurve[i] = cum
	}

	// Reconstruct open positions: symbols where openCount > closeCount.
	for sym, st := range symState {
		if st.openCount > st.closeCount {
			ts, _ := time.Parse(time.RFC3339, st.entry.TS)
			c.Opens = append(c.Opens, OpenPos{
				Symbol: sym,
				Side:   st.entry.Side,
				Entry:  st.entry.Entry,
				Stop:   st.entry.Stop,
				Target: st.entry.Target,
				OpenTS: ts,
			})
		}
	}
	sort.Slice(c.Opens, func(i, j int) bool {
		return c.Opens[i].Symbol < c.Opens[j].Symbol
	})

	c.CacheFreshnessTS = freshest
	return c, nil
}

// readJSONL calls fn for each valid JSON line in the file.
// Malformed lines are silently skipped (matches engine's silent-on-corrupt-input pattern).
func readJSONL(path string, fn func(journalEntry)) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e journalEntry
		if json.Unmarshal(line, &e) != nil {
			continue // corrupt / partial line — skip
		}
		fn(e)
	}
}

// GateStatus encodes CLAUDE.md forward-paper go/no-go gates.
type GateStatus struct {
	Label string
	Value string
	Pass  bool
	Pending bool // insufficient data
}

const (
	minTrades   = 150
	minDays     = 60
	minWRPct    = 14.3
	maxSymPct   = 40.0
	maxFeeBps   = 12.0
	maxSlipBps  = 20.0
)

// Gates evaluates the forward-paper criteria for a cohort.
func (c *Cohort) Gates() []GateStatus {
	n := c.TotalTrades
	days := c.DaysElapsed()
	wr := c.WinRate()
	feeBps := c.FeeBps()
	slipBps := c.SlipBps()
	netPnL := c.NetPnL
	maxSym, maxSymName := c.MaxSymbolPct()

	dataFloor := n >= minTrades && days >= minDays

	gate := func(label, val string, pass, pending bool) GateStatus {
		return GateStatus{Label: label, Value: val, Pass: pass, Pending: pending}
	}

	return []GateStatus{
		gate(
			"Trades", fmt.Sprintf("%d / %d", n, minTrades),
			n >= minTrades, false,
		),
		gate(
			"Days", fmt.Sprintf("%d / %d", days, minDays),
			days >= minDays, false,
		),
		gate(
			"Win rate", fmt.Sprintf("%.1f%% / ≥%.1f%%", wr, minWRPct),
			wr >= minWRPct, !dataFloor,
		),
		gate(
			"Net PnL", fmt.Sprintf("$%+.0f", netPnL),
			netPnL >= 0, !dataFloor,
		),
		gate(
			"Fee bps", fmt.Sprintf("%.2f / ≤%.0f bp", feeBps, maxFeeBps),
			c.NotionalAll > 0 && feeBps <= maxFeeBps,
			c.NotionalAll == 0,
		),
		gate(
			"Slip bps (losers)", fmt.Sprintf("%.2f / ≤%.0f bp", slipBps, maxSlipBps),
			c.NotionalLosers > 0 && slipBps <= maxSlipBps,
			c.NotionalLosers == 0,
		),
		gate(
			"Single-sym %", fmt.Sprintf("%.1f%% (%s) / ≤%.0f%%", maxSym, maxSymName, maxSymPct),
			maxSym <= maxSymPct, !dataFloor,
		),
	}
}

// OverallVerdict returns a short verdict string.
func (c *Cohort) OverallVerdict() string {
	n := c.TotalTrades
	days := c.DaysElapsed()
	slipBps := c.SlipBps()
	feeBps := c.FeeBps()

	if c.NotionalLosers > 0 && slipBps > 25.0 {
		return "KILL"
	}
	if c.NotionalAll > 0 && feeBps > maxFeeBps {
		return "DEPLOY-FAIL"
	}
	if c.NotionalLosers > 0 && slipBps > maxSlipBps {
		return "DEPLOY-FAIL"
	}
	if n < minTrades || days < minDays {
		return "WAITING"
	}
	maxSym, _ := c.MaxSymbolPct()
	if c.NetPnL < 0 || c.WinRate() < minWRPct || maxSym > maxSymPct {
		return "DEPLOY-FAIL"
	}
	return "DEPLOY-READY"
}

// DriftStatus reads the last entry from drift_check_history.jsonl.
type DriftStatus struct {
	LastRunTS  time.Time
	ExitCode   int
	AgeDays    int
	Missing    bool
}

func LoadDriftStatus(resultsDir string) DriftStatus {
	path := filepath.Join(resultsDir, "drift_check_history.jsonl")
	f, err := os.Open(path)
	if err != nil {
		return DriftStatus{Missing: true}
	}
	defer f.Close()

	var lastLine []byte
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if len(sc.Bytes()) > 0 {
			lastLine = make([]byte, len(sc.Bytes()))
			copy(lastLine, sc.Bytes())
		}
	}
	if len(lastLine) == 0 {
		return DriftStatus{Missing: true}
	}

	var rec struct {
		TS       string `json:"ts"`
		ExitCode int    `json:"exit_code"`
	}
	if json.Unmarshal(lastLine, &rec) != nil {
		return DriftStatus{Missing: true}
	}
	ts, err := time.Parse(time.RFC3339, rec.TS)
	if err != nil {
		return DriftStatus{Missing: true}
	}
	age := int(time.Since(ts).Hours() / 24)
	return DriftStatus{
		LastRunTS: ts,
		ExitCode:  rec.ExitCode,
		AgeDays:   age,
	}
}

// TodayPnL returns net PnL from trades closed today (UTC).
func (c *Cohort) TodayPnL() float64 {
	today := time.Now().UTC().Format("2006-01-02")
	sum := 0.0
	for _, t := range c.Trades {
		if strings.HasPrefix(t.TS.UTC().Format(time.RFC3339), today) {
			sum += t.PnlUSD
		}
	}
	return sum
}
