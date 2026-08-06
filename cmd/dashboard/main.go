package main

import (
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

var (
	flagPort       = flag.String("port", "8080", "listen port (localhost only)")
	flagJournalDir = flag.String("journal-dir", "results/journal_cache", "path to journal cache dir")
	flagResultsDir = flag.String("results-dir", "results", "path to results dir (for drift_check_history.jsonl)")
)

func main() {
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleIndex)
	mux.HandleFunc("/status", handleStatus)
	mux.HandleFunc("/cohorts", handleCohorts)
	mux.HandleFunc("/symbol/", handleSymbol)
	mux.HandleFunc("/healthz", handleHealthz)

	addr := "127.0.0.1:" + *flagPort
	log.Printf("dashboard listening on http://%s  (journal-dir=%s)", addr, *flagJournalDir)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func loadStateOrError(w http.ResponseWriter) (*State, bool) {
	st, err := LoadState(*flagJournalDir)
	if err != nil {
		http.Error(w, "failed to load journals: "+err.Error(), http.StatusInternalServerError)
		return nil, false
	}
	return st, true
}

// isStale reports whether the newest journal file is older than the 1h staleness
// threshold (zero mtime = nothing read = stale). Shared by every page so the
// stale banner isn't index-only — a fail-open where /cohorts and /status showed
// verdicts with no freshness context.
func isStale(st *State) bool {
	mtime := st.NewestCacheMtime()
	return mtime.IsZero() || time.Since(mtime) > time.Hour
}

// handleIndex — forward-paper gate dashboard.
func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	st, ok := loadStateOrError(w)
	if !ok {
		return
	}
	drift := LoadDriftStatus(*flagResultsDir)

	type pageData struct {
		State     *State
		Drift     DriftStatus
		LiveGates []GateStatus
		Verdict   string
		Now       string
		Stale     bool
	}

	data := pageData{
		State:     st,
		Drift:     drift,
		LiveGates: st.Live.Gates(),
		Verdict:   st.Live.OverallVerdict(),
		Now:       time.Now().UTC().Format("2006-01-02 15:04 UTC"),
		Stale:     isStale(st),
	}
	renderTemplate(w, tmplIndex, data)
}

// handleStatus — per-symbol open position table.
func handleStatus(w http.ResponseWriter, r *http.Request) {
	st, ok := loadStateOrError(w)
	if !ok {
		return
	}
	type pageData struct {
		State *State
		Now   string
		Stale bool
	}
	renderTemplate(w, tmplStatus, pageData{State: st, Now: time.Now().UTC().Format("2006-01-02 15:04 UTC"), Stale: isStale(st)})
}

// handleCohorts — live vs shadows side-by-side with equity curves.
func handleCohorts(w http.ResponseWriter, r *http.Request) {
	st, ok := loadStateOrError(w)
	if !ok {
		return
	}
	all := st.AllCohorts()
	svg := template.HTML(EquityCurveSVG(all))

	type pageData struct {
		State    *State
		CurveSVG template.HTML
		Now      string
		Stale    bool
	}
	renderTemplate(w, tmplCohorts, pageData{State: st, CurveSVG: svg, Now: time.Now().UTC().Format("2006-01-02 15:04 UTC"), Stale: isStale(st)})
}

// handleSymbol — drill-down for one symbol.
func handleSymbol(w http.ResponseWriter, r *http.Request) {
	sym := strings.ToUpper(strings.TrimPrefix(r.URL.Path, "/symbol/"))
	if sym == "" {
		http.Error(w, "missing symbol", http.StatusBadRequest)
		return
	}
	st, ok := loadStateOrError(w)
	if !ok {
		return
	}

	// Build a synthetic per-symbol cohort from live trades.
	symCohort := &Cohort{
		Label:        sym + " (live)",
		SymbolPnL:    make(map[string]float64),
		SymbolTrades: make(map[string]int),
	}
	for _, t := range st.Live.Trades {
		if t.Symbol == sym {
			symCohort.Trades = append(symCohort.Trades, t)
			if t.Outcome != "PARTIAL" {
				symCohort.TotalTrades++
				if t.Outcome == "TARGET" {
					symCohort.Wins++
				}
			}
			symCohort.NetPnL += t.PnlUSD
		}
	}
	cum := 0.0
	symCohort.EquityCurve = make([]float64, len(symCohort.Trades))
	for i, t := range symCohort.Trades {
		cum += t.PnlUSD
		symCohort.EquityCurve[i] = cum
	}
	var openPos *OpenPos
	for i := range st.Live.Opens {
		if st.Live.Opens[i].Symbol == sym {
			p := st.Live.Opens[i]
			openPos = &p
			break
		}
	}

	svg := template.HTML(EquityCurveSVG([]*Cohort{symCohort}))

	type pageData struct {
		Symbol   string
		Cohort   *Cohort
		OpenPos  *OpenPos
		CurveSVG template.HTML
		Now      string
	}
	renderTemplate(w, tmplSymbol, pageData{
		Symbol:   sym,
		Cohort:   symCohort,
		OpenPos:  openPos,
		CurveSVG: svg,
		Now:      time.Now().UTC().Format("2006-01-02 15:04 UTC"),
	})
}

// handleHealthz — health endpoint. Returns 200 + "OK" only when the cache is
// fresh, has data, and the drift detector is clean. Otherwise 503 + "DEGRADED"
// with machine-readable reason tokens (STALE / NO_DATA / DRIFT_KILL / ...), so a
// poller or operator never reads green while monitoring is stale or a kill has
// fired. See healthReport + docs/AUDIT_LENS.md.
func handleHealthz(w http.ResponseWriter, r *http.Request) {
	st, err := LoadState(*flagJournalDir)
	if err != nil {
		http.Error(w, "error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	drift := LoadDriftStatus(*flagResultsDir)
	ok, status, reasons := healthReport(st, drift, time.Hour)

	mtime := st.NewestCacheMtime()
	total := 0
	for _, c := range st.AllCohorts() {
		total += c.TotalTrades
	}

	if !ok {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	fmt.Fprintf(w, "%s\ncache_mtime=%s\ntotal_terminal_trades=%d\n",
		status, mtime.Format(time.RFC3339), total)
	for _, rsn := range reasons {
		fmt.Fprintf(w, "reason=%s\n", rsn)
	}
}

func renderTemplate(w http.ResponseWriter, tmpl *template.Template, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		log.Printf("template error: %v", err)
	}
}

// shortLabel strips "shadow/" prefix for display.
func shortLabel(label string) string {
	if strings.HasPrefix(label, "shadow/") {
		return label[7:]
	}
	return label
}

// verdictClass maps verdict string to a CSS class.
func verdictClass(v string) string {
	switch {
	case strings.HasPrefix(v, "DEPLOY-READY"):
		return "verdict-green"
	case strings.HasPrefix(v, "KILL"):
		return "verdict-red"
	case strings.HasPrefix(v, "DEPLOY-FAIL"):
		return "verdict-orange"
	default:
		return "verdict-gray"
	}
}

func gateClass(g GateStatus) string {
	if g.Pending {
		return "gate-pending"
	}
	if g.Pass {
		return "gate-pass"
	}
	return "gate-fail"
}

func gateLabel(g GateStatus) string {
	if g.Pending {
		return "PENDING"
	}
	if g.Pass {
		return "PASS"
	}
	return "FAIL"
}

// relativePath turns an absolute journal-dir into something for display.
func relativePath(abs string) string {
	if strings.Contains(abs, "results/") {
		i := strings.LastIndex(abs, "results/")
		return abs[i:]
	}
	return filepath.Base(abs)
}

var tmplFuncs = template.FuncMap{
	"shortLabel":   shortLabel,
	"verdictClass": verdictClass,
	"gateClass":    gateClass,
	"gateLabel":    gateLabel,
	"relativePath": relativePath,
	"now":          func() string { return time.Now().UTC().Format("2006-01-02 15:04 UTC") },
	"sub":          func(a, b int) int { return a - b },
	"slice":        func(s []tradeRecord, i, j int) []tradeRecord { return s[i:j] },
}

func mustParse(name, src string) *template.Template {
	return template.Must(template.New(name).Funcs(tmplFuncs).Parse(baseHTML + src))
}

// ── Templates ─────────────────────────────────────────────────────────────────

const baseHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta http-equiv="refresh" content="30">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>fin-trading-engine dashboard</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:ui-monospace,monospace;background:#0f172a;color:#e2e8f0;font-size:13px;padding:16px}
h1{font-size:18px;color:#f8fafc;margin-bottom:4px}
h2{font-size:14px;color:#94a3b8;margin:20px 0 8px;text-transform:uppercase;letter-spacing:.05em}
h3{font-size:13px;color:#cbd5e1;margin:12px 0 6px}
nav{margin-bottom:20px;display:flex;gap:12px;flex-wrap:wrap}
nav a{color:#60a5fa;text-decoration:none;padding:4px 10px;border:1px solid #1e40af;border-radius:4px;font-size:12px}
nav a:hover{background:#1e3a5f}
.stale-banner{background:#7c2d12;color:#fed7aa;padding:8px 12px;border-radius:4px;margin-bottom:12px;font-size:12px}
table{width:100%;border-collapse:collapse;margin-bottom:16px}
th{color:#94a3b8;text-align:left;padding:4px 8px;border-bottom:1px solid #1e293b;font-weight:normal;font-size:11px;text-transform:uppercase}
td{padding:4px 8px;border-bottom:1px solid #0f172a}
tr:hover td{background:#1e293b}
.verdict-green{color:#4ade80;font-weight:bold}
.verdict-red{color:#f87171;font-weight:bold}
.verdict-orange{color:#fb923c;font-weight:bold}
.verdict-gray{color:#94a3b8}
.gate-pass{color:#4ade80}
.gate-fail{color:#f87171;font-weight:bold}
.gate-pending{color:#fbbf24}
.open-pos{background:#1e293b;padding:10px 12px;border-radius:6px;margin-bottom:8px}
.open-sym{color:#60a5fa;font-weight:bold}
.pos-long{color:#4ade80}
.pos-short{color:#f87171}
.meta{color:#64748b;font-size:11px;margin-top:4px}
.drift-ok{color:#4ade80}
.drift-warn{color:#fbbf24}
.drift-dead{color:#f87171}
.section{margin-bottom:24px}
.chip{display:inline-block;padding:2px 8px;border-radius:10px;font-size:11px;margin-left:6px}
.chip-waiting{background:#1e3a5f;color:#93c5fd}
.chip-ready{background:#14532d;color:#86efac}
.chip-kill{background:#7f1d1d;color:#fca5a5}
.chip-fail{background:#431407;color:#fed7aa}
footer{margin-top:24px;color:#334155;font-size:11px}
</style>
</head>
<body>
<h1>fin-trading-engine</h1>
<nav>
  <a href="/">Gates</a>
  <a href="/status">Status</a>
  <a href="/cohorts">Cohorts</a>
  <a href="/healthz">healthz</a>
</nav>
`

var tmplIndex = mustParse("index", `
{{if .Stale}}<div class="stale-banner">⚠ Cache stale — last journal file &gt;1h old. Run: bash scripts/journal_fetch.sh</div>{{end}}
<div class="section">
<h2>Forward-paper go/no-go — live cohort</h2>
<p class="meta">{{.Now}} &nbsp;·&nbsp; cache: {{relativePath .State.CacheDir}}</p>
<br>
<p>Verdict: <span class="{{verdictClass .Verdict}}">{{.Verdict}}</span></p>
<br>
<table>
<tr><th>Gate</th><th>Value</th><th>Status</th></tr>
{{range .LiveGates}}
<tr><td>{{.Label}}</td><td>{{.Value}}</td><td class="{{gateClass .}}">{{gateLabel .}}</td></tr>
{{end}}
</table>
</div>

<div class="section">
<h2>Open positions — live</h2>
{{if .State.Live.Opens}}
{{range .State.Live.Opens}}
<div class="open-pos">
  <span class="open-sym">{{.Symbol}}</span>
  <span class="{{if eq .Side "SHORT"}}pos-short{{else}}pos-long{{end}}">{{.Side}}</span>
  &nbsp; entry={{printf "%.6g" .Entry}} &nbsp; stop={{printf "%.6g" .Stop}} &nbsp; target={{printf "%.6g" .Target}}
  <br>
  <span class="meta">opened {{.OpenTS.Format "2006-01-02 15:04 UTC"}} &nbsp;·&nbsp; {{printf "%.1f" .HoldHours}}h held</span>
</div>
{{end}}
{{else}}
<p class="meta">No open positions.</p>
{{end}}
</div>

<div class="section">
<h2>Drift detector</h2>
{{if .Drift.Missing}}
<p class="drift-dead">⚠ drift_check_history.jsonl missing — run scripts/run_drift_check.sh once to seed</p>
{{else if gt .Drift.AgeDays 10}}
<p class="drift-dead">⚠ last run {{.Drift.LastRunTS.Format "2006-01-02"}} ({{.Drift.AgeDays}}d ago) — cron may be dead</p>
{{else if gt .Drift.AgeDays 7}}
<p class="drift-warn">ⓘ last run {{.Drift.LastRunTS.Format "2006-01-02"}} ({{.Drift.AgeDays}}d ago — 1 cadence cycle)</p>
{{else}}
<p class="drift-ok">✓ last run {{.Drift.LastRunTS.Format "2006-01-02"}} ({{.Drift.AgeDays}}d ago) &nbsp;·&nbsp; exit={{.Drift.ExitCode}}</p>
{{end}}
</div>

<div class="section">
<h2>All cohorts summary</h2>
<table>
<tr><th>Cohort</th><th>Trades</th><th>WR</th><th>Net PnL</th><th>Max DD</th><th>Open</th><th>Today</th><th>Verdict</th></tr>
{{range .State.AllCohorts}}
<tr>
  <td><a href="/cohorts" style="color:#60a5fa">{{shortLabel .Label}}</a></td>
  <td>{{.TotalTrades}}</td>
  <td>{{printf "%.1f%%" .WinRate}}</td>
  <td>{{printf "$%+.0f" .NetPnL}}</td>
  <td>{{printf "$%.0f" .MaxDrawdown}}</td>
  <td>{{len .Opens}}</td>
  <td>{{printf "$%+.0f" .TodayPnL}}</td>
  <td class="{{verdictClass .OverallVerdict}}">{{.OverallVerdict}}</td>
</tr>
{{end}}
</table>
</div>
<footer>auto-refreshes every 30s &nbsp;·&nbsp; read-only &nbsp;·&nbsp; localhost only</footer>
</body></html>`)

var tmplStatus = mustParse("status", `
{{if .Stale}}<div class="stale-banner">⚠ Cache stale — last journal file &gt;1h old. Run: bash scripts/journal_fetch.sh</div>{{end}}
<div class="section">
<h2>Open positions — all cohorts</h2>
<p class="meta">{{.Now}}</p>
<br>
{{range .State.AllCohorts}}
{{if .Opens}}
<h3>{{shortLabel .Label}}</h3>
<table>
<tr><th>Symbol</th><th>Side</th><th>Entry</th><th>Stop</th><th>Target</th><th>Opened</th><th>Held</th></tr>
{{range .Opens}}
<tr>
  <td><a href="/symbol/{{.Symbol}}" style="color:#60a5fa">{{.Symbol}}</a></td>
  <td class="{{if eq .Side "SHORT"}}pos-short{{else}}pos-long{{end}}">{{.Side}}</td>
  <td>{{printf "%.6g" .Entry}}</td>
  <td>{{printf "%.6g" .Stop}}</td>
  <td>{{printf "%.6g" .Target}}</td>
  <td>{{.OpenTS.Format "2006-01-02 15:04"}}</td>
  <td>{{printf "%.1fh" .HoldHours}}</td>
</tr>
{{end}}
</table>
{{end}}
{{end}}
</div>

<div class="section">
<h2>Recent closes — live (last 20)</h2>
<table>
<tr><th>Symbol</th><th>TS</th><th>Outcome</th><th>PnL</th><th>MFE-R</th><th>MAE-R</th></tr>
{{$trades := .State.Live.Trades}}
{{$n := len $trades}}
{{$start := 0}}{{if gt $n 20}}{{$start = sub $n 20}}{{end}}
{{range slice $trades $start $n}}
<tr>
  <td><a href="/symbol/{{.Symbol}}" style="color:#60a5fa">{{.Symbol}}</a></td>
  <td>{{.TS.Format "01-02 15:04"}}</td>
  <td>{{.Outcome}}</td>
  <td>{{printf "$%+.0f" .PnlUSD}}</td>
  <td>{{printf "%.2f" .MFER}}</td>
  <td>{{printf "%.2f" .MAER}}</td>
</tr>
{{end}}
</table>
</div>
<footer>auto-refreshes every 30s</footer>
</body></html>`)

var tmplCohorts = mustParse("cohorts", `
{{if .Stale}}<div class="stale-banner">⚠ Cache stale — last journal file &gt;1h old. Run: bash scripts/journal_fetch.sh</div>{{end}}
<div class="section">
<h2>Cohort comparison</h2>
<p class="meta">{{.Now}}</p>
<br>
{{.CurveSVG}}
<br><br>
<table>
<tr><th>Cohort</th><th>Trades</th><th>WR%</th><th>Net PnL</th><th>Fee bp</th><th>Slip bp</th><th>Max DD</th><th>Days</th><th>Open</th></tr>
{{range .State.AllCohorts}}
<tr>
  <td>{{shortLabel .Label}}</td>
  <td>{{.TotalTrades}}</td>
  <td>{{printf "%.1f" .WinRate}}</td>
  <td>{{printf "$%+.0f" .NetPnL}}</td>
  <td>{{printf "%.2f" .FeeBps}}</td>
  <td>{{printf "%.2f" .SlipBps}}</td>
  <td>{{printf "$%.0f" .MaxDrawdown}}</td>
  <td>{{.DaysElapsed}}</td>
  <td>{{len .Opens}}</td>
</tr>
{{end}}
</table>
</div>
<footer>auto-refreshes every 30s</footer>
</body></html>`)

var tmplSymbol = mustParse("symbol", `
<div class="section">
<h2>{{.Symbol}}</h2>
<p class="meta">{{.Now}}</p>
<br>
{{if .OpenPos}}
<div class="open-pos">
  <span class="open-sym">{{.Symbol}}</span>
  <span class="{{if eq .OpenPos.Side "SHORT"}}pos-short{{else}}pos-long{{end}}">OPEN {{.OpenPos.Side}}</span>
  &nbsp; entry={{printf "%.6g" .OpenPos.Entry}} &nbsp; stop={{printf "%.6g" .OpenPos.Stop}} &nbsp; target={{printf "%.6g" .OpenPos.Target}}
  <br>
  <span class="meta">opened {{.OpenPos.OpenTS.Format "2006-01-02 15:04 UTC"}} &nbsp;·&nbsp; {{printf "%.1f" .OpenPos.HoldHours}}h held</span>
</div>
<br>
{{end}}
<p>Trades: {{.Cohort.TotalTrades}} &nbsp;·&nbsp; WR: {{printf "%.1f%%" .Cohort.WinRate}} &nbsp;·&nbsp; Net: {{printf "$%+.0f" .Cohort.NetPnL}}</p>
<br>
{{.CurveSVG}}
<br><br>
<h3>Trade history</h3>
<table>
<tr><th>Date</th><th>Outcome</th><th>PnL</th><th>MFE-R</th><th>MAE-R</th></tr>
{{range .Cohort.Trades}}
<tr>
  <td>{{.TS.Format "2006-01-02 15:04"}}</td>
  <td>{{.Outcome}}</td>
  <td>{{printf "$%+.0f" .PnlUSD}}</td>
  <td>{{printf "%.2f" .MFER}}</td>
  <td>{{printf "%.2f" .MAER}}</td>
</tr>
{{end}}
</table>
</div>
<footer>auto-refreshes every 30s</footer>
</body></html>`)
