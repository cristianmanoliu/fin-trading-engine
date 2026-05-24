package main

import (
	"fmt"
	"math"
	"strings"
)

const (
	chartW = 700
	chartH = 200
	chartPadL = 55
	chartPadR = 15
	chartPadT = 15
	chartPadB = 30
)

// plotW/plotH are the drawable area dimensions.
const (
	plotW = chartW - chartPadL - chartPadR
	plotH = chartH - chartPadT - chartPadB
)

// EquityCurveSVG returns an inline SVG equity-curve chart for one or more cohorts.
// Each cohort contributes one polyline. Colors cycle through a small palette.
// If all equity curves are empty, returns an empty string.
func EquityCurveSVG(cohorts []*Cohort) string {
	if len(cohorts) == 0 {
		return ""
	}

	// Find global y-range across all cohorts.
	globalMin := 0.0
	globalMax := 0.0
	hasData := false
	for _, c := range cohorts {
		for _, v := range c.EquityCurve {
			if !hasData || v < globalMin {
				globalMin = v
			}
			if !hasData || v > globalMax {
				globalMax = v
			}
			hasData = true
		}
	}
	if !hasData {
		return ""
	}

	// Pad range slightly so line doesn't hug top/bottom.
	span := globalMax - globalMin
	if span == 0 {
		span = 1
	}
	globalMin -= span * 0.05
	globalMax += span * 0.05
	span = globalMax - globalMin

	// Map data coords to SVG coords.
	toX := func(i, n int) float64 {
		if n <= 1 {
			return float64(chartPadL)
		}
		return float64(chartPadL) + float64(i)/float64(n-1)*float64(plotW)
	}
	toY := func(v float64) float64 {
		frac := (v - globalMin) / span
		return float64(chartPadT) + float64(plotH)*(1-frac)
	}

	colors := []string{"#4ade80", "#60a5fa", "#f97316", "#a78bfa"}
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf(
		`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" style="background:#1e293b;border-radius:6px;">`,
		chartW, chartH,
	))

	// Zero line.
	if globalMin < 0 && globalMax > 0 {
		y0 := toY(0)
		sb.WriteString(fmt.Sprintf(
			`<line x1="%d" y1="%.1f" x2="%d" y2="%.1f" stroke="#475569" stroke-width="1" stroke-dasharray="4,3"/>`,
			chartPadL, y0, chartW-chartPadR, y0,
		))
	}

	// Y-axis labels (3 ticks).
	for i := 0; i <= 2; i++ {
		v := globalMin + span*float64(i)/2
		y := toY(v)
		label := fmt.Sprintf("$%+.0f", v)
		sb.WriteString(fmt.Sprintf(
			`<text x="%d" y="%.1f" fill="#94a3b8" font-size="10" text-anchor="end" dominant-baseline="middle">%s</text>`,
			chartPadL-4, y, label,
		))
		sb.WriteString(fmt.Sprintf(
			`<line x1="%d" y1="%.1f" x2="%d" y2="%.1f" stroke="#1e3a5f" stroke-width="1"/>`,
			chartPadL, y, chartW-chartPadR, y,
		))
	}

	// Polylines for each cohort.
	for ci, c := range cohorts {
		if len(c.EquityCurve) == 0 {
			continue
		}
		color := colors[ci%len(colors)]
		n := len(c.EquityCurve)

		var pts []string
		for i, v := range c.EquityCurve {
			pts = append(pts, fmt.Sprintf("%.1f,%.1f", toX(i, n), toY(v)))
		}
		sb.WriteString(fmt.Sprintf(
			`<polyline points="%s" fill="none" stroke="%s" stroke-width="1.5" stroke-linejoin="round"/>`,
			strings.Join(pts, " "), color,
		))
	}

	// Legend.
	lx := chartPadL + 8
	for ci, c := range cohorts {
		color := colors[ci%len(colors)]
		ly := float64(chartPadT + 12 + ci*16)
		shortLabel := c.Label
		if strings.HasPrefix(shortLabel, "shadow/") {
			shortLabel = shortLabel[7:]
		}
		sb.WriteString(fmt.Sprintf(
			`<rect x="%d" y="%.1f" width="18" height="3" fill="%s"/>`,
			lx, ly, color,
		))
		sb.WriteString(fmt.Sprintf(
			`<text x="%d" y="%.1f" fill="#cbd5e1" font-size="10" dominant-baseline="middle">%s ($%+.0f)</text>`,
			lx+22, ly+1, shortLabel, c.NetPnL,
		))
	}

	sb.WriteString(`</svg>`)
	return sb.String()
}

// SymbolBarSVG returns an inline SVG bar chart of PnL by symbol for one cohort.
func SymbolBarSVG(c *Cohort) string {
	if len(c.SymbolPnL) == 0 {
		return ""
	}

	// Build sorted list by abs PnL.
	type bar struct {
		sym string
		pnl float64
	}
	bars := make([]bar, 0, len(c.SymbolPnL))
	for sym, pnl := range c.SymbolPnL {
		bars = append(bars, bar{sym, pnl})
	}
	// Sort: biggest positive first, then negative by magnitude.
	// Actually sort by value descending for natural display.
	sortBars := func(a, b bar) bool { return a.pnl > b.pnl }
	_ = sortBars
	// Sort descending by value.
	for i := 0; i < len(bars); i++ {
		for j := i + 1; j < len(bars); j++ {
			if bars[j].pnl > bars[i].pnl {
				bars[i], bars[j] = bars[j], bars[i]
			}
		}
	}

	maxAbs := 0.0
	for _, b := range bars {
		if math.Abs(b.pnl) > maxAbs {
			maxAbs = math.Abs(b.pnl)
		}
	}
	if maxAbs == 0 {
		return ""
	}

	barH := 16
	gap := 4
	labelW := 90
	barMaxW := chartW - labelW - 80
	svgH := (barH+gap)*len(bars) + 20

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(
		`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" style="background:#1e293b;border-radius:6px;">`,
		chartW, svgH,
	))

	for i, b := range bars {
		y := 10 + i*(barH+gap)
		w := int(math.Abs(b.pnl) / maxAbs * float64(barMaxW))
		color := "#4ade80"
		if b.pnl < 0 {
			color = "#f87171"
		}
		sb.WriteString(fmt.Sprintf(
			`<text x="%d" y="%d" fill="#cbd5e1" font-size="11" text-anchor="end" dominant-baseline="hanging">%s</text>`,
			labelW-4, y, b.sym,
		))
		if w > 0 {
			sb.WriteString(fmt.Sprintf(
				`<rect x="%d" y="%d" width="%d" height="%d" fill="%s" rx="2"/>`,
				labelW, y, w, barH, color,
			))
		}
		sb.WriteString(fmt.Sprintf(
			`<text x="%d" y="%d" fill="#94a3b8" font-size="10" dominant-baseline="hanging">$%+.0f</text>`,
			labelW+w+4, y, b.pnl,
		))
	}

	sb.WriteString(`</svg>`)
	return sb.String()
}
